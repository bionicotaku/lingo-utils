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

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}
