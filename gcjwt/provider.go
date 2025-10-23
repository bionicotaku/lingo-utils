package gcjwt

import (
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/google/wire"
)

// ClientMiddleware 标识出站（调用方）JWT 中间件，便于依赖注入时区分。
type ClientMiddleware middleware.Middleware

// ServerMiddleware 标识入站（被调方）JWT 中间件。
type ServerMiddleware middleware.Middleware

// Component 聚合客户端与服务端中间件实例。
type Component struct {
	client middleware.Middleware
	server middleware.Middleware
}

// NewComponent 根据配置构造 JWT 中间件能力。
//
// 行为：
//   - 当 cfg.Client/Server 为空时，保持对应中间件为 nil；
//   - 校验每侧配置并包装成 gcjwt.Client/Server；
//   - 返回 no-op cleanup，预留未来扩展。
func NewComponent(cfg Config, logger log.Logger) (*Component, func(), error) {
	if cfg.IsZero() {
		return &Component{}, func() {}, nil
	}
	if logger == nil {
		return nil, nil, fmt.Errorf("gcjwt: logger is nil")
	}

	var (
		client middleware.Middleware
		server middleware.Middleware
	)

	if cfg.Client != nil {
		if err := cfg.Client.Validate(); err != nil {
			return nil, nil, fmt.Errorf("gcjwt: invalid client config: %w", err)
		}
		clientOpts := []ClientOption{
			WithClientLogger(logger),
		}
		if cfg.Client.Audience != "" {
			clientOpts = append(clientOpts, WithAudience(cfg.Client.Audience))
		}
		if cfg.Client.HeaderKey != "" {
			clientOpts = append(clientOpts, WithHeaderKey(cfg.Client.HeaderKey))
		}
		if cfg.Client.Disabled {
			clientOpts = append(clientOpts, WithClientDisabled(true))
		}
		client = Client(clientOpts...)
	}

	if cfg.Server != nil {
		if err := cfg.Server.Validate(); err != nil {
			return nil, nil, fmt.Errorf("gcjwt: invalid server config: %w", err)
		}
		serverOpts := []ServerOption{
			WithServerLogger(logger),
			WithSkipValidate(cfg.Server.SkipValidate),
			WithTokenRequired(cfg.Server.Required),
		}
		if cfg.Server.ExpectedAudience != "" {
			serverOpts = append(serverOpts, WithExpectedAudience(cfg.Server.ExpectedAudience))
		}
		if cfg.Server.HeaderKey != "" {
			serverOpts = append(serverOpts, WithServerHeaderKey(cfg.Server.HeaderKey))
		}
		server = Server(serverOpts...)
	}

	comp := &Component{
		client: client,
		server: server,
	}

	return comp, func() {}, nil
}

// ProvideClientMiddleware 暴露客户端中间件；当未启用时返回 nil。
func ProvideClientMiddleware(comp *Component) (ClientMiddleware, error) {
	if comp == nil || comp.client == nil {
		return nil, nil
	}
	return ClientMiddleware(comp.client), nil
}

// ProvideServerMiddleware 暴露服务端中间件；当未启用时返回 nil。
func ProvideServerMiddleware(comp *Component) (ServerMiddleware, error) {
	if comp == nil || comp.server == nil {
		return nil, nil
	}
	return ServerMiddleware(comp.server), nil
}

// ProviderSet 供 Wire 使用，统一注入组件与中间件。
var ProviderSet = wire.NewSet(
	NewComponent,
	ProvideClientMiddleware,
	ProvideServerMiddleware,
)
