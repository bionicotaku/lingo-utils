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

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}
