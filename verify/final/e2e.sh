#!/usr/bin/env bash
# W8 自验收（可自动化的部分）：管理面端点全量实跑 + 端到端金标准。
#
# 覆盖 goal-prompt §六 的 A1（端点无 5xx/无未实现桩）、A2（四模式与熔断半开，见 verify/w2）、
# 以及 B 的可自动化子集。不可自动化项（视觉走查、长稳、回滚演练等）在 verify/final/README.md
# 中逐条列出并说明原因，不计入通过。
#
# 全程本地：独立端口、独立 SQLite、内置假上游；不触碰任何现网容器与凭据。
set -uo pipefail

REPO="${PBR_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORK=/tmp/pbr-final
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/final/run-$STAMP.log"
PORT=6799
UPSTREAM_PORT=6810
GOOD_KEY='sk-final-good'
PBR_PW='Pbr-Final-Acceptance-Passw0rd!2026'

exec > "$EVID" 2>&1
echo "=== W8 自验收（自动化部分）@ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}
note() { echo "  NOTE: $1"; }

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

echo "--- 0) 构建"
( cd "$REPO/web" && pnpm build ) > "$WORK/pnpm.log" 2>&1 || { echo "FAIL: 控制台构建"; tail -20 "$WORK/pnpm.log"; exit 1; }
( cd "$REPO" && go build -o "$WORK/pbr" . \
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: go build"; exit 1; }
( cd "$REPO" && git checkout -- web/dist/index.html )
check "控制台构建零报错" "$(grep -c 'built in' "$WORK/pnpm.log")" "1"

"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.out" 2>&1 &
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=final-session CRYPTO_SECRET=final-crypto GIN_MODE=release \
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

echo "--- 2) 初始化与配置"
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
# PBR 渠道没有 priority：两个渠道只是"都声明同一模型"，顺序由车道成员决定。
for ch in channel-a channel-b; do
  curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["e2e-model","e2e-embed","e2e-long"],"enabled":true}' "$BASE/api/v1/channels/$ch" > /dev/null
done
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":60,"member_stream_first_event_timeout_seconds":30,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"channel-a","upstream_model":"e2e-model","priority":20},{"channel":"channel-b","upstream_model":"e2e-model","priority":10}]}' "$BASE/api/v1/lanes/e2e-model" > /dev/null
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":60,"member_stream_first_event_timeout_seconds":30,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"channel-a","upstream_model":"e2e-embed","priority":1}]}' "$BASE/api/v1/lanes/e2e-embed" > /dev/null
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":60,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"channel-a","upstream_model":"e2e-long","priority":1}]}' "$BASE/api/v1/lanes/e2e-long" > /dev/null
CLIENT_PLAIN=$(curl -s "${A[@]}" -X POST -d '{"name":"e2e-client"}' "$BASE/api/v1/keys" | jget 'd["key"]')
[[ -n "$CLIENT_PLAIN" ]] || { echo "FAIL: 未取得客户端密钥"; exit 1; }
echo "  配置完成（3 条车道 / 2 个渠道 / 1 把客户端密钥）"

echo
echo "=== A1：管理面端点全量实跑（不得 5xx、不得未实现）==="
curl -s "${A[@]}" "$BASE/api/v1/openapi.json" -o "$WORK/openapi.json"
ENDPOINT_REPORT=$(python3 "$REPO/verify/final/endpoint_sweep.py" "$WORK/openapi.json" "$BASE" "$ADMIN_KEY" "http://127.0.0.1:$UPSTREAM_PORT" "$GOOD_KEY")
echo "$ENDPOINT_REPORT" | python3 -c 'import sys,json;d=json.load(sys.stdin);print("  实跑端点：",len(d["results"]));[print("   ",r["status"],r["method"].upper(),r["path"],r.get("note","")) for r in d["results"]]'
assert_ep() {
  local desc="$1" expr="$2"
  if echo "$ENDPOINT_REPORT" | python3 -c "import sys,json;d=json.load(sys.stdin);sys.exit(0 if ($expr) else 1)"; then
    echo "  PASS: $desc"; PASS=$((PASS+1))
  else
    echo "  FAIL: $desc"; echo "$ENDPOINT_REPORT" | head -c 1500; FAIL=$((FAIL+1))
  fi
}
assert_ep "没有 5xx" "all(r['status']<500 for r in d['results'])"
assert_ep "没有 501/未实现" "all(r['status']!=501 for r in d['results'])"
assert_ep "没有 404（除按设计不存在的对象）" "all(r['status']!=404 or r.get('expected_missing') for r in d['results'])"
assert_ep "sync-models 能真的同步（探针渠道指向假上游）" "any(r['path']=='/channels/{name}/sync-models' and r['status']==200 for r in d['results'])"
assert_ep "DELETE 三类资源都删得掉" "len([r for r in d['results'] if r['method']=='delete' and r['status']==200])==3"

echo
echo "=== B① 工具调用透传 ==="
TOOL=$(curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"e2e-model","messages":[{"role":"user","content":"weather?"}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}')
echo "  $(echo "$TOOL" | head -c 300)"
check "返回 tool_calls" "$TOOL" '"tool_calls"'
check "工具名为 get_weather" "$TOOL" '"name":"get_weather"'
check "finish_reason 为 tool_calls" "$TOOL" '"finish_reason":"tool_calls"'

echo
echo "=== B② 多模态小图透传 ==="
IMG=$(python3 -c "print('data:image/png;base64,' + 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8AAAwAB/gGjHwAAAABJRU5ErkJggg==')")
MULTI=$(curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"e2e-model","messages":[{"role":"user","content":[{"type":"text","text":"what is this"},{"type":"image_url","image_url":{"url":"'"$IMG"'"}}]}]}')
check "多模态请求成功" "$MULTI" 'pong from fake upstream'
UP_BODY=$(grep -a -o 'image_url' "$WORK/upstream.log" | head -1)
check "上游确实收到了图片内容" "$UP_BODY" 'image_url'

echo
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/e2e-model/circuits/reset" > /dev/null
echo "=== B③ 思考参数半开：上游 400 时原样透传（不换人、不冷却）==="
control '{"model":"e2e-model","status":400,"body":"{\"error\":{\"message\":\"thinking budget not supported\",\"type\":\"invalid_request_error\"}}"}'
THINK=$(curl -s -o "$WORK/think.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"e2e-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high"}')
echo "  HTTP $THINK $(head -c 200 "$WORK/think.json")"
check "上游 400 原样返回（不是 503）" "$THINK" "400"
check "错误体来自上游" "$(cat "$WORK/think.json")" 'thinking budget not supported'
HEALTH_AFTER_400=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/e2e-model/health")
assert_json_health() {
  local desc="$1" expr="$2" payload="$3"
  if echo "$payload" | python3 -c "import sys,json;d=json.load(sys.stdin);sys.exit(0 if ($expr) else 1)"; then
    echo "  PASS: $desc"; PASS=$((PASS+1))
  else
    echo "  FAIL: $desc（$payload）"; FAIL=$((FAIL+1))
  fi
}
# api-spec §6.5：cooldown_until 为 RFC3339 字符串，无冷却为 null（不再是 unix 毫秒 0）。
assert_json_health "client_error 不冷却不换人" "all(m['cooldown_until'] is None and m['failure_score']==0 for m in d['members'])" "$HEALTH_AFTER_400"
control '{"model":"e2e-model","status":200}'

echo
echo "=== B④ 长流式（≥100K 输入 token 量级）==="
python3 - "$WORK/long.json" <<'PYLONG'
import json, sys
# 约 12 万 token 量级的输入（约 720KB），验证长上下文的转发与流式收尾
payload = {"model": "e2e-long", "stream": True,
           "messages": [{"role": "user", "content": "token " * 120000}]}
open(sys.argv[1], "w").write(json.dumps(payload))
PYLONG
echo "  请求体大小：$(wc -c < "$WORK/long.json") 字节"
LONG_CODE=$(curl -s -o "$WORK/long.out" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  --data-binary @"$WORK/long.json")
echo "  HTTP $LONG_CODE；响应前 120 字节：$(head -c 120 "$WORK/long.out")"
check "长输入流式返回 200" "$LONG_CODE" "200"
check "流式为 SSE 且含内容" "$(head -c 400 "$WORK/long.out")" 'pong'
check "流式以 [DONE] 收尾" "$(tail -c 60 "$WORK/long.out")" '[DONE]'

echo
echo "=== B⑤ /v1/messages（Anthropic 入口）==="
MSG=$(curl -s -X POST "$BASE/v1/messages" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"e2e-model","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}')
echo "  $(echo "$MSG" | head -c 300)"
check "Anthropic 入口可用" "$MSG" '"type":"message"'

echo
echo "=== B⑥ embeddings ==="
EMB=$(curl -s -X POST "$BASE/v1/embeddings" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"e2e-embed","input":["a","b"]}')
echo "  $(echo "$EMB" | head -c 240)"
check "嵌入返回两条向量" "$(echo "$EMB" | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["data"]))')" "2"

echo
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/e2e-model/circuits/reset" > /dev/null
echo "=== B⑦ 车道全挂 → 快抛 503（不轮询等待）==="
control '{"model":"e2e-model","status":503,"body":"{\"error\":{\"message\":\"upstream down\"}}"}'
BEFORE=$(grep -ac '"path"' "$WORK/upstream.log")
START=$(date +%s%N)
DOWN=$(curl -s -o "$WORK/down.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"e2e-model","messages":[{"role":"user","content":"hi"}]}')
ELAPSED_MS=$(( ($(date +%s%N) - START) / 1000000 ))
AFTER=$(grep -ac '"path"' "$WORK/upstream.log")
echo "  HTTP $DOWN，耗时 ${ELAPSED_MS}ms，上游命中 $BEFORE → $AFTER"
check "全挂返回 503" "$DOWN" "503"
check "固定 body 文案" "$(cat "$WORK/down.json")" 'No available channel for model e2e-model'
check "快抛（<2s，非轮询等待）" "$(python3 -c "print(1 if $ELAPSED_MS < 2000 else 0)")" "1"
control '{"model":"e2e-model","status":200}'

echo
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/e2e-model/circuits/reset" > /dev/null
echo "=== B⑧ 429 不误判为硬故障 ==="
control '{"model":"e2e-model","status":429,"body":"{\"error\":{\"message\":\"rate limited\"}}"}'
curl -s -o /dev/null -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"e2e-model","messages":[{"role":"user","content":"hi"}]}'
RATE_HEALTH=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/e2e-model/health")
assert_json_health "429 记入软故障且熔断未打开" "any(m['last_error_kind']=='soft_rate_limit' for m in d['members']) and all(m['circuit']!='open' for m in d['members'])" "$RATE_HEALTH"
control '{"model":"e2e-model","status":200}'

echo
echo "=== F：安全（未初始化/错误密钥/被拒车道/密钥明文可回读）==="
WRONG=$(curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer not-the-key' "$BASE/api/v1/lanes")
check "错误管理密钥 401" "$WRONG" "401"
# allow 白名单里放一个真实存在的车道（e2e-embed），从而对 e2e-model 形成拒绝：
# 不能再用不存在的 nope——批次2 B1 已把"未知路由键"改判 422，那条 PUT 会被拒、策略不生效。
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"lane_policy":{"mode":"allow","allow_lanes":["e2e-embed"],"deny_lanes":[]}}' "$BASE/api/v1/keys/e2e-client" > /dev/null
DENY=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"e2e-model","messages":[{"role":"user","content":"hi"}]}')
check "被拒车道 403" "$DENY" "403"
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"lane_policy":{"mode":"all","allow_lanes":[],"deny_lanes":[]}}' "$BASE/api/v1/keys/e2e-client" > /dev/null
# 现行契约（token-spec v1 §3.3，commit 03a91f）：客户端密钥**明文入库**、管理 API
# 详情可回读，与创建响应一致；仅管理密钥仍以 sha256 落库、明文不得出现。
KEY_READBACK=$(curl -s "${A[@]}" "$BASE/api/v1/keys/e2e-client" | jget 'd["key"]')
if [[ -n "$KEY_READBACK" && "$KEY_READBACK" == "$CLIENT_PLAIN" ]]; then
  echo "  PASS: GET /api/keys/{name} 回读明文与创建响应一致"; PASS=$((PASS+1))
else
  echo "  FAIL: 回读明文与创建响应不一致（回读=$KEY_READBACK）"; FAIL=$((FAIL+1))
fi
# SQLite 跑在 WAL 模式：新写入可能还在 -wal 里未回写主库，必须连同 -wal 一起查。
if grep -a -q "$CLIENT_PLAIN" "$WORK"/pbr.db* 2>/dev/null; then
  echo "  PASS: 客户端密钥明文入库（§3.3 明文存储契约）"; PASS=$((PASS+1))
else
  echo "  FAIL: 库中应能检索到客户端密钥明文"; FAIL=$((FAIL+1))
fi
if grep -a -q "$ADMIN_KEY" "$WORK"/pbr.db* 2>/dev/null; then
  echo "  FAIL: 库中出现管理密钥明文"; FAIL=$((FAIL+1))
else
  echo "  PASS: 库中无管理密钥明文"; PASS=$((PASS+1))
fi
SENSITIVE=$(curl -s "${A[@]}" "$BASE/api/v1/openapi.json" | grep -c -iE '"[^"]*key[^"]*"\s*:\s*"[^"]+"' || true)
check "OpenAPI 不泄漏密钥字段值" "$SENSITIVE" "0"

echo
echo "=== E：重启后配置与令牌不丢、运行态清空 ==="
BEFORE_KEY=$(curl -s "${A[@]}" "$BASE/api/v1/keys/e2e-client" | jget 'd["key_prefix"]')
pkill -f "$WORK/pbr" 2>/dev/null; sleep 2
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=final-session CRYPTO_SECRET=final-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr-restart.log" 2>&1 ) &
ok=0
for i in $(seq 1 60); do curl -sf "$BASE/api/v1/health" >/dev/null 2>&1 && { ok=1; break; }; sleep 1; done
[[ $ok == 1 ]] || { echo "FAIL: 重启后未就绪"; tail -30 "$WORK/pbr-restart.log"; exit 1; }
AFTER_KEY=$(curl -s "${A[@]}" "$BASE/api/v1/keys/e2e-client" | jget 'd["key_prefix"]')
check "重启后密钥前缀不变" "$AFTER_KEY" "$BEFORE_KEY"
AFTER_MODELS=$(curl -s "${A[@]}" "$BASE/api/v1/models")
check "重启后模型路由仍在" "$AFTER_MODELS" '"model":"e2e-model"'
AFTER_HEALTH=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/e2e-model/health")
assert_json_health "重启后运行态清空（无残留冷却）" "all(m['cooldown_until'] is None for m in d['members'])" "$AFTER_HEALTH"
RESP_AFTER=$(curl -s -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"e2e-model","messages":[{"role":"user","content":"hi"}]}')
check "重启后原密钥仍可转发" "$RESP_AFTER" 'pong from fake upstream'

echo
echo "--- 还原占位页与私有数据自查"
( cd "$REPO" && git checkout -- web/dist/index.html )
LEAK=$(grep -rn "sk-final-good" "$REPO" --exclude-dir=.git --exclude-dir=verify 2>/dev/null | head -3)
if [[ -z "$LEAK" ]]; then echo "  PASS: 源码无测试密钥残留"; PASS=$((PASS+1)); else echo "  FAIL: $LEAK"; FAIL=$((FAIL+1)); fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: W8 自动化自验收通过"; else
  echo "FAIL: 存在未通过项"; echo "--- pbr.log 末尾 ---"; tail -40 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
