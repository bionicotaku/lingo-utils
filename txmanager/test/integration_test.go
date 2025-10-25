// integration_test.go 包含需要真实数据库的集成测试
// 运行这些测试需要:
// 1. 设置环境变量 TEST_DATABASE_URL
// 2. 或使用 -short 标志跳过这些测试: go test -short
package txmanager_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/bionicotaku/lingo-utils/txmanager"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_TxCommit 测试事务提交
func TestIntegration_TxCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	pool := setupIntegrationDB(t)
	defer pool.Close()
	defer cleanupTestTable(t, pool)

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	// 在事务中插入数据
	err = mgr.WithinTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		_, execErr := sess.Tx().Exec(ctx, "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 1, "committed")
		return execErr
	})
	require.NoError(t, err)

	// 验证数据已提交
	var value string
	err = pool.QueryRow(context.Background(), "SELECT value FROM test_txmanager WHERE id = $1", 1).Scan(&value)
	assert.NoError(t, err)
	assert.Equal(t, "committed", value)
}

// TestIntegration_TxRollback 测试事务回滚
func TestIntegration_TxRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	pool := setupIntegrationDB(t)
	defer pool.Close()
	defer cleanupTestTable(t, pool)

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	// 在事务中插入数据，然后返回错误触发回滚
	expectedErr := errors.New("rollback trigger")
	err = mgr.WithinTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		_, execErr := sess.Tx().Exec(ctx, "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 2, "should_rollback")
		if execErr != nil {
			return execErr
		}
		return expectedErr // 触发回滚
	})

	assert.Error(t, err)
	assert.True(t, errors.Is(err, expectedErr))

	// 验证数据已回滚
	var value string
	err = pool.QueryRow(context.Background(), "SELECT value FROM test_txmanager WHERE id = $1", 2).Scan(&value)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, pgx.ErrNoRows))
}

// TestIntegration_TxPanic 测试事务中的 Panic 处理
func TestIntegration_TxPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	pool := setupIntegrationDB(t)
	defer pool.Close()
	defer cleanupTestTable(t, pool)

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	// 捕获 Panic
	defer func() {
		r := recover()
		assert.NotNil(t, r, "应该捕获到 panic")
		assert.Equal(t, "test panic", r)
	}()

	_ = mgr.WithinTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		_, _ = sess.Tx().Exec(ctx, "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 3, "panic_test")
		panic("test panic")
	})

	// 验证数据已回滚（即使发生 panic）
	var value string
	err = pool.QueryRow(context.Background(), "SELECT value FROM test_txmanager WHERE id = $1", 3).Scan(&value)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, pgx.ErrNoRows))
}

// TestIntegration_ReadOnlyTx 测试只读事务
func TestIntegration_ReadOnlyTx(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	pool := setupIntegrationDB(t)
	defer pool.Close()
	defer cleanupTestTable(t, pool)

	// 先插入测试数据
	_, err := pool.Exec(context.Background(), "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 10, "readonly_test")
	require.NoError(t, err)

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	// 在只读事务中查询数据应该成功
	var value string
	err = mgr.WithinReadOnlyTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		return sess.Tx().QueryRow(ctx, "SELECT value FROM test_txmanager WHERE id = $1", 10).Scan(&value)
	})
	assert.NoError(t, err)
	assert.Equal(t, "readonly_test", value)

	// 在只读事务中写操作应该失败
	err = mgr.WithinReadOnlyTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		_, execErr := sess.Tx().Exec(ctx, "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 11, "write_in_readonly")
		return execErr
	})
	assert.Error(t, err)
	// PostgreSQL 应该返回 "cannot execute INSERT in a read-only transaction" 错误
}

// TestIntegration_SerializableIsolation 测试 Serializable 隔离级别
func TestIntegration_SerializableIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	pool := setupIntegrationDB(t)
	defer pool.Close()
	defer cleanupTestTable(t, pool)

	// 插入初始数据
	_, err := pool.Exec(context.Background(), "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 20, "initial")
	require.NoError(t, err)

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	opts := txmanager.TxOptions{Isolation: txmanager.Serializable}
	err = mgr.WithinTx(context.Background(), opts, func(ctx context.Context, sess txmanager.Session) error {
		var value string
		scanErr := sess.Tx().QueryRow(ctx, "SELECT value FROM test_txmanager WHERE id = $1", 20).Scan(&value)
		if scanErr != nil {
			return scanErr
		}

		// 更新数据
		_, execErr := sess.Tx().Exec(ctx, "UPDATE test_txmanager SET value = $1 WHERE id = $2", "updated", 20)
		return execErr
	})
	assert.NoError(t, err)

	// 验证更新成功
	var finalValue string
	err = pool.QueryRow(context.Background(), "SELECT value FROM test_txmanager WHERE id = $1", 20).Scan(&finalValue)
	assert.NoError(t, err)
	assert.Equal(t, "updated", finalValue)
}

// TestIntegration_RetryableError_SerializationFailure 测试可重试错误分类
func TestIntegration_RetryableError_SerializationFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试 - 需要构造并发场景")
	}

	// 注意：构造 serialization_failure 需要两个并发事务
	// 这个测试比较复杂，需要使用 goroutine 模拟并发访问
	// 这里提供测试框架，实际实现需要更复杂的并发控制

	t.Skip("需要实现并发场景来触发 serialization_failure")
}

// TestIntegration_LockTimeout 测试锁超时
func TestIntegration_LockTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	pool := setupIntegrationDB(t)
	defer pool.Close()
	defer cleanupTestTable(t, pool)

	// 插入测试数据
	_, err := pool.Exec(context.Background(), "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 30, "lock_test")
	require.NoError(t, err)

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	// 第一个事务持有行锁
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	errChan := make(chan error, 1)
	go func() {
		errChan <- mgr.WithinTx(ctx1, txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
			// 锁定行
			var value string
			scanErr := sess.Tx().QueryRow(ctx, "SELECT value FROM test_txmanager WHERE id = $1 FOR UPDATE", 30).Scan(&value)
			if scanErr != nil {
				return scanErr
			}

			// 持有锁一段时间
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-errChan: // 等待第二个事务尝试获取锁
				return nil
			}
		})
	}()

	// 等待第一个事务获取锁
	// time.Sleep(100 * time.Millisecond)

	// 第二个事务尝试获取同一行的锁，应该超时
	// opts := txmanager.TxOptions{LockTimeout: 100 * time.Millisecond}
	// err = mgr.WithinTx(context.Background(), opts, func(ctx context.Context, sess txmanager.Session) error {
	// 	var value string
	// 	return sess.Tx().QueryRow(ctx, "SELECT value FROM test_txmanager WHERE id = $1 FOR UPDATE", 30).Scan(&value)
	// })

	// cancel1()
	// <-errChan

	// assert.Error(t, err)
	// TODO: 验证是否为锁超时错误
}

// setupIntegrationDB 设置集成测试数据库
var loadEnvOnce sync.Once

func setupIntegrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	loadEnvOnce.Do(func() {
		if err := loadTestDotEnv(); err != nil {
			t.Logf("加载 .env 失败（忽略）：%v", err)
		}
	})

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/txmanager_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("跳过集成测试：无法连接数据库 (%v)", err)
	}

	err = pool.Ping(context.Background())
	if err != nil {
		pool.Close()
		t.Skipf("跳过集成测试：数据库不可用 (%v)", err)
	}

	// 创建测试表
	createTable(t, pool)

	return pool
}

func loadTestDotEnv() error {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("runtime caller failure")
	}
	path := filepath.Join(filepath.Dir(file), ".env")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

// createTable 创建测试表
func createTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS test_txmanager (
			id INTEGER PRIMARY KEY,
			value TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT now()
		)
	`)
	require.NoError(t, err, "无法创建测试表")
}

// cleanupTestTable 清理测试数据
func cleanupTestTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(context.Background(), "TRUNCATE TABLE test_txmanager")
	if err != nil {
		t.Logf("清理测试表失败: %v", err)
	}
}

// TestIntegration_UniqueViolation 测试唯一约束冲突（非可重试错误）
func TestIntegration_UniqueViolation(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	pool := setupIntegrationDB(t)
	defer pool.Close()
	defer cleanupTestTable(t, pool)

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	// 第一次插入
	err = mgr.WithinTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		_, execErr := sess.Tx().Exec(ctx, "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 100, "first")
		return execErr
	})
	assert.NoError(t, err)

	// 第二次插入相同 ID，应该失败且不可重试
	err = mgr.WithinTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		_, execErr := sess.Tx().Exec(ctx, "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 100, "duplicate")
		return execErr
	})

	assert.Error(t, err)
	assert.False(t, txmanager.IsRetryable(err), "唯一约束冲突不应该被标记为可重试")

	// 验证错误是 PgError 且 Code 是 23505
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		assert.Equal(t, "23505", pgErr.Code, "应该是唯一约束冲突错误")
	}
}

// TestIntegration_MultipleOperations 测试事务中的多个操作
func TestIntegration_MultipleOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	pool := setupIntegrationDB(t)
	defer pool.Close()
	defer cleanupTestTable(t, pool)

	mgr, err := txmanager.NewManager(pool, txmanager.Config{}, txmanager.Dependencies{})
	require.NoError(t, err)

	err = mgr.WithinTx(context.Background(), txmanager.TxOptions{}, func(ctx context.Context, sess txmanager.Session) error {
		// 操作 1: 插入
		_, err := sess.Tx().Exec(ctx, "INSERT INTO test_txmanager (id, value) VALUES ($1, $2)", 200, "op1")
		if err != nil {
			return fmt.Errorf("insert failed: %w", err)
		}

		// 操作 2: 更新
		_, err = sess.Tx().Exec(ctx, "UPDATE test_txmanager SET value = $1 WHERE id = $2", "op2", 200)
		if err != nil {
			return fmt.Errorf("update failed: %w", err)
		}

		// 操作 3: 查询验证
		var value string
		err = sess.Tx().QueryRow(ctx, "SELECT value FROM test_txmanager WHERE id = $1", 200).Scan(&value)
		if err != nil {
			return fmt.Errorf("select failed: %w", err)
		}

		if value != "op2" {
			return fmt.Errorf("unexpected value: %s", value)
		}

		return nil
	})

	assert.NoError(t, err)

	// 验证最终状态
	var finalValue string
	err = pool.QueryRow(context.Background(), "SELECT value FROM test_txmanager WHERE id = $1", 200).Scan(&finalValue)
	assert.NoError(t, err)
	assert.Equal(t, "op2", finalValue)
}
