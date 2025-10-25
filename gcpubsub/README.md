# gcpubsub 组件

`gcpubsub` 是一个可复用的 Google Cloud Pub/Sub 发布与 StreamingPull 组件，遵循 `lingo-utils` 的组件化模式（Config → Component → ProviderSet）。

## 功能亮点

- **统一配置**：`Config` 负责默认值填充、Emulator/ExactlyOnce 兼容处理。
- **可插拔依赖**：通过 `Dependencies` 注入日志、指标、追踪、凭证及自定义 `ClientFactory`。
- **发布能力**：封装 `Publish`/`Flush`，内置结构化日志与 OTel 指标；支持消息排序、超时控制。
- **StreamingPull**：封装 `Receive`/`Stop`，自动 Ack/Nack、处理 panic、记录指标与日志。
- **Wire 集成**：`ProviderSet` 输出 `Publisher` 与 `Subscriber`，与 `gclog` / `observability` / `txmanager` 一致。

## 快速使用

```go
var gcpPubsubProvider = wire.NewSet(
    configloader.ProvidePubsubConfig,       // 返回 gcpubsub.Config
    configloader.ProvidePubsubDependencies, // 返回 gcpubsub.Dependencies
    gcpubsub.ProviderSet,                   // 注入 Publisher / Subscriber
)
```

在 `NewComponent` 之前确保：

- `Config.ProjectID` 必填。
- 如需发布，设置 `Config.TopicID`; 如需消费，设置 `Config.SubscriptionID`。
- 本地开发可设置 `Config.EmulatorEndpoint = "localhost:8085"`。

## 观测与日志

- 日志字段：`topic` / `event_id` / `ordering_key` / `subscription` / `message_id` / `delivery_attempt` / `latency_ms`。
- 指标：`pubsub_publish_total`、`pubsub_publish_latency_ms`、`pubsub_publish_payload_bytes`、`pubsub_receive_total`、`pubsub_handler_duration_ms`、`pubsub_ack_latency_ms`、`pubsub_delivery_attempt_total`。
- `Config.EnableLogging` 与 `Config.EnableMetrics`（默认 `true`）可分别关闭日志或指标。
- Exactly once 交付需在 GCP 端更新 Subscription 配置；组件会在启用 Emulator 时自动关闭该标志，避免与本地环境冲突。

## Emulator 支持

当 `Config.EmulatorEndpoint` 非空时：

- 使用 `option.WithEndpoint` + `option.WithoutAuthentication`
- 自动开启 `DialOptions.Insecure`
- 强制关闭 `ExactlyOnceDelivery`（Emulator 不支持）

## 下一步

详细设计与 TODO 请参考 [`gcpubsub-design.md`](../gcpubsub-design.md)。
