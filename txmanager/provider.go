package txmanager

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Component wraps the constructed Manager and aligns with the shared component
// pattern used in other lingo-utils packages (gclog, observability, etc.).
type Component struct {
	Manager Manager
}

// NewComponent builds a transaction manager using the provided configuration,
// pgx pool and structured logger. The cleanup currently no-ops but matches the
// lifecycle expectations of other components so it can be extended later.
func NewComponent(cfg Config, pool *pgxpool.Pool, logger log.Logger) (*Component, func(), error) {
	manager, err := NewManager(pool, cfg, Dependencies{Logger: logger})
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
