package gclog

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"go.opentelemetry.io/otel/trace"
)

// Config captures runtime metadata used to annotate structured logs.
type Config struct {
	Service              string
	Version              string
	Environment          string
	InstanceID           string
	StaticLabels         map[string]string
	EnableSourceLocation bool
}

// Component bundles the Kratos-compatible logger.
type Component struct {
	Logger log.Logger
}

// NewComponent builds a structured logger enriched with trace/span context.
func NewComponent(cfg Config) (*Component, func(), error) {
	opts := []Option{
		WithService(cfg.Service),
		WithVersion(cfg.Version),
		WithEnvironment(cfg.Environment),
	}
	labels := map[string]string{}
	for k, v := range cfg.StaticLabels {
		labels[k] = v
	}
	if cfg.InstanceID != "" {
		labels["service.id"] = cfg.InstanceID
	}
	if len(labels) > 0 {
		opts = append(opts, WithStaticLabels(labels))
	}
	if cfg.EnableSourceLocation {
		opts = append(opts, EnableSourceLocation())
	}

	baseLogger, err := NewLogger(opts...)
	if err != nil {
		return nil, nil, err
	}

	logger := log.With(
		baseLogger,
		"trace_id", log.Valuer(func(ctx context.Context) any {
			sc := trace.SpanContextFromContext(ctx)
			if sc.HasTraceID() {
				return sc.TraceID().String()
			}
			return ""
		}),
		"span_id", log.Valuer(func(ctx context.Context) any {
			sc := trace.SpanContextFromContext(ctx)
			if sc.HasSpanID() {
				return sc.SpanID().String()
			}
			return ""
		}),
	)

	comp := &Component{Logger: logger}
	cleanup := func() {}

	return comp, cleanup, nil
}

// ProvideLogger exposes the structured logger for injection.
func ProvideLogger(comp *Component) log.Logger {
	return comp.Logger
}

// ProvideHelper exposes a log.Helper built atop the structured logger.
func ProvideHelper(comp *Component) *log.Helper {
	return log.NewHelper(comp.Logger)
}

// ProviderSet wires the logging component for Wire-based injection.
var ProviderSet = wire.NewSet(NewComponent, ProvideLogger, ProvideHelper)
