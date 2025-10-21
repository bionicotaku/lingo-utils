package observability

import "time"

// Common exporter identifiers.
const (
	ExporterOTLPgRPC = "otlp_grpc"
	ExporterStdout   = "stdout"
)

// ObservabilityConfig aggregates tracing and metrics configuration.
type ObservabilityConfig struct {
	Tracing          *TracingConfig
	Metrics          *MetricsConfig
	GlobalAttributes map[string]string
}

// TracingConfig controls tracer provider initialization.
type TracingConfig struct {
	Enabled            bool
	Exporter           string
	Endpoint           string
	Headers            map[string]string
	Insecure           bool
	SamplingRatio      float64
	ServiceName        string
	ServiceVersion     string
	Environment        string
	Attributes         map[string]string
	BatchTimeout       time.Duration
	ExportTimeout      time.Duration
	MaxQueueSize       int
	MaxExportBatchSize int
	Required           bool
}

// MetricsConfig controls meter provider initialization.
type MetricsConfig struct {
	Enabled             bool
	Exporter            string
	Endpoint            string
	Headers             map[string]string
	Insecure            bool
	Interval            time.Duration
	ResourceAttributes  map[string]string
	DisableRuntimeStats bool
	Required            bool
}

// sanitize prepares configuration with sensible defaults without mutating original instance.
func (c ObservabilityConfig) sanitize() ObservabilityConfig {
	cfg := c
	if cfg.Tracing == nil {
		cfg.Tracing = &TracingConfig{}
	} else {
		tr := *cfg.Tracing
		if tr.Exporter == "" {
			tr.Exporter = ExporterOTLPgRPC
		}
		if tr.SamplingRatio <= 0 {
			tr.SamplingRatio = 1.0
		} else if tr.SamplingRatio > 1 {
			tr.SamplingRatio = 1.0
		}
		if tr.BatchTimeout <= 0 {
			tr.BatchTimeout = 5 * time.Second
		}
		if tr.ExportTimeout <= 0 {
			tr.ExportTimeout = 10 * time.Second
		}
		if tr.MaxQueueSize <= 0 {
			tr.MaxQueueSize = 2048
		}
		if tr.MaxExportBatchSize <= 0 || tr.MaxExportBatchSize > tr.MaxQueueSize {
			tr.MaxExportBatchSize = 512
		}
		cfg.Tracing = &tr
	}

	if cfg.Metrics == nil {
		cfg.Metrics = &MetricsConfig{}
	} else {
		mt := *cfg.Metrics
		if mt.Exporter == "" {
			mt.Exporter = ExporterOTLPgRPC
		}
		if mt.Interval <= 0 {
			mt.Interval = 60 * time.Second
		}
		cfg.Metrics = &mt
	}

	return cfg
}
