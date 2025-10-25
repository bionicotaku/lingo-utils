package gcpubsub

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	attrTopicKey        = "pubsub.topic"
	attrResultKey       = "pubsub.result"
	attrSubscriptionKey = "pubsub.subscription"
	attrAttemptKey      = "pubsub.delivery_attempt"
)

var (
	attrTopic        = attribute.Key(attrTopicKey)
	attrResult       = attribute.Key(attrResultKey)
	attrSubscription = attribute.Key(attrSubscriptionKey)
	attrAttempt      = attribute.Key(attrAttemptKey)
)

// telemetry 负责记录指标与结构化日志。
type telemetry struct {
	enabled bool
	helper  *log.Helper

	publishCount   metric.Int64Counter
	publishLatency metric.Float64Histogram
	publishBytes   metric.Int64Histogram

	receiveCount   metric.Int64Counter
	handlerLatency metric.Float64Histogram
	ackLatency     metric.Float64Histogram
	deliveryCount  metric.Int64Counter
}

func newTelemetry(meter metric.Meter, helper *log.Helper, enabled bool) *telemetry {
	t := &telemetry{enabled: enabled, helper: helper}
	if !enabled {
		return t
	}

	var err error
	if t.publishCount, err = meter.Int64Counter("pubsub_publish_total"); err != nil {
		helper.Warnw("msg", "gcpubsub: register publish_total", "err", err)
	}
	if t.publishLatency, err = meter.Float64Histogram("pubsub_publish_latency_ms", metric.WithUnit("ms")); err != nil {
		helper.Warnw("msg", "gcpubsub: register publish_latency", "err", err)
	}
	if t.publishBytes, err = meter.Int64Histogram("pubsub_publish_payload_bytes", metric.WithUnit("By")); err != nil {
		helper.Warnw("msg", "gcpubsub: register publish_payload", "err", err)
	}
	if t.receiveCount, err = meter.Int64Counter("pubsub_receive_total"); err != nil {
		helper.Warnw("msg", "gcpubsub: register receive_total", "err", err)
	}
	if t.handlerLatency, err = meter.Float64Histogram("pubsub_handler_duration_ms", metric.WithUnit("ms")); err != nil {
		helper.Warnw("msg", "gcpubsub: register handler_latency", "err", err)
	}
	if t.ackLatency, err = meter.Float64Histogram("pubsub_ack_latency_ms", metric.WithUnit("ms")); err != nil {
		helper.Warnw("msg", "gcpubsub: register ack_latency", "err", err)
	}
	if t.deliveryCount, err = meter.Int64Counter("pubsub_delivery_attempt_total"); err != nil {
		helper.Warnw("msg", "gcpubsub: register delivery_attempt", "err", err)
	}
	return t
}

func (t *telemetry) recordPublish(ctx context.Context, topic string, payloadBytes int, latency time.Duration, err error) {
	if !t.enabled {
		return
	}
	result := "success"
	if err != nil {
		result = "error"
	}
	attrs := metric.WithAttributes(
		attrTopic.String(topic),
		attrResult.String(result),
	)
	if t.publishCount != nil {
		t.publishCount.Add(ctx, 1, attrs)
	}
	if t.publishLatency != nil {
		t.publishLatency.Record(ctx, float64(latency.Milliseconds()), attrs)
	}
	if payloadBytes >= 0 && t.publishBytes != nil {
		t.publishBytes.Record(ctx, int64(payloadBytes), attrs)
	}
}

func (t *telemetry) recordReceive(ctx context.Context, subscription string, handlerLatency time.Duration, ackLatency time.Duration, deliveryAttempt int, err error) {
	if !t.enabled {
		return
	}
	result := "success"
	if err != nil {
		result = "error"
	}
	attrs := metric.WithAttributes(
		attrSubscription.String(subscription),
		attrResult.String(result),
	)
	if t.receiveCount != nil {
		t.receiveCount.Add(ctx, 1, attrs)
	}
	if t.handlerLatency != nil {
		t.handlerLatency.Record(ctx, float64(handlerLatency.Milliseconds()), attrs)
	}
	if ackLatency >= 0 && t.ackLatency != nil {
		t.ackLatency.Record(ctx, float64(ackLatency.Milliseconds()), attrs)
	}
	if deliveryAttempt > 0 && t.deliveryCount != nil {
		t.deliveryCount.Add(ctx, int64(deliveryAttempt), metric.WithAttributes(
			attrSubscription.String(subscription),
			attrAttempt.Int(deliveryAttempt),
		))
	}
}
