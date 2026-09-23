#!/usr/bin/env bash
# W8-A3：控制台逐页走查（真实无头浏览器 + 鉴权）。
#
# 流程：构建控制台与网关 → 独立端口/独立 SQLite 起服务 → 设口令建渠道 →
#       用 CDP 以登录口令同源登录（服务端下发 HttpOnly 会话 Cookie）→ 逐页导航、
#       断言渲染与 console 无报错、截图存档 → 源码级检查三态组件与品牌残留，
#       最后跑 console_flow.py 完整使用流程（自包含假上游 + 后端强断言）。
#
# 全程本地：独立端口、独立 SQLite；不触碰任何现网容器与凭据。
set -uo pipefail

REPO="${PBR_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORK=/tmp/pbr-a3
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/final/a3-$STAMP.log"
PORT=6821
UPSTREAM_PORT=6822
GOOD_KEY='sk-a3-good'
PBR_PW='Pbr-A3-Walkthrough-Passw0rd!2026'

exec > "$EVID" 2>&1
echo "=== W8-A3 控制台逐页走查 @ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}

cleanup() { pkill -f "$WORK/pbr" 2>/dev/null; pkill -f "$WORK/fakeupstream" 2>/dev/null; }
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"

echo "--- 0) 构建（控制台 + 网关 + 假上游）"
( cd "$REPO/web" && pnpm build ) > "$WORK/pnpm.log" 2>&1 || { echo "FAIL: 控制台构建"; tail -20 "$WORK/pnpm.log"; exit 1; }
( cd "$REPO" && go build -o "$WORK/pbr" . \
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: go build"; exit 1; }
( cd "$REPO" && git checkout -- web/dist/index.html )
check "控制台构建零报错" "$(grep -c 'built in' "$WORK/pnpm.log")" "1"

"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.out" 2>&1 &
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=a3-session CRYPTO_SECRET=a3-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

BASE="http://127.0.0.1:$PORT"
echo "--- 1) 等待就绪"
ok=0
for _ in $(seq 1 60); do
  if curl -sf "$BASE/api/v1/health" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
[[ $ok == 1 ]] || { echo "FAIL: 服务未就绪"; tail -40 "$WORK/pbr.log"; exit 1; }
echo "  ready"

H='-H Content-Type:application/json'
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')

# 造点数据，让各页不是纯空态（同时验证页面能渲染真实内容）
curl -s "${A[@]}" -X PUT "$BASE/api/v1/channels/a3-ch" \
  -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["a3-model"],"enabled":true}' >/dev/null
curl -s "${A[@]}" -X PUT "$BASE/api/v1/lanes/a3-model" \
  -d '{"enabled":true,"mode":"failover","members":[{"channel":"a3-ch","model":"a3-model","priority":10}]}' >/dev/null
CLIENT_PLAIN=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/keys" -d '{"name":"a3-key"}' | jget 'd["key"]')
curl -s -o /dev/null -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"a3-model","messages":[{"role":"user","content":"hi"}]}'
sleep 1
echo "  数据就绪：1 渠道 / 1 车道 / 1 密钥 / 1 条请求日志"

echo "--- 2) 浏览器逐页走查（CDP）"
# 走查脚本用**登录口令**在页面内同源登录（服务端下发 HttpOnly 会话 Cookie）。
REPORT=$(python3 "$REPO/verify/final/console_walkthrough.py" "$BASE" "$PBR_PW" "$REPO" "$WORK/shots")
RC=$?
echo "$REPORT" | head -c 4000
echo
if [[ $RC == 2 ]]; then
  echo "  SKIP: 浏览器不可用（见上）"
  exit 2
fi
[[ $RC == 0 ]] || { echo "FAIL: 走查脚本退出码 $RC"; exit 1; }

echo
echo "--- 3) 断言"
n_rendered=$(echo "$REPORT" | jget 'len([p for p in d["pages"] if p["rendered"]])')
n_routed=$(echo "$REPORT" | jget 'len([p for p in d["pages"] if p["routed"]])')
n_total=$(echo "$REPORT" | jget 'd["pages_total"]')
check "全部页面渲染出内容" "$n_rendered" "$n_total"
check "全部页面深链直达（刷新不丢位置）" "$n_routed" "$n_total"

errs=$(echo "$REPORT" | jget 'len(d["console_errors"])')
check "无页面级 console 报错" "$errs" "0"

for key in Skeleton EmptyState ErrorState LoadingState; do
  check "三态组件存在（$key）" "$(echo "$REPORT" | jget "d[\"three_states\"][\"$key\"]")" "True"
done

brand=$(echo "$REPORT" | jget 'len(d["brand_hits"])')
check "无第三方品牌残留" "$brand" "0"

echo
echo "--- 4) 完整使用流程（console_flow.py 自包含：自己拉假上游/PBR/浏览器）"
# 审查 B3：console_flow.py 已改为自包含并强化后端断言；这里以外部实例模式复用
# 本脚本已拉起的服务与假上游，避免重复构建，同时保证它不再依赖任何外部残留进程。
FLOW_REPORT=$(python3 "$REPO/verify/final/console_flow.py" "$REPO" "$WORK/flow-shots" "$PBR_PW" \
  --base-url "$BASE" --admin-key "$ADMIN_KEY" \
  --upstream-url "http://127.0.0.1:$UPSTREAM_PORT" --upstream-key "$GOOD_KEY")
FLOW_RC=$?
echo "$FLOW_REPORT" | tail -c 3000
echo
if [[ $FLOW_RC == 2 ]]; then
  echo "  SKIP: console_flow 环境不支持"
else
  flow_total=$(echo "$FLOW_REPORT" | jget 'len(d["steps"])' 2>/dev/null || echo 0)
  flow_failed=$(echo "$FLOW_REPORT" | jget 'd["failed"]' 2>/dev/null || echo 999)
  check "完整使用流程失败数为 0（共 ${flow_total} 断言）" "$flow_failed" "0"
fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
[[ $FAIL == 0 ]] || exit 1
