#!/usr/bin/env bash
# fde-admin 项目生成器: 从本模板复制一份新项目
# 用法: ./fde.sh <目标目录> [go模块名] [应用名称]
# 示例: ./fde.sh ~/dev/my-admin github.com/me/my-admin "我的管理系统"
set -euo pipefail

TARGET="${1:?用法: ./fde.sh <目标目录> [go模块名] [应用名称]}"
MODULE="${2:-fde-admin}"
APP_NAME="${3:-FDE-Fast-Framework}"
SRC="$(cd "$(dirname "$0")" && pwd)"

if [ -e "$TARGET" ]; then
  echo "错误: 目标目录已存在: $TARGET" >&2
  exit 1
fi

echo "==> 生成新项目: $TARGET"
mkdir -p "$TARGET"
cp -R "$SRC/server" "$TARGET/server"
cp -R "$SRC/web" "$TARGET/web"
cp "$SRC/README.md" "$TARGET/README.md" 2>/dev/null || true
cp "$SRC/LICENSE" "$TARGET/LICENSE"
cp "$SRC/.gitignore" "$TARGET/.gitignore" 2>/dev/null || true
cp "$SRC/fde.sh" "$TARGET/fde.sh" 2>/dev/null || true

# 清理运行时产物
rm -rf "$TARGET/server/data" "$TARGET/server/fde-admin" "$TARGET/web/node_modules" "$TARGET/web/dist"

# 替换 go 模块名
if [ "$MODULE" != "fde-admin" ]; then
  echo "==> go 模块名: fde-admin -> $MODULE"
  find "$TARGET/server" -type f \( -name '*.go' -o -name 'go.mod' \) -print0 \
    | xargs -0 perl -pi -e "s#fde-admin#$MODULE#g"
fi

# 替换应用名(配置 + 前端标题/logo)
echo "==> 应用名称: $APP_NAME"
perl -pi -e "s#FDE-Fast-Framework#$APP_NAME#g" "$TARGET/server/config.yaml"
find "$TARGET/web" -type f \( -name '*.html' -o -name '*.vue' -o -name '*.ts' \) -print0 \
  | xargs -0 perl -pi -e "s#FDE-Fast-Framework#$APP_NAME#g"

cat <<EOF

✔ 生成完成: $TARGET

启动后端:
  cd $TARGET/server && go mod tidy && go run .

启动前端:
  cd $TARGET/web && pnpm install && pnpm dev

默认账号: admin / 123456
EOF
