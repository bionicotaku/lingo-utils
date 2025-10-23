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

// Validate 对基础字段执行最小化校验。
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

// ValidateWithLogging 与 Validate 一致，但会在 logger 中输出提示信息。
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
func (c *CloudRunClaims) IsExpired() bool {
	return time.Now().Unix() >= c.ExpiresAt
}

// GetExpiresIn 返回距离过期剩余时间。
func (c *CloudRunClaims) GetExpiresIn() time.Duration {
	return time.Until(time.Unix(c.ExpiresAt, 0))
}

// String 提供调试友好的输出。
func (c *CloudRunClaims) String() string {
	return fmt.Sprintf("CloudRunClaims{email=%s, aud=%s, exp=%s}",
		c.Email, c.Audience, time.Unix(c.ExpiresAt, 0).UTC().Format(time.RFC3339))
}
