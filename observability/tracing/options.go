// Package tracing 封装 OpenTelemetry TracerProvider 的初始化逻辑。
package tracing

import (
	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Option configures tracing initialization.
type Option func(*options)

type options struct {
	logger     log.Logger
	resource   *resource.Resource
	propagator propagation.TextMapPropagator
}

func defaultOptions() options {
	return options{
		logger:   nil,
		resource: nil,
	}
}

// WithLogger sets the logger used for diagnostic output.
func WithLogger(logger log.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithResource associates a pre-built OpenTelemetry resource with the provider.
func WithResource(res *resource.Resource) Option {
	return func(o *options) {
		if res != nil {
			o.resource = res
		}
	}
}

// WithPropagator overrides the default context propagator.
func WithPropagator(p propagation.TextMapPropagator) Option {
	return func(o *options) {
		if p != nil {
			o.propagator = p
		}
	}
}
