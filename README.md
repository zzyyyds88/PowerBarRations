# PowerBarRations

单用户、自用的 LLM 聚合网关：**一个二进制、一个 SQLite、一条管道**。
请求里的 `model` 就是路由键——渠道声明自己提供哪些模型，网关对每个模型名自动按渠道
优先级排成故障链（上游1的模型1 → 上游2的模型1），零配置即可路由；显式车道只是可选的
覆盖层（自定义顺序、成员改名、把不同上游的不同模型名池化到一个名字下）。

对下游协议零破坏：存量车道名与 `/v1/*` 协议、错误语义一律不变，下游只改 `base_url`。

- 设计基线（架构与取舍）：[`docs/design-v1.md`](docs/design-v1.md)
- 管理 API 契约 + AI 调用手册：[`docs/api-spec-v1.md`](docs/api-spec-v1.md)
- 路由与故障转移：[`docs/routing-spec-v1.md`](docs/routing-spec-v1.md)
- 令牌与认证：[`docs/token-spec-v1.md`](docs/token-spec-v1.md)
- 控制台规格：[`docs/ui-spec-v1.md`](docs/ui-spec-v1.md)
- 关键取舍记录：[`docs/adr/`](docs/adr/)
- 协作规则：[`AGENTS.md`](AGENTS.md)

---

## 1. 快速开始（Docker）

```bash
export PBR_SESSION_SECRET="$(openssl rand -hex 32)"
export PBR_CRYPTO_SECRET="$(openssl rand -hex 32)"
docker compose up -d --build
# 默认 HTTPS + 自签证书（首次启动自动生成到数据卷 /data/tls）；自签需 -k 或导入受信任证书
curl -sk https://127.0.0.1:5700/api/v1/health
```

首次使用按顺序做三件事（**全部是 HTTP 调用，不需要改文件、不需要读库**）：

```bash
BASE=https://127.0.0.1:5700        # 自签证书：下面所有 curl 请加 -k（或先导入受信任证书）

# 1) 设登录口令 → 响应里的一次性管理密钥（Base64(SHA256(口令))，只在这里出现）
curl -s -X POST $BASE/api/v1/setup -H 'Content-Type: application/json' \
  -d '{"password":"<长随机串，建议 ≥32 字符>"}'
export ADMIN_KEY='<上一步返回的 admin_key>'

# 2) 建渠道：声明它提供哪些模型，声明即可路由
curl -s -X PUT $BASE/api/v1/channels/vendor-a \
  -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{"type":"openai","base_url":"https://vendor.example/v1",
       "key":"__INJECT_BY_OPERATOR__","priority":20,
       "models":["model-1","model-2"],"enabled":true}'

# 3) 建客户端密钥（明文只回显一次）
curl -s -X POST $BASE/api/v1/keys -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' -d '{"name":"client-a"}'
```

下游把 `base_url` 指到 `$BASE/v1` 即可。

### 不用 Docker（手动 docker run / 裸机）

```bash
go build -o pbr .
PORT=5700 \
SQLITE_PATH="./pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
SESSION_SECRET="$(openssl rand -hex 32)" CRYPTO_SECRET="$(openssl rand -hex 32)" \
./pbr
```

等价的 `docker run`：

```bash
docker run -d --name pbr -p 5700:5700 \
  -e SESSION_SECRET="$(openssl rand -hex 32)" \
  -e CRYPTO_SECRET="$(openssl rand -hex 32)" \
  -v pbr-data:/data powerbar-rations:latest
```

`PBR_BIND=127.0.0.1` 可把监听收紧到本机（默认 `0.0.0.0`，对局域网开放）。
`PBR_ADMIN_KEY` 可直接指定管理密钥（无头/AI 部署），此时不需要口令派生。

---

## 2. 车道与路由

### 2.1 路由键 = 模型名

```
渠道/vendor-a（priority 20）—— model-1, model-2
渠道/vendor-b（priority 10）—— model-1, model-2

请求 model-1  →  vendor-a 的 model-1  →（失败）→  vendor-b 的 model-1
```

- 渠道声明 `models` 后，这些模型名**立刻可用**，不需要先建任何对象。
- 排序依据是渠道 `priority`，**数字大者优先**；相同则按渠道 id 定序。
- 没有任何渠道声明该模型 → `503 No available channel for model <X>`，
  与"上游全挂"**同形**，下游无需分支。
- 请求名带思考后缀（如 `model-1-thinking`）时，原文未命中会按基座既有规则归一化再匹配一次，
  响应里的 `model` 仍是请求原文。

### 2.2 显式车道（可选覆盖层）

需要自定义顺序、成员改名、或池化不同上游的不同模型名时才建：

```bash
curl -s -X PUT $BASE/api/v1/lanes/lane-1 -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' -d '{
  "enabled": true, "mode": "failover",
  "members": [
    {"channel":"vendor-a","upstream_model":"real-name-a","priority":20},
    {"channel":"vendor-b","upstream_model":"real-name-b","priority":10,"weight":3}
  ]}'
```

`mode` 四种：`failover`（默认，按优先级降序取首个可用）、`manual`（只用手工指定的
`active_member`，不可用即无可用）、`weighted`（按 `weight` 加权随机）、`round_robin`（环形轮询）。
四者共用同一套冷却/熔断/超时与尝试预算。

### 2.3 六键与超时算术

| 键 | 默认 | 含义 |
|---|---|---|
| `member_max_attempts` | 2 | 单成员含首发的总尝试次数 |
| `member_retry_interval_seconds` | 3 | 同成员相邻尝试间隔 |
| `member_non_stream_response_timeout_seconds` | 120 | 非流式整响应超时 |
| `member_stream_first_event_timeout_seconds` | 30 | 流式首个事件超时 |
| `member_cooldown_seconds` | 60 | 成员耗尽尝试后被跳过的秒数 |
| `member_affinity_seconds` | **0** | 切换成功后保持当前成员的秒数（默认不粘滞） |

**超时算术（排障必用）**：最坏判定耗时 = 成员数 × attempts ×（超时 + 重试间隔）；
端到端 = 客户端重试次数 × 该数。嫌慢时正确旋钮是 response timeout，而不是砍 attempts。

### 2.4 冷却、熔断与亲和

- **冷却**：成员尝试预算耗尽 → 跳过 `member_cooldown_seconds`；到期只放行**一个**探测请求。
- **熔断**：三态 `closed/open/half_open`，按失败计分打开——硬故障权重 1.0、5xx 0.6、
  429 仅 0.2（持续限流不会像硬故障那样快速打开）。退避期到 → 半开放一个探测；
  成功即复通并写恢复事件；重复打开按 `open_seconds × 2^k` 指数退避（设上限）。
- **错误分类**：欠费关键词优先于状态码；`client_error`（请求本身不合法）不冷却、不换人、
  不记熔断分——否则一个坏请求会把健康成员打冷。
- **亲和**：默认 0 = 不做粘滞，高优先级成员恢复后立刻切回；需要压抖动时按车道显式配置。
- **自动恢复默认开启**：`AutomaticEnableChannelEnabled` 默认为 `true`，**与旧系统相反**
  （旧系统默认关闭）。原因：关闭时"自动禁用"是单向操作——渠道被自动禁用后没有任何
  路径能改回 `enabled`，而成员链只取 `enabled` 渠道，该成员会从车道上静默消失。
  开关名保留旧口径以便现网对照，可在设置页或 `PUT /api/v1/system/options` 关闭。
- **全部成员耗尽 → 立即 503 快抛**，不轮询等待、不静默兜底、不跨车道逃逸。
  503 只代表"没有可用渠道"：基座继承的**本机资源过载守卫默认关闭**（`PERFORMANCE_MONITOR_ENABLED=false`），
  避免把"网关所在机器忙"误判成"路由全挂"；需要时可用该环境变量打开并配 `PERFORMANCE_*_THRESHOLD`。

阈值与时长经 `PUT /api/v1/system/options` 配置；`POST /api/v1/lanes/{name}/circuits/reset` 手动清除。
**运行态是进程内的，重启清空**（冷却/熔断/亲和都会重置）。

---

## 3. 访问与密钥

| | 管理凭据 | 客户端密钥 |
|---|---|---|
| 面向 | 控制台登录 + `/api/v1/*` | `/v1/*` 模型流量 |
| 生成 | 由登录口令派生：`Base64(SHA256(口令))` | 服务端随机 `pbr-<32 位 base62>` |
| 存储 | 只存 `sha256(管理密钥)` | 只存 `sha256(明文)` + 展示前缀 |
| 默认权限 | 全量 | **允许全部车道**，只能显式拒绝 |

- 管理密钥**不能**用于模型面，客户端密钥**不能**用于管理面。
- 口令丢失：`PBR_ADMIN_KEY` 环境变量直接指定，或清掉库内凭据回到未初始化状态。
- 口令变更即管理密钥变更，旧密钥立即失效。登录接口对连续失败做指数退避（上限 30s）。
- 局域网内是明文 HTTP，且派生规则（单次 SHA256、无盐）抗离线爆破弱——
  **口令必须是长随机串**（建议 ≥32 字符）。

---

## 4. 与旧系统的差异

| 项 | 旧（两代网关） | PowerBarRations |
|---|---|---|
| 层数 | 路由层 + 厂商层两层 | 一层：模型名即路由键 |
| 模型映射 | 厂商层 `model_mapping` 表 | 成员级 `upstream_model`，映射表消失 |
| 限流误判 | 429 与 401 一视同仁 | 分类处理，429 不误伤 |
| 全挂行为 | 轮询等待（请求悬挂） | 503 快抛（下游可分类） |
| 自愈 | 只禁不通，无半开 | 熔断三态 + 半开自动复通 |
| 模式 | manual / failover | failover / manual / weighted / round_robin |
| 准入 | 白名单（忘配就 400） | 默认放行 + 显式拒绝 |
| 账号 | 多用户 + 计费 | 单用户、无计费（成本只折算展示） |
| 运维 | 改文件、拷库、抓前端 | 全部 HTTP API + OpenAPI |

---

## 5. 运维

```bash
GET  /api/v1/health                     # 存活（免鉴权）
GET  /api/v1/models                     # 全部可路由模型名
GET  /api/v1/routes/{model}             # 某模型的成员链（排障用）
GET  /api/v1/lanes/{name}/health        # 冷却/熔断/亲和快照（含带时间戳事件）
POST /api/v1/lanes/{name}/probe         # 逐成员真实探活
GET  /api/v1/logs?success=false         # 元数据日志（含 attempts 链）
GET  /api/v1/stats?granularity=hour&group_by=lane
GET  /api/v1/export                     # 导出配置（不含密钥明文与哈希）
POST /api/v1/import?dry_run=true        # 导入前先看 diff
GET  /api/v1/openapi.json               # 完整契约
```

- **备份**：备份 Docker 卷里的 `pbr.db`（含 `-wal`/`-shm`）即可；也可用 `/export` 导出配置。
- **日志**：只存元数据（车道、成员、尝试链、token 数、耗时、折算金额），
  不存请求/响应正文，不存密钥明文，不存余额。
- **升级**：`docker compose up -d --build`；数据库结构由启动时的迁移自动补齐。
- **验收证据**：每一波的实测结论在 [`verify/`](verify/)（`w1`/`w2`/`w3`/`w5`/`deploy` 各有 README）。

### 5.1 HTTPS（自签与导入证书）

默认（Docker）**开启 HTTPS**：`TLS_ENABLED=true`，首次启动若无证书会自动生成自签证书到
`TLS_DIR`（容器内 `/data/tls`，随数据卷持久化）。裸机运行需显式开启：

```bash
# 方式一：自动生成自签证书（SAN 含本机名/回环/本机 IP）
TLS_ENABLED=true TLS_DIR=./tls ./pbr

# 方式二：指定已有证书（导入）
TLS_ENABLED=true TLS_CERT_FILE=/path/fullchain.pem TLS_KEY_FILE=/path/privkey.pem ./pbr

# 离线先生成自签证书 / 查看
./pbr tls gen --out ./tls --host pbr.example.com,192.168.1.10 --days 825
./pbr tls show --cert ./tls/cert.pem
```

- 自签证书浏览器/客户端会告警：把 `cert.pem` 导入系统信任，或客户端加 `-k`（curl）。控制台在 HTTPS 下会显示安全标记。
- **运行期导入自有证书**（PEM，LE/商业证书均可）：`PUT /api/v1/tls/certificate`
  body `{"cert_pem":"-----BEGIN CERTIFICATE-----...","key_pem":"-----BEGIN PRIVATE KEY-----..."}`；
  写盘并在**下次握手立即生效，无需重启**。
- 查看/重新自签：`GET /api/v1/tls`；`POST /api/v1/tls/self-signed` body `{"hosts":[...],"days":825}`。

---

## 6. 上游参考

`reference/` 为只读参考，不入本仓库（见 `.gitignore`）：

| 目录 | 来源 | revision | 用途 |
|---|---|---|---|
| `new-api` | QuantumNous/new-api | `main@04c64734` | 转发管道与厂商适配层复用来源（W0 已迁入为基座） |
| `octopus-bestrui` | bestruirui/octopus | `e7a1455`（钉线上镜像） | 线上路由层真实上游；车道/冷却/亲和语义基准 |
| `octopus-hureru-fork` | Hureru/octopus | `dev@0e1a7ee` | 仅熔断器三态算法的设计参考（schema 已分叉） |

## 7. 许可

本项目是 [new-api](https://github.com/Calcium-Ion/new-api) 的二次开发，保留其 AGPL 版权头、
`LICENSE`、`NOTICE` 与 `THIRD-PARTY-LICENSES.md`。品牌可替换，版权不可替换。
控制台蓝本为 [octopus](https://github.com/bestruirui/octopus)，另附其许可清单。
