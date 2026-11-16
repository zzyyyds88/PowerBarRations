#!/usr/bin/env bash
# 安装本仓库的 git hooks（目前只有 pre-commit 的凭据闸口）。
#
#   bash scripts/install-hooks.sh          # 安装
#   bash scripts/install-hooks.sh --check  # 只看是否已安装
#
# 用 core.hooksPath 指向 scripts/hooks/，不复制文件，改动即时生效。
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
HOOKS_DIR="$REPO_ROOT/scripts/hooks"

mkdir -p "$HOOKS_DIR"
cat >"$HOOKS_DIR/pre-commit" <<'HOOK'
#!/usr/bin/env bash
# 由 scripts/install-hooks.sh 生成；实际逻辑在 scripts/pre-commit。
exec "$(git rev-parse --show-toplevel)/scripts/pre-commit" "$@"
HOOK
chmod +x "$HOOKS_DIR/pre-commit"

current="$(git config --get core.hooksPath || true)"
if [[ "${1:-}" == "--check" ]]; then
  if [[ "$current" == "scripts/hooks" ]]; then
    echo "hooks 已安装（core.hooksPath=$current）"
    exit 0
  fi
  echo "hooks 未安装（core.hooksPath=${current:-<未设置>}）"
  exit 1
fi

git config core.hooksPath scripts/hooks
chmod +x "$REPO_ROOT/scripts/pre-commit" 2>/dev/null || true
echo "已安装：core.hooksPath = scripts/hooks"
echo "试跑：git commit 时会先执行 scripts/pre-commit"
