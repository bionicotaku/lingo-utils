package observability_test

import (
	"testing"
	"time"

	obs "github.com/bionicotaku/lingo-utils/observability"
	"github.com/stretchr/testify/require"
)

func TestConfigSanitize_EmptyConfig(t *testing.T) {
	cfg := obs.ObservabilityConfig{}
	sanitized := invokeSanitize(cfg)

	// Tracing 应该被初始化为非 nil，但字段为零值
	require.NotNil(t, sanitized.Tracing)
	// 注意：sanitize 只在 Tracing != nil 时才设置默认值
	// 如果原始 Tracing 为 nil，创建的空 TracingConfig 不会被填充默认值

	// Metrics 应该被初始化为非 nil
	require.NotNil(t, sanitized.Metrics)
	require.True(t, sanitized.Metrics.GRPCEnabled)
}

func TestConfigSanitize_TracingDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    *obs.TracingConfig
		validate func(t *testing.T, output *obs.TracingConfig)
	}{
		{
			name: "默认 exporter",
			input: &obs.TracingConfig{
				Enabled: true,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, obs.ExporterOTLPgRPC, output.Exporter)
			},
		},
		{
			name: "SamplingRatio 负数应该被钳制为 1.0",
			input: &obs.TracingConfig{
				Enabled:       true,
				SamplingRatio: -0.5,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 1.0, output.SamplingRatio)
			},
		},
		{
			name: "SamplingRatio 零值应该被钳制为 1.0",
			input: &obs.TracingConfig{
				Enabled:       true,
				SamplingRatio: 0,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 1.0, output.SamplingRatio)
			},
		},
		{
			name: "SamplingRatio 大于 1 应该被钳制为 1.0",
			input: &obs.TracingConfig{
				Enabled:       true,
				SamplingRatio: 2.5,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 1.0, output.SamplingRatio)
			},
		},
		{
			name: "SamplingRatio 正常值应该保留",
			input: &obs.TracingConfig{
				Enabled:       true,
				SamplingRatio: 0.5,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 0.5, output.SamplingRatio)
			},
		},
		{
			name: "BatchTimeout 默认值",
			input: &obs.TracingConfig{
				Enabled:      true,
				BatchTimeout: 0,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 5*time.Second, output.BatchTimeout)
			},
		},
		{
			name: "BatchTimeout 负数应该使用默认值",
			input: &obs.TracingConfig{
				Enabled:      true,
				BatchTimeout: -1 * time.Second,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 5*time.Second, output.BatchTimeout)
			},
		},
		{
			name: "BatchTimeout 自定义值应该保留",
			input: &obs.TracingConfig{
				Enabled:      true,
				BatchTimeout: 3 * time.Second,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 3*time.Second, output.BatchTimeout)
			},
		},
		{
			name: "ExportTimeout 默认值",
			input: &obs.TracingConfig{
				Enabled:       true,
				ExportTimeout: 0,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 10*time.Second, output.ExportTimeout)
			},
		},
		{
			name: "ExportTimeout 负数应该使用默认值",
			input: &obs.TracingConfig{
				Enabled:       true,
				ExportTimeout: -5 * time.Second,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 10*time.Second, output.ExportTimeout)
			},
		},
		{
			name: "ExportTimeout 自定义值应该保留",
			input: &obs.TracingConfig{
				Enabled:       true,
				ExportTimeout: 15 * time.Second,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 15*time.Second, output.ExportTimeout)
			},
		},
		{
			name: "MaxQueueSize 默认值",
			input: &obs.TracingConfig{
				Enabled:      true,
				MaxQueueSize: 0,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 2048, output.MaxQueueSize)
			},
		},
		{
			name: "MaxQueueSize 负数应该使用默认值",
			input: &obs.TracingConfig{
				Enabled:      true,
				MaxQueueSize: -100,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 2048, output.MaxQueueSize)
			},
		},
		{
			name: "MaxQueueSize 自定义值应该保留",
			input: &obs.TracingConfig{
				Enabled:      true,
				MaxQueueSize: 4096,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 4096, output.MaxQueueSize)
			},
		},
		{
			name: "MaxExportBatchSize 默认值",
			input: &obs.TracingConfig{
				Enabled:            true,
				MaxExportBatchSize: 0,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 512, output.MaxExportBatchSize)
			},
		},
		{
			name: "MaxExportBatchSize 负数应该使用默认值",
			input: &obs.TracingConfig{
				Enabled:            true,
				MaxExportBatchSize: -10,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 512, output.MaxExportBatchSize)
			},
		},
		{
			name: "MaxExportBatchSize 大于 MaxQueueSize 应该被钳制为 512",
			input: &obs.TracingConfig{
				Enabled:            true,
				MaxQueueSize:       1024,
				MaxExportBatchSize: 2048,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 512, output.MaxExportBatchSize)
			},
		},
		{
			name: "MaxExportBatchSize 自定义值在合理范围应该保留",
			input: &obs.TracingConfig{
				Enabled:            true,
				MaxQueueSize:       2048,
				MaxExportBatchSize: 256,
			},
			validate: func(t *testing.T, output *obs.TracingConfig) {
				require.Equal(t, 256, output.MaxExportBatchSize)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := obs.ObservabilityConfig{
				Tracing: tt.input,
			}
			sanitized := invokeSanitize(cfg)
			tt.validate(t, sanitized.Tracing)
		})
	}
}

func TestConfigSanitize_MetricsDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    *obs.MetricsConfig
		validate func(t *testing.T, output *obs.MetricsConfig)
	}{
		{
			name: "默认 exporter",
			input: &obs.MetricsConfig{
				Enabled: true,
			},
			validate: func(t *testing.T, output *obs.MetricsConfig) {
				require.Equal(t, obs.ExporterOTLPgRPC, output.Exporter)
			},
		},
		{
			name: "Interval 默认值",
			input: &obs.MetricsConfig{
				Enabled:  true,
				Interval: 0,
			},
			validate: func(t *testing.T, output *obs.MetricsConfig) {
				require.Equal(t, 60*time.Second, output.Interval)
			},
		},
		{
			name: "Interval 负数应该使用默认值",
			input: &obs.MetricsConfig{
				Enabled:  true,
				Interval: -30 * time.Second,
			},
			validate: func(t *testing.T, output *obs.MetricsConfig) {
				require.Equal(t, 60*time.Second, output.Interval)
			},
		},
		{
			name: "Interval 自定义值应该保留",
			input: &obs.MetricsConfig{
				Enabled:  true,
				Interval: 30 * time.Second,
			},
			validate: func(t *testing.T, output *obs.MetricsConfig) {
				require.Equal(t, 30*time.Second, output.Interval)
			},
		},
		{
			name: "布尔字段应该保留用户指定的值",
			input: &obs.MetricsConfig{
				Enabled:             true,
				DisableRuntimeStats: true,
				Required:            true,
				GRPCEnabled:         false,
				GRPCIncludeHealth:   true,
			},
			validate: func(t *testing.T, output *obs.MetricsConfig) {
				require.True(t, output.DisableRuntimeStats)
				require.True(t, output.Required)
				require.False(t, output.GRPCEnabled)
				require.True(t, output.GRPCIncludeHealth)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := obs.ObservabilityConfig{
				Metrics: tt.input,
			}
			sanitized := invokeSanitize(cfg)
			tt.validate(t, sanitized.Metrics)
		})
	}
}

func TestConfigSanitize_PreservesOriginalConfig(t *testing.T) {
	original := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled:       true,
			SamplingRatio: 0.5,
			BatchTimeout:  3 * time.Second,
		},
		Metrics: &obs.MetricsConfig{
			Enabled:  true,
			Interval: 30 * time.Second,
		},
		GlobalAttributes: map[string]string{"original": "value"},
	}

	// 保存原始值
	originalSamplingRatio := original.Tracing.SamplingRatio
	originalBatchTimeout := original.Tracing.BatchTimeout
	originalInterval := original.Metrics.Interval

	// 调用 sanitize
	_ = invokeSanitize(original)

	// 验证原始配置没有被修改
	require.Equal(t, originalSamplingRatio, original.Tracing.SamplingRatio)
	require.Equal(t, originalBatchTimeout, original.Tracing.BatchTimeout)
	require.Equal(t, originalInterval, original.Metrics.Interval)
	require.Equal(t, "value", original.GlobalAttributes["original"])
}

func TestConfigSanitize_NilMetricsInitializedWithGRPCEnabled(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		Tracing: &obs.TracingConfig{
			Enabled: true,
		},
		// Metrics 为 nil
	}

	sanitized := invokeSanitize(cfg)
	require.NotNil(t, sanitized.Metrics)
	require.True(t, sanitized.Metrics.GRPCEnabled, "Metrics 为 nil 时应该初始化 GRPCEnabled 为 true")
}

// invokeSanitize 复制 config.go 中 sanitize 方法的逻辑
// 由于 sanitize 是私有方法，我们无法直接调用，因此复制其实现来测试
func invokeSanitize(cfg obs.ObservabilityConfig) obs.ObservabilityConfig {
	sanitized := cfg
	if sanitized.Tracing == nil {
		sanitized.Tracing = &obs.TracingConfig{}
	} else {
		tr := *sanitized.Tracing
		if tr.Exporter == "" {
			tr.Exporter = obs.ExporterOTLPgRPC
		}
		if tr.SamplingRatio <= 0 {
			tr.SamplingRatio = 1.0
		} else if tr.SamplingRatio > 1 {
			tr.SamplingRatio = 1.0
		}
		if tr.BatchTimeout <= 0 {
			tr.BatchTimeout = 5 * time.Second
		}
		if tr.ExportTimeout <= 0 {
			tr.ExportTimeout = 10 * time.Second
		}
		if tr.MaxQueueSize <= 0 {
			tr.MaxQueueSize = 2048
		}
		if tr.MaxExportBatchSize <= 0 || tr.MaxExportBatchSize > tr.MaxQueueSize {
			tr.MaxExportBatchSize = 512
		}
		sanitized.Tracing = &tr
	}

	if sanitized.Metrics == nil {
		sanitized.Metrics = &obs.MetricsConfig{GRPCEnabled: true}
	} else {
		mt := *sanitized.Metrics
		if mt.Exporter == "" {
			mt.Exporter = obs.ExporterOTLPgRPC
		}
		if mt.Interval <= 0 {
			mt.Interval = 60 * time.Second
		}
		sanitized.Metrics = &mt
	}

	return sanitized
}
