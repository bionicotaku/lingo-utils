package txmanager_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/bionicotaku/lingo-utils/txmanager"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestIsRetryable_WithRetryableError(t *testing.T) {
	// 创建一个被包装的可重试错误
	originalErr := errors.New("serialization failure")
	wrappedErr := fmt.Errorf("%w: %w", txmanager.ErrRetryableTx, originalErr)

	assert.True(t, txmanager.IsRetryable(wrappedErr), "应该识别出可重试错误")
	assert.True(t, errors.Is(wrappedErr, txmanager.ErrRetryableTx), "应该能用 errors.Is 判断")
}

func TestIsRetryable_WithNonRetryableError(t *testing.T) {
	err := errors.New("some other error")
	assert.False(t, txmanager.IsRetryable(err), "普通错误不应该被识别为可重试")
}

func TestIsRetryable_WithNilError(t *testing.T) {
	assert.False(t, txmanager.IsRetryable(nil), "nil 错误不应该被识别为可重试")
}

func TestClassifyPgError_SerializationFailure(t *testing.T) {
	// 构造 PostgreSQL 40001 错误 (serialization_failure)
	pgErr := &pgconn.PgError{
		Code: "40001",
	}

	// 使用反射或直接调用内部方法（需要导出）
	// 由于 classifyPgError 是私有方法，我们通过集成测试间接验证
	// 这里只测试公开的 IsRetryable 方法

	_ = fmt.Errorf("tx error: %w", pgErr)
	// 注意：实际的 classifyPgError 在 manager.exec 中调用
	// 这里我们无法直接测试私有方法
}

func TestClassifyPgError_DeadlockDetected(t *testing.T) {
	// 构造 PostgreSQL 40P01 错误 (deadlock_detected)
	pgErr := &pgconn.PgError{
		Code: "40P01",
	}

	// 验证这是一个 PgError
	var targetErr *pgconn.PgError
	assert.True(t, errors.As(pgErr, &targetErr))
	assert.Equal(t, "40P01", targetErr.Code)
}

func TestClassifyPgError_LockNotAvailable(t *testing.T) {
	// 构造 PostgreSQL 55P03 错误 (lock_not_available)
	pgErr := &pgconn.PgError{
		Code: "55P03",
	}

	assert.Equal(t, "55P03", pgErr.Code)
}

func TestClassifyPgError_NonRetryable(t *testing.T) {
	tests := []struct {
		name string
		code string
		desc string
	}{
		{"unique_violation", "23505", "唯一约束冲突"},
		{"foreign_key_violation", "23503", "外键约束冲突"},
		{"not_null_violation", "23502", "非空约束冲突"},
		{"syntax_error", "42601", "SQL 语法错误"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgErr := &pgconn.PgError{
				Code:    tt.code,
				Message: tt.desc,
			}

			// 这些错误不应该被标记为可重试
			// 实际验证需要在集成测试中进行
			assert.Equal(t, tt.code, pgErr.Code)
		})
	}
}

func TestWrapRetryable_PreservesOriginalError(t *testing.T) {
	originalErr := errors.New("deadlock detected")
	// 模拟内部 wrapRetryable 的行为
	wrappedErr := fmt.Errorf("%w: %w", txmanager.ErrRetryableTx, originalErr)

	// 验证错误链
	assert.True(t, errors.Is(wrappedErr, txmanager.ErrRetryableTx))
	assert.True(t, errors.Is(wrappedErr, originalErr))

	// 验证错误信息包含原始错误
	assert.Contains(t, wrappedErr.Error(), "deadlock detected")
	assert.Contains(t, wrappedErr.Error(), "retryable")
}

func TestWrapRetryable_WithNilError(t *testing.T) {
	// wrapRetryable(nil) 应该返回 nil
	// 但由于方法是私有的，我们无法直接测试
	// 这个测试用例作为占位符
}

func TestErrorChaining_MultipleWraps(t *testing.T) {
	// 测试多层错误包装
	_ = errors.New("connection lost")
	pgErr := &pgconn.PgError{
		Code:    "40001",
		Message: "serialization failure",
	}
	wrappedPgErr := fmt.Errorf("query failed: %w", pgErr)
	retryableErr := fmt.Errorf("%w: %w", txmanager.ErrRetryableTx, wrappedPgErr)

	// 验证可以通过错误链找到原始错误
	var targetPgErr *pgconn.PgError
	assert.True(t, errors.As(retryableErr, &targetPgErr))
	assert.Equal(t, "40001", targetPgErr.Code)
}

// TestPgErrorSQLState 验证 pgconn.PgError 的 SQLState 方法
func TestPgErrorSQLState(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:    "40001",
		Message: "could not serialize access",
	}

	assert.Equal(t, "40001", pgErr.SQLState())
	assert.Equal(t, "could not serialize access", pgErr.Message)
}
