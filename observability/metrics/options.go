// Package metrics 封装 OpenTelemetry MeterProvider 的初始化逻辑。
package metrics

import (
	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Option configures metrics initialization.
type Option func(*options)

type options struct {
	logger   log.Logger
	resource *resource.Resource
}

func defaultOptions() options {
	return options{
		logger: nil,
	}
}

// WithLogger overrides the default logger.
func WithLogger(logger log.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithResource sets the resource used by the meter provider.
func WithResource(res *resource.Resource) Option {
	return func(o *options) {
		if res != nil {
			o.resource = res
		}
	}
}
