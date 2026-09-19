#!/usr/bin/env bash
# L3-manual：真实图形浏览器环境（人可亲手操作）。
#
# 与 L3 自动化的区别：L3 是脚本用 CDP 驱动；本脚本起一个**可见的** Chromium
# （Xvfb 虚拟显示 + openbox 窗口管理器），并通过 x11vnc + noVNC 暴露成网页，
# 人用真实鼠标/键盘（或浏览器里的 VNC 画面）亲手操作 PBR 控制台。
#
# 用法：
#   bash verify/e2e-ui/live_browser.sh start   # 起环境并打印访问地址/凭据
#   bash verify/e2e-ui/live_browser.sh stop    # 停掉全部进程
#   bash verify/e2e-ui/live_browser.sh status  # 查看状态
#   bash verify/e2e-ui/live_browser.sh shot [名字]  # 抓一张当前屏幕截图
#
# 依赖：Xvfb x11vnc websockify novnc openbox xdotool imagemagick（Debian/Ubuntu 包名）。
set -uo pipefail

REPO="${PBR_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORK=/tmp/pbr-live
DISPLAY_NUM=:99
SCREEN=1440x900x24
PBR_PORT=6890
UP_PORT=6891
VNC_PORT=5900
NOVNC_PORT=6080
PW="Pbr-Live-Manual-Passw0rd!2026"
GOOD_KEY="sk-live-good"

mkdir -p "$WORK"

stop_all() {
  pkill -f "$WORK/pbr" 2>/dev/null
  pkill -f "$WORK/fakeupstream" 2>/dev/null
  pkill -f "user-data-dir=$WORK/profile" 2>/dev/null
  pkill -f "openbox" 2>/dev/null
  pkill -f "x11vnc -display $DISPLAY_NUM" 2>/dev/null
  pkill -f "websockify --web=/usr/share/novnc $NOVNC_PORT" 2>/dev/null
  pkill -f "Xvfb $DISPLAY_NUM" 2>/dev/null
  sleep 1
}

case "${1:-status}" in
  stop)
    stop_all
    echo "已停止"
    exit 0
    ;;
  status)
    echo "--- 进程 ---"
    for pat in "Xvfb $DISPLAY_NUM" "x11vnc -display $DISPLAY_NUM" "websockify --web=/usr/share/novnc" "user-data-dir=$WORK/profile" "$WORK/pbr"; do
      n=$(pgrep -f "$pat" 2>/dev/null | wc -l)
      printf "%-42s %s\n" "$pat" "$n"
    done
    echo "--- 端口 ---"
    ss -ltn 2>/dev/null | grep -E ":$PBR_PORT|:$UP_PORT|:$VNC_PORT|:$NOVNC_PORT" || echo "(未监听)"
    echo "--- 访问 ---"
    echo "noVNC:  http://<本机IP>:$NOVNC_PORT/vnc.html?autoconnect=1&resize=scale"
    exit 0
    ;;
  shot)
    export DISPLAY="$DISPLAY_NUM"
    out="$WORK/shot-${2:-manual}-$(date +%H%M%S).png"
    import -window root "$out" 2>/dev/null && echo "$out" || { echo "抓图失败（环境未运行？）"; exit 1; }
    exit 0
    ;;
  start) ;;
  *) echo "用法: $0 {start|stop|status|shot [名字]}"; exit 2 ;;
esac

stop_all

echo "--- 构建（控制台 + 网关 + 假上游）"
( cd "$REPO/web" && pnpm build ) > "$WORK/pnpm.log" 2>&1 || { echo "FAIL: web build"; tail -20 "$WORK/pnpm.log"; exit 1; }
( cd "$REPO" && go build -o "$WORK/pbr" . && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: go build"; exit 1; }
( cd "$REPO" && git checkout -- web/dist/index.html )

echo "--- Xvfb $DISPLAY_NUM ($SCREEN)"
Xvfb "$DISPLAY_NUM" -screen 0 "$SCREEN" -ac +extension GLX +render -noreset > "$WORK/xvfb.log" 2>&1 &
sleep 2
export DISPLAY="$DISPLAY_NUM"
openbox --sm-disable > "$WORK/openbox.log" 2>&1 &
sleep 1

echo "--- 假上游 :$UP_PORT"
"$WORK/fakeupstream" -addr "127.0.0.1:$UP_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" > "$WORK/upstream.out" 2>&1 &

echo "--- PBR :$PBR_PORT"
( cd "$WORK" && exec env PORT=$PBR_PORT PBR_BIND=0.0.0.0 \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=live-session CRYPTO_SECRET=live-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &
for i in $(seq 1 60); do curl -sf "http://127.0.0.1:$PBR_PORT/api/v1/health" >/dev/null 2>&1 && break; sleep 1; done

echo "--- 预置演示数据（渠道 + 车道 + 客户端密钥）"
H='-H Content-Type:application/json'
AK=$(curl -s $H -d '{"password":"'"$PW"'"}' "http://127.0.0.1:$PBR_PORT/api/v1/setup" | python3 -c "import sys,json;print(json.load(sys.stdin).get('admin_key',''))" 2>/dev/null)
if [ -z "$AK" ]; then
  AK=$(python3 -c "import hashlib,base64;print(base64.b64encode(hashlib.sha256(b'$PW').digest()).decode())")
fi
A=(-H "Authorization: Bearer $AK" -H 'Content-Type: application/json')
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UP_PORT"'","key":"'"$GOOD_KEY"'","models":["demo-model"],"enabled":true}' "http://127.0.0.1:$PBR_PORT/api/v1/channels/demo-channel" >/dev/null
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","members":[{"channel":"demo-channel","upstream_model":"demo-model","priority":10}]}' "http://127.0.0.1:$PBR_PORT/api/v1/lanes/demo-model" >/dev/null
CK=$(curl -s "${A[@]}" -X POST -d '{"name":"demo-client"}' "http://127.0.0.1:$PBR_PORT/api/v1/keys" | python3 -c "import sys,json;print(json.load(sys.stdin).get('key',''))" 2>/dev/null)

echo "--- x11vnc :$VNC_PORT"
x11vnc -display "$DISPLAY_NUM" -rfbport "$VNC_PORT" -forever -shared -nopw -quiet -bg > "$WORK/x11vnc.log" 2>&1
sleep 2
echo "--- noVNC :$NOVNC_PORT"
websockify --web=/usr/share/novnc "$NOVNC_PORT" "localhost:$VNC_PORT" > "$WORK/novnc.log" 2>&1 &
sleep 2

echo "--- 可见 Chromium（$DISPLAY_NUM）"
DISPLAY="$DISPLAY_NUM" chromium --no-sandbox --disable-gpu --disable-dev-shm-usage \
  --no-first-run --no-default-browser-check --lang=zh-CN \
  --user-data-dir="$WORK/profile" --window-size=1440,900 --window-position=0,0 \
  "http://127.0.0.1:$PBR_PORT/" > "$WORK/chromium.log" 2>&1 &
sleep 5

IP=$(ip -4 addr show 2>/dev/null | grep -oE "inet [0-9.]+" | awk '{print $2}' | grep -v "^127\." | head -1)
echo
echo "==================== 就绪 ===================="
echo "在浏览器打开（noVNC，可直接用鼠标键盘操作）："
echo "  http://${IP:-127.0.0.1}:$NOVNC_PORT/vnc.html?autoconnect=1&resize=scale"
echo
echo "PBR 控制台（在 VNC 画面里已打开）：http://127.0.0.1:$PBR_PORT/"
echo "登录口令：$PW"
echo "管理密钥：$AK"
echo "客户端密钥：$CK"
echo "演示模型：demo-model（渠道 demo-channel）"
echo "=============================================="