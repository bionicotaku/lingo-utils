package gcjwt

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-kratos/kratos/v2/log"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
)

var (
	factoryMu            sync.RWMutex
	idTokenSourceFactory = defaultIDTokenSourceFactory
)

func defaultIDTokenSourceFactory(ctx context.Context, audience string) (oauth2.TokenSource, error) {
	return idtoken.NewTokenSource(ctx, audience)
}

// SetTokenSourceFactory 仅用于测试覆盖场景，允许注入自定义工厂。
func SetTokenSourceFactory(factory func(context.Context, string) (oauth2.TokenSource, error)) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if factory == nil {
		idTokenSourceFactory = defaultIDTokenSourceFactory
		return
	}
	idTokenSourceFactory = factory
}

// TokenSource 封装 Cloud Run ID Token 的获取逻辑。
//
// 特性：
//   - 延迟初始化：首次调用 Token 时才初始化底层 oauth2.TokenSource
//   - 线程安全：Token 方法可被多个 goroutine 并发调用
//   - 自动缓存与刷新：依赖 oauth2.TokenSource 的内置缓存机制（约 1 小时）
//
// 使用示例：
//
//	ts := gcjwt.NewTokenSource("https://my-service.run.app/", logger)
//	token, err := ts.Token(ctx)
//	if err != nil {
//	    // 处理错误
//	}
type TokenSource struct {
	audience string
	logger   *log.Helper

	once    sync.Once
	ts      oauth2.TokenSource
	initErr error
}

// NewTokenSource 创建新的 TokenSource 实例。
//
// 参数：
//   - audience: 目标服务的 URL（例如 https://service-b.run.app/）
//   - logger: Kratos 日志器，用于记录初始化和获取过程
//
// 注意：
//   - TokenSource 会在首次调用 Token() 时延迟初始化
//   - audience 不能为空，否则初始化会失败
func NewTokenSource(audience string, logger log.Logger) *TokenSource {
	return &TokenSource{
		audience: audience,
		logger:   log.NewHelper(log.With(logger, "component", "gcjwt.token")),
	}
}

// Token 返回当前可用的 ID Token 字符串。
//
// 行为：
//   - 首次调用：初始化 oauth2.TokenSource（连接 Metadata Server）
//   - 后续调用：返回缓存的 Token，过期时自动刷新
//
// 线程安全：
//   - 可被多个 goroutine 并发调用
//   - 使用 sync.Once 保证初始化只执行一次
//
// 返回：
//   - token: JWT 格式的 ID Token 字符串
//   - error: 初始化失败或获取失败时返回错误（ErrTokenSourceInit 或 ErrTokenAcquire）
//
// 性能：
//   - Token 有效期约 1 小时，缓存避免频繁调用 Metadata Server
//   - 自动刷新发生在 Token 过期前
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.once.Do(func() {
		s.logger.Infof("initializing idtoken source (audience=%s)", s.audience)
		factoryMu.RLock()
		factory := idTokenSourceFactory
		factoryMu.RUnlock()

		s.ts, s.initErr = factory(ctx, s.audience)
		if s.initErr != nil {
			s.logger.Errorf("token source init failed: %v", s.initErr)
			return
		}
		s.logger.Info("token source initialized successfully")
	})

	if s.initErr != nil {
		return "", fmt.Errorf("%w: %v", ErrTokenSourceInit, s.initErr)
	}

	token, err := s.ts.Token()
	if err != nil {
		s.logger.Errorf("failed to acquire id token: %v", err)
		return "", fmt.Errorf("%w: %v", ErrTokenAcquire, err)
	}

	return token.AccessToken, nil
}
