package gcpubsub

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/go-kratos/kratos/v2/log"
)

// Subscriber 定义 StreamingPull 消费接口。
type Subscriber interface {
	Receive(ctx context.Context, handler func(context.Context, *Message) error) error
	Stop()
}

var errSubscriberDisabled = errors.New("gcpubsub: subscriber disabled")

type subscriber struct {
	subscription   *pubsub.Subscription
	telemetry      *telemetry
	logger         *log.Helper
	loggingEnabled bool
	subName        string
	clock          func() time.Time
}

func newSubscriber(sub *pubsub.Subscription, telem *telemetry, helper *log.Helper, cfg Config, clock func() time.Time) Subscriber {
	if sub == nil {
		return noopSubscriber{}
	}
	sub.ReceiveSettings.NumGoroutines = cfg.Receive.NumGoroutines
	sub.ReceiveSettings.MaxOutstandingMessages = cfg.Receive.MaxOutstandingMessages
	sub.ReceiveSettings.MaxOutstandingBytes = cfg.Receive.MaxOutstandingBytes
	sub.ReceiveSettings.MaxExtension = cfg.Receive.MaxExtension
	sub.ReceiveSettings.MaxExtensionPeriod = cfg.Receive.MaxExtensionPeriod

	return &subscriber{
		subscription:   sub,
		telemetry:      telem,
		logger:         helper,
		loggingEnabled: cfg.loggingEnabled(),
		subName:        cfg.SubscriptionID,
		clock:          clock,
	}
}

func (s *subscriber) Receive(ctx context.Context, handler func(context.Context, *Message) error) error {
	if s.subscription == nil {
		return errSubscriberDisabled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.subscription.Receive(ctx, func(rCtx context.Context, m *pubsub.Message) {
		wrapped := convertPubsubMessage(m)
		start := s.clock()
		handlerErr := s.invokeHandler(rCtx, handler, wrapped)
		handlerLatency := time.Since(start)

		if handlerErr != nil {
			m.Nack()
		} else {
			m.Ack()
		}

		ackLatency := time.Since(start)
		if s.telemetry != nil {
			s.telemetry.recordReceive(rCtx, s.subName, handlerLatency, ackLatency, wrapped.DeliveryAttempt, handlerErr)
		}
		if s.loggingEnabled {
			s.logReceive(rCtx, wrapped, handlerLatency, handlerErr)
		}
	})
}

func (s *subscriber) Stop() {}

func (s *subscriber) invokeHandler(ctx context.Context, handler func(context.Context, *Message) error, msg *Message) (err error) {
	if handler == nil {
		return errors.New("gcpubsub: nil handler")
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gcpubsub: handler panic: %v", r)
			if s.loggingEnabled {
				s.logger.WithContext(ctx).Errorw("msg", "gcpubsub handler panic", "subscription", s.subName, "error", err)
			}
		}
	}()
	return handler(ctx, msg)
}

func (s *subscriber) logReceive(ctx context.Context, msg *Message, latency time.Duration, err error) {
	fields := []any{
		"subscription", s.subName,
		"message_id", msg.ID,
		"delivery_attempt", msg.DeliveryAttempt,
		"latency_ms", latency.Milliseconds(),
	}
	if err != nil {
		args := append([]any{"msg", "gcpubsub receive failed"}, fields...)
		args = append(args, "error", err)
		s.logger.WithContext(ctx).Warnw(args...)
		return
	}
	args := append([]any{"msg", "gcpubsub receive success"}, fields...)
	s.logger.WithContext(ctx).Debugw(args...)
}

func convertPubsubMessage(m *pubsub.Message) *Message {
	if m == nil {
		return &Message{}
	}
	attrs := cloneAttributes(m.Attributes)
	dataCopy := make([]byte, len(m.Data))
	copy(dataCopy, m.Data)
	attempt := 0
	if m.DeliveryAttempt != nil {
		attempt = *m.DeliveryAttempt
	}
	return &Message{
		ID:              m.ID,
		Data:            dataCopy,
		Attributes:      attrs,
		OrderingKey:     m.OrderingKey,
		PublishTime:     m.PublishTime,
		DeliveryAttempt: attempt,
	}
}

// noopSubscriber 为占位实现。
type noopSubscriber struct{}

func (noopSubscriber) Receive(_ context.Context, _ func(context.Context, *Message) error) error {
	return errSubscriberDisabled
}

func (noopSubscriber) Stop() {}
