#!/usr/bin/env bash
# W4 验收（后端可见部分）：控制台构建零报错、产物被打进二进制并可服务、SSE 可流。
# 页面级走查（登录/仪表盘/车道拖拽/日志尝试链等）由浏览器黑盒测试单独覆盖，见 verify/w4/README.md。
#
# 全程本地：独立端口、独立 SQLite、内置假上游；不触碰任何现网容器与凭据。
set -uo pipefail

REPO=/root/powerbar-rations
WORK=/tmp/pbr-w4
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/w4/run-$STAMP.log"
PORT=6797
UPSTREAM_PORT=6808
GOOD_KEY='sk-w4-good'
PBR_PW='Pbr-W4-Verify-Passw0rd!2026'

exec > "$EVID" 2>&1
echo "=== W4 控制台验收（构建 + 服务 + SSE）@ $STAMP ==="

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

cleanup() {
  pkill -f "$WORK/pbr" 2>/dev/null
  pkill -f "$WORK/fakeupstream" 2>/dev/null
  # 还原仓库里的占位页，避免把构建产物留在工作区
  ( cd "$REPO" && git checkout -- web/dist/index.html ) 2>/dev/null
  wait 2>/dev/null
}
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"
for p in "$PORT" "$UPSTREAM_PORT"; do
  if ss -ltn 2>/dev/null | grep -q ":$p "; then echo "FAIL: 端口 $p 被占用"; exit 1; fi
done

echo "--- 0) 控制台构建（验收门：零报错）"
( cd "$REPO/web" && pnpm build ) > "$WORK/pnpm-build.log" 2>&1
BUILD_EXIT=$?
tail -12 "$WORK/pnpm-build.log"
check "pnpm build 退出码为 0" "$BUILD_EXIT" "0"
check "产物含 hash 化的 JS" "$(grep -c 'assets/index-.*\.js' "$WORK/pnpm-build.log")" "1"
ls -1 "$REPO/web/dist" "$REPO/web/dist/assets" 2>/dev/null | head -10

echo "--- 1) 构建二进制（embed 真实控制台）与假上游"
( cd "$REPO" && go build -o "$WORK/pbr" . \
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: build"; exit 1; }
"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.stdout" 2>&1 &
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=w4-session CRYPTO_SECRET=w4-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

echo "--- 2) 等待就绪"
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

echo "--- 3) 首页由二进制内嵌的控制台提供（不是占位页）"
INDEX=$(curl -s "$BASE/")
echo "  $(echo "$INDEX" | head -c 200)"
check "首页引用打包后的 JS" "$INDEX" '/assets/index-'
check_not "不是 W0 的占位页" "$INDEX" '控制台将在 W4 迁入'
ASSET=$(echo "$INDEX" | grep -o '/assets/index-[^"]*\.js' | head -1)
echo "  入口资源：$ASSET"
ASSET_HEAD=$(curl -s -D - -o /dev/null "$BASE$ASSET")
check "入口 JS 可访问" "$ASSET_HEAD" "200"
check "入口 JS 为 js 内容类型" "$ASSET_HEAD" "javascript"
ASSET_BODY=$(curl -s "$BASE$ASSET")
check "入口 JS 含控制台品牌" "$ASSET_BODY" "PowerBarRations"
check "入口 JS 含关键页面文案" "$ASSET_BODY" "请求日志"

echo "--- 4) SPA 回退：未知前端路径返回首页（/api 与 /v1 不吞）"
FALLBACK=$(curl -s "$BASE/lanes")
check "前端路由回退到首页" "$FALLBACK" '/assets/index-'
API404=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/does-not-exist")
check "未知管理接口不返回首页（404）" "$API404" "404"

echo "--- 5) 配置出一份可看的数据，供页面走查"
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
for name in channel-a:20 channel-b:10; do
  ch="${name%%:*}"; pr="${name##*:}"
  curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","priority":'"$pr"',"models":["console-model"],"enabled":true}' \
    "$BASE/api/v1/channels/$ch" > /dev/null
done
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","members":[{"channel":"channel-a","upstream_model":"console-model","priority":20},{"channel":"channel-b","upstream_model":"console-model","priority":10}]}' \
  "$BASE/api/v1/lanes/console-model" > /dev/null
CLIENT_PLAIN=$(curl -s "${A[@]}" -X POST -d '{"name":"console-client"}' "$BASE/api/v1/keys" | jget 'd["key"]')
curl -s -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"console-model","messages":[{"role":"user","content":"ping"}]}' > /dev/null
curl -s -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT_PLAIN" -H 'Content-Type: application/json' \
  -d '{"model":"ghost-model","messages":[{"role":"user","content":"ping"}]}' > /dev/null
LOGS=$(curl -s "${A[@]}" "$BASE/api/v1/logs")
check "日志有成功与失败两种" "$LOGS" '"success":false'
MODELS=$(curl -s "${A[@]}" "$BASE/api/v1/models")
check "模型路由可见" "$MODELS" '"model":"console-model"'

echo "--- 6) SSE：/route-events 可流式推送车道运行态"
SSE=$(curl -sN --max-time 3 -H "Authorization: Bearer $ADMIN_KEY" "$BASE/api/v1/route-events" | head -c 600)
echo "  $(echo "$SSE" | head -c 300)"
check "SSE 事件名正确" "$SSE" 'event: route-state'
check "SSE 载荷含车道运行态" "$SSE" '"lanes"'
check "SSE 载荷含成员熔断状态" "$SSE" '"circuit"'

echo "--- 7) 未带凭据的 SSE 应被拒（401）"
SSE_CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$BASE/api/v1/route-events")
check "未鉴权 SSE 返回 401" "$SSE_CODE" "401"

echo "--- 8) 控制台只调用契约里登记的端点（控制台 ↔ openapi 一致性）"
curl -s "${A[@]}" "$BASE/api/v1/openapi.json" -o "$WORK/openapi.json"
CONSISTENCY=$(python3 "$REPO/verify/w4/console_contract_check.py" "$REPO" "$WORK/openapi.json")
echo "  结论：$(echo "$CONSISTENCY" | head -c 500)"
assert_ct() {
  local desc="$1" expr="$2"
  if echo "$CONSISTENCY" | python3 -c "import sys,json;d=json.load(sys.stdin);sys.exit(0 if ($expr) else 1)"; then
    echo "  PASS: $desc"; PASS=$((PASS+1))
  else
    echo "  FAIL: $desc（$CONSISTENCY）"; FAIL=$((FAIL+1))
  fi
}
assert_ct "控制台调用过的端点都在 openapi 里" "len(d['missing'])==0"
# 覆盖数下限：api/ 层当前封装了 29 个路径；低于 25 说明解析退化（曾因嵌套泛型
# 与 withQuery 包装漏掉大半端点，覆盖率从 32 掉到 13 而阈值 8 仍"通过"）。
assert_ct "至少覆盖 25 个端点" "len(d['checked'])>=25"
# ui-spec §1：api/ 是唯一允许封装管理端点的层，页面/组件不得直连或自造接口。
assert_ct "没有页面绕过 api 层直连管理端点" "len(d['bypass'])==0"

echo "--- 9) 还原占位页（构建产物不入库）"
( cd "$REPO" && git checkout -- web/dist/index.html )
check "工作区占位页已还原" "$(cat "$REPO/web/dist/index.html")" 'W4 迁入'

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: W4 后端可见部分验收通过"; else
  echo "FAIL: 存在未通过项"; echo "--- pbr.log 末尾 ---"; tail -40 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
