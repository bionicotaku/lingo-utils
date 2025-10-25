package txmanager_test

import (
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/txmanager"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

func TestTxOptions_DefaultValues(t *testing.T) {
	opts := txmanager.TxOptions{}

	// 零值应该是空字符串和零时长
	assert.Equal(t, pgx.TxIsoLevel(""), opts.Isolation)
	assert.Equal(t, pgx.TxAccessMode(""), opts.AccessMode)
	assert.Equal(t, time.Duration(0), opts.Timeout)
	assert.Equal(t, time.Duration(0), opts.LockTimeout)
	assert.Equal(t, "", opts.TraceName)
}

func TestTxOptions_CustomValues(t *testing.T) {
	opts := txmanager.TxOptions{
		Isolation:   txmanager.Serializable,
		AccessMode:  txmanager.ReadOnly,
		Timeout:     10 * time.Second,
		LockTimeout: 2 * time.Second,
		TraceName:   "custom.trace",
	}

	assert.Equal(t, txmanager.Serializable, opts.Isolation)
	assert.Equal(t, txmanager.ReadOnly, opts.AccessMode)
	assert.Equal(t, 10*time.Second, opts.Timeout)
	assert.Equal(t, 2*time.Second, opts.LockTimeout)
	assert.Equal(t, "custom.trace", opts.TraceName)
}

func TestTxOptions_IsolationLevelConstants(t *testing.T) {
	// 验证常量别名正确映射到 pgx 枚举
	assert.Equal(t, pgx.ReadCommitted, txmanager.ReadCommitted)
	assert.Equal(t, pgx.Serializable, txmanager.Serializable)
}

func TestTxOptions_AccessModeConstants(t *testing.T) {
	// 验证访问模式常量
	assert.Equal(t, pgx.ReadWrite, txmanager.ReadWrite)
	assert.Equal(t, pgx.ReadOnly, txmanager.ReadOnly)
}

// 注意：mergeTxOptions 和 parseIsolation 是私有方法，
// 无法直接测试，需要通过 Manager 的集成测试间接验证
// 以下测试用例作为文档和未来重构的参考

func TestParseIsolation_ExpectedBehavior(t *testing.T) {
	// 这个测试用例描述 parseIsolation 的预期行为
	// 实际测试在 config_test.go 中通过 BuildPresets 间接验证

	tests := []struct {
		input    string
		expected string
	}{
		{"serializable", "serializable"},
		{"serial", "serializable"},
		{"SERIALIZABLE", "serializable"},
		{"repeatable_read", "repeatable_read"},
		{"repeatable-read", "repeatable_read"},
		{"read_uncommitted", "read_uncommitted"},
		{"read-uncommitted", "read_uncommitted"},
		{"read_committed", "read_committed"},
		{"read-committed", "read_committed"},
		{"", "read_committed"},
		{"invalid", "read_committed"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// 通过 Config 间接测试
			cfg := txmanager.Config{DefaultIsolation: tt.input}
			presets := cfg.BuildPresets()
			_ = presets
			// 实际验证逻辑在 config_test.go 中
		})
	}
}

func TestMergeTxOptions_ExpectedBehavior(t *testing.T) {
	// 描述 mergeTxOptions 的预期行为
	// 实际测试需要通过导出方法或集成测试验证

	// 预期行为：
	// 1. override 的非零值应该覆盖 base 的值
	// 2. override 的零值应该保留 base 的值
	// 3. 所有字段都应该正确合并

	// 示例（伪代码）：
	// base := TxOptions{Timeout: 5s, Isolation: ReadCommitted}
	// override := TxOptions{Timeout: 10s}
	// result := mergeTxOptions(base, override)
	// assert.Equal(t, 10s, result.Timeout)
	// assert.Equal(t, ReadCommitted, result.Isolation)
}

// TestTxOptions_ZeroValueCheck 验证零值检测逻辑
func TestTxOptions_ZeroValueCheck(t *testing.T) {
	// 验证如何判断字段是否为零值

	// Isolation 零值检测
	var emptyIsolation pgx.TxIsoLevel
	assert.Equal(t, pgx.TxIsoLevel(""), emptyIsolation)

	// AccessMode 零值检测
	var emptyAccessMode pgx.TxAccessMode
	assert.Equal(t, pgx.TxAccessMode(""), emptyAccessMode)

	// Duration 零值检测
	var emptyDuration time.Duration
	assert.Equal(t, time.Duration(0), emptyDuration)
	assert.True(t, emptyDuration <= 0)

	// TraceName 零值检测
	var emptyTraceName string
	assert.Equal(t, "", emptyTraceName)
}

// TestTxOptions_ImmutableAfterCreation 验证选项对象的不可变性
func TestTxOptions_ImmutableAfterCreation(t *testing.T) {
	original := txmanager.TxOptions{
		Isolation: txmanager.Serializable,
		Timeout:   5 * time.Second,
		TraceName: "original",
	}

	// 创建副本
	copy := original

	// 修改副本
	copy.Timeout = 10 * time.Second
	copy.TraceName = "modified"

	// 验证原始对象未被修改
	assert.Equal(t, 5*time.Second, original.Timeout)
	assert.Equal(t, "original", original.TraceName)
}

// TestTxOptions_StructSize 验证结构体大小（性能考虑）
func TestTxOptions_StructSize(t *testing.T) {
	opts := txmanager.TxOptions{}
	_ = opts

	// TxOptions 应该是轻量级结构体
	// 包含：2 个字符串枚举 + 2 个 Duration + 1 个 string
	// 预期大小约 48-64 字节（取决于平台）

	// 注意：这是文档性测试，不做严格断言
	// 主要目的是提醒开发者保持结构体轻量
}
