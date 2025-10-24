package txmanager

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type telemetry struct {
	enabled bool

	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
	retries  metric.Int64Counter
	failures metric.Int64Counter

	logger *log.Helper
}

func newTelemetry(meter metric.Meter, logger *log.Helper, enabled bool) *telemetry {
	t := &telemetry{enabled: enabled, logger: logger}
	if !enabled {
		return t
	}

	var err error
	t.duration, err = meter.Float64Histogram("db.tx.duration", metric.WithUnit("ms"))
	if err != nil {
		logger.Warnf("txmanager: create histogram: %v", err)
	}
	t.active, err = meter.Int64UpDownCounter("db.tx.active")
	if err != nil {
		logger.Warnf("txmanager: create active counter: %v", err)
	}
	t.retries, err = meter.Int64Counter("db.tx.retries")
	if err != nil {
		logger.Warnf("txmanager: create retries counter: %v", err)
	}
	t.failures, err = meter.Int64Counter("db.tx.failures")
	if err != nil {
		logger.Warnf("txmanager: create failures counter: %v", err)
	}
	return t
}

func (t *telemetry) recordStart(ctx context.Context, method string, isolation string) {
	if !t.enabled || t.active == nil {
		return
	}
	t.active.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("tx.method", method),
			attribute.String("tx.isolation", isolation),
		),
	)
}

func (t *telemetry) recordEnd(ctx context.Context, method string, isolation string, retryable bool, err error, elapsed time.Duration) {
	if !t.enabled {
		return
	}
	opts := metric.WithAttributes(
		attribute.String("tx.method", method),
		attribute.String("tx.isolation", isolation),
		attribute.Bool("tx.retryable", retryable),
	)
	if t.active != nil {
		t.active.Add(ctx, -1,
			metric.WithAttributes(
				attribute.String("tx.method", method),
				attribute.String("tx.isolation", isolation),
			),
		)
	}
	if t.duration != nil {
		t.duration.Record(ctx, float64(elapsed.Milliseconds()), opts)
	}
	if err != nil && t.failures != nil {
		t.failures.Add(ctx, 1, opts)
	}
	if retryable && t.retries != nil {
		t.retries.Add(ctx, 1, opts)
	}
}
