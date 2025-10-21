package observability

import (
	"context"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
)

func TestInitWithStdoutExporters(t *testing.T) {
	cfg := ObservabilityConfig{
		Tracing: &TracingConfig{
			Enabled:       true,
			Exporter:      ExporterStdout,
			SamplingRatio: 2, // should be clamped to 1.0
		},
		Metrics: &MetricsConfig{
			Enabled:             true,
			Exporter:            ExporterStdout,
			Interval:            50 * time.Millisecond,
			DisableRuntimeStats: true,
		},
		GlobalAttributes: map[string]string{"global.attr": "value"},
	}

	shutdown, err := Init(context.Background(), cfg,
		WithLogger(log.NewStdLogger(testWriter{t})),
		WithServiceName("gateway"),
		WithServiceVersion("dev-test"),
		WithEnvironment("dev"),
		WithAttributes(map[string]string{"extra": "attr"}),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	require.NoError(t, shutdown(context.Background()))
}

func TestBuildResourceMergesAttributes(t *testing.T) {
	cfg := ObservabilityConfig{
		Tracing: &TracingConfig{
			Attributes: map[string]string{"trace": "attr"},
		},
		Metrics: &MetricsConfig{
			ResourceAttributes: map[string]string{"metric": "attr"},
		},
		GlobalAttributes: map[string]string{"global": "attr"},
	}
	opts := defaultInitOptions()
	opts.serviceName = "svc"
	opts.serviceVersion = "v1"
	opts.environment = "dev"
	opts.attributes = map[string]string{"extra": "attr"}

	res, err := buildResource(context.Background(), cfg, opts)
	require.NoError(t, err)

	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}

	require.Equal(t, "svc", got["service.name"])
	require.Equal(t, "v1", got["service.version"])
	require.Equal(t, "dev", got["deployment.environment"])
	require.Equal(t, "attr", got["trace"])
	require.Equal(t, "attr", got["metric"])
	require.Equal(t, "attr", got["global"])
	require.Equal(t, "attr", got["extra"])
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}
