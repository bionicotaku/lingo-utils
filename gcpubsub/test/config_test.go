package gcpubsub_test

import (
    "testing"
    "time"

    "github.com/bionicotaku/lingo-utils/gcpubsub"
)

func TestConfigNormalizeDefaults(t *testing.T) {
    cfg := gcpubsub.Config{}
    normalized := cfg.Normalize()

    if normalized.PublishTimeout <= 0 {
        t.Fatalf("expected positive publish timeout, got %v", normalized.PublishTimeout)
    }
    if !normalized.OrderingKeyEnabledValue() {
        t.Fatalf("expected ordering enabled by default")
    }
    if !normalized.LoggingEnabled() {
        t.Fatalf("expected logging enabled by default")
    }
    if !normalized.MetricsEnabled() {
        t.Fatalf("expected metrics enabled by default")
    }
    if normalized.Receive.NumGoroutines != 1 {
        t.Fatalf("expected default goroutines = 1, got %d", normalized.Receive.NumGoroutines)
    }
    if normalized.Receive.MaxOutstandingMessages <= 0 {
        t.Fatalf("expected positive max outstanding messages")
    }
}

func TestConfigNormalizeEmulatorDisablesExactlyOnce(t *testing.T) {
    cfg := gcpubsub.Config{
        EmulatorEndpoint:    "localhost:8085",
        ExactlyOnceDelivery: true,
    }
    normalized := cfg.Normalize()
    if normalized.ExactlyOnceDelivery {
        t.Fatalf("exactly once should be disabled when emulator endpoint is set")
    }
}

func TestConfigNormalizeCustomValues(t *testing.T) {
    timeout := 5 * time.Second
    logging := false
    metrics := false
    ordering := false
    cfg := gcpubsub.Config{
        PublishTimeout:     timeout,
        EnableLogging:      &logging,
        EnableMetrics:      &metrics,
        OrderingKeyEnabled: &ordering,
        Receive: gcpubsub.ReceiveConfig{
            NumGoroutines:          2,
            MaxOutstandingMessages: 10,
            MaxOutstandingBytes:    1024,
            MaxExtension:           time.Second,
            MaxExtensionPeriod:     2 * time.Second,
        },
    }

    normalized := cfg.Normalize()
    if normalized.PublishTimeout != timeout {
        t.Fatalf("unexpected timeout: %v", normalized.PublishTimeout)
    }
    if normalized.LoggingEnabled() {
        t.Fatalf("logging should remain disabled")
    }
    if normalized.MetricsEnabled() {
        t.Fatalf("metrics should remain disabled")
    }
    if normalized.OrderingKeyEnabledValue() {
        t.Fatalf("ordering should remain disabled")
    }
    if normalized.Receive.NumGoroutines != 2 {
        t.Fatalf("expected goroutines = 2, got %d", normalized.Receive.NumGoroutines)
    }
}
