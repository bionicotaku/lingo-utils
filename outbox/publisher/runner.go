package publisher

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/bionicotaku/lingo-utils/gcpubsub"
	"github.com/bionicotaku/lingo-utils/outbox/config"
	"github.com/bionicotaku/lingo-utils/outbox/store"
	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Runner 封装 Outbox 发布循环，便于在服务内最小化调用。
type Runner struct {
	task *Task
}

// RunnerParams 描述构建 Runner 所需的依赖。
type RunnerParams struct {
	Store     *store.Repository
	Publisher gcpubsub.Publisher
	Config    config.PublisherConfig
	Logger    log.Logger
	Meter     metric.Meter
}

// NewRunner 根据配置与依赖构造发布器 Runner。
func NewRunner(params RunnerParams) (*Runner, error) {
	if params.Store == nil {
		return nil, ErrMissingStore
	}
	if params.Publisher == nil {
		return nil, ErrMissingPublisher
	}
	cfg := params.Config.Normalize()

	logger := params.Logger
	if logger == nil {
		logger = log.NewStdLogger(io.Discard)
	}

	meter := params.Meter
	if meter == nil {
		meter = otel.GetMeterProvider().Meter("lingo-utils.outbox.publisher")
	}

	taskCfg := Config{
		BatchSize:      cfg.BatchSize,
		TickInterval:   cfg.TickInterval,
		InitialBackoff: cfg.InitialBackoff,
		MaxBackoff:     cfg.MaxBackoff,
		MaxAttempts:    cfg.MaxAttempts,
		PublishTimeout: cfg.PublishTimeout,
		Workers:        cfg.Workers,
		LockTTL:        cfg.LockTTL,
		LoggingEnabled: cfg.LoggingEnabled,
		MetricsEnabled: cfg.MetricsEnabled,
	}

	task := NewTask(params.Store, params.Publisher, taskCfg, logger, meter)
	return &Runner{task: task}, nil
}

// Run 执行发布循环。
func (r *Runner) Run(ctx context.Context) error {
	if r == nil || r.task == nil {
		return nil
	}
	return r.task.Run(ctx)
}

// WithClock 暴露测试辅助方法。
func (r *Runner) WithClock(clock func() time.Time) {
	if r == nil || r.task == nil {
		return
	}
	r.task.WithClock(clock)
}

var (
	// ErrMissingStore 在未提供仓储实例时返回。
	ErrMissingStore = errors.New("outbox: repository is required")
	// ErrMissingPublisher 在未提供消息发布器时返回。
	ErrMissingPublisher = errors.New("outbox: publisher is required")
)
