package observability

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// buildResource assembles the OpenTelemetry resource describing the running service.
func buildResource(ctx context.Context, base ObservabilityConfig, overrides initOptions) (*resource.Resource, error) {
	cfg := base.sanitize()

	serviceName := overrides.serviceName
	if serviceName == "" {
		serviceName = cfg.Tracing.ServiceName
	}
	serviceVersion := overrides.serviceVersion
	if serviceVersion == "" {
		serviceVersion = cfg.Tracing.ServiceVersion
	}
	environment := overrides.environment
	if environment == "" {
		environment = cfg.Tracing.Environment
	}

	var attrs []attribute.KeyValue
	if serviceName != "" {
		attrs = append(attrs, semconv.ServiceNameKey.String(serviceName))
	}
	if serviceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersionKey.String(serviceVersion))
	}
	if environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentKey.String(environment))
	}
	if hostname, _ := os.Hostname(); hostname != "" {
		attrs = append(attrs, semconv.HostNameKey.String(hostname))
	}

	for k, v := range cfg.GlobalAttributes {
		attrs = append(attrs, attribute.String(k, v))
	}
	for k, v := range cfg.Tracing.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}
	for k, v := range cfg.Metrics.ResourceAttributes {
		attrs = append(attrs, attribute.String(k, v))
	}
	for k, v := range overrides.attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithOS(),
		resource.WithHost(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, err
	}

	return res, nil
}
