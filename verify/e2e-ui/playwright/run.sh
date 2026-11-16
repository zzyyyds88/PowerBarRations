#!/usr/bin/env bash
# L3-E2E smoke（docs/test-spec-v1.md §4）：Playwright 覆盖登录 → 7+1 路由 →
# 日志三页签 → 命令面板实体搜索 → 渠道/密钥导出入口。
#
# 全程自包含：独立端口 + 临时 SQLite + 内置假上游 + 已知口令。缺少
# @playwright/test / 已缓存 Chromium / go 时 SKIP（退出码 2），不包装成 PASS。
#
# 用法：bash verify/e2e-ui/playwright/run.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${PBR_REPO:-$(cd "$SCRIPT_DIR/../../.." && pwd)}"
STAMP="$(date +%Y%m%d-%H%M%S)"
LOG="$SCRIPT_DIR/run-$STAMP.log"
PW="${PBR_E2E_PASSWORD:-test123}"
CHANNEL="pw-e2e"
MODEL="pw-model"

skip() {
  echo "SKIP: $*" | tee -a "$LOG"
  exit 2
}

free_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

mkdir -p "$(dirname "$LOG")"
: >"$LOG"

export PATH="/opt/go/bin:$PATH"
command -v go >/dev/null 2>&1 || skip "go 未安装"
command -v node >/dev/null 2>&1 || skip "node 未安装"
command -v curl >/dev/null 2>&1 || skip "curl 未安装"
[[ -f "$REPO/web/dist/index.html" ]] || skip "web/dist/index.html 缺失（先构建控制台）"
[[ -d "$REPO/web/node_modules/@playwright/test" ]] || skip "@playwright/test 未安装（cd web && pnpm install）"
compgen -G "$HOME/.cache/ms-playwright/chromium-*/chrome-linux*/chrome" >/dev/null \
  || skip "未找到已缓存的 Chromium（~/.cache/ms-playwright）"

WORK="$(mktemp -d /tmp/pbr-playwright.XXXXXX)"
PIDS=()
cleanup() {
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "=== L3-E2E Playwright @ $STAMP ===" | tee -a "$LOG"

echo "--- 构建网关与假上游" | tee -a "$LOG"
(
  cd "$REPO"
  export PATH="/opt/go/bin:$PATH"
  export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
  go build -o "$WORK/pbr" . >>"$LOG" 2>&1
  go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream >>"$LOG" 2>&1
)

UP_PORT="$(free_port)"
APP_PORT="$(free_port)"
UP_KEY="sk-pw-$(date +%s)"
BASE="http://127.0.0.1:$APP_PORT"
UP_BASE="http://127.0.0.1:$UP_PORT"

"$WORK/fakeupstream" -addr "127.0.0.1:$UP_PORT" -require-key "$UP_KEY" -models "$MODEL" \
  -log "$WORK/upstream.log" >>"$LOG" 2>&1 &
PIDS+=("$!")

PORT="$APP_PORT" PBR_BIND=127.0.0.1 GIN_MODE=release \
  SESSION_SECRET=pw-session CRYPTO_SECRET=pw-crypto \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  "$WORK/pbr" >>"$WORK/pbr.log" 2>&1 &
PIDS+=("$!")
echo "gateway: $BASE (log: $WORK/pbr.log)" | tee -a "$LOG"

ready=0
for _ in $(seq 1 120); do
  if curl -fsS "$BASE/api/v1/health" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.5
done
if [[ "$ready" != 1 ]]; then
  tail -40 "$WORK/pbr.log" | tee -a "$LOG"
  echo "FAIL: 网关未就绪" | tee -a "$LOG"
  exit 1
fi

echo "--- 初始化口令 + 种子渠道（仅测试数据）" | tee -a "$LOG"
curl -fsS -X POST "$BASE/api/v1/setup" \
  -H 'Content-Type: application/json' \
  -d "{\"password\":\"$PW\"}" >>"$LOG" 2>&1 || {
  echo "FAIL: 设置登录口令失败" | tee -a "$LOG"
  exit 1
}

ADMIN_KEY="$(printf '%s' "$PW" | openssl dgst -sha256 -binary | base64)"
HTTP_CODE="$(curl -sS -o "$WORK/channel.json" -w '%{http_code}' -X PUT \
  "$BASE/api/v1/channels/$CHANNEL" \
  -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d "{\"type\":\"openai\",\"base_url\":\"$UP_BASE\",\"key\":\"$UP_KEY\",\"models\":[\"$MODEL\"],\"enabled\":true}")"
if [[ "$HTTP_CODE" != 200 && "$HTTP_CODE" != 201 ]]; then
  echo "FAIL: 种子渠道创建失败 HTTP $HTTP_CODE" | tee -a "$LOG"
  cat "$WORK/channel.json" | tee -a "$LOG"
  exit 1
fi

echo "--- Playwright（只读页面）" | tee -a "$LOG"
set +e
(
  cd "$REPO/web"
  PBR_E2E_BASE_URL="$BASE" \
  PBR_E2E_PASSWORD="$PW" \
  PBR_E2E_CHANNEL="$CHANNEL" \
  PLAYWRIGHT_BROWSERS_PATH="${PLAYWRIGHT_BROWSERS_PATH:-$HOME/.cache/ms-playwright}" \
  pnpm exec playwright test \
    --config "${PBR_PLAYWRIGHT_CONFIG:-$SCRIPT_DIR/playwright.config.ts}" \
    --output "$WORK/test-results" 2>&1 | tee -a "$LOG"
  exit "${PIPESTATUS[0]}"
)
RC=$?
set -e

if [[ "$RC" == 0 ]]; then
  echo "=== 结果：PASS ===" | tee -a "$LOG"
else
  echo "=== 结果：FAIL (exit $RC) ===" | tee -a "$LOG"
  echo "artifacts kept until cleanup: $WORK/test-results" | tee -a "$LOG"
fi
exit "$RC"