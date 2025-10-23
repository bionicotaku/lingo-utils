# gcjwt · Cloud Run 微服务 JWT 中间件

> 本组件为 Kratos 微服务在 Google Cloud Run 内部进行 **gRPC 服务间调用** 时提供统一的 ID Token 获取与校验能力。本文档汇总了当前实现的主要设计、接入步骤与调试方法。

---

## 核心特性

- **Cloud Run 原生支持**：客户端通过 `idtoken.NewTokenSource` 自动获取并缓存 Google OIDC ID Token，服务端依赖 Cloud Run 入口验签，仅在应用层复核 `aud/exp/email`。
- **Kratos 中间件接口**：提供 `middleware.Middleware` 形式的 `Client` / `Server` 方法，配套 Option 及 Wire Provider，可直接接入现有链路。
- **最小化实现**：专注服务间鉴权，无额外自定义密钥或冗余校验逻辑；支持跳过验证、匿名放行等开发期选项。
- **完备单元测试**：涵盖 Claims 校验、TokenSource 注入、客户端/服务端关键分支，确保行为稳定。

---

## 目录结构

```
lingo-utils/gcjwt/
├── claims.go            # 定义 CloudRunClaims 与上下文辅助方法
├── errors.go            # 统一错误常量
├── token_source.go      # 包装 idtoken.NewTokenSource
├── client.go            # Kratos 客户端中间件
├── server.go            # Kratos 服务端中间件
├── config.go            # ClientConfig / ServerConfig 及校验
├── provider.go          # Wire ProviderSet
├── README.md            # 本文档
├── TODO.md              # 实现路线图与后续任务
└── test/                # 单元测试
    ├── claims_test.go
    ├── token_source_test.go
    ├── client_test.go
    ├── server_test.go
    └── mocks_test.go    # 测试专用 header/transport mock
```

---

## 环境与依赖

- Go 1.22+
- Kratos v2.9.x
- Google Cloud Run（服务需开启 “需要身份验证”）
- 可用的服务账号，已授予目标服务 `roles/run.invoker`

### module 依赖

`go.mod` 中已经声明关键库：

```bash
go get google.golang.org/api/idtoken@latest
go get github.com/go-kratos/kratos/v2@v2.9.1
go get github.com/google/wire@v0.6.0       # 可选，仅在使用 Wire 时需要
```

项目本身在 `lingo-utils` 目录下初始化了独立 module，若作为子模块引入其它服务，请确保 `go.work` 已包含该路径。

---

## 快速开始

### 1. 安装

在服务仓库中引用：

```bash
go get github.com/bionicotaku/lingo-utils/gcjwt
```

### 2. 客户端中间件

```go
import (
    gcjwt "github.com/bionicotaku/lingo-utils/gcjwt"
    kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
)

conn, err := kgrpc.Dial(ctx,
    kgrpc.WithEndpoint("service-b.run.app:443"),
    kgrpc.WithMiddleware(
        gcjwt.Client(
            gcjwt.WithAudience("https://service-b.run.app/"),
            gcjwt.WithClientLogger(logger),
        ),
    ),
)
```

默认会把 `Bearer <token>` 写入 `authorization` header。若本地调试需要禁用，可追加 `gcjwt.WithClientDisabled(true)`。

### 3. 服务端中间件

```go
import (
    gcjwt "github.com/bionicotaku/lingo-utils/gcjwt"
    "github.com/go-kratos/kratos/v2/transport/grpc"
)

srv := grpc.NewServer(
    grpc.Middleware(
        gcjwt.Server(
            gcjwt.WithExpectedAudience("https://service-b.run.app/"),
            gcjwt.WithServerLogger(logger),
        ),
    ),
)
```

服务端会从 `authorization` 读取 Token，若为空则回退 `X-Serverless-Authorization`。校验通过后 `CloudRunClaims` 会写入 context，可通过 `gcjwt.FromContext(ctx)` 获取调用方邮箱等信息。

---

## 配置整合

### 结构体

```go
type ClientConfig struct {
    Audience  string
    Disabled  bool
    HeaderKey string
}

type ServerConfig struct {
    ExpectedAudience string
    SkipValidate     bool
    Required         bool
    HeaderKey        string
}

type Config struct {
    Client *ClientConfig
    Server *ServerConfig
}
```

`Validate()` 会确保在启用模式下必填字段存在；`Config` 用于一次性传入客户端/服务端配置，方便 `NewComponent` 构建中间件。

### 示例 YAML

```yaml
# configs/config.yaml
client:
  jwt:
    audience: "https://service-b.run.app/"
    disabled: false

server:
  jwt:
    expected_audience: "https://service-b.run.app/"
    skip_validate: false
    required: true
```

### Wire 集成

```go
import (
    gcjwt "github.com/bionicotaku/lingo-utils/gcjwt"
    "github.com/google/wire"
)

var ProviderSet = wire.NewSet(
    configloader.ProviderSet,   // 负责生成 gcjwt.Config
    gcjwt.ProviderSet,          // 构造 JWT 组件并暴露中间件
    grpcserver.ProviderSet,     // 在 gRPC Server 中挂载 ServerMiddleware
)
```

`gcjwt.ProviderSet` 现在输出：

- `*gcjwt.Component`：聚合客户端/服务端中间件；
- `gcjwt.ClientMiddleware`、`gcjwt.ServerMiddleware`：若配置缺失则为 `nil`；
- `error` 会在 Wire 生成代码中显式返回，保持与其他组件一致的错误处理流程。

若服务无需出站调用，可仅在 gRPC Server 构造函数中注入 `gcjwt.ServerMiddleware`，未启用一侧不必额外配置。

---

## TokenSource 行为说明

- 首次调用 `Token()` 时通过 `idtoken.NewTokenSource(ctx, audience)` 创建底层 `oauth2.TokenSource`。
- 后续调用复用同一个对象，依赖官方内部缓存与刷新策略（默认提前 225 秒更新 Token），无需自建缓存。
- 失败场景会返回：
  - `ErrTokenSourceInit`：初始化失败（例如 Metadata Server 不可达）。
  - `ErrTokenAcquire`：获取 Token 失败。
- 测试可用 `gcjwt.SetTokenSourceFactory` 注入假工厂，方便模拟异常。

> 建议始终传入带超时的 `context`：`ctx, cancel := context.WithTimeout(ctx, 5*time.Second)`，避免 Metadata Server 长时间阻塞。

---

## 服务端中间件流程

1. 提取 Header（`authorization` → `X-Serverless-Authorization` 回退）。
2. 按 JWT 三段结构拆解 payload，反序列化为 `CloudRunClaims`。
3. 在非跳过模式下执行 `ValidateWithLogging`：
   - `aud` 必须匹配 `WithExpectedAudience`。
   - 当前时间必须小于 `exp`。
   - `email` 不得为空（可结合 `WithTokenRequired(false)` 放宽要求）。
4. 通过 `NewContext` 写入 context 并调用实际 handler。

若缺少 Token、格式错误或校验失败，将返回前文定义的 Kratos 错误：

| 错误 | 场景 |
| ---- | ---- |
| `ErrMissingToken` | 请求未携带 ID Token |
| `ErrInvalidTokenFormat` | Header 非 `Bearer xxx` 格式 |
| `ErrTokenParseFail` | JWT 解码失败（payload 非合法 Base64/JSON） |
| `ErrInvalidAudience` | aud 与期望不一致 |
| `ErrTokenExpired` | Token 过期 |
| `ErrMissingEmail` | Token 未包含 email |

---

## 客户端中间件行为

- 默认注入到 `authorization`，可通过 `WithHeaderKey` 覆盖。
- 初始化失败或 Token 获取失败会返回错误，调用直接终止。
- `WithClientDisabled(true)` 可用于本地跳过，生产环境应保持默认。
- 如果 `transport.FromClientContext` 返回 false，会报错提醒需在 Kratos gRPC 客户端上下文中使用。

---

## 调试与排错

### 常见错误

| 错误 | 排查建议 |
| ---- | -------- |
| `invalid audience` | 确认客户端配置的 `audience` 与服务端 Cloud Run URL 完全一致。 |
| `token expired` | 检查调用时间是否超过 1 小时，或机器时钟是否漂移。 |
| `missing authorization header` | 确认调用方在 Cloud Run 环境运行，或本地已通过 `gcloud auth print-identity-token` 获取 Token。 |
| `failed to initialize ID token source` | 通常为本地缺少 ADC 凭据，执行 `gcloud auth application-default login`。 |

### 调试脚本

调试脚本可参考下述示例（已在“调试脚本”一节描述），执行后可解析 Token payload，比对 `aud/exp/email`。

---

## 单元测试

在 `lingo-utils` 目录运行：

```bash
go test ./...
```

测试要点：

- `claims_test.go`：校验逻辑。
- `token_source_test.go`：工厂注入与缓存复用。
- `client_test.go`：Header 注入、禁用模式、错误路径。
- `server_test.go`：解析成功、各种异常、开发模式跳过校验。

（可在未来补充 `integration` 标签测试，对接真实 Cloud Run 环境。）

---

## 使用注意事项

1. **不要重新验签**：Cloud Run 入口已经完成验签与 IAM 校验，应用层无需再次处理。
2. **严格管理 Audience**：服务端 `WithExpectedAudience()` 与 Cloud Run 控制台保持一致，避免上线后突然 401。
3. **匿名/跳过校验谨慎使用**：
   - `WithTokenRequired(false)`：适合健康检查/公开 API，但需要业务层防止越权。
   - `WithSkipValidate(true)`：仅在本地或集成测试启用，启动时应输出 WARN。
4. **日志隐私**：中间件日志仅输出 email、audience，若有进一步审计需求，可在业务层追加结构化日志，但切勿打印 Token 原文。

---

## 后续工作

详细的迭代计划与待办集中在 `TODO.md`，当前主要包括：

- 可选的 Cloud Run 实机集成测试。
- 如需多 audience 或额外协议支持，需先确认业务场景，再在 README 补充扩展说明。

---

## FAQ

**Q: 本地开发如何拿到 ID Token？**  
使用 `gcloud auth application-default login` 获取 ADC；客户端中间件会自动从本地凭据生成 Token，也可通过调试脚本手动获取。

**Q: 为什么不验证 issuer 或 email_verified？**  
Cloud Run 服务账号 Token 默认不包含 `email_verified`，issuer 亦已由平台保证，为避免拒绝合法请求，仅保留 `aud/exp/email` 的最小校验。

**Q: 如何处理匿名请求？**  
可以设置 `WithTokenRequired(false)` 并在 handler 中容忍 `ErrMissingEmail`，但务必在业务逻辑中明确区分匿名权限。

---

## 许可证

遵循仓库主项目的许可协议（若未指定，则默认 MIT/Apache-2.0 等开源协议，请参考根目录 README）。

---

## 维护者

- 初始实现：后端平台团队（2025-10）
- 后续改动请同步更新 `TODO.md` 与本 README，确保文档与代码一致。
