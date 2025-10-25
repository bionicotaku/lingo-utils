package inbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bionicotaku/lingo-utils/gcpubsub"
    "github.com/bionicotaku/lingo-utils/outbox/store"
	"github.com/bionicotaku/lingo-utils/txmanager"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
)

// Decoder 将消息字节解析为领域事件。
type Decoder[T any] interface {
	Decode(data []byte) (*T, error)
}

// Handler 处理解析后的领域事件。
type Handler[T any] interface {
    Handle(ctx context.Context, sess txmanager.Session, evt *T, inboxEvt *store.InboxEvent) error
}

// ConsumerOptions 配置项。
type ConsumerOptions struct {
	SourceService  string
	MaxConcurrency int
}

// Consumer 封装 StreamingPull + Inbox 幂等流程。
type Consumer[T any] struct {
	subscriber gcpubsub.Subscriber
    store      *store.Repository
	txManager  txmanager.Manager
	decoder    Decoder[T]
	handler    Handler[T]
	opts       ConsumerOptions
	log        *log.Helper
	clock      func() time.Time
}

func NewConsumer[T any](sub gcpubsub.Subscriber, store *store.Repository, tx txmanager.Manager, decoder Decoder[T], handler Handler[T], opts ConsumerOptions, logger log.Logger) *Consumer[T] {
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = 4
	}
	helper := log.NewHelper(logger)
	return &Consumer[T]{
		subscriber: sub,
		store:      store,
		txManager:  tx,
		decoder:    decoder,
		handler:    handler,
		opts:       opts,
		log:        helper,
		clock:      time.Now,
	}
}

func (c *Consumer[T]) WithClock(clock func() time.Time) {
	if clock != nil {
		c.clock = clock
	}
}

func (c *Consumer[T]) Run(ctx context.Context) error {
	if c.subscriber == nil {
		return nil
	}
	sem := make(chan struct{}, c.opts.MaxConcurrency)
	return c.subscriber.Receive(ctx, func(ctx context.Context, msg *gcpubsub.Message) error {
		sem <- struct{}{}
		defer func() { <-sem }()
		return c.handleMessage(ctx, msg)
	})
}

var (
	errMissingEventID   = errors.New("inbox consumer: missing event_id attribute")
	errMissingEventType = errors.New("inbox consumer: missing event_type attribute")
)

func (c *Consumer[T]) handleMessage(ctx context.Context, msg *gcpubsub.Message) error {
	if msg == nil {
		return errors.New("inbox consumer: nil message")
	}

	inboxMsg, err := c.buildInboxMessage(msg)
	if err != nil {
		return err
	}

	return c.txManager.WithinTx(ctx, txmanager.TxOptions{}, func(txCtx context.Context, sess txmanager.Session) error {
		if err := c.store.RecordInboxEvent(txCtx, sess, inboxMsg); err != nil {
			return fmt.Errorf("record inbox event: %w", err)
		}

		inboxEvt, err := c.store.GetInboxEvent(txCtx, sess, inboxMsg.EventID)
		if err != nil {
			return fmt.Errorf("get inbox event: %w", err)
		}

		if inboxEvt.ProcessedAt != nil {
			c.log.WithContext(txCtx).Debugw("msg", "inbox event already processed", "event_id", inboxEvt.EventID)
			return nil
		}

		decoded, err := c.decoder.Decode(msg.Data)
		if err != nil {
			if recErr := c.store.RecordInboxError(txCtx, sess, inboxEvt.EventID, err.Error()); recErr != nil {
				c.log.WithContext(txCtx).Warnw("msg", "record inbox error failed", "event_id", inboxEvt.EventID, "error", recErr)
			}
			return fmt.Errorf("decode payload: %w", err)
		}

		if err := c.handler.Handle(txCtx, sess, decoded, inboxEvt); err != nil {
			if recErr := c.store.RecordInboxError(txCtx, sess, inboxEvt.EventID, err.Error()); recErr != nil {
				c.log.WithContext(txCtx).Warnw("msg", "record inbox error failed", "event_id", inboxEvt.EventID, "error", recErr)
			}
			return err
		}

		return c.store.MarkInboxProcessed(txCtx, sess, inboxEvt.EventID, c.clock().UTC())
	})
}

func (c *Consumer[T]) buildInboxMessage(msg *gcpubsub.Message) (store.InboxMessage, error) {
    attrs := msg.Attributes
    eventIDStr := attrs["event_id"]
    if eventIDStr == "" {
        return store.InboxMessage{}, errMissingEventID
    }
    eventID, err := uuid.Parse(eventIDStr)
    if err != nil {
        return store.InboxMessage{}, fmt.Errorf("inbox consumer: parse event_id: %w", err)
    }

    eventType := attrs["event_type"]
    if eventType == "" {
        return store.InboxMessage{}, errMissingEventType
    }

    inboxMsg := store.InboxMessage{
		EventID:       eventID,
		SourceService: c.opts.SourceService,
		EventType:     eventType,
		Payload:       append([]byte(nil), msg.Data...),
	}

	if aggType := attrs["aggregate_type"]; aggType != "" {
		inboxMsg.AggregateType = &aggType
	}
	if aggID := attrs["aggregate_id"]; aggID != "" {
		inboxMsg.AggregateID = &aggID
	}

	return inboxMsg, nil
}
