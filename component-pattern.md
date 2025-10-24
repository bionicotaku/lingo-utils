# lingo-utils 组件模式指引

> 本文总结 `lingo-utils/gclog` 与 `lingo-utils/observability` 采用的组件化注入模式，梳理配置、Provider、Wire 集成的完整流程，为后续新增共享组件提供统一范式。

---

## 1. 模式概览

两个组件都遵循「**Config → Component → ProviderSet → Typed Output**」的结构，通过 Google Wire 将能力注入到各服务。

核心要素：

- **Config/ServiceInfo**：记录运行时元信息（服务名、版本、环境等），通常由 `internal/infrastructure/config_loader` 根据 Bootstrap 配置与环境变量推导。
- **Component**：封装实际初始化结果，并返回 `cleanup func()` 统一释放资源。
- **ProviderSet**：对外暴露 `NewComponent` 及派生能力（日志器、指标配置等），供 Wire 在依赖图中使用。
- **Typed Output**：通过专门类型（如 `log.Logger`、`*MetricsConfig`）明确依赖关系，避免多出口冲突。

此模式的目标是：**在服务装配期就完成依赖注入和生命周期管理**，业务层只消费已经准备好的能力。

---

## 2. gclog 组件

路径：`lingo-utils/gclog/provider.go`

### 2.1 Config

```go
type Config struct {
    Service, Version, Environment string
    InstanceID                    string
    StaticLabels                  map[string]string
    EnableSourceLocation          bool
}
```

`config_loader.ServiceMetadata.LoggerConfig()` 会从服务元信息拼装此结构（`kratos-template/internal/infrastructure/config_loader/loader.go:80-94`），确保所有服务统一填充日志上下文。

### 2.2 Component

```go
func NewComponent(cfg Config) (*Component, func(), error)
```

- 内部调用 `NewLogger` 构建 GCP JSON 结构化日志。
- 使用 `log.With` 注入 Trace/Span 自动字段。
- 返回 `cleanup`（当前为空实现，保留扩展点）。

### 2.3 ProviderSet

```go
var ProviderSet = wire.NewSet(NewComponent, ProvideLogger, ProvideHelper)
```

对外暴露：

- `ProvideLogger(*Component) log.Logger`
- `ProvideHelper(*Component) *log.Helper`

服务在 Wire 中引用 `gclog.ProviderSet` 即可注入结构化日志能力。

---

## 3. observability 组件

路径：`lingo-utils/observability/provider.go`

### 3.1 ServiceInfo + ObservabilityConfig

- `ServiceInfo`（Name/Version/Environment）同样由 `config_loader` 推导（`ServiceMetadata.ObservabilityInfo()`）。
- `ObservabilityConfig` 来自 Bootstrap 配置，包含追踪/指标出口、启用开关等。

### 3.2 Component

```go
func NewComponent(ctx context.Context, cfg ObservabilityConfig, info ServiceInfo, logger log.Logger) (*Component, func(), error)
```

- 调用 `Init` 注册 OpenTelemetry Tracer/Meter Provider。
- 保存 `shutdown` 回调，并在 `cleanup` 中应用 5 秒超时。
- 暴露 `Shutdown(ctx)` 方便应用提前刷新。

### 3.3 ProviderSet

```go
var ProviderSet = wire.NewSet(NewComponent, ProvideMetricsConfig)
```

除组件外还提供：

- `ProvideMetricsConfig(cfg ObservabilityConfig) *MetricsConfig`：供 gRPC Server/Client 中决定是否启用指标采集。

---

## 4. 集成流程对比

| 步骤 | 日志（gclog） | 可观测性（observability） |
| ---- | ------------- | ------------------------- |
| 配置来源 | `config_loader` 派生 `gclog.Config` | `Bootstrap.Observability` + `ServiceMetadata` |
| Wire Provider | `gclog.ProviderSet` | `observability.ProviderSet` |
| 返回能力 | `log.Logger`, `*log.Helper` | `*observability.Component`, `*MetricsConfig` |
| 清理策略 | 当前空实现（预留扩展） | `cleanup` 自动触发 `shutdown` |
| 在服务中的挂载 | `grpc_server.NewGRPCServer`、业务层 `log.Helper` 注入 | `grpc_server` 指标、`grpc_client` 指标、应用退出时 `component.Shutdown` |

两者均保持：**配置集中、生命周期集中、对外暴露少量稳定依赖**。

---

## 5. 使用建议

1. **新增组件复用模式**  
   - 若需要对外暴露多种能力，务必以 Component 聚合并通过 ProviderSet 输出 typed 结果，避免直接在 ProviderSet 中返回多个相同类型的匿名函数。

2. **配置 → Provider 分层**  
   - 把配置解析放在 `config_loader` 或对应 `ProvideXXXConfig` 中统一完成，组件包只接收结构化好的配置。

3. **生命周期管理**  
   - 即使当前没有清理动作，也建议保留 `cleanup func()`，方便未来拓展（如刷新缓存、关闭连接）。

4. **错误处理**  
   - 保留 `error` 返回值而非 `panic`，让 Wire 统一处理初始化失败。

5. **文档与示例**  
   - 在组件 README 中展示 Wire 整合示例（如 `gclog/README.md`），并指出配置结构、注入位置，降低接入成本。

---

## 6. 下一步

针对 `lingo-utils/gcjwt` 等新组件，推荐沿用此模式：

- 定义 `ClientMiddleware` / `ServerMiddleware` 等命名类型，避免接口冲突。
- 提供 `NewComponent` 聚合客户端、服务端中间件及 cleanup。
- 在 `config_loader` 承担配置映射，保证服务写法统一。

这样可以让整个仓库的依赖注入路径保持一致，减少重复学习和维护成本。***

---

## TxManager 构建 TODO（2025-10-24）

1. 组件落地  
   - [x] 在 `lingo-utils/txmanager` 新建模块，初始化 `go.mod` 并加入 `go.work`。  
   - [x] 实现 `Config`/`Option`/`TxOptions` 及默认别名（`Default`/`Serializable`/`ReadOnly`）。  
   - [x] 编写 `manager.go`：定义 `Manager`/`Session`，实现 `WithinTx`、`WithinReadOnlyTx`、`ErrRetryableTx`、panic/rollback 处理。  
   - [x] 实现 `metrics.go`：集成 OTel 指标（`db.tx.duration`、`active`、`retries`、`failures`），支持配置开关。  
   - [x] 集成 `log.Logger`：使用 `log.Helper` 记录事务错误、重试、超时。  
   - [x] 输出 `component.go` + `ProviderSet`，接收 `Config`、`*pgxpool.Pool`、`log.Logger`，返回 `Manager` 与 cleanup。  
   - [x] 编写 `README.md`，包含配置示例、Wire 集成、Service/Repository 使用示例。

2. 配置与注入  
   - [ ] 更新 `kratos-template/internal/infrastructure/config_loader`，映射 `txmanager.Config`（默认超时、隔离级别、锁超时、指标开关）。  
   - [ ] 在各服务 Wire 中引入 `txmanager.ProviderSet`，确保 `gclog`、`observability`、`pgxpool` Provider 在其前。  
   - [ ] 调整服务启动流程，注册 `txmanager` cleanup。

3. Repository / Service 改造  
   - [ ] 修改 catalog 仓储接口签名：写操作接收 `txmanager.Session`，只读复杂查询走 `WithinReadOnlyTx`。  
   - [ ] Service 使用 `Manager.WithinTx` / `WithinReadOnlyTx`，移除直接操作 `sqlc.Queries` 的写路径。  
   - [ ] 更新 Outbox/Inbox 任务及相关服务逻辑，确保写入均在事务中执行。  
   - [ ] 必要时新增仓储接口（例如 `GetForUpdate(ctx, sess)`）。

4. 指标与日志验证  
   - [ ] 使用 `observability` stdout exporter 验证 OTel 指标与 span 属性。  
   - [ ] 检查 gclog 输出，确认 `tx_name`、`sql_state`、`elapsed_ms` 等字段。  
   - [ ] 添加集成测试模拟死锁/超时，验证 `ErrRetryableTx` 与指标数据。

5. QA & 文档  
   - [ ] 更新 `docs/txmanager-design.md` 状态与实施结果。  
   - [ ] 在服务 README 或运行指南中补充 TxManager 使用说明与配置项。  
   - [ ] 执行 `sqlc generate`、`make lint`、`make test` 确认无回归。

6. 后续推广  
   - [ ] 评估其他微服务（feed/progress/media 等）迁移排期。  
   - [ ] 规划 Savepoint 扩展需求与只读事务 lint 检查。
