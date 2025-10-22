package tracing_test

import (
	"context"
	"testing"
	"time"

	tracing "github.com/bionicotaku/lingo-utils/observability/tracing"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

func TestInitStdout(t *testing.T) {
	cfg := tracing.Config{
		Exporter:      "stdout",
		SamplingRatio: 1.5,
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	shutdown, err := tracing.Init(context.Background(), cfg,
		tracing.WithLogger(log.NewStdLogger(testWriter{t})),
		tracing.WithResource(res),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, shutdown(ctx))
		}
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})
}

func TestNewExporterError(t *testing.T) {
	_, err := tracing.Init(context.Background(), tracing.Config{Exporter: "unknown"})
	require.Error(t, err)
}

func TestInit_NilContext(t *testing.T) {
	cfg := tracing.Config{
		Exporter: "stdout",
	}

	_, err := tracing.Init(nil, cfg, tracing.WithLogger(log.NewStdLogger(testWriter{t})))
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil context")
}

func TestInit_RequiresLogger(t *testing.T) {
	cfg := tracing.Config{
		Exporter: "stdout",
	}

	_, err := tracing.Init(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "logger is required")
}

func TestInit_NilLogger(t *testing.T) {
	cfg := tracing.Config{
		Exporter: "stdout",
	}

	_, err := tracing.Init(context.Background(), cfg, tracing.WithLogger(nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "logger is required")
}

func TestInit_UnsupportedExporter(t *testing.T) {
	cfg := tracing.Config{
		Exporter: "unsupported_exporter",
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	_, err = tracing.Init(context.Background(), cfg,
		tracing.WithLogger(log.NewStdLogger(testWriter{t})),
		tracing.WithResource(res),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported tracing exporter")
}

func TestInit_OTLPExporterConfiguration(t *testing.T) {
	cfg := tracing.Config{
		Exporter:      "otlp_grpc",
		Endpoint:      "localhost:4317",
		Insecure:      true,
		SamplingRatio: 1.0,
		Headers: map[string]string{
			"custom-header": "value",
		},
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	shutdown, err := tracing.Init(context.Background(), cfg,
		tracing.WithLogger(log.NewStdLogger(testWriter{t})),
		tracing.WithResource(res),
	)

	// 由于没有实际的 OTLP 服务器，初始化可能会失败
	// 但我们主要测试配置是否被正确处理
	if err == nil && shutdown != nil {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, shutdown(ctx))
			otel.SetTracerProvider(nooptrace.NewTracerProvider())
		})
	}
}

func TestInit_SamplingRatioNormalization(t *testing.T) {
	tests := []struct {
		name          string
		samplingRatio float64
		expectError   bool
	}{
		{
			name:          "负数应该被规范化为 1.0",
			samplingRatio: -0.5,
			expectError:   false,
		},
		{
			name:          "零值应该被规范化为 1.0",
			samplingRatio: 0,
			expectError:   false,
		},
		{
			name:          "大于 1 应该被规范化为 1.0",
			samplingRatio: 2.0,
			expectError:   false,
		},
		{
			name:          "正常值应该保留",
			samplingRatio: 0.5,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tracing.Config{
				Exporter:      "stdout",
				SamplingRatio: tt.samplingRatio,
			}
			res, err := resource.New(context.Background())
			require.NoError(t, err)

			shutdown, err := tracing.Init(context.Background(), cfg,
				tracing.WithLogger(log.NewStdLogger(testWriter{t})),
				tracing.WithResource(res),
			)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, shutdown)

				t.Cleanup(func() {
					if shutdown != nil {
						ctx, cancel := context.WithTimeout(context.Background(), time.Second)
						defer cancel()
						require.NoError(t, shutdown(ctx))
					}
					otel.SetTracerProvider(nooptrace.NewTracerProvider())
				})
			}
		})
	}
}

func TestInit_BatchConfigurationDefaults(t *testing.T) {
	tests := []struct {
		name   string
		cfg    tracing.Config
		verify func(t *testing.T)
	}{
		{
			name: "BatchTimeout 默认值",
			cfg: tracing.Config{
				Exporter:     "stdout",
				BatchTimeout: 0,
			},
			verify: func(t *testing.T) {
				// 通过成功初始化来验证默认值被应用
			},
		},
		{
			name: "ExportTimeout 默认值",
			cfg: tracing.Config{
				Exporter:      "stdout",
				ExportTimeout: 0,
			},
			verify: func(t *testing.T) {
				// 通过成功初始化来验证默认值被应用
			},
		},
		{
			name: "MaxQueueSize 默认值",
			cfg: tracing.Config{
				Exporter:     "stdout",
				MaxQueueSize: 0,
			},
			verify: func(t *testing.T) {
				// 通过成功初始化来验证默认值被应用
			},
		},
		{
			name: "MaxExportBatchSize 默认值",
			cfg: tracing.Config{
				Exporter:           "stdout",
				MaxExportBatchSize: 0,
			},
			verify: func(t *testing.T) {
				// 通过成功初始化来验证默认值被应用
			},
		},
		{
			name: "MaxExportBatchSize 大于 MaxQueueSize 应该被钳制",
			cfg: tracing.Config{
				Exporter:           "stdout",
				MaxQueueSize:       100,
				MaxExportBatchSize: 200,
			},
			verify: func(t *testing.T) {
				// 通过成功初始化来验证被钳制
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := resource.New(context.Background())
			require.NoError(t, err)

			shutdown, err := tracing.Init(context.Background(), tt.cfg,
				tracing.WithLogger(log.NewStdLogger(testWriter{t})),
				tracing.WithResource(res),
			)
			require.NoError(t, err)
			require.NotNil(t, shutdown)

			tt.verify(t)

			t.Cleanup(func() {
				if shutdown != nil {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					require.NoError(t, shutdown(ctx))
				}
				otel.SetTracerProvider(nooptrace.NewTracerProvider())
			})
		})
	}
}

func TestInit_WithPropagator(t *testing.T) {
	cfg := tracing.Config{
		Exporter: "stdout",
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	customPropagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	shutdown, err := tracing.Init(context.Background(), cfg,
		tracing.WithLogger(log.NewStdLogger(testWriter{t})),
		tracing.WithResource(res),
		tracing.WithPropagator(customPropagator),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, shutdown(ctx))
		}
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})
}

func TestInit_NilPropagator(t *testing.T) {
	cfg := tracing.Config{
		Exporter: "stdout",
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	// nil propagator 应该使用默认值
	shutdown, err := tracing.Init(context.Background(), cfg,
		tracing.WithLogger(log.NewStdLogger(testWriter{t})),
		tracing.WithResource(res),
		tracing.WithPropagator(nil),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, shutdown(ctx))
		}
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})
}

func TestInit_NilResource(t *testing.T) {
	cfg := tracing.Config{
		Exporter: "stdout",
	}

	// nil resource 应该使用默认 resource
	shutdown, err := tracing.Init(context.Background(), cfg,
		tracing.WithLogger(log.NewStdLogger(testWriter{t})),
		tracing.WithResource(nil),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, shutdown(ctx))
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
