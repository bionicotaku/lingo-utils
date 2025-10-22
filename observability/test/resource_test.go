package observability_test

import (
	"context"
	"os"
	"testing"

	obs "github.com/bionicotaku/lingo-utils/observability"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestBuildResource_ServiceInfoInjection(t *testing.T) {
	tests := []struct {
		name     string
		cfg      obs.ObservabilityConfig
		opts     []obs.Option
		validate func(t *testing.T, attrs map[string]string)
	}{
		{
			name: "从 Tracing 配置注入 ServiceInfo",
			cfg: obs.ObservabilityConfig{
				Tracing: &obs.TracingConfig{
					ServiceName:    "test-service",
					ServiceVersion: "v1.0.0",
					Environment:    "production",
				},
			},
			opts: nil,
			validate: func(t *testing.T, attrs map[string]string) {
				require.Equal(t, "test-service", attrs["service.name"])
				require.Equal(t, "v1.0.0", attrs["service.version"])
				require.Equal(t, "production", attrs["deployment.environment"])
			},
		},
		{
			name: "Option 覆盖 Tracing 配置",
			cfg: obs.ObservabilityConfig{
				Tracing: &obs.TracingConfig{
					ServiceName:    "old-service",
					ServiceVersion: "old-version",
					Environment:    "old-env",
				},
			},
			opts: []obs.Option{
				obs.WithServiceName("new-service"),
				obs.WithServiceVersion("new-version"),
				obs.WithEnvironment("new-env"),
			},
			validate: func(t *testing.T, attrs map[string]string) {
				require.Equal(t, "new-service", attrs["service.name"])
				require.Equal(t, "new-version", attrs["service.version"])
				require.Equal(t, "new-env", attrs["deployment.environment"])
			},
		},
		{
			name: "Option 部分覆盖",
			cfg: obs.ObservabilityConfig{
				Tracing: &obs.TracingConfig{
					ServiceName:    "config-service",
					ServiceVersion: "config-version",
					Environment:    "config-env",
				},
			},
			opts: []obs.Option{
				obs.WithServiceName("override-service"),
				// 不覆盖 version 和 environment
			},
			validate: func(t *testing.T, attrs map[string]string) {
				require.Equal(t, "override-service", attrs["service.name"])
				require.Equal(t, "config-version", attrs["service.version"])
				require.Equal(t, "config-env", attrs["deployment.environment"])
			},
		},
		{
			name: "空配置不应该注入空值",
			cfg: obs.ObservabilityConfig{
				Tracing: &obs.TracingConfig{
					ServiceName: "", // 空字符串
				},
			},
			opts: nil,
			validate: func(t *testing.T, attrs map[string]string) {
				// 空字符串不应该被注入
				_, hasServiceName := attrs["service.name"]
				require.False(t, hasServiceName, "空 service.name 不应该被注入")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := obs.BuildResource(context.Background(), tt.cfg, tt.opts...)
			require.NoError(t, err)

			attrs := extractStringAttributes(res)
			tt.validate(t, attrs)
		})
	}
}

func TestBuildResource_AttributesMergePriority(t *testing.T) {
	tests := []struct {
		name     string
		cfg      obs.ObservabilityConfig
		opts     []obs.Option
		validate func(t *testing.T, attrs map[string]string)
	}{
		{
			name: "GlobalAttributes 应该被包含",
			cfg: obs.ObservabilityConfig{
				GlobalAttributes: map[string]string{
					"global.key1": "global.value1",
					"global.key2": "global.value2",
				},
			},
			opts: nil,
			validate: func(t *testing.T, attrs map[string]string) {
				require.Equal(t, "global.value1", attrs["global.key1"])
				require.Equal(t, "global.value2", attrs["global.key2"])
			},
		},
		{
			name: "Tracing.Attributes 应该被包含",
			cfg: obs.ObservabilityConfig{
				Tracing: &obs.TracingConfig{
					Attributes: map[string]string{
						"trace.key": "trace.value",
					},
				},
			},
			opts: nil,
			validate: func(t *testing.T, attrs map[string]string) {
				require.Equal(t, "trace.value", attrs["trace.key"])
			},
		},
		{
			name: "Metrics.ResourceAttributes 应该被包含",
			cfg: obs.ObservabilityConfig{
				Metrics: &obs.MetricsConfig{
					ResourceAttributes: map[string]string{
						"metric.key": "metric.value",
					},
				},
			},
			opts: nil,
			validate: func(t *testing.T, attrs map[string]string) {
				require.Equal(t, "metric.value", attrs["metric.key"])
			},
		},
		{
			name: "WithAttributes Option 应该被包含",
			cfg:  obs.ObservabilityConfig{},
			opts: []obs.Option{
				obs.WithAttributes(map[string]string{
					"option.key": "option.value",
				}),
			},
			validate: func(t *testing.T, attrs map[string]string) {
				require.Equal(t, "option.value", attrs["option.key"])
			},
		},
		{
			name: "所有来源的属性应该被合并",
			cfg: obs.ObservabilityConfig{
				GlobalAttributes: map[string]string{
					"global.key": "global.value",
				},
				Tracing: &obs.TracingConfig{
					Attributes: map[string]string{
						"trace.key": "trace.value",
					},
				},
				Metrics: &obs.MetricsConfig{
					ResourceAttributes: map[string]string{
						"metric.key": "metric.value",
					},
				},
			},
			opts: []obs.Option{
				obs.WithAttributes(map[string]string{
					"option.key": "option.value",
				}),
			},
			validate: func(t *testing.T, attrs map[string]string) {
				require.Equal(t, "global.value", attrs["global.key"])
				require.Equal(t, "trace.value", attrs["trace.key"])
				require.Equal(t, "metric.value", attrs["metric.key"])
				require.Equal(t, "option.value", attrs["option.key"])
			},
		},
		{
			name: "后续属性应该覆盖前面的同名属性",
			cfg: obs.ObservabilityConfig{
				GlobalAttributes: map[string]string{
					"duplicate.key": "global.value",
				},
				Tracing: &obs.TracingConfig{
					Attributes: map[string]string{
						"duplicate.key": "trace.value",
					},
				},
				Metrics: &obs.MetricsConfig{
					ResourceAttributes: map[string]string{
						"duplicate.key": "metric.value",
					},
				},
			},
			opts: []obs.Option{
				obs.WithAttributes(map[string]string{
					"duplicate.key": "option.value",
				}),
			},
			validate: func(t *testing.T, attrs map[string]string) {
				// Option 应该最后被应用，因此覆盖其他所有来源
				require.Equal(t, "option.value", attrs["duplicate.key"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := obs.BuildResource(context.Background(), tt.cfg, tt.opts...)
			require.NoError(t, err)

			attrs := extractStringAttributes(res)
			tt.validate(t, attrs)
		})
	}
}

func TestBuildResource_HostnameInjection(t *testing.T) {
	cfg := obs.ObservabilityConfig{}

	res, err := obs.BuildResource(context.Background(), cfg)
	require.NoError(t, err)

	attrs := extractStringAttributes(res)

	// 验证 hostname 被注入（如果可用）
	hostname, _ := os.Hostname()
	if hostname != "" {
		require.Equal(t, hostname, attrs["host.name"])
	}
}

func TestBuildResource_IncludesSDKDefaults(t *testing.T) {
	cfg := obs.ObservabilityConfig{}

	res, err := obs.BuildResource(context.Background(), cfg)
	require.NoError(t, err)

	attrs := extractStringAttributes(res)

	// 验证 SDK 默认属性被包含
	// 这些属性由 resource.WithTelemetrySDK() 提供
	require.NotEmpty(t, attrs["telemetry.sdk.name"])
	require.NotEmpty(t, attrs["telemetry.sdk.language"])
	require.NotEmpty(t, attrs["telemetry.sdk.version"])
}

func TestBuildResource_EmptyConfig(t *testing.T) {
	cfg := obs.ObservabilityConfig{}

	res, err := obs.BuildResource(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, res)

	// 即使配置为空，也应该返回包含默认属性的 resource
	attrs := extractStringAttributes(res)
	require.NotEmpty(t, attrs, "即使配置为空，resource 也应该包含默认属性")
}

func TestBuildResource_MultipleWithAttributes(t *testing.T) {
	cfg := obs.ObservabilityConfig{}

	res, err := obs.BuildResource(context.Background(), cfg,
		obs.WithAttributes(map[string]string{
			"attr1": "value1",
		}),
		obs.WithAttributes(map[string]string{
			"attr2": "value2",
		}),
		obs.WithAttributes(map[string]string{
			"attr3": "value3",
		}),
	)
	require.NoError(t, err)

	attrs := extractStringAttributes(res)
	require.Equal(t, "value1", attrs["attr1"])
	require.Equal(t, "value2", attrs["attr2"])
	require.Equal(t, "value3", attrs["attr3"])
}

func TestBuildResource_WithAttributesOverwritesSameKey(t *testing.T) {
	cfg := obs.ObservabilityConfig{}

	res, err := obs.BuildResource(context.Background(), cfg,
		obs.WithAttributes(map[string]string{
			"key": "first",
		}),
		obs.WithAttributes(map[string]string{
			"key": "second",
		}),
	)
	require.NoError(t, err)

	attrs := extractStringAttributes(res)
	require.Equal(t, "second", attrs["key"], "后续的 WithAttributes 应该覆盖前面的值")
}

func TestBuildResource_NilAttributeMaps(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		GlobalAttributes: nil,
		Tracing: &obs.TracingConfig{
			Attributes: nil,
		},
		Metrics: &obs.MetricsConfig{
			ResourceAttributes: nil,
		},
	}

	res, err := obs.BuildResource(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, res)

	// nil 属性 map 不应该导致错误
}

func TestBuildResource_EmptyAttributeMaps(t *testing.T) {
	cfg := obs.ObservabilityConfig{
		GlobalAttributes: map[string]string{},
		Tracing: &obs.TracingConfig{
			Attributes: map[string]string{},
		},
		Metrics: &obs.MetricsConfig{
			ResourceAttributes: map[string]string{},
		},
	}

	res, err := obs.BuildResource(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, res)

	// 空属性 map 不应该导致错误
}

func TestBuildResource_SanitizeApplied(t *testing.T) {
	// 验证 BuildResource 内部调用了 sanitize
	cfg := obs.ObservabilityConfig{
		Tracing: nil, // nil Tracing 应该被 sanitize 初始化
		Metrics: nil, // nil Metrics 应该被 sanitize 初始化
	}

	res, err := obs.BuildResource(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, res)

	// 如果 sanitize 被正确调用，不应该因为 nil Tracing/Metrics 而 panic
}

// extractStringAttributes 从 resource 中提取所有字符串类型的属性
func extractStringAttributes(res *resource.Resource) map[string]string {
	attrs := map[string]string{}
	for _, kv := range res.Attributes() {
		if kv.Value.Type() == attribute.STRING {
			attrs[string(kv.Key)] = kv.Value.AsString()
		}
	}
	return attrs
}
