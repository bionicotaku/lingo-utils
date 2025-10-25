package gcpubsub

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/pubsub"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
)

// Component 聚合 gcpubsub 组件资源。
type Component struct {
	client     *pubsub.Client
	publisher  Publisher
	subscriber Subscriber
	logger     *log.Helper
	cfg        Config
}

// NewComponent 构建 gcpubsub 组件。
func NewComponent(ctx context.Context, cfg Config, deps Dependencies) (*Component, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}

    sanitized := cfg.Normalize()
	if sanitized.ProjectID == "" {
		return nil, nil, errors.New("gcpubsub: projectID is required")
	}

	resolved := resolveDependencies(sanitized, deps)
	helper := log.NewHelper(resolved.logger)

	telemetry := newTelemetry(resolved.meter, helper, sanitized.metricsEnabled())

	client, err := resolved.factory(ctx, sanitized.ProjectID, resolved.credentials, resolved.dial)
	if err != nil {
		return nil, nil, fmt.Errorf("gcpubsub: create client: %w", err)
	}

	var topic *pubsub.Topic
	if sanitized.TopicID != "" {
		topic = client.Topic(sanitized.TopicID)
	}

	var sub *pubsub.Subscription
	if sanitized.SubscriptionID != "" {
		sub = client.Subscription(sanitized.SubscriptionID)
	}

	pub := newPublisher(topic, telemetry, helper, sanitized, resolved.clock)
	subc := newSubscriber(sub, telemetry, helper, sanitized, resolved.clock)

	component := &Component{
		client:     client,
		publisher:  pub,
		subscriber: subc,
		logger:     helper,
		cfg:        sanitized,
	}

	cleanup := func() {
		_ = component.publisher.Flush(context.Background())
		if err := client.Close(); err != nil && component.cfg.loggingEnabled() {
			helper.Warnw("msg", "gcpubsub client close failed", "error", err)
		}
	}

	return component, cleanup, nil
}

// ProvidePublisher 暴露 Publisher。
func ProvidePublisher(c *Component) Publisher {
	if c == nil || c.publisher == nil {
		return noopPublisher{}
	}
	return c.publisher
}

// ProvideSubscriber 暴露 Subscriber。
func ProvideSubscriber(c *Component) Subscriber {
	if c == nil || c.subscriber == nil {
		return noopSubscriber{}
	}
	return c.subscriber
}

// ProviderSet 用于 Wire 注入。
var ProviderSet = wire.NewSet(NewComponent, ProvidePublisher, ProvideSubscriber)
