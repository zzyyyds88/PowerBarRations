#!/usr/bin/env bash
# W1 验收：单层化路由（模型名键控）。
#
# 验收门（docs/goal-prompt.md §四 W1）：
#   1) 两渠道声明同一模型名 → 走 P1；
#   2) 改坏 P1 的 key → 落 P2，且响应 model 不变；
#   3) 都不声明 → 503 No available channel for model <X>。
# 附加（design-v1 §4.1 / §3.3）：成员改名 + 响应 model 回填 + X-Served-By。
#
# 全程本地：独立端口、独立 SQLite、内置假上游；不触碰任何现网容器与凭据。
set -uo pipefail

REPO=/root/powerbar-rations
WORK=/tmp/pbr-w1
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/w1/run-$STAMP.log"
PORT=6792
UPSTREAM_PORT=6803
GOOD_KEY='sk-w1-good'
BAD_KEY='sk-w1-bad'
PBR_PW='Pbr-W1-Verify-Passw0rd!2026'
LEGACY_PW='Pbr-W1-Legacy-Passw0rd!2026'

exec > "$EVID" 2>&1
echo "=== W1 单层化路由验收 @ $STAMP ==="

PASS=0
FAIL=0
check() { # check <描述> <实际> <期望子串>
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then
    echo "  PASS: $desc"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $desc"
    echo "        期望包含: $want"
    echo "        实际     : $actual"
    FAIL=$((FAIL + 1))
  fi
}
check_not() { # check_not <描述> <实际> <不应包含>
  local desc="$1" actual="$2" unwanted="$3"
  if [[ "$actual" != *"$unwanted"* ]]; then
    echo "  PASS: $desc"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $desc（不应包含 $unwanted）"
    echo "        实际: $actual"
    FAIL=$((FAIL + 1))
  fi
}

cleanup() {
  pkill -f "$WORK/pbr" 2>/dev/null
  pkill -f "$WORK/fakeupstream" 2>/dev/null
  wait 2>/dev/null
}
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"

for p in "$PORT" "$UPSTREAM_PORT"; do
  if ss -ltn 2>/dev/null | grep -q ":$p "; then
    echo "FAIL: 端口 $p 已被占用，先清理残留进程"; exit 1
  fi
done

echo "--- 0) 构建二进制"
( cd "$REPO" && go build -o "$WORK/pbr" . \
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) \
  || { echo "FAIL: build"; exit 1; }
ls -lh "$WORK/pbr" "$WORK/fakeupstream"

echo "--- 1) 启动内置假上游 :$UPSTREAM_PORT（要求 key=$GOOD_KEY）"
"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.stdout" 2>&1 &

echo "--- 2) 启动 pbr :$PORT（独立库 $WORK/pbr.db）"
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=w1-session CRYPTO_SECRET=w1-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

echo "--- 3) 等待就绪"
ok=0
for i in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:$PORT/api/v1/health" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
[[ $ok == 1 ]] || { echo "FAIL: 服务未就绪"; tail -40 "$WORK/pbr.log"; exit 1; }
echo "ready after ${i}s"

H='-H Content-Type:application/json'
BASE="http://127.0.0.1:$PORT"
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }

echo
echo "--- 4) 首启设口令（POST /api/v1/setup），取得派生管理密钥"
SETUP=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup")
echo "$SETUP" | head -c 300; echo
ADMIN_KEY=$(echo "$SETUP" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
echo "admin_key: 已取得（${#ADMIN_KEY} 字节，不打印）"

A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')

echo "--- 5) 未带凭据访问管理面应 401；未初始化前应 409（此处已初始化，验 401）"
NOAUTH=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/lanes")
check "无凭据访问 /api/v1/lanes 返回 401" "$NOAUTH" "401"
BADKEY=$(curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer wrong-key' "$BASE/api/v1/lanes")
check "错误管理密钥返回 401" "$BADKEY" "401"

echo
echo "--- 6) 建两个渠道，都声明同一模型名 wire-model（P1=channel-a priority 20, P2=channel-b priority 10）"
curl -s "${A[@]}" -X PUT -d '{
  "type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'",
  "priority":20,"models":["wire-model"],"enabled":true,"param_override":{}
}' "$BASE/api/v1/channels/channel-a" | head -c 400; echo
curl -s "${A[@]}" -X PUT -d '{
  "type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'",
  "priority":10,"models":["wire-model"],"enabled":true,"param_override":{}
}' "$BASE/api/v1/channels/channel-b" | head -c 400; echo

echo "--- 6.1) 写后回读：GET /channels/channel-a 不应出现 key 明文"
CH_A=$(curl -s "${A[@]}" "$BASE/api/v1/channels/channel-a")
echo "$CH_A"
check_not "读渠道不回显密钥明文" "$CH_A" "$GOOD_KEY"
check "读渠道有 key_set=true" "$CH_A" '"key_set":true'

echo
echo "--- 7) 模型清单与成员链（零配置即可路由）"
MODELS=$(curl -s "${A[@]}" "$BASE/api/v1/models")
echo "$MODELS" | head -c 400; echo
check "GET /models 含 wire-model" "$MODELS" '"model":"wire-model"'
check "wire-model 来源为隐式链" "$MODELS" '"source":"implicit"'

ROUTE=$(curl -s "${A[@]}" "$BASE/api/v1/routes/wire-model")
echo "$ROUTE"
# 用解析后的顺序断言，避免依赖 JSON 字段排序。
ORDER=$(echo "$ROUTE" | python3 -c "import sys,json;ms=json.load(sys.stdin)['members'];print(','.join(m['channel']+'@'+str(m['priority']) for m in ms))")
SHAPE=$(echo "$ROUTE" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d["model"],d["source"],len(d["members"]))')
check "成员链按 priority 降序（P1 channel-a → P2 channel-b）" "$ORDER" 'channel-a@20,channel-b@10'
check "路由来源为隐式、成员数 2" "$SHAPE" 'wire-model implicit 2'

echo
echo
echo "--- 8) 模型面鉴权：PBR 客户端密钥（W7 后基座用户/令牌链路已删除）"
CLIENT_KEY=$(curl -s "${A[@]}" -X POST -d '{"name":"w1-key"}' "$BASE/api/v1/keys" | jget 'd["key"]' 2>/dev/null)
[[ -n "$CLIENT_KEY" ]] || { echo "FAIL: 未取得客户端密钥"; exit 1; }
echo "客户端密钥: 已取得"

chat() { # chat <model>
  curl -s -D "$WORK/headers.txt" -X POST "$BASE/v1/chat/completions" \
    -H "Authorization: Bearer $CLIENT_KEY" -H 'Content-Type: application/json' \
    -d '{"model":"'"$1"'","messages":[{"role":"user","content":"ping"}]}'
}

echo
echo "--- 9) 验收门 1：两渠道声明同一模型名 → 走 P1"
grep -c . "$WORK/upstream.log" > /dev/null 2>&1
RESP1=$(chat wire-model)
echo "$RESP1" | head -c 400; echo
check "请求成功" "$RESP1" 'pong from fake upstream'
check "响应 model 为请求名" "$RESP1" '"model":"wire-model"'
SERVED1=$(grep -i '^x-served-by:' "$WORK/headers.txt" | tr -d '\r')
echo "  X-Served-By: ${SERVED1:-<无>}"
check "X-Served-By 指向 P1（channel-a）" "$SERVED1" 'channel=1:channel-a'

echo
echo "--- 10) 验收门 2：改坏 P1 的 key → 落 P2，响应 model 不变"
curl -s "${A[@]}" -X PUT -d '{
  "type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$BAD_KEY"'",
  "priority":20,"models":["wire-model"],"enabled":true
}' "$BASE/api/v1/channels/channel-a" > /dev/null
: > "$WORK/upstream.log"
RESP2=$(chat wire-model)
echo "$RESP2" | head -c 400; echo
check "P1 失败后由 P2 服务成功" "$RESP2" 'pong from fake upstream'
check "响应 model 仍为请求名" "$RESP2" '"model":"wire-model"'
SERVED2=$(grep -i '^x-served-by:' "$WORK/headers.txt" | tr -d '\r')
echo "  X-Served-By: ${SERVED2:-<无>}"
check "X-Served-By 指向 P2（channel-b）" "$SERVED2" 'channel=2:channel-b'
echo "  假上游收到的尝试："
sed -n '1,5p' "$WORK/upstream.log"

echo
echo "--- 11) 验收门 3：没有渠道声明该模型 → 503 固定 body"
GHOST=$(curl -s -o "$WORK/ghost.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_KEY" -H 'Content-Type: application/json' \
  -d '{"model":"ghost-model","messages":[{"role":"user","content":"ping"}]}')
GHOST_BODY=$(cat "$WORK/ghost.json")
echo "  HTTP $GHOST  $GHOST_BODY"
check "返回 503" "$GHOST" "503"
check "body 文案固定" "$GHOST_BODY" '{"error":{"message":"No available channel for model ghost-model"}}'

echo
echo "--- 12) 附加：显式车道成员改名 + 响应 model 回填（design-v1 §3.3/§4.1）"
curl -s "${A[@]}" -X PUT -d '{
  "enabled":true,"mode":"failover",
  "members":[{"channel":"channel-b","upstream_model":"vendor-real-name","priority":1,"weight":1}]
}' "$BASE/api/v1/lanes/alias-model" | head -c 400; echo
: > "$WORK/upstream.log"
RESP3=$(chat alias-model)
echo "$RESP3" | head -c 400; echo
check "显式车道请求成功" "$RESP3" 'pong from fake upstream'
check "响应 model 回填为请求名" "$RESP3" '"model":"alias-model"'
check_not "响应 model 不是上游真名" "$RESP3" '"model":"vendor-real-name"'
echo "  假上游收到："
sed -n '1,3p' "$WORK/upstream.log"
check "上游收到的是成员真名" "$(grep -a -o '"model":"[^"]*"' "$WORK/upstream.log" | head -1)" '"model":"vendor-real-name"'
SERVED3=$(grep -i '^x-served-by:' "$WORK/headers.txt" | tr -d '\r')
check "X-Served-By 暴露上游真名" "$SERVED3" 'model=vendor-real-name'

echo
echo "--- 13) 清理私有数据自查：源码中不得出现本次测试密钥"
# verify/ 下的测试脚本与证据本就含占位密钥，不构成部署私有数据；检查范围排除它。
LEAK=$(grep -rn "sk-w1-good\|sk-w1-bad" "$REPO" --exclude-dir=.git --exclude-dir=verify 2>/dev/null | head -5)
if [[ -z "$LEAK" ]]; then
  echo "  PASS: 仓库源码中无测试密钥残留"
  PASS=$((PASS + 1))
else
  echo "  FAIL: 仓库中出现测试密钥："; echo "$LEAK"
  FAIL=$((FAIL + 1))
fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then
  echo "PASS: W1 验收门全部通过"
else
  echo "FAIL: 存在未通过项"
  echo "--- pbr.log 末尾 ---"; tail -40 "$WORK/pbr.log"
  exit 2
fi
echo "=== evidence: $EVID ==="
