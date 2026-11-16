# 部署验收证据（goal-prompt §六 G）

## 目标

从零用 `docker-compose.yml` 起容器 → 首启设口令 → 建渠道/客户端密钥 → 跑通一发真实请求 →
重启容器数据仍在；镜像可重建，`docker run` 说明可照抄，卷挂载与备份步骤实测。

## 复现

```bash
bash verify/deploy/smoke.sh
```

脚本用**独立 compose 项目**（`pbr-verify`）+ 独立命名卷 + 端口 6796，假上游只绑到 docker 网桥
（`172.17.0.1`），不占 LAN；结束时 `down -v` 清理干净。全程不触碰现网在跑的网关容器与数据卷。

## 结果（run-20260915-031244.log，PASS=11 FAIL=0）

| 检查项 | 命令 | 结论 | 证据 |
|---|---|---|---|
| 镜像可重建 | `docker compose -p pbr-verify up -d --build` | 多阶段构建成功；构建期 Go 模块代理可用 `--build-arg GOPROXY` 覆盖 | 日志第 1–95 行 |
| 健康检查 | `GET /api/v1/health` | `{"status":"ok",...}` | 日志第 97 行 |
| 首启设口令 | `POST /api/v1/setup` | 返回一次性管理密钥 | 日志第 99 行 |
| 建渠道/车道/密钥（容器内） | `PUT /channels`、`PUT /lanes`、`POST /keys` | 均 200 | 日志第 100–102 行 |
| 跑通真实请求 | `POST /v1/chat/completions` | 返回假上游内容，响应 `model` 回填请求名，`X-Served-By: channel=1:deploy-channel` | 日志第 104–106 行 |
| 重启数据仍在 | `docker restart pbr` 后回读 | 客户端密钥仍在、前缀未变、原密钥仍可转发、模型路由仍在 | 日志第 108–111 行 |
| 运行态按设计重启清空 | `GET /lanes/deploy-model/health` | 重启后无残留冷却 | 日志第 113 行 |
| 现网容器未被触碰 | 前后比对 `docker ps` | 容器集合未变 | 日志第 117 行 |
| 清理 | `down -v` | 本次项目容器数为 0 | 日志第 119 行 |

## 备份与回滚（实测过的步骤）

```bash
# 备份：卷里的 SQLite（含 -wal/-shm）
docker run --rm -v pbr-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/pbr-data-$(date +%F).tgz -C /data .

# 回滚：恢复卷后重启
docker run --rm -v pbr-data:/data -v "$PWD":/backup alpine \
  sh -c 'rm -rf /data/* && tar xzf /backup/pbr-data-<日期>.tgz -C /data'
docker restart pbr
```

配置级备份/回滚另有一条不依赖容器的路径：`GET /api/v1/export` 导出，
`POST /api/v1/import?dry_run=true` 先看 diff 再落地。

## 边界与说明

- 构建容器需要能拉到 Go 模块：本机不可达官方代理，验收脚本用 `PBR_GOPROXY="$(go env GOPROXY)"`
  覆盖（默认值仍是官方代理）。这是**环境相关**的构建参数，不影响产物。
- 验收期间观察到现网 5 个容器在本次操作前约 6 分钟自行重启过（`StartedAt` 早于本脚本首次运行），
  与本验收无关；脚本本身只操作 `pbr-verify` 项目，且退出时清理干净。
- 控制台尚未迁入（W4），镜像里的 `web/dist` 目前是占位页。
