# 生产发布（deploy/）

> 本目录是 PowerBarRations 的生产发布体系：本地构建 → SSH 传输镜像 → 生产机 `docker load` → 切换容器。
> 生产凭据只保存在本机未跟踪的 `production.env`；仓库只保留 `*.example` 模板。

首次使用（真实文件均已被 `.gitignore` 排除）：

```bash
cp deploy/production.env.example deploy/production.env
cp deploy/test.env.example deploy/test.env
# 填入真实主机、口令与密钥；不要提交这两个文件
```

## 0. 硬性约定（2026-11-06）

1. **构建只在本机做**（构建机地址见本地 `production.env`）；生产机只负责运行 Docker，**不留源代码**。
2. **替换生产必须逐次获得用户明确许可**。脚本默认只构建；`--deploy` 必须同时带
   `--approved "<用户批准说明>"`，否则拒绝执行。
3. 当前 `stop-first` 发布会中断在途请求；用户已决定**不引入代理/蓝绿**（2026-11-06），
   因此部署还必须显式 `--allow-downtime`，表示已批准该中断。**禁止以“标签对齐/补记录”为由发布。**
4. 构建完成后通过 SSH 把镜像传到生产机（`docker save | gzip` → `ssh` → `docker load`）。
5. 构建必须注入版本号，部署后 `GET /api/v1/version` 可直接核对线上版本。
6. 版本号由发布脚本管理，规则见 §2。
7. 部署凭据（SSH、SESSION/CRYPTO secret）只保存在本机未跟踪的 env 文件；禁止入库。

### 事故记录（2026-11-06）

- 当天连续两次发布（`v1.0.0`、`v1.0.1`）均采用 `docker stop --time 130` → `rm` → `run`。
  旧容器有在途流式请求时，停止阶段会等待排空，期间 6700 已不可用、新容器尚未启动；
  `v1.0.1` 实测中断约 **1.5 分钟**，`v1.0.0` 同样中断。
- 第二次发布仅为了对齐镜像 revision 标签，**没有业务必要性**，属于错误决策。
- 整改：默认只构建；部署需用户逐次批准。用户明确**不引入代理/蓝绿**（2026-11-06），
  维持直接端口；每次升级仍会有 `stop-first` 的短暂中断，批准即视为知悉。

## 1. 拓扑

| 角色 | 地址 | 说明 |
|---|---|---|
| 构建机 | `<build-host>` | 仓库根目录，Docker 29.x |
| 生产 | `<prod-host>`（`<prod-hostname>`） | 仅 Docker：容器 `pbr` + 数据目录 |

生产容器参数（脚本已固化）：

```
名称      pbr
镜像      powerbar-rations:<version>
端口      6700:5700
数据      /srv/docker/data/pbr -> /data（bind mount，升级不删）
重启      unless-stopped
停止宽限  --stop-timeout 130（≥ 服务 120s 优雅关闭窗口）
环境      SESSION_SECRET / CRYPTO_SECRET / PBR_BIND=0.0.0.0 / GIN_MODE=release
健康检查  wget /api/v1/health（30s 间隔）
```

> 生产机 `/root/PowerBarRations` 已于 2026-11-06 删除；忽略文件（日志）归档到
> `/srv/docker/archive/`。后续不再在生产机构建或保留源码。

## 2. 版本号管理

- 格式：`vMAJOR.MINOR.PATCH`（例：`v1.0.0`）。
- 初始基线：`v1.0.0`（2026-11-06 首次按本流程部署）。
- 之后每次生产部署默认 **PATCH +1**（脚本自动完成）。
- 只有不兼容变更/重大功能才手动升 MAJOR/MINOR：
  `./deploy/release.sh --version v1.1.0`。
- 上次成功部署的版本记录在 `deploy/version`；构建/部署失败不会写入，重试沿用同一版本。
- 部署成功后追加 `deploy/deploy-history.log`（已 gitignore）：
  时间、版本、commit、镜像 ID、上一版本。
- **验收硬标准**：`GET /api/v1/version` 的 `version` 必须等于本次版本号；
  脚本内置校验，不一致会自动回滚。

## 3. 日常命令

```bash
# 构建（默认行为，不碰生产）
./deploy/release.sh --build-only
./deploy/release.sh --version v1.1.0 --build-only

# 替换生产（必须逐次获得用户明确许可；蓝绿落地前还需 --allow-downtime）
./deploy/release.sh --deploy --approved "用户在 2026-11-06 20:30 批准发布 v1.1.0" --allow-downtime

./deploy/rollback.sh                    # 列出生产可用镜像
./deploy/rollback.sh powerbar-rations:rollback-YYYYmmdd-HHMMSS   # 回滚
```

产物：`deploy/images/powerbar-rations-<version>.tar.gz`（本地保留最近 3 个，已 gitignore）。
生产旧镜像：回滚标签 `powerbar-rations:rollback-<时间>`，
并在 `/srv/docker/images/` 留一份 `docker save` 归档。

## 4. 发布流程（脚本内部）

1. 校验仓库干净；计算版本号。
2. **审批闸口**：`--deploy` 必须带非空 `--approved`；蓝绿未落地时还必须
   `--allow-downtime`，否则在构建前直接拒绝。
3. 本机 `docker build --build-arg VERSION=<vX.Y.Z>`，打标签 `<version>` 与 `prod`。
4. `docker save | gzip` 到 `deploy/images/`。
5. SSH 预检生产；给运行中镜像打回滚标签并归档。
6. 传输镜像 → 生产 `docker load`。
7. `docker stop --time 130 pbr` → `docker rm` → `docker run`（参数见 §1）。
   **这一步会中断在途请求，是已知缺陷**，蓝绿方案待用户选定后替换。
8. 本机轮询 `http://<prod-host>:6700/api/v1/version` 最多 90s，
   校验 `version == 本次版本` 且接口可用。
9. 失败：自动用切换前镜像重建容器；数据卷不受影响。
10. 成功：写 `deploy/version`、追加 `deploy/deploy-history.log`（含批准说明）。

## 5. 回滚

```bash
./deploy/rollback.sh
./deploy/rollback.sh powerbar-rations:rollback-20261106-120000
```

- 回滚只换镜像，数据卷不动；不会回写 `deploy/version`。
- 若历史版本已从生产删除，可从 `deploy/images/powerbar-rations-<version>.tar.gz`
  重新传输加载。

## 6. 数据备份

生产机没有源码，备份入口是镜像内的 `pbr backup`（SQLite 在线快照 + `quick_check`）：

```bash
# 在线备份：容器内执行，产物先落在 /data/backups，再拷到宿主机备份目录。
docker exec pbr /app/pbr backup --out /data/backups/pbr-$(date -u +%Y%m%dT%H%M%SZ).db
docker cp pbr:/data/backups/pbr-<时间>.db /srv/docker/backup/

# 恢复（必须先停容器；不要热 tar，也不要把运行中的 -wal/-shm 当备份）
docker stop pbr
install -m 600 /srv/docker/backup/pbr-<时间>.db /srv/docker/data/pbr/pbr.db
rm -f /srv/docker/data/pbr/pbr.db-wal /srv/docker/data/pbr/pbr.db-shm
docker start pbr
```

恢复后确认 `/api/health` 为 `ok`、`db_last_write_error` 为空、日志能继续写入。
配置级备份：`GET /api/v1/export`；导入先 `POST /api/v1/import?dry_run=true`。

聚合 outbox 排障：

```bash
docker exec pbr /app/pbr outbox status            # pending/dead/最早时间/最近错误
docker exec pbr /app/pbr outbox retry --dead      # 磁盘/数据库临时故障修复后重试死信
docker exec pbr /app/pbr outbox discard --dead --yes  # payload 损坏时丢弃；随后可 rebuild
```

## 7. 安全纪律

- `production.env` / `test.env` 已被 gitignore，且**从未进入过公开发布的 Git 历史**；请勿把它们或其内容带入任何提交。
- 不要在其他公开渠道（issue、日志、聊天）复制 `production.env` 内容。
- 删除生产源码前已确认：工作区干净、HEAD 已在 GitHub。
- GitHub 推送：`origin` 走 SSH；本机私钥路径由本机 Git 配置决定，私钥文件不要复制进仓库。

## 8. 本机测试环境

- 容器 `pbr-test`，镜像与数据目录见本地 `deploy/test.env`。
- **不随开机自启**（`--restart no`）。
- **存续 30 分钟**（`test.env` 的 `PBR_TEST_TTL_MINUTES`）：每次 `refresh`/`up` 重新计时；
  到期由后台 watchdog 自动停止并删除容器，数据保留。
- 登录口令与配置见本地 `deploy/test.env`（已 gitignore，禁止提交）。
- 数据来源：生产 SQLite 在线快照（只读，不停服、不改生产）：

```bash
./deploy/test-env.sh refresh   # 拉最新快照 + 重建测试环境 + 重置口令
./deploy/test-env.sh up        # 启动已有测试环境
./deploy/test-env.sh down      # 停容器，保留数据
./deploy/test-env.sh status    # 健康/版本/初始化状态
```

- 测试环境复用生产 `CRYPTO_SECRET`（否则复制来的渠道密钥无法解密）；
  `SESSION_SECRET` 用测试独立值。
- **注意**：快照包含生产渠道的真实上游密钥，测试环境发起请求会打到真实上游；
  不要用它做压测或批量造请求。

## 9. 历史

| 日期 | 事件 |
|---|---|
| 2026-11-06 | 建立本发布体系；生产源码删除；首次按版本号部署，随后对齐发布体系 |
| 2026-11-05 | 上一版生产镜像未注入版本号（health 显示 dev），促成版本注入约定 |
