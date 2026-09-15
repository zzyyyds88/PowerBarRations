# PowerBarRations 管理 API 契约 v1

> 规范性文件，从属于 [`design-v1.md`](design-v1.md) §5。
> 目标读者：实现者（Go 后端 / 前端）与**调用方 AI**。
> 本文中的 `$PBR` 代指网关地址（如 `http://127.0.0.1:5700`），`$ADMIN_KEY` / `$CLIENT_KEY` 为凭据，**均为占位符**。

---

## 1. 认证

| 面 | 前缀 | 凭据 |
|---|---|---|
| 管理面 | `/api/v1/*` | **二选一**：`Authorization: Bearer <管理密钥>`（AI/脚本）或 HttpOnly 会话 Cookie（浏览器，登录后自动携带） |
| 模型面 | `/v1/*` | `Authorization: Bearer <客户端密钥>`（同时兼容 `X-Api-Key`） |

**管理密钥由登录口令派生**（无账号体系，详见 [`token-spec-v1.md`](token-spec-v1.md) §2）：

```
管理密钥 = Base64( SHA256( 登录口令 ) )
```

- 首次启动时未初始化，必须先 `POST /api/v1/setup` 设置口令；否则除 `/health`、`/version`、`/setup*`、`/auth/login` 外一律 `409`/`401`。
- 服务端只存 `sha256(管理密钥)`；口令与管理密钥明文都不落库。
- **浏览器走会话 Cookie**：`POST /api/v1/auth/login` 成功后签发 HttpOnly Cookie，控制台**不再把管理密钥写进 localStorage**；`POST /api/v1/auth/logout` 清除。
- **AI/脚本走 Bearer**：管理密钥 = `Base64(SHA256(登录口令))`，由调用方自行计算，无需人工复制（见 token-spec §2.1）。
- 两条通道等价：任一通过即鉴权成功。口令变更后旧 Cookie 与新签名不匹配，自动失效。
- **监听 `0.0.0.0` 对局域网开放**（模型面与管理面同端口），凭凭据鉴权、不做来源限制（业主决定）；可用 `PBR_BIND=127.0.0.1` 收紧。局域网为明文 HTTP，故口令须为长随机串。
- 可用 `PBR_ADMIN_KEY` 环境变量显式覆盖（无头/AI 部署）；客户端密钥只存哈希，明文仅在创建/轮换响应出现一次。

**认证失败响应**

```json
HTTP/1.1 401 Unauthorized
{ "error": { "code": "unauthorized", "message": "missing or invalid bearer token" } }
```

---

## 2. 通用约定

1. **全量幂等写**：`PUT /api/v1/{resource}/{name}`，body 为完整对象，upsert 语义；同名重复提交结果一致。
2. **写后回读**：响应体是**落库后重新读取**的最终状态。实现必须在 handler 内 re-read 再返回。
3. **dry-run**：任何 `PUT/POST/DELETE` 支持 `?dry_run=true`，返回将发生的 diff 而不落库：

   ```json
   { "dry_run": true, "valid": true,
     "diff": { "lanes": { "add": ["lane-beta"], "update": [], "remove": [] } } }
   ```
4. **分页**：列表用 cursor。请求 `?limit=50&cursor=<opaque>`，响应 `{"items":[...], "next_cursor":"<opaque|null>"}`；`limit` 上限 200，默认 50。
5. **时间**：RFC3339 UTC（`2026-09-14T12:00:00Z`）。
6. **审计**：所有变更写 `audit_logs`（`ts, actor, action, resource, name, before_digest, after_digest, dry_run`），只记元数据，不记密钥与请求正文。
7. **幂等键（可选）**：请求头 `Idempotency-Key` 可用于重试去重。
8. **管理面不做全局限流**：PBR 是自用单用户网关，管理面 `/api/*` 与静态控制台**默认关闭**基座遗留的全局限流
   （`GLOBAL_API_RATE_LIMIT_ENABLE`/`GLOBAL_WEB_RATE_LIMIT_ENABLE` 默认 `false`）。控制台一次页面加载会并发多个
   管理请求，基座默认的 360/120 次窗口（继承自 new-api 的多租户公网假设）会把正常浏览打成 429。
   限流只作用于**客户端密钥与模型面**（`rate_limit_rpm`/`max_concurrency`，见 token-spec §3.4）与登录失败退避。
   公网部署如确需，可显式打开上述环境变量。

---

## 3. 错误模型

```json
{ "error": { "code": "lane_not_found", "message": "lane 'lane-alpha' not found", "hint": "GET /api/v1/lanes" } }
```

| HTTP | code | 触发场景 |
|---|---|---|
| 400 | `invalid_request` | JSON 解析失败 / 字段类型错 |
| 400 | `validation_failed` | 字段校验失败（`details` 给出字段级原因） |
| 401 | `unauthorized` | 密钥缺失或错误 |
| 403 | `forbidden_scope` | 客户端密钥访问了被 deny 的车道（仅模型面） |
| 404 | `lane_not_found` / `channel_not_found` / `key_not_found` / `log_not_found` | 对象不存在 |
| 409 | `conflict` | 唯一名冲突 / 乐观锁冲突 / 车道名与成员别名冲突 |
| 409 | `not_initialized` | 未设置登录口令就调用管理接口（先 `POST /api/v1/setup`） |
| 422 | `lane_has_no_members` | 启用车道但无成员 |
| 422 | `member_channel_missing` | 成员引用的渠道不存在 |
| 422 | `invalid_mode` | 模式不在 failover/manual/weighted/round_robin |
| 502 | `upstream_error` | 探活时上游返回错误 |
| 503 | `no_available_member` | 模型面：车道无可用成员（body 形态见 §6.1） |

`code` 为稳定字符串，**调用方 AI 应据此分支**，不要解析 `message`。

---

## 4. 资源对象

### 4.1 Channel

```json
{
  "name": "channel-a",
  "type": "openai",
  "base_url": "https://vendor.example/v1",
  "priority": 1,
  "models": ["model-1", "model-2", "model-3"],
  "param_override": {},
  "enabled": true,
  "proxy": "",
  "key_set": true,
  "key_prefix": "sk-abcd",
  "created_at": "2026-09-14T12:00:00Z",
  "updated_at": "2026-09-14T12:00:00Z"
}
```

- `models`：本渠道提供的**路由键**（模型名）。声明后这些模型名即刻可路由，无需再建对象（见 [routing-spec-v1.md](routing-spec-v1.md) §1.1）。
- `priority`：隐式成员链的排序依据，数字大者优先。
- **写**：body 可含 `"key": "<明文>"`；**读**：一律不含 `key`，只有 `key_set` 与 `key_prefix`。`PUT` 时若省略 `key` 则保留原值。
- `type` 取值见 `GET /api/v1/capabilities` 的 `adapters`。

### 4.2 Lane

```json
{
  "name": "lane-alpha",
  "enabled": true,
  "mode": "failover",
  "config": {
    "member_max_attempts": 2,
    "member_retry_interval_seconds": 3,
    "member_non_stream_response_timeout_seconds": 120,
    "member_stream_first_event_timeout_seconds": 30,
    "member_cooldown_seconds": 60,
    "member_affinity_seconds": 0
  },
  "members": [
    { "channel": "channel-a", "upstream_model": "model-x", "public_alias": "", "priority": 1, "weight": 1 },
    { "channel": "channel-b", "upstream_model": "model-x", "public_alias": "", "priority": 2, "weight": 3,
      "overrides": { "member_max_attempts": 1 } }
  ]
}
```

- 成员在请求/响应中用 `channel`（渠道名）引用，不暴露内部 ID。
- **`priority` 数字大者优先**（与渠道 `priority` 一致），示例中的 1/2 仅为占位。
- `overrides` 为成员级六键覆盖，省略字段表示继承车道。
- **显式车道**才需要创建；绝大多数模型走**隐式车道**（渠道 `models` 声明自动成链），不在 `GET /lanes` 中列出，见 §5.7。

### 4.3 ClientKey

```json
{
  "name": "client-a",
  "enabled": true,
  "lane_policy": { "mode": "all", "allow_lanes": [], "deny_lanes": [] },
  "ip_allowlist": [],
  "rate_limit_rpm": 0,
  "max_concurrency": 0,
  "expires_at": null,
  "notes": "",
  "key_prefix": "pbr-a1b2c3d4",
  "created_at": "2026-09-14T12:00:00Z",
  "updated_at": "2026-09-14T12:00:00Z",
  "last_used_at": null
}
```

- `lane_policy.mode ∈ all | allow`；生效车道 = `(all ? 全部 : allow_lanes) - deny_lanes`。默认 `all` 且不拒绝任何车道（见 [token-spec-v1.md](token-spec-v1.md) §3.2）。
- 创建/轮换响应额外含一次性 `"key": "<明文>"`。

### 4.4 RequestLog（见 §4.5 端点的响应）

```json
{
  "id": 12345,
  "ts": "2026-09-14T12:00:00Z",
  "lane": "lane-alpha",
  "request_model": "lane-alpha",
  "channel": "channel-b",
  "upstream_model": "model-x",
  "key_name": "client-a",
  "success": true,
  "http_status": 200,
  "prompt_tokens": 1200,
  "completion_tokens": 340,
  "cache_read_tokens": 0,
  "cache_write_tokens": 0,
  "reasoning_tokens": 0,
  "ttft_ms": 420,
  "total_ms": 3100,
  "is_stream": true,
  "total_attempts": 2,
  "attempts": [
    { "attempt_num": 1, "member": "channel-a/model-x", "status": "failed",
      "duration_ms": 800, "error_kind": "timeout", "msg": "stream first event timeout" },
    { "attempt_num": 2, "member": "channel-b/model-x", "status": "success", "duration_ms": 2300 }
  ]
}
```

`status ∈ success | failed | cooldown | circuit_break | skipped`。

---

## 5. 端点总表

### 5.1 系统 / 发现

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/health` | 存活与依赖状态（**免鉴权**） |
| GET | `/api/v1/version` | 版本与构建信息（**免鉴权**） |
| GET | `/api/v1/setup/status` | `{initialized}`（免鉴权） |
| POST | `/api/v1/setup` | 首次设置登录口令，**签发会话 Cookie** 并返回派生管理密钥（仅未初始化时可用） |
| POST | `/api/v1/auth/login` | 口令校验通过→**签发会话 Cookie**，并返回派生管理密钥 |
| POST | `/api/v1/auth/logout` | 清除会话 Cookie |
| POST | `/api/v1/auth/password` | 修改口令（会改变管理密钥；旧会话随之失效，当前会话自动续签） |
| GET | `/api/v1/capabilities` | 适配器、模式、能力枚举 |
| GET | `/api/v1/openapi.json` | OpenAPI 3 文档 |
| GET | `/api/v1/system/options` | 全局选项 |
| PUT | `/api/v1/system/options` | 更新全局选项（全量） |

### 5.2 车道

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/lanes` | 列表（cursor） |
| GET | `/api/v1/lanes/{name}` | 详情（含成员） |
| PUT | `/api/v1/lanes/{name}` | 全量 upsert（含成员，按数组顺序即优先级） |
| DELETE | `/api/v1/lanes/{name}` | 删除 |
| PUT | `/api/v1/lanes/{name}/members` | 仅替换成员列表（有序全量） |
| POST | `/api/v1/lanes/{name}/probe` | 逐成员探活 |
| GET | `/api/v1/lanes/{name}/health` | 当前冷却/熔断/亲和快照 |
| POST | `/api/v1/lanes/{name}/circuits/reset` | 清除该车道全部熔断与冷却 |

### 5.3 渠道

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/channels` | 列表 |
| GET | `/api/v1/channels/{name}` | 详情 |
| PUT | `/api/v1/channels/{name}` | 全量 upsert（`key` 只写不读） |
| DELETE | `/api/v1/channels/{name}` | 删除（被车道引用时 409） |
| POST | `/api/v1/channels/{name}/test` | 单渠道探活 |
| POST | `/api/v1/channels/{name}/sync-models` | 从上游拉取模型清单并回写 `models`（`?dry_run=` 只返回差异）。**上游返回空清单时默认拒绝清空**（需 `?force=1`）；被显式车道成员点名的模型仍在引用时返回 409（同样需 `?force=1` 覆盖） |

### 5.4 客户端密钥

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/keys` | 列表 |
| POST | `/api/v1/keys` | 创建（响应含一次性明文） |
| GET | `/api/v1/keys/{name}` | 详情 |
| PUT | `/api/v1/keys/{name}` | 更新（不含明文） |
| DELETE | `/api/v1/keys/{name}` | 删除 |
| POST | `/api/v1/keys/{name}/rotate` | 轮换（响应含新明文一次） |

### 5.5 观测

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/logs` | 过滤：`lane` `channel` `key` `success` `since` `until` `cursor` `limit` |
| GET | `/api/v1/logs/{id}` | 单条（含 attempts 链） |
| POST | `/api/v1/logs/prune?before=&dry_run=` | 按需清理明细日志（`before` 省略则按 `system/options.log_retention_days`，默认 30 天）；只删明细，聚合表长期保留 |
| GET | `/api/v1/stats` | 聚合：`granularity=hour\|day` `from` `to` `group_by=lane\|channel\|key\|model` |
| GET | `/api/v1/route-events` | **SSE**：车道运行态增量（当前成员/探测占用/亲和/冷却表），供控制台实时显示 |
| GET | `/api/v1/audit` | 变更审计 |

### 5.6 配置生命周期

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/export` | 导出完整配置 JSON（不含密钥明文与哈希） |
| POST | `/api/v1/import?dry_run=` | 导入并可选 dry-run，返回 diff |

### 5.7 模型路由（隐式车道）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/models` | 全部可路由模型名：`{model, source: implicit\|explicit, member_count}` |
| GET | `/api/v1/routes/{model}` | 解析该模型的**成员链**（含来源与优先级），用于排障与 UI 展示 |
| PUT | `/api/v1/lanes/{model}` | **把某模型的成员链固化为显式顺序（故障切换）**：车道名 = 模型名，成员按数组顺序即优先级；模型管理页的"优先上游1 → 上游2"即写这里 |

**UI 心智**（design-v1 §7.7）：渠道管理填上游与模型 → 模型管理页为该模型设定成员顺序（写 `PUT /lanes/{model}`）→ 令牌允许该模型。未固化时保持隐式链（渠道声明即自动成链）。

```bash
curl -s $PBR/api/v1/routes/model-1 -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{
  "model": "model-1",
  "source": "implicit",
  "members": [
    { "channel": "channel-a", "upstream_model": "model-1", "priority": 1 },
    { "channel": "channel-b", "upstream_model": "model-1", "priority": 2 }
  ]
}
```

- 未声明该模型的渠道 → `members: []`；模型面请求此时返回 `503 No available channel for model model-1`（与全挂同形）。

---

## 6. 关键请求/响应示例

### 6.1 健康与能力

```bash
curl -s $PBR/api/v1/health
```
```json
{ "status": "ok", "version": "0.1.0", "uptime_s": 12345, "db": "ok" }
```

```bash
curl -s $PBR/api/v1/capabilities -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{
  "api_version": "v1",
  "lane_modes": ["failover", "manual", "weighted", "round_robin"],
  "adapters": ["openai", "anthropic", "gemini", "ollama", "…共 40 家，完整列表见实际实现…"],
  "inbound_formats": ["openai", "openai_responses", "anthropic", "embeddings"]
}
```

### 6.2 建渠道（写后回读）

```bash
curl -s -X PUT $PBR/api/v1/channels/channel-a \
  -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{
    "type": "openai",
    "base_url": "https://vendor.example/v1",
    "key": "__INJECT_BY_OPERATOR__",
    "enabled": true,
    "param_override": {}
  }'
```
```json
{ "name": "channel-a", "type": "openai", "base_url": "https://vendor.example/v1",
  "enabled": true, "key_set": true, "key_prefix": "__IN", "param_override": {},
  "created_at": "2026-09-14T12:00:00Z", "updated_at": "2026-09-14T12:00:00Z" }
```

### 6.3 建车道（含两成员不同渠道不同上游名）

```bash
curl -s -X PUT $PBR/api/v1/lanes/lane-alpha \
  -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{
    "enabled": true,
    "mode": "failover",
    "config": {
      "member_max_attempts": 2,
      "member_retry_interval_seconds": 3,
      "member_non_stream_response_timeout_seconds": 120,
      "member_stream_first_event_timeout_seconds": 30,
      "member_cooldown_seconds": 60,
      "member_affinity_seconds": 0
    },
    "members": [
      { "channel": "channel-a", "upstream_model": "model-x", "priority": 1, "weight": 1 },
      { "channel": "channel-b", "upstream_model": "model-y", "priority": 2, "weight": 1 }
    ]
  }'
```

响应 = 回读后的完整 Lane 对象（§4.2）。`config` 省略时用默认值。

### 6.4 探活（逐成员）

```bash
curl -s -X POST $PBR/api/v1/lanes/lane-alpha/probe \
  -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{
  "lane": "lane-alpha",
  "results": [
    { "channel": "channel-a", "upstream_model": "model-x", "status": "success", "duration_ms": 640 },
    { "channel": "channel-b", "upstream_model": "model-y", "status": "failed",
      "duration_ms": 8000, "error_kind": "timeout", "msg": "non-stream response timeout" }
  ]
}
```

### 6.5 车道健康快照（冷却/熔断/亲和）

```bash
curl -s $PBR/api/v1/lanes/lane-alpha/health -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{
  "lane": "lane-alpha",
  "members": [
    { "channel": "channel-a", "upstream_model": "model-x",
      "circuit": "open", "cooldown_until": "2026-09-14T12:01:00Z",
      "consecutive_failures": 3, "rolling_success_rate": 0.0, "last_error_kind": "timeout" },
    { "channel": "channel-b", "upstream_model": "model-y",
      "circuit": "closed", "cooldown_until": null,
      "consecutive_failures": 0, "rolling_success_rate": 1.0 }
  ],
  "affinity": { "channel": "channel-a", "upstream_model": "model-x", "until": "2026-09-14T12:05:00Z" }
}
```

### 6.6 重置熔断

```bash
curl -s -X POST $PBR/api/v1/lanes/lane-alpha/circuits/reset \
  -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{ "lane": "lane-alpha", "reset": 2 }
```

### 6.7 创建客户端密钥（明文只出现一次）

```bash
curl -s -X POST $PBR/api/v1/keys \
  -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{ "name": "client-a", "enabled": true,
        "lane_policy": { "mode": "all", "allow_lanes": [], "deny_lanes": [] } }'
```
```json
{ "name": "client-a", "key": "pbr-<一次性明文>", "key_prefix": "pbr-a1b2c3d4",
  "enabled": true, "lane_policy": { "mode": "all", "allow_lanes": [], "deny_lanes": [] },
  "created_at": "2026-09-14T12:00:00Z", "last_used_at": null }
```

### 6.8 读日志（含逐尝试链）

```bash
curl -s "$PBR/api/v1/logs?lane=lane-alpha&success=false&limit=1" \
  -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{ "items": [ /* §4.4 的 RequestLog 对象 */ ], "next_cursor": null }
```

### 6.9 更新全局选项（关键词表等；取代拷库改表）

```bash
curl -s -X PUT $PBR/api/v1/system/options \
  -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{
    "automatic_disable_keywords": ["arrearage", "insufficient balance", "..."],
    "automatic_disable_channel_enabled": true,
    "automatic_enable_channel_enabled": true,
    "circuit_failure_threshold": 5,
    "circuit_open_seconds": 60,
    "circuit_max_open_seconds": 1800,
    "log_retention_days": 30,
    "probe_concurrency": 4,
    "model_prices": [
      {"model": "model-1", "input": 2.5, "output": 10, "cache_read": 1.25, "cache_write": 3}
    ]
  }'
```

- 字段名以本表为准（`automatic_disable_keywords`，不是 `auto_disable_keywords`）。
- **未在 body 中出现的键保持不变**（字段级补丁，不是全量替换）：只想改熔断阈值时
  不会把关键词表清空。
- 车道六键（`member_max_attempts` 等）是**车道级**配置，在
  `PUT /api/v1/lanes/{name}` 的 `config` 里设置，不属于全局选项。
- `log_retention_days`（默认 30）只作配置；实际清理由 `POST /api/v1/logs/prune` 触发。
- `probe_concurrency`（默认 4）限制 `POST /lanes/{name}/probe` 对上游的并发压力。
- `model_prices`（design-v1 §16.9#7）是**单价表**，单位**人民币 / 百万 token**，
  四个价格字段（`input` / `output` / `cache_read` / `cache_write`）可留空或为 0。
  键语义：**整表替换**（传 `[]` 即清空），模型不在表里就完全不折算。
  这张表**只用于日志 `estimated_cost` 的折算展示**——不参与准入、不扣任何额度
  （design-v1 G7「只看不扣」）。计价键是**请求模型名**（不是被改写的上游模型名）。
  校验：`model` 非空、不重复，价格非负；否则 400。

### 6.10 导出 / 导入（取代拷库备份）

```bash
curl -s $PBR/api/v1/export -H "Authorization: Bearer $ADMIN_KEY" -o pbr-config.json

curl -s -X POST "$PBR/api/v1/import?dry_run=true" \
  -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  --data-binary @pbr-config.json
```
```json
{ "dry_run": true, "valid": true,
  "diff": { "channels": { "add": [], "update": ["channel-a"], "remove": [] },
            "lanes": { "add": ["lane-alpha"], "update": [], "remove": [] },
            "keys": { "add": [], "update": [], "remove": [] },
            "options": { "changed": [] } } }
```

### 6.11 模型面调用（下游协议不变）

```bash
curl -sN $PBR/v1/chat/completions \
  -H "Authorization: Bearer $CLIENT_KEY" -H 'Content-Type: application/json' \
  -d '{ "model": "lane-alpha", "stream": true,
        "messages": [{ "role": "user", "content": "hi" }] }'
```

响应为上游原生 SSE；非流式响应体中 `model` 字段回填 `lane-alpha`，真实服务者见响应头：

```
X-Served-By: channel=12:channel-b, model=model-y
```

（格式为 `channel=<渠道 id>:<渠道名>, model=<上游真名>`，与 design-v1 §4.1 一致；
`<id>` 用于同名渠道的消歧，日志与控制台均按此解析。）

无可用成员时：

```json
HTTP/1.1 503 Service Unavailable
{ "error": { "message": "No available channel for model lane-alpha" } }
```

---

## 7. AI 调用手册（典型工作流）

> 供 harness / 自动化 AI 直接照抄。**AI 只要知道登录口令，就能自己算出管理密钥**，
> 不需要人工复制任何东西：
>
> ```bash
> ADMIN_KEY=$(printf '%s' '<登录口令>' | openssl dgst -sha256 -binary | openssl base64 -A)
> # 等价 python：base64.b64encode(hashlib.sha256(pw.encode()).digest()).decode()
> ```
>
> 之后所有管理调用带 `-H "Authorization: Bearer $ADMIN_KEY"` 即可。

### 7.1 起步三步

```bash
# 0) 只有首次需要：设置登录口令（响应会带 admin_key，但 AI 也可自行计算）
curl -sf $PBR/api/v1/setup/status
curl -sfX POST $PBR/api/v1/setup -H 'Content-Type: application/json' \
  -d '{"password":"<16 位以上随机口令>"}'      # 响应含 admin_key；浏览器另得到会话 Cookie

# 1) 探活（无需鉴权）
curl -sf $PBR/api/v1/health || echo "gateway down"

# 2) 发现能力（模式、适配器枚举）
curl -sf $PBR/api/v1/capabilities -H "Authorization: Bearer $ADMIN_KEY"

# 3) 拉取 OpenAPI（自描述，供工具注册）
curl -sf $PBR/api/v1/openapi.json -H "Authorization: Bearer $ADMIN_KEY" -o openapi.json
```

### 7.2 新增一条"优先 A 渠道再用 B 渠道"的车道

```bash
# 1) 建两个渠道（key 由运维注入，AI 只填占位符）
curl -sfX PUT $PBR/api/v1/channels/channel-a -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"type":"openai","base_url":"https://a.example/v1","key":"__INJECT_BY_OPERATOR__"}'
curl -sfX PUT $PBR/api/v1/channels/channel-b -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"type":"openai","base_url":"https://b.example/v1","key":"__INJECT_BY_OPERATOR__"}'

# 2) 建车道并排好成员优先级
curl -sfX PUT $PBR/api/v1/lanes/lane-beta -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"mode":"failover","members":[
        {"channel":"channel-a","upstream_model":"model-x","priority":1},
        {"channel":"channel-b","upstream_model":"model-y","priority":2}]}'

# 3) 回读确认（写接口自带回读，但首次接入仍建议显式 GET）
curl -sf $PBR/api/v1/lanes/lane-beta -H "Authorization: Bearer $ADMIN_KEY"
```

### 7.3 排障：某车道"断断续续"

```bash
# 1) 看车道当前冷却/熔断/亲和
curl -sf $PBR/api/v1/lanes/<lane>/health -H "Authorization: Bearer $ADMIN_KEY"

# 2) 拉最近失败日志，看 attempts 链定位是哪个成员在挂
curl -sf "$PBR/api/v1/logs?lane=<lane>&success=false&limit=20" \
  -H "Authorization: Bearer $ADMIN_KEY"

# 3) 主动逐成员探活
curl -sfX POST $PBR/api/v1/lanes/<lane>/probe -H "Authorization: Bearer $ADMIN_KEY"

# 4) 确认上游已恢复后，清掉冷却立即复通（正常情况下应等自动半开）
curl -sfX POST $PBR/api/v1/lanes/<lane>/circuits/reset -H "Authorization: Bearer $ADMIN_KEY"
```

### 7.4 配置备份与回滚（不碰文件与数据库）

```bash
curl -sf $PBR/api/v1/export -H "Authorization: Bearer $ADMIN_KEY" -o backup-$(date +%s).json

# 回滚前先 dry-run 看 diff
curl -sfX POST "$PBR/api/v1/import?dry_run=true" -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' --data-binary @backup.json
# 确认后去掉 dry_run 执行
curl -sfX POST "$PBR/api/v1/import" -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' --data-binary @backup.json
```

### 7.5 调用约定（给 AI 的硬规则）

- **只走 API**：不读/写 SQLite 文件、不解析前端、不直接编辑配置。
- **写操作必须能判定成功**：以 HTTP 状态 + `error.code` 判定；响应体即最终态，可直接用作断言。
- **先 dry-run 再写**：批量或破坏性变更（`import`、`DELETE`、成员重排）先 `?dry_run=true`。
- **凭据**：只在请求头传 `Bearer`；不把密钥写进日志/输出/提交；`POST /keys` 返回的明文只在内存中用于即时配置下游，**不落盘到仓库**。
- **错误分支**：按 §3 的 `code` 处理。`409 conflict` 先 GET 再决定；`422 lane_has_no_members` 说明车道未配成员；`503 no_available_member` 是模型面语义，不是管理面错误。

---

## 8. 与 UI 的对应关系

控制台每个页面只调用本契约的端点（详见 [`ui-spec-v1.md`](ui-spec-v1.md) §3 的"数据来源"列）。若某页面需要的数据在本契约中缺端点，**先补契约再实现页面**，不许前端直连数据库或自造接口。
