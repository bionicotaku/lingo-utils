package observability

import (
	"context"
	"errors"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/bionicotaku/lingo-utils/observability/metrics"
	"github.com/bionicotaku/lingo-utils/observability/tracing"
)

// Option customizes observability initialization.
type Option func(*initOptions)

type initOptions struct {
	logger         log.Logger
	propagator     propagation.TextMapPropagator
	serviceName    string
	serviceVersion string
	environment    string
	attributes     map[string]string
	resource       *resource.Resource
}

func defaultInitOptions() initOptions {
	return initOptions{
		logger:     nil,
		propagator: propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
		attributes: map[string]string{},
	}
}

// WithLogger overrides the default no-op logger.
func WithLogger(logger log.Logger) Option {
	return func(o *initOptions) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithPropagator overrides the default W3C TraceContext propagator.
func WithPropagator(p propagation.TextMapPropagator) Option {
	return func(o *initOptions) {
		if p != nil {
			o.propagator = p
		}
	}
}

// WithServiceName overrides the service.name attribute.
func WithServiceName(name string) Option {
	return func(o *initOptions) {
		o.serviceName = name
	}
}

// WithServiceVersion overrides the service.version attribute.
func WithServiceVersion(version string) Option {
	return func(o *initOptions) {
		o.serviceVersion = version
	}
}

// WithEnvironment overrides the deployment.environment attribute.
func WithEnvironment(env string) Option {
	return func(o *initOptions) {
		o.environment = env
	}
}

// WithAttributes appends extra resource attributes.
func WithAttributes(attrs map[string]string) Option {
	return func(o *initOptions) {
		for k, v := range attrs {
			if o.attributes == nil {
				o.attributes = make(map[string]string, len(attrs))
			}
			o.attributes[k] = v
		}
	}
}

// Init sets up tracing and metrics providers according to the provided configuration.
func Init(ctx context.Context, cfg ObservabilityConfig, opts ...Option) (func(context.Context) error, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}

	options := defaultInitOptions()
	for _, opt := range opts {
		opt(&options)
	}
	if options.logger == nil {
		return nil, errors.New("observability: logger is required (use observability.WithLogger)")
	}

	res, err := buildResource(ctx, cfg, options)
	if err != nil {
		return nil, err
	}
	options.resource = res

	cfg = cfg.sanitize()

	var shutdowns []func(context.Context) error

	if cfg.Tracing.Enabled {
		traceCfg := tracing.Config{
			Exporter:           cfg.Tracing.Exporter,
			Endpoint:           cfg.Tracing.Endpoint,
			Headers:            cfg.Tracing.Headers,
			Insecure:           cfg.Tracing.Insecure,
			SamplingRatio:      cfg.Tracing.SamplingRatio,
			BatchTimeout:       cfg.Tracing.BatchTimeout,
			ExportTimeout:      cfg.Tracing.ExportTimeout,
			MaxQueueSize:       cfg.Tracing.MaxQueueSize,
			MaxExportBatchSize: cfg.Tracing.MaxExportBatchSize,
			Required:           cfg.Tracing.Required,
		}
		if options.serviceName != "" {
			traceCfg.ServiceName = options.serviceName
		} else {
			traceCfg.ServiceName = cfg.Tracing.ServiceName
		}
		if options.serviceVersion != "" {
			traceCfg.ServiceVersion = options.serviceVersion
		} else {
			traceCfg.ServiceVersion = cfg.Tracing.ServiceVersion
		}
		if options.environment != "" {
			traceCfg.Environment = options.environment
		} else {
			traceCfg.Environment = cfg.Tracing.Environment
		}
		traceCfg.Attributes = mergeAttributes(cfg.Tracing.Attributes, options.attributes)

		shutdown, err := tracing.Init(ctx, traceCfg,
			tracing.WithLogger(options.logger),
			tracing.WithResource(options.resource),
			tracing.WithPropagator(options.propagator),
		)
		if err != nil {
			if traceCfg.Required {
				return nil, err
			}
			log.NewHelper(options.logger).Warnf("tracing disabled due to initialization error: %v", err)
		} else if shutdown != nil {
			shutdowns = append(shutdowns, shutdown)
		}
	}

	if cfg.Metrics.Enabled {
		metricCfg := metrics.Config{
			Exporter:            cfg.Metrics.Exporter,
			Endpoint:            cfg.Metrics.Endpoint,
			Headers:             cfg.Metrics.Headers,
			Insecure:            cfg.Metrics.Insecure,
			Interval:            cfg.Metrics.Interval,
			DisableRuntimeStats: cfg.Metrics.DisableRuntimeStats,
			Required:            cfg.Metrics.Required,
		}
		metricCfg.ResourceAttributes = mergeAttributes(cfg.Metrics.ResourceAttributes, nil)
		metricCfg.ServiceName = chooseNonEmpty(options.serviceName, cfg.Tracing.ServiceName)
		metricCfg.ServiceVersion = chooseNonEmpty(options.serviceVersion, cfg.Tracing.ServiceVersion)
		metricCfg.Environment = chooseNonEmpty(options.environment, cfg.Tracing.Environment)

		shutdown, err := metrics.Init(ctx, metricCfg,
			metrics.WithLogger(options.logger),
			metrics.WithResource(options.resource),
		)
		if err != nil {
			if metricCfg.Required {
				return nil, err
			}
			log.NewHelper(options.logger).Warnf("metrics disabled due to initialization error: %v", err)
		} else if shutdown != nil {
			shutdowns = append(shutdowns, shutdown)
		}
	}

	return func(ctx context.Context) error {
		var result error
		for i := len(shutdowns) - 1; i >= 0; i-- {
			if err := shutdowns[i](ctx); err != nil && result == nil {
				result = err
			}
		}
		return result
	}, nil
}

// BuildResource 根据传入配置与 Option 构建 OpenTelemetry resource，可用于自定义初始化流程。
func BuildResource(ctx context.Context, cfg ObservabilityConfig, opts ...Option) (*resource.Resource, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	options := defaultInitOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return buildResource(ctx, cfg, options)
}

func mergeAttributes(src map[string]string, extra map[string]string) map[string]string {
	if len(src) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]string, len(src)+len(extra))
	for k, v := range src {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func chooseNonEmpty(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}
