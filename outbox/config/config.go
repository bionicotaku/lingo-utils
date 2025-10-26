package config

import (
	"errors"
	"time"
)

// Config 聚合 Outbox/Inbox 运行所需的核心配置。
type Config struct {
	// Schema 指定事件表所在的 PostgreSQL schema，例如 "catalog"。
	Schema string
	// Publisher 控制 Outbox 发布器的调度行为。
	Publisher PublisherConfig
	// Inbox 控制 StreamingPull 消费者的并发与观测策略。
	Inbox InboxConfig
}

// PublisherConfig 对应 Outbox 发布器的运行参数，字段与
// publisher.Config 对齐，保持默认值语义一致。
type PublisherConfig struct {
	BatchSize      int
	TickInterval   time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	MaxAttempts    int
	PublishTimeout time.Duration
	Workers        int
	LockTTL        time.Duration
	LoggingEnabled *bool
	MetricsEnabled *bool
}

// InboxConfig 描述消费者级别的并发与观测设置。
type InboxConfig struct {
	// SourceService 表示事件来源服务名称，会写入 inbox_events.source_service。
	SourceService string
	// MaxConcurrency 限制单实例同时处理的消息数量。
	MaxConcurrency int
	// LoggingEnabled 控制消费者是否输出日志；nil 表示使用默认值 true。
	LoggingEnabled *bool
	// MetricsEnabled 控制消费者是否上报指标；nil 表示使用默认值 true。
	MetricsEnabled *bool
}

// Normalize 应用默认值并返回副本，不改变原始配置。
func (c Config) Normalize() Config {
	normalized := c
	normalized.Publisher = normalized.Publisher.Normalize()
	normalized.Inbox = normalized.Inbox.Normalize()
	return normalized
}

// Validate 检查配置是否满足最小运行要求。
func (c Config) Validate() error {
	if c.Schema == "" {
		return errors.New("outbox: schema is required")
	}
	if err := c.Publisher.Validate(); err != nil {
		return err
	}
	if err := c.Inbox.Validate(); err != nil {
		return err
	}
	return nil
}

// Normalize 应用默认值（针对发布器配置）。
func (c PublisherConfig) Normalize() PublisherConfig {
	normalized := c
	if normalized.BatchSize <= 0 {
		normalized.BatchSize = 100
	}
	if normalized.TickInterval <= 0 {
		normalized.TickInterval = time.Second
	}
	if normalized.InitialBackoff <= 0 {
		normalized.InitialBackoff = 2 * time.Second
	}
	if normalized.MaxBackoff <= 0 {
		normalized.MaxBackoff = 120 * time.Second
	}
	if normalized.MaxAttempts <= 0 {
		normalized.MaxAttempts = 20
	}
	if normalized.PublishTimeout <= 0 {
		normalized.PublishTimeout = 10 * time.Second
	}
	if normalized.Workers <= 0 {
		normalized.Workers = 4
	}
	if normalized.LockTTL <= 0 {
		normalized.LockTTL = 2 * time.Minute
	}
	return normalized
}

// Validate 检查发布器配置。
func (c PublisherConfig) Validate() error {
	if c.BatchSize <= 0 {
		return errors.New("outbox: publisher batch_size must be positive")
	}
	if c.Workers <= 0 {
		return errors.New("outbox: publisher workers must be positive")
	}
	return nil
}

// Normalize 应用默认值（针对消费者配置）。
func (c InboxConfig) Normalize() InboxConfig {
	normalized := c
	if normalized.MaxConcurrency <= 0 {
		normalized.MaxConcurrency = 4
	}
	return normalized
}

// Validate 检查消费者配置。
func (c InboxConfig) Validate() error {
	if c.SourceService == "" {
		return errors.New("outbox: inbox source_service is required")
	}
	if c.MaxConcurrency <= 0 {
		return errors.New("outbox: inbox max_concurrency must be positive")
	}
	return nil
}
