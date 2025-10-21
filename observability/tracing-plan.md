# Kratos Tracing 统一组件设计

## 目标
- 为所有 Kratos 服务提供一致的 OpenTelemetry TracerProvider 初始化与关闭流程。
- 对齐项目架构：配置经由各服务 `internal/infra/config`，公共实现放在 `lingo-utils` 供引用。
- 支持本地快速验证（stdout exporter）与生产 OTLP/Jaeger/Tempo。

## 模块划分
1. `config`：定义 `TracingConfig` 结构体，支持以下字段：`Enabled`、`Exporter`(`otlp_grpc`/`stdout`)、`Endpoint`、`Insecure`、`Headers`、`SamplingRatio`、`ServiceName`、`ServiceVersion`、`Environment`、`Attributes`。
2. `provider`：
   - 构建 `resource.Resource`，写入 `semconv.ServiceNameKey` 等。
   - 根据 `Exporter` 创建 `sdktrace.TracerProvider`：
     - `otlp_grpc` → `otlptracegrpc.New`（支持 TLS/自定义 header）。
     - `stdout` → `stdouttrace.New(stdouttrace.WithPrettyPrint())`。
   - 设置采样器：`sdktrace.ParentBased(sdktrace.TraceIDRatioBased(SamplingRatio))`。
   - 注册全局 `otel.SetTracerProvider` 与 `otel.SetTextMapPropagator`（TraceContext+Baggage）。
   - 返回 `Shutdown(ctx)` 闭包，内部调用 `tp.Shutdown(ctx)`。
3. `middleware`：
   - 提供 `ServerOptions(tp trace.TracerProvider, propagator propagation.TextMapPropagator) []http.ServerOption` 等辅助，或简单导出 `Server()`/`Client()` 包装 Kratos 自带 `tracing` 中间件。
   - 封装 selector，默认排除 `/healthz`, `/readyz`。
4. `bootstrap`：
   - 暴露 `Init(ctx context.Context, cfg TracingConfig, opts ...Option) (Shutdown, error)`；`Shutdown` 类型为 `func(context.Context) error`。
   - 允许注入 `WithLogger(log.Logger)`，在初始化失败时输出结构化日志。

## 集成流程
1. 各服务在 `cmd/.../main.go` 中：
   ```go
   shutdown, err := tracing.Init(ctx, cfg.Tracing,
       tracing.WithServiceName("gateway"),
       tracing.WithLogger(logger),
   )
   if err != nil { return err }
   defer shutdown(context.Background())
   ```
2. `internal/server/http.go` / `grpc.go`：
   ```go
   opts := []http.ServerOption{
       http.Middleware(
           recovery.Recovery(),
           tracingmiddleware.Server(),
           logging.Server(logger),
       ),
   }
   ```
3. `internal/client/grpc.go`：调用统一 `lingo-utils/observability/tracing.Dial` 返回 `*grpc.ClientConn`，内部组合 `tracing.Client()`、超时和 breaker。

## 迁移与验证
- 阶段 1：Gateway 服务接入；启用 stdout exporter 验证 Span。
- 阶段 2：Catalog/Search 等服务迁移；在本地启动 Jaeger (`docker run jaegertracing/all-in-one`)，通过 `make test-e2e` 验证链路。
- 阶段 3：统一配置热加载策略，必要时监听 FSnotify 触发 `Reinit`（需设计为幂等）。

## 需要确认的问题
- 是否强制 OTLP gRPC，还是需要支持 HTTP / Zipkin？
- 与现有日志/metrics 组件是否合并到同一 Observability 包？
- 配置热更新是否要求无中断？若是，需要在内部维护 atomic.Value 存储 provider。

