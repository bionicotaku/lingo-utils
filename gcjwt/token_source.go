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

// TokenSource 封装 Cloud Run ID Token 的获取逻辑，内部复用官方缓存与重试策略。
type TokenSource struct {
	audience string
	logger   *log.Helper

	once    sync.Once
	ts      oauth2.TokenSource
	initErr error
}

// NewTokenSource 创建新的 TokenSource。
func NewTokenSource(audience string, logger log.Logger) *TokenSource {
	return &TokenSource{
		audience: audience,
		logger:   log.NewHelper(log.With(logger, "component", "gcjwt.token")),
	}
}

// Token 返回当前可用的 ID Token 字符串。
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
