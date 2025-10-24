#!/bin/bash
# 运行 txmanager 完整测试套件（包括集成测试）

set -e  # 遇到错误立即退出

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo -e "${GREEN}=== txmanager 测试套件 ===${NC}\n"

# 1. 加载环境变量
if [ -f "$SCRIPT_DIR/.env" ]; then
    echo -e "${BLUE}[0/6] 加载环境变量: $SCRIPT_DIR/.env${NC}"
    set -a  # 自动导出所有变量
    source "$SCRIPT_DIR/.env"
    set +a
    echo -e "${GREEN}✓ 环境变量已加载${NC}\n"
else
    echo -e "${YELLOW}警告: 未找到 .env 文件${NC}"
    echo -e "${YELLOW}请复制 .env.example 为 .env 并配置数据库连接:${NC}"
    echo -e "${YELLOW}  cp $SCRIPT_DIR/.env.example $SCRIPT_DIR/.env${NC}"
    echo -e "${YELLOW}  vim $SCRIPT_DIR/.env${NC}\n"
fi

# 2. 检查必需的环境变量
if [ -z "$TEST_DATABASE_URL" ]; then
    echo -e "${RED}错误: TEST_DATABASE_URL 环境变量未设置${NC}"
    echo -e "${RED}请在 .env 文件中配置数据库连接${NC}"
    echo ""
    echo -e "${YELLOW}快速开始:${NC}"
    echo -e "  1. cp $SCRIPT_DIR/.env.example $SCRIPT_DIR/.env"
    echo -e "  2. 编辑 .env 文件，填写数据库连接信息"
    echo -e "  3. 重新运行此脚本"
    exit 1
fi

# 设置默认值
KEEP_TEST_TABLE=${KEEP_TEST_TABLE:-false}
TEST_TIMEOUT=${TEST_TIMEOUT:-120}
VERBOSE=${VERBOSE:-true}
GENERATE_HTML_REPORT=${GENERATE_HTML_REPORT:-true}

# 显示配置
echo -e "${GREEN}测试配置:${NC}"
echo -e "  数据库: ${TEST_DATABASE_URL%%:*}://*****@${TEST_DATABASE_URL#*@}"
echo -e "  保留测试表: $KEEP_TEST_TABLE"
echo -e "  超时时间: ${TEST_TIMEOUT}s"
echo -e "  详细输出: $VERBOSE"
echo ""

# 3. 测试数据库连接
echo -e "${GREEN}[1/6] 测试数据库连接...${NC}"
# 尝试使用 timeout 或 gtimeout，如果都没有则直接连接
if command -v timeout &> /dev/null; then
    TIMEOUT_CMD="timeout 10"
elif command -v gtimeout &> /dev/null; then
    TIMEOUT_CMD="gtimeout 10"
else
    TIMEOUT_CMD=""
fi

if ! $TIMEOUT_CMD psql "$TEST_DATABASE_URL" -c "SELECT 1" > /dev/null 2>&1; then
    echo -e "${RED}错误: 无法连接到测试数据库${NC}"
    echo -e "${RED}请检查 .env 文件中的 TEST_DATABASE_URL 是否正确${NC}"
    echo ""
    echo -e "${YELLOW}排查建议:${NC}"
    echo -e "  1. 检查数据库是否运行"
    echo -e "  2. 检查用户名和密码是否正确"
    echo -e "  3. 检查网络连接"
    echo -e "  4. 手动测试: psql \"\$TEST_DATABASE_URL\" -c 'SELECT 1'"
    exit 1
fi
echo -e "${GREEN}✓ 数据库连接成功${NC}\n"

# 4. 初始化测试表
echo -e "${GREEN}[2/6] 初始化测试表...${NC}"
if ! psql "$TEST_DATABASE_URL" -f "$SCRIPT_DIR/setup_testdb.sql" > /dev/null 2>&1; then
    echo -e "${RED}错误: 无法创建测试表${NC}"
    echo -e "${RED}请检查数据库权限${NC}"
    exit 1
fi
echo -e "${GREEN}✓ 测试表初始化完成 (test_txmanager)${NC}\n"

# 5. 运行单元测试（快速）
echo -e "${GREEN}[3/6] 运行单元测试...${NC}"
cd "$SCRIPT_DIR/.."  # 切换到 txmanager 根目录

if [ "$VERBOSE" = "true" ]; then
    go test -short -v ./test/ -timeout ${TEST_TIMEOUT}s
else
    go test -short ./test/ -timeout ${TEST_TIMEOUT}s
fi

if [ $? -ne 0 ]; then
    echo -e "${RED}✗ 单元测试失败${NC}"
    exit 1
fi
echo -e "${GREEN}✓ 单元测试通过${NC}\n"

# 6. 运行完整测试（包括集成测试）
echo -e "${GREEN}[4/6] 运行集成测试...${NC}"
if [ "$VERBOSE" = "true" ]; then
    go test -v ./test/ -timeout ${TEST_TIMEOUT}s
else
    go test ./test/ -timeout ${TEST_TIMEOUT}s
fi

if [ $? -ne 0 ]; then
    echo -e "${RED}✗ 集成测试失败${NC}"
    if [ "$KEEP_TEST_TABLE" = "false" ]; then
        echo -e "${YELLOW}提示: 设置 KEEP_TEST_TABLE=true 可以保留测试表用于调试${NC}"
    fi
    exit 1
fi
echo -e "${GREEN}✓ 集成测试通过${NC}\n"

# 7. 生成覆盖率报告
echo -e "${GREEN}[5/6] 生成覆盖率报告...${NC}"
go test -coverpkg=github.com/bionicotaku/lingo-utils/txmanager \
    -coverprofile=coverage.out \
    ./test/ \
    -timeout ${TEST_TIMEOUT}s > /dev/null 2>&1

COVERAGE=$(go tool cover -func=coverage.out | tail -1 | awk '{print $3}')
echo -e "${GREEN}✓ 代码覆盖率: ${COVERAGE}${NC}"

if [ "$GENERATE_HTML_REPORT" = "true" ]; then
    go tool cover -html=coverage.out -o coverage.html
    echo -e "${GREEN}✓ HTML 报告已生成: $(pwd)/coverage.html${NC}"
fi
echo ""

# 8. 清理测试数据
if [ "$KEEP_TEST_TABLE" = "false" ]; then
    echo -e "${GREEN}[6/6] 清理测试数据...${NC}"
    psql "$TEST_DATABASE_URL" -c "DROP TABLE IF EXISTS test_txmanager;" > /dev/null 2>&1 || true
    echo -e "${GREEN}✓ 测试表已删除${NC}\n"
else
    echo -e "${YELLOW}[6/6] 保留测试表 test_txmanager (KEEP_TEST_TABLE=true)${NC}\n"
fi

# 9. 显示测试总结
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}✨ 所有测试通过! ✨${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "${BLUE}测试统计:${NC}"
echo -e "  覆盖率: ${COVERAGE}"
echo -e "  测试时长: ~${TEST_TIMEOUT}s 内"
echo ""
echo -e "${BLUE}查看详细报告:${NC}"
if [ "$GENERATE_HTML_REPORT" = "true" ]; then
    echo -e "  open coverage.html"
fi
echo -e "  cat test/TEST_SUMMARY.md"
echo ""
