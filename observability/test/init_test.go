package observability_test

import (
	"context"
	"testing"
	"time"

	obs "github.com/bionicotaku/lingo-utils/observability"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

type stubHandler struct{}

func (stubHandler) Handle(error) {}

func TestInitWithStdoutExporters(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:       true,
			Exporter:      obs.ExporterStdout,
			SamplingRatio: 2, // should be clamped to 1.0
		},
		Metrics: &obs.MetricsConfig{
			Enabled:             true,
			Exporter:            obs.ExporterStdout,
			Interval:            50 * time.Millisecond,
			DisableRuntimeStats: true,
		},
		GlobalAttributes: map[string]string{"global.attr": "value"},
	}

	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
		obs.WithServiceName("gateway"),
		obs.WithServiceVersion("dev-test"),
		obs.WithEnvironment("dev"),
		obs.WithAttributes(map[string]string{"extra": "attr"}),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			require.NoError(t, shutdown(context.Background()))
		}
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
		otel.SetMeterProvider(noopmetric.NewMeterProvider())
	})
}

func TestInitRequiresLogger(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
	}

	_, err := obs.Init(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "logger is required")
}

func TestShutdownRestoresErrorHandler(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
	}

	origHandler := otel.GetErrorHandler()
	sentinel := &stubHandler{}
	otel.SetErrorHandler(sentinel)
	t.Cleanup(func() {
		otel.SetErrorHandler(origHandler)
	})

	logger := log.NewStdLogger(testWriter{t})

	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(logger),
		obs.WithServiceName("test-service"),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NotEqual(t, sentinel, otel.GetErrorHandler())

	require.NoError(t, shutdown(context.Background()))
	require.Equal(t, sentinel, otel.GetErrorHandler())

	otel.SetTracerProvider(nooptrace.NewTracerProvider())
}

func TestBuildResourceMergesAttributes(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:        true,
			Exporter:       obs.ExporterStdout,
			ServiceName:    "cfg-service",
			ServiceVersion: "cfg-version",
			Environment:    "cfg-env",
			Attributes:     map[string]string{"trace": "attr"},
		},
		Metrics: &obs.MetricsConfig{
			Enabled:            true,
			Exporter:           obs.ExporterStdout,
			ResourceAttributes: map[string]string{"metric": "attr"},
		},
		GlobalAttributes: map[string]string{"global": "attr"},
	}

	res, err := obs.BuildResource(context.Background(), cfg,
		obs.WithServiceName("svc"),
		obs.WithServiceVersion("v1"),
		obs.WithEnvironment("dev"),
		obs.WithAttributes(map[string]string{"extra": "attr"}),
	)
	require.NoError(t, err)

	attrs := map[string]string{}
	for _, kv := range res.Attributes() {
		if kv.Value.Type() == attribute.STRING {
			attrs[string(kv.Key)] = kv.Value.AsString()
		}
	}

	require.Equal(t, "svc", attrs["service.name"])
	require.Equal(t, "v1", attrs["service.version"])
	require.Equal(t, "dev", attrs["deployment.environment"])
	require.Equal(t, "attr", attrs["trace"])
	require.Equal(t, "attr", attrs["metric"])
	require.Equal(t, "attr", attrs["global"])
	require.Equal(t, "attr", attrs["extra"])
}

func TestInit_NilContext(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
	}

	_, err := obs.Init(nil, cfg, obs.WithLogger(log.NewStdLogger(testWriter{t})))
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil context")
}

func TestInit_TracingRequiredFails(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: "invalid_exporter",
			Required: true,
		},
	}

	_, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
	)
	require.Error(t, err, "Required=true 时初始化失败应该返回错误")
}

func TestInit_TracingNotRequiredDegrades(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: "invalid_exporter",
			Required: false,
		},
		Metrics: &obs.MetricsConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
	}

	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
	)
	require.NoError(t, err, "Required=false 时初始化失败应该降级，不返回错误")
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			require.NoError(t, shutdown(context.Background()))
		}
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
		otel.SetMeterProvider(noopmetric.NewMeterProvider())
	})
}

func TestInit_TracingDisabled(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled: false,
		},
		Metrics: &obs.MetricsConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
	}

	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			require.NoError(t, shutdown(context.Background()))
		}
		otel.SetMeterProvider(noopmetric.NewMeterProvider())
	})
}

func TestInit_MetricsDisabled(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
		Metrics: &obs.MetricsConfig{
			Enabled: false,
		},
	}

	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			require.NoError(t, shutdown(context.Background()))
		}
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})
}

func TestInit_BothDisabled(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled: false,
		},
		Metrics: &obs.MetricsConfig{
			Enabled: false,
		},
	}

	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Shutdown 应该成功，即使没有初始化任何 provider
	require.NoError(t, shutdown(context.Background()))
}

func TestInit_MetricsRequiredFails(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Metrics: &obs.MetricsConfig{
			Enabled:  true,
			Exporter: "invalid_exporter",
			Required: true,
		},
	}

	_, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
	)
	require.Error(t, err, "Required=true 时 metrics 初始化失败应该返回错误")
}

func TestInit_MetricsNotRequiredDegrades(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
		Metrics: &obs.MetricsConfig{
			Enabled:  true,
			Exporter: "invalid_exporter",
			Required: false,
		},
	}

	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
	)
	require.NoError(t, err, "Required=false 时 metrics 初始化失败应该降级")
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			require.NoError(t, shutdown(context.Background()))
		}
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
		otel.SetMeterProvider(noopmetric.NewMeterProvider())
	})
}

func TestInit_ShutdownAggregatesMultipleFunctions(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
		Metrics: &obs.MetricsConfig{
			Enabled:             true,
			Exporter:            obs.ExporterStdout,
			DisableRuntimeStats: true,
		},
	}

	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Shutdown 应该调用所有注册的清理函数
	err = shutdown(context.Background())
	require.NoError(t, err)

	otel.SetTracerProvider(nooptrace.NewTracerProvider())
	otel.SetMeterProvider(noopmetric.NewMeterProvider())
}

func TestBuildResource_NilContext(t *testing.T) {
	cfg := obs.ObservabilityConfig{}
	_, err := obs.BuildResource(nil, cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil context")
}

func TestBuildResource_ConfigPriorityOverrides(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			ServiceName:    "tracing-service",
			ServiceVersion: "tracing-version",
			Environment:    "tracing-env",
		},
	}

	// WithServiceName 等 Option 应该覆盖 Tracing 配置
	res, err := obs.BuildResource(context.Background(), cfg,
		obs.WithServiceName("override-service"),
		obs.WithServiceVersion("override-version"),
		obs.WithEnvironment("override-env"),
	)
	require.NoError(t, err)

	attrs := map[string]string{}
	for _, kv := range res.Attributes() {
		if kv.Value.Type() == attribute.STRING {
			attrs[string(kv.Key)] = kv.Value.AsString()
		}
	}

	require.Equal(t, "override-service", attrs["service.name"])
	require.Equal(t, "override-version", attrs["service.version"])
	require.Equal(t, "override-env", attrs["deployment.environment"])
}

func TestBuildResource_GlobalAttributesMerge(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		GlobalAttributes: map[string]string{
			"global.key1": "global.value1",
			"global.key2": "global.value2",
		},
		Tracing: &obs.TracingConfig{
			Attributes: map[string]string{
				"trace.key": "trace.value",
			},
		},
		Metrics: &obs.MetricsConfig{
			ResourceAttributes: map[string]string{
				"metric.key": "metric.value",
			},
		},
	}

	res, err := obs.BuildResource(context.Background(), cfg,
		obs.WithAttributes(map[string]string{
			"extra.key": "extra.value",
		}),
	)
	require.NoError(t, err)

	attrs := map[string]string{}
	for _, kv := range res.Attributes() {
		if kv.Value.Type() == attribute.STRING {
			attrs[string(kv.Key)] = kv.Value.AsString()
		}
	}

	require.Equal(t, "global.value1", attrs["global.key1"])
	require.Equal(t, "global.value2", attrs["global.key2"])
	require.Equal(t, "trace.value", attrs["trace.key"])
	require.Equal(t, "metric.value", attrs["metric.key"])
	require.Equal(t, "extra.value", attrs["extra.key"])
}

func TestWithLogger_NilLogger(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
	}

	// 传递 nil logger 应该被忽略
	_, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(nil),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "logger is required")
}

func TestWithPropagator_NilPropagator(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:  true,
			Exporter: obs.ExporterStdout,
		},
	}

	// nil propagator 应该被忽略，使用默认值
	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
		obs.WithPropagator(nil),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			require.NoError(t, shutdown(context.Background()))
		}
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})
}

func TestInit_SamplingRatioClamped(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:       true,
			Exporter:      obs.ExporterStdout,
			SamplingRatio: 2.5, // 超过 1.0 应该被钳制
		},
	}

	shutdown, err := obs.Init(context.Background(), cfg,
		obs.WithLogger(log.NewStdLogger(testWriter{t})),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			require.NoError(t, shutdown(context.Background()))
		}
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}
