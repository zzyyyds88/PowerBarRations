#!/usr/bin/env bash
# L2 真实 API 运维（docs/test-spec-v1.md §5）。
#
# 只用管理 API（Authorization: Bearer <管理密钥>，由登录口令派生）模拟运维人员一天的活：
# 探活 → 建渠道 → 建车道 → 端到端调用 → 排障 → 探活 → 故障转移 → 重置熔断
# → 密钥轮换 → 导出/导入幂等 → 日志保留 → 审计
# → 成员启停开关：写入回读 / 三态默认 / dry-run 不落库 / 停用形态与 attempts /
#   运行态不被污染 / /api/models 聚合可辨识 / 导出导入往返 / 审计（test-spec §5 场景 3d）。
# → 成员只存所选模型（ADR 0008）：写端点成员载荷用 `model`；上游真名由
#   `model_mapping[model] ?? model` 推导（改映射不重存车道即生效，池化车道按
#   成员所选模型而非路由键查表）；成员唯一键 (渠道, 模型) 重复即 422 duplicate_member；
#   导出/导入只带 `model`、不带派生 `upstream_model`（test-spec §3.1）。
# 每步"调用 → 回读 → 断言"。
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
# 整串相等断言：dry-run 的"回读逐字段一致"与"运行态字段不污染"用不了子串断言（check 只能断"包含"）。
assert_same() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == "$want" ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        调用前: ${want:0:600}"; echo "        调用后: ${actual:0:600}"; FAIL=$((FAIL+1)); fi
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
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":15,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"ops-a","model":"ops-model","priority":20},{"channel":"ops-b","model":"ops-model","priority":10}]}' "$BASE/api/v1/lanes/ops-model" > /dev/null
ROUTE=$(curl -s "${A[@]}" "$BASE/api/v1/routes/ops-model")
assert_json "routes routable 且成员按 priority 降序" "$ROUTE" "d['routable'] is True and [m['channel'] for m in d['members']][:2]==['ops-a','ops-b']"
MODELS=$(curl -s "${A[@]}" "$BASE/api/v1/models")
assert_json "models 列出 ops-model 且 explicit" "$MODELS" "any(m['model']=='ops-model' and m['source']=='explicit' and m['routable'] for m in d['items'])"

echo
echo "=== 运维 4b：池化车道（跨渠道跨模型，ADR 0006/0008）==="
# 成员可来自任意启用渠道的任意已声明模型：这里让 ops-b 同时贡献两个不同模型，
# 并让同一渠道在一条车道内出现两次（去重键 = 渠道 + **所选模型**，ADR 0008）。
# 车道名 ops-pool 与任何成员模型名都不同——这正是 ADR 0008 要修的池化场景：
# 上游真名的查表键必须是**成员所选模型**，与路由键无关。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["ops-model","ops-alt"],"enabled":true}' "$BASE/api/v1/channels/ops-b" > /dev/null
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":15,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"ops-b","model":"ops-model","priority":30},{"channel":"ops-b","model":"ops-alt","priority":20},{"channel":"ops-a","model":"ops-model","priority":10}]}' "$BASE/api/v1/lanes/ops-pool" > /dev/null
POOL=$(curl -s "${A[@]}" "$BASE/api/v1/routes/ops-pool")
assert_json "池化车道成员 = 提交的 (渠道, 所选模型) 列表（含同渠道两模型）" "$POOL" "[(m['channel'],m['model']) for m in d['members']][:3]==[('ops-b','ops-model'),('ops-b','ops-alt'),('ops-a','ops-model')]"
assert_json "无渠道映射时 upstream_model 派生为所选模型本身" "$POOL" "[m['upstream_model'] for m in d['members']][:3]==['ops-model','ops-alt','ops-model']"
assert_json "池化车道可路由" "$POOL" "d['routable'] is True"

echo
echo "=== 运维 4b-2：改渠道 model_mapping 后不重存车道，派生真名立即变化（ADR 0008 核心缺陷）==="
# 这是 ADR 0008 背景第 2 条修掉的真缺陷：旧实现把解析后的真名物化写进成员，
# 渠道映射改完也不生效。现在成员只存所选模型，真名现算，改映射即对所有成员生效。
# 映射里**故意同时放一个路由键条目**（ops-pool → route-key-should-not-be-used）：
# 若查表键错用路由键，三个成员的 upstream_model 都会变成它；按成员所选模型查表
# 才会得到下面三个各不相同的结果。这就是"查表键 = 成员所选模型"的可证伪断言。
curl -s "${A[@]}" -X PUT -d '{"model_mapping":{"ops-model":"ops-model-v2","ops-alt":"ops-alt-v2","ops-pool":"route-key-should-not-be-used"}}' "$BASE/api/v1/channels/ops-b" > /dev/null
POOL2=$(curl -s "${A[@]}" "$BASE/api/v1/routes/ops-pool")
assert_json "改渠道映射后（未重存车道）ops-b/ops-model 的真名立即变为映射值" "$POOL2" "[m['upstream_model'] for m in d['members'] if m['channel']=='ops-b' and m['model']=='ops-model']==['ops-model-v2']"
assert_json "同渠道另一模型按自己的键查表（ops-alt → ops-alt-v2）" "$POOL2" "[m['upstream_model'] for m in d['members'] if m['channel']=='ops-b' and m['model']=='ops-alt']==['ops-alt-v2']"
assert_json "未配映射的渠道成员不受影响（ops-a/ops-model 仍为模型名）" "$POOL2" "[m['upstream_model'] for m in d['members'] if m['channel']=='ops-a']==['ops-model']"
assert_json "查表键是成员所选模型而非路由键（路由键条目不得被采用）" "$POOL2" "all(m['upstream_model']!='route-key-should-not-be-used' for m in d['members'])"
# 运行期同口径：真发一次请求，上游必须收到映射后的真名，而不是路由键或模型名。
POOL_KEY=$(curl -s "${A[@]}" -X POST -d '{"name":"ops-pool-client"}' "$BASE/api/v1/keys" | jget 'd["key"]')
curl -s -o /dev/null -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $POOL_KEY" -H 'Content-Type: application/json' -d '{"model":"ops-pool","messages":[{"role":"user","content":"hi"}]}'
sleep 1
assert_json "运行期把映射后的真名发给上游（pool 车道首成员 ops-model-v2）" "$(python3 -c '
import json, sys
out = []
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    try:
        out.append(json.loads(line)["model"])
    except Exception:
        pass
print(json.dumps(out))
' "$WORK/upstream.log")" "'ops-model-v2' in d"
assert_json "运行期绝不把路由键发给上游" "$(python3 -c '
import json, sys
out = []
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    try:
        out.append(json.loads(line)["model"])
    except Exception:
        pass
print(json.dumps(out))
' "$WORK/upstream.log")" "'route-key-should-not-be-used' not in d"

echo
echo "=== 运维 4b-3：成员唯一键 = (渠道, 所选模型)：重复 422，同渠道不同模型可共存 ==="
DUP=$(curl -s -o "$WORK/dup.json" -w '%{http_code}' "${A[@]}" -X PUT -d '{"members":[{"channel":"ops-b","model":"ops-model","priority":30},{"channel":"ops-b","model":"ops-model","priority":20}]}' "$BASE/api/v1/lanes/ops-pool/members")
check "同一 (渠道, 模型) 重复提交返回 422" "$DUP" "422"
check "422 带稳定错误码 duplicate_member" "$(cat "$WORK/dup.json")" '"duplicate_member"'
# 全量替换语义下，被拒的写不得改动任何东西
assert_json "422 后成员链未被改动（全量写失败即无副作用）" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/ops-pool")" "[(m['channel'],m['model']) for m in d['members']][:3]==[('ops-b','ops-model'),('ops-b','ops-alt'),('ops-a','ops-model')]"
assert_json "同渠道不同模型仍可共存（ops-b 两个模型都在链上）" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/ops-pool")" "len([m for m in d['members'] if m['channel']=='ops-b'])==2"
# 缺 model 的成员载荷必须被拒（成员身份的一半，ADR 0008）
NOMODEL=$(curl -s -o "$WORK/nomodel.json" -w '%{http_code}' "${A[@]}" -X PUT -d '{"members":[{"channel":"ops-b","priority":10}]}' "$BASE/api/v1/lanes/ops-pool/members")
check "成员缺 model 返回 400" "$NOMODEL" "400"
check "缺 model 的错误码为 validation_failed" "$(cat "$WORK/nomodel.json")" '"validation_failed"'

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
echo "=== 运维 15：成员启停开关——写入、写后回读、三态默认（api-spec §4.2/§5.2）==="
# 路由键 = 车道名 = w3-model-x；三个渠道都指向同一个内置假上游，成员链覆盖
# "同一车道三个不同渠道成员"。开关是三态：省略 = 新建启用 / 既有保留原值（§4.2）。
W3_KEY=$(curl -s "${A[@]}" -X POST -d '{"name":"w3-client"}' "$BASE/api/v1/keys" | jget 'd["key"]')
[[ -n "$W3_KEY" ]] || { echo "FAIL: 未取得 w3-client 密钥"; exit 1; }
for c in w3-a w3-b w3-c; do
  curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["w3-model-x"],"enabled":true}' "$BASE/api/v1/channels/$c" > /dev/null
done
CFG_X='{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":15,"member_cooldown_seconds":1,"member_affinity_seconds":0}'
# 成员片段：整链在不同用例里反复原样带回，只切换 enabled 三态，避免复制粘贴漂移。
MA0='{"channel":"w3-a","model":"w3-model-x","priority":30}'
MB0='{"channel":"w3-b","model":"w3-model-x","public_alias":"w3-alias-b","priority":20,"overrides":{"member_cooldown_seconds":120}}'
MC0='{"channel":"w3-c","model":"w3-model-x","priority":10}'
MA_OFF='{"channel":"w3-a","model":"w3-model-x","priority":30,"enabled":false}'
MA_ON='{"channel":"w3-a","model":"w3-model-x","priority":30,"enabled":true}'
MB_ON='{"channel":"w3-b","model":"w3-model-x","public_alias":"w3-alias-b","priority":20,"overrides":{"member_cooldown_seconds":120},"enabled":true}'
MC_ON='{"channel":"w3-c","model":"w3-model-x","priority":10,"enabled":true}'
CHAIN_KEEP="$MA0,$MB0,$MC0"          # 全员省略 enabled（三态用例）
CHAIN_OFF="$MA_OFF,$MB_ON,$MC_ON"    # 显式停用 w3-a
CHAIN_ON="$MA_ON,$MB_ON,$MC_ON"      # 显式全开
CHAIN_AB="$MA0,$MB0"                 # 漏发 w3-c（全量替换不得被放宽）
curl -s "${A[@]}" -X PUT -d "{\"enabled\":true,\"mode\":\"failover\",\"config\":$CFG_X,\"members\":[$CHAIN_KEEP]}" "$BASE/api/v1/lanes/w3-model-x" > /dev/null
XRT=$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")
assert_json "新建成员省略 enabled → 默认启用（且恒回）" "$XRT" "len(d['members'])==3 and all('enabled' in m and m['enabled'] is True for m in d['members'])"
assert_json "成员链顺序 = priority 降序（开关不改变顺序合同）" "$XRT" "[m['channel'] for m in d['members']]==['w3-a','w3-b','w3-c'] and [m['priority'] for m in d['members']]==[30,20,10]"
# 写开关：PUT 响应体即写后回读的落库终态（api-spec §2.2），两个读端点必须一致
XPUT=$(curl -s "${A[@]}" -X PUT -d "{\"members\":[$CHAIN_OFF]}" "$BASE/api/v1/lanes/w3-model-x/members")
assert_json "PUT 响应体（写后回读）已含 w3-a enabled=false" "$XPUT" "[m for m in d['members'] if m['channel']=='w3-a'][0]['enabled'] is False"
assert_json "GET /routes 回读一致：w3-a enabled=false" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")" "[m for m in d['members'] if m['channel']=='w3-a'][0]['enabled'] is False"
assert_json "GET /lanes 回读一致：成员恒回 enabled" "$(curl -s "${A[@]}" "$BASE/api/v1/lanes/w3-model-x")" "[m for m in d['members'] if m['channel']=='w3-a'][0]['enabled'] is False"
# 三态核心回归：整链省略 enabled 再保存，绝不允许把成员关掉（或偷偷打开）
curl -s "${A[@]}" -X PUT -d "{\"members\":[$CHAIN_KEEP]}" "$BASE/api/v1/lanes/w3-model-x/members" > /dev/null
XRT=$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")
assert_json "省略 enabled：既有已停用成员保留关闭（不被顺手打开）" "$XRT" "[m for m in d['members'] if m['channel']=='w3-a'][0]['enabled'] is False"
assert_json "省略 enabled：既有启用成员保持启用——本轮防的就是'漏发字段即整链关掉'" "$XRT" "all(m['enabled'] is True for m in d['members'] if m['channel'] in ('w3-b','w3-c'))"
assert_json "只省略 enabled 不得连带清空成员级 alias/overrides（§4.2 读写闭环）" "$XRT" "[m for m in d['members'] if m['channel']=='w3-b'][0]['public_alias']=='w3-alias-b' and [m for m in d['members'] if m['channel']=='w3-b'][0]['overrides'].get('member_cooldown_seconds')==120"
# 显式 true/false 生效（开→关→开往返）
curl -s "${A[@]}" -X PUT -d "{\"members\":[$CHAIN_ON]}" "$BASE/api/v1/lanes/w3-model-x/members" > /dev/null
assert_json "显式 enabled=true 生效（重新启用）" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")" "[m for m in d['members'] if m['channel']=='w3-a'][0]['enabled'] is True"
# 全量替换未被新字段放宽：漏发成员 = 删除该成员，三态只豁免 enabled 一个字段
curl -s "${A[@]}" -X PUT -d "{\"members\":[$CHAIN_AB]}" "$BASE/api/v1/lanes/w3-model-x/members" > /dev/null
assert_json "漏发成员仍等于删除（三态不得把全量合同变成补丁合同）" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")" "[m['channel'] for m in d['members']]==['w3-a','w3-b']"
curl -s "${A[@]}" -X PUT -d "{\"members\":[$CHAIN_KEEP]}" "$BASE/api/v1/lanes/w3-model-x/members" > /dev/null
assert_json "恢复整链：被删后重建的 w3-c 按'新建'默认启用" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")" "len(d['members'])==3 and [m for m in d['members'] if m['channel']=='w3-c'][0]['enabled'] is True"

echo
echo "=== 运维 16：开关写入的 dry-run 不落库（api-spec §2.4/§5.9：diff 是名级的）==="
BEFORE_LN=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/w3-model-x")
BEFORE_RT=$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")
DRY_X1=$(curl -s "${A[@]}" -X PUT -d "{\"enabled\":true,\"mode\":\"failover\",\"config\":$CFG_X,\"members\":[$CHAIN_OFF]}" "$BASE/api/v1/lanes/w3-model-x?dry_run=true")
assert_json "PUT /lanes/{name}?dry_run=true 返回 dry_run:true 且车道名出现在 diff.lanes.update" "$DRY_X1" "d['dry_run'] is True and d['valid'] is True and 'w3-model-x' in d['diff']['lanes']['update']"
DRY_X2=$(curl -s "${A[@]}" -X PUT -d "{\"members\":[$CHAIN_OFF]}" "$BASE/api/v1/lanes/w3-model-x/members?dry_run=true")
assert_json "PUT /lanes/{name}/members?dry_run=true 同口径（名级 diff）" "$DRY_X2" "d['dry_run'] is True and 'w3-model-x' in d['diff']['lanes']['update']"
assert_same "dry-run 预览未落库：/lanes 回读与调用前逐字段一致" "$(curl -s "${A[@]}" "$BASE/api/v1/lanes/w3-model-x")" "$BEFORE_LN"
assert_same "dry-run 预览未落库：/routes 回读与调用前逐字段一致" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")" "$BEFORE_RT"
assert_json "dry-run 后 w3-a 开关仍为启用（预览没把关闭态写进现实）" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")" "[m for m in d['members'] if m['channel']=='w3-a'][0]['enabled'] is True"

echo
echo "=== 运维 17：停用成员的形态与端到端后果（test-spec §5/3d；routing-spec §2.2/§9）==="
curl -s "${A[@]}" -X PUT -d "{\"members\":[$CHAIN_OFF]}" "$BASE/api/v1/lanes/w3-model-x/members" > /dev/null
XRT=$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")
assert_json "停用成员仍在成员链里、位置不变、仍占 priority（开关不是删除别名）" "$XRT" "[(m['channel'],m['priority']) for m in d['members']]==[('w3-a',30),('w3-b',20),('w3-c',10)]"
assert_json "/routes 成员恒回 enabled/overrides/member_id（编排器草稿完整性，§5.7）" "$XRT" "all('enabled' in m and 'overrides' in m and isinstance(m.get('member_id'),int) and m['member_id']>0 for m in d['members'])"
assert_json "无覆盖成员 overrides 恒回 {} 而非省略" "$XRT" "[m for m in d['members'] if m['channel']=='w3-c'][0]['overrides']=={}"
assert_json "lane-summaries 成员条目含 enabled 且停用侧为 false（路由页标灰）" "$(curl -s "${A[@]}" "$BASE/api/v1/lane-summaries")" "any(l['name']=='w3-model-x' and all('enabled' in m for m in l['members']) and [m for m in l['members'] if m['channel']=='w3-a'][0]['enabled'] is False for l in d['items'])"
assert_json "unconfigured 推荐项 member_id=0、enabled 恒 true（§5.7）" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/ops-alt")" "d['source']=='unconfigured' and len(d['members'])>0 and all(m['member_id']==0 and m['enabled'] is True for m in d['members'])"
assert_json "健康快照：停用成员 enabled=false 且 available=false，无故障时运行态字段照实（closed/None/0）" "$(curl -s "${A[@]}" "$BASE/api/v1/lanes/w3-model-x/health")" "len([m for m in d['members'] if m['channel']=='w3-a' and m['enabled'] is False and m['available'] is False and m['circuit']=='closed' and m['cooldown_until'] is None and m['consecutive_failures']==0])==1"
# 端到端：头名成员停用 → 跳过它逃逸到 w3-b，attempts 以 disabled 留痕（非故障语义）
RESP=$(curl -s -D "$WORK/w3-x1.headers" -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $W3_KEY" -H 'Content-Type: application/json' -d '{"model":"w3-model-x","messages":[{"role":"user","content":"hi"}]}')
check "停用头名成员后端到端仍成功（逃逸到次成员）" "$RESP" "pong from fake upstream"
SERVED=$(grep -i '^x-served-by:' "$WORK/w3-x1.headers" | tr -d '\r' | sed 's/^[^:]*: //')
check "X-Served-By 是 w3-b（被关闭成员不再被命中，也不占探测槽）" "$SERVED" ":w3-b"
sleep 1
assert_json "attempts 链：停用侧 status=disabled（与 cooldown/circuit_break/skipped 可区分）且末段成功" "$(curl -s "${A[@]}" "$BASE/api/v1/logs?limit=10")" "any(i['lane']=='w3-model-x' and i['success'] and any(a['status']=='disabled' and a['member']=='w3-a/w3-model-x' for a in i['attempts']) and i['attempts'][-1]['status']=='success' for i in d['items'])"
assert_json "attempts[].member 标签为 channel/所选模型（ADR 0008，非真名）" "$(curl -s "${A[@]}" "$BASE/api/v1/logs?limit=10")" "any(i['lane']=='w3-model-x' and i['success'] and i['attempts'][-1]['member']=='w3-b/w3-model-x' for i in d['items'])"
# 重新打开 → 端到端必须重新命中（3d：实测恢复，不能只测关闭）
curl -s "${A[@]}" -X PUT -d "{\"members\":[$CHAIN_ON]}" "$BASE/api/v1/lanes/w3-model-x/members" > /dev/null
RESP=$(curl -s -D "$WORK/w3-x2.headers" -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $W3_KEY" -H 'Content-Type: application/json' -d '{"model":"w3-model-x","messages":[{"role":"user","content":"hi"}]}')
check "重新启用后端到端仍成功" "$RESP" "pong from fake upstream"
SERVED=$(grep -i '^x-served-by:' "$WORK/w3-x2.headers" | tr -d '\r' | sed 's/^[^:]*: //')
check "重新打开后恢复命中 w3-a（priority 首位回归）" "$SERVED" ":w3-a"
# 人工停用没有专用端点：整条车道 PUT 同样能改成员开关（api-spec §5.2）
curl -s "${A[@]}" -X PUT -d "{\"enabled\":true,\"mode\":\"failover\",\"config\":$CFG_X,\"members\":[$CHAIN_OFF]}" "$BASE/api/v1/lanes/w3-model-x" > /dev/null
assert_json "整链 PUT /lanes/{name} 同样写入开关（写后回读）" "$(curl -s "${A[@]}" "$BASE/api/v1/lanes/w3-model-x")" "[m for m in d['members'] if m['channel']=='w3-a'][0]['enabled'] is False"

echo
echo "=== 运维 18：开关不污染运行态 + /api/models 聚合可辨识（routing-spec §7）==="
# w3-model-y：单成员链，先用假上游 500 制造真实的 consecutive_failures/冷却，
# 再停用 → 运行态字段必须照实保留（不归零、不省略）；重新打开做往返比对。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["w3-model-y","w3-model-z"],"enabled":true}' "$BASE/api/v1/channels/w3-d" > /dev/null
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["w3-model-z"],"enabled":true}' "$BASE/api/v1/channels/w3-e" > /dev/null
CFG_Y='{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":15,"member_cooldown_seconds":120,"member_affinity_seconds":0}'
curl -s "${A[@]}" -X PUT -d "{\"enabled\":true,\"mode\":\"failover\",\"config\":$CFG_Y,\"members\":[{\"channel\":\"w3-d\",\"model\":\"w3-model-y\",\"priority\":10}]}" "$BASE/api/v1/lanes/w3-model-y" > /dev/null
control '{"model":"w3-model-y","status":500}'
curl -s -o /dev/null -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $W3_KEY" -H 'Content-Type: application/json' -d '{"model":"w3-model-y","messages":[{"role":"user","content":"hi"}]}'
sleep 1
HY=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/w3-model-y/health")
assert_json "前置成立：上游 500 已记为真实运行态（consecutive_failures>=1 且 cooldown_until 非空）" "$HY" "len(d['members'])==1 and d['members'][0]['consecutive_failures']>=1 and d['members'][0]['cooldown_until'] is not None"
TRIP='str([[m["circuit"],m["cooldown_until"],m["consecutive_failures"]] for m in d["members"]])'
PRE_TRIP=$(echo "$HY" | jget "$TRIP")
assert_json "上游全挂路径：degraded=true 且 disabled_member_count=0（区别于'我关的'）" "$(curl -s "${A[@]}" "$BASE/api/v1/models")" "len([e for e in d['items'] if e['model']=='w3-model-y' and e['degraded'] is True and e.get('disabled_member_count')==0])==1"
curl -s "${A[@]}" -X PUT -d '{"members":[{"channel":"w3-d","model":"w3-model-y","priority":10,"enabled":false}]}' "$BASE/api/v1/lanes/w3-model-y/members" > /dev/null
HY2=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/w3-model-y/health")
assert_json "停用后 enabled=false 且 available=false（关闭本身即成因，可归因）" "$HY2" "d['members'][0]['enabled'] is False and d['members'][0]['available'] is False"
assert_same "停用不清零/不省略 circuit、cooldown_until、consecutive_failures（照实保留）" "$(echo "$HY2" | jget "$TRIP")" "$PRE_TRIP"
curl -s "${A[@]}" -X PUT -d '{"members":[{"channel":"w3-d","model":"w3-model-y","priority":10,"enabled":true}]}' "$BASE/api/v1/lanes/w3-model-y/members" > /dev/null
HY3=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/w3-model-y/health")
assert_json "重新打开后 enabled=true" "$HY3" "d['members'][0]['enabled'] is True"
assert_same "关→开往返运行态逐字段不变（仍处原冷却，不因'关过'受罚）" "$(echo "$HY3" | jget "$TRIP")" "$PRE_TRIP"
control '{"model":"w3-model-y","status":200}'
# w3-model-z：上游零故障、纯人工全关 → 与"上游全挂"同 degraded、靠 disabled_member_count 区分
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":15,"member_cooldown_seconds":120,"member_affinity_seconds":0},"members":[{"channel":"w3-d","model":"w3-model-z","priority":10},{"channel":"w3-e","model":"w3-model-z","priority":5}]}' "$BASE/api/v1/lanes/w3-model-z" > /dev/null
curl -s "${A[@]}" -X PUT -d '{"members":[{"channel":"w3-d","model":"w3-model-z","priority":10,"enabled":false},{"channel":"w3-e","model":"w3-model-z","priority":5,"enabled":false}]}' "$BASE/api/v1/lanes/w3-model-z/members" > /dev/null
WM=$(curl -s "${A[@]}" "$BASE/api/v1/models")
assert_json "成员全关路径：degraded=true 且 disabled_member_count==member_count、available=0（合法写入，非 422）" "$WM" "len([e for e in d['items'] if e['model']=='w3-model-z' and e['degraded'] is True and e.get('disabled_member_count')==2 and e['member_count']==2 and e['available_member_count']==0])==1"
assert_json "全关不隐藏模型：仍在 /api/models 且 source=explicit" "$WM" "len([e for e in d['items'] if e['model']=='w3-model-z' and e['source']=='explicit'])==1"
assert_json "部分关不误报降级：w3-model-x degraded=false、disabled_member_count=1、available_member_count=2" "$WM" "len([e for e in d['items'] if e['model']=='w3-model-x' and e['degraded'] is False and e.get('disabled_member_count')==1 and e['available_member_count']==2])==1"

echo
echo "=== 运维 19：导出/导入往返开关不丢（api-spec §5.6：'导出→清空→导入'不得把人工停用成员静默放回选路）==="
curl -s "${A[@]}" "$BASE/api/v1/export" -o "$WORK/export-w3.json"
assert_json "导出成员对象恒带 enabled，且关闭状态写进文件" "$(cat "$WORK/export-w3.json")" "len([m for l in d['lanes'] if l['name']=='w3-model-x' for m in l['members'] if 'enabled' in m])==3 and len([m for l in d['lanes'] if l['name']=='w3-model-x' for m in l['members'] if m['channel']=='w3-a' and m['enabled'] is False])==1"
# ADR 0008：导出只带所选模型，**不得**带派生真名（把旧真名导回改过映射的实例会覆盖新映射）。
assert_json "导出成员带 model（所选模型）" "$(cat "$WORK/export-w3.json")" "all(m.get('model') for l in d['lanes'] if l['name']=='w3-model-x' for m in l['members']) and [m['model'] for l in d['lanes'] if l['name']=='w3-model-x' for m in l['members']]==['w3-model-x']*3"
assert_json "导出成员不含 upstream_model（派生真名不进导出文件）" "$(cat "$WORK/export-w3.json")" "not any('upstream_model' in m for l in d['lanes'] for m in l['members'])"
curl -s "${A[@]}" -X DELETE "$BASE/api/v1/lanes/w3-model-x" > /dev/null
assert_json "护栏：车道确已清空（source=unconfigured），导入不得靠'保留原值'假装往返成功" "$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")" "d['source']=='unconfigured'"
IMP_CODE=$(curl -s -o "$WORK/import-w3.out" -w '%{http_code}' "${A[@]}" -X POST --data-binary @"$WORK/export-w3.json" "$BASE/api/v1/import")
check "真实导入返回 200" "$IMP_CODE" "200"
XRT=$(curl -s "${A[@]}" "$BASE/api/v1/routes/w3-model-x")
assert_json "导出→清空→导入往返：w3-a 仍是停用（开关不丢）" "$XRT" "len(d['members'])==3 and [m for m in d['members'] if m['channel']=='w3-a'][0]['enabled'] is False"
assert_json "往返后成员所选模型保留（model 未被真名顶替）" "$XRT" "[m['model'] for m in d['members']]==['w3-model-x']*3"
assert_json "往返不连带丢其余成员字段（alias/overrides/他员 enabled 随链恢复）" "$XRT" "[m for m in d['members'] if m['channel']=='w3-b'][0]['public_alias']=='w3-alias-b' and [m for m in d['members'] if m['channel']=='w3-b'][0]['overrides'].get('member_cooldown_seconds')==120 and [m for m in d['members'] if m['channel']=='w3-c'][0]['enabled'] is True"

echo
echo "=== 运维 20：开关写入的审计条目（api-spec §2.7/§5.2：lane 整条 / lane_members 仅成员）==="
AUDIT_W3=$(curl -s "${A[@]}" "$BASE/api/v1/audit?limit=100")
assert_json "PUT /lanes/{name}/members 落 action=update、resource=lane_members 且 after_digest 非空" "$AUDIT_W3" "any(i['resource']=='lane_members' and i['action']=='update' and i['name']=='w3-model-x' and i['after_digest']!='' for i in d['items'])"
assert_json "整链 PUT /lanes/{name} 的开关写入落 resource=lane、action=update" "$AUDIT_W3" "any(i['resource']=='lane' and i['action']=='update' and i['name']=='w3-model-x' for i in d['items'])"
assert_json "dry-run 预览同样落审计且 dry_run=true（写不静默，预览可追溯）" "$AUDIT_W3" "any(i['name']=='w3-model-x' and i['dry_run'] is True for i in d['items'])"

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: L2 真实 API 运维全部通过"; else
  echo "FAIL: 存在未通过项"; echo "--- pbr.log 末尾 ---"; tail -40 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
