package gcpubsub

import (
	"context"
	"errors"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/go-kratos/kratos/v2/log"
)

// Message 表示发布到 Pub/Sub 的消息结构。
type Message struct {
	ID              string
	Data            []byte
	Attributes      map[string]string
	OrderingKey     string
	EventID         string
	PublishTime     time.Time
	DeliveryAttempt int
}

// Publisher 定义发布接口。
type Publisher interface {
	Publish(ctx context.Context, msg Message) (string, error)
	Flush(ctx context.Context) error
}

var errPublisherDisabled = errors.New("gcpubsub: publisher disabled")

type publisher struct {
	topic           *pubsub.Topic
	telemetry       *telemetry
	logger          *log.Helper
	loggingEnabled  bool
	orderingEnabled bool
	topicName       string
	clock           func() time.Time
	publishTimeout  time.Duration

	stopOnce sync.Once
}

func newPublisher(topic *pubsub.Topic, telem *telemetry, helper *log.Helper, cfg Config, clock func() time.Time) Publisher {
	if topic == nil {
		return noopPublisher{}
	}
	topic.EnableMessageOrdering = cfg.orderingEnabled()
	return &publisher{
		topic:           topic,
		telemetry:       telem,
		logger:          helper,
		loggingEnabled:  cfg.loggingEnabled(),
		orderingEnabled: cfg.orderingEnabled(),
		topicName:       cfg.TopicID,
		clock:           clock,
		publishTimeout:  cfg.PublishTimeout,
	}
}

func (p *publisher) Publish(ctx context.Context, msg Message) (string, error) {
	if p.topic == nil {
		return "", errPublisherDisabled
	}
	if ctx == nil {
		ctx = context.Background()
	}

	publishCtx := ctx
	var cancel context.CancelFunc
	if p.publishTimeout > 0 {
		publishCtx, cancel = context.WithTimeout(ctx, p.publishTimeout)
		defer cancel()
	}

	attributes := cloneAttributes(msg.Attributes)
	pubsubMsg := &pubsub.Message{
		Data:       msg.Data,
		Attributes: attributes,
	}
	if p.orderingEnabled {
		pubsubMsg.OrderingKey = msg.OrderingKey
	}

	start := p.clock()
	result := p.topic.Publish(publishCtx, pubsubMsg)
	serverID, err := result.Get(publishCtx)
	latency := time.Since(start)

	if p.telemetry != nil {
		p.telemetry.recordPublish(ctx, p.topicName, len(msg.Data), latency, err)
	}

	if p.loggingEnabled {
		p.logPublishResult(ctx, msg, pubsubMsg.OrderingKey, serverID, latency, err)
	}

	if err != nil {
		return "", err
	}
	return serverID, nil
}

func (p *publisher) Flush(context.Context) error {
	if p.topic == nil {
		return nil
	}
	p.stopOnce.Do(func() {
		p.topic.Stop()
	})
	return nil
}

func (p *publisher) logPublishResult(ctx context.Context, msg Message, orderingKey string, serverID string, latency time.Duration, err error) {
	fields := []any{
		"topic", p.topicName,
		"event_id", msg.EventID,
		"ordering_key", orderingKey,
		"latency_ms", latency.Milliseconds(),
	}
	if err != nil {
		args := append([]any{"msg", "gcpubsub publish failed"}, fields...)
		args = append(args, "error", err)
		p.logger.WithContext(ctx).Warnw(args...)
		return
	}
	args := append([]any{"msg", "gcpubsub publish success"}, fields...)
	args = append(args, "server_id", serverID)
	p.logger.WithContext(ctx).Debugw(args...)
}

func cloneAttributes(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	copied := make(map[string]string, len(attrs))
	for k, v := range attrs {
		copied[k] = v
	}
	return copied
}

// noopPublisher 是占位实现，用于在 topic 未配置时作为安全默认。
type noopPublisher struct{}

func (noopPublisher) Publish(_ context.Context, _ Message) (string, error) {
	return "", errPublisherDisabled
}

func (noopPublisher) Flush(_ context.Context) error { return nil }
