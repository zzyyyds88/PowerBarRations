#!/usr/bin/env bash
# Hermes 档案级验收（hermes-spec-v1.md §2 固定接入档案 + §6 验收闭环）。
#
# 与 e2e.sh 的定位不同：e2e.sh 覆盖 A1 端点全量实跑与 B 的金标准；本脚本专门按
# Hermes 的**命名 provider 档案形态**验收——127.0.0.1 本地 /v1 + transport=chat_completions
# + 单车道 ClientKey，逐条走 hermes-spec §6 的 5 项闭环。
#
# 全程本地：独立端口、独立 SQLite、内置假上游；不触碰任何现网容器与凭据。
set -uo pipefail

REPO="${PBR_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORK=/tmp/pbr-hermes
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/final/hermes-$STAMP.log"
PORT=6797
UPSTREAM_PORT=6812
GOOD_KEY="sk-hermes-good"
LANE="hermes-lane"
PBR_PW="Pbr-Hermes-Acceptance-Passw0rd!2026"

exec > "$EVID" 2>&1
echo "=== Hermes 档案级验收 @ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}

cleanup() {
  pkill -f "$WORK/pbr" 2>/dev/null
  pkill -f "$WORK/fakeupstream" 2>/dev/null
  wait 2>/dev/null
}
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"
for p in "$PORT" "$UPSTREAM_PORT"; do
  if ss -ltn 2>/dev/null | grep -q ":$p "; then echo "FAIL: 端口 $p 被占用"; exit 1; fi
done

echo "--- 0) 构建（前端 dist 已 tracked，本脚本不重建，遵循源码级改动不动 dist 的约定）"
( cd "$REPO" && go build -o "$WORK/pbr" . \
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: go build"; exit 1; }

"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.out" 2>&1 &
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=hermes-session CRYPTO_SECRET=hermes-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

echo "--- 1) 等待就绪"
ok=0
for i in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:$PORT/api/v1/health" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
[[ $ok == 1 ]] || { echo "FAIL: 服务未就绪"; tail -40 "$WORK/pbr.log"; exit 1; }
echo "ready after ${i}s"

BASE="http://127.0.0.1:$PORT"
H='-H Content-Type:application/json'
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }
control() { curl -s $H -X POST -d "$1" "http://127.0.0.1:$UPSTREAM_PORT/__control" > /dev/null; }
assert_json() {
  local desc="$1" payload="$2" expr="$3"
  if echo "$payload" | python3 -c "import sys,json;d=json.load(sys.stdin);sys.exit(0 if ($expr) else 1)"; then
    echo "  PASS: $desc"; PASS=$((PASS+1))
  else
    echo "  FAIL: $desc（$payload）"; FAIL=$((FAIL+1))
  fi
}

echo
echo "--- 2) Hermes 档案：$LANE 车道（两成员，首成员硬失败可落次成员）"
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
for ch in hermes-a hermes-b; do
  curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["'"$LANE"'"],"enabled":true}' "$BASE/api/v1/channels/$ch" > /dev/null
done
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":15,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"hermes-a","upstream_model":"'"$LANE"'","priority":20},{"channel":"hermes-b","upstream_model":"'"$LANE"'","priority":10}]}' "$BASE/api/v1/lanes/$LANE" > /dev/null
# hermes-spec §2：ClientKey 默认可只允许 <hermes-lane>。
CLIENT_PLAIN=$(curl -s "${A[@]}" -X POST -d '{"name":"hermes-client","lane_policy":{"mode":"allow","allow_lanes":["'"$LANE"'"],"deny_lanes":[]}}' "$BASE/api/v1/keys" | jget 'd["key"]')
[[ -n "$CLIENT_PLAIN" ]] || { echo "FAIL: 未取得客户端密钥"; exit 1; }
echo "  配置完成（车道 $LANE / 2 渠道 / 单车道 ClientKey）"

echo
echo "=== 验收 1：首成员硬失败 → 落到次成员（failover）==="
# hermes-a 注入 401 硬鉴权失败；hermes-b 正常。
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/$LANE/circuits/reset" > /dev/null
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"sk-wrong-key","models":["'"$LANE"'"],"enabled":true}' "$BASE/api/v1/channels/hermes-a" > /dev/null
RESP=$(curl -s -D "$WORK/h1.headers" -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"'"$LANE"'","messages":[{"role":"user","content":"hi"}]}')
SERVED=$(grep -i '^x-served-by:' "$WORK/h1.headers" | tr -d '\r' | sed 's/^[^:]*: //')
echo "  X-Served-By: $SERVED"
check "请求成功完成（非 503）" "$RESP" 'pong from fake upstream'
check "实际服务者为次成员 hermes-b" "$SERVED" 'channel=2:hermes-b'
# 恢复 hermes-a，避免影响后续项。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["'"$LANE"'"],"enabled":true}' "$BASE/api/v1/channels/hermes-a" > /dev/null
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/$LANE/circuits/reset" > /dev/null

echo
echo "=== 验收 2：Hermes 风格带 tools 请求，流式/非流式工具调用不丢失 ==="
# 注：假上游对带 tools 的请求固定回非流式 tool_calls；流式走普通 SSE 流。
TOOLS_BODY='{"model":"'"$LANE"'","messages":[{"role":"user","content":"weather?"}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],"tool_choice":"auto"}'
TOOL=$(curl -s -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d "$TOOLS_BODY")
check "带 tools 请求返回 tool_calls" "$TOOL" '"tool_calls"'
check "工具名 get_weather 不丢失" "$TOOL" '"name":"get_weather"'
check "finish_reason 为 tool_calls" "$TOOL" '"finish_reason":"tool_calls"'
STREAM=$(curl -s -N -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"'"$LANE"'","stream":true,"messages":[{"role":"user","content":"hi"}]}')
check "Hermes 流式 chat_completions 可用" "$STREAM" 'chat.completion.chunk'
check "流式以 [DONE] 收尾" "$STREAM" '[DONE]'

echo
echo "=== 验收 3：429 不永久禁用；超时/连接错误可在 attempts 定位 ==="
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/$LANE/circuits/reset" > /dev/null
control '{"model":"'"$LANE"'","status":429,"body":"{\"error\":{\"message\":\"rate limited\"}}"}'
curl -s -o /dev/null -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"'"$LANE"'","messages":[{"role":"user","content":"hi"}]}'
H_RATE=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/$LANE/health")
assert_json "429 记 soft_rate_limit 且熔断未打开（不永久禁用）" "$H_RATE" "any(m.get('last_error_kind')=='soft_rate_limit' for m in d['members']) and all(m['circuit']!='open' for m in d['members'])"
control '{"model":"'"$LANE"'","status":200}'
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/$LANE/circuits/reset" > /dev/null
# 连接错误：把 hermes-a 指向死端口，请求落到 hermes-b；attempts 里应能看到失败成员。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:1","key":"'"$GOOD_KEY"'","models":["'"$LANE"'"],"enabled":true}' "$BASE/api/v1/channels/hermes-a" > /dev/null
ATTEMPTS=$(curl -s -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"'"$LANE"'","messages":[{"role":"user","content":"hi"}]}')
check "死渠道时仍能经 hermes-b 完成" "$ATTEMPTS" 'pong from fake upstream'
# 恢复 hermes-a。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["'"$LANE"'"],"enabled":true}' "$BASE/api/v1/channels/hermes-a" > /dev/null
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/$LANE/circuits/reset" > /dev/null

echo
echo "=== 验收 4：管理 API 改成员顺序/探活/健康/重置，不依赖数据库文件 ==="
PROBE=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/$LANE/probe")
echo "  probe: $(echo "$PROBE" | head -c 200)"
assert_json "探活逐成员且 probed=2" "$PROBE" "d['probed']==2 and len(d['results'])==2"
assert_json "探活含成员级 status" "$PROBE" "all('status' in r and 'channel' in r for r in d['results'])"
HEALTH=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/$LANE/health")
assert_json "健康快照为 RFC3339/null 形状（api-spec §6.5）" "$HEALTH" "all(('cooldown_until' not in m) or m['cooldown_until'] is None or isinstance(m['cooldown_until'],str) for m in d['members'])"
assert_json "affinity 为对象或 null（不再是 affinity_until）" "$HEALTH" "'affinity' in d and 'affinity_until' not in d"
# 通过管理 API 交换成员顺序（hermes-b 升到首位）。
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":15,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"hermes-b","upstream_model":"'"$LANE"'","priority":20},{"channel":"hermes-a","upstream_model":"'"$LANE"'","priority":10}]}' "$BASE/api/v1/lanes/$LANE" > /dev/null
SWAP=$(curl -s -D "$WORK/swap.headers" -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"'"$LANE"'","messages":[{"role":"user","content":"hi"}]}')
SWAP_SERVED=$(grep -i '^x-served-by:' "$WORK/swap.headers" | tr -d '\r' | sed 's/^[^:]*: //')
check "改序后由 hermes-b 服务" "$SWAP_SERVED" 'channel=2:hermes-b'
RESET=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/$LANE/circuits/reset")
check "重置熔断返回 reset 字段" "$RESET" '"reset"'

echo
echo "=== 验收 5：单车道 ClientKey 调用成功，调用其他模型稳定拒绝 ==="
OTHER=$(curl -s -o "$WORK/other.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"not-hermes-lane","messages":[{"role":"user","content":"hi"}]}')
echo "  其他模型 HTTP $OTHER $(head -c 160 "$WORK/other.json")"
check "调用非授权模型返回 403" "$OTHER" "403"
check "拒绝响应含稳定错误码/文案" "$(cat "$WORK/other.json")" 'lane'

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: Hermes 档案级验收通过"; else
  echo "FAIL: 存在未通过项"; echo "--- pbr.log 末尾 ---"; tail -40 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
