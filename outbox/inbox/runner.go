package inbox

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/bionicotaku/lingo-utils/gcpubsub"
	"github.com/bionicotaku/lingo-utils/outbox/config"
	"github.com/bionicotaku/lingo-utils/outbox/store"
	"github.com/bionicotaku/lingo-utils/txmanager"
	"github.com/go-kratos/kratos/v2/log"
)

// Runner 将 Consumer 与配置解耦，便于服务直接调用。
type Runner[T any] struct {
	consumer *Consumer[T]
}

// RunnerParams 构造消费者所需的依赖。
type RunnerParams[T any] struct {
	Store      *store.Repository
	Subscriber gcpubsub.Subscriber
	TxManager  txmanager.Manager
	Decoder    Decoder[T]
	Handler    Handler[T]
	Config     config.InboxConfig
	Logger     log.Logger
}

// NewRunner 构造 StreamingPull 消费 Runner。
func NewRunner[T any](params RunnerParams[T]) (*Runner[T], error) {
	if params.Store == nil {
		return nil, ErrMissingStore
	}
	if params.Subscriber == nil {
		return nil, ErrMissingSubscriber
	}
	if params.TxManager == nil {
		return nil, ErrMissingTxManager
	}
	if params.Decoder == nil {
		return nil, ErrMissingDecoder
	}
	if params.Handler == nil {
		return nil, ErrMissingHandler
	}

	cfg := params.Config.Normalize()

	logger := params.Logger
	if logger == nil {
		logger = log.NewStdLogger(io.Discard)
	}

	consumer := NewConsumer(params.Subscriber, params.Store, params.TxManager, params.Decoder, params.Handler, ConsumerOptions{
		SourceService:  cfg.SourceService,
		MaxConcurrency: cfg.MaxConcurrency,
	}, logger)

	return &Runner[T]{consumer: consumer}, nil
}

// Run 启动消费循环。
func (r *Runner[T]) Run(ctx context.Context) error {
	if r == nil || r.consumer == nil {
		return nil
	}
	return r.consumer.Run(ctx)
}

// WithClock 暴露测试辅助注入。
func (r *Runner[T]) WithClock(clock func() time.Time) {
	if r == nil || r.consumer == nil {
		return
	}
	r.consumer.WithClock(clock)
}

var (
	ErrMissingStore      = errors.New("outbox: inbox repository is required")
	ErrMissingSubscriber = errors.New("outbox: subscriber is required")
	ErrMissingTxManager  = errors.New("outbox: tx manager is required")
	ErrMissingDecoder    = errors.New("outbox: decoder is required")
	ErrMissingHandler    = errors.New("outbox: handler is required")
)
