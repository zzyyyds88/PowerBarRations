#!/usr/bin/env bash
# PowerBarRations 本机测试环境管理
#
# 用法：
#   ./deploy/test-env.sh refresh   # 从生产拉最新数据快照，重建本机测试环境（默认口令）
#   ./deploy/test-env.sh up        # 启动已有测试环境
#   ./deploy/test-env.sh down      # 停止并删除测试容器（保留数据）
#   ./deploy/test-env.sh status    # 查看状态
#
# 说明：
#   - 数据来自生产 SQLite 在线备份（只读，不停服、不修改生产）。
#   - 测试环境复用生产的 CRYPTO_SECRET，否则复制来的渠道密钥无法解密。
#   - 每次 refresh 会清除测试库的管理凭据并重新设置口令（见 test.env）。
#   - 容器不随开机自启（--restart no）。
#   - 存续时间默认 30 分钟（test.env 的 PBR_TEST_TTL_MINUTES）：每次 refresh/up 重新计时，
#     到期由后台 watchdog 自动停容器并删除（数据保留）。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CFG_DIR="${PBR_CFG_DIR:-$SCRIPT_DIR}"
# shellcheck disable=SC1090
set -a
source "$CFG_DIR/production.env"
source "$CFG_DIR/test.env"
set +a

BASE="http://127.0.0.1:${PBR_TEST_HOST_PORT}"
STATE_DIR="$CFG_DIR/.test-env"
TTL_SECONDS="${PBR_TEST_TTL_SECONDS:-$(( ${PBR_TEST_TTL_MINUTES:-30} * 60 ))}"
SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10
          -o ServerAliveInterval=15 -o NumberOfPasswordPrompts=1)
remote() { SSHPASS="$PBR_PROD_SSH_PASSWORD" sshpass -e ssh "${SSH_OPTS[@]}" \
             "${PBR_PROD_SSH_USER}@${PBR_PROD_HOST}" "$@"; }

log()  { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
die()  { printf '\033[31mERROR: %s\033[0m\n' "$*" >&2; exit 1; }

json_get() { python3 -I -c 'import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1],{"d":d}))' "$1"; }

wait_health() {
  local i
  for i in $(seq 1 60); do
    if curl -sf --noproxy '*' -m 3 "$BASE/api/v1/health" >/dev/null 2>&1; then
      echo "ready after ${i}s"
      return 0
    fi
    sleep 1
  done
  return 1
}

# ── 存续时间（TTL）──────────────────────────────────────────────────────
kill_ttl_watchdog() {
  if [[ -f "$STATE_DIR/watchdog.pid" ]]; then
    kill "$(cat "$STATE_DIR/watchdog.pid")" 2>/dev/null || true
    rm -f "$STATE_DIR/watchdog.pid"
  fi
}

expire_container() {
  docker stop --time 30 "$PBR_TEST_CONTAINER" >/dev/null 2>&1 || true
  docker rm "$PBR_TEST_CONTAINER" >/dev/null 2>&1 || true
  rm -f "$STATE_DIR/expires_at" "$STATE_DIR/watchdog.pid"
}

# 每次启动/刷新都重新计时；到期由后台 watchdog 停容器并删除（数据保留）。
start_ttl() {
  kill_ttl_watchdog
  mkdir -p "$STATE_DIR"
  local expires=$(( $(date +%s) + TTL_SECONDS ))
  printf '%s\n' "$expires" >"$STATE_DIR/expires_at"
  nohup bash -c '
    sleep "$1"
    docker stop --time 30 "$2" >/dev/null 2>&1 || true
    docker rm "$2" >/dev/null 2>&1 || true
    rm -f "$3" "$4"
  ' _ "$TTL_SECONDS" "$PBR_TEST_CONTAINER" "$STATE_DIR/expires_at" "$STATE_DIR/watchdog.pid" \
    </dev/null >/dev/null 2>&1 &
  echo $! >"$STATE_DIR/watchdog.pid"
}

ttl_status() {
  if [[ ! -f "$STATE_DIR/expires_at" ]]; then
    echo "存续: 未设置"
    return
  fi
  local exp now left
  exp="$(cat "$STATE_DIR/expires_at")"
  now="$(date +%s)"
  if (( now >= exp )); then
    echo "存续: 已到期"
    expire_container
  else
    left=$(( (exp - now + 59) / 60 ))
    echo "存续: 剩余约 ${left} 分钟（到期 $(date -d "@$exp" '+%H:%M:%S')）"
  fi
}

start_container() {
  docker rm -f "$PBR_TEST_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$PBR_TEST_CONTAINER" \
    --restart no \
    --stop-timeout 130 \
    -p "${PBR_TEST_HOST_PORT}:5700" \
    -v "${PBR_TEST_DATA_DIR}:/data" \
    -e SESSION_SECRET="$PBR_TEST_SESSION_SECRET" \
    -e CRYPTO_SECRET="$PBR_PROD_CRYPTO_SECRET" \
    -e PBR_BIND=0.0.0.0 \
    -e GIN_MODE=release \
    --health-cmd 'wget -qO- http://127.0.0.1:5700/api/v1/health >/dev/null || exit 1' \
    --health-interval 30s --health-timeout 5s --health-retries 3 --health-start-period 20s \
    "$PBR_TEST_IMAGE" >/dev/null
  wait_health || die "测试容器未就绪"
  start_ttl
}

# 设置/校验口令，返回管理密钥（不打印）
ensure_admin_key() {
  local initialized resp
  initialized="$(curl -s --noproxy '*' "$BASE/api/v1/setup/status" | json_get 'd["initialized"]')"
  if [[ "$initialized" != "True" ]]; then
    resp="$(curl -s --noproxy '*' -X POST "$BASE/api/v1/setup" \
      -H 'Content-Type: application/json' \
      -d "{\"password\":\"${PBR_TEST_LOGIN_PASSWORD}\"}")"
  else
    resp="$(curl -s --noproxy '*' -X POST "$BASE/api/v1/auth/login" \
      -H 'Content-Type: application/json' \
      -d "{\"password\":\"${PBR_TEST_LOGIN_PASSWORD}\"}")"
  fi
  printf '%s' "$resp" | json_get 'd["admin_key"]'
}

count_api() {
  local ep="$1" key="$2"
  curl -s --noproxy '*' -H "Authorization: Bearer $key" "$BASE/api/v1/$ep" \
    | python3 -I -c '
import json,sys
d=json.load(sys.stdin)
if isinstance(d,list): print(len(d))
elif isinstance(d,dict):
    for k in ("data","items","channels","keys","lanes"):
        if isinstance(d.get(k),list): print(len(d[k])); break
    else: print("-")
else: print("-")
'
}

status() {
  log "测试环境状态"
  ttl_status
  docker ps -a --filter "name=^/${PBR_TEST_CONTAINER}$" \
    --format '容器: {{.Names}}  状态: {{.Status}}  端口: {{.Ports}}' || true
  echo -n "健康: "; curl -s --noproxy '*' -m 3 "$BASE/api/v1/health" || echo "不可达"
  echo -n "版本: "; curl -s --noproxy '*' -m 3 "$BASE/api/v1/version" || echo "不可达"
  echo -n "初始化: "; curl -s --noproxy '*' -m 3 "$BASE/api/v1/setup/status" || echo "不可达"
}

refresh_data() {
  log "从生产拉取数据快照（在线备份，不影响生产）"
  remote bash -s <<REMOTE
set -euo pipefail
rm -rf /tmp/pbr-testbackup
mkdir -p /tmp/pbr-testbackup
docker run --rm \
  -v '${PBR_TEST_SOURCE_DATA_DIR}':/data \
  -v /tmp/pbr-testbackup:/backup \
  alpine:3.20 sh -c '
    sed -i "s|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g" /etc/apk/repositories
    apk add --no-cache sqlite >/dev/null
    sqlite3 /data/pbr.db ".backup /backup/pbr.db"
    sqlite3 /backup/pbr.db "PRAGMA integrity_check;" | grep -qx ok
  '
REMOTE

  sshpass -p "$PBR_PROD_SSH_PASSWORD" scp "${SSH_OPTS[@]}" \
    "${PBR_PROD_SSH_USER}@${PBR_PROD_HOST}:/tmp/pbr-testbackup/pbr.db" \
    /tmp/pbr-test-snapshot.db
  remote "rm -rf /tmp/pbr-testbackup"
  echo "  快照: $(du -h /tmp/pbr-test-snapshot.db | cut -f1)  integrity: ok"

  log "替换测试库并重建容器"
  kill_ttl_watchdog
  docker stop --time 30 "$PBR_TEST_CONTAINER" >/dev/null 2>&1 || true
  docker rm "$PBR_TEST_CONTAINER" >/dev/null 2>&1 || true
  mkdir -p "$PBR_TEST_DATA_DIR"
  if [[ -f "$PBR_TEST_DATA_DIR/pbr.db" ]]; then
    mv "$PBR_TEST_DATA_DIR/pbr.db" "$PBR_TEST_DATA_DIR/pbr.db.prev-$(date +%Y%m%d-%H%M%S)"
  fi
  rm -f "$PBR_TEST_DATA_DIR"/pbr.db-wal "$PBR_TEST_DATA_DIR"/pbr.db-shm
  cp /tmp/pbr-test-snapshot.db "$PBR_TEST_DATA_DIR/pbr.db"
  chown 10001:10001 "$PBR_TEST_DATA_DIR/pbr.db"
  chmod 600 "$PBR_TEST_DATA_DIR/pbr.db"
  ls -1t "$PBR_TEST_DATA_DIR"/pbr.db.prev-* 2>/dev/null | tail -n +3 | xargs -r rm -f

  log "重置测试库口令并启动"
  docker run --rm -v "$PBR_TEST_DATA_DIR:/data" "$PBR_TEST_IMAGE" \
    auth reset --db /data/pbr.db --yes
  start_container

  ADMIN_KEY="$(ensure_admin_key)"
  [[ -n "$ADMIN_KEY" ]] || die "未能取得管理密钥"
  log "测试环境就绪"
  printf '  登录口令: %s\n' "$PBR_TEST_LOGIN_PASSWORD"
  printf '  渠道: %s  客户端密钥: %s  车道: %s\n' \
    "$(count_api channels "$ADMIN_KEY")" \
    "$(count_api keys "$ADMIN_KEY")" \
    "$(count_api lanes "$ADMIN_KEY")"
  status
  rm -f /tmp/pbr-test-snapshot.db
}

case "${1:-}" in
  refresh) refresh_data ;;
  up)
    if docker inspect "$PBR_TEST_CONTAINER" >/dev/null 2>&1; then
      docker start "$PBR_TEST_CONTAINER" >/dev/null 2>&1 || true
      wait_health || die "测试容器未就绪"
      start_ttl
    else
      [[ -f "$PBR_TEST_DATA_DIR/pbr.db" ]] || die "测试库不存在，请先 refresh"
      start_container
    fi
    status
    ;;
  down)
    kill_ttl_watchdog
    rm -f "$STATE_DIR/expires_at"
    docker stop --time 30 "$PBR_TEST_CONTAINER" >/dev/null 2>&1 || true
    docker rm "$PBR_TEST_CONTAINER" >/dev/null 2>&1 || true
    echo "已停止并删除 $PBR_TEST_CONTAINER（数据保留在 $PBR_TEST_DATA_DIR）"
    ;;
  status) status ;;
  *)
    echo "用法: $0 {refresh|up|down|status}" >&2
    exit 2
    ;;
esac
