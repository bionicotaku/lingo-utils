# Observability（Tracing + Metrics）统一组件设计

## 目标
- 为所有 Kratos 服务提供一致的 OpenTelemetry 初始化流程，覆盖 **Trace** 与 **Metric** 两类信号。
- 对齐项目架构：配置经由各服务 `internal/infra/config`，公共实现集中在 `lingo-utils/observability`。
- 统一采用 **OTLP Push** 模式：服务内置 OTLP gRPC exporter，将数据发送到 **OpenTelemetry Collector** 或 Google Cloud 官方后端；不暴露本地 `/metrics` HTTP 端点。
- 降低重复工作：中间件、拨号器、Shutdown 流程统一封装，避免各服务重复拼装 Provider。

## 目录结构
```
lingo-utils/observability/
├── config.go            # 公共配置结构与默认值
├── resource.go          # service.name/version/env 等 Resource 构造
├── init.go              # 一站式初始化入口（Init + Option）
├── init_test.go         # 聚合单测
├── tracing/
│   ├── init.go          # TracerProvider + exporter 装配
│   ├── options.go       # tracing.Init 的可选项
│   ├── init_test.go     # tracing 单测
│   └── middleware.go    # Kratos tracing.Server()/Client() 包装
└── metrics/
    ├── init.go          # MeterProvider + exporter 装配
    ├── options.go       # metrics.Init 的可选项
    └── init_test.go     # metrics 单测
```

## 配置模型
- `ObservabilityConfig`
  - `Tracing`：`Enabled`、`Exporter`(`otlp_grpc`/`stdout`)、`Endpoint`、`Headers`、`Insecure`、`SamplingRatio`、`ServiceName`、`ServiceVersion`、`Environment`、`Attributes(map[string]string)`。
  - `Metrics`：`Enabled`、`Exporter`(`otlp_grpc`/`stdout`)、`Endpoint`、`Headers`、`Insecure`、`Interval`、`ResourceAttributes`、`DisableRuntimeStats`。
  - 公共：`GlobalAttributes`（同时附加到 Trace/Metric）、`Propagators`（扩展 B3/Jaeger）、`Logger`。
- 默认值遵循 OpenTelemetry 规范：先读服务配置，再读 `OTEL_*` 环境变量，最后落入内建默认（如 Endpoint= `otel.googleapis.com:4317`、Interval=60s）。

## Tracing 模块实现要点
1. **Provider 构建**（`tracing/init.go`）
   - 统一 Resource：由上层 `observability.Init → buildResource` 提供，已包含 `service.name`、`deployment.environment`、全局属性等。
   - 采样器：`sdktrace.ParentBased(sdktrace.TraceIDRatioBased(SamplingRatio))`，默认 dev=1.0、prod=0.1。
   - Exporter：
     - `otlp_grpc`（默认）：`otlptracegrpc.New`，允许配置自定义 header（如 `Authorization`、`x-goog-user-project`）以及 TLS/非 TLS。
     - `stdout`：`stdouttrace.New(stdouttrace.WithPrettyPrint())`，用于本地验证。
   - Provider：`sdktrace.NewTracerProvider(
       sdktrace.WithSampler(...),
       sdktrace.WithBatcher(exporter, sdktrace.WithMaxExportBatchSize(...), sdktrace.WithBatchTimeout(...)),
       sdktrace.WithResource(res),
     )`。
   - 全局注册：`otel.SetTracerProvider(tp)`、`otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))`。
   - 返回 `Shutdown func(context.Context) error`，内部调用 `tp.Shutdown(ctx)` 并记录错误。

2. **Kratos 中间件封装**（`tracing/middleware.go`）
   - `Server(opts ...ServerOption)`：基于 `tracing.Server` 封装，默认排除 `/healthz`、`/readyz` 路由，可通过 Option 调整。
   - `Client(opts ...ClientOption)`：封装 `tracing.Client`，支持追加默认属性（如 `peer.service`）。
   - 提供快捷函数 `HTTPServerOptions()`、`GRPCServerOptions()`，供 `internal/server` 直接 append。

3. **Kratos 集成**
   - `tracing.Server()` / `tracing.Client()` 默认通过 selector 排除健康检查路由，必要时可用 `WithServerSkipper` 自定义。
   - 若需自定义 propagator 或 tracer name，可在 `observability.Init` 处通过 `observability.WithPropagator()` 或在中间件注入 `WithServerTracerName`。

## Metrics 模块实现要点
1. **Provider 构建**（`metrics/init.go`）
   - 共享 Resource，保持与 Trace 一致的标签。
   - 仅提供 Push 出口：
     - `otlp_grpc`：`otlpmetricgrpc.New`；默认 Endpoint=`otel.googleapis.com:4317`，推送到 Collector 或 Cloud Monitoring。需要时可在配置中追加 header（Cloud Run + Workload Identity 下通常不必手写）。
     - `stdout`：`stdoutmetric.New(stdoutmetric.WithPrettyPrint())`。
   - Reader：`sdkmetric.NewPeriodicReader(exporter, metric.WithInterval(cfg.Interval))`（默认 60s）；`DisableRuntimeStats=false` 时注册 `runtime.Start` 给 Runtime 指标。
   - 注册 `global.SetMeterProvider(mp)`。
   - 返回 `Shutdown`：调用 `MeterProvider.Shutdown(ctx)` 即可完成 reader/exporter 收尾。

2. **Collector 配置建议**
   - **本地**：启动独立 Collector（docker），配置 OTLP gRPC receiver → logging exporter；如需联动 Jaeger/Tempo，再追加 OTLP trace exporter。
   - **Cloud Run**：
     - 方案 A：应用直接指向 `otel.googleapis.com:4317`，凭借 Workload Identity 写入 Cloud Trace / Cloud Monitoring。
     - 方案 B：部署托管或自建 Collector（GCE/GKE/Cloud Run），Collector 导出到多种后端（Cloud、Datadog、Prometheus Remote Write）；应用只需改配置即可切换。
   - 文档中提供示例 Collector YAML，并提示服务账号需 `roles/cloudtrace.agent`、`roles/monitoring.metricWriter` 权限。

3. **常用指标**
   - 规范：所有自定义指标推荐与 `service.name`、`deployment.environment` 维度关联；在业务层通过 `meter := otel.GetMeterProvider().Meter("gateway/http")` 获取仪表。
   - 常见组合：HTTP 请求总数（Counter）、延迟（Histogram）、错误计数。可后续新增 helper。

## 配置与调用示例

### 1. 服务配置结构
在服务内添加类似配置（YAML / TOML）：
```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp_grpc
    endpoint: otel.googleapis.com:4317     # 可留空采用默认环境变量
    insecure: false
    samplingRatio: 0.2
    batchTimeout: 5s
    exportTimeout: 10s
    maxQueueSize: 4096
    maxExportBatchSize: 512
    headers:
      x-goog-user-project: your-project-id   # Cloud Run 非必须
    required: true
  metrics:
    enabled: true
    exporter: otlp_grpc
    endpoint: otel.googleapis.com:4317
    interval: 60s
    disableRuntimeStats: false
    required: true
  globalAttributes:
    service.group: gateway
    region: local
```

### 2. Go 结构体映射
```go
type Config struct {
    Observability observability.ObservabilityConfig `yaml:"observability"`
}
```

### 3. 服务入口调用
```go
obsShutdown, err := observability.Init(ctx, svcCfg.Observability,
    observability.WithLogger(logger),
    observability.WithServiceName("gateway"),
    observability.WithServiceVersion(version),
    observability.WithEnvironment(cfg.AppEnvironment),
)
if err != nil {
    return fmt.Errorf("init observability: %w", err)
}
defer func() {
    if err := obsShutdown(context.Background()); err != nil {
        log.NewHelper(logger).Warnf("shutdown observability: %v", err)
    }
}()
```

### 4. Kratos Server/Client 集成
```go
httpServer := http.NewServer(
    http.Address(cfg.HTTPAddr),
    http.Middleware(
        recovery.Recovery(),
        tracing.Server(),
        logging.Server(logger),
    ),
)

grpcServer := grpc.NewServer(
    grpc.Middleware(
        recovery.Recovery(),
        tracing.Server(),
    ),
)
```

客户端拨号统一使用自定义封装（示例）：
```go
conn, err := grpc.DialContext(ctx, target,
    grpc.WithTransportCredentials(creds),
    grpc.WithUnaryInterceptor(middleware.Chain(
        tracing.Client(),
        // other middlewares...
    )),
)
```

### 5. 环境变量默认值
- `OTEL_EXPORTER_OTLP_ENDPOINT`、`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`、`OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` 会被 exporter 自动读取，若未在配置显式指定，可直接设置环境变量。
- 在 Cloud Run 环境，只需启用 Workload Identity 并赋予服务账号 `roles/cloudtrace.agent`、`roles/monitoring.metricWriter`，无需显式 header。

## 初始化流程
1. `cmd/<service>/main.go`：
   ```go
   obsCfg := loadObservability(cfg)
   shutdown, err := observability.Init(ctx, obsCfg,
       observability.WithLogger(logger),
       observability.WithServiceName("gateway"),
       observability.WithServiceVersion(version),
   )
   if err != nil {
       return fmt.Errorf("init observability: %w", err)
   }
   defer func() {
       if err := shutdown(context.Background()); err != nil {
           logger.Warn("shutdown observability", "error", err)
       }
   }()
   ```
   - `Init` 先构建 Resource → `tracing.Init` → `metrics.Init`，按需启用；若某一模块失败，根据配置决定是否降级（例如 `Tracing.Required`）。

2. `internal/server/http.go`/`grpc.go`：
   ```go
   opts := []http.ServerOption{
       http.Middleware(
           recovery.Recovery(),
           tracingmiddleware.Server(),
           logging.Server(logger),
       ),
   }
   ```
   - gRPC server 同理。

3. `internal/client/grpc.go`：在拨号时追加 `tracing.Client()` 中间件；若已有统一封装，可在 `lingo-utils` 后续新增 `Dial` 助手。

## 验证与运维
- **本地**：
  - 启动 Collector（logging exporter）观察 Trace/Metrics 输出；或设置 `Exporter=stdout` 查看控制台日志。
  - 使用 `vegeta` / `hey` 压测，确认 Span/Metric 周期性推送。
- **Cloud**：
- Cloud Trace：在控制台查看调用链，验证 `service.name`、`deployment.environment` 等标签。
- Cloud Monitoring：创建自定义指标图表，按 `resource.label.\"service.name\"` 聚合查看请求量/延迟。
  - Collector 可配置告警（如导出失败）或限流。

## 迁移步骤
1. **准备**：在各服务配置层新增 `ObservabilityConfig`，并在 `internal/infra/config` 解析；同时引入 `github.com/bionicotaku/lingo-utils/observability`。
2. **接入 Gateway**：调用 `observability.Init`，替换旧 tracing 初始化，删除 `/metrics` 暴露；本地使用 Collector logging exporter 验证。
3. **推广其他服务**：复用初始化与中间件接入逻辑，逐步在 Catalog/Search/Feed 等服务落地。
4. **CI 持续验证**：在 `make test`、`make lint` 流程中包含新模块；考虑在 e2e 中加入对 Cloud Trace / Monitoring 检查脚本。

## 风险与回滚
- OTLP Endpoint 不可达：导出器自带重试，日志中追加告警；必要时通过配置切换到 stdout 以保留数据。
- Collector 故障：Collector 侧加高可用部署或备份；应用层提供 `Enabled=false` 开关快速关闭。
- 性能：监控 CPU/内存；可通过配置调整 batch/interval。Trace 采样率需与业务 SLO 协调。
- 回滚：保留旧 tracing 初始化方案一段时间（配置开关）；指标可临时关闭 push，避免影响生产。

## 待办与扩展
- 是否需要统一日志（logs）模块，将 trace_id/span_id 注入结构化日志。
- Collector 部署脚本/ IaC：本地 docker-compose、Cloud 环境 Terraform。
- 按需追加 Zipkin/B3 Propagator 支持。
- 未来若接入第三方监控（Datadog、New Relic），仅需调整配置或 Collector exporter。

## 验收标准
- Gateway 接入后，在 Cloud Trace 能看到完整调用链，Span 标签包含 `service.name=gateway`、`deployment.environment=dev`。
- Metrics 通过 OTLP 推送至 Cloud Monitoring，可按服务维度查看请求量、延迟等自定义指标。
- `make lint`、`make test` 全量通过；Observability 模块测试覆盖率 ≥80%。
- 文档说明清晰，团队成员能根据本文完成配置与验证。
