#!/bin/bash
set -e

# 颜色输出
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

OLD_MODULE="github.com/bionicotaku/lingo-services-catalog"
NEW_MODULE="github.com/bionicotaku/lingo-services-catalog"
TARGET_DIR="/Users/evan/Code/learning-app/back-end/services-catalog"

echo -e "${YELLOW}开始替换 module 名称...${NC}"
echo "旧名称: $OLD_MODULE"
echo "新名称: $NEW_MODULE"
echo "目标目录: $TARGET_DIR"
echo ""

# 查找所有包含旧 module 名称的文件
FILES=$(grep -rl "$OLD_MODULE" "$TARGET_DIR" 2>/dev/null || true)

if [ -z "$FILES" ]; then
    echo -e "${YELLOW}没有找到需要替换的文件${NC}"
    exit 0
fi

# 统计文件数量
FILE_COUNT=$(echo "$FILES" | wc -l | tr -d ' ')
echo -e "${YELLOW}找到 $FILE_COUNT 个文件需要替换${NC}"
echo ""

# 执行替换（macOS 使用 sed -i ''）
echo "$FILES" | while read -r file; do
    if [ -f "$file" ]; then
        echo "处理: $file"
        sed -i '' "s|$OLD_MODULE|$NEW_MODULE|g" "$file"
    fi
done

echo ""
echo -e "${GREEN}✓ 替换完成！${NC}"
echo ""
echo "验证替换结果："
REMAINING=$(grep -rl "$OLD_MODULE" "$TARGET_DIR" 2>/dev/null | wc -l | tr -d ' ')
if [ "$REMAINING" -eq 0 ]; then
    echo -e "${GREEN}✓ 所有文件已成功替换${NC}"
else
    echo -e "${YELLOW}⚠ 还有 $REMAINING 个文件包含旧名称${NC}"
    grep -rl "$OLD_MODULE" "$TARGET_DIR" 2>/dev/null || true
fi

echo ""
echo "接下来需要执行的命令："
echo "  cd $TARGET_DIR"
echo "  go mod tidy"
echo "  make lint"
