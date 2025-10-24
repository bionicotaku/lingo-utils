# txmanager – Transaction Management Component for Kratos Services

`txmanager` bundles best practices for managing PostgreSQL transactions in Kratos
services. It exposes a `Manager` interface that enforces explicit transaction
boundaries, integrates with Google Cloud Logging via Kratos `log.Logger`, and
emits OpenTelemetry metrics compatible with the existing
`lingo-utils/observability` package.

## Features

- `WithinTx` / `WithinReadOnlyTx` helpers with consistent timeout, isolation and
  error handling semantics.
- Automatic classification of retryable errors (deadlocks, serialization
  failures, lock timeouts) with `ErrRetryableTx` sentinel.
- Structured logs enriched with trace/span identifiers when used with
  `lingo-utils/gclog`.
- Metrics (`db.tx.duration`, `db.tx.active`, `db.tx.retries`,
  `db.tx.failures`) emitted via OpenTelemetry.
- Wire-compatible component for painless dependency injection across services.

## Quick Start

```go
import (
    txmanager "github.com/bionicotaku/lingo-utils/txmanager"
)

// Wire set
var ProviderSet = wire.NewSet(
    config.ProviderSet,          // service specific config loader
    database.ProviderSet,        // supplies *pgxpool.Pool
    gclog.ProviderSet,           // structured logger
    observability.ProviderSet,   // installs OTel providers
    txmanager.ProviderSet,       // exposes txmanager.Manager
)

// Service usage
func (s *VideoService) Publish(ctx context.Context, cmd PublishCommand) error {
    return s.txManager.WithinTx(ctx, txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
        q := catalogsql.New(sess.Tx())
        if err := q.UpdateVideoStatus(ctx, catalogsql.UpdateVideoStatusParams{...}); err != nil {
            return err
        }
        return s.outboxRepo.Enqueue(ctx, sess.Tx(), event)
    })
}
```

Configuration snippet:

```yaml
data:
  postgres:
    tx:
      defaultIsolation: read_committed
      defaultTimeout: 3s
      lockTimeout: 1s
      maxRetries: 3
      metricsEnabled: true
```

## Customising Dependencies

`NewManager` accepts a `txmanager.Dependencies` struct for advanced overrides
(custom tracer/meter/clock或强制关闭指标)。常规服务通过组件自动注入，**无需**手动设置；
在测试中可直接调用：

```go
import "time"

fakeClock := func() time.Time { return fixed }
deps := txmanager.Dependencies{Clock: fakeClock}
m, err := txmanager.NewManager(pool, cfg, deps)
```

## Metrics Reference

| Name | Type | Attributes |
| ---- | ---- | ---------- |
| `db.tx.duration` | Histogram (ms) | `tx.method`, `tx.isolation`, `tx.retryable` |
| `db.tx.active` | UpDownCounter | `tx.method`, `tx.isolation` |
| `db.tx.retries` | Counter | `tx.method`, `tx.isolation`, `tx.retryable=true` |
| `db.tx.failures` | Counter | `tx.method`, `tx.isolation`, `tx.retryable` |

## Error Handling

- Retryable errors wrap the original cause with `ErrRetryableTx`. Use
  `txmanager.IsRetryable(err)` to decide whether to retry.
- Non-retryable errors keep the original cause intact for precise Problem
  Details mapping at the controller layer.
- Panics inside the transactional function are rethrown after safely rolling
  back the transaction; logs and metrics capture the failure.

## Testing Tips

- Inject a deterministic clock via `Dependencies{Clock: fakeNow}` to assert metrics values.
- Use `pgxmock` or Testcontainers with Supabase configuration to exercise
  deadlock/serialization retry paths.
- Validate metrics output using the `observability` stdout exporter in local
  runs.
