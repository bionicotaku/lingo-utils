# pkg/gclog —— Kratos × Google Cloud Logging 封装指引

本文档总结在 `pkg/gclog` 下实现 Google Cloud Logging 适配器的最佳实践，目标是：

1. **继续沿用 Kratos 统一的 `log.Logger` 接口**，业务仍可使用 `log.With`、`log.NewHelper` 等常规入口。
2. **保证 Cloud Logging 所需核心字段**（`time`、`level`、`msg`、`service`、`version`、`trace_id`、`span_id`）始终存在且位于约定位置。
3. **输出 Cloud Logging 兼容的 JSON** 到任意 `io.Writer`（stdout、文件等），既满足 Cloud Run / GKE 采集链路，也兼顾本地调试与文件落盘。

---

## 设计思路

| 目标字段 | 必填 | 来源说明 |
| -------- | ---- | -------- |
| `time`   | 是   | 由适配器自动写入，格式 RFC3339Nano / UTC |
| `level`  | 是   | Kratos `log.Level` → Cloud Logging `Severity` |
| `msg`    | 是   | `log.DefaultMessageKey`（Kratos Helper 默认键） |
| `service`| 是   | 初始化时通过 `Options.Service` 指定 |
| `version`| 是   | 初始化时通过 `Options.Version` 指定 |
| `component` | 否 | 通过 `Options.ComponentTypes` 注册的枚举值，表示子组件名称 |
| `instance_id` | 否 | 默认取 `os.Hostname()`，可通过 `Options.InstanceID` 覆盖或 `Options.DisableInstanceID` 关闭 |
| `trace_id` / `span_id` | 是 | `WithTrace(ctx, kv...)` 或从 OTel `SpanContext` 派生 |
| `payload` | 否 | 额外业务字段的字典（`map[string]any`），仅包含列表外的自定义键值 |

实现时建议拆分为三类组件（需引入 `fmt`、`os`、`sync` 等标准库，并依赖 `github.com/go-kratos/kratos/v2/log`）：

1. **构造层**  
   - `Options`：包含 `Service`、`Version`（必填）以及 `ComponentTypes map[string]struct{}`、`InstanceID`、`DisableInstanceID`、`Writer` 等可选项，统一在 `ValidateOptions` 中做严格校验；`Writer` 支持任意 `io.Writer`（stdout、文件、ring buffer 等）。  
   - `NewLogger(opts Options) (log.Logger, FlushFn, error)`：根据 `Options` 返回实现 Kratos 接口的 Logger，`FlushFn` 用于未来切换 Cloud Logging API 时执行 `logger.Flush()`。内部只接受 `log.DefaultMessageKey`、`trace_id`、`span_id`、`component`、`payload` 键，并将 `service`/`version` 写入 `ServiceContext`，`component`、`instance_id` 写入 `Labels`。  
   - Option Builder：如 `WithComponentTypes("gateway","catalog")`、`WithInstanceID(id)`、`DisableInstanceID()`、`WithWriter(io.Writer)`，避免外部重复拼 map。

2. **上下文与 Helper**  
   - `AppendTrace(ctx context.Context, projectID string, kvs []interface{}) []interface{}`：从 OTel `SpanContext` 生成符合 Cloud Logging 要求的 `projects/<project>/traces/<trace>`，用于 `log.With`。  
   - `WithTrace(ctx context.Context, projectID string, base log.Logger) log.Logger`：封装 `AppendTrace` + `log.WithContext`，方便直接得到带 trace 的 logger。  
   - `type Helper struct{ *log.Helper }`：在 `Helper` 上扩展 `InfoWithPayload(msg string, payload map[string]any, kvs ...interface{})`、`WithComponent(name string) *Helper` 等方法，保证所有扩展字段自动走 `payload` 并校验组件枚举。  
   - `RequestLogger(ctx, base log.Logger, component string, payload map[string]any, projectID string) (*Helper, error)`：组合常见模式（trace + component + payload），供 HTTP/gRPC middleware 一行注入。

3. **字段辅助**  
   - `WithComponent(logger log.Logger, component string) (log.Logger, error)`：校验枚举后附加 `component`。  
   - `WithPayload(logger log.Logger, payload map[string]any) (log.Logger, error)`：统一将自定义字段落在 `payload` key，下游 JSON 结构保持稳定。  
   - `SeverityFromHTTP(status int) log.Level`（可选）：把 HTTP 状态码映射到 Kratos 日志级别，便于中间件统一处理。  
   - **测试 Helper**：例如 `NewTestLogger()` 返回基于 `bytes.Buffer` 的 logger，配合断言函数 `AssertEntry(t, buf, want)` 确认输出字段；或提供 `StubTraceContext(traceID, spanID)` 生成固定 trace/span，方便单元测试不依赖真实 OTel。  

配合上述 API，业务侧无需关心 JSON 结构细节，只需：

```go
logger, flush, err := gclog.NewLogger(
    gclog.WithService("catalog-service"),
    gclog.WithVersion("2025.10.21"),
    gclog.WithComponentTypes("gateway", "catalog"),
)
if err != nil { log.Fatalf("init logger: %v", err) }
defer flush(context.Background())

ctxLogger := gclog.WithTrace(ctx, projectID, logger)
helper := gclog.NewHelper(ctxLogger).WithComponent("catalog")
helper.InfoWithPayload("video accepted", map[string]any{"video_id": id})
```

### Trace/Span 注入

提供一个辅助函数，把 OTel `SpanContext` 映射到 Cloud Logging 需要的格式（`projects/<project>/traces/<trace-id>`）：

```go
func AppendTrace(ctx context.Context, project string, kvs []interface{}) []interface{} {
    sc := trace.SpanContextFromContext(ctx)
    if sc.HasTraceID() {
        kvs = append(kvs, "trace_id", fmt.Sprintf("projects/%s/traces/%s", project, sc.TraceID()))
    }
    if sc.HasSpanID() {
        kvs = append(kvs, "span_id", sc.SpanID().String())
    }
    return kvs
}
```

在 Handler 中可配合 `log.WithContext(ctx, logger)` 与 `log.NewHelper` 调用：

```go
ctxLogger := log.With(logger, gclog.AppendTrace(ctx, projectID, nil)...)
helper := log.NewHelper(ctxLogger)
helper.Infow("request completed", "route", "/api/v1/videos")
```

### 快速开始与测试

```bash
cd pkg/gclog
go test ./...
```

在其他模块中引用：

```go
import "github.com/bionicotaku/lingo-utils-gclog"

logger, flush, err := gclog.NewLogger(
    gclog.WithService("catalog"),
    gclog.WithVersion("2025.10.21"),
)
if err != nil {
    panic(err)
}
defer flush(context.Background())

log.NewHelper(logger).Info("booted")
```

### 并发与生命周期

- `Logger` 内部写入已通过互斥锁串行化；若追加压缩或网络写入，可在外层包装新的 `io.Writer`。
- 若未来改用 `cloud.google.com/go/logging.Client`，记得在应用退出时调用 `logger.Flush()`，并通过 `Client.OnError` 记录上报失败。

---

## 最佳实践清单

1. **强制校验必填字段**：`service`、`version` 在初始化时校验；`msg` 无论如何保证非空；`trace_id`、`span_id` 自动兜底。  
2. **统一结构化命名**：建议所有字段使用下划线小写（`user_id`、`error_kind`）以便 Cloud Logging 查询。  
3. **区分环境 Writer**：
   - 生产：默认写往 `os.Stdout`（Cloud Run / GKE 自动采集）。  
   - 本地调试：可替换为 `bytes.Buffer`、`io.MultiWriter` 等，以满足测试或文件记录需求。  
4. **配合 Kratos Filter**：必要时使用 `log.NewFilter` 做采样 / 级别限制，或拦截敏感字段。  
5. **自定义字段统一放入 `payload`**：除表格内字段外不允许额外键名，业务扩展统一通过 `payload`（`map[string]any`）传递，未遵循将直接报错，避免日志结构漂移。  
6. **组件枚举管理**：通过 `Options.ComponentTypes` 提前注册合法值（例如 `gateway`、`catalog`），日志侧严格校验；未登记的组件会把状态写入 `payload.component_status`，辅助排查。  
7. **实例标识**：默认通过标签输出 `instance_id=os.Hostname()`，如需禁用或自定义主机标识，可通过 `Options.InstanceID` / `Options.DisableInstanceID` 调整。  
8. **版本字段**：统一通过 `ServiceContext.Version` 填充版本信息，结合服务名方便在 Cloud Logging 中筛选部署批次。  
9. **与 OTel 打通**：结合已有的 tracing middleware，把 span 信息注入日志，实现日志与 trace 的 cross-link。  
10. **Ops 告警**：在 GCP 端使用基于 `severity>=ERROR` 或关键字的 Rule，统一落地报警策略。

按照上述约束实现后，`pkg/gclog` 能在不侵入业务代码的情况下，满足 Google Cloud Logging 的格式要求，并保留 Kratos 生态的全部能力。*** End Patch
