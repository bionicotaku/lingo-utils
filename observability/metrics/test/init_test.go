package metrics_test

import (
	"context"
	"testing"
	"time"

	metrics "github.com/bionicotaku/lingo-utils/observability/metrics"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestInitStdout(t *testing.T) {
	cfg := metrics.Config{
		Exporter:            "stdout",
		Interval:            10 * time.Millisecond,
		DisableRuntimeStats: true,
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	shutdown, err := metrics.Init(context.Background(), cfg,
		metrics.WithLogger(log.NewStdLogger(testWriter{t})),
		metrics.WithResource(res),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, shutdown(ctx))
		}
		otel.SetMeterProvider(noopmetric.NewMeterProvider())
	})
}

func TestNewExporterError(t *testing.T) {
	_, err := metrics.Init(context.Background(), metrics.Config{Exporter: "unknown"})
	require.Error(t, err)
}

func TestInit_NilContext(t *testing.T) {
	cfg := metrics.Config{
		Exporter: "stdout",
	}

	_, err := metrics.Init(nil, cfg, metrics.WithLogger(log.NewStdLogger(testWriter{t})))
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil context")
}

func TestInit_RequiresLogger(t *testing.T) {
	cfg := metrics.Config{
		Exporter: "stdout",
	}

	_, err := metrics.Init(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "logger is required")
}

func TestInit_NilLogger(t *testing.T) {
	cfg := metrics.Config{
		Exporter: "stdout",
	}

	_, err := metrics.Init(context.Background(), cfg, metrics.WithLogger(nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "logger is required")
}

func TestInit_UnsupportedExporter(t *testing.T) {
	cfg := metrics.Config{
		Exporter: "unsupported_exporter",
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	_, err = metrics.Init(context.Background(), cfg,
		metrics.WithLogger(log.NewStdLogger(testWriter{t})),
		metrics.WithResource(res),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported metrics exporter")
}

func TestInit_RuntimeStatsEnabled(t *testing.T) {
	cfg := metrics.Config{
		Exporter:            "stdout",
		Interval:            10 * time.Millisecond,
		DisableRuntimeStats: false, // 明确启用
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	shutdown, err := metrics.Init(context.Background(), cfg,
		metrics.WithLogger(log.NewStdLogger(testWriter{t})),
		metrics.WithResource(res),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, shutdown(ctx))
		}
		otel.SetMeterProvider(noopmetric.NewMeterProvider())
	})
}

func TestInit_RuntimeStatsDisabled(t *testing.T) {
	cfg := metrics.Config{
		Exporter:            "stdout",
		Interval:            10 * time.Millisecond,
		DisableRuntimeStats: true,
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	shutdown, err := metrics.Init(context.Background(), cfg,
		metrics.WithLogger(log.NewStdLogger(testWriter{t})),
		metrics.WithResource(res),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, shutdown(ctx))
		}
		otel.SetMeterProvider(noopmetric.NewMeterProvider())
	})
}

func TestInit_IntervalConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		valid    bool
	}{
		{
			name:     "零值应该使用默认值 60s",
			interval: 0,
			valid:    true,
		},
		{
			name:     "负数应该使用默认值 60s",
			interval: -10 * time.Second,
			valid:    true,
		},
		{
			name:     "自定义值应该保留",
			interval: 30 * time.Second,
			valid:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := metrics.Config{
				Exporter:            "stdout",
				Interval:            tt.interval,
				DisableRuntimeStats: true,
			}
			res, err := resource.New(context.Background())
			require.NoError(t, err)

			shutdown, err := metrics.Init(context.Background(), cfg,
				metrics.WithLogger(log.NewStdLogger(testWriter{t})),
				metrics.WithResource(res),
			)

			if tt.valid {
				require.NoError(t, err)
				require.NotNil(t, shutdown)

				t.Cleanup(func() {
					if shutdown != nil {
						ctx, cancel := context.WithTimeout(context.Background(), time.Second)
						defer cancel()
						require.NoError(t, shutdown(ctx))
					}
					otel.SetMeterProvider(noopmetric.NewMeterProvider())
				})
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestInit_OTLPExporterConfiguration(t *testing.T) {
	t.Skip("OTLP exporter 需要真实的服务器，跳过集成测试")
}

func TestInit_NilResource(t *testing.T) {
	cfg := metrics.Config{
		Exporter:            "stdout",
		Interval:            10 * time.Millisecond,
		DisableRuntimeStats: true,
	}

	// nil resource 应该使用默认 resource
	shutdown, err := metrics.Init(context.Background(), cfg,
		metrics.WithLogger(log.NewStdLogger(testWriter{t})),
		metrics.WithResource(nil),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	t.Cleanup(func() {
		if shutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, shutdown(ctx))
		}
		otel.SetMeterProvider(noopmetric.NewMeterProvider())
	})
}

func TestInit_DefaultExporter(t *testing.T) {
	t.Skip("默认 OTLP exporter 需要真实的服务器，跳过集成测试")
}

func TestInit_ShutdownCleanup(t *testing.T) {
	cfg := metrics.Config{
		Exporter:            "stdout",
		Interval:            10 * time.Millisecond,
		DisableRuntimeStats: true,
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	shutdown, err := metrics.Init(context.Background(), cfg,
		metrics.WithLogger(log.NewStdLogger(testWriter{t})),
		metrics.WithResource(res),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// 测试 shutdown 成功
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = shutdown(ctx)
	require.NoError(t, err)

	otel.SetMeterProvider(noopmetric.NewMeterProvider())
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}
