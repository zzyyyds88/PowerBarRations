#!/usr/bin/env bash
# PowerBarRations 生产回滚脚本
#
# 用法：
#   ./deploy/rollback.sh                 # 列出生产机上的镜像标签
#   ./deploy/rollback.sh <镜像tag>       # 切换回指定镜像（如 powerbar-rations:rollback-20261106-120000）
#
# 说明：数据卷 /srv/docker/data/pbr 不会被动；deploy/version 不会回写（下次 release 仍按当前版本递增）。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CFG_DIR="${PBR_CFG_DIR:-$SCRIPT_DIR}"
ENV_FILE="$CFG_DIR/production.env"
[[ -f "$ENV_FILE" ]] || { echo "ERROR: 缺少 $ENV_FILE" >&2; exit 1; }
# shellcheck disable=SC1090
set -a; source "$ENV_FILE"; set +a

SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10
          -o ServerAliveInterval=15 -o NumberOfPasswordPrompts=1)
remote() { SSHPASS="$PBR_PROD_SSH_PASSWORD" sshpass -e ssh "${SSH_OPTS[@]}" \
             "${PBR_PROD_SSH_USER}@${PBR_PROD_HOST}" "$@"; }

log() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
die() { printf '\033[31mERROR: %s\033[0m\n' "$*" >&2; exit 1; }

if [[ $# -eq 0 ]]; then
  log "生产机可用镜像"
  remote "docker images --format '{{.Repository}}:{{.Tag}}  {{.ID}}  {{.CreatedAt}}' | grep '^${PBR_PROD_IMAGE_REPO}:' || true"
  echo
  echo "用法：$0 <镜像tag>"
  exit 0
fi

TARGET="$1"
remote "docker image inspect '${TARGET}' >/dev/null 2>&1" \
  || die "生产机不存在镜像：${TARGET}"

log "回滚到 ${TARGET}"
remote bash -s <<REMOTE
set -euo pipefail
docker stop --time ${PBR_PROD_STOP_GRACE} ${PBR_PROD_CONTAINER} >/dev/null 2>&1 || true
docker rm ${PBR_PROD_CONTAINER} >/dev/null 2>&1 || true
docker run -d --name ${PBR_PROD_CONTAINER} \
  --restart unless-stopped \
  --stop-timeout ${PBR_PROD_STOP_GRACE} \
  -p ${PBR_PROD_HOST_PORT}:${PBR_PROD_CONTAINER_PORT} \
  -v ${PBR_PROD_DATA_DIR}:/data \
  -e SESSION_SECRET='${PBR_PROD_SESSION_SECRET}' \
  -e CRYPTO_SECRET='${PBR_PROD_CRYPTO_SECRET}' \
  -e PBR_BIND=0.0.0.0 \
  -e GIN_MODE=release \
  --health-cmd 'wget -qO- http://127.0.0.1:5700/api/v1/health >/dev/null || exit 1' \
  --health-interval 30s --health-timeout 5s --health-retries 3 --health-start-period 20s \
  '${TARGET}' >/dev/null
REMOTE

body=""
for _ in $(seq 1 90); do
  body="$(curl -s --noproxy '*' -m 3 \
    "http://${PBR_PROD_HOST}:${PBR_PROD_HOST_PORT}/api/v1/version" 2>/dev/null || true)"
  if printf '%s' "$body" | grep -q '"version"'; then break; fi
  sleep 1
done

if printf '%s' "$body" | grep -q '"version"'; then
  log "回滚完成：${body}"
else
  remote "docker logs --tail 80 ${PBR_PROD_CONTAINER}" 2>&1 | tail -40 || true
  die "回滚后服务未通过健康检查，请人工介入"
fi
