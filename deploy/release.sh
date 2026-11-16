#!/usr/bin/env bash
# PowerBarRations 发布脚本（本地构建 -> SSH 传输镜像 -> 生产 docker load -> 切换容器）
#
# 用法：
#   ./deploy/release.sh --build-only        # 只构建并归档镜像（默认行为，不碰生产）
#   ./deploy/release.sh --deploy --approved "<用户批准说明>" [--allow-downtime]
#                                           # 替换生产：必须逐次获得用户明确许可
#   ./deploy/release.sh --version v1.2.0 --build-only
#   ./deploy/release.sh --allow-dirty       # 允许工作区有未提交改动（不推荐）
#
# 生产变更纪律（2026-11-06 事故后新增，强制）：
#   - 默认只构建，不部署；替换生产必须显式 --deploy 且 --approved 非空。
#   - 当前 stop-first 方式会中断在途请求；没有蓝绿方案时必须显式 --allow-downtime。
#   - 禁止把“标签对齐/版本记录”等非业务原因当作发布理由。
#
# 设计约束（2026-11-06）：
#   - 构建只在本机做；生产机只负责 docker load / docker run，不留源码。
#   - 版本号由本脚本管理：默认在「上次成功部署版本」基础上 patch+1。
#   - 构建时注入 VERSION，/api/v1/version 必须能读到该版本号。
#   - 停止旧容器前必须先确认新容器可接管；失败自动回滚。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CFG_DIR="${PBR_CFG_DIR:-$SCRIPT_DIR}"
ENV_FILE="$CFG_DIR/production.env"
[[ -f "$ENV_FILE" ]] || { echo "ERROR: 缺少 $ENV_FILE" >&2; exit 1; }
# shellcheck disable=SC1090
set -a; source "$ENV_FILE"; set +a

REPO="${PBR_REPO:-$(cd "$SCRIPT_DIR/.." && pwd)}"
VERSION_FILE="${PBR_VERSION_FILE:-$CFG_DIR/version}"
HISTORY="${PBR_DEPLOY_HISTORY:-$CFG_DIR/deploy-history.log}"
ART_DIR="${PBR_ARTIFACT_DIR:-$CFG_DIR/images}"

MODE="build"
EXPLICIT_VERSION=""
ALLOW_DIRTY=0
APPROVED=""
ALLOW_DOWNTIME=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --build-only) MODE="build"; shift ;;
    --deploy) MODE="deploy"; shift ;;
    --approved) APPROVED="${2:?--approved 需要参数}"; shift 2 ;;
    --allow-downtime) ALLOW_DOWNTIME=1; shift ;;
    --version) EXPLICIT_VERSION="${2:?--version 需要参数}"; shift 2 ;;
    --allow-dirty) ALLOW_DIRTY=1; shift ;;
    -h|--help) sed -n '2,26p' "$0"; exit 0 ;;
    *) echo "ERROR: 未知参数 $1" >&2; exit 2 ;;
  esac
done

log()  { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[33mWARN: %s\033[0m\n' "$*" >&2; }
die()  { printf '\033[31mERROR: %s\033[0m\n' "$*" >&2; exit 1; }

# 生产替换必须逐次得到用户明确许可；默认只构建。
if [[ "$MODE" == "deploy" ]]; then
  [[ -n "${APPROVED// /}" ]] || die "拒绝部署：必须获得用户明确许可后传 --approved \"<批准说明>\""
  if [[ $ALLOW_DOWNTIME -ne 1 ]]; then
    die "拒绝部署：当前 stop-first 方式会中断在途请求；蓝绿方案落地前，必须由用户批准 --allow-downtime"
  fi
fi

# 同一时间只允许一个发布进程
mkdir -p "$CFG_DIR"
exec 9>"$CFG_DIR/.release.lock"
flock -n 9 || die "已有发布正在进行（$CFG_DIR/.release.lock）"

# ── 1. 前置检查 ─────────────────────────────────────────────────────────
[[ -d "$REPO/.git" ]] || die "仓库不存在：$REPO"
cd "$REPO"

if [[ -n "$(git status --porcelain)" ]]; then
  if [[ $ALLOW_DIRTY -eq 1 ]]; then
    warn "工作区有未提交改动，按 --allow-dirty 继续"
  else
    git status --short >&2
    die "工作区不干净；请先提交，或显式使用 --allow-dirty"
  fi
fi
SHA="$(git rev-parse HEAD)"
SHORT="$(git rev-parse --short=12 HEAD)"
if REMOTE_SHA="$(git rev-parse origin/main 2>/dev/null)"; then
  [[ "$REMOTE_SHA" == "$SHA" ]] || warn "HEAD ($SHORT) 与 origin/main (${REMOTE_SHA:0:12}) 不一致，确认是否已推送"
fi

# ── 2. 版本号 ───────────────────────────────────────────────────────────
LAST_VERSION="$(cat "$VERSION_FILE" 2>/dev/null || echo v0.0.0)"
if [[ -n "$EXPLICIT_VERSION" ]]; then
  NEXT="$EXPLICIT_VERSION"
else
  core="${LAST_VERSION#v}"
  IFS=. read -r major minor patch <<<"$core"
  case "$major$minor$patch" in
    ''|*[!0-9]*) die "本地版本文件格式非法：$LAST_VERSION" ;;
  esac
  NEXT="v${major}.${minor}.$((patch + 1))"
fi
[[ "$NEXT" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "版本号必须形如 v1.2.3：$NEXT"

log "版本：${LAST_VERSION} -> ${NEXT}（commit ${SHORT}）"

# 脱敏模型随镜像分发（privacy-spec §3.2）；模型文件被 .gitignore 排除，
# 发布前必须存在于本地工作区，否则 Dockerfile 的 COPY 会失败。
PII_MODEL_DIR="$REPO/privacy/astrlink-guard/7216b6a3b897"
for file in model_int8.onnx model_int8.onnx_data tokenizer.json; do
  [[ -f "$PII_MODEL_DIR/$file" ]] || die "缺少脱敏模型文件：$PII_MODEL_DIR/$file（先在控制台下载/预置后再发布）"
done

# 公开仓库的密钥形状自查：GitHub secret scanning 按形状匹配，测试夹具里的
# 完整字面量会在 Security 页挂出误报告警（2026-11-15 实测）。
if [[ -x "$REPO/scripts/check-public-secrets.py" ]]; then
  python3 "$REPO/scripts/check-public-secrets.py" \
    || die "检出会被 GitHub secret scanning 标记的密钥形状，先拆分字面量再发布"
fi

# ── 3. 本地构建 ─────────────────────────────────────────────────────────
BUILD_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
IMAGE="${PBR_PROD_IMAGE_REPO}:${NEXT}"
log "构建镜像 ${IMAGE}"
docker build \
  --build-arg VERSION="$NEXT" \
  --label "org.opencontainers.image.title=PowerBarRations" \
  --label "org.opencontainers.image.version=${NEXT}" \
  --label "org.opencontainers.image.revision=${SHA}" \
  --label "org.opencontainers.image.created=${BUILD_TS}" \
  -t "$IMAGE" \
  -t "${PBR_PROD_IMAGE_REPO}:prod" \
  "$REPO"

IMAGE_ID="$(docker image inspect "$IMAGE" --format '{{.Id}}')"
log "镜像完成：${IMAGE_ID:0:24}"

# ── 4. 归档 ─────────────────────────────────────────────────────────────
mkdir -p "$ART_DIR"
TAR="$ART_DIR/${PBR_PROD_IMAGE_REPO}-${NEXT}.tar.gz"
log "归档镜像：$TAR"
docker save "$IMAGE" | gzip -6 >"$TAR"
printf '  归档大小：%s\n' "$(du -h "$TAR" | cut -f1)"
# 只保留最近 3 个归档
ls -1t "$ART_DIR"/*.tar.gz 2>/dev/null | tail -n +4 | xargs -r rm -f

if [[ "$MODE" == "build" ]]; then
  log "构建完成（--build-only，未部署）"
  exit 0
fi

# ── 5. 生产预检 ─────────────────────────────────────────────────────────
SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10
          -o ServerAliveInterval=15 -o NumberOfPasswordPrompts=1)
remote() { SSHPASS="$PBR_PROD_SSH_PASSWORD" sshpass -e ssh "${SSH_OPTS[@]}" \
             "${PBR_PROD_SSH_USER}@${PBR_PROD_HOST}" "$@"; }

remote "docker version >/dev/null 2>&1 && test -d '${PBR_PROD_DATA_DIR}'" \
  || die "生产预检失败：docker 不可用或数据目录 ${PBR_PROD_DATA_DIR} 不存在"

OLD_IMAGE="$(remote "docker inspect ${PBR_PROD_CONTAINER} --format '{{.Config.Image}}'" 2>/dev/null || true)"
log "当前生产镜像：${OLD_IMAGE:-<无容器>}"

BK_TAG="${PBR_PROD_IMAGE_REPO}:rollback-$(date +%Y%m%d-%H%M%S)"
if [[ -n "$OLD_IMAGE" ]]; then
  remote "docker tag '${OLD_IMAGE}' '${BK_TAG}'" || warn "旧镜像打回滚标签失败"
  OLD_SAFE="$(printf '%s' "$OLD_IMAGE" | tr '/:' '__')"
  remote "mkdir -p '${PBR_PROD_IMAGE_DIR}' && docker save '${OLD_IMAGE}' | gzip -1 > '${PBR_PROD_IMAGE_DIR}/${OLD_SAFE}-$(date +%Y%m%d-%H%M%S).tar.gz'" \
    || warn "旧镜像归档失败（回滚标签仍可用）"
fi

# ── 6. 传输并加载镜像 ───────────────────────────────────────────────────
log "传输镜像到生产（$(du -h "$TAR" | cut -f1)）"
remote 'gunzip | docker load' <"$TAR"
remote "docker image inspect '${IMAGE}' --format '生产镜像：{{.Id}}'" \
  || die "生产未加载到镜像 ${IMAGE}"

# ── 7. 切换容器 ─────────────────────────────────────────────────────────
run_container() {
  local image="$1" label_version="$2"
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
  --label org.opencontainers.image.version='${label_version}' \
  '${image}' >/dev/null
REMOTE
}

wait_health() {
  local want_version="$1" i body
  for i in $(seq 1 90); do
    body="$(curl -s --noproxy '*' -m 3 \
      "http://${PBR_PROD_HOST}:${PBR_PROD_HOST_PORT}/api/v1/version" 2>/dev/null || true)"
    if [[ -n "$body" ]] && printf '%s' "$body" | grep -q '"version"'; then
      if [[ -z "$want_version" ]] || printf '%s' "$body" | grep -Fq "\"version\":\"${want_version}\""; then
        printf '%s\n' "$body"
        return 0
      fi
    fi
    sleep 1
  done
  return 1
}

log "切换容器到 ${NEXT}（已获用户批准：${APPROVED}）"
printf '  版本=%s  commit=%s  旧镜像=%s  停止宽限=%ss（会中断在途请求）\n' \
  "$NEXT" "$SHORT" "${OLD_IMAGE:-none}" "$PBR_PROD_STOP_GRACE"
run_container "$IMAGE" "$NEXT" || die "容器启动失败"

if VERSION_BODY="$(wait_health "$NEXT")"; then
  log "健康校验通过：$VERSION_BODY"
else
  warn "新版本健康/版本校验失败，回滚到 ${OLD_IMAGE:-<无旧镜像>}"
  if [[ -n "$OLD_IMAGE" ]]; then
    run_container "$OLD_IMAGE" "${OLD_IMAGE##*:}" || warn "回滚容器启动失败，请人工介入"
    ROLLBACK_BODY="$(wait_health "" 2>/dev/null || true)"
    [[ -n "$ROLLBACK_BODY" ]] && log "回滚后健康：$ROLLBACK_BODY" || warn "回滚后仍未健康，请人工介入"
  else
    remote "docker logs --tail 80 ${PBR_PROD_CONTAINER}" 2>&1 | tail -30 || true
  fi
  exit 1
fi

# ── 8. 记录成功 ─────────────────────────────────────────────────────────
printf '%s\n' "$NEXT" >"$VERSION_FILE"
printf '%s  version=%s  commit=%s  image=%s  previous=%s  approved=%s\n' \
  "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$NEXT" "$SHA" "$IMAGE_ID" "${OLD_IMAGE:-none}" "$APPROVED" \
  >>"$HISTORY"
log "部署成功：${NEXT}"
printf '  健康接口：http://%s:%s/api/v1/health\n' "$PBR_PROD_HOST" "$PBR_PROD_HOST_PORT"
printf '  版本接口：http://%s:%s/api/v1/version\n' "$PBR_PROD_HOST" "$PBR_PROD_HOST_PORT"
printf '  回滚标签：%s\n' "$BK_TAG"
