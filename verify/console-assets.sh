#!/usr/bin/env bash
# 控制台产物一致性闸口（docs/test-spec-v1.md §8）。
#
# 背景：web/dist/index.html 是**被跟踪的真实构建产物**，Go 通过 go:embed 内嵌它。
# 它引用的 /static/** 资源由 pnpm build 生成（不跟踪）。若 index.html 落后于
# 实际构建，浏览器请求到的会是 SPA 兜底 HTML（Content-Type: text/html），
# 表现为整站白屏。本脚本把这种"陈旧入口"变成显式失败。
#
# 用法：bash verify/console-assets.sh
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$REPO/web/dist"
INDEX="$DIST/index.html"
FAIL=0

echo "=== 控制台产物一致性检查 ==="

if [ ! -f "$INDEX" ]; then
  echo "FAIL: 缺少 $INDEX"
  exit 1
fi

if ! grep -q '/static/' "$INDEX"; then
  echo "FAIL: $INDEX 不含 /static/ 引用（是占位页而非真实构建产物）"
  exit 1
fi

# 1) index.html 引用的每个 /static/** 必须在 dist 下真实存在。
refs=$(grep -oE '/static/[^"]+' "$INDEX" | sort -u)
if [ -z "$refs" ]; then
  echo "FAIL: 未从 index.html 解析出任何 /static/ 引用"
  exit 1
fi
while IFS= read -r ref; do
  [ -z "$ref" ] && continue
  if [ ! -f "$DIST$ref" ]; then
    echo "FAIL: index.html 引用了不存在的资源 $ref"
    echo "      先跑 (cd web && pnpm build)，再重新 go build 内嵌。"
    FAIL=1
  else
    echo "  ok  $ref"
  fi
done <<< "$refs"

# 2) 被跟踪的 index.html 必须与当前构建一致（否则下次 go build 会内嵌陈旧入口）。
if command -v git >/dev/null 2>&1 && git -C "$REPO" rev-parse --git-dir >/dev/null 2>&1; then
  if ! git -C "$REPO" diff --quiet -- web/dist/index.html; then
    echo "FAIL: web/dist/index.html 与当前构建不一致（有未提交改动）"
    echo "      构建入口必须提交，否则 verify 脚本的 git checkout 会还原成陈旧入口。"
    git -C "$REPO" diff --stat -- web/dist/index.html
    FAIL=1
  else
    echo "  ok  web/dist/index.html 与构建一致"
  fi
fi

if [ "$FAIL" -ne 0 ]; then
  echo "=== 结果：FAIL ==="
  exit 1
fi
echo "=== 结果：PASS ==="
