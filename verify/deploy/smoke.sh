#!/usr/bin/env bash
# 部署验收（goal-prompt §六 G）：从零用 docker-compose.yml 起容器 → 首启设口令 →
# 建渠道/客户端密钥 → 跑通一发真实请求 → 重启容器数据仍在。
#
# 安全边界：独立 compose 项目名（pbr-verify）与独立命名卷，端口 6796；
# 只绑定 docker 网桥 IP 给容器内的假上游，不占 LAN；绝不触碰现网容器与数据卷。
set -uo pipefail

REPO="${PBR_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORK=/tmp/pbr-deploy
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/deploy/run-$STAMP.log"
PROJECT=pbr-verify
PORT=6796
export PBR_PORT=$PORT
UPSTREAM_PORT=6807
GOOD_KEY='sk-deploy-verify'
PBR_PW='Pbr-Deploy-Verify-Passw0rd!2026'

exec > "$EVID" 2>&1
echo "=== 部署验收 @ $STAMP ==="

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

BRIDGE_IP=$(ip -4 addr show docker0 2>/dev/null | grep -o 'inet [0-9.]*' | awk '{print $2}' | head -1)
[[ -n "$BRIDGE_IP" ]] || { echo "FAIL: 未找到 docker 网桥地址"; exit 1; }
echo "  docker 网桥：$BRIDGE_IP"

cleanup() {
  pkill -f "$WORK/fakeupstream" 2>/dev/null
  ( cd "$REPO" && docker compose -p "$PROJECT" -f docker-compose.yml -f verify/deploy/compose.override.yml down -v ) >/dev/null 2>&1
  wait 2>/dev/null
}
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"
if ss -ltn 2>/dev/null | grep -q ":$PORT "; then echo "FAIL: 端口 $PORT 被占用"; exit 1; fi

echo "--- 1) 先记下现网容器（只观察，不动）"
live_containers() { docker ps --format '{{.Names}}' | grep -v '^pbr$' | sort | tr '\n' ' '; }
BEFORE_PS=$(live_containers)
echo "  $BEFORE_PS"

echo "--- 2) 构建并启动（独立项目 $PROJECT，独立命名卷）"
( cd "$REPO" && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: 构建假上游"; exit 1; }
"$WORK/fakeupstream" -addr "$BRIDGE_IP:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.stdout" 2>&1 &

export PBR_SESSION_SECRET="$(openssl rand -hex 32)"
export PBR_CRYPTO_SECRET="$(openssl rand -hex 32)"
# 构建容器需要能拉到 Go 模块：沿用宿主机的 GOPROXY（本机不可达官方代理）。
export PBR_GOPROXY="$(go env GOPROXY)"
( cd "$REPO" && docker compose -p "$PROJECT" -f docker-compose.yml -f verify/deploy/compose.override.yml up -d --build ) \
  || { echo "FAIL: compose up 失败"; exit 1; }
docker compose -p "$PROJECT" -f "$REPO/docker-compose.yml" -f "$REPO/verify/deploy/compose.override.yml" ps 2>/dev/null | head -5

echo "--- 3) 等待健康检查"
BASE="http://127.0.0.1:$PORT"
ok=0
for i in $(seq 1 90); do
  if curl -sf "$BASE/api/v1/health" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
if [[ $ok != 1 ]]; then
  echo "FAIL: 容器未就绪"; docker logs pbr 2>&1 | tail -40; exit 1
fi
echo "ready after ${i}s"
HEALTH=$(curl -s "$BASE/api/v1/health")
echo "  $HEALTH"
check "健康检查通过" "$HEALTH" '"status":"ok"'

H='-H Content-Type:application/json'
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }

echo "--- 4) 首启设口令"
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
echo "  admin_key: 已取得（${#ADMIN_KEY} 字节，不打印）"

echo "--- 5) 建渠道（指向容器外的假上游）与客户端密钥"
curl -s "${A[@]}" -X PUT -d '{
  "type":"openai","base_url":"http://'"$BRIDGE_IP"':'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'",
  "priority":10,"models":["deploy-model"],"enabled":true
}' "$BASE/api/v1/channels/deploy-channel" > /dev/null
curl -s "${A[@]}" -X PUT -d '{
  "enabled":true,"mode":"failover",
  "members":[{"channel":"deploy-channel","model":"deploy-model","priority":1}]
}' "$BASE/api/v1/lanes/deploy-model" > /dev/null
CLIENT_PLAIN=$(curl -s "${A[@]}" -X POST -d '{"name":"deploy-client"}' "$BASE/api/v1/keys" | jget 'd["key"]')
[[ -n "$CLIENT_PLAIN" ]] || { echo "FAIL: 未取得客户端密钥"; exit 1; }
echo "  渠道与客户端密钥已建立"

echo "--- 6) 跑通一发真实请求"
RESP=$(curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"deploy-model","messages":[{"role":"user","content":"ping"}]}')
echo "  $(echo "$RESP" | head -c 240)"
check "容器内网关转发成功" "$RESP" 'pong from fake upstream'
check "响应 model 回填请求名" "$RESP" '"model":"deploy-model"'
SERVED=$(curl -s -D - -o /dev/null -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"deploy-model","messages":[{"role":"user","content":"ping"}]}' | grep -i '^x-served-by:' | tr -d '\r')
check "X-Served-By 指向成员" "$SERVED" 'channel=1:deploy-channel'

echo "--- 7) 数据卷持久化：重启容器后配置与密钥仍在"
BEFORE_KEY=$(curl -s "${A[@]}" "$BASE/api/v1/keys/deploy-client")
docker restart pbr >/dev/null 2>&1
ok=0
for i in $(seq 1 60); do
  if curl -sf "$BASE/api/v1/health" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
[[ $ok == 1 ]] || { echo "FAIL: 重启后未就绪"; docker logs pbr 2>&1 | tail -30; exit 1; }
AFTER_KEY=$(curl -s "${A[@]}" "$BASE/api/v1/keys/deploy-client")
check "重启后客户端密钥仍在" "$AFTER_KEY" '"name":"deploy-client"'
check "重启后前缀未变" "$AFTER_KEY" "$(echo "$BEFORE_KEY" | jget 'd["key_prefix"]')"
RESP2=$(curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"deploy-model","messages":[{"role":"user","content":"ping"}]}')
check "重启后原密钥仍可转发" "$RESP2" 'pong from fake upstream'
AFTER_CH=$(curl -s "${A[@]}" "$BASE/api/v1/models")
check "重启后模型路由仍在" "$AFTER_CH" '"model":"deploy-model"'

echo "--- 8) 冷却/熔断状态按设计重启清空（进程内运行态）"
HEALTH_AFTER=$(curl -s "${A[@]}" "$BASE/api/v1/lanes/deploy-model/health" 2>/dev/null)
assert_json "重启后运行态无残留冷却" "$HEALTH_AFTER" "all(m['cooldown_until'] is None for m in d['members'])"

echo "--- 9) 现网容器未被触碰"
AFTER_PS=$(live_containers)
echo "  之前：$BEFORE_PS"
echo "  之后：$AFTER_PS"
check "现网容器集合未变" "$AFTER_PS" "$BEFORE_PS"

echo "--- 10) 清理（移除本次独立项目与其卷）"
( cd "$REPO" && docker compose -p "$PROJECT" -f docker-compose.yml -f verify/deploy/compose.override.yml down -v ) >/dev/null 2>&1
REMAIN=$(docker ps -a --filter "label=com.docker.compose.project=$PROJECT" --format '{{.Names}}' | wc -l)
check "本次项目容器已清理" "$REMAIN" "0"

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: 部署验收全部通过"; else
  echo "FAIL: 存在未通过项"
  docker logs pbr 2>&1 | tail -40
  exit 2
fi
echo "=== evidence: $EVID ==="
