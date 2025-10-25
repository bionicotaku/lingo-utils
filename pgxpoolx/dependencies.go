package pgxpoolx

import (
	"io"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Dependencies lists collaborators required during component construction.
// Callers may omit optional fields to fall back onto sensible defaults.
type Dependencies struct {
	Logger log.Logger
	Meter  metric.Meter
	Tracer pgx.QueryTracer
	Clock  func() time.Time
}

type componentDeps struct {
	logger log.Logger
	meter  metric.Meter
	tracer pgx.QueryTracer
	clock  func() time.Time
}

func sanitizeDependencies(deps Dependencies) componentDeps {
	logger := deps.Logger
	if logger == nil {
		logger = log.NewStdLogger(io.Discard)
	}

	meter := deps.Meter
	if meter == nil {
		meter = otel.GetMeterProvider().Meter("lingo-utils/pgxpoolx")
	}

	tracer := deps.Tracer
	if tracer == nil {
		tracer = newPGXLogger(log.NewHelper(logger))
	}

	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}

	return componentDeps{
		logger: logger,
		meter:  meter,
		tracer: tracer,
		clock:  clock,
	}
}
