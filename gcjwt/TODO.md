# gcjwt 实现路线图

> 目标：按设计文档在 `lingo-utils/gcjwt` 下交付 Cloud Run 微服务间 gRPC JWT 中间件，并通过 Kratos 接口集成。

## 阶段 0：基础准备
- [x] **目录骨架**：创建 `claims.go`、`errors.go`、`token_source.go`、`client.go`、`server.go`、`config.go`、`provider.go` 以及 `test/` 子目录（按设计说明拆分）。
- [x] **依赖引入**：
   - `go get google.golang.org/api/idtoken`（ID Token 获取）。
   - `go get github.com/go-kratos/kratos/v2`（中间件接口、日志）。
   - `go get github.com/stretchr/testify`（单元测试）。
- [x] **go.mod 设置**：在 `lingo-utils/gcjwt` 下初始化 module（如合并到仓库根 go.work，可跳过）。

## 阶段 1：核心类型与错误
- [x] **Claims/Context**（`claims.go`）：
   - 定义 `CloudRunClaims` 结构（字段见设计文档）。
   - 实现 `NewContext` / `FromContext`。
   - 实现 `Validate(expectedAudience string) error` 与 `ValidateWithLogging(...)`（aud/exp/email 校验）。
- [x] **错误常量**（`errors.go`）：
   - 定义 `ErrMissingToken`、`ErrInvalidTokenFormat`、`ErrInvalidAudience`、`ErrTokenExpired`、`ErrMissingEmail`、`ErrTokenParseFail`、`ErrTokenSourceInit`、`ErrTokenAcquire`。

## 阶段 2：Token 获取
- [x] **TokenSource**（`token_source.go`）：
   - 结构体包含 audience、`oauth2.TokenSource`、`sync.Once`、`*log.Helper`。
   - `NewTokenSource(aud, logger)` 初始化 log helper。
   - `Token(ctx)` 内调用 `idtoken.NewTokenSource`（once），返回 `ts.Token()` 的 `AccessToken`。
   - 记录必要日志（init 成功、获取失败等）。

## 阶段 3：中间件实现
- [x] **客户端中间件**（`client.go`）：
   - `ClientOption` 设计：`WithAudience`、`WithClientLogger`、`WithHeaderKey`、`WithClientDisabled`。
   - `Client(opts...)`：构造 TokenSource，拦截请求，从 `transport.FromClientContext` 写入 `Bearer <token>`。
   - 错误处理：Token 获取失败直接返回；`transport` 缺失返回显式错误。
- [x] **服务端中间件**（`server.go`）：
   - `ServerOption`：`WithExpectedAudience`、`WithServerLogger`、`WithSkipValidate`、`WithServerHeaderKey`、`WithTokenRequired`。
   - `Server(opts...)`：解析 Header（支持回退 `X-Serverless-Authorization`），拆解 JWT（Base64 解码），调用 `ValidateWithLogging`，将 Claims 写入 context。
   - `required=false` 时允许缺失 Token；`skipValidate=true` 时打印 warn 并直接放行。

## 阶段 4：配置与依赖注入
- [x] **Config 结构**（`config.go`）：
   - `ClientConfig`、`ServerConfig` 和对应 `Validate()`。
- [x] **Wire Provider**（`provider.go`）：
   - `ProvideClientMiddleware(cfg *ClientConfig, logger log.Logger)`。
   - `ProvideServerMiddleware(cfg *ServerConfig, logger log.Logger)`。
   - `ProviderSet` 聚合两者。

- [x] **单元测试**（`test/`）
   - [x] `claims_test.go`：覆盖正常、aud mismatch、过期、缺少 email 情况。
   - [x] `token_source_test.go`：通过自定义工厂注入假 TokenSource，验证缓存与错误路径。
   - [x] `client_test.go`：验证 Header 注入、禁用模式、Token 获取失败路径。
   - [x] `server_test.go`：验证解析成功、audience 不匹配、缺少 Token、skipValidate/required=false 分支。
- [ ] **（可选）集成测试**：
   - `go test -tags=integration` 结合本地 ADC 获取真实 ID Token（参考设计文档脚本）。

## 阶段 6：示例与文档校验
1. **示例代码**（`examples/`，可选）：编写最小客户端/服务端使用示例。
2. **设计文档核对**：逐章检查 `gcjwt-design.md` 是否与实际实现一致（字段命名、日志、错误行为）。

## 阶段 7：交付前检查
1. `gofumpt -w . && goimports -w .`
2. `go test ./...`
3. 如果与 Kratos 服务集成：更新相应服务的 Wire、配置及 README。
