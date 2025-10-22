package observability

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
)

const defaultShutdownTimeout = 5 * time.Second

// ServiceInfo carries metadata used to annotate telemetry resources.
type ServiceInfo struct {
	Name        string
	Version     string
	Environment string
}

// Component wraps the initialized telemetry providers and exposes shutdown hooks.
type Component struct {
	shutdown func(context.Context) error
	logger   log.Logger
}

// NewComponent installs tracing and metrics providers according to cfg. The
// returned cleanup function applies a bounded timeout to flush buffered data.
func NewComponent(ctx context.Context, cfg ObservabilityConfig, info ServiceInfo, logger log.Logger) (*Component, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}

	opts := []Option{WithLogger(logger)}
	if info.Name != "" {
		opts = append(opts, WithServiceName(info.Name))
	}
	if info.Version != "" {
		opts = append(opts, WithServiceVersion(info.Version))
	}
	if info.Environment != "" {
		opts = append(opts, WithEnvironment(info.Environment))
	}

	shutdown, err := Init(ctx, cfg, opts...)
	if err != nil {
		return nil, nil, err
	}

	comp := &Component{shutdown: shutdown, logger: logger}
	cleanup := func() {
		if comp == nil || comp.shutdown == nil {
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancel()
		if err := comp.shutdown(shutdownCtx); err != nil {
			log.NewHelper(comp.logger).Warnf("shutdown observability: %v", err)
		}
	}

	return comp, cleanup, nil
}

// Shutdown flushes telemetry using the supplied context.
func (c *Component) Shutdown(ctx context.Context) error {
	if c == nil || c.shutdown == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return c.shutdown(ctx)
}

// ProvideMetricsConfig exposes a pointer to MetricsConfig with defaults applied
// so downstream components can inject gRPC instrumentation settings.
func ProvideMetricsConfig(cfg ObservabilityConfig) *MetricsConfig {
	if cfg.Metrics == nil {
		return &MetricsConfig{GRPCEnabled: true}
	}
	return cfg.Metrics
}

// ProviderSet wires observability component and helpers for use with Wire.
var ProviderSet = wire.NewSet(NewComponent, ProvideMetricsConfig)
