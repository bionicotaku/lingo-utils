// Package gcpubsub 提供 Google Cloud Pub/Sub 组件的配置定义。
package gcpubsub

import "time"

const (
	defaultPublishTimeout         = 10 * time.Second
	defaultMeterName              = "lingo-utils.gcpubsub"
	defaultReceiveNumGoroutines   = 1
	defaultMaxOutstandingMessages = 1000
	defaultMaxOutstandingBytes    = 64 << 20 // 64 MiB
	defaultMaxExtension           = time.Minute
	defaultMaxExtensionPeriod     = 10 * time.Minute
)

// Config 定义 gcpubsub 组件的运行参数。
type Config struct {
	ProjectID           string        `json:"projectID" yaml:"projectID"`
	TopicID             string        `json:"topicID" yaml:"topicID"`
	SubscriptionID      string        `json:"subscriptionID" yaml:"subscriptionID"`
	PublishTimeout      time.Duration `json:"publishTimeout" yaml:"publishTimeout"`
	OrderingKeyEnabled  *bool         `json:"orderingKeyEnabled" yaml:"orderingKeyEnabled"`
	EnableLogging       *bool         `json:"enableLogging" yaml:"enableLogging"`
	EnableMetrics       *bool         `json:"enableMetrics" yaml:"enableMetrics"`
	MeterName           string        `json:"meterName" yaml:"meterName"`
	EmulatorEndpoint    string        `json:"emulatorEndpoint" yaml:"emulatorEndpoint"`
	Receive             ReceiveConfig `json:"receive" yaml:"receive"`
	ExactlyOnceDelivery bool          `json:"exactlyOnceDelivery" yaml:"exactlyOnceDelivery"`
}

// ReceiveConfig 定义 StreamingPull 的并发与流控设置。
type ReceiveConfig struct {
	NumGoroutines          int           `json:"numGoroutines" yaml:"numGoroutines"`
	MaxOutstandingMessages int           `json:"maxOutstandingMessages" yaml:"maxOutstandingMessages"`
	MaxOutstandingBytes    int           `json:"maxOutstandingBytes" yaml:"maxOutstandingBytes"`
	MaxExtension           time.Duration `json:"maxExtension" yaml:"maxExtension"`
	MaxExtensionPeriod     time.Duration `json:"maxExtensionPeriod" yaml:"maxExtensionPeriod"`
}

// Normalize 返回填充默认值后的配置副本。
func (c Config) Normalize() Config {
	s := c

	if s.PublishTimeout <= 0 {
		s.PublishTimeout = defaultPublishTimeout
	}
	if s.OrderingKeyEnabled == nil {
		s.OrderingKeyEnabled = boolPtr(true)
	}
	if s.EnableLogging == nil {
		s.EnableLogging = boolPtr(true)
	}
	if s.EnableMetrics == nil {
		s.EnableMetrics = boolPtr(true)
	}
	if s.MeterName == "" {
		s.MeterName = defaultMeterName
	}

	s.Receive = s.Receive.withDefaults()

	// Emulator 与 ExactlyOnceDelivery 不兼容，默认关闭。
	if s.EmulatorEndpoint != "" {
		s.ExactlyOnceDelivery = false
	}

	return s
}

func (rc ReceiveConfig) withDefaults() ReceiveConfig {
	s := rc
	if s.NumGoroutines <= 0 {
		s.NumGoroutines = defaultReceiveNumGoroutines
	}
	if s.MaxOutstandingMessages <= 0 {
		s.MaxOutstandingMessages = defaultMaxOutstandingMessages
	}
	if s.MaxOutstandingBytes <= 0 {
		s.MaxOutstandingBytes = defaultMaxOutstandingBytes
	}
	if s.MaxExtension <= 0 {
		s.MaxExtension = defaultMaxExtension
	}
	if s.MaxExtensionPeriod <= 0 {
		s.MaxExtensionPeriod = defaultMaxExtensionPeriod
	}
	return s
}

func boolPtr(v bool) *bool {
	b := v
	return &b
}

func (c Config) loggingEnabled() bool {
	return c.EnableLogging == nil || *c.EnableLogging
}

func (c Config) metricsEnabled() bool {
	return c.EnableMetrics == nil || *c.EnableMetrics
}

func (c Config) orderingEnabled() bool {
	return c.OrderingKeyEnabled == nil || *c.OrderingKeyEnabled
}

// LoggingEnabled 返回配置是否开启日志。
func (c Config) LoggingEnabled() bool {
	return c.loggingEnabled()
}

// MetricsEnabled 返回配置是否开启指标。
func (c Config) MetricsEnabled() bool {
	return c.metricsEnabled()
}

// OrderingKeyEnabledValue 返回是否启用消息有序投递。
func (c Config) OrderingKeyEnabledValue() bool {
	return c.orderingEnabled()
}
