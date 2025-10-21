package tracing

import (
	"context"
	"strings"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/selector"
	krtracing "github.com/go-kratos/kratos/v2/middleware/tracing"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// ServerOption configures the server middleware.
type ServerOption func(*serverOptions)

type serverOptions struct {
	propagator     propagation.TextMapPropagator
	tracerProvider trace.TracerProvider
	tracerName     string
	skipper        func(string) bool
}

func defaultServerOptions() serverOptions {
	return serverOptions{
		skipper: func(operation string) bool {
			normalized := strings.ToLower(operation)
			return normalized == "/_health" ||
				strings.HasPrefix(normalized, "/_health/") ||
				normalized == "/healthz" ||
				normalized == "/readyz"
		},
	}
}

// WithServerPropagator overrides the propagator used by the middleware.
func WithServerPropagator(p propagation.TextMapPropagator) ServerOption {
	return func(o *serverOptions) {
		o.propagator = p
	}
}

// WithServerTracerProvider overrides the tracer provider.
func WithServerTracerProvider(tp trace.TracerProvider) ServerOption {
	return func(o *serverOptions) {
		o.tracerProvider = tp
	}
}

// WithServerTracerName sets a custom tracer name.
func WithServerTracerName(name string) ServerOption {
	return func(o *serverOptions) {
		o.tracerName = name
	}
}

// WithServerSkipper overrides the default skipper.
func WithServerSkipper(skip func(operation string) bool) ServerOption {
	return func(o *serverOptions) {
		if skip != nil {
			o.skipper = skip
		}
	}
}

// Server returns a Kratos middleware that instruments inbound requests.
func Server(opts ...ServerOption) middleware.Middleware {
	cfg := defaultServerOptions()
	for _, opt := range opts {
		opt(&cfg)
	}

	var tracingOpts []krtracing.Option
	if cfg.propagator != nil {
		tracingOpts = append(tracingOpts, krtracing.WithPropagator(cfg.propagator))
	}
	if cfg.tracerProvider != nil {
		tracingOpts = append(tracingOpts, krtracing.WithTracerProvider(cfg.tracerProvider))
	}
	if cfg.tracerName != "" {
		tracingOpts = append(tracingOpts, krtracing.WithTracerName(cfg.tracerName))
	}

	base := krtracing.Server(tracingOpts...)
	if cfg.skipper == nil {
		return base
	}

	builder := selector.Server(base).Match(func(ctx context.Context, operation string) bool {
		return !cfg.skipper(operation)
	})
	return builder.Build()
}

// ClientOption configures the client middleware.
type ClientOption func(*clientOptions)

type clientOptions struct {
	propagator     propagation.TextMapPropagator
	tracerProvider trace.TracerProvider
	tracerName     string
	skipper        func(string) bool
}

func defaultClientOptions() clientOptions {
	return clientOptions{
		skipper: func(string) bool { return false },
	}
}

// WithClientPropagator overrides the propagator for client middleware.
func WithClientPropagator(p propagation.TextMapPropagator) ClientOption {
	return func(o *clientOptions) {
		o.propagator = p
	}
}

// WithClientTracerProvider overrides the tracer provider for client middleware.
func WithClientTracerProvider(tp trace.TracerProvider) ClientOption {
	return func(o *clientOptions) {
		o.tracerProvider = tp
	}
}

// WithClientTracerName sets a tracer name for the client middleware.
func WithClientTracerName(name string) ClientOption {
	return func(o *clientOptions) {
		o.tracerName = name
	}
}

// WithClientSkipper configures a skipper for client middleware.
func WithClientSkipper(skip func(string) bool) ClientOption {
	return func(o *clientOptions) {
		if skip != nil {
			o.skipper = skip
		}
	}
}

// Client returns a Kratos middleware that instruments outbound requests.
func Client(opts ...ClientOption) middleware.Middleware {
	cfg := defaultClientOptions()
	for _, opt := range opts {
		opt(&cfg)
	}

	var tracingOpts []krtracing.Option
	if cfg.propagator != nil {
		tracingOpts = append(tracingOpts, krtracing.WithPropagator(cfg.propagator))
	}
	if cfg.tracerProvider != nil {
		tracingOpts = append(tracingOpts, krtracing.WithTracerProvider(cfg.tracerProvider))
	}
	if cfg.tracerName != "" {
		tracingOpts = append(tracingOpts, krtracing.WithTracerName(cfg.tracerName))
	}

	base := krtracing.Client(tracingOpts...)
	if cfg.skipper == nil {
		return base
	}

	builder := selector.Client(base).Match(func(ctx context.Context, operation string) bool {
		return !cfg.skipper(operation)
	})
	return builder.Build()
}
