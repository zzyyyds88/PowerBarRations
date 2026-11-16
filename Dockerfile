# PowerBarRations —— 多阶段构建
#
# 阶段 1：构建 Go 二进制（控制台产物由 web/dist 提供，见 W4）。
# 阶段 2：最小运行镜像，非 root 用户，数据卷落在 /data。
# 阶段 0：构建控制台（产物供阶段 1 的 go:embed 使用）
#
# 用 Debian(glibc) 而非 Alpine(musl)：@rspack 的 Linux 原生绑定以可选依赖分发，
# Alpine 上曾出现 `Cannot find module '@rspack/binding-linux-x64-musl'`（musl
# 包未落盘）。这个阶段只提供构建产物、不会进入最终镜像；换成 glibc 不增加交付
# 镜像体积，却把构建平台从"依赖 musl 原生产物"变成与本地/CI 一致的 glibc。
FROM node:22-bookworm-slim AS web

WORKDIR /src/web
RUN corepack enable
# 说明：脱敏模型不在前端阶段；见下面的 pii-model 阶段（构建期下载并校验）。
# 国内源：pnpm/corepack 走 npmmirror，避免拉包超时。
ENV COREPACK_NPM_REGISTRY=https://registry.npmmirror.com \
    npm_config_registry=https://registry.npmmirror.com
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# 阶段 PII：把本地已下载的脱敏模型校验后打进镜像，运行时不再依赖网络。
# 版本与 SHA-256 必须与 internal/privacy/pii/service.go 的 modelFiles 保持一致。
# 模型文件被 .gitignore 排除，构建前需存在于下面的路径（见 deploy/release.sh 的前置检查）。
FROM alpine:3.20 AS pii-model

WORKDIR /out
COPY privacy/astrlink-guard/7216b6a3b897/ ./
RUN set -eux; \
    printf '%s  %s\n' \
      '0a070d3113615e33ba3b84d6bd486f663a377c04c4b056e7381732179f51832a' 'model_int8.onnx' \
      '0ba34216ceead01e0ae2dfd8e46d52b67e7aee80b04f722d9b758613a91c0067' 'model_int8.onnx_data' \
      '0cf3b235ef125658015a6c8026a5fe6f28088bf825101a990ec18a10200feef4' 'tokenizer.json' \
      > /tmp/SHA256SUMS; \
    sha256sum -c /tmp/SHA256SUMS; \
    rm -f /tmp/SHA256SUMS

# 阶段 1：构建 Go 二进制
FROM golang:1.25.1-alpine AS build

WORKDIR /src
# 国内源：apk 走阿里云镜像。
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
 && apk add --no-cache git ca-certificates

# 国内源：Go module 走 goproxy.cn；go.sum 已锁定，关闭校验库以避免访问 sum.golang.org。
# 可用 --build-arg GOPROXY=/GOSUMDB 覆盖。
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=off
ENV GOPROXY=${GOPROXY} GOSUMDB=${GOSUMDB}

# 先拷依赖清单，利用层缓存（relaykit 是本地 replace 的嵌套模块，需要一并拷入）
COPY go.mod go.sum ./
COPY relaykit/go.mod relaykit/go.sum ./relaykit/
RUN go mod download

COPY . .
# 用阶段 0 的真实控制台覆盖仓库里的占位页
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -X github.com/zzyyyds88/PowerBarRations/common.Version=${VERSION} -X github.com/zzyyyds88/PowerBarRations/common.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o /out/pbr .

FROM alpine:3.20

# 国内源：apk 走阿里云镜像。
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
 && apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 -h /app pbr \
 && mkdir -p /data && chown -R pbr:pbr /data /app

WORKDIR /app
COPY --from=build /out/pbr /app/pbr
# 内置脱敏模型：默认路径指向镜像内只读副本；部署方可用 -e PBR_PII_MODEL_DIR=...
# 改回数据卷或自定义目录。模型启用开关仍默认关闭（privacy-spec §3.2）。
COPY --from=pii-model --chown=pbr:pbr /out /app/privacy/astrlink-guard/7216b6a3b897

USER pbr
VOLUME ["/data"]

ENV PORT=5700 \
    SQLITE_PATH="/data/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
    PBR_PII_MODEL_DIR="/app/privacy/astrlink-guard/7216b6a3b897" \
    GIN_MODE=release

EXPOSE 5700

# SESSION_SECRET / CRYPTO_SECRET 必须由部署方提供，不给默认值（见 README）。
ENTRYPOINT ["/app/pbr"]
