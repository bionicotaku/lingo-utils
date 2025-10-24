# txmanager 测试套件

本目录包含 txmanager 模块的完整测试套件。

## 测试文件结构

```
test/
├── config_test.go        # 配置处理和默认值测试
├── errors_test.go        # 错误分类和可重试性测试
├── tx_options_test.go    # 事务选项合并和解析测试
├── manager_test.go       # 事务管理器核心功能测试
├── integration_test.go   # 需要真实数据库的集成测试
└── README.md             # 本文件
```

## 快速开始

### 方式 1: 使用测试脚本（推荐）⭐

这是最简单的方式，脚本会自动处理所有配置和清理工作。

```bash
# 1. 复制环境变量模板
cd /Users/evan/Code/learning-app/back-end/lingo-utils/txmanager/test
cp .env.example .env

# 2. 编辑 .env 文件，填写数据库连接
vim .env
# 或
code .env

# 3. 运行测试脚本
./run_tests.sh
```

**`.env` 文件示例:**
```bash
TEST_DATABASE_URL=postgresql://postgres:your_password@your_host:5432/your_database?sslmode=require
KEEP_TEST_TABLE=false
TEST_TIMEOUT=120
VERBOSE=true
GENERATE_HTML_REPORT=true
```

### 方式 2: 手动运行测试

#### 运行单元测试（不需要数据库）

```bash
# 在 txmanager 目录下
cd /Users/evan/Code/learning-app/back-end/lingo-utils/txmanager

# 运行快速测试（跳过集成测试）
go test -short ./test/

# 运行单元测试并显示覆盖率
go test -short -cover ./test/

# 生成覆盖率报告
go test -short -coverprofile=coverage.out ./test/
go tool cover -html=coverage.out -o coverage.html
```

#### 运行集成测试（需要 PostgreSQL）

**选项 A: 使用现有数据库（Supabase/Cloud）**

```bash
# 1. 加载 .env 文件
source test/.env

# 2. 初始化测试表
psql "$TEST_DATABASE_URL" -f test/setup_testdb.sql

# 3. 运行所有测试
go test -v ./test/

# 4. 清理（可选）
psql "$TEST_DATABASE_URL" -c "DROP TABLE IF EXISTS test_txmanager;"
```

**选项 B: 使用本地 Docker 数据库**

```bash
# 1. 启动 PostgreSQL
docker run --name txmanager-test-db \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=txmanager_test \
  -p 5432:5432 \
  -d postgres:16-alpine

# 2. 设置环境变量
export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/txmanager_test?sslmode=disable"

# 3. 初始化测试表
psql "$TEST_DATABASE_URL" -f test/setup_testdb.sql

# 4. 运行所有测试
go test -v ./test/
```

**选项 C: 运行特定测试**

```bash
# 运行特定集成测试
go test -v -run TestIntegration_TxCommit ./test/
go test -v -run TestIntegration_TxRollback ./test/

# 运行特定模块测试
go test -v -run TestConfig ./test/
go test -v -run TestError ./test/
```

## 测试覆盖目标

- **总体覆盖率**: ≥ 80%
- **核心模块**:
  - `manager.go`: ≥ 90%
  - `errors.go`: 100%
  - `config.go`: ≥ 85%
  - `tx_options.go`: ≥ 80%

## 测试场景覆盖

### ✅ 已覆盖场景

#### 配置测试
- [x] 默认值设置
- [x] 自定义隔离级别解析
- [x] 超时配置验证
- [x] LockTimeout 配置
- [x] Metrics 开关

#### 错误处理测试
- [x] 可重试错误识别 (40001, 40P01, 55P03)
- [x] 非可重试错误处理
- [x] 错误链保留
- [x] IsRetryable 判断

#### 事务管理测试
- [x] Context 传播
- [x] 事务提交
- [x] 事务回滚
- [x] Panic 恢复
- [x] 超时控制
- [x] 父 Context Deadline 尊重
- [x] 只读事务
- [x] Serializable 隔离级别

### 🚧 部分覆盖场景

- [ ] 并发场景下的 serialization_failure
- [ ] 锁超时测试（需要复杂的并发控制）
- [ ] Metrics 数据验证
- [ ] Trace span 验证

## 测试最佳实践

### 1. 使用 testing.Short() 标记

所有需要外部依赖（数据库、网络）的测试都应该检查 `testing.Short()`：

```go
func TestIntegration_Something(t *testing.T) {
    if testing.Short() {
        t.Skip("跳过集成测试")
    }
    // ... 测试逻辑
}
```

### 2. 清理测试数据

每个集成测试都应该清理自己创建的数据：

```go
func TestSomething(t *testing.T) {
    pool := setupIntegrationDB(t)
    defer pool.Close()
    defer cleanupTestTable(t, pool)  // ✅ 清理数据

    // ... 测试逻辑
}
```

### 3. 使用 testify 断言

```go
import "github.com/stretchr/testify/assert"

assert.NoError(t, err)
assert.Equal(t, expected, actual)
assert.True(t, condition)
```

### 4. 表驱动测试

```go
tests := []struct {
    name     string
    input    string
    expected string
}{
    {"case1", "input1", "output1"},
    {"case2", "input2", "output2"},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        actual := function(tt.input)
        assert.Equal(t, tt.expected, actual)
    })
}
```

## 持续集成

### GitHub Actions 配置示例

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:16-alpine
        env:
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: txmanager_test
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.23'

      - name: Run unit tests
        run: go test -short -coverprofile=coverage.out ./test/

      - name: Run integration tests
        env:
          TEST_DATABASE_URL: postgres://postgres:postgres@localhost:5432/txmanager_test?sslmode=disable
        run: go test -v ./test/

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./coverage.out
```

## 故障排查

### 集成测试失败

**问题**: `无法连接到测试数据库`

**解决方案**:
1. 检查 PostgreSQL 是否运行: `docker ps | grep postgres`
2. 检查端口是否可访问: `nc -zv localhost 5432`
3. 验证环境变量: `echo $TEST_DATABASE_URL`
4. 手动测试连接: `psql $TEST_DATABASE_URL`

**问题**: `测试表已存在`

**解决方案**:
```bash
# 清理测试数据库
psql $TEST_DATABASE_URL -c "DROP TABLE IF EXISTS test_txmanager;"
```

### 覆盖率不足

**查看未覆盖的代码**:
```bash
go test -coverprofile=coverage.out ./test/
go tool cover -func=coverage.out | grep -v "100.0%"
```

**生成 HTML 覆盖率报告**:
```bash
go tool cover -html=coverage.out
```

## 性能基准测试

```bash
# 运行基准测试（待添加）
go test -bench=. -benchmem ./test/

# 比较两次基准测试结果
go test -bench=. -benchmem ./test/ > old.txt
# ... 修改代码 ...
go test -bench=. -benchmem ./test/ > new.txt
benchcmp old.txt new.txt
```

## 贡献指南

添加新测试时请遵循：

1. 在正确的测试文件中添加测试用例
2. 使用描述性的测试名称 (`TestXxx_Scenario`)
3. 添加必要的注释说明测试目的
4. 确保测试独立且可重复
5. 更新本 README 的覆盖清单

## 参考资料

- [Go Testing 官方文档](https://pkg.go.dev/testing)
- [Testify 断言库](https://github.com/stretchr/testify)
- [Table Driven Tests](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)
