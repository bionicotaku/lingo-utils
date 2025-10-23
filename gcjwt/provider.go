package gcjwt

import (
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/google/wire"
)

// ProviderSet 暴露给 Wire 使用的中间件构造器。
var ProviderSet = wire.NewSet(
	ProvideClientMiddleware,
	ProvideServerMiddleware,
)

// ProvideClientMiddleware 构建客户端中间件实例。
func ProvideClientMiddleware(cfg *ClientConfig, logger log.Logger) middleware.Middleware {
	if cfg == nil {
		panic("gcjwt: client config is nil")
	}
	if err := cfg.Validate(); err != nil {
		panic(fmt.Errorf("gcjwt: invalid client config: %w", err))
	}

	opts := []ClientOption{
		WithClientLogger(logger),
	}
	if cfg.Audience != "" {
		opts = append(opts, WithAudience(cfg.Audience))
	}
	if cfg.HeaderKey != "" {
		opts = append(opts, WithHeaderKey(cfg.HeaderKey))
	}
	if cfg.Disabled {
		opts = append(opts, WithClientDisabled(true))
	}

	return Client(opts...)
}

// ProvideServerMiddleware 构建服务端中间件实例。
func ProvideServerMiddleware(cfg *ServerConfig, logger log.Logger) middleware.Middleware {
	if cfg == nil {
		panic("gcjwt: server config is nil")
	}
	if err := cfg.Validate(); err != nil {
		panic(fmt.Errorf("gcjwt: invalid server config: %w", err))
	}

	opts := []ServerOption{
		WithServerLogger(logger),
		WithSkipValidate(cfg.SkipValidate),
		WithTokenRequired(cfg.Required),
	}
	if cfg.ExpectedAudience != "" {
		opts = append(opts, WithExpectedAudience(cfg.ExpectedAudience))
	}
	if cfg.HeaderKey != "" {
		opts = append(opts, WithServerHeaderKey(cfg.HeaderKey))
	}

	return Server(opts...)
}
