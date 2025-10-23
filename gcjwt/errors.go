package gcjwt

import (
	"errors"

	kerrors "github.com/go-kratos/kratos/v2/errors"
)

const errorDomain = "gcjwt"

var (
	// ErrMissingToken 表示请求头缺少授权信息。
	ErrMissingToken = kerrors.Unauthorized(errorDomain, "missing authorization header")

	// ErrInvalidTokenFormat 表示 Authorization 不符合 Bearer 规范。
	ErrInvalidTokenFormat = kerrors.Unauthorized(errorDomain, "invalid token format, expected 'Bearer <token>'")

	// ErrTokenParseFail 表示 JWT payload 解析失败。
	ErrTokenParseFail = kerrors.Unauthorized(errorDomain, "failed to parse token payload")

	// ErrInvalidAudience 表示 aud 与预期不匹配。
	ErrInvalidAudience = kerrors.Unauthorized(errorDomain, "invalid audience")

	// ErrTokenExpired 表示 Token 已过期。
	ErrTokenExpired = kerrors.Unauthorized(errorDomain, "token expired")

	// ErrMissingEmail 表示 Token 缺少 email 字段。
	ErrMissingEmail = kerrors.Unauthorized(errorDomain, "missing email claim")

	// ErrTokenSourceInit 表示初始化 TokenSource 失败。
	ErrTokenSourceInit = errors.New("failed to initialize ID token source")

	// ErrTokenAcquire 表示获取 ID Token 失败。
	ErrTokenAcquire = errors.New("failed to acquire ID token")
)
