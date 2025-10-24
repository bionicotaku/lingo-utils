-- 测试数据库初始化脚本
-- 用法: psql $TEST_DATABASE_URL -f test/setup_testdb.sql

-- 创建测试表
CREATE TABLE IF NOT EXISTS test_txmanager (
    id INTEGER PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);

-- 清空现有数据（如果表已存在）
TRUNCATE TABLE test_txmanager;

-- 验证表创建成功
SELECT 'Test table created successfully' AS status;
