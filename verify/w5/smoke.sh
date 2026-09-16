#!/usr/bin/env bash
# W5 验收：元数据日志与成本折算。
#
# 验收门（docs/goal-prompt.md §四 W5）：
#   20 条混合请求 → 日志行数 = 请求数；attempts 完整；单条平均 < 2KB。
# 附加（design-v1 §8、api-spec §5.5）：只存元数据（无正文、无余额字段）、
#   成本只折算不扣费、/stats 聚合可用、失败请求也留痕（含 503 快抛）。
#
# 全程本地：独立端口、独立 SQLite、内置假上游；不触碰任何现网容器与凭据。
set -uo pipefail

REPO=/root/powerbar-rations
WORK=/tmp/pbr-w5
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/w5/run-$STAMP.log"
PORT=6795
UPSTREAM_PORT=6806
GOOD_KEY='sk-w5-good'
BAD_KEY='sk-w5-bad'
PBR_PW='Pbr-W5-Verify-Passw0rd!2026'
REQUESTS=20

exec > "$EVID" 2>&1
echo "=== W5 元数据日志与记账验收 @ $STAMP ==="

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
  SESSION_SECRET=w5-session CRYPTO_SECRET=w5-crypto GIN_MODE=release \
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

echo "--- 2) 初始化与配置"
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')

for name in channel-a:20:"$GOOD_KEY" channel-b:10:"$GOOD_KEY"; do
  ch=$(echo "$name" | cut -d: -f1); pr=$(echo "$name" | cut -d: -f2); ky=$(echo "$name" | cut -d: -f3)
  curl -s "${A[@]}" -X PUT -d '{
    "type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$ky"'",
    "priority":'"$pr"',"models":["w5-model"],"enabled":true
  }' "$BASE/api/v1/channels/$ch" > /dev/null
done
curl -s "${A[@]}" -X PUT -d '{
  "enabled":true,"mode":"failover",
  "config":{"member_max_attempts":1,"member_retry_interval_seconds":0,
            "member_non_stream_response_timeout_seconds":60,"member_stream_first_event_timeout_seconds":30,
            "member_cooldown_seconds":1,"member_affinity_seconds":0},
  "members":[
    {"channel":"channel-a","upstream_model":"w5-model","priority":20},
    {"channel":"channel-b","upstream_model":"w5-model","priority":10}
  ]
}' "$BASE/api/v1/lanes/w5-model" > /dev/null
CLIENT_PLAIN=$(curl -s "${A[@]}" -X POST -d '{"name":"client-w5"}' "$BASE/api/v1/keys" | jget 'd["key"]')
[[ -n "$CLIENT_PLAIN" ]] || { echo "FAIL: 未取得客户端密钥"; exit 1; }

# 单价表（design-v1 §16.9#7）：人民币/百万 token，只用于日志折算，不参与准入、不扣额度。
# 断言按"实际 usage × 单价 / 1e6"推导，而不是写死数字：假上游的 token 数变化时
# 这条用例仍然验的是"折算公式对不对"，而不是"fixture 有没有变"。
curl -s "${A[@]}" -X PUT -d '{"model_prices":[{"model":"w5-model","input":1000,"output":2000}]}' \
  "$BASE/api/v1/system/options" > /dev/null
OPTIONS_AFTER_PRICING=$(curl -s "${A[@]}" "$BASE/api/v1/system/options")
echo "  配置完成（管理密钥与客户端密钥均不打印）"

chat() { # chat <model> [client-key]
  local model="$1" key="${2:-$CLIENT_PLAIN}"
  curl -s -o "$WORK/body.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
    -H "Authorization: Bearer $key" -H 'Content-Type: application/json' \
    -d "$3"
}
control() { curl -s $H -X POST -d "$1" "http://127.0.0.1:$UPSTREAM_PORT/__control" > /dev/null; }
ok_body() { echo '{"model":"'"$1"'","messages":[{"role":"user","content":"ping"}]}'; }

echo
echo "=== 造 $REQUESTS 条混合请求 ==="
COUNT=0
# 1) 12 条成功
for i in $(seq 1 12); do
  CODE=$(chat w5-model "$CLIENT_PLAIN" "$(ok_body w5-model)"); COUNT=$((COUNT+1))
  [[ "$CODE" == "200" ]] || { echo "  ! 第 $i 条成功请求返回 $CODE"; }
done
echo "  成功请求 12 条"

# 2) 2 条未知模型 → 503（中间件快抛）
for i in 1 2; do
  CODE=$(chat ghost-model "$CLIENT_PLAIN" "$(ok_body ghost-model)"); COUNT=$((COUNT+1))
  echo "  未知模型 → $CODE"
done

# 3) 2 条 client_error → 400（请求体不合法，基座校验层）
for i in 1 2; do
  CODE=$(chat w5-model "$CLIENT_PLAIN" '{"model":"w5-model"}'); COUNT=$((COUNT+1))
  echo "  缺 messages → $CODE"
done

# 4) 2 条故障转移：打坏 P1 的 key → 落 P2，attempts 应为 2 段
control '{"model":"w5-model","status":401,"body":"{\"error\":{\"message\":\"invalid key\"}}"}'
curl -s "${A[@]}" -X PUT -d '{
  "type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$BAD_KEY"'",
  "priority":20,"models":["w5-model"],"enabled":true
}' "$BASE/api/v1/channels/channel-a" > /dev/null
control '{"model":"w5-model","status":200}'
for i in 1 2; do
  CODE=$(chat w5-model "$CLIENT_PLAIN" "$(ok_body w5-model)"); COUNT=$((COUNT+1))
  echo "  故障转移 → $CODE"
done

# 5) 2 条被拒车道 → 403（中间件阶段结束）
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"lane_policy":{"mode":"allow","allow_lanes":["other-model"],"deny_lanes":[]}}' \
  "$BASE/api/v1/keys/client-w5" > /dev/null
for i in 1 2; do
  CODE=$(chat w5-model "$CLIENT_PLAIN" "$(ok_body w5-model)"); COUNT=$((COUNT+1))
  echo "  被拒车道 → $CODE"
done
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"lane_policy":{"mode":"all","allow_lanes":[],"deny_lanes":[]}}' \
  "$BASE/api/v1/keys/client-w5" > /dev/null

echo "  总请求数：$COUNT"
check "脚本发出的请求数等于约定值" "$COUNT" "$REQUESTS"

echo
echo "=== 验收门：日志行数 = 请求数 ==="
LOGS=$(curl -s "${A[@]}" "$BASE/api/v1/logs?limit=200")
ROWS=$(echo "$LOGS" | jq1 "len(d['items'])")
echo "  日志行数：$ROWS"
check "日志行数等于请求数" "$ROWS" "$REQUESTS"
assert_json "成功与失败都留痕" "$LOGS" "any(i['success'] for i in d['items']) and any(not i['success'] for i in d['items'])"
assert_json "503 快抛也有日志且无可用成员" "$LOGS" "any(i['http_status']==503 and i['request_model']=='ghost-model' for i in d['items'])"
assert_json "403 被拒车道也有日志" "$LOGS" "any(i['http_status']==403 for i in d['items'])"
assert_json "client_error 归类为 client_error" "$LOGS" "any(i['error_kind']=='client_error' for i in d['items'])"

echo
echo "=== 验收门：attempts 完整 ==="
# 脚本连发 2 条故障转移：第 1 条是 P1 真失败后换 P2（链首为 failed），第 2 条时
# P1 已在冷却中（链首为 cooldown）。两条都满足 total_attempts==2，故按"链首状态"
# 精确选取，不能只按条数取（否则会拿到冷却态那条）。
FAILOVER_ID=$(echo "$LOGS" | jq1 "[i['id'] for i in d['items'] if i['success'] and i['total_attempts']==2 and len(i['attempts'])>0 and i['attempts'][0]['status']=='failed'][0]")
echo "  故障转移那条的 id：$FAILOVER_ID"
DETAIL=$(curl -s "${A[@]}" "$BASE/api/v1/logs/$FAILOVER_ID")
echo "  $(echo "$DETAIL" | head -c 500)"
assert_json "attempts 链为 2 段且首段失败、末段成功" "$DETAIL" \
  "len(d['attempts'])==2 and d['attempts'][0]['status']=='failed' and d['attempts'][1]['status']=='success'"
assert_json "首段记录了错误分类" "$DETAIL" "d['attempts'][0]['error_kind']=='hard_auth'"
assert_json "首段指向 P1、末段指向 P2" "$DETAIL" \
  "'channel-a' in d['attempts'][0]['member'] and 'channel-b' in d['attempts'][1]['member']"
assert_json "记录了两段的耗时" "$DETAIL" \
  "all(isinstance(a.get('duration_ms'), int) for a in d['attempts'])"

# 冷却态那条：链首必须留下 cooldown 记录（routing-spec §9：解释"为什么没用 P1"）
COOLDOWN_ID=$(echo "$LOGS" | jq1 "[i['id'] for i in d['items'] if i['success'] and i['total_attempts']==2 and len(i['attempts'])>0 and i['attempts'][0]['status']=='cooldown'][0]")
echo "  冷却跳过那条的 id：$COOLDOWN_ID"
if [[ -n "$COOLDOWN_ID" && "$COOLDOWN_ID" != "None" ]]; then
  COOLDOWN_DETAIL=$(curl -s "${A[@]}" "$BASE/api/v1/logs/$COOLDOWN_ID")
  assert_json "被冷却跳过的成员留下 cooldown 记录" "$COOLDOWN_DETAIL" \
    "d['attempts'][0]['status']=='cooldown' and 'channel-a' in d['attempts'][0]['member']"
fi
assert_json "记录了路由来源与真实服务者" "$DETAIL" \
  "d['route_source']=='explicit' and d['channel']=='channel-b' and d['upstream_model']=='w5-model'"
assert_json "记录了密钥归属" "$DETAIL" "d['key_name']=='client-w5'"
SUCCESS_ID=$(echo "$LOGS" | jq1 "[i['id'] for i in d['items'] if i['success'] and i['total_attempts']==1][0]")
SUCCESS_DETAIL=$(curl -s "${A[@]}" "$BASE/api/v1/logs/$SUCCESS_ID")
assert_json "一次成功请求的 attempts 为单段成功" "$SUCCESS_DETAIL" \
  "len(d['attempts'])==1 and d['attempts'][0]['status']=='success'"
assert_json "成功请求带 token 用量与折算金额" "$SUCCESS_DETAIL" \
  "d['prompt_tokens']>=0 and d['completion_tokens']>=0 and d['total_ms']>=0"

echo
echo "=== 验收门：单价表只折算不扣费（design-v1 §16.9#7 / G7）==="
assert_json "PUT 后回读单价表一致" "$OPTIONS_AFTER_PRICING" \
  "len(d['model_prices'])==1 and d['model_prices'][0]['model']=='w5-model' and d['model_prices'][0]['input']==1000 and d['model_prices'][0]['output']==2000"
assert_json "按单价表折算出的金额正确（usage × 单价 / 1e6）" "$SUCCESS_DETAIL" \
  "abs(d['estimated_cost'] - (d['prompt_tokens']*1000 + d['completion_tokens']*2000)/1e6) < 1e-9 and d['estimated_cost'] > 0"
# 未配置单价的模型不折算：请求模型名才是计价键，找不到就不给金额（不是回退到基座比例价）。
curl -s "${A[@]}" -X PUT -d '{"model_prices":[]}' "$BASE/api/v1/system/options" > /dev/null
CLEARED=$(curl -s "${A[@]}" "$BASE/api/v1/system/options")
assert_json "清空单价表后回读为空" "$CLEARED" "d['model_prices']==[]"

echo
echo "=== 验收门：单条平均 < 2KB ==="
SIZE_REPORT=$(python3 - "$WORK/pbr.db" <<'PY'
import sqlite3, sys
conn = sqlite3.connect(sys.argv[1])
cur = conn.cursor()
cur.execute("""SELECT COUNT(*),
  COALESCE(SUM(LENGTH(COALESCE(lane_name,''))+LENGTH(COALESCE(request_model,''))+
    LENGTH(COALESCE(route_source,''))+LENGTH(COALESCE(member_channel_name,''))+
    LENGTH(COALESCE(upstream_model,''))+LENGTH(COALESCE(token_name,''))+
    LENGTH(COALESCE(inbound_format,''))+LENGTH(COALESCE(error_kind,''))+
    LENGTH(COALESCE(error_summary,''))+LENGTH(COALESCE(attempts,''))),0) FROM pbr_request_logs""")
count, total = cur.fetchone()
print(f"{count} {total} {total/max(count,1):.1f}")
PY
)
echo "  行数 / 载荷总字节 / 平均字节：$SIZE_REPORT"
AVG=$(echo "$SIZE_REPORT" | awk '{print $3}')
AVG_INT=${AVG%.*}
if [[ -n "$AVG_INT" ]] && [[ "$AVG_INT" -lt 2048 ]]; then
  echo "  PASS: 单条平均载荷 ${AVG} 字节 < 2048"; PASS=$((PASS+1))
else
  echo "  FAIL: 单条平均载荷 ${AVG} 字节未小于 2048"; FAIL=$((FAIL+1))
fi

echo
echo "=== 验收门：只存元数据 ==="
BODY_LEAK=$(python3 - "$WORK/pbr.db" <<'PY'
import sqlite3, sys
conn = sqlite3.connect(sys.argv[1])
cur = conn.cursor()
cur.execute("SELECT COUNT(*) FROM pbr_request_logs WHERE error_summary LIKE '%ping%'")
print(cur.fetchone()[0])
PY
)
check "日志里没有请求正文内容" "$BODY_LEAK" "0"
SCHEMA=$(python3 - "$WORK/pbr.db" <<'PY'
import sqlite3, sys
conn = sqlite3.connect(sys.argv[1])
cols = [r[1] for r in conn.execute("PRAGMA table_info(pbr_request_logs)")]
banned = [c for c in cols if c in ("quota","remain_quota","used_quota","content","body","request_body","response_body")]
print(",".join(banned) if banned else "clean")
PY
)
check "表里没有额度/正文列" "$SCHEMA" "clean"
check_not "API 不返回请求正文" "$DETAIL" '"content":'

echo
echo "=== 附加：/stats 聚合与筛选 ==="
STATS=$(curl -s "${A[@]}" "$BASE/api/v1/stats?granularity=hour&group_by=lane")
echo "  $(echo "$STATS" | head -c 400)"
assert_json "stats 按车道聚合出 w5-model" "$STATS" "any(i['group']=='w5-model' and i['requests']>0 for i in d['items'])"
assert_json "聚合里成功数小于等于请求数" "$STATS" "all(i['successes']<=i['requests'] for i in d['items'])"
FILTERED=$(curl -s "${A[@]}" "$BASE/api/v1/logs?success=false&limit=200")
assert_json "按 success=false 过滤有效" "$FILTERED" "all(not i['success'] for i in d['items']) and len(d['items'])>0"

echo
echo "--- 附加：prune 明细后 /stats 历史仍在（/stats 读聚合表，审查 F5）"
PRUNE=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/logs/prune?before=9999999999")
echo "  prune: $PRUNE"
check "prune 删除了明细" "$PRUNE" '"deleted":'
DETAIL_AFTER=$(curl -s "${A[@]}" "$BASE/api/v1/logs?limit=200")
assert_json "明细已清空（证明下一条断言不是靠明细）" "$DETAIL_AFTER" "len(d['items'])==0"
STATS_AFTER=$(curl -s "${A[@]}" "$BASE/api/v1/stats?granularity=hour&group_by=lane")
echo "  $(echo "$STATS_AFTER" | head -c 300)"
assert_json "prune 后 stats 仍能聚合出 w5-model" "$STATS_AFTER" "any(i['group']=='w5-model' and i['requests']>0 for i in d['items'])"

echo
echo "--- 私有数据自查：源码中不得出现本次测试密钥"
LEAK=$(grep -rn "sk-w5-good\|sk-w5-bad" "$REPO" --exclude-dir=.git --exclude-dir=verify 2>/dev/null | head -5)
if [[ -z "$LEAK" ]]; then echo "  PASS: 源码无测试密钥残留"; PASS=$((PASS+1)); else echo "  FAIL: $LEAK"; FAIL=$((FAIL+1)); fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: W5 验收门全部通过"; else
  echo "FAIL: 存在未通过项"; echo "--- pbr.log 末尾 ---"; tail -40 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
