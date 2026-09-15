#!/usr/bin/env bash
# W3 验收：访问与 AI 管理面（客户端密钥、车道权限、自描述、配置生命周期）。
#
# 验收门（docs/goal-prompt.md §四 W3）：
#   每类资源"写→GET 回读"断言一致；openapi 可被解析；
#   export→import(dry_run) diff 为空；库里 grep 不到请求正文。
# 附加（design-v1 §5、token-spec §2/§3/§4）：未初始化 409、错误密钥 401、
#   被拒车道 403、弱口令有明确提示、密钥明文只回显一次且不入库、
#   PBR 客户端密钥可驱动模型面、管理密钥不能用于模型面。
#
# 全程本地：独立端口、独立 SQLite、内置假上游；不触碰任何现网容器与凭据。
set -uo pipefail

REPO=/root/powerbar-rations
WORK=/tmp/pbr-w3
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/w3/run-$STAMP.log"
PORT=6794
UPSTREAM_PORT=6805
GOOD_KEY='sk-w3-good'
PBR_PW='Pbr-W3-Verify-Passw0rd!2026'

exec > "$EVID" 2>&1
echo "=== W3 访问与管理面验收 @ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}
check_not() {
  local desc="$1" actual="$2" unwanted="$3"
  if [[ "$actual" != *"$unwanted"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc（不应包含 $unwanted）"; FAIL=$((FAIL+1)); fi
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
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: build"; exit 1; }

"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.stdout" 2>&1 &
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=w3-session CRYPTO_SECRET=w3-crypto GIN_MODE=release \
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
jq1() { python3 -c "import sys,json;print(eval(sys.argv[1],{'d':json.load(sys.stdin),'json':json}))" "$1"; }

echo
echo "=== 未初始化：管理面应 409 not_initialized ==="
CODE=$(curl -s -o "$WORK/b.json" -w '%{http_code}' "$BASE/api/v1/lanes")
echo "  GET /lanes → $CODE $(cat "$WORK/b.json")"
check "未初始化返回 409" "$CODE" "409"
check "错误码为 not_initialized" "$(cat "$WORK/b.json")" '"code":"not_initialized"'
STATUS=$(curl -s "$BASE/api/v1/setup/status")
check "setup/status 报告未初始化" "$STATUS" '"initialized":false'

echo
echo "=== 口令：弱口令明确提示、过短拒绝 ==="
WEAK=$(curl -s $H -d '{"password":"weak1234"}' "$BASE/api/v1/setup")
echo "  弱口令响应: $(echo "$WEAK" | head -c 200)"
check "弱口令给出明确提示" "$WEAK" '"warning"'
check "弱口令仍可完成初始化" "$WEAK" '"initialized":true'
ADMIN_KEY=$(echo "$WEAK" | jget 'd["admin_key"]')
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')

# 改成一个足够长的口令（后续沿用），并验证旧密钥立即失效
curl -s "${A[@]}" -X POST -d '{"current":"weak1234","new":"'"$PBR_PW"'"}' "$BASE/api/v1/auth/password" > /dev/null
OLD=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $ADMIN_KEY" "$BASE/api/v1/lanes")
check "改口令后旧管理密钥立即失效（401）" "$OLD" "401"
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/auth/login" | jget 'd["token"]')
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 登录未取到管理密钥"; exit 1; }
echo "  admin_key: 已重新取得（${#ADMIN_KEY} 字节，不打印）"

echo
echo "=== 渠道：写 → 回读一致；密钥只写不读 ==="
curl -s "${A[@]}" -X PUT -d '{
  "type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'",
  "priority":20,"models":["w3-model"],"enabled":true,"param_override":{"temperature":1}
}' "$BASE/api/v1/channels/channel-a" > /dev/null
curl -s "${A[@]}" -X PUT -d '{
  "type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'",
  "priority":10,"models":["w3-model"],"enabled":true
}' "$BASE/api/v1/channels/channel-b" > /dev/null
CH=$(curl -s "${A[@]}" "$BASE/api/v1/channels/channel-a")
echo "  $CH"
check "回读 type 一致" "$CH" '"type":"openai"'
check "回读 priority 一致" "$CH" '"priority":20'
check "回读 models 一致" "$CH" '"models":["w3-model"]'
check "回读 param_override 一致" "$CH" '"temperature":1'
check "key_set=true" "$CH" '"key_set":true'
check_not "不回显密钥明文" "$CH" "$GOOD_KEY"

echo
echo "=== 车道：写 → 回读一致 ==="
curl -s "${A[@]}" -X PUT -d '{
  "enabled":true,"mode":"failover",
  "config":{"member_max_attempts":1,"member_retry_interval_seconds":0,
            "member_non_stream_response_timeout_seconds":60,"member_stream_first_event_timeout_seconds":30,
            "member_cooldown_seconds":5,"member_affinity_seconds":0},
  "members":[
    {"channel":"channel-a","upstream_model":"w3-model","priority":20},
    {"channel":"channel-b","upstream_model":"w3-model","priority":10,"weight":3}
  ]
}' "$BASE/api/v1/lanes/lane-verify" > /dev/null
LANE=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/lane-verify")
echo "  $(echo "$LANE" | head -c 400)"
check "回读 mode 一致" "$LANE" '"mode":"failover"'
assert_json "回读成员数与优先级顺序一致" "$LANE" "[m['channel'] for m in d['members']]==['channel-a','channel-b'] and [m['priority'] for m in d['members']]==[20,10]"
assert_json "回读六键一致" "$LANE" "d['config']['member_max_attempts']==1 and d['config']['member_cooldown_seconds']==5"
assert_json "回读成员权重一致" "$LANE" "d['members'][1]['weight']==3"

echo
echo "=== 客户端密钥：创建回显一次；此后只有前缀 ==="
CREATE=$(curl -s "${A[@]}" -X POST -d '{"name":"client-a","lane_policy":{"mode":"all","allow_lanes":[],"deny_lanes":[]}}' "$BASE/api/v1/keys")
echo "  $(echo "$CREATE" | head -c 300)"
CLIENT_PLAIN=$(echo "$CREATE" | jget 'd["key"]')
[[ -n "$CLIENT_PLAIN" ]] || { echo "FAIL: 创建未返回明文"; exit 1; }
echo "  明文已取得（${#CLIENT_PLAIN} 字节，不打印；前缀 ${CLIENT_PLAIN:0:12}）"
check "明文以 pbr- 开头" "$CLIENT_PLAIN" "pbr-"
KEY_GET=$(curl -s "${A[@]}" "$BASE/api/v1/keys/client-a")
echo "  $KEY_GET"
check_not "再次读取不含明文" "$KEY_GET" "$CLIENT_PLAIN"
check "读取含 key_prefix" "$KEY_GET" '"key_prefix":"'"${CLIENT_PLAIN:0:12}"'"'
check "默认放行全部车道" "$KEY_GET" '"mode":"all"'

echo "--- 显式拒绝车道：deny_lanes 生效"
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"lane_policy":{"mode":"all","allow_lanes":[],"deny_lanes":["w3-model"]}}' \
  "$BASE/api/v1/keys/client-a" > /dev/null
DENIED=$(curl -s -o "$WORK/denied.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"w3-model","messages":[{"role":"user","content":"ping"}]}')
echo "  被拒车道 → $DENIED $(cat "$WORK/denied.json")"
check "被拒车道返回 403" "$DENIED" "403"
check "错误码为 forbidden_scope" "$(cat "$WORK/denied.json")" '"code":"forbidden_scope"'
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"lane_policy":{"mode":"all","allow_lanes":[],"deny_lanes":[]}}' \
  "$BASE/api/v1/keys/client-a" > /dev/null

echo
echo "=== 模型面：PBR 客户端密钥可驱动转发 ==="
SENTINEL="pbr-w3-body-sentinel-$STAMP"
RESP=$(curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"w3-model","messages":[{"role":"user","content":"'"$SENTINEL"'"}]}')
echo "  $(echo "$RESP" | head -c 240)"
check "PBR 客户端密钥转发成功" "$RESP" 'pong from fake upstream'
check "响应 model 回填请求名" "$RESP" '"model":"w3-model"'

echo "--- 管理密钥不能用于模型面（token-spec §4.2）"
CROSS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{"model":"w3-model","messages":[{"role":"user","content":"ping"}]}')
check "管理密钥打模型面返回 401" "$CROSS" "401"

echo "--- 停用/轮换：立即生效"
curl -s "${A[@]}" -X PUT -d '{"enabled":false}' "$BASE/api/v1/keys/client-a" > /dev/null
DISABLED=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"w3-model","messages":[{"role":"user","content":"ping"}]}')
check "停用后 401" "$DISABLED" "401"
ROTATED=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/keys/client-a/rotate")
NEW_PLAIN=$(echo "$ROTATED" | jget 'd["key"]')
curl -s "${A[@]}" -X PUT -d '{"enabled":true}' "$BASE/api/v1/keys/client-a" > /dev/null
OLD_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"w3-model","messages":[{"role":"user","content":"ping"}]}')
NEW_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $NEW_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"w3-model","messages":[{"role":"user","content":"ping"}]}')
check "轮换后旧明文失效（401）" "$OLD_CODE" "401"
check "轮换后新明文可用（200）" "$NEW_CODE" "200"
CLIENT_PLAIN="$NEW_PLAIN"

echo
echo "=== openapi 可被解析 ==="
curl -s "${A[@]}" "$BASE/api/v1/openapi.json" -o "$WORK/openapi.json"
python3 - "$WORK/openapi.json" <<'PY' && { echo "  PASS: openapi.json 可解析且含 paths"; PASS=$((PASS+1)); } || { echo "  FAIL: openapi 解析失败"; FAIL=$((FAIL+1)); }
import json, sys
doc = json.load(open(sys.argv[1]))
assert doc["openapi"].startswith("3.")
assert "/channels/{name}" in doc["paths"]
assert "/keys/{name}/rotate" in doc["paths"]
PY
CAP=$(curl -s "${A[@]}" "$BASE/api/v1/capabilities")
check "capabilities 列出四种模式" "$CAP" '"round_robin"'

echo
echo "=== export → import(dry_run) diff 为空 ==="
curl -s "${A[@]}" "$BASE/api/v1/export" -o "$WORK/export.json"
EXPORT_KEYS=$(python3 -c "import json;d=json.load(open('$WORK/export.json'));print(','.join(sorted(d.keys())))")
echo "  导出字段: $EXPORT_KEYS"
check_not "导出不含密钥明文" "$(cat "$WORK/export.json")" "$CLIENT_PLAIN"
check_not "导出不含密钥哈希" "$(cat "$WORK/export.json")" "key_hash"
IMPORT=$(curl -s "${A[@]}" -X POST --data-binary @"$WORK/export.json" "$BASE/api/v1/import?dry_run=true")
echo "  $(echo "$IMPORT" | head -c 500)"
# diff 键名遵循 api-spec §6.10：channels/lanes/keys 为 add/update/remove 三分类，
# options 为 {changed:[...]}。故只对前三者断言 add/update 为空。
assert_json "import dry_run 的 add 全为空" "$IMPORT" "all(len(d['diff'][k]['add'])==0 for k in ('channels','lanes','keys'))"
assert_json "import dry_run 的 update 全为空" "$IMPORT" "all(len(d['diff'][k]['update'])==0 for k in ('channels','lanes','keys'))"
assert_json "import dry_run 标记 unchanged" "$IMPORT" "len(d['diff']['channels']['unchanged'])==2 and len(d['diff']['lanes']['unchanged'])==1"
assert_json "import dry_run 的 options 无变更" "$IMPORT" "len(d['diff']['options']['changed'])==0"

echo
echo "=== 审计：写了变更就应有记录，且不含正文 ==="
AUDIT=$(curl -s "${A[@]}" "$BASE/api/v1/audit")
echo "  $(echo "$AUDIT" | head -c 300)"
check "审计有记录" "$AUDIT" '"resource":"channel"'
check "审计含 lane 变更" "$AUDIT" '"resource":"lane"'
check "审计含密钥轮换动作" "$AUDIT" '"action":"rotate"'
check_not "审计不含请求正文" "$AUDIT" "$SENTINEL"

echo
echo "=== 库里 grep 不到请求正文 ==="
if grep -a -q "$SENTINEL" "$WORK/pbr.db" 2>/dev/null; then
  echo "  FAIL: 数据库中出现请求正文哨兵"; FAIL=$((FAIL+1))
else
  echo "  PASS: 数据库中没有请求正文哨兵"; PASS=$((PASS+1))
fi
if grep -a -q "$CLIENT_PLAIN" "$WORK/pbr.db" 2>/dev/null; then
  echo "  FAIL: 数据库中出现客户端密钥明文"; FAIL=$((FAIL+1))
else
  echo "  PASS: 数据库中没有客户端密钥明文"; PASS=$((PASS+1))
fi
if grep -ra -q "$ADMIN_KEY" "$WORK/pbr.db" 2>/dev/null; then
  echo "  FAIL: 数据库中出现管理密钥明文"; FAIL=$((FAIL+1))
else
  echo "  PASS: 数据库中没有管理密钥明文"; PASS=$((PASS+1))
fi
LOGS_LEAK=$(grep -a -c "$CLIENT_PLAIN" "$WORK/pbr.log" 2>/dev/null || true)
check "运行日志不含客户端密钥明文" "${LOGS_LEAK:-0}" "0"

echo
echo "--- 私有数据自查：源码中不得出现本次测试密钥"
LEAK=$(grep -rn "sk-w3-good" "$REPO" --exclude-dir=.git --exclude-dir=verify 2>/dev/null | head -5)
if [[ -z "$LEAK" ]]; then echo "  PASS: 源码无测试密钥残留"; PASS=$((PASS+1)); else echo "  FAIL: $LEAK"; FAIL=$((FAIL+1)); fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: W3 验收门全部通过"; else
  echo "FAIL: 存在未通过项"; echo "--- pbr.log 末尾 ---"; tail -50 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
