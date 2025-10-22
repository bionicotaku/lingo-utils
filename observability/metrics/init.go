package metrics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
)

// Config holds metrics specific settings.
type Config struct {
	Exporter            string
	Endpoint            string
	Headers             map[string]string
	Insecure            bool
	Interval            time.Duration
	DisableRuntimeStats bool
	ResourceAttributes  map[string]string
	ServiceName         string
	ServiceVersion      string
	Environment         string
	Required            bool
}

// Init creates a meter provider and installs it globally.
func Init(ctx context.Context, cfg Config, opts ...Option) (func(context.Context) error, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	options := defaultOptions()
	for _, opt := range opts {
		opt(&options)
	}
	if options.logger == nil {
		return nil, errors.New("metrics: logger is required (use metrics.WithLogger)")
	}
	if options.resource == nil {
		options.resource = resource.Default()
	}

	cfg = sanitizeConfig(cfg)

	exp, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	helper := log.NewHelper(options.logger)

	reader := metric.NewPeriodicReader(exp, metric.WithInterval(cfg.Interval))
	mp := metric.NewMeterProvider(
		metric.WithReader(reader),
		metric.WithResource(options.resource),
	)

	otel.SetMeterProvider(mp)

	if !cfg.DisableRuntimeStats {
		if err := runtime.Start(
			runtime.WithMeterProvider(mp),
			runtime.WithMinimumReadMemStatsInterval(cfg.Interval),
		); err != nil {
			helper.Warnf("failed to start runtime metrics instrumentation: %v", err)
		}
	}

	helper.Infof("metrics initialized exporter=%s endpoint=%s interval=%s", cfg.Exporter, cfg.Endpoint, cfg.Interval)
	return func(ctx context.Context) error {
		helper.Info("shutting down metrics provider")
		return mp.Shutdown(ctx)
	}, nil
}

func sanitizeConfig(cfg Config) Config {
	if cfg.Exporter == "" {
		cfg.Exporter = "otlp_grpc"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	return cfg
}

func newExporter(ctx context.Context, cfg Config) (metric.Exporter, error) {
	switch cfg.Exporter {
	case "otlp_grpc":
		var opts []otlpmetricgrpc.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlpmetricgrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(cfg.Headers))
		}
		return otlpmetricgrpc.New(ctx, opts...)
	case "stdout":
		return stdoutmetric.New(stdoutmetric.WithPrettyPrint())
	default:
		return nil, fmt.Errorf("unsupported metrics exporter %q", cfg.Exporter)
	}
}
