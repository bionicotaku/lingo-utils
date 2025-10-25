package pgxpoolx

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProvideComponent builds a Component using the shared logger and default
// dependency set. Additional dependencies can be injected by supplying a
// custom Dependencies value via Wire bindings if needed.
func ProvideComponent(ctx context.Context, cfg Config, logger log.Logger) (*Component, func(), error) {
	deps := Dependencies{Logger: logger}
	return NewComponent(ctx, cfg, deps)
}

// ProvidePool exposes the constructed pgxpool.Pool for downstream injection.
func ProvidePool(component *Component) *pgxpool.Pool {
	if component == nil {
		return nil
	}
	return component.Pool
}

// ProviderSet wires the pgxpoolx component and pool output for dependency
// injection via Google Wire.
var ProviderSet = wire.NewSet(ProvideComponent, ProvidePool)
