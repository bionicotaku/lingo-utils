package txmanager

import (
	"io"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Option func(*managerOptions)

type managerOptions struct {
	logger                 log.Logger
	meter                  metric.Meter
	tracer                 trace.Tracer
	clock                  func() time.Time
	metricsEnabledOverride *bool
}

func defaultManagerOptions() managerOptions {
	return managerOptions{
		logger: log.NewStdLogger(io.Discard),
		meter:  nil,
		tracer: nil,
		clock:  time.Now,
	}
}

// WithLogger injects a structured logger used to report transaction lifecycle events.
func WithLogger(logger log.Logger) Option {
	return func(opts *managerOptions) {
		if logger != nil {
			opts.logger = logger
		}
	}
}

// WithMeter injects a custom OpenTelemetry meter used for metrics emission.
func WithMeter(meter metric.Meter) Option {
	return func(opts *managerOptions) {
		if meter != nil {
			opts.meter = meter
		}
	}
}

// WithTracer injects the tracer used to create child spans for transactional work.
func WithTracer(tracer trace.Tracer) Option {
	return func(opts *managerOptions) {
		if tracer != nil {
			opts.tracer = tracer
		}
	}
}

// WithClock allows overriding the time source (useful for deterministic tests).
func WithClock(now func() time.Time) Option {
	return func(opts *managerOptions) {
		if now != nil {
			opts.clock = now
		}
	}
}

// WithMetricsEnabled overrides the metrics switch regardless of configuration defaults.
func WithMetricsEnabled(enabled bool) Option {
	return func(opts *managerOptions) {
		opts.metricsEnabledOverride = new(bool)
		*opts.metricsEnabledOverride = enabled
	}
}
