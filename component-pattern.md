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
