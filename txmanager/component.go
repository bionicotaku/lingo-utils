package txmanager

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
)

// Component wraps the constructed manager and exposes cleanup hooks for Wire.
type Component struct {
	Manager Manager
}

// NewComponent builds a transactional manager component that can be injected
// through Google Wire. The returned cleanup currently no-ops but is reserved for
// parity with other lingo-utils components.
func NewComponent(cfg Config, pool *pgxpool.Pool, logger log.Logger, opts ...Option) (*Component, func(), error) {
	sanitized := cfg.sanitized()

	// Enrich options with global meter/tracer defaults when callers did not
	// specify them explicitly.
	defaultOpts := []Option{
		WithMeter(otel.GetMeterProvider().Meter(sanitized.MeterName)),
		WithTracer(otel.Tracer(sanitized.MeterName)),
		WithLogger(logger),
	}
	options := append(defaultOpts, opts...)

	manager, err := NewManager(pool, cfg, options...)
	if err != nil {
		return nil, nil, err
	}

	comp := &Component{Manager: manager}
	cleanup := func() {}
	return comp, cleanup, nil
}

// ProvideManager exposes the Manager interface for Wire injection.
func ProvideManager(comp *Component) Manager {
	return comp.Manager
}

// ProviderSet collects constructors for Wire integration.
var ProviderSet = wire.NewSet(NewComponent, ProvideManager)
