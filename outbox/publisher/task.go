package publisher

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"time"

    "github.com/bionicotaku/lingo-utils/gcpubsub"
    "github.com/bionicotaku/lingo-utils/outbox/store"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
)

const (
	defaultBatchSize      = 100
	defaultTickInterval   = time.Second
	defaultInitialBackoff = 2 * time.Second
	defaultMaxBackoff     = 120 * time.Second
	defaultMaxAttempts    = 20
	defaultPublishTimeout = 10 * time.Second
	defaultWorkers        = 4
	defaultLockTTL        = 2 * time.Minute
)

// Config 定义 Outbox 发布任务运行参数。
type Config struct {
	BatchSize      int
	TickInterval   time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	MaxAttempts    int
	PublishTimeout time.Duration
	Workers        int
	LockTTL        time.Duration
	LoggingEnabled *bool
	MetricsEnabled *bool
}

// Publisher 直接复用 gcpubsub.Publisher（面向 Pub/Sub 发布）。
type Publisher = gcpubsub.Publisher

// Task 负责扫描 Outbox 并将事件发布出去。
type Task struct {
    repo           *store.Repository
	publisher      Publisher
	cfg            Config
	clock          func() time.Time
	log            *log.Helper
	lockToken      string
	metrics        *publisherMetrics
	loggingEnabled bool
	metricsEnabled bool
}

// NewTask 构造 Outbox 发布任务。
func NewTask(repo *store.Repository, pub Publisher, cfg Config, logger log.Logger, meter metric.Meter) *Task {
	sanitized := sanitizeConfig(cfg)

	logEnabled := true
	if sanitized.LoggingEnabled != nil {
		logEnabled = *sanitized.LoggingEnabled
	}
	metricsEnabled := true
	if sanitized.MetricsEnabled != nil {
		metricsEnabled = *sanitized.MetricsEnabled
	}

	var helper *log.Helper
	if logEnabled {
		helper = log.NewHelper(logger)
	} else {
		helper = log.NewHelper(log.NewStdLogger(io.Discard))
	}

	var metr *publisherMetrics
	if metricsEnabled {
		metr = newPublisherMetrics(meter, helper)
	}

	return &Task{
		repo:           repo,
		publisher:      pub,
		cfg:            sanitized,
		clock:          time.Now,
		log:            helper,
		lockToken:      uuid.NewString(),
		metrics:        metr,
		loggingEnabled: logEnabled,
		metricsEnabled: metricsEnabled,
	}
}

// WithClock 允许测试注入自定义时钟。
func (t *Task) WithClock(clock func() time.Time) {
	if clock != nil {
		t.clock = clock
	}
}

// Run 持续发布 Outbox 事件，直到 ctx 取消。
func (t *Task) Run(ctx context.Context) error {
	ticker := time.NewTicker(t.cfg.TickInterval)
	defer ticker.Stop()
	if t.metricsEnabled && t.metrics != nil {
		defer t.metrics.shutdown()
	}

	for {
		if err := t.drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.log.WithContext(ctx).Errorf("outbox publish: %v", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (t *Task) drain(ctx context.Context) error {
	now := t.clock()
	staleBefore := now.Add(-t.cfg.LockTTL)
	if staleBefore.After(now) {
		staleBefore = now
	}

	events, err := t.repo.ClaimPending(ctx, now, staleBefore, t.cfg.BatchSize, t.lockToken)
	if err != nil {
		return err
	}

	backlogBefore, backlogErr := t.refreshBacklog(ctx)
	if backlogErr != nil {
		t.log.WithContext(ctx).Warnw("msg", "outbox backlog count failed", "error", backlogErr)
	}

	if len(events) == 0 {
		if backlogErr == nil {
			t.log.WithContext(ctx).Debugw("msg", "outbox idle", "backlog", backlogBefore)
		}
		return nil
	}

	batchFields := []any{"msg", "outbox batch", "count", len(events)}
	if backlogErr == nil {
		batchFields = append(batchFields, "backlog_before", backlogBefore)
	}
	t.log.WithContext(ctx).Infow(batchFields...)

	var successCount, failureCount int32
	workers := t.cfg.Workers
	if workers <= 1 {
		for _, event := range events {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := t.publishOnce(ctx, event); err != nil {
				atomic.AddInt32(&failureCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}
	} else {
		sem := make(chan struct{}, workers)
		grp, grpCtx := errgroup.WithContext(ctx)
		for _, evt := range events {
			event := evt
			sem <- struct{}{}
			grp.Go(func() error {
				defer func() { <-sem }()
				if err := t.publishOnce(grpCtx, event); err != nil {
					atomic.AddInt32(&failureCount, 1)
				} else {
					atomic.AddInt32(&successCount, 1)
				}
				return nil
			})
		}
		if err := grp.Wait(); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}

	backlogAfter, backlogAfterErr := t.refreshBacklog(ctx)
	if backlogAfterErr != nil {
		t.log.WithContext(ctx).Warnw("msg", "outbox backlog recount failed", "error", backlogAfterErr)
	}

	elapsed := t.clock().Sub(now)
	fields := []any{
		"msg", "outbox batch finished",
		"claimed", len(events),
		"success", atomic.LoadInt32(&successCount),
		"failure", atomic.LoadInt32(&failureCount),
		"elapsed_ms", elapsed.Milliseconds(),
	}
	if backlogErr == nil {
		fields = append(fields, "backlog_before", backlogBefore)
	}
	if backlogAfterErr == nil {
		fields = append(fields, "backlog_after", backlogAfter)
	}
	t.log.WithContext(ctx).Infow(fields...)

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func (t *Task) refreshBacklog(ctx context.Context) (int64, error) {
	if t.repo == nil {
		return 0, nil
	}
	count, err := t.repo.CountPending(ctx)
	if err != nil {
		return 0, err
	}
	if t.metricsEnabled && t.metrics != nil {
		t.metrics.setBacklog(count)
	}
	return count, nil
}

func (t *Task) publishOnce(ctx context.Context, event store.Event) error {
	if event.LockToken == nil || *event.LockToken != t.lockToken {
		t.log.WithContext(ctx).Warnw("msg", "outbox lock token mismatch", "event_id", event.EventID, "expected", t.lockToken, "actual", event.LockToken)
		return nil
	}

	publishCtx := ctx
	var cancel context.CancelFunc
	if t.cfg.PublishTimeout > 0 {
		publishCtx, cancel = context.WithTimeout(ctx, t.cfg.PublishTimeout)
		defer cancel()
	}

	attributes := event.Headers
	if attributes == nil {
		attributes = map[string]string{}
	}

	msg := gcpubsub.Message{
		Data:            event.Payload,
		Attributes:      attributes,
		OrderingKey:     event.AggregateID.String(),
		EventID:         event.EventID.String(),
		PublishTime:     event.OccurredAt,
		DeliveryAttempt: int(event.DeliveryAttempts) + 1,
	}

	start := t.clock()
	_, err := t.publisher.Publish(publishCtx, msg)
	latency := t.clock().Sub(start)

	if err != nil {
		if t.metricsEnabled && t.metrics != nil {
			t.metrics.recordFailure(ctx, event, latency)
		}
		lag := t.clock().Sub(event.OccurredAt)
		t.log.WithContext(ctx).Warnw(
			"msg", "outbox publish failed",
			"event_id", event.EventID,
			"aggregate_id", event.AggregateID,
			"attempt", event.DeliveryAttempts+1,
			"latency_ms", latency.Milliseconds(),
			"lag_ms", lag.Milliseconds(),
			"error", err,
		)
		return t.handleFailure(ctx, event, err)
	}

	publishedAt := t.clock()
	if err := t.repo.MarkPublished(ctx, nil, event.EventID, t.lockToken, publishedAt); err != nil {
		if t.metricsEnabled && t.metrics != nil {
			t.metrics.recordFailure(ctx, event, latency)
		}
		t.log.WithContext(ctx).Errorw(
			"msg", "outbox mark published failed",
			"event_id", event.EventID,
			"aggregate_id", event.AggregateID,
			"attempt", event.DeliveryAttempts+1,
			"error", err,
		)
		return err
	}
	if t.metricsEnabled && t.metrics != nil {
		t.metrics.recordSuccess(ctx, event, latency)
	}
	lag := publishedAt.Sub(event.OccurredAt)
	t.log.WithContext(ctx).Infow(
		"msg", "outbox publish success",
		"event_id", event.EventID,
		"aggregate_id", event.AggregateID,
		"attempt", event.DeliveryAttempts+1,
		"latency_ms", latency.Milliseconds(),
		"lag_ms", lag.Milliseconds(),
		"published_at", publishedAt.UTC(),
	)
	return nil
}

func (t *Task) handleFailure(ctx context.Context, event store.Event, publishErr error) error {
	now := t.clock()
	next := now.Add(t.backoffDuration(int(event.DeliveryAttempts)))
	lastErr := publishErr.Error()
	if err := t.repo.Reschedule(ctx, nil, event.EventID, t.lockToken, next, lastErr); err != nil {
		return err
	}

	if t.cfg.MaxAttempts > 0 && int(event.DeliveryAttempts)+1 >= t.cfg.MaxAttempts {
		t.log.WithContext(ctx).Warnw("msg", "outbox retries exhausted", "event_id", event.EventID, "aggregate_id", event.AggregateID, "attempts", event.DeliveryAttempts+1)
	}

	delay := next.Sub(now)
	t.log.WithContext(ctx).Infow(
		"msg", "outbox publish rescheduled",
		"event_id", event.EventID,
		"aggregate_id", event.AggregateID,
		"next_available_at", next.UTC(),
		"retry_in_ms", delay.Milliseconds(),
	)
	return nil
}

func (t *Task) backoffDuration(attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	backoff := t.cfg.InitialBackoff * time.Duration(1<<attempts)
	if backoff <= 0 {
		backoff = t.cfg.InitialBackoff
	}
	if t.cfg.MaxBackoff > 0 && backoff > t.cfg.MaxBackoff {
		backoff = t.cfg.MaxBackoff
	}
	return backoff
}

func sanitizeConfig(cfg Config) Config {
	result := cfg
	if result.BatchSize <= 0 {
		result.BatchSize = defaultBatchSize
	}
	if result.TickInterval <= 0 {
		result.TickInterval = defaultTickInterval
	}
	if result.InitialBackoff <= 0 {
		result.InitialBackoff = defaultInitialBackoff
	}
	if result.MaxBackoff <= 0 {
		result.MaxBackoff = defaultMaxBackoff
	}
	if result.MaxAttempts <= 0 {
		result.MaxAttempts = defaultMaxAttempts
	}
	if result.PublishTimeout <= 0 {
		result.PublishTimeout = defaultPublishTimeout
	}
	if result.Workers <= 0 {
		result.Workers = defaultWorkers
	}
	if result.Workers > result.BatchSize && result.BatchSize > 0 {
		result.Workers = result.BatchSize
	}
	if result.LockTTL <= 0 {
		result.LockTTL = defaultLockTTL
	}
	if cfg.LoggingEnabled == nil {
		result.LoggingEnabled = boolPtr(true)
	}
	if cfg.MetricsEnabled == nil {
		result.MetricsEnabled = boolPtr(true)
	}
	return result
}

func boolPtr(v bool) *bool {
	b := v
	return &b
}

// ---------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------

type publisherMetrics struct {
	success      metric.Int64Counter
	failure      metric.Int64Counter
	latency      metric.Float64Histogram
	backlogGauge metric.Int64ObservableGauge
	registration metric.Registration
	backlog      atomic.Int64
	helper       *log.Helper
	enabled      bool
}

const (
	metricNamePublishSuccess = "outbox_publish_success_total"
	metricNamePublishFailure = "outbox_publish_failure_total"
	metricNamePublishLatency = "outbox_publish_latency_ms"
	metricNameBacklogGauge   = "outbox_backlog"
)

var (
	attrAggregateType = attribute.Key("outbox.aggregate_type")
	attrEventType     = attribute.Key("outbox.event_type")
	attrResult        = attribute.Key("outbox.result")
)

func newPublisherMetrics(meter metric.Meter, helper *log.Helper) *publisherMetrics {
	m := &publisherMetrics{helper: helper}
	if helper == nil {
		return m
	}
	if meter == nil {
		meter = otel.GetMeterProvider().Meter("lingo-utils.outbox")
	}

	var err error
	m.success, err = meter.Int64Counter(metricNamePublishSuccess,
		metric.WithDescription("Number of outbox events published successfully"))
	if err != nil {
		helper.Warnw("msg", "outbox metrics register success counter", "err", err)
	}

	m.failure, err = meter.Int64Counter(metricNamePublishFailure,
		metric.WithDescription("Number of outbox events that failed to publish"))
	if err != nil {
		helper.Warnw("msg", "outbox metrics register failure counter", "err", err)
	}

	m.latency, err = meter.Float64Histogram(metricNamePublishLatency,
		metric.WithDescription("Latency for publishing outbox events"), metric.WithUnit("ms"))
	if err != nil {
		helper.Warnw("msg", "outbox metrics register latency histogram", "err", err)
	}

	m.backlogGauge, err = meter.Int64ObservableGauge(metricNameBacklogGauge,
		metric.WithDescription("Current number of unpublished outbox events"))
	if err != nil {
		helper.Warnw("msg", "outbox metrics register backlog gauge", "err", err)
	} else {
		reg, regErr := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
			observer.ObserveInt64(m.backlogGauge, m.backlog.Load())
			return nil
		}, m.backlogGauge)
		if regErr != nil {
			helper.Warnw("msg", "outbox metrics register backlog callback", "err", regErr)
		} else {
			m.registration = reg
		}
	}

	m.enabled = true
	return m
}

func (m *publisherMetrics) recordSuccess(ctx context.Context, event store.Event, latency time.Duration) {
	if m == nil || !m.enabled {
		return
	}
	attrs := []attribute.KeyValue{
		attrAggregateType.String(event.AggregateType),
		attrEventType.String(event.EventType),
	}
	if m.success != nil {
		m.success.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if m.latency != nil {
		latencyAttrs := append(attrs, attrResult.String("success"))
		m.latency.Record(ctx, float64(latency.Milliseconds()), metric.WithAttributes(latencyAttrs...))
	}
}

func (m *publisherMetrics) recordFailure(ctx context.Context, event store.Event, latency time.Duration) {
	if m == nil || !m.enabled {
		return
	}
	attrs := []attribute.KeyValue{
		attrAggregateType.String(event.AggregateType),
		attrEventType.String(event.EventType),
	}
	if m.failure != nil {
		m.failure.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if m.latency != nil {
		latencyAttrs := append(attrs, attrResult.String("failure"))
		m.latency.Record(ctx, float64(latency.Milliseconds()), metric.WithAttributes(latencyAttrs...))
	}
}

func (m *publisherMetrics) setBacklog(count int64) {
	if m == nil || !m.enabled {
		return
	}
	m.backlog.Store(count)
}

func (m *publisherMetrics) shutdown() {
	if m == nil || !m.enabled {
		return
	}
	if m.registration != nil {
		if err := m.registration.Unregister(); err != nil && m.helper != nil {
			m.helper.Warnw("msg", "outbox metrics unregister backlog gauge", "err", err)
		}
	}
}
