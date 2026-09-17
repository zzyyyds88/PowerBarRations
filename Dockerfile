# PowerBarRations —— 多阶段构建
#
# 阶段 1：构建 Go 二进制（控制台产物由 web/dist 提供，见 W4）。
# 阶段 2：最小运行镜像，非 root 用户，数据卷落在 /data。
# 阶段 0：构建控制台（产物供阶段 1 的 go:embed 使用）
FROM node:22-alpine AS web

WORKDIR /src/web
RUN corepack enable
# 国内源：pnpm/corepack 走 npmmirror，避免拉包超时。
ENV COREPACK_NPM_REGISTRY=https://registry.npmmirror.com \
    npm_config_registry=https://registry.npmmirror.com
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

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
      -ldflags "-s -w -X pbr/common.Version=${VERSION} -X pbr/common.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o /out/pbr .

FROM alpine:3.20

# 国内源：apk 走阿里云镜像。
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
 && apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 -h /app pbr \
 && mkdir -p /data && chown -R pbr:pbr /data /app

WORKDIR /app
COPY --from=build /out/pbr /app/pbr

USER pbr
VOLUME ["/data"]

ENV PORT=5700 \
    SQLITE_PATH="/data/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
    GIN_MODE=release

EXPOSE 5700

# SESSION_SECRET / CRYPTO_SECRET 必须由部署方提供，不给默认值（见 README）。
ENTRYPOINT ["/app/pbr"]
