#!/usr/bin/env bash
# W0 验收：以 new-api 源码为基座迁入后，能原样转发一发真实请求。
# 全程本地：独立端口 6790、独立 SQLite、假上游 6801；不触碰任何现网容器与凭据。
set -uo pipefail

REPO=/root/powerbar-rations
WORK=/tmp/pbr-w0
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/w0/run-$STAMP.log"
PORT=6790
UPSTREAM_PORT=6801

exec > "$EVID" 2>&1
echo "=== W0 smoke @ $STAMP ==="
PW='Pbr-W0-Passw0rd!2026'

cleanup() {
  pkill -f "$WORK/pbr" 2>/dev/null
  pkill -f "fake_upstream.py" 2>/dev/null
  wait 2>/dev/null
}
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"

# 端口占用预检，避免孤儿进程让本次测试打到旧实例上
if ss -ltn 2>/dev/null | grep -q ":$PORT "; then
  echo "FAIL: 端口 $PORT 已被占用，先清理残留进程"; exit 1
fi
if ss -ltn 2>/dev/null | grep -q ":$UPSTREAM_PORT "; then
  echo "FAIL: 端口 $UPSTREAM_PORT 已被占用，先清理残留进程"; exit 1
fi

echo "--- 1) 构建二进制"
( cd "$REPO" && go build -o "$WORK/pbr" . ) || { echo "FAIL: build"; exit 1; }
ls -lh "$WORK/pbr"

echo "--- 2) 启动假上游 :$UPSTREAM_PORT"
python3 "$REPO/verify/w0/fake_upstream.py" "$UPSTREAM_PORT" > "$WORK/upstream.log" 2>&1 &
UP_PID=$!

echo "--- 3) 启动 pbr :$PORT （独立库 $WORK/pbr.db）"
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=w0-session CRYPTO_SECRET=w0-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &
PB_PID=$!

echo "--- 4) 等待就绪"
ok=0
for i in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:$PORT/api/status" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
[[ $ok == 1 ]] || { echo "FAIL: 服务未就绪"; tail -30 "$WORK/pbr.log"; exit 1; }
echo "ready after ${i}s"

COOKIE="$WORK/cookies.txt"
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }

echo "--- 5) 首启设口令并取得管理密钥（PBR）"
ADMIN_KEY=$(curl -s -H 'Content-Type: application/json' \
  -d '{"password":"'"$PW"'"}' "http://127.0.0.1:$PORT/api/v1/setup" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
echo "admin_key: 已取得"

H_ADMIN=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')

echo "--- 6) 写入比率选项（仅本次测试库）"
for kv in \
  '{"key":"ModelRatio","value":"{\"test-model\":1}"}' \
  '{"key":"CompletionRatio","value":"{\"test-model\":1}"}' \
  '{"key":"GroupRatio","value":"{\"default\":1}"}' \
  '{"key":"SelfUseModeEnabled","value":"true"}' ; do
  curl -s "${H_ADMIN[@]}" -X PUT -d "$kv" "http://127.0.0.1:$PORT/api/option/" | head -c 200; echo
done

echo "--- 7) 建渠道（指向假上游，基座 /api/channel 保留面）"
curl -s "${H_ADMIN[@]}" -X POST -d '{
  "mode":"single",
  "channel":{"name":"w0-fake","type":1,"key":"sk-w0-test",
    "base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","models":"test-model",
    "group":"default","status":1,"priority":1,"weight":1,"auto_ban":0}
}' "http://127.0.0.1:$PORT/api/channel/" | head -c 400; echo

echo "--- 8) 建 PBR 客户端密钥"
TOKEN=$(curl -s "${H_ADMIN[@]}" -X POST -d '{"name":"w0-key"}' "http://127.0.0.1:$PORT/api/v1/keys" | jget 'd["key"]')
[[ -n "$TOKEN" ]] || { echo "FAIL: 未取得客户端密钥"; exit 1; }
echo "client key: 已取得"

echo "--- 10) 经网关转发一发 chat 请求"
RESP=$(curl -s -X POST "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"test-model","messages":[{"role":"user","content":"ping"}]}')
echo "$RESP" | head -c 600; echo

echo "--- 11) 假上游收到的请求"
cat "$WORK/upstream.log"

echo "--- 12) 判定"
if echo "$RESP" | grep -q 'pong from fake upstream'; then
  echo "PASS: 网关已把请求转发到假上游并回传响应"
else
  echo "FAIL: 响应不符合预期"
  echo "--- pbr.log 末尾 ---"; tail -40 "$WORK/pbr.log"
  exit 2
fi
echo "=== evidence: $EVID ==="
