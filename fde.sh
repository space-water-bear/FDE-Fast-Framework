#!/usr/bin/env bash
# fde-admin 项目生成器: 从本模板复制一份新项目
# 用法: ./fde.sh <目标目录> [go模块名] [应用名称]
# 示例: ./fde.sh ~/dev/my-admin github.com/me/my-admin "我的管理系统"
set -euo pipefail

TARGET="${1:?用法: ./fde.sh <目标目录> [go模块名] [应用名称]}"
MODULE="${2:-fde-admin}"
APP_NAME="${3:-FDE-Fast-Framework}"
SRC="$(cd "$(dirname "$0")" && pwd)"
TEMPLATE_REF="${FDE_TEMPLATE_REF:-HEAD}"

if [[ ! "$MODULE" =~ ^[a-zA-Z0-9._~/-]+$ ]] || [[ "$MODULE" == -* ]] || [[ "$MODULE" == *..* ]]; then
  echo "错误: go 模块名无效" >&2
  exit 1
fi

if [ -e "$TARGET" ]; then
  echo "错误: 目标目录已存在: $TARGET" >&2
  exit 1
fi

echo "==> 生成新项目: $TARGET"
TEMPLATE_COMMIT="$(git -C "$SRC" rev-parse --verify --end-of-options "$TEMPLATE_REF^{commit}")"
mkdir -p -- "$(dirname "$TARGET")"
mkdir -- "$TARGET"
trap 'rm -rf -- "$TARGET"' ERR
git -C "$SRC" archive --format=tar "$TEMPLATE_COMMIT" | tar -xf - -C "$TARGET"

# 清理运行时产物
rm -rf -- "$TARGET/server/data" "$TARGET/server/log" "$TARGET/server/uploads" "$TARGET/server/bin" \
  "$TARGET/server/.fde-gen-backup" "$TARGET/server/fde-admin" "$TARGET/.fde-gen-backup" \
  "$TARGET/.pnpm-store" "$TARGET/web/node_modules" "$TARGET/web/dist"
find "$TARGET" -type f \( -name '.env' -o -name '.env.*' -o -name '*.log' \) ! -name '.env.example' -delete

# 替换 go 模块名
if [ "$MODULE" != "fde-admin" ]; then
  echo "==> go 模块名: fde-admin -> $MODULE"
  find "$TARGET/server" -type f \( -name '*.go' -o -name 'go.mod' \) -print0 \
    | FDE_REPLACE_MODULE="$MODULE" xargs -0 perl -pi -e 's#fde-admin#$ENV{FDE_REPLACE_MODULE}#g'
fi

# 替换应用名(配置 + 前端标题/logo)
echo "==> 应用名称: $APP_NAME"
FDE_REPLACE_APP="$APP_NAME" perl -pi -e 's#FDE-Fast-Framework#$ENV{FDE_REPLACE_APP}#g' "$TARGET/server/config.yaml"
find "$TARGET/web" -type f \( -name '*.html' -o -name '*.vue' -o -name '*.ts' \) -print0 \
  | FDE_REPLACE_APP="$APP_NAME" xargs -0 perl -pi -e 's#FDE-Fast-Framework#$ENV{FDE_REPLACE_APP}#g'

trap - ERR

cat <<EOF

✔ 生成完成: $TARGET
模板提交: ${TEMPLATE_COMMIT}（仅导出已提交源码）

启动后端:
  cd $TARGET/server && go mod tidy && go run .

启动前端:
  cd $TARGET/web && pnpm install && pnpm dev

默认账号: admin / 123456
EOF
