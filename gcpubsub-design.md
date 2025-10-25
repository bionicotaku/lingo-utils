# GCP Pub/Sub 组件详细设计

> 目标：在 `lingo-utils` 中提供统一的 Google Cloud Pub/Sub 发布与 StreamingPull 消费能力，复用现有组件模式（Config → Component → ProviderSet），并内置日志与指标。该组件将服务于 `kratos-template` 及其他微服务，实现可靠的 Outbox Publisher 与读模型消费者。

---

## 1. 设计原则

- **遵循组件模式**：仿照 `gclog` / `observability` / `txmanager`，以 `Config`、`Dependencies`、`Component`、`ProviderSet` 组织，返回 `cleanup func()`。
- **显式依赖**：禁止 `[]option.ClientOption` 这类不定参数，所有外部依赖（凭证、时钟、工厂函数）都在 `Dependencies` 中明确定义，可为空则提供默认实现。
- **横切能力内置**：组件内部负责结构化日志、OpenTelemetry 指标与 Trace，调用方无需重复埋点。
- **业务解耦**：消息载荷、属性编码、重试策略等业务逻辑仍由各服务实现；组件只处理 Cloud Pub/Sub 交互与观测。
- **兼容本地/云环境**：同时支持 GCP 正式环境、ADC、本地 Emulator；配置项明确控制行为。

---

## 2. Config 结构

```go
type Config struct {
    ProjectID   string         `json:"projectID" yaml:"projectID"`
    TopicID     string         `json:"topicID" yaml:"topicID"`
    SubscriptionID string      `json:"subscriptionID" yaml:"subscriptionID"`

    PublishTimeout time.Duration `json:"publishTimeout" yaml:"publishTimeout"`
    OrderingKeyEnabled *bool     `json:"orderingKeyEnabled" yaml:"orderingKeyEnabled"`

    EnableLogging *bool `json:"enableLogging" yaml:"enableLogging"`
    EnableMetrics *bool `json:"enableMetrics" yaml:"enableMetrics"`
    MeterName     string `json:"meterName" yaml:"meterName"`

    EmulatorEndpoint string `json:"emulatorEndpoint" yaml:"emulatorEndpoint"`

    Receive ReceiveConfig `json:"receive" yaml:"receive"`
    ExactlyOnceDelivery bool `json:"exactlyOnceDelivery" yaml:"exactlyOnceDelivery"`
}

type ReceiveConfig struct {
    NumGoroutines          int           `json:"numGoroutines" yaml:"numGoroutines"`
    MaxOutstandingMessages int           `json:"maxOutstandingMessages" yaml:"maxOutstandingMessages"`
    MaxOutstandingBytes    int           `json:"maxOutstandingBytes" yaml:"maxOutstandingBytes"`
    MaxExtension           time.Duration `json:"maxExtension" yaml:"maxExtension"`
    MaxExtensionPeriod     time.Duration `json:"maxExtensionPeriod" yaml:"maxExtensionPeriod"`
}
```

### 2.1 默认策略

- `PublishTimeout`: 默认 10s。
- `OrderingKeyEnabled`: 默认 true，使用指针区分“未配置”和“显式关闭”。
- `EnableLogging` / `EnableMetrics`: 默认 true；使用指针字段区分“未配置”和“显式关闭”。
- `MeterName`: 默认 `lingo-utils.gcpubsub`.
- `Receive.NumGoroutines`: 默认 1（遵循官方 StreamingPull 指南）。
- `Receive.MaxOutstandingMessages`: 默认 1000；`MaxOutstandingBytes`: 默认 64MiB。
- `MaxExtension`: 默认 60s；`MaxExtensionPeriod`: 默认 10m。
- `ExactlyOnceDelivery`: 默认 false（只有在 Pull/StreamingPull 且确需幂等保障时开启）。
- `EmulatorEndpoint` 为空时走正式环境；非空时使用 Emulator 并禁用 TLS。

`Config.Normalize()` 负责填充这些默认值并保证互斥约束（例如 Emulator 时忽略 Exactly-once）。

---

## 3. Dependencies

```go
type Dependencies struct {
    Logger log.Logger
    Meter  metric.Meter
    Tracer trace.Tracer
    Clock  func() time.Time

    CredentialsJSON []byte // nil 表示使用 ADC / 环境默认

    Dial DialOptions
    ClientFactory ClientFactory
}

type DialOptions struct {
    GRPCConnPoolSize int
    Insecure         bool
}

type ClientFactory func(ctx context.Context, projectID string, creds Credentials, dial DialOptions) (*pubsub.Client, error)
```

- `Credentials` 将根据 `CredentialsJSON` 与 `EmulatorEndpoint` 自动推导（若使用 Emulator 则忽略凭证）。
- 若 `ClientFactory` 为空，使用默认实现（`pubsub.NewClient` + `option.WithCredentialsJSON` 等）。
- `DialOptions.Insecure` 在 Emulator 模式自动设为 true；`GRPCConnPoolSize` 默认 4（官方推荐值）。
- `sanitizeDependencies` 与 `txmanager` 一致：缺省 logger → `log.NewStdLogger(io.Discard)`；meter / tracer → OTel 全局；clock → `time.Now`。

---

## 4. Component & ProviderSet

```go
type Component struct {
    client        *pubsub.Client
    topic         *pubsub.Topic
    subscription  *pubsub.Subscription
    logger        *log.Helper
    telemetry     *telemetry
}

func NewComponent(ctx context.Context, cfg Config, deps Dependencies) (*Component, func(), error)

func ProvidePublisher(c *Component) Publisher
func ProvideSubscriber(c *Component) Subscriber

var ProviderSet = wire.NewSet(NewComponent, ProvidePublisher, ProvideSubscriber)
```

- `telemetry` 将封装 OTel 指标、logger。
- `cleanup` 调 `client.Close()` 并 Flush 未发送消息。
- 如果只配置 Topic（无订阅），Subscriber 返回 `nil`，Wire 注入时由上层决定是否依赖。

---

## 5. 发布接口

```go
type Message struct {
    Data        []byte
    Attributes  map[string]string
    OrderingKey string
    EventID     string    // 可选，仅用于日志
    ID          string    // 消费端填充：Pub/Sub Message ID
    PublishTime time.Time // 消费端填充：消息发布时间
    DeliveryAttempt int   // 消费端填充：递送尝试次数（默认 0）
}

type Publisher interface {
    Publish(ctx context.Context, msg Message) (serverID string, err error)
    Flush(ctx context.Context) error
}
```

实现要点：

1. 复用单一 `*pubsub.Topic`，每次 `Publish` 都调用 `topic.Publish` 并同步等待 `result.Get(ctx)`。
2. 结合 `cfg.PublishTimeout` 设置 `context.WithTimeout`。
3. 若 `OrderingKeyEnabled` 为 false，应清空 `Message.OrderingKey` 防止客户端报错。
4. 日志字段：`topic`, `event_id`, `ordering_key`, `latency_ms`, `server_id`（成功），`error_kind`（失败）。
5. 指标：`pubsub_publish_total`（labels：topic, result），`pubsub_publish_latency_ms`、`pubsub_publish_payload_bytes`。

---

## 6. StreamingPull 接口

```go
type Subscriber interface {
    Receive(ctx context.Context, handler func(context.Context, *pubsub.Message) error) error
    Stop()
}
```

实现要点：

1. 使用 `subscription.Receive`，加载 `ReceiveSettings`：
   - `NumGoroutines = cfg.Receive.NumGoroutines`（默认 1）；
   - `MaxOutstandingMessages`, `MaxOutstandingBytes`；
   - `MaxExtension`, `MaxExtensionPeriod`；
   - `EnableExactlyOnceDelivery = cfg.ExactlyOnceDelivery`（仅正式环境）。
2. Handler 包装：
   - 记录开始时间；
   - 捕获 panic，转为 error；
   - 调用用户 handler；
   - 成功后 `msg.Ack()` 并记录 ack 延迟；失败时 `msg.Nack()` 或保留未 Ack（由 handler 返回自定义错误决定）。
3. 日志字段：`subscription`, `message_id`, `delivery_attempt`, `processing_ms`, `handler_error`.
4. 指标：`pubsub_receive_total{subscription, result}`, `pubsub_ack_latency_ms`, `pubsub_delivery_attempt`, `pubsub_handler_duration_ms`。
5. `Stop()` 调 `subscription.Stop()`，供优雅关闭使用。

---

## 7. Emulator / 凭证处理

- 若 `cfg.EmulatorEndpoint` 非空：
  - 强制 `DialOptions.Insecure = true`；
  - 使用 `option.WithEndpoint(endpoint)` + `option.WithoutAuthentication`；
  - 忽略 `CredentialsJSON`；
  - `ExactlyOnceDelivery` 自动关闭（Emulator 不支持）。
- 否则：
  - 若提供 `CredentialsJSON`，使用 `google.CredentialsFromJSON` 创建 `option.WithCredentials`；
  - 否则默认走 ADC（环境变量 / gcloud auth）。

---

## 8. 日志与指标

### 8.1 日志

- 使用 `log.Helper` 输出结构化 JSON（依赖外部 `gclog`）。
- 成功级别 `DEBUG`（发布）或 `INFO`（消费），失败 `WARN/ERROR`。
- 统一字段：
  - 发布：`topic`, `event_id`, `ordering_key`, `publish_latency_ms`, `pubsub_msg_id`, `error_kind`.
  - 消费：`subscription`, `message_id`, `delivery_attempt`, `handler_latency_ms`, `ack_latency_ms`, `error_kind`.

### 8.2 指标

- 采用 `cfg.MeterName` 注册 Histogram / Counter：
  - `pubsub_publish_total`、`pubsub_publish_latency_ms`、`pubsub_publish_payload_bytes`.
  - `pubsub_receive_total`、`pubsub_handler_duration_ms`、`pubsub_ack_latency_ms`、`pubsub_delivery_attempt_total`.
- 指标开关由 `cfg.EnableMetrics` 控制；若禁用则仍创建 no-op 结构避免分支散落。

---

## 9. Wire 集成示例

```go
var gcpPubsubProvider = wire.NewSet(
    configloader.ProvidePubsubConfig,        // 返回 gcpubsub.Config
    configloader.ProvidePubsubDependencies,  // 返回 gcpubsub.Dependencies
    gcpubsub.ProviderSet,                    // 注入 Publisher/Subscriber
)
```

在 `cmd/grpc/wire.go` 中引入：

```go
wire.Build(
    ...,
    gcpPubsubProvider,
    // 业务任务依赖 Publisher / Subscriber
)
```

---

## 10. 测试策略

1. **单元测试**（`gcpubsub/test` 包）：
   - 模拟 `ClientFactory`，验证 Publish/Receive 调用顺序、超时处理、指标计数。
   - 模拟 Emulator/凭证场景，断言 `Insecure` / 认证选项选择正确。
2. **集成测试**：
   - 使用官方 Pub/Sub Emulator，覆盖：
     - 发布成功 / 超时 / 错误；
     - StreamingPull：处理成功、handler 报错、ack 超时；
     - Exactly-once 关闭情况下的重复消息。
3. **混沌演练**：
   - 断网 / 客户端关闭 / handler panic，验证组件日志与指标输出。
4. **文档验证**：
   - README 中提供配置示例、Wire 集成、Emulator 启动脚本、指标名称说明。

---

## 11. 实施计划（摘要）

1. 建立 `gcpubsub` 目录，编写 `config.go`、`dependencies.go`、`telemetry.go`、`publisher.go`、`subscriber.go`、`provider.go`。
2. 更新 `lingo-utils/go.mod` 引入 `cloud.google.com/go/pubsub`、`google.golang.org/api/option`。
3. 在 `kratos-template/internal/infrastructure/config_loader` 增加配置映射，Wire 导入新 Provider。
4. 调整阶段四 Outbox Publisher 使用 `gcpubsub.Publisher`；阶段五消费者使用 `gcpubsub.Subscriber`。
5. 编写 README / 示例 / 集成测试，确保 `go test ./...` 和 `make lint` 通过。

---

## 12. 开放问题

- 是否需要在组件层内置 Dead-letter 回调或仅记录指标交由上层处理。
- 是否提前支持订阅端的 `Seek`/回放；若启用需暴露额外方法（可后续演进）。
- Exactly-once 模式的重试策略是否由组件提供默认实现，还是完全交给 handler（当前建议后者）。

---

## 13. TODO 列表

- [x] 建立 `lingo-utils/gcpubsub` 目录并创建骨架文件：
  - [x] `config.go`（定义结构体与注释，占位 `Normalize()` 实现）
  - [x] `dependencies.go`（占位 `Dependencies` / `DialOptions` / `ClientFactory` 类型）
  - [x] `telemetry.go`（定义结构体和 TODO 注释）
  - [x] `publisher.go`、`subscriber.go`（空实现 + 接口定义）
  - [x] `provider.go`（`Component`、`ProviderSet` 占位）
  - [x] `README.md`（记录组件目标与 TODO）
  - [x] `test/` 目录（创建 `README.md` 或占位测试文件）
- [x] 实现 `config.go`：补齐默认值、`Normalize()`、校验逻辑。
  - [x] 默认常量定义（PublishTimeout 等）
  - [x] 合并 ReceiveConfig 默认值
  - [x] Emulator 与 ExactlyOnce 互斥规则
- [x] 实现 `dependencies.go`：完成 `sanitizeDependencies`、凭证与 ClientFactory 默认实现。
  - [x] 引入 `cloud.google.com/go/pubsub`、`google.golang.org/api/option`
  - [x] 支持 Emulator/ADC/JSON 凭证三种模式
  - [x] 为测试暴露 `ClientFactory`
- [x] 实现 `telemetry.go`：封装指标注册与记录方法，支持开关。
  - [x] 定义指标名称常量
  - [x] 记录发布/消费时长、payload、ACK 延迟
- [x] 完成 `publisher.go`：实现 Publish/Flush、日志/指标、OrderingKey 处理。
  - [x] 构造 `pubsub.Topic` 包装器
  - [x] 支持上下文超时与错误分类
- [x] 完成 `subscriber.go`：实现 Receive/Stop、包装 handler、指标/日志输出。
  - [x] 配置 ReceiveSettings、流控
  - [x] handler 包裹 panic/错误
  - [x] Ack/Nack 与指标采集
- [x] 实现 `provider.go`：构建 `Component`、`NewComponent`、`ProvidePublisher/Subscriber`，含 cleanup。
  - [x] 注入 logger、telemetry
  - [x] 发布/消费均可按需禁用（nil 时返回 noop）
- [x] 编写 `README.md`：提供配置示例、Emulator 用法、指标字段说明。
- [x] 在 `lingo-utils` `go.mod`/`go.sum` 中加入 `cloud.google.com/go/pubsub`、`google.golang.org/api/option` 依赖。
- [x] 编写单元测试与集成测试（覆盖 Config Normalize 及 pstest 流程；后续可扩展更多场景）。
- [x] 增补测试用例：
  - [x] 发布禁用场景（无 Topic）返回预期错误。
  - [x] OrderingKey 关闭时消费者收到空 key。
  - [x] 处理函数返回错误时触发 Nack 并重新投递。
- [x] 追加测试：
  - [x] 发布前上下文已取消时返回 context 错误。
  - [x] 消费处理函数 panic 被捕获并允许重投。
- [x] 编写集成测试（基于 pstest 模拟器）验证发布/消费链路。
- [ ] 运行 `go test ./...`、`make lint`，确认通过并记录结果。
- [ ] 更新 `kratos-template/internal/infrastructure/config_loader` 与 Wire，将 gcpubsub 注入 Outbox/Projection 流程。
- [ ] 阶段四/五 实施时，将原计划的 `internal/infrastructure/pubsub` 替换为新组件，并复查设计文档。

---

## 14. 实施进度

| 步骤 | 描述 | 状态 | 备注 |
| ---- | ---- | ---- | ---- |
| 1 | 细化设计文档与任务拆解 | ✅ 完成 | 2025-10-25 |
| 2 | 创建 `gcpubsub` 目录与骨架文件 | ✅ 完成 | 2025-10-25 |
| 3 | 实现核心源码（config/dependencies/telemetry/publisher/subscriber/provider） | ✅ 完成 | 2025-10-25 |
| 4 | 文档/测试/依赖集成 | ✅ 完成 | 2025-10-25（集成测试待步骤 5） |
| 5 | 测试验证与服务集成 | ⏳ 进行中 | 准备运行 go test / 更新 kratos-template 配置 |
