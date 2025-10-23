package gcjwt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
)

type serverOptions struct {
	expectedAudience string
	logger           log.Logger
	headerKey        string
	skipValidate     bool
	required         bool
}

// ServerOption 用于自定义服务器端中间件行为。
type ServerOption func(*serverOptions)

func defaultServerOptions() *serverOptions {
	return &serverOptions{
		headerKey: "authorization",
		required:  true,
	}
}

// WithExpectedAudience 指定服务端期望的 audience。
func WithExpectedAudience(aud string) ServerOption {
	return func(o *serverOptions) {
		o.expectedAudience = aud
	}
}

// WithServerLogger 注入日志器。
func WithServerLogger(logger log.Logger) ServerOption {
	return func(o *serverOptions) {
		o.logger = logger
	}
}

// WithSkipValidate 本地开发可跳过校验。
func WithSkipValidate(skip bool) ServerOption {
	return func(o *serverOptions) {
		o.skipValidate = skip
	}
}

// WithServerHeaderKey 自定义读取 Token 的 Header。
func WithServerHeaderKey(header string) ServerOption {
	return func(o *serverOptions) {
		o.headerKey = header
	}
}

// WithTokenRequired 控制是否允许匿名请求。
func WithTokenRequired(required bool) ServerOption {
	return func(o *serverOptions) {
		o.required = required
	}
}

// Server 返回 Kratos 服务端中间件，对 Cloud Run ID Token 进行解析与基础校验。
func Server(opts ...ServerOption) middleware.Middleware {
	options := defaultServerOptions()
	for _, opt := range opts {
		opt(options)
	}

	if options.logger == nil {
		options.logger = log.NewStdLogger(io.Discard)
	}
	helper := log.NewHelper(log.With(options.logger, "middleware", "gcjwt.server"))

	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			token, err := extractToken(ctx, options.headerKey)
			if err != nil {
				if options.skipValidate {
					helper.Warnf("skip validate enabled, ignore token error: %v", err)
					return next(ctx, req)
				}
				if !options.required {
					helper.Debugf("token not required, bypassing error: %v", err)
					return next(ctx, req)
				}
				return nil, err
			}

			claims, err := parseTokenClaims(token)
			if err != nil {
				helper.Errorf("parse token claims: %v", err)
				return nil, ErrTokenParseFail
			}

			if !options.skipValidate {
				if err := claims.ValidateWithLogging(options.expectedAudience, helper); err != nil {
					return nil, err
				}
			}

			ctx = NewContext(ctx, claims)
			helper.Debugf("authenticated request from email=%s aud=%s", claims.Email, claims.Audience)

			return next(ctx, req)
		}
	}
}

func extractToken(ctx context.Context, headerKey string) (string, error) {
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return "", ErrMissingToken
	}

	header := tr.RequestHeader().Get(headerKey)
	if header == "" && strings.EqualFold(headerKey, "authorization") {
		header = tr.RequestHeader().Get("x-serverless-authorization")
	}
	if header == "" {
		return "", ErrMissingToken
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", ErrInvalidTokenFormat
	}

	return strings.TrimPrefix(header, prefix), nil
}

func parseTokenClaims(token string) (*CloudRunClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid jwt format: expected 3 parts, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims CloudRunClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	return &claims, nil
}
