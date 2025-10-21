# pkg/gclog TODO

> 目标：将 README 中的最佳实践落地为可复用的日志适配库，并确保覆盖单元测试与示例。

## P0 — 基础实现
- [x] 定义 `Options` 结构体与 Option Builder（`WithService`、`WithVersion`、`WithWriter` 等），实现 `ValidateOptions`。
- [x] 实现 `type Logger struct{...}`，满足 Kratos `log.Logger` 接口：
  - [x] 严格校验允许的键（`log.DefaultMessageKey`、`trace_id`、`span_id`、`caller`、`payload`、`labels`、`http_request`、`error`）。
  - [x] 将 `service`/`version`、`environment` 写入 `Entry.ServiceContext`。
  - [x] 将 `caller`、`instance_id` 以及静态标签写入 `entry.Labels`。
  - [x] 自定义字段统一放入 `payload`。
- [x] `NewLogger` 返回 `(log.Logger, func(context.Context) error, error)`，支持 writer、instance ID 配置。

- [x] `AppendTrace(ctx, projectID, kvs)`：从 OTel `SpanContext` 生成 Cloud Logging 兼容 trace/span。
- [x] `WithTrace(ctx, projectID, base)`：封装 `AppendTrace` + `log.WithContext`。
- [x] `WithCaller`、`WithPayload` 等字段型 helper，自动调用校验逻辑。
- [x] `Helper` 封装：提供 `InfoWithPayload`、`WithCaller` 等便捷方法。
- [x] `SeverityFromHTTP(status int)`（可选）：HTTP 状态码与日志级别映射。

- [x] 提供 `NewTestLogger()`（基于 `bytes.Buffer`）及解码辅助，辅助单元测试。
- [x] 提供固定 trace/span 生成器（例如 `StubTraceContext`）。
- [x] 为关键路径编写单元测试（字段校验、component 枚举、payload 类型、防并发问题）。
- [x] 在 README 补充代码示例、测试示例与常见坑提醒。

## P3 — 拓展（可选）
- [ ] 支持直接调用 `cloud.google.com/go/logging.Client` 的实现（保留与 stdout JSON 共存的能力）。
- [ ] 提供 gRPC / HTTP middleware 示例，演示如何注入 logger、payload、trace。
