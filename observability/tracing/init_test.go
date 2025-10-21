package tracing

import (
	"context"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestInitStdout(t *testing.T) {
	cfg := Config{
		Exporter:      "stdout",
		SamplingRatio: 1.5,
	}
	res, err := resource.New(context.Background())
	require.NoError(t, err)

	shutdown, err := Init(context.Background(), cfg,
		WithLogger(log.NewStdLogger(testWriter{t})),
		WithResource(res),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx))
}

func TestNewExporterError(t *testing.T) {
	_, err := newExporter(context.Background(), Config{Exporter: "unknown"})
	require.Error(t, err)
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}
