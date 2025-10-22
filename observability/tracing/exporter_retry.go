package tracing

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v5"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	errdetails "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

type retryingClient struct {
	delegate otlptrace.Client
	logger   *exporterLogger
	settings retrySettings
}

type retrySettings struct {
	initialInterval time.Duration
	maxInterval     time.Duration
	maxElapsed      time.Duration
}

func newRetryingExporter(ctx context.Context, settings retrySettings, logger *exporterLogger, clientOpts ...otlptracegrpc.Option) (sdktrace.SpanExporter, error) {
	// 禁用内置重试，避免重复回退逻辑。
	clientOpts = append(clientOpts, otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{
		Enabled:         false,
		InitialInterval: 0,
		MaxInterval:     0,
		MaxElapsedTime:  0,
	}))

	client := otlptracegrpc.NewClient(clientOpts...)
	retrying := &retryingClient{
		delegate: client,
		logger:   logger,
		settings: settings,
	}

	return otlptrace.New(ctx, retrying)
}

func defaultRetrySettings() retrySettings {
	return retrySettings{
		initialInterval: 5 * time.Second,
		maxInterval:     30 * time.Second,
		maxElapsed:      time.Minute,
	}
}

// Start implements otlptrace.Client.
func (c *retryingClient) Start(ctx context.Context) error {
	return c.delegate.Start(ctx)
}

// Stop implements otlptrace.Client.
func (c *retryingClient) Stop(ctx context.Context) error {
	return c.delegate.Stop(ctx)
}

// UploadTraces 实现自定义的指数退避重试，并在每次失败/恢复时输出结构化日志。
func (c *retryingClient) UploadTraces(ctx context.Context, spans []*tracepb.ResourceSpans) error {
	attempt := 0
	start := time.Now()

	backoffSeq := backoff.NewExponentialBackOff()
	backoffSeq.InitialInterval = c.settings.initialInterval
	backoffSeq.MaxInterval = c.settings.maxInterval
	backoffSeq.Multiplier = backoff.DefaultMultiplier
	backoffSeq.RandomizationFactor = backoff.DefaultRandomizationFactor
	backoffSeq.Reset()

	for {
		err := c.delegate.UploadTraces(ctx, spans)
		if err == nil {
			c.logger.logRecovery(spanCount(spans), attempt, time.Since(start))
			return nil
		}

		attempt++

		retryable, code, throttle := classifyExportError(err)
		if !retryable {
			c.logger.logPermanentFailure(err, attempt, code)
			return &loggedExporterError{err: err}
		}

		delay := nextDelay(backoffSeq, throttle)
		if c.settings.maxElapsed > 0 && time.Since(start)+delay > c.settings.maxElapsed {
			finalErr := fmt.Errorf("otel exporter max retry time would elapse: %w", err)
			c.logger.logPermanentFailure(finalErr, attempt, code)
			return &loggedExporterError{err: finalErr}
		}

		c.logger.logRetry(err, attempt, delay, throttle, code)

		if err := waitWithContext(ctx, delay); err != nil {
			finalErr := fmt.Errorf("otel exporter retry aborted: %w", err)
			c.logger.logContextFailure(finalErr, attempt)
			return &loggedExporterError{err: finalErr}
		}
	}
}

func classifyExportError(err error) (retryable bool, code codes.Code, throttle time.Duration) {
	if err == nil {
		return false, codes.OK, 0
	}
	s, ok := status.FromError(err)
	if !ok {
		return false, codes.Unknown, 0
	}
	retryable, throttle = retryableStatus(s)
	return retryable, s.Code(), throttle
}

func retryableStatus(s *status.Status) (bool, time.Duration) {
	switch s.Code() {
	case codes.Canceled,
		codes.DeadlineExceeded,
		codes.Aborted,
		codes.OutOfRange,
		codes.Unavailable,
		codes.DataLoss:
		_, d := throttleDelay(s)
		return true, d
	case codes.ResourceExhausted:
		retryable, d := throttleDelay(s)
		if !retryable {
			return true, 0
		}
		return true, d
	}
	return false, 0
}

func throttleDelay(s *status.Status) (bool, time.Duration) {
	for _, detail := range s.Details() {
		if t, ok := detail.(*errdetails.RetryInfo); ok {
			return true, t.RetryDelay.AsDuration()
		}
	}
	return false, 0
}

func nextDelay(seq *backoff.ExponentialBackOff, throttle time.Duration) time.Duration {
	delay := seq.NextBackOff()
	if delay == backoff.Stop {
		delay = seq.MaxInterval
	}
	if throttle > delay {
		return throttle
	}
	return delay
}

func waitWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		default:
			return nil
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func spanCount(spans []*tracepb.ResourceSpans) int {
	total := 0
	for _, rs := range spans {
		if rs == nil {
			continue
		}
		for _, ss := range rs.ScopeSpans {
			if ss == nil {
				continue
			}
			total += len(ss.Spans)
		}
	}
	return total
}

// TestingClassifyExportError 暴露导出错误分类逻辑，便于外部测试验证重试策略。
func TestingClassifyExportError(err error) (bool, codes.Code, time.Duration) {
	return classifyExportError(err)
}

// TestingSpanCount 暴露 span 计数逻辑，确保日志与实际导出数据一致。
func TestingSpanCount(spans []*tracepb.ResourceSpans) int {
	return spanCount(spans)
}
