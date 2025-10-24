# TxManager 设计方案

> 目标：在 Kratos 模板项目中提供一个统一、可观测、可测试的事务管理器，实现 Service 层的事务边界控制，并支撑 Outbox/Inbox 等关键场景。

## 1. 背景与问题

- 当前 Service 调用 Repository 时直接使用 `pgxpool.Pool` 或全局 `sqlc.Queries`，难以保障多表写入的一致性。
- Outbox/Inbox、幂等写接口需要“业务表 + outbox”同事务完成，否则会出现事件丢失或重复。
- 缺少统一的事务超时、错误分类与重试提示，定位死锁或长事务问题困难。

## 2. 设计目标

1. **显式事务边界**：Service 层通过闭包开启/提交事务，Repository 不感知事务生命周期。
2. **分层解耦**：`controllers → services → repositories` 依赖方向不变，Service 仅依赖接口抽象。
3. **安全可靠**：自动处理 `Rollback`、`Commit` 错误，捕获 panic，支持重试信号。
4. **可观测性**：统一采集事务耗时、重试次数、SQLSTATE，输出 OTel Span 与 slog。
5. **可配置**：支持超时、隔离级别、读/写模式、重试策略；默认兼容 Supabase Postgres。

## 3. 组件划分

TxManager 作为共享能力封装在 `lingo-utils/txmanager` 模块中，通过 Wire 与其他组件（`gclog`、`observability`）一同注入到各微服务。

```
lingo-utils/txmanager/
  ├── component.go       # Config → Component → ProviderSet
  ├── manager.go         # Manager/Session 接口与实现（WithinTx/WithinReadOnlyTx）
  ├── dependencies.go    # Dependencies、TxOptions 默认协作者
  ├── metrics.go         # OTel 指标埋点
  ├── errors.go          # ErrRetryableTx 等哨兵错误
  └── README.md          # 使用示例与 Wire 集成
```

服务侧仍按 MVC 分层组织，差异在于依赖注入方式：

- **Infrastructure**：通过 Wire 引入 `txmanager.Component`，同时提供服务自有 `pgxpool.Pool`。
- **Services**：持有 `txmanager.Manager` 接口处理用例事务。
- **Repositories**：接收 `txmanager.Session` 执行 sqlc 查询。

核心接口保持不变：

```go
type Manager interface {
    WithinTx(ctx context.Context, opts TxOptions, fn func(ctx context.Context, s Session) error) error
    WithinReadOnlyTx(ctx context.Context, opts TxOptions, fn func(ctx context.Context, s Session) error) error
}
```

`Session` 封装 `pgx.Tx` 与基于事务的 `sqlc.Queries`，避免 Service 直接依赖实现细节。

### 3.1 Config

```go
type Config struct {
    DefaultIsolation string        // read_committed / serializable
    DefaultTimeout   time.Duration // 事务超时
    LockTimeout      time.Duration // SET LOCAL lock_timeout
    MaxRetries       int           // ErrRetryableTx 提示值
    MeterName        string        // 默认为 "lingo-utils.txmanager"
    MetricsEnabled   bool          // 允许关闭指标上报
}
```

- `config_loader` 负责从服务配置映射成 `Config`，结合 Supabase 环境变量生成默认值。
- 在 `txmanager` 组件内部，通过 `Dependencies` 结构注入日志、Meter、Tracer、Clock 等协作者；缺省值由组件自动提供。

### 3.2 ProviderSet

```
// component.go
var ProviderSet = wire.NewSet(
    NewComponent,
    ProvideManager,
)
```

- `NewComponent(cfg Config, pool *pgxpool.Pool, logger log.Logger)` 负责构造 `managerImpl`。
- 组件默认从 `otel.GetMeterProvider()` 获取 Meter；当 `observability` 未启用时回退到 no-op。
- `ProvideManager` 将 `Component.Manager` 暴露给 Service 层，类型为接口以便测试替换。

### 3.3 服务侧集成

```
var ProviderSet = wire.NewSet(
    config.ProviderSet,
    gclog.ProviderSet,
    observability.ProviderSet,
    database.ProviderSet,     // 提供 *pgxpool.Pool
    txmanager.ProviderSet,    // 提供 txmanager.Manager
    services.ProviderSet,
)
```

- 保证 `gclog` 与 `observability` 在 `txmanager` 之前初始化，以便日志/指标能力就绪。
- 业务单元测试可通过 `NewNoopManager()` 或手写 stub 注入替代实现。

## 4. 事务选项（TxOptions）

| 字段             | 说明                                      | 默认值            |
| ---------------- | ----------------------------------------- | ----------------- |
| `Isolation`      | 事务隔离级别 (`pgx.TxIsoLevel`)           | `ReadCommitted`   |
| `AccessMode`     | 读写模式（读/写）                         | `pgx.ReadWrite`   |
| `Timeout`        | 单次事务超时时间                          | 3s（可配置）      |
| `LockTimeout`    | 可选，设置 `SET LOCAL lock_timeout`       | 1s（可配置）      |
| `TraceName`      | Span 名称附加后缀                         | 自动推导          |
| `MaxRetries`     | 上层配置的最大重试次数（仅用于幂等场景） | 0（不重试）       |

> 如果外层 ctx 已包含更短的 deadline，则以外层为准；若 `Timeout` 更短则缩短。

常用别名：

- `txopts.Default()`：写事务（ReadCommitted + ReadWrite + default timeout）
- `txopts.Serializable()`：串行化写事务
- `txopts.ReadOnly()`：只读事务（ReadCommitted + ReadOnly）

## 5. WithinTx 与 WithinReadOnlyTx 执行流程

```
WithinTx(ctx, opts, fn)
  ├─ ctx = withTimeout(ctx, opts.Timeout)
  ├─ span = tracer.Start(ctx, "db.tx.<service>.<usecase>")
  ├─ conn.BeginTx(ctx, pgx.TxOptions)
  ├─ defer safeRollback(tx, span)
  ├─ session := newTxSession(tx, sqlcFactory.WithTx(tx))
  ├─ err = fn(ctxWithTx, session)
  ├─ if err != nil → return wrap(err)
  ├─ err = tx.Commit(ctx)
  ├─ if err != nil → classifyPgError(err) → return
  └─ span.End()
```

- `WithinReadOnlyTx` 执行步骤与上方一致，但会强制：
  - 将 `opts.AccessMode` 置为 `pgx.ReadOnly`；
  - 禁止 `TxSession` 暴露写操作（只提供 `Queries()` 的只读调用或在 Repository 内断言）；
  - Span 名称以 `db.tx.readonly` 前缀，默认不计入 Outbox/InBox 指标。

- `safeRollback`：处理 panic/错误路径；忽略 `pgx.ErrTxClosed`。
- `classifyPgError`：使用 `errors.As(err, *pgconn.PgError)` 判定 SQLSTATE，并根据 `SafeToRetry` 决定是否返回 `ErrRetryableTx`。
- `ctxWithTx`：可在 `context` 中注入 trace 信息，但不直接存放事务句柄。

## 6. Repository 调用方式

- Repository 接口示例：

```go
type VideoRepository interface {
    Save(ctx context.Context, sess txmanager.Session, video po.Video) error
    ListReady(ctx context.Context, sess txmanager.Session) ([]po.Video, error)
}
// 假设仓储层使用 import 别名：
// txmanager "github.com/bionicotaku/lingo-utils/txmanager"
```

- 实现内部通过 `sess.Queries()` 获取基于事务的 `*catalogsql.Queries`，确保所有 SQL 均在同一事务内。
- 对多表 JOIN 或需要一致性的只读查询，推荐通过 `WithinReadOnlyTx` 获得只读 `Session`；简单单表读可继续使用注入的非事务 `Queries`。
- Outbox/Inbox Repository 在方法醒目位置注明“必须在事务内调用”，以避免误用。

## 7. Service 用例示例

```go
func (s *VideoService) Publish(ctx context.Context, cmd PublishCommand) error {
    return s.txManager.WithinTx(ctx, txopts.Default(), func(ctx context.Context, sess txmanager.Session) error {
        video, err := s.videoRepo.GetForUpdate(ctx, sess, cmd.VideoID)
        if err != nil { return err }

        if err := s.videoRepo.MarkPublished(ctx, sess, video); err != nil { return err }

        event := buildVideoPublishedEvent(video)
        if err := s.outboxRepo.Enqueue(ctx, sess, event); err != nil { return err }

        return nil
    })
}

func (s *VideoService) ListReadyVideos(ctx context.Context, query ListReadyQuery) ([]po.Video, error) {
    var result []po.Video
    err := s.txManager.WithinReadOnlyTx(ctx, txopts.ReadOnly(), func(ctx context.Context, sess txmanager.Session) error {
        videos, err := s.videoRepo.ListReady(ctx, sess)
        if err != nil {
            return err
        }
        result = videos
        return nil
    })
    return result, err
}
```

## 8. 错误处理策略

- **分类**：  
  - `ErrRetryableTx`：SQLSTATE `40001`（序列化失败）、`40P01`（死锁）、`55P03`（锁等待超时）→ Service 可指数退避重试。  
  - `ErrConflict`：唯一约束/外键冲突，映射到 Problem Details。  
  - 其他错误原样返回，但附带 `trace_id`、`span_id` 便于排查。
- **panic**：`WithinTx` 捕获后 rollback，并将 panic 重新抛出，保障调用者能观察到。
- **日志**：借助 `gclog` 注入的 `log.Logger` 与 `log.Helper` 输出结构化日志，仅在事务失败、重试、超时时打印；关键字段包括 `tx_name`、`isolation`、`sql_state`、`error_kind`、`elapsed_ms`，Trace/Span 信息由 `gclog` 自动填充。

## 9. 可观测性与指标

| 指标                     | 类型           | 说明                                                        |
| ------------------------ | -------------- | ----------------------------------------------------------- |
| `db.tx.duration`         | Histogram      | 事务耗时（毫秒），按 `method`、`isolation`、`retryable` 细分 |
| `db.tx.active`           | UpDownCounter  | 当前活跃事务数                                              |
| `db.tx.retries`          | Counter        | 命中 `ErrRetryableTx` 次数                                  |
| `db.tx.failures`         | Counter        | 非重试类失败次数（含超时、上下文取消）                      |

- 指标通过 `otel.GetMeterProvider()` 获取的 Meter 上报；若 `observability` 未启用，则自动降级为 no-op。
- Span 属性：`db.system=postgresql`、`db.name=<schema>`、`db.tx.isolation`、`db.tx.retryable`、`tx.component=txmanager`。
- 将 Outbox `delivery_attempts`、Inbox `event_id` 写入 span/event，便于与日志对齐排查。
- 推荐在服务 Wire 中同时引入 `gclog.ProviderSet`（提供 `log.Logger`）与 `observability.ProviderSet`（安装 Meter/Tracer），确保 TxManager 的日志/指标能力完整生效。

## 10. 配置项

```yaml
data:
  postgres:
    dsn: ${PG_DSN}
    max_conns: 20
    tx:
      timeout: 3s
      lock_timeout: 1s
      isolation: read_committed
      max_retries: 3
```

## 11. 测试策略

1. **单元测试**（pgxmock）：验证 `BeginTx` / `Commit` 调用顺序、rollback 覆盖 panic。  
2. **集成测试**（Supabase Dev / testcontainers）：  
   - 成功事务：视频入库 + Outbox 写入。  
   - 回滚：回调返回错误 → Outbox 无记录。  
   - 死锁模拟：两个事务 `SELECT ... FOR UPDATE` 冲突 → 返回 `ErrRetryableTx`。  
   - 超时：`pg_sleep` 超过 timeout → `context.DeadlineExceeded`。  
3. **负载测试**：并发 100 次事务，观察连接池是否耗尽、监控指标是否合理。

## 12. 迁移步骤

1. 在 `lingo-utils/txmanager` 中实现组件、Dependencies、指标，遵循组件模式。  
2. 更新各服务 `go.work` / `go.mod`，引用新模块并在 Wire 中引入 `txmanager.ProviderSet`。  
3. 调整 Repository 签名，确保写操作接收 `txmanager.Session`，只读查询按需走 `WithinReadOnlyTx`。  
4. Service 落地 `WithinTx` / `WithinReadOnlyTx` 模式，先覆盖 catalog 核心用例。  
5. 编写 Outbox/Inbox 集成测试，验证事务、重试、指标；在本地 Supabase 执行。  
6. 通过 `observability` 验证指标输出与 span 属性，结合 gclog 检查日志字段。  
7. 评估其它服务（feed/progress 等）迁移计划，并更新服务文档。

## 13. 风险与对策

| 风险                     | 对策                                                   |
| ------------------------ | ------------------------------------------------------ |
| 嵌套事务导致重复 `Begin` | 在 `WithinTx` 中检测 ctx 是否已有 session，禁止嵌套或复用 |
| 误用非事务 Queries       | Service 不注入 `*catalogsql.Queries`，统一经 Repository 申请 |
| Supabase 权限限制        | 启动时运行健康检查，验证隔离级别/`lock_timeout` 设置     |
| 长事务占满连接           | 通过指标监控，结合 `timeout` 与业务拆分                 |
| 组件初始化顺序错误       | Wire 中将 `gclog`、`observability`、`pgxpool` provider 放在 `txmanager` 之前 |
| 指标开销无法接受         | 通过 `Config.MetricsEnabled` 控制，或调整 histogram 精度  |

## 14. 开放问题与决策

1. **WithinReadOnlyTx**：已确定需要实现，为多表 JOIN 但无写操作的场景提供只读事务能力，确保一致性同时避免误写。
2. **Savepoint 支持**：当前无强需求，暂不实现；文档保留该扩展点，待出现批量部分失败或复杂重试场景时再评估。
3. **公共包化**：后续视其他服务复用情况，评估是否抽象至 `pkg/txmanager`。

---

本方案落地后，TxManager 将作为 `lingo-utils` 组件统一提供给各服务：Catalog 等业务上下文在 Wire 中一次性注入 `gclog`、`observability`、`txmanager`，即可获得结构化日志、OTel 指标与一致性事务管理能力。实施前请评审开放问题，确认配置项与 Supabase 权限。 
