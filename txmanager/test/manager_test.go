package txmanager_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/txmanager"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewManager_NilPool 验证必须提供连接池
func TestNewManager_NilPool(t *testing.T) {
	_, err := txmanager.NewManager(nil, txmanager.Config{}, txmanager.Dependencies{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool is required")
}

// TestNewManager_WithDefaults 验证使用默认配置创建管理器
func TestNewManager_WithDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

// TestNewManager_WithCustomLogger 验证自定义日志器
func TestNewManager_WithCustomLogger(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	// 使用默认的标准输出日志器
	logger := log.DefaultLogger
	deps := txmanager.Dependencies{Logger: logger}

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, deps)
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

// TestNewManager_WithCustomClock 验证自定义时钟（用于测试）
func TestNewManager_WithCustomClock(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	fixedTime := time.Date(2024, 10, 24, 12, 0, 0, 0, time.UTC)
	fakeClock := func() time.Time { return fixedTime }

	deps := txmanager.Dependencies{Clock: fakeClock}
	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, deps)
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

// TestNewManager_WithMetricsDisabled 验证禁用指标
func TestNewManager_WithMetricsDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	disabled := false
	cfg := txmanager.Config{MetricsEnabled: &disabled}
	mgr, err := txmanager.NewManager(pool, cfg, txmanager.Dependencies{})
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

// TestWithinTx_ContextPropagation 验证 Context 传播
func TestWithinTx_ContextPropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	type ctxKey string
	key := ctxKey("test-key")
	ctx := context.WithValue(context.Background(), key, "test-value")

	var receivedCtx context.Context
	err = mgr.WithinTx(ctx, txmanager.TxOptions{}, func(txCtx context.Context, sess txmanager.Session) error {
		receivedCtx = txCtx
		return nil
	})

	assert.NoError(t, err)
	assert.NotNil(t, receivedCtx)
	assert.Equal(t, "test-value", receivedCtx.Value(key))
}

// TestWithinTx_NilContext 验证 nil Context 处理
func TestWithinTx_NilContext(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	// 传入 nil context 应该被转换为 Background
	err = mgr.WithinTx(nil, txmanager.TxOptions{}, func(txCtx context.Context, sess txmanager.Session) error {
		assert.NotNil(t, txCtx, "txCtx 不应为 nil")
		return nil
	})

	assert.NoError(t, err)
}

// TestWithinTx_SessionNotNil 验证 Session 不为 nil
func TestWithinTx_SessionNotNil(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	err = mgr.WithinTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		assert.NotNil(t, sess, "Session 不应为 nil")
		assert.NotNil(t, sess.Tx(), "Session.Tx() 不应为 nil")
		assert.NotNil(t, sess.Context(), "Session.Context() 不应为 nil")
		return nil
	})

	assert.NoError(t, err)
}

// TestWithinTx_ErrorReturned 验证错误返回
func TestWithinTx_ErrorReturned(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	expectedErr := errors.New("business logic error")
	err = mgr.WithinTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		return expectedErr
	})

	assert.Error(t, err)
	assert.True(t, errors.Is(err, expectedErr))
}

// TestWithinTx_TimeoutApplied 验证超时设置生效
func TestWithinTx_TimeoutApplied(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	// 设置非常短的超时
	opts := txmanager.TxOptions{Timeout: 1 * time.Millisecond}
	err = mgr.WithinTx(context.Background(), opts, func(ctx context.Context, sess txmanager.Session) error {
		// 等待超时
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	// 应该超时
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}

// TestWithinTx_ParentContextDeadlineRespected 验证父 Context 的 Deadline 被尊重
func TestWithinTx_ParentContextDeadlineRespected(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	// 父 Context 有 10ms 超时
	parentCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// 事务设置 1s 超时，但应该被父 Context 覆盖
	opts := txmanager.TxOptions{Timeout: 1 * time.Second}
	err = mgr.WithinTx(parentCtx, opts, func(ctx context.Context, sess txmanager.Session) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	assert.Error(t, err)
}

// TestWithinReadOnlyTx_AccessModeSet 验证只读事务的访问模式
func TestWithinReadOnlyTx_AccessModeSet(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	err = mgr.WithinReadOnlyTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		// 在只读事务中尝试写操作应该失败
		// 但这需要实际的数据库表，在集成测试中验证
		return nil
	})

	assert.NoError(t, err)
}

// TestWithinTx_CustomIsolationLevel 验证自定义隔离级别
func TestWithinTx_CustomIsolationLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	opts := txmanager.TxOptions{Isolation: txmanager.Serializable}
	err = mgr.WithinTx(context.Background(), opts, func(ctx context.Context, sess txmanager.Session) error {
		// 验证隔离级别设置成功
		// 实际验证需要查询 pg_stat_activity 或执行并发操作
		return nil
	})

	assert.NoError(t, err)
}

// TestWithinTx_CustomTraceName 验证自定义追踪名称
func TestWithinTx_CustomTraceName(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要数据库连接的测试")
	}

	pool := setupTestPool(t)
	defer pool.Close()

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	opts := txmanager.TxOptions{TraceName: "custom.transaction.name"}
	err = mgr.WithinTx(context.Background(), opts, func(ctx context.Context, sess txmanager.Session) error {
		// Trace name 应该在 OpenTelemetry span 中可见
		// 实际验证需要 span exporter
		return nil
	})

	assert.NoError(t, err)
}

// setupTestPool 创建测试用的连接池
// 需要设置环境变量 TEST_DATABASE_URL
func setupTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	// 从环境变量读取数据库 URL
	// 例如: postgres://user:pass@localhost:5432/testdb?sslmode=disable
	dbURL := getTestDatabaseURL(t)

	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err, "无法创建数据库连接池")

	// 验证连接
	err = pool.Ping(context.Background())
	require.NoError(t, err, "无法连接到数据库")

	return pool
}

// getTestDatabaseURL 获取测试数据库 URL
func getTestDatabaseURL(t *testing.T) string {
	t.Helper()

	// 优先使用环境变量 TEST_DATABASE_URL
	if dbURL := os.Getenv("TEST_DATABASE_URL"); dbURL != "" {
		return dbURL
	}

	// 降级使用 DATABASE_URL (兼容 kratos-template)
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		return dbURL
	}

	// 如果都没有，使用默认的 localhost 连接
	return "postgres://postgres:postgres@localhost:5432/txmanager_test?sslmode=disable"
}
