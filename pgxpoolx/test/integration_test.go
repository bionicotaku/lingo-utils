package pgxpoolx_test

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/pgxpoolx"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/otel/metric/noop"
)

var (
	loadEnvOnce sync.Once
	loadEnvErr  error
)

func loadEnv(t *testing.T) {
	t.Helper()
	loadEnvOnce.Do(func() {
		loadEnvErr = godotenv.Load(".env")
	})
	if loadEnvErr != nil && !os.IsNotExist(loadEnvErr) {
		t.Fatalf("load env: %v", loadEnvErr)
	}
}

func databaseURL(t *testing.T) string {
	t.Helper()
	loadEnv(t)
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	return dsn
}

func TestNewComponentWithRealDatabase(t *testing.T) {
	dsn := databaseURL(t)
	cfg := pgxpoolx.Config{
		DSN:                dsn,
		Schema:             "public",
		HealthCheckTimeout: 10 * time.Second,
	}
	logger := log.NewStdLogger(io.Discard)
	comp, cleanup, err := pgxpoolx.ProvideComponent(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("ProvideComponent: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := comp.Pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestNewComponentWithMetricsEnabled(t *testing.T) {
	dsn := databaseURL(t)
	enable := true
	cfg := pgxpoolx.Config{
		DSN:                dsn,
		MetricsEnabled:     &enable,
		HealthCheckTimeout: 10 * time.Second,
	}
	deps := pgxpoolx.Dependencies{
		Logger: log.NewStdLogger(io.Discard),
		Meter:  noop.Meter{},
	}
	comp, cleanup, err := pgxpoolx.NewComponent(context.Background(), cfg, deps)
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := comp.Pool.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func TestProvideComponentInvalidHost(t *testing.T) {
	cfg := pgxpoolx.Config{DSN: "postgres://foo:bar@127.0.0.1:65000/postgres?connect_timeout=1"}
	logger := log.NewStdLogger(io.Discard)
	comp, cleanup, err := pgxpoolx.ProvideComponent(context.Background(), cfg, logger)
	if err == nil {
		cleanup()
		t.Fatal("expected error for unreachable host")
	}
	if comp != nil || cleanup != nil {
		t.Fatal("expected nil component on error")
	}
}
