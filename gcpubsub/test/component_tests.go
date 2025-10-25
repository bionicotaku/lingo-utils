package gcpubsub_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/pubsub/pstest"
	"github.com/bionicotaku/lingo-utils/gcpubsub"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestIntegrationPublishAndReceive(t *testing.T) {
	cfg := gcpubsub.Config{
		ProjectID:      "test-project",
		TopicID:        "test-topic",
		SubscriptionID: "test-sub",
	}

	comp, baseCtx := setupComponent(t, cfg)

	msg := gcpubsub.Message{
		Data:        []byte("hello"),
		Attributes:  map[string]string{"k": "v"},
		OrderingKey: "order-1",
		EventID:     "event-1",
	}

	if _, err := comp.Publish(baseCtx, msg); err != nil {
		t.Fatalf("publish: %v", err)
	}

	recvCtx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	received := false
	handler := func(ctx context.Context, m *gcpubsub.Message) error {
		mu.Lock()
		defer mu.Unlock()
		received = true
		if string(m.Data) != "hello" {
			t.Fatalf("unexpected data: %s", string(m.Data))
		}
		if m.Attributes["k"] != "v" {
			t.Fatalf("unexpected attribute: %v", m.Attributes)
		}
		cancel()
		return nil
	}

	err := comp.Receive(recvCtx, handler)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("receive: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !received {
		t.Fatalf("message not received")
	}
}

func TestPublishDisabledWithoutTopic(t *testing.T) {
	cfg := gcpubsub.Config{
		ProjectID: "test-project",
	}
	comp, ctx := setupComponent(t, cfg)
	_, err := comp.Publish(ctx, gcpubsub.Message{Data: []byte("payload")})
	if err == nil {
		t.Fatalf("expected error when topic is not configured")
	}
	if err.Error() != "gcpubsub: publisher disabled" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOrderingDisabledClearsOrderingKey(t *testing.T) {
	ordering := false
	cfg := gcpubsub.Config{
		ProjectID:          "test-project",
		TopicID:            "test-topic",
		SubscriptionID:     "test-sub",
		OrderingKeyEnabled: &ordering,
	}
	comp, baseCtx := setupComponent(t, cfg)

	if _, err := comp.Publish(baseCtx, gcpubsub.Message{Data: []byte("hello"), OrderingKey: "ordered"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	recvCtx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
	defer cancel()

	handler := func(ctx context.Context, msg *gcpubsub.Message) error {
		if msg.OrderingKey != "" {
			t.Fatalf("expected empty ordering key, got %q", msg.OrderingKey)
		}
		cancel()
		return nil
	}

	err := comp.Receive(recvCtx, handler)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("receive: %v", err)
	}
}

func TestHandlerErrorTriggersRedelivery(t *testing.T) {
	cfg := gcpubsub.Config{
		ProjectID:      "test-project",
		TopicID:        "test-topic",
		SubscriptionID: "test-sub",
	}
	comp, baseCtx := setupComponent(t, cfg)

	if _, err := comp.Publish(baseCtx, gcpubsub.Message{Data: []byte("hello")}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	recvCtx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	var attempts []int

	handler := func(ctx context.Context, msg *gcpubsub.Message) error {
		mu.Lock()
		defer mu.Unlock()
		attempts = append(attempts, msg.DeliveryAttempt)
		if len(attempts) == 1 {
			return errors.New("fail once")
		}
		cancel()
		return nil
	}

	err := comp.Receive(recvCtx, handler)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("receive: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(attempts) < 2 {
		t.Fatalf("expected redelivery, attempts=%d", len(attempts))
	}
	if len(attempts) >= 2 && attempts[1] <= attempts[0] {
		t.Logf("warning: delivery attempt did not increase: %v", attempts)
	}
}

func TestPublishContextCanceled(t *testing.T) {
	cfg := gcpubsub.Config{
		ProjectID:      "test-project",
		TopicID:        "test-topic",
		SubscriptionID: "test-sub",
	}
	comp, baseCtx := setupComponent(t, cfg)

	ctx, cancel := context.WithCancel(baseCtx)
	cancel()

	_, err := comp.Publish(ctx, gcpubsub.Message{Data: []byte("hello")})
	if err == nil {
		t.Fatalf("expected error when context canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandlerPanicIsRecovered(t *testing.T) {
	cfg := gcpubsub.Config{
		ProjectID:      "test-project",
		TopicID:        "test-topic",
		SubscriptionID: "test-sub",
	}
	comp, baseCtx := setupComponent(t, cfg)

	if _, err := comp.Publish(baseCtx, gcpubsub.Message{Data: []byte("hello")}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	recvCtx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	var attempts int
	panicOnce := true

	handler := func(ctx context.Context, msg *gcpubsub.Message) error {
		mu.Lock()
		attempts++
		mu.Unlock()

		if panicOnce {
			panicOnce = false
			panic("boom")
		}
		cancel()
		return nil
	}

	err := comp.Receive(recvCtx, handler)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("receive: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", attempts)
	}
}

func setupComponent(t *testing.T, cfg gcpubsub.Config) (*gcpubsub.Component, context.Context) {
	t.Helper()

	srv := pstest.NewServer()
	t.Cleanup(func() { _ = srv.Close() })

	cfg.EmulatorEndpoint = srv.Addr

	factory := func(ctx context.Context, projectID string, creds gcpubsub.Credentials, dial gcpubsub.DialOptions) (*pubsub.Client, error) {
		opts := []option.ClientOption{
			option.WithEndpoint(srv.Addr),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		}
		client, err := pubsub.NewClient(ctx, projectID, opts...)
		if err != nil {
			return nil, err
		}
		if cfg.TopicID != "" {
			if _, err := client.CreateTopic(ctx, cfg.TopicID); err != nil {
				return nil, err
			}
		}
		if cfg.SubscriptionID != "" {
			if cfg.TopicID == "" {
				return nil, errors.New("subscription requires topic")
			}
			topic := client.Topic(cfg.TopicID)
			if _, err := client.CreateSubscription(ctx, cfg.SubscriptionID, pubsub.SubscriptionConfig{Topic: topic}); err != nil {
				return nil, err
			}
		}
		return client, nil
	}

	deps := gcpubsub.Dependencies{
		Logger:        log.NewStdLogger(io.Discard),
		ClientFactory: factory,
		Dial:          gcpubsub.DialOptions{Insecure: true},
	}

	ctx := context.Background()
	comp, cleanup, err := gcpubsub.NewComponent(ctx, cfg, deps)
	if err != nil {
		t.Fatalf("new component: %v", err)
	}
	t.Cleanup(func() {
		cleanup()
	})
	return comp, ctx
}
