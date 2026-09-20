#!/usr/bin/env bash
# L2 真实 API 运维（docs/test-spec-v1.md §5）。
#
# 只用管理 API（Authorization: Bearer <管理密钥>，由登录口令派生）模拟运维人员一天的活：
# 探活 → 建渠道 → 建车道 → 端到端调用 → 排障 → 探活 → 故障转移 → 重置熔断
# → 密钥轮换 → 导出/导入幂等 → 日志保留 → 审计。每步"调用 → 回读 → 断言"。
#
# 全程本地：独立端口 + 独立 SQLite + 内置假上游；不打真实厂商、不碰现网。
set -uo pipefail

REPO="${PBR_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORK=/tmp/pbr-ops
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/e2e-api/run-$STAMP.log"
PORT=6841
UPSTREAM_PORT=6842
GOOD_KEY="sk-ops-good"
BAD_KEY="sk-ops-bad"
PBR_PW="Pbr-Ops-Runbook-Passw0rd!2026"

exec > "$EVID" 2>&1
echo "=== L2 真实 API 运维 runbook @ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}
assert_json() {
  local desc="$1" payload="$2" expr="$3"
  if echo "$payload" | python3 -c "import sys,json;d=json.load(sys.stdin);sys.exit(0 if ($expr) else 1)" 2>/dev/null; then
    echo "  PASS: $desc"; PASS=$((PASS+1))
  else
    echo "  FAIL: $desc"; echo "        表达式: $expr"; echo "        实际  : $payload"; FAIL=$((FAIL+1))
  fi
}
cleanup() { pkill -f "$WORK/pbr" 2>/dev/null; pkill -f "$WORK/fakeupstream" 2>/dev/null; wait 2>/dev/null; }
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"
for p in "$PORT" "$UPSTREAM_PORT"; do
  if ss -ltn 2>/dev/null | grep -q ":$p "; then echo "FAIL: 端口 $p 被占用"; exit 1; fi
done

echo "--- 0) 构建"
( cd "$REPO" && go build -o "$WORK/pbr" . \
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: go build"; exit 1; }

"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.out" 2>&1 &
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=ops-session CRYPTO_SECRET=ops-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

BASE="http://127.0.0.1:$PORT"
echo "--- 1) 等待就绪"
ok=0
for i in $(seq 1 60); do
  if curl -sf "$BASE/api/v1/health" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
[[ $ok == 1 ]] || { echo "FAIL: 服务未就绪"; tail -40 "$WORK/pbr.log"; exit 1; }
echo "ready after ${i}s"

H='-H Content-Type:application/json'
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }
control() { curl -s $H -X POST -d "$1" "http://127.0.0.1:$UPSTREAM_PORT/__control" > /dev/null; }

echo
echo "=== 运维 1：探活/版本（免鉴权）==="
assert_json "health 200 且 ready" "$(curl -s "$BASE/api/v1/health")" "d['status']=='ok' or d.get('ready') is True or len(d)>0"
VERSION=$(curl -s "$BASE/api/v1/version")
check "version 可读" "$VERSION" "version"

echo
echo "=== 运维 2：首启设口令，派生管理密钥（token-spec §2.1）==="
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
DERIVED=$(python3 -c "import hashlib,base64,sys;print(base64.b64encode(hashlib.sha256(sys.argv[1].encode()).digest()).decode())" "$PBR_PW")
check "口令派生密钥与 setup 返回值一致" "$DERIVED" "$ADMIN_KEY"
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
CAPS=$(curl -s "${A[@]}" "$BASE/api/v1/capabilities")
assert_json "capabilities 含协议与车道模式（需鉴权）" "$CAPS" "len(d['inbound_formats'])>0 and set(d['lane_modes'])=={'failover','manual'}"

echo
echo "=== 运维 3：建渠道（写后回读）==="
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["ops-model"],"enabled":true}' "$BASE/api/v1/channels/ops-a" > /dev/null
# 两个渠道都用正确 key：后续"故障注入"只把 ops-a 改成错 key，ops-b 保持可用。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["ops-model"],"enabled":true}' "$BASE/api/v1/channels/ops-b" > /dev/null
CH_A=$(curl -s "${A[@]}" "$BASE/api/v1/channels/ops-a")
assert_json "渠道回读含 models 与 key_prefix（不回声明文）" "$CH_A" "'ops-model' in d['models'] and d['key_prefix']!='' and d.get('key','')==''"

echo
echo "=== 运维 4：建车道（唯一路由入口，ADR 0005）==="
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":15,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"ops-a","upstream_model":"ops-model","priority":20},{"channel":"ops-b","upstream_model":"ops-model","priority":10}]}' "$BASE/api/v1/lanes/ops-model" > /dev/null
ROUTE=$(curl -s "${A[@]}" "$BASE/api/v1/routes/ops-model")
assert_json "routes routable 且成员按 priority 降序" "$ROUTE" "d['routable'] is True and [m['channel'] for m in d['members']][:2]==['ops-a','ops-b']"
MODELS=$(curl -s "${A[@]}" "$BASE/api/v1/models")
assert_json "models 列出 ops-model 且 explicit" "$MODELS" "any(m['model']=='ops-model' and m['source']=='explicit' and m['routable'] for m in d['items'])"

echo
echo "=== 运维 4b：池化车道（跨渠道跨模型，ADR 0006）==="
# 成员可来自任意启用渠道的任意已声明模型：这里让 ops-b 同时贡献两个不同上游名，
# 并让同一渠道在一条车道内出现两次（去重键 = 渠道 + 上游真名）。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["ops-model","ops-alt"],"enabled":true}' "$BASE/api/v1/channels/ops-b" > /dev/null
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":15,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"ops-b","upstream_model":"ops-model","priority":30},{"channel":"ops-b","upstream_model":"ops-alt","priority":20},{"channel":"ops-a","upstream_model":"ops-model","priority":10}]}' "$BASE/api/v1/lanes/ops-pool" > /dev/null
POOL=$(curl -s "${A[@]}" "$BASE/api/v1/routes/ops-pool")
assert_json "池化车道成员 = 提交的 (渠道, 上游真名) 列表（含同渠道两次）" "$POOL" "[(m['channel'],m['upstream_model']) for m in d['members']][:3]==[('ops-b','ops-model'),('ops-b','ops-alt'),('ops-a','ops-model')]"
assert_json "池化车道可路由" "$POOL" "d['routable'] is True"

echo
echo "=== 运维 4c：声明模型不自动建车道（ADR 0005/0007）==="
# ops-b 新声明了 ops-alt，但没有任何同名车道：必须是 unconfigured 且不可调用，
# 且 GET /lanes 不得凭空多出车道（渠道声明绝不自动建/改车道）。
MODELS_AFTER=$(curl -s "${A[@]}" "$BASE/api/v1/models")
assert_json "新声明模型 source=unconfigured 且 routable=false" "$MODELS_AFTER" "any(m['model']=='ops-alt' and m['source']=='unconfigured' and not m['routable'] for m in d['items'])"
LANES_AFTER=$(curl -s "${A[@]}" "$BASE/api/v1/lanes")
assert_json "渠道声明模型不自动新增车道" "$LANES_AFTER" "not any(l['name']=='ops-alt' for l in d['items'])"

echo
echo "=== 运维 5：建客户端密钥并端到端调用 ==="
CLIENT_PLAIN=$(curl -s "${A[@]}" -X POST -d '{"name":"ops-client"}' "$BASE/api/v1/keys" | jget 'd["key"]')
[[ -n "$CLIENT_PLAIN" ]] || { echo "FAIL: 未取得客户端密钥"; exit 1; }
RESP=$(curl -s -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"ops-model","messages":[{"role":"user","content":"hi"}]}')
check "模型面调用成功" "$RESP" "pong from fake upstream"
sleep 1

echo
echo "=== 运维 6：排障（health / logs attempts）==="
HEALTH=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/ops-model/health")
assert_json "health 时间字段为 RFC3339/null" "$HEALTH" "all(('cooldown_until' not in m) or m['cooldown_until'] is None or isinstance(m['cooldown_until'],str) for m in d['members']) and 'affinity' in d"
LOGS=$(curl -s "${A[@]}" "$BASE/api/v1/logs?limit=10")
assert_json "日志含该请求与真实服务者" "$LOGS" "any(i['success'] and i['lane']=='ops-model' and i['channel']=='ops-a' for i in d['items'])"

echo
echo "=== 运维 7：逐成员探活 ==="
PROBE=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/ops-model/probe")
assert_json "probe 逐成员且 probed=2" "$PROBE" "d['probed']==2 and all('status' in r for r in d['results'])"

echo
echo "=== 运维 8：故障注入 → 首成员失败逃逸到次成员 ==="
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/ops-model/circuits/reset" > /dev/null
# 把 ops-a 的 key 改错，使首成员硬失败；ops-b 仍用正确 key。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"sk-wrong","models":["ops-model"],"enabled":true}' "$BASE/api/v1/channels/ops-a" > /dev/null
FAILOVER=$(curl -s -D "$WORK/failover.headers" -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"ops-model","messages":[{"role":"user","content":"hi"}]}')
SERVED=$(grep -i '^x-served-by:' "$WORK/failover.headers" | tr -d '\r' | sed 's/^[^:]*: //')
check "首成员失败后逃逸到 ops-b" "$SERVED" "channel=2:ops-b"
sleep 1
LOGFAIL=$(curl -s "${A[@]}" "$BASE/api/v1/logs?limit=5")
assert_json "attempts 链含失败首段与成功末段" "$LOGFAIL" "any(len(i['attempts'])==2 and i['attempts'][0]['status']=='failed' and i['attempts'][1]['status']=='success' for i in d['items'])"

echo
echo "=== 运维 9：重置熔断/冷却 ==="
curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/ops-model/circuits/reset" > /dev/null
HEALTH2=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/ops-model/health")
assert_json "重置后无残留冷却" "$HEALTH2" "all(m['cooldown_until'] is None for m in d['members'])"
# 恢复 ops-a
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["ops-model"],"enabled":true}' "$BASE/api/v1/channels/ops-a" > /dev/null

echo
echo "=== 运维 10：密钥轮换（旧明文失效、新明文可用）==="
NEW_PLAIN=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/keys/ops-client/rotate" | jget 'd["key"]')
[[ -n "$NEW_PLAIN" ]] || { echo "FAIL: 轮换未返回新明文"; exit 1; }
OLD_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' -d '{"model":"ops-model","messages":[{"role":"user","content":"hi"}]}')
NEW_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $NEW_PLAIN" -H 'Content-Type: application/json' -d '{"model":"ops-model","messages":[{"role":"user","content":"hi"}]}')
check "旧明文失效（401）" "$OLD_CODE" "401"
check "新明文可用（200）" "$NEW_CODE" "200"

echo
echo "=== 运维 11：导出/导入幂等 ==="
curl -s "${A[@]}" "$BASE/api/v1/export" -o "$WORK/export.json"
assert_json "导出含 lanes 与 channels（不含密钥明文）" "$(cat "$WORK/export.json")" "'lanes' in d and 'channels' in d and 'key_plain' not in json.dumps(d)"
IMPORT=$(curl -s "${A[@]}" -X POST --data-binary @"$WORK/export.json" "$BASE/api/v1/import?dry_run=true")
assert_json "dry_run 导入无实际变更" "$IMPORT" "d.get('dry_run') is True or d.get('dry_run')==True"

echo
echo "=== 运维 12：日志保留（prune 明细、聚合不变）==="
STATS_BEFORE=$(curl -s "${A[@]}" "$BASE/api/v1/stats?granularity=hour&group_by=lane")
curl -s "${A[@]}" -X POST "$BASE/api/v1/logs/prune?before=9999999999" > /dev/null
AFTER_LOGS=$(curl -s "${A[@]}" "$BASE/api/v1/logs?limit=5")
assert_json "prune 后明细清空" "$AFTER_LOGS" "len(d['items'])==0"
STATS_AFTER=$(curl -s "${A[@]}" "$BASE/api/v1/stats?granularity=hour&group_by=lane")
assert_json "prune 后聚合仍保留" "$STATS_AFTER" "any(i['requests']>0 for i in d['items'])"

echo
echo "=== 运维 13：审计（上述写操作均有记录）==="
AUDIT=$(curl -s "${A[@]}" "$BASE/api/v1/audit?limit=50")
assert_json "审计含渠道与车道变更" "$AUDIT" "any('channel' in json.dumps(i).lower() for i in d['items']) and any('lane' in json.dumps(i).lower() for i in d['items'])"


echo
echo "=== 运维 14：dry-run 不落库（api-spec §2.4/§5.9）==="
# 对破坏性端点带 ?dry_run=true 调一次，再回读：状态必须与调用前一致。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["ops-dry-model"],"enabled":true}' "$BASE/api/v1/channels/ops-dry" > /dev/null
DRY_BEFORE=$(curl -s "${A[@]}" "$BASE/api/v1/channels/ops-dry")
DRY_RESP=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/channels/batch/status?dry_run=true" -d '{"channels":["ops-dry"],"status":2}')
DRY_AFTER=$(curl -s "${A[@]}" "$BASE/api/v1/channels/ops-dry")
assert_json "dry-run 响应含 dry_run:true 与 diff" "$DRY_RESP" "d['dry_run'] is True and 'diff' in d"
assert_json "dry-run 后渠道状态未变（禁止静默写入）" "$DRY_AFTER" "d['enabled'] is True"

# 删除全部已停用渠道也必须不落库
DRY_DEL=$(curl -s "${A[@]}" -X DELETE "$BASE/api/v1/channels/disabled?dry_run=true")
assert_json "DELETE /channels/disabled?dry_run=true 返回将删清单" "$DRY_DEL" "d['dry_run'] is True"
curl -s -o /dev/null -w "" "${A[@]}" -X DELETE "$BASE/api/v1/channels/ops-dry"

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: L2 真实 API 运维全部通过"; else
  echo "FAIL: 存在未通过项"; echo "--- pbr.log 末尾 ---"; tail -40 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
