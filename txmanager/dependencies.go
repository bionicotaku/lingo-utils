package txmanager

import (
	"io"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Dependencies collects optional collaborators used by the manager. Callers can
// supply custom telemetry or time sources for testing; zero values trigger
// sensible defaults.
type Dependencies struct {
	Logger                 log.Logger
	Meter                  metric.Meter
	Tracer                 trace.Tracer
	Clock                  func() time.Time
	MetricsEnabledOverride *bool
}

type managerDeps struct {
	logger                 log.Logger
	meter                  metric.Meter
	tracer                 trace.Tracer
	clock                  func() time.Time
	metricsEnabledOverride *bool
}

func sanitizeDependencies(cfg Config, deps Dependencies) managerDeps {
	// Default logger discards output to avoid nil checks downstream.
	logger := deps.Logger
	if logger == nil {
		logger = log.NewStdLogger(io.Discard)
	}

	meter := deps.Meter
	if meter == nil {
		meter = otel.GetMeterProvider().Meter(cfg.MeterName)
	}

	tracer := deps.Tracer
	if tracer == nil {
		tracer = otel.Tracer(cfg.MeterName)
	}

	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}

	return managerDeps{
		logger:                 logger,
		meter:                  meter,
		tracer:                 tracer,
		clock:                  clock,
		metricsEnabledOverride: deps.MetricsEnabledOverride,
	}
}
