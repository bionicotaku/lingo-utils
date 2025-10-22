# Kratos 项目集成指南

本指南演示如何在 `kratos-template` 这类 Kratos 服务中集成 `github.com/bionicotaku/lingo-utils/observability`，实现统一的 Trace 与 Metrics 推送。步骤分为配置扩展、入口初始化、中间件接入与本地/云端验证四部分。

---

## 1. 安装依赖

在待集成服务的 `go.mod` 中添加引用（如与本仓同机开发，可通过 `go.work` 指向本地路径）：

```bash
go get github.com/bionicotaku/lingo-utils/observability@latest
```

若使用 `go.work`，建议追加：

```go
use (
    ...
    ./lingo-utils/observability
)

replace github.com/bionicotaku/lingo-utils/observability => ./lingo-utils/observability
```

---

## 2. 扩展配置

### 2.1 Proto Schema

在服务的 `internal/conf/conf.proto` 中新增 Observability 结构。例如：

```proto
message Observability {
  Tracing tracing = 1;
  Metrics metrics = 2;
  map<string, string> global_attributes = 3;

  message Tracing {
    bool enabled = 1;
    string exporter = 2; // otlp_grpc / stdout
    string endpoint = 3;
    bool insecure = 4;
    double sampling_ratio = 5;
    google.protobuf.Duration batch_timeout = 6;
    google.protobuf.Duration export_timeout = 7;
    uint32 max_queue_size = 8;
    uint32 max_export_batch_size = 9;
    map<string, string> headers = 10;
    bool required = 11;
  }

  message Metrics {
    bool enabled = 1;
    string exporter = 2; // otlp_grpc / stdout
    string endpoint = 3;
    bool insecure = 4;
    google.protobuf.Duration interval = 5;
    bool disable_runtime_stats = 6;
    map<string, string> headers = 7;
    bool required = 8;
  }
}
```

在 `Bootstrap` 消息中追加 `Observability observability = <index>;`，执行 `make config` 生成最新的 `conf.pb.go`。

### 2.2 默认配置文件

在服务的 `configs/config.yaml` 中补充示例：

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp_grpc
    endpoint: otel.googleapis.com:4317
    samplingRatio: 0.2
    batchTimeout: 5s
    exportTimeout: 10s
    maxQueueSize: 4096
    maxExportBatchSize: 512
    # headers:
    #   x-goog-user-project: your-project-id
    required: true
  metrics:
    enabled: true
    exporter: otlp_grpc
    endpoint: otel.googleapis.com:4317
    interval: 60s
    disableRuntimeStats: false
    required: true
  globalAttributes:
    service.group: template
    region: local
```

---

## 3. 入口初始化

在 `cmd/server/main.go` 中读取配置后调用 `observability.Init`：

```go
import (
    ...
    obs "github.com/bionicotaku/lingo-utils/observability"
)

func main() {
    ...
    shutdownObs, err := obs.Init(ctx, convertObservabilityConfig(bc.Observability),
        obs.WithLogger(logger),
        obs.WithServiceName(Name),
        obs.WithServiceVersion(Version),
        obs.WithEnvironment(os.Getenv("APP_ENV")),
    )
    if err != nil {
        panic(fmt.Errorf("init observability: %w", err))
    }
    defer func() {
        if err := shutdownObs(context.Background()); err != nil {
            log.NewHelper(logger).Warnf("shutdown observability: %v", err)
        }
    }()
    ...
}
```

`convertObservabilityConfig` 用于将 protobuf 映射为 `observability.ObservabilityConfig`：

```go
func convertObservabilityConfig(in *conf.Observability) obs.ObservabilityConfig {
    if in == nil {
        return obs.ObservabilityConfig{}
    }
    return obs.ObservabilityConfig{
        GlobalAttributes: in.GlobalAttributes,
        Tracing: &obs.TracingConfig{
            Enabled:            in.Tracing.GetEnabled(),
            Exporter:           in.Tracing.GetExporter(),
            Endpoint:           in.Tracing.GetEndpoint(),
            Headers:            in.Tracing.GetHeaders(),
            Insecure:           in.Tracing.GetInsecure(),
            SamplingRatio:      in.Tracing.GetSamplingRatio(),
            BatchTimeout:       in.Tracing.GetBatchTimeout().AsDuration(),
            ExportTimeout:      in.Tracing.GetExportTimeout().AsDuration(),
            MaxQueueSize:       int(in.Tracing.GetMaxQueueSize()),
            MaxExportBatchSize: int(in.Tracing.GetMaxExportBatchSize()),
            Required:           in.Tracing.GetRequired(),
        },
        Metrics: &obs.MetricsConfig{
            Enabled:             in.Metrics.GetEnabled(),
            Exporter:            in.Metrics.GetExporter(),
            Endpoint:            in.Metrics.GetEndpoint(),
            Headers:             in.Metrics.GetHeaders(),
            Insecure:            in.Metrics.GetInsecure(),
            Interval:            in.Metrics.GetInterval().AsDuration(),
            DisableRuntimeStats: in.Metrics.GetDisableRuntimeStats(),
            Required:            in.Metrics.GetRequired(),
        },
    }
}
```

---

## 4. 中间件接入

### 4.1 HTTP / gRPC Server

在 `internal/server/http.go` 和 `internal/server/grpc.go` 中，将 tracing 中间件加入链路：

```go
import (
    ...
    obsTracing "github.com/bionicotaku/lingo-utils/observability/tracing"
)

func NewHTTPServer(...){
    srv := http.NewServer(
        http.Middleware(
            recovery.Recovery(),
            obsTracing.Server(),     // 新增
            logging.Server(logger),
        ),
        ...
    )
    ...
}
```

默认配置会自动跳过健康检查路由（`/_health`, `/healthz`, `/readyz`）；如需修改，可通过 `obsTracing.WithServerSkipper(...)` 自定义。

### 4.2 gRPC 客户端拨号

在 `internal/client/grpc.go` 中追加 `tracing.Client()`：

```go
conn, err := kgrpc.DialInsecure(ctx,
    kgrpc.WithEndpoint(c.GrpcClient.Target),
    kgrpc.WithMiddleware(
        recovery.Recovery(),
        obsTracing.Client(), // 新增
        circuitbreaker.Client(),
    ),
)
```

---

## 5. 指标上报

初始化完成后，可在业务层或中间件中通过全局 Meter 上报指标：

```go
meter := otel.GetMeterProvider().Meter("kratos-template/http")
requestCounter, _ := meter.Int64Counter("http.requests.total")
requestCounter.Add(ctx, 1, attribute.String("route", templateRoute))
```

无需暴露 `/metrics` 端点；指标将按配置周期性推送至 OTLP 目标。

---

## 6. 验证流程

### 6.1 本地

1. 将 exporter 设为 `stdout`，运行 `go test ./...` 或直接启动服务，观察控制台输出的 Span/Metric 数据。
2. 若要验证 OTLP Push，可使用本地 Collector（示例命令）：

```bash
docker run --rm -p 4317:4317 \
  -v $(pwd)/otel-collector.yaml:/etc/otelcol/config.yaml \
  otel/opentelemetry-collector:latest
```

Collector `otel-collector.yaml` 示例：

```yaml
receivers:
  otlp:
    protocols:
      grpc:

processors:
  batch:

exporters:
  logging:
    loglevel: debug

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
```

### 6.2 Cloud Run（推送至 Cloud Trace / Monitoring）

1. 启用 Cloud Run 服务账号的 `roles/cloudtrace.agent` 与 `roles/monitoring.metricWriter`。
2. 配置 `endpoint=otel.googleapis.com:4317`，其余 header 通常无需设置。
3. 部署后在 Cloud Console 检查 Trace 与 Metrics 日志，确认 `service.name`、`deployment.environment` 等标签一致。

---

## 7. 常见问题与建议

| 问题                                   | 建议处理                                                |
| -------------------------------------- | ------------------------------------------------------- |
| OTLP 目标不可达                        | 启用 `stdout` 或关闭 `Required` 开关，避免阻塞启动      |
| Trace 采样率过高，资源消耗大           | 调低配置中的 `samplingRatio`                            |
| 指标维度过多                           | 控制 Label 数量，避免使用动态字符串（如原始 URL）       |
| 需要跨语言传播                         | 保持默认 W3C TraceContext；如需 B3，可自定义 propagator |
| Collector 需要 TLS                     | 配置 `import` 证书或使用受信任的负载均衡器              |

---

## 8. 下一步

- 若多个服务使用同一转换逻辑，可在 `lingo-utils/observability` 增加 `ConvertFromProto` 工具函数。
- 可扩展 metrics 子包以提供常用指标 helper（比如 HTTP 请求量/延迟）。
- 如需日志链路统一，可后续引入 `logs` 子包，将 trace_id/span_id 注入日志字段。
