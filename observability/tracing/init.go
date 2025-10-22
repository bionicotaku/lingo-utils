package tracing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config holds tracing specific settings.
type Config struct {
	Exporter           string
	Endpoint           string
	Headers            map[string]string
	Insecure           bool
	SamplingRatio      float64
	BatchTimeout       time.Duration
	ExportTimeout      time.Duration
	MaxQueueSize       int
	MaxExportBatchSize int
	ServiceName        string
	ServiceVersion     string
	Environment        string
	Attributes         map[string]string
	Required           bool
}

// Init creates a tracer provider and installs it as global provider.
func Init(ctx context.Context, cfg Config, opts ...Option) (func(context.Context) error, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	options := defaultOptions()
	for _, opt := range opts {
		opt(&options)
	}
	if options.logger == nil {
		return nil, errors.New("tracing: logger is required (use tracing.WithLogger)")
	}

	if options.resource == nil {
		options.resource = resource.Default()
	}
	if options.propagator == nil {
		options.propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	}

	cfg = sanitizeConfig(cfg)

	telemetryLogger := newExporterLogger(options.logger)
	exporter, err := newExporter(ctx, cfg, telemetryLogger)
	if err != nil {
		return nil, err
	}

	helper := log.NewHelper(options.logger)
	otel.SetErrorHandler(newErrorHandler(telemetryLogger))

	batcher := sdktrace.WithBatcher(exporter,
		sdktrace.WithMaxQueueSize(cfg.MaxQueueSize),
		sdktrace.WithMaxExportBatchSize(cfg.MaxExportBatchSize),
		sdktrace.WithBatchTimeout(cfg.BatchTimeout),
		sdktrace.WithExportTimeout(cfg.ExportTimeout),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplingRatio))),
		sdktrace.WithResource(options.resource),
		batcher,
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(options.propagator)

	helper.Infof("tracing initialized exporter=%s endpoint=%s", cfg.Exporter, cfg.Endpoint)
	return func(ctx context.Context) error {
		helper.Info("shutting down tracing provider")
		return tp.Shutdown(ctx)
	}, nil
}

func sanitizeConfig(cfg Config) Config {
	if cfg.Exporter == "" {
		cfg.Exporter = "otlp_grpc"
	}
	if cfg.SamplingRatio <= 0 || cfg.SamplingRatio > 1 {
		cfg.SamplingRatio = 1.0
	}
	if cfg.BatchTimeout <= 0 {
		cfg.BatchTimeout = 5 * time.Second
	}
	if cfg.ExportTimeout <= 0 {
		cfg.ExportTimeout = 10 * time.Second
	}
	if cfg.MaxQueueSize <= 0 {
		cfg.MaxQueueSize = 2048
	}
	if cfg.MaxExportBatchSize <= 0 || cfg.MaxExportBatchSize > cfg.MaxQueueSize {
		cfg.MaxExportBatchSize = minInt(512, cfg.MaxQueueSize)
	}
	return cfg
}

func newExporter(ctx context.Context, cfg Config, logger *exporterLogger) (sdktrace.SpanExporter, error) {
	switch cfg.Exporter {
	case "otlp_grpc":
		var clientOpts []otlptracegrpc.Option
		if cfg.Endpoint != "" {
			clientOpts = append(clientOpts, otlptracegrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			clientOpts = append(clientOpts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		if cfg.ExportTimeout > 0 {
			clientOpts = append(clientOpts, otlptracegrpc.WithTimeout(cfg.ExportTimeout))
		}
		return newRetryingExporter(ctx, defaultRetrySettings(), logger, clientOpts...)
	case "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	default:
		return nil, fmt.Errorf("unsupported tracing exporter %q", cfg.Exporter)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
