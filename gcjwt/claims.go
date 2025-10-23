// Package gcjwt 提供符合 Cloud Run 最佳实践的 JWT 认证中间件。
//
// 设计目标：
//   - 专注服务间认证（Service-to-Service）
//   - 自动管理 Google Cloud Run OIDC ID Token
//   - 无缝集成 Kratos 中间件体系
//   - 本地开发友好（支持跳过验证）
//
// 核心组件：
//   - Client: 客户端中间件，自动注入 ID Token
//   - Server: 服务端中间件，提取并验证 Claims
//   - TokenSource: 封装 Token 获取逻辑（缓存、自动刷新）
//   - CloudRunClaims: Cloud Run ID Token 的 Claims 结构
//
// 快速开始：
//
// 客户端（调用方）：
//
//	conn, err := kgrpc.Dial(ctx,
//	    kgrpc.WithEndpoint("service-b.run.app:443"),
//	    kgrpc.WithMiddleware(
//	        gcjwt.Client(
//	            gcjwt.WithAudience("https://service-b.run.app/"),
//	            gcjwt.WithClientLogger(logger),
//	        ),
//	    ),
//	)
//
// 服务端（被调方）：
//
//	srv := grpc.NewServer(
//	    grpc.Middleware(
//	        gcjwt.Server(
//	            gcjwt.WithExpectedAudience("https://my-service.run.app/"),
//	            gcjwt.WithServerLogger(logger),
//	        ),
//	    ),
//	)
//
// 业务逻辑中使用 Claims：
//
//	func (h *Handler) MyMethod(ctx context.Context, req *pb.Request) (*pb.Response, error) {
//	    claims, ok := gcjwt.FromContext(ctx)
//	    if ok {
//	        log.Infof("caller: %s", claims.Email)
//	    }
//	    // ...
//	}
//
// 本地开发配置：
//
//	// 客户端禁用
//	gcjwt.Client(
//	    gcjwt.WithClientDisabled(true),
//	)
//
//	// 服务端跳过验证
//	gcjwt.Server(
//	    gcjwt.WithSkipValidate(true),
//	    gcjwt.WithTokenRequired(false),
//	)
//
// 安全模型：
//
// gcjwt 依赖 Cloud Run 的三层安全防护：
//  1. Network: TLS/HTTP2 加密传输
//  2. Identity: Google 签发的 OIDC ID Token
//  3. Authorization: IAM roles/run.invoker 权限检查
//
// 应用层职责：
//   - 提取 Claims 用于业务逻辑（审计、权限控制）
//   - 验证 audience 和过期时间（Cloud Run 已验签）
//
// 性能特性：
//   - Token 缓存约 1 小时，自动刷新
//   - 延迟初始化，避免不必要的开销
//   - 线程安全，支持高并发场景
package gcjwt

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// CloudRunClaims 表示 Cloud Run OIDC ID Token 中应用层关注的字段。
type CloudRunClaims struct {
	Subject         string `json:"sub"`
	Audience        string `json:"aud"`
	Email           string `json:"email"`
	IssuedAt        int64  `json:"iat"`
	ExpiresAt       int64  `json:"exp"`
	AuthorizedParty string `json:"azp,omitempty"`
}

type contextKey struct{}

// NewContext 将 CloudRunClaims 注入到上下文，供后续 handler 使用。
func NewContext(ctx context.Context, claims *CloudRunClaims) context.Context {
	return context.WithValue(ctx, contextKey{}, claims)
}

// FromContext 从上下文中提取 CloudRunClaims。
func FromContext(ctx context.Context) (*CloudRunClaims, bool) {
	claims, ok := ctx.Value(contextKey{}).(*CloudRunClaims)
	return claims, ok
}

// Validate 对 Claims 的基础字段执行最小化校验。
//
// 校验项：
//   - audience: 若 expectedAudience 非空，检查是否匹配
//   - 过期时间: 检查 Token 是否已过期（now >= exp）
//   - email: 检查 email 字段是否存在（Service Account 身份）
//
// 参数：
//   - expectedAudience: 期望的 audience 值，传空字符串则跳过 audience 检查
//
// 返回错误类型：
//   - ErrInvalidAudience: audience 不匹配
//   - ErrTokenExpired: Token 已过期
//   - ErrMissingEmail: 缺少 email 字段
//
// 注意：
//   - 本方法不验证签名（Cloud Run 入口已验签）
//   - 生产环境建议使用 ValidateWithLogging 以便调试
func (c *CloudRunClaims) Validate(expectedAudience string) error {
	if expectedAudience != "" && c.Audience != expectedAudience {
		return ErrInvalidAudience
	}

	now := time.Now().Unix()
	if now >= c.ExpiresAt {
		return fmt.Errorf("%w: expired at %v (now %v)", ErrTokenExpired,
			time.Unix(c.ExpiresAt, 0), time.Unix(now, 0))
	}

	if c.Email == "" {
		return ErrMissingEmail
	}

	return nil
}

// ValidateWithLogging 与 Validate 行为一致，但会将验证失败的详细信息记录到日志。
//
// 适用场景：
//   - 服务端中间件（需要调试信息）
//   - 生产环境排查认证问题
//
// 与 Validate 的区别：
//   - 验证失败时记录详细的 audience 和过期时间信息
//   - 错误返回值相同，但日志包含更多上下文
//
// 参数：
//   - expectedAudience: 期望的 audience
//   - logger: Kratos log.Helper 实例
func (c *CloudRunClaims) ValidateWithLogging(expectedAudience string, logger *log.Helper) error {
	if expectedAudience != "" && c.Audience != expectedAudience {
		logger.Warnf("audience mismatch: got=%q want=%q", c.Audience, expectedAudience)
		return ErrInvalidAudience
	}

	now := time.Now().Unix()
	if now >= c.ExpiresAt {
		logger.Warnf("token expired: exp=%v now=%v", time.Unix(c.ExpiresAt, 0), time.Unix(now, 0))
		return fmt.Errorf("%w: expired at %v", ErrTokenExpired, time.Unix(c.ExpiresAt, 0))
	}

	if c.Email == "" {
		logger.Warn("token missing email claim")
		return ErrMissingEmail
	}

	return nil
}

// NOTE: 如果业务需要允许匿名调用，可结合 WithTokenRequired(false) 并在 handler 中对 ErrMissingEmail 做兼容处理。

// IsExpired 判断 Token 是否已过期。
//
// 返回：
//   - true: Token 已过期（now >= exp）
//   - false: Token 仍然有效
func (c *CloudRunClaims) IsExpired() bool {
	return time.Now().Unix() >= c.ExpiresAt
}

// GetExpiresIn 返回距离过期的剩余时间。
//
// 返回：
//   - 正值: Token 仍有效，表示剩余时间
//   - 负值: Token 已过期，表示过期时长
//
// 使用示例：
//
//	if claims.GetExpiresIn() < 5*time.Minute {
//	    log.Warn("Token 即将过期")
//	}
func (c *CloudRunClaims) GetExpiresIn() time.Duration {
	return time.Until(time.Unix(c.ExpiresAt, 0))
}

// String 返回 Claims 的可读字符串表示，用于日志和调试。
//
// 输出格式：
//
//	CloudRunClaims{email=service-a@project.iam.gserviceaccount.com, aud=https://..., exp=2025-01-22T10:00:00Z}
func (c *CloudRunClaims) String() string {
	return fmt.Sprintf("CloudRunClaims{email=%s, aud=%s, exp=%s}",
		c.Email, c.Audience, time.Unix(c.ExpiresAt, 0).UTC().Format(time.RFC3339))
}
