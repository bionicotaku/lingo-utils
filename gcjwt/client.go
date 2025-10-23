package gcjwt

import (
	"context"
	"fmt"
	"io"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
)

type clientOptions struct {
	audience  string
	logger    log.Logger
	headerKey string
	disabled  bool
}

// ClientOption 定义客户端中间件配置。
type ClientOption func(*clientOptions)

func defaultClientOptions() *clientOptions {
	return &clientOptions{
		headerKey: "authorization",
	}
}

// WithAudience 指定目标服务的 audience。
func WithAudience(aud string) ClientOption {
	return func(o *clientOptions) {
		o.audience = aud
	}
}

// WithClientLogger 注入日志器。
func WithClientLogger(logger log.Logger) ClientOption {
	return func(o *clientOptions) {
		o.logger = logger
	}
}

// WithHeaderKey 自定义 ID Token 写入的 Header。
func WithHeaderKey(header string) ClientOption {
	return func(o *clientOptions) {
		o.headerKey = header
	}
}

// WithClientDisabled 允许在本地禁用中间件。
func WithClientDisabled(disabled bool) ClientOption {
	return func(o *clientOptions) {
		o.disabled = disabled
	}
}

// Client 返回 Kratos 客户端中间件，为每次调用注入 Cloud Run ID Token。
func Client(opts ...ClientOption) middleware.Middleware {
	options := defaultClientOptions()
	for _, opt := range opts {
		opt(options)
	}

	if options.audience == "" {
		panic("gcjwt: audience is required for client middleware")
	}
	if options.logger == nil {
		options.logger = log.NewStdLogger(io.Discard)
	}

	helper := log.NewHelper(log.With(options.logger, "middleware", "gcjwt.client"))
	tokenSource := NewTokenSource(options.audience, options.logger)

	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			if options.disabled {
				helper.Debug("client middleware disabled, skipping token injection")
				return next(ctx, req)
			}

			token, err := tokenSource.Token(ctx)
			if err != nil {
				helper.Errorf("failed to get id token: %v", err)
				return nil, fmt.Errorf("gcjwt client: %w", err)
			}

			tr, ok := transport.FromClientContext(ctx)
			if !ok {
				helper.Error("transport not found in client context")
				return nil, fmt.Errorf("gcjwt client: transport not available in context")
			}

			tr.RequestHeader().Set(options.headerKey, "Bearer "+token)
			helper.Debugf("attached id token (audience=%s)", options.audience)

			return next(ctx, req)
		}
	}
}
