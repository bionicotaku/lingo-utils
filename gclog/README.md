# gclog – Kratos Logger Aligned with Google Cloud Logging

`gclog` 提供一套与 **Google Cloud Logging** 模型完全对齐的日志适配器，同时继续复用 Kratos 的 `log.Logger` 抽象。使用它可以：

1. **无侵入切换输出**：业务代码仍然调用 `log.With`、`log.NewHelper`、Kratos logging middleware；底层统一输出 Cloud Logging 兼容的 JSON 或对接未来的 Cloud Logging API。
2. **保证核心字段完整**：自动填充 `timestamp`、`severity`、`message`、`serviceContext`、`trace`/`spanId`、`labels`、`jsonPayload` 等字段，便于在 Cloud Logging / Error Reporting / Trace 控制台直接检索和聚合。
3. **封装常用辅助**：提供追加 trace、caller、请求 ID、用户 ID、HTTP 请求摘要等 helper，并附带测试工具 `NewTestLogger`、`StubTraceContext` 等，方便单元测试校验日志内容。

---

## 字段映射

| Cloud Logging 字段 | 是否必填 | 说明/来源 |
|--------------------|----------|-----------|
| `timestamp`        | ✅       | `time.Now().UTC().Format(time.RFC3339Nano)` |
| `severity`         | ✅       | Kratos `log.Level` 映射为 `DEBUG/INFO/WARNING/ERROR/CRITICAL` 等 |
| `message`          | ✅       | Kratos 默认消息键 `log.DefaultMessageKey` |
| `serviceContext`   | ✅       | `Options.Service`、`Options.Version`，可扩展 `environment` |
| `trace`            | 可选    | 若设置 `Options.ProjectID` + OTel SpanContext，则输出 `projects/<project>/traces/<trace-id>` |
| `spanId`           | 可选    | 来自 OTel SpanContext |
| `labels`           | 可选    | `caller`、`instance_id`、`request_id`、`user_id`、`env` 等维度信息 |
| `httpRequest`      | 可选    | HTTP 摘要（方法、URL、状态、时延、UA 等），由 helper/middleware 填充 |
| `sourceLocation`   | 可选    | 源码位置（文件/行号/函数），可通过 `EnableSourceLocation` 自动收集 |
| `jsonPayload`      | 可选    | `payload` 字段承载业务自定义数据 |

---

## 模块结构与 API

### 1. 构造层

```go
logger, flush, err := gclog.NewLogger(
    gclog.WithService("catalog-service"),
    gclog.WithVersion("2025.10.21"),
    gclog.WithProjectID("my-gcp-project"),
    gclog.WithEnvironment("prod"),
    gclog.WithInstanceID("instance-001"),
    gclog.WithStaticLabels(map[string]string{"team": "growth"}),
    gclog.EnableSourceLocation(),
)
if err != nil {
    panic(err)
}
defer flush(context.Background())
```

**Options & Option Builders**
- `WithService(string)` / `WithVersion(string)`：必填字段，映射到 `serviceContext`。
- `WithProjectID(string)`：用于构建 Cloud Logging `trace` 字段；若部署在 GCP，务必提供项目 ID 才能在 Cloud Trace 控制台串联完整链路。
- `WithEnvironment(string)`：写入 `serviceContext.environment` 或 `labels.env`，便于跨环境筛选。
- `WithStaticLabels(map[string]string)`：预置自定义标签，如 `team`、`region`。
- `WithInstanceID(string)` / `DisableInstanceID()`：控制 `labels.instance_id`。
- `WithWriter(io.Writer)`：输出目标（默认 stdout，可换文件/缓冲区）。
- `EnableSourceLocation()`：基于 `runtime.Caller` 自动填充 `sourceLocation`。
- `WithFlushFunc(gclog.FlushFunc)`：覆写 flush 行为，接入 `cloud.google.com/go/logging` 时可传入 `logger.Flush`/`client.Close`。
- `WithAllowedKeys(keys ...string)`：注册额外允许的字段，字段值会直接合并进 `jsonPayload` 顶层。

> ⚠️ **字段约束**：`gclog` 默认只接受核心字段（message/trace/span/caller/payload/labels/http_request/error）。如需输出自定义键，必须通过 helper（`WithPayload`/`WithLabels` 等）或 `WithAllowedKeys` 显式注册，否则会返回错误，避免出现与 Cloud Logging 不兼容的结构。

### 2. 上下文与 Helper

| Helper                               | 说明 |
| Helper | 说明 |
| --- | --- |
| `AppendTrace(ctx, projectID, kvs)` | 将 OTel SpanContext 转换为 Cloud Logging `trace`/`spanId` 字段 |
| `AppendLabels(kvs, map[string]string)` | 在 kv 列表上追加 labels，便于 `logging.WithFields` 使用 |
| `WithTrace(ctx, projectID, logger)` | 创建带 trace 元数据的新 logger，并保留原 context |
| `WithCaller(logger, caller)` | 追加 `caller` 标签（如 `pkg.Func:line`） |
| `WithLabels(logger, map[string]string)` | 批量追加标签（team、region 等） |
| `WithRequestID` / `WithUser` | 快速写入 `request_id`、`user_id` 标签 |
| `WithPayload(logger, payload)` | 将业务对象放入 `jsonPayload.payload` |
| `WithStatus(logger, status)` | 将业务状态写入 payload（与 `WithPayload` 可叠加） |
| `WithError(logger, error)` | 将错误信息结构化输出到 `jsonPayload.error` |
| `WithHTTPRequest(logger, req, status, latency)` | 写入 Cloud Logging `httpRequest` 结构（方法、URL、状态、耗时、UA 等） |
| `HTTPRequestResponseSize(bytes)` / `HTTPRequestServerIP(ip)` / `HTTPRequestCacheStatus(lookup, hit, validated)` | 配合 `WithHTTPRequest` 丰富响应体大小、服务端 IP、缓存命中信息 |
| `SeverityFromHTTP(status)` | HTTP 状态码与 Kratos 日志级别映射 |
| `type Helper struct{ *log.Helper }` | 扩展 Kratos Helper：`InfoWithPayload`、`WithCaller`、`WithPayload` 等 |
| `RequestLogger(ctx, base, projectID, caller, labels, payload)` | 常见组合（trace + caller + labels + payload），可直接用于 middleware |

### 3. 测试工具

- `NewTestLogger(opts ...Option) (log.Logger, *bytes.Buffer, FlushFn, error)`：返回基于内存缓冲的 logger，配合单测断言输出。
- `StubTraceContext(ctx, traceID, spanID)`：生成固定 trace/span，避免依赖真实 OTel tracer。
- `DecodeEntries(buffer)`（建议扩展）：将多条日志解析为 `[]Entry`，便于批量断言。

---

## 中间件接入示例

### gRPC Server
```go
srv := grpc.NewServer(
    grpc.Middleware(
        recovery.Recovery(),
        tracing.Server(tracing.WithTracerProvider(tp)),
        logging.Server(
            logging.WithLogger(logger),
            logging.WithFields(func(ctx context.Context) map[string]interface{} {
                fields := gclog.AppendTrace(ctx, projectID, nil)
                if rid := requestid.FromContext(ctx); rid != "" {
                    fields = gclog.AppendLabels(fields, map[string]string{"request_id": rid})
                }
                return gclog.LabelsFromKVs(fields)
            }),
        ),
    ),
)
```

### gRPC Client
```go
conn, err := grpc.DialInsecure(
    ctx,
    grpc.WithEndpoint("127.0.0.1:9000"),
    grpc.WithMiddleware(
        logging.Client(
            logging.WithLogger(logger),
            logging.WithFields(func(ctx context.Context) map[string]interface{} {
                return gclog.LabelsFromKVs(gclog.AppendTrace(ctx, projectID, nil))
            }),
        ),
    ),
)
```

### HTTP Server/Client
HTTP 中可配合 `WithHTTPRequest` 写入 Cloud Logging 的 `httpRequest`，并仅记录必要摘要，避免采集请求体；若需要补充响应体大小、Server IP 等，可追加对应的 `HTTPRequestOption`。

```go
func httpLoggingFields(ctx context.Context) map[string]interface{} {
    fields := gclog.AppendTrace(ctx, projectID, nil)
    if info, ok := transport.FromServerContext(ctx).(*http.Context); ok {
        fields = append(fields,
            "caller", info.Route(),
        )
    }
    return gclog.LabelsFromKVs(fields)
}
```

```go
helper := gclog.NewHelper(
    gclog.WithHTTPRequest(
        baseLogger,
        req,
        status,
        latency,
        gclog.HTTPRequestResponseSize(respBytes),
        gclog.HTTPRequestServerIP(serverIP),
    ),
)
```

---

## 输出示例

```json
{
  "timestamp": "2025-10-21T14:52:30.123456Z",
  "severity": "INFO",
  "message": "video accepted",
  "serviceContext": {
    "service": "catalog-service",
    "version": "2025.10.21",
    "environment": "prod"
  },
  "trace": "projects/my-project/traces/3d8f09bd2cd9d4f7",
  "spanId": "a1b2c3d4e5f6g7h8",
  "sourceLocation": {
    "file": "internal/service/video.go",
    "line": 82,
    "function": "catalog.VideoService.Accept"
  },
  "labels": {
    "caller": "catalog.VideoService.Accept",
    "instance_id": "instance-001",
    "request_id": "req-123",
    "user_id": "user-456",
    "team": "growth"
  },
  "httpRequest": {
    "requestMethod": "POST",
    "requestUrl": "https://api.example.com/v1/catalog/videos",
    "status": 200,
    "remoteIp": "10.1.2.3",
    "latency": "0.120s",
    "userAgent": "chrome/127.0.0.1",
    "protocol": "HTTP/2"
  },
  "jsonPayload": {
    "payload": {
      "video_id": "vid-01XYZ",
      "status": "accepted"
    }
  }
}
```

---

## 快速开始

```bash
cd lingo-utils/gclog
go test ./...
```

在其它模块引用：

```go
import gclog "github.com/bionicotaku/lingo-utils/gclog"

logger, flush, err := gclog.NewLogger(
    gclog.WithService("catalog"),
    gclog.WithVersion("2025.10.21"),
    gclog.WithEnvironment("prod-cn"),
)
if err != nil {
    panic(err)
}
defer flush(context.Background())

log.NewHelper(logger).Info("booted")
```

---

## 实施与验证建议

1. **字段检查**：确保 `service`、`version`、`message`、`trace`（若启用）在输出中存在；labels/HTTP 请求字段符合 Cloud Logging 格式。
2. **中间件升级**：把 gRPC / HTTP Server & Client 的 logging middleware 全部替换为 `logging.Server(logging.WithLogger(logger), logging.WithFields(...))`，并在 `fields` 回调里调用 `gclog.AppendTrace`、`gclog.AppendLabels` 等 helper，将 `request_id`、`user_id` 等维度统一放入 labels。  
3. **trace 必要性**：只有在上下游链路都启用了 OTel tracing 时才输出 `trace`/`spanId`；否则留空即可。
4. **敏感数据处理**：可结合 Kratos `log.NewFilter` 或自定义 helper 对 payload 中的隐私字段做脱敏。
5. **测试**：使用 `NewTestLogger` + `StubTraceContext` 构造单测，确保日志 JSON 符合预期。
6. **部署验证**：在 Cloud Logging 控制台确认日志能按 `serviceContext.service`、`serviceContext.version`、`labels` 等维度筛选；如启用了 Error Reporting，检查是否按版本自动聚合。

> **关于 Operation / Resource**  
> gclog 目前专注于应用日志的核心字段。若需要将多条日志归属同一长操作（`operation.id/first/last`）或显式指定 GCP 资源类型（`resource.type`），可以在后续迭代中通过扩展 Options 与 logEntry 结构实现；Cloud Logging API 允许直接设定这些字段。

---

## TODO

- [ ] 提供 `DecodeEntries`、`AssertEntry` 等测试辅助函数
- [ ] 增加 `WithHTTPRequest` 默认实现，并在 README 中加入 HTTP middleware 示例
- [ ] 可选：直接集成 `cloud.google.com/go/logging` API（FlushFunc 真正调用 `logger.Flush()`）

---

如需讨论更多字段/辅助方法或 Pull Request，欢迎在 [GitHub: bionicotaku/lingo-utils](https://github.com/bionicotaku/lingo-utils) 提 Issue/PR。
