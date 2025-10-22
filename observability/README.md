# Observability Toolkit for Kratos Services

`github.com/bionicotaku/lingo-utils/observability` 为本仓各微服务提供统一的 OpenTelemetry 初始化、配置与 Kratos 中间件封装。目标是让每个服务在最少的代码改动下同时具备 Trace 与 Metrics 推送能力，并与 Cloud Run / Cloud Monitoring / Cloud Trace 等环境保持一致。

> 如果需要更结构化的接入示例，请配合 `INTEGRATION.md`（以 `kratos-template` 为例）阅读。

---

## 功能一览

| 功能                   | 描述                                                                                   |
| ---------------------- | -------------------------------------------------------------------------------------- |
| 统一配置模型           | `ObservabilityConfig` 支持 Tracing + Metrics + 全局属性，自动填充默认值。             |
| 资源构建               | 自动注入 `service.name`、`service.version`、`deployment.environment` 等属性。         |
| OTLP Push 支持         | Tracing 与 Metrics 默认使用 OTLP gRPC 推送，可切换 stdout 调试，不提供 `/metrics` 端点。 |
| Kratos 中间件封装      | `tracing.Server()` / `tracing.Client()` 直接嵌入 `http.Server` / `grpc.Server`。        |
| gRPC 拨号集成          | 为下游 gRPC 客户端提供带 tracing 的拨号逻辑示例。                                      |
| Runtime Metrics        | 默认使用 `runtime.Start` 采集 Go Runtime 指标，按配置周期推送。                       |
| 降级与必需开关         | `Required` 字段控制初始化失败是否阻断启动；也可通过 `Enabled` 单独关闭子模块。        |
| 可测试性               | 自带 stdout 模式与单元测试，方便在 CI 或本地验证。                                     |

---

## 模块结构

```
observability/
├── config.go         # 公共配置结构与默认值
├── resource.go       # Resource 构建（service.*、environment、全局属性）
├── init.go           # 聚合入口：Init + Option + Shutdown
├── init_test.go      # 集成单元测试
├── INTEGRATION.md    # 集成示例文档（以 kratos-template 为例）
├── README.md         # 本说明
├── metrics/
│   ├── init.go       # MeterProvider + exporter 初始化
│   ├── options.go    # metrics.Init 可选项
│   └── init_test.go  # metrics 测试
└── tracing/
    ├── init.go       # TracerProvider + exporter 初始化
    ├── options.go    # tracing.Init 可选项
    ├── middleware.go # Kratos HTTP/gRPC 中间件封装
    └── init_test.go  # tracing 测试
```

---

## 快速上手

1. **安装依赖**
   ```bash
   go get github.com/bionicotaku/lingo-utils/observability@latest
   ```
   若在同一仓库内开发，可通过 `go.work` 将模块指向本地路径。

2. **扩展配置**
   - 在服务的 `conf.proto` 中新增 `Observability` 结构（参考 `INTEGRATION.md`）。
   - 在 `config.yaml` 添加默认配置：
     ```yaml
     observability:
       tracing:
         enabled: true
         exporter: otlp_grpc
         endpoint: otel.googleapis.com:4317
         samplingRatio: 0.1
         batchTimeout: 5s
         exportTimeout: 10s
         maxQueueSize: 4096
         maxExportBatchSize: 512
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

3. **入口初始化**
   ```go
   shutdownObs, err := observability.Init(ctx, cfg.Observability,
       observability.WithLogger(logger),
       observability.WithServiceName(Name),
       observability.WithServiceVersion(Version),
       observability.WithEnvironment(os.Getenv("APP_ENV")),
   )
   if err != nil {
       return fmt.Errorf("init observability: %w", err)
   }
   defer shutdownObs(context.Background())
   ```

4. **中间件接入**
   ```go
   http.NewServer(
       http.Middleware(
           recovery.Recovery(),
           tracing.Server(),
           logging.Server(logger),
       ),
   )

   grpc.NewServer(
       grpc.Middleware(
           recovery.Recovery(),
           tracing.Server(),
       ),
   )
   ```

5. **gRPC 客户端拨号**
   ```go
   conn, err := kgrpc.DialInsecure(context.Background(),
       kgrpc.WithEndpoint(target),
       kgrpc.WithMiddleware(
           recovery.Recovery(),
           tracing.Client(),
           circuitbreaker.Client(),
       ),
   )
   ```

---

## 配置字段详解

| 字段 | 描述 | 建议值 |
| ---- | ---- | ------ |
| `Tracing.Enabled` | 是否启用追踪 | 开发/生产均建议开启 |
| `Tracing.Exporter` | `otlp_grpc` / `stdout` | 生产必用 `otlp_grpc` |
| `Tracing.Endpoint` | OTLP gRPC 地址 | Cloud Run 指向 `otel.googleapis.com:4317` |
| `Tracing.SamplingRatio` | 0~1 范围，超出将被钳制 | Dev: 1.0；Prod: 0.1~0.2 |
| `Tracing.BatchTimeout` & `ExportTimeout` | 批量导出超时 | 默认为 5s / 10s |
| `Tracing.MaxQueueSize` / `MaxExportBatchSize` | 内部队列大小/批量大小 | 默认 2048 / 512 |
| `Tracing.Headers` | 额外请求头 | 多数云环境不需要；特殊场景用于注入认证信息 |
| `Tracing.Required` | 初始化失败是否阻断启动 | 生产模式建议 `true` |
| `Metrics.Interval` | 指标推送周期 | 60s；根据需要调整 |
| `Metrics.DisableRuntimeStats` | 是否关闭 Go runtime 指标 | 只有在指标量太大时才关闭 |
| `Metrics.Required` | 初始化失败是否阻断启动 | 看业务需求决定 |
| `GlobalAttributes` | 追踪与指标共享的标签 | `service.group`、`region` 等组织维度 |

默认值来源顺序：显式配置 > 环境变量（如 `OTEL_EXPORTER_OTLP_ENDPOINT`）> 模块内默认。

---

## 验证与部署

### 本地调试
1. 将 exporter 改为 `stdout`，运行 `go test ./...` 或启动服务，观察 Span/Metric JSON 输出。
2. 若需完整 OTLP 流程，可通过 Docker 启动本地 Collector：
   ```bash
   docker run --rm -p 4317:4317 -v $(pwd)/otel-collector.yaml:/etc/otelcol/config.yaml otel/opentelemetry-collector:latest
   ```
3. Collector `otlp -> logging` 配置可参考 `INTEGRATION.md`。

### Cloud Run / Cloud Monitoring / Cloud Trace
1. 启用 Cloud Run 服务账号的 `roles/cloudtrace.agent` 和 `roles/monitoring.metricWriter`。
2. 配置 `endpoint=otel.googleapis.com:4317`，其余 header 可留空。
3. 部署后在 Cloud Console：  
   - Trace 页面确认 `service.name`、`deployment.environment` 等标签。  
   - Monitoring 创建自定义指标图表（维度 `resource.label.\"service.name\"`）观察请求量/延迟。

### Collector 分层架构
若需统一管线，可将 OTLP 导出目标指向自建或托管的 OpenTelemetry Collector，再由 Collector 转发到多家后端（Cloud、Tempo、Datadog 等）。应用侧无需改动代码，只调整配置 Endpoint 即可。

---

## 常见问题

| 问题 | 可能原因 | 解决建议 |
| ---- | -------- | -------- |
| `dial tcp ...: connect: connection refused` | Collector / OTLP Endpoint 不可达 | 确认端口、网络、防火墙；必要时切换 stdout 或关闭 Required |
| Trace 无数据 | 采样率太低或中间件未挂载 | 调整 `SamplingRatio`、检查 `tracing.Server()` 是否在 middleware 链上 |
| 指标缺失 | Metrics Disabled 或 Interval 太长 | 设置 `Metrics.Enabled=true`、缩短 Interval |
| Cloud Trace 提示权限不足 | 服务账号缺少角色 | 为运行服务的 SA 添加 `roles/cloudtrace.agent` |
| Cloud Monitoring 报错 `permission denied` | 未授予写指标权限 | 添加 `roles/monitoring.metricWriter` |
| Exporter 初始化超时 | TLS/证书问题 | 对本地 collector 设置 `Insecure=true`；生产使用受信任证书 |
| Runtime 指标太多 | 指标维度过大或频率过高 | 关闭 `DisableRuntimeStats` 或延长 `Interval` |

---

## 最佳实践

### 日志整合与错误处理

1. **强制注入结构化 Logger**  
   `observability.Init` 必须显式传入 `observability.WithLogger`，否则会直接返回错误。请将统一的 Kratos `log.Logger` 注入遥测组件，并追加固定字段，确保 Trace/Metrics 与业务日志共用同一条输出链路：
   ```go
   baseLogger := log.With(logger,
       "component", "observability",
       "service.name", serviceName,
   )
   shutdownObs, err := observability.Init(ctx, cfg.Observability,
       observability.WithLogger(baseLogger),
       observability.WithServiceName(serviceName),
       observability.WithServiceVersion(version),
       observability.WithEnvironment(env),
   )
   ```

2. **自定义 `otel.ErrorHandler`，接管所有导出异常**  
   OpenTelemetry SDK 默认把 exporter 错误直接写到 `stderr`。在初始化后立即注册自定义 Handler，把 SDK 抛出的所有错误转换为结构化日志（与 Kratos 等级体系一致）：
   ```go
   func installOTELErrorHandler(logger log.Logger) {
       // 需 import "google.golang.org/grpc/codes" 与 "google.golang.org/grpc/status"。
       helper := log.NewHelper(logger)
       otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
           st, ok := status.FromError(err)
           if !ok {
               helper.Errorw("msg", "otel exporter error", "error", err)
               return
           }
           switch st.Code() {
           case codes.Unavailable, codes.ResourceExhausted, codes.DeadlineExceeded:
               helper.Warnw("msg", "otel exporter retrying",
                   "error", err,
                   "grpc_code", st.Code().String(),
                   "retry_backoff", extractBackoff(err),
               )
           case codes.InvalidArgument, codes.PermissionDenied, codes.Unauthenticated:
               helper.Errorw("msg", "otel exporter permanent failure",
                   "error", err,
                   "grpc_code", st.Code().String(),
               )
           default:
               helper.Errorw("msg", "otel exporter unexpected error",
                   "error", err,
                   "grpc_code", st.Code().String(),
               )
           }
       }))
   }
   ```
   `extractBackoff` 可从错误消息或自行维护的上下文中解析出当前退避间隔，用于帮助运维判断恢复时间。

3. **包装 OTLP 重试策略并输出退避状态**  
   使用 `otlptracegrpc.WithRetry` 包装默认配置，在请求函数外围记录尝试次数、退避时长，并将其写入 Kratos 日志。建议在连续失败 N 次后发出额外的 `Error` 日志或 Prometheus 告警，同时在恢复成功时输出一条 `Info`：
   ```go
   retryCfg := otlptracegrpc.RetryConfig{
       Enabled:         true,
       InitialInterval: 5 * time.Second,
       MaxInterval:     30 * time.Second,
       MaxElapsedTime:  time.Minute,
   }
   clientOpt := otlptracegrpc.WithRetry(retryCfg)
   // 在 observability/tracing 内部：对 retryCfg.RequestFunc 进行装饰，写入 helper.Warnw。
   ```

4. **指标与告警闭环**  
   - 订阅 `otelcol_exporter_send_failed_*`、`otelcol_exporter_queue_size` 等 Collector 指标，用于自动告警。  
   - 在自定义 `ErrorHandler` 中维护连续失败计数，达到阈值时写入报警字段或触发内部事件；当 exporter 恢复成功时输出一条 `Info`，形成告警闭环。  
   - 健康探针通常访问频繁，如需纳入指标，可在服务配置中将 `observability.metrics.grpc_include_health` 设为 `true`；默认 `false` 会过滤 `/grpc.health.v1.Health/Check` 调用，避免噪音；若需完全关闭 gRPC 指标，设置 `observability.metrics.grpc_enabled=false`。

### 传播器约定

库内部默认注册 `TraceContext + Baggage` 组合传播器，这是 OpenTelemetry 官方推荐的跨进程上下文标准；因此模板无需额外代码即可让 `observability/tracing` 中的 gRPC/HTTP 中间件透传 TraceID。如果需要兼容 Jaeger、B3 或自定义头部，可在服务入口显式传入新的组合：
```go
shutdown, err := observability.Init(ctx, cfg,
    observability.WithLogger(logger),
    observability.WithPropagator(propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{},
        propagation.Baggage{},
        jaegerPropagation{},
    )),
)
```
在接入额外传播器时，请同步更新项目文档，确保上下游服务采用相同的头部协议。

- **尽早初始化**：在服务入口配置加载后立即调用 `observability.Init`，确保后续组件（数据库、外部服务）也能获得 Trace 信息。
- **统一命名**：使用一致的 `service.name`（如 `gateway`、`catalog`）与 `deployment.environment`（`dev/staging/prod`）方便跨服务聚合。
- **采样策略**：生产环境根据请求量调整 `SamplingRatio`；临时排障时可动态提高采样率，再恢复常规值。
- **幂等性**：Tracing/Metrics 的 `Shutdown` 要在 `defer` 中调用，确保批量数据在退出前写出。
- **日志关联**：若日志系统支持结构化输出，可通过 `kratos` 的 log middleware 注入 `trace_id`、`span_id`（Kratos tracing middleware 已提供 valuer）。
- **最小权限**：仅授予服务账号所需权限；生产环境禁止 `Insecure=true`。
---

## Roadmap

- [ ] 提供 `convert` 工具，将 protobuf 配置直接转换为 `ObservabilityConfig`。
- [ ] 拓展 metrics/instruments 辅助函数（HTTP 请求数、延迟等）。
- [ ] 增加 logs 模块，实现 trace-context 与结构化日志联动。
- [ ] 引入配置热更新能力（通过 `atomic.Value` 支持在线 Reconfigure）。
- [ ] 提供更多 Collector 部署样例（Terraform / Helm）。

---

## 参考资料

- [OpenTelemetry Specification](https://opentelemetry.io/docs/specs/)
- [Kratos Middleware - Tracing](https://go-kratos.dev/en/docs/component/middleware/tracing/)
- [Google Cloud Observability (OTLP Export)](https://cloud.google.com/stackdriver/docs/export/otlp)
- 仓库内集成示例：`INTEGRATION.md`

欢迎在实际接入中根据需要扩展配置字段或提交改进建议。💕
