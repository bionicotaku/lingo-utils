package gcpubsub_test

import (
	"context"
	"io"
	"testing"
	"time"

	g "github.com/bionicotaku/lingo-utils/gcpubsub"
	"github.com/go-kratos/kratos/v2/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestTelemetryPublishMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	helper := log.NewHelper(log.NewStdLogger(io.Discard))

	tel := g.NewTelemetryForTest(provider.Meter("test"), helper, true)

	ctx := context.Background()
	tel.RecordPublish(ctx, "topicA", 128, 20*time.Millisecond, nil)

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("collect: %v", err)
	}

	counts := 0
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "pubsub_publish_total" {
				counts++
			}
		}
	}
	if counts == 0 {
		t.Fatalf("expected publish_total metric to be recorded")
	}
}

func TestTelemetryConsumeMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	helper := log.NewHelper(log.NewStdLogger(io.Discard))

	tel := g.NewTelemetryForTest(provider.Meter("test"), helper, true)

	ctx := context.Background()
	tel.RecordReceive(ctx, "subA", 30*time.Millisecond, 10*time.Millisecond, 2, nil)

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("collect: %v", err)
	}

	counts := 0
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "pubsub_receive_total" {
				counts++
			}
		}
	}
	if counts == 0 {
		t.Fatalf("expected receive_total metric to be recorded")
	}
}
