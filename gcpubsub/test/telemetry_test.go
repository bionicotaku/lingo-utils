package gcpubsub_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	g "github.com/bionicotaku/lingo-utils/gcpubsub"
	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel/attribute"
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
	tel.RecordPublish(ctx, "topicA", 64, 15*time.Millisecond, errors.New("publish failed"))

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("collect: %v", err)
	}

	var successCount, errorCount bool
	var latencyRecorded, payloadRecorded bool

	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				if m.Name == "pubsub_publish_total" {
					for _, dp := range d.DataPoints {
						attrs := attributeMap(dp.Attributes)
						switch attrs["pubsub.result"].AsString() {
						case "success":
							successCount = true
						case "error":
							errorCount = true
						}
					}
				}
			case metricdata.Histogram[float64]:
				if m.Name == "pubsub_publish_latency_ms" && len(d.DataPoints) > 0 {
					latencyRecorded = true
				}
			case metricdata.Histogram[int64]:
				if m.Name == "pubsub_publish_payload_bytes" && len(d.DataPoints) > 0 {
					payloadRecorded = true
				}
			}
		}
	}

	if !successCount || !errorCount {
		t.Fatalf("expected both success and error publish_total datapoints, success=%v error=%v", successCount, errorCount)
	}
	if !latencyRecorded {
		t.Fatalf("expected latency histogram datapoints")
	}
	if !payloadRecorded {
		t.Fatalf("expected payload histogram datapoints")
	}
}

func TestTelemetryConsumeMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	helper := log.NewHelper(log.NewStdLogger(io.Discard))

	tel := g.NewTelemetryForTest(provider.Meter("test"), helper, true)

	ctx := context.Background()
	tel.RecordReceive(ctx, "subA", 30*time.Millisecond, 10*time.Millisecond, 2, nil)
	tel.RecordReceive(ctx, "subA", 15*time.Millisecond, 5*time.Millisecond, 1, errors.New("handler failed"))

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("collect: %v", err)
	}

	var receiveSuccess, receiveError bool
	var handlerLatencyRecorded, ackLatencyRecorded bool
	var deliveryAttempts []int64

	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				switch m.Name {
				case "pubsub_receive_total":
					for _, dp := range d.DataPoints {
						attrs := attributeMap(dp.Attributes)
						switch attrs["pubsub.result"].AsString() {
						case "success":
							receiveSuccess = true
						case "error":
							receiveError = true
						}
					}
				case "pubsub_delivery_attempt_total":
					for _, dp := range d.DataPoints {
						attrs := attributeMap(dp.Attributes)
						if v, ok := attrs["pubsub.delivery_attempt"]; ok {
							deliveryAttempts = append(deliveryAttempts, v.AsInt64())
						}
					}
				}
			case metricdata.Histogram[float64]:
				if m.Name == "pubsub_handler_duration_ms" && len(d.DataPoints) > 0 {
					handlerLatencyRecorded = true
				}
				if m.Name == "pubsub_ack_latency_ms" && len(d.DataPoints) > 0 {
					ackLatencyRecorded = true
				}
			}
		}
	}

	if !receiveSuccess || !receiveError {
		t.Fatalf("expected both success and error receive_total datapoints")
	}
	if !handlerLatencyRecorded {
		t.Fatalf("expected handler latency datapoints")
	}
	if !ackLatencyRecorded {
		t.Fatalf("expected ack latency datapoints")
	}
	if len(deliveryAttempts) == 0 {
		t.Fatalf("expected delivery attempt counter datapoints")
	}
}

func attributeMap(set attribute.Set) map[string]attribute.Value {
	out := make(map[string]attribute.Value, set.Len())
	for _, kv := range set.ToSlice() {
		out[string(kv.Key)] = kv.Value
	}
	return out
}
