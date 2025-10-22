package tracing_test

import (
	"context"
	"testing"
	"time"

	tracing "github.com/bionicotaku/lingo-utils/observability/tracing"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
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

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}
