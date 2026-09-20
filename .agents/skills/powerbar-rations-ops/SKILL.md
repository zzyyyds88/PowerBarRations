---
name: powerbar-rations-ops
description: 运维 PowerBarRations（PBR）自用 AI 网关：用登录口令派生管理密钥并调用 /api 管理面（渠道/车道/密钥/日志/统计/导出导入/系统选项/webhook）。需要管理 PBR 网关、排障路由、调整车道或渠道时使用。
version: 2.0.0
---
# PowerBarRations 网关运维（管理 API）

> 本文件是 AI 运维 PBR 的**唯一操作手册**：只凭本文件 + 登录口令即可完成全部日常运维，
> 不需要读 Go 代码、不需要解析控制台、不需要打开 SQLite。
> 规范源：`docs/api-spec-v1.md`（契约）、`docs/routing-spec-v1.md`（路由语义）、
>`docs/token-spec-v1.md`（认证）、`docs/design-v1.md`（基线）。

## 1. 系统是什么（30 秒模型）

- **单二进制 + 单 SQLite** 的自用 AI 网关（Go）。把旧两层（octopus 路由层 + new-api 厂商层）
  塌成一层：没有账号体系、没有计费、没有额度，只有一个登录口令。
- **一个端口两个面**：模型面 `/v1/*`（客户端密钥，OpenAI/Anthropic/Gemini 兼容推理）；
  管理面 `/api/*`（管理密钥，本手册的对象）。`/api/v1/*` 是**完全等价的兼容别名**，
  两个前缀注册同一批处理器。
- **路由模型（ADR 0005）**：请求里的 `model` 就是**路由键**；**每个路由键必须有一条同名车道
  （有序成员链）才能被调用**，没有车道一律 `503 No available channel for model <X>`。
  渠道只负责"声明提供哪些模型 + 上游真名映射"，**渠道不参与路由排序**。
- **三张表的心智模型**：`channel`（厂商接入点：base_url + key + 声明模型）→
  `lane`（路由键的成员链，按 priority 故障转移）→ `client key`（消费者凭据 + 车道白/黑名单）。
  请求 `model=X` 只查"名叫 X 的车道"；车道成员从任意渠道任意已声明模型里挑（ADR 0006）。

## 2. 认证：人机分离，两条通道等价

- **人**：浏览器打开控制台 → 输入登录口令 → 服务端下发 **HttpOnly 会话 Cookie**，浏览器不存密钥。
- **AI/脚本（你）**：用登录口令**自行计算管理密钥**，放进 `Authorization: Bearer`：

  ```
  管理密钥 = Base64( SHA256( UTF-8( 登录口令 ) ) )   # 标准 Base64，带 = 填充，恒为 44 字符
  ```

  一行算出（把 `<口令>` 换成真实口令）：

  ```bash
  AK=$(printf '%s' '<口令>' | openssl dgst -sha256 -binary | openssl base64 -A)
  # 或 python3:
  # AK=$(python3 -c "import hashlib,base64,sys;print(base64.b64encode(hashlib.sha256(sys.argv[1].encode()).digest()).decode())" '<口令>')
  BASE=http://<host>:5700      # 默认端口 5700（示例；以部署实际端口为准）
  A=(-H "Authorization: Bearer $AK" -H 'Content-Type: application/json')
  ```

  服务端只存 `sha256(管理密钥)`；**知道口令就能自己算，无需人工复制**。口令变更后旧密钥立即失效，
  重新计算即可。浏览器会话与 Bearer **两条通道完全等价**，都拥有全量权限。

- **免鉴权端点**（其余全部需要管理密钥）：`GET /api/health`、`GET /api/version`、
  `GET /api/setup/status`、`POST /api/setup`、`POST /api/auth/login`、`POST /api/auth/logout`、
  `GET /api/auth/session`、`GET /api/openapi.json`，以及根路径文档页 `/doc`、`/doc/ui`、`/llms.txt`。
- **网关只跑明文 HTTP**（不加载证书、无 TLS 端点）；需要 HTTPS 由外部反向代理终结。
  **跨机部署务必收紧监听**（`PBR_BIND=127.0.0.1`），否则口令/密钥/渠道 key 明文过网。
- **口令恢复**（需机器访问权限）：临时覆盖管理密钥用 `PBR_ADMIN_KEY=<密钥>`（多把 `PBR_ADMIN_KEYS=a,b`）；
  回到未初始化用 `pbr auth reset --db <pbr.db> --yes`（只清凭据，渠道/密钥数据不动）。

## 3. 铁律（写错会排障半天）

1. **写后回读**：写响应是服务端落库后重读的最终状态，但跨请求仍应 `GET` 校验后再下结论。
2. **`code` 是稳定契约**：按 `error.code` 分支，**绝不解析 message**（文案会变）。
3. **成功无信封**：成功一律返回**裸资源**——单对象就是对象本身；列表是 `{"items":[...],"next_cursor":...}`；
  动作类返回语义化最小对象（`{"changed":3}` / `{"deleted":true}` / `{"reset":1}`）。
  **没有 `{success,message,data}` 包装，也没有"HTTP 200 + 业务失败"**。
4. **失败带真实状态码**，但**两个面的错误体不同**：
   - **管理面** `/api/*`：`{"error":{"code","message","hint"?,"details"?}}`，按 `code` 分支；
   - **模型面** `/v1/*`：**OpenAI 兼容错误体**（如 `{"error":{"message":"..."}}`），
     通常**没有** PBR 的 `code`；此时按 **HTTP 状态码 + message 前缀**判定
     （`503` 且 message 以 `No available channel for model ` 开头 = 路由不可用）。
5. **模型面 503 ≠ 本机繁忙**：`503 No available channel for model <X>` 既表示"上游全挂"，
  也表示"**该路由键还没配车道**"；本机资源过载用 **529**（非权威码），两者不要混。
6. **不猜端点**：完整机器可读契约看 `GET /api/openapi.json`（免鉴权），能力枚举看 `GET /api/capabilities`。
7. **管理密钥不落日志**；不要把口令写进命令历史（用变量或 env）。
8. **`?dry_run=true`（强制声明，禁止静默写入）**：配置类写端点三分类——
   `preview`（返回 `{dry_run:true,valid:true,diff:{...}}`，**绝不落库**）、
   `reject`（**400 `dry_run_not_supported`** 且不执行）、
   `irrelevant`（非配置类：只读/运行态/上游动作/任务触发，参数被忽略）。
   **批量/破坏性操作先 `?dry_run=true` 看 diff**；若某端点返回 `dry_run_not_supported`，说明它不支持预览，
   必须改用它的专用 preview 端点（如 `GET /api/model-catalog/sync-upstream/preview`）或直接执行。
   **例外（不适用本约定）**：`POST /channels/{name}/test`、`POST /lanes/{name}/probe`（探活即结论）、
   `POST /auth/*`、`POST /setup`（无预览语义）。
9. **分页**：`?limit=`（**默认 50，上限 200**）`&cursor=<opaque>`；`next_cursor` 是不透明串
   （如 `"MTA"`），原样回传即可。**非法 cursor 返回 400 `validation_failed`**，不会静默回退第一页。
   列表默认按 name 升序；**要拿全量请显式给 `?limit=200` 并翻页到 `next_cursor=null`**。
10. **时间统一 RFC3339 UTC**（`2026-09-14T12:00:00Z`）；**审计只记元数据**，不记密钥与正文。

## 4. 端点速查

### 4.1 发现与系统

| 目的 | 调用 |
|---|---|
| 存活/版本（免鉴权） | `GET /api/health`、`GET /api/version` |
| 是否已初始化（免鉴权） | `GET /api/setup/status` |
| 能力枚举（**需鉴权**） | `GET /api/capabilities`（`adapters` / `lane_modes` / `inbound_formats` / `circuit`） |
| 机器可读契约（免鉴权） | `GET /api/openapi.json` |
| 全局选项 | `GET/PUT /api/system/options`（部分更新；可写键见 §6.3） |
| 完整选项（含站点/内容/运维） | `GET/PUT /api/system/options/all`（PUT body `{key,value}`） |
| 系统任务 | `GET /api/system-tasks`、`GET /api/system-tasks/current?type=`、`GET /api/system-tasks/{id}`、`POST /api/system-tasks/log-cleanup?target_timestamp=` |
| 性能/日志文件 | `GET /api/system/performance`、`POST .../reset`、`POST .../gc`、`DELETE /api/system/performance/disk-cache`、`GET/DELETE /api/system/log-files` |
| 审计 | `GET /api/audit`（每项 `{id,ts,action,actor,resource,name,dry_run,before_digest,after_digest}`） |

### 4.2 渠道

| 目的 | 调用 |
|---|---|
| 列表/详情 | `GET /api/channels`、`GET /api/channels/{name}` |
| 全量 upsert（写后回读） | `PUT /api/channels/{name}` |
| 删除（被车道引用 409） | `DELETE /api/channels/{name}` |
| 探活 | `POST /api/channels/{name}/test` |
| 拉上游模型清单并回写 | `POST /api/channels/{name}/sync-models`（`?dry_run=` / `?force=1`） |
| 读渠道上游密钥明文（排障） | `GET /api/channels/{name}/key` |
| 批量启停/标签/复制/拉模型 | `POST /api/channels/batch/{status,tag,copy,fetch-models}` |
| 按标签批量改/启停/取模型 | `PUT /api/channels/by-tag`、`POST /api/channels/by-tag/status`、`GET /api/channels/by-tag/models?tag=` |
| 删除全部已停用渠道 | `DELETE /api/channels/disabled` |
| 多密钥管理 | `POST /api/channels/{name}/multi-keys`（body `{action,...}`） |
| 上游模型变更探测/应用 | `POST /api/channels/{name}/upstream-updates/{detect,apply}`、`POST /api/channels/upstream-updates/{detect-all,apply-all}` |
| Codex 凭据/用量 | `POST /api/channels/{name}/codex/{refresh,reset}`、`GET /api/channels/{name}/codex/{usage,reset-credits}` |
| Ollama 拉/删/版本 | `POST /api/channels/{name}/ollama/pull[/stream]`、`DELETE /api/channels/{name}/ollama/models`、`GET /api/channels/{name}/ollama/version` |

### 4.3 车道（唯一路由入口）

| 目的 | 调用 |
|---|---|
| 列表/详情 | `GET /api/lanes`、`GET /api/lanes/{name}` |
| 全量 upsert（成员数组顺序即优先级） | `PUT /api/lanes/{name}` |
| 只替换成员列表 | `PUT /api/lanes/{name}/members` |
| 删除 | `DELETE /api/lanes/{name}` |
| 清理悬空成员（渠道已不存在） | `POST /api/lanes/cleanup-members`（`?dry_run=`） |
| 逐成员探活 | `POST /api/lanes/{name}/probe` |
| 运行态快照（冷却/熔断/亲和） | `GET /api/lanes/{name}/health` |
| 清除全部熔断与冷却 | `POST /api/lanes/{name}/circuits/reset` |
| 全部车道顺序摘要（不分页） | `GET /api/lane-summaries` |

> **成员别名点名**：`health` / `probe` / `circuits/reset` 的 `{name}` 除车道名外也接受成员的
> `public_alias`（点名到该别名所属车道）。`GET/PUT/DELETE /lanes/{name}` 只按车道名。

### 4.4 模型路由

| 目的 | 调用 |
|---|---|
| 全部路由键 | `GET /api/models` |
| 某路由键成员链 | `GET /api/routes/{model}`（`model` 可含 `/`，用 catch-all） |
| 模型目录元数据（不分页） | `GET /api/model-metadata`、`PUT/DELETE /api/model-metadata/{model}` |
| 模型目录同步/缺失/批删 | `GET /api/model-catalog/sync-upstream/preview`、`POST /api/model-catalog/sync-upstream`、`GET /api/model-catalog/missing`、`POST /api/model-catalog/batch-delete` |

`GET /api/models` 每项：`{model, source: explicit|unconfigured|disabled, routable, member_count,
available_member_count}`；explicit 车道额外给 `healthy_member_count` / `health_member_count` /
`degraded`。**`source` 三态语义**：

- `explicit` = 有同名车道（`routable=true` 才当前可调用）；
- `unconfigured` = 渠道声明了但没有车道（**当前不可调用**）；
- `disabled` = 有同名车道但被停用（**当前不可调用**）。

`GET /api/routes/{model}` 顶层含 `source` / `mode` / `config`（六键）；`members[]` 每项含
`channel` / `channel_enabled` / `upstream_model` / `priority`，成员级显式改名给 `upstream_override`，
设了别名给 `public_alias`。**无车道时返回 200 + 推荐成员链 + `routable:false`（不是 404）**；
停用车道返回真实成员链 + `source:disabled`。
**`members` 与 `candidates` 的实测语义**（两者容易记反）：

- **没有同名车道**（`source:unconfigured`）：推荐链就在 **`members[]`**，`candidates` 恒为 `[]`；
- **已有同名车道**（`source:explicit`/`disabled`）：`members[]` 是真实成员链，`candidates[]` 是
  "**已启用、声明/映射了该键、但不在成员链里**"的渠道（全部是成员时为空数组）。

推荐项只供界面排序/高亮，**不限制成员可选范围**（成员可任选任意启用渠道的任意已声明模型）。

### 4.5 客户端密钥

| 目的 | 调用 |
|---|---|
| 列表/创建 | `GET/POST /api/keys` |
| 详情/更新/删除 | `GET/PUT/DELETE /api/keys/{name}` |
| 轮换（换新明文，旧密钥立即失效） | `POST /api/keys/{name}/rotate` |

密钥对象：`id` / `name` / `enabled` / `lane_policy{mode,allow_lanes,deny_lanes}` / `ip_allowlist` /
`rate_limit_rpm` / `max_concurrency` / `expires_at` / `notes` / **`key`（明文，创建与详情可回读）** /
`key_prefix` / `created_at` / `updated_at` / `last_used_at` / `cost`（只读，元）。

- **PBR 没有额度语义**：不存在 `remain_quota` / `used_quota` / 钱包 / 订阅字段，请求体也不接受。
- `lane_policy.mode` 只接受 `all` / `allow`（非法值写入报 422）；`allow_lanes` / `deny_lanes` 必须是
  **真实存在的路由键**（既无同名车道、也无启用渠道声明的键会被 422 拒绝，避免"死键"）。
- 创建/更新**不接受**额度字段；`cost` 是只读统计（小时聚合表派生），不参与任何鉴权。

### 4.6 观测 / 配置生命周期 / Webhook

| 目的 | 调用 |
|---|---|
| 请求日志 | `GET /api/logs?lane=&channel=&key=&model=&success=&since=&until=&cursor=&limit=` |
| 单条日志（含 attempts 链） | `GET /api/logs/{id}` |
| 清理明细日志（聚合不变） | `POST /api/logs/prune?before=&dry_run=` |
| 统计聚合 | `GET /api/stats?granularity=hour|day&from=&to=&group_by=lane|channel|key|model|channel_model` |
| 车道运行态增量（SSE） | `GET /api/route-events` |
| 导出/导入 | `GET /api/export`、`POST /api/import?dry_run=` |
| Webhook 配置/测试/投递 | `GET/PUT /api/webhooks`、`POST /api/webhooks/test`、`GET /api/webhooks/deliveries` |

## 5. 对象与字段（写 PUT 前必读）

### 5.1 渠道（PUT body）

> **PUT 语义（务必看清，否则会丢成员）**：
> `PUT /api/channels/{name}` 是**部分合并**——只给 `enabled` 会保留原有 `models` / `key` /
> `model_mapping` 等（字段缺席 = 保持原值，不是清空）。
>
> `PUT /api/lanes/{name}` 的 `enabled` / `mode` / `config` 也是部分合并，但 **`members` 是
> 整体替换、且省略即视为空**：
> - `{"enabled":false}` → **200，并静默清空整条成员链**（数据丢失路径！）；
> - `{"enabled":true}` / `{"mode":"manual"}` / `{"config":{...}}` 等**结果仍为启用**且没带
>   `members` → `422 lane_has_no_members`；
> - 想只改开关或六键、又想保留成员：**必须把完整 `members` 数组一并带上**；
> - 只改成员用 `PUT /api/lanes/{name}/members`（body `{"members":[...]}`，同样整体替换）。

```json
{
  "type": "openai",
  "base_url": "https://vendor.example/v1",
  "key": "sk-...",
  "models": ["model-1", "vendor/model-2"],
  "model_mapping": {"model-1": "vendor-a/model-1"},
  "enabled": true,
  "proxy": "",
  "param_override": {},
  "prices": [{"model":"model-1","input":1.0,"output":2.0,"cache_read":0.1,"cache_write":0.1}]
}
```

- `type` 是**字符串 slug**（`openai` / `anthropic` / `gemini` / `ollama` / `advancedcustom` …），
  大小写与 `-`/`_` 不敏感；完整枚举见 `GET /api/capabilities` 的 `adapters`。
- `models` 是**路由键**声明（不是上游真名）；上游真名不一致时配 `model_mapping`（路由键 → 上游真名）。
- `key` **只写不读**：响应只有 `key_set`（布尔）与 `key_prefix`（≤4 字符），**永不回显明文**。
  省略 `key` = 保留原值；`GET /api/channels/{name}/key` 是唯一的明文读取入口（排障用）。
- 响应含 `prices`（渠道级上游单价，人民币/百万 token）、`model_mapping`、`param_override`、
  `created_at` / `updated_at`（RFC3339）。
- **`priority` / `weight` 已删除**：请求体里出现一律忽略；路由顺序只在车道成员上。
- **收窄 `models` 时若被移除的键命中同名车道且该车道有本渠道成员**：默认 **409 `conflict`**，
  被引用的车道名在 `error.details.lanes`；`?force=1` 继续并清理这些车道上的本渠道成员。

### 5.2 车道（PUT body）

```json
{
  "enabled": true,
  "mode": "failover",
  "active_member": "",
  "config": {
    "member_max_attempts": 2,
    "member_retry_interval_seconds": 3,
    "member_non_stream_response_timeout_seconds": 120,
    "member_stream_first_event_timeout_seconds": 30,
    "member_cooldown_seconds": 60,
    "member_affinity_seconds": 0
  },
  "members": [
    {"channel": "ch-a", "upstream_model": "model-1", "priority": 20, "public_alias": ""},
    {"channel": "ch-b", "upstream_model": "vendor/model-1", "priority": 10}
  ]
}
```

- **车道名 = 路由键**（`PUT /api/lanes/{model}`）；成员按 `priority` 降序尝试（数组顺序即优先级）。
- `mode`：`failover`（默认，按顺序逃逸）或 `manual`（只走 `active_member` 指定的成员；
  **manual 失败不写冷却**）。
- **成员可任选任意启用渠道的任意已声明模型**，不要求成员声明了该路由键，**同一渠道可多次出现**
  （去重键 = `(渠道, 上游真名)`）。`upstream_model` 留空 = 用渠道 `model_mapping` / 路由键。
- **上游真名解析优先级**：成员 `upstream_model`（非空且 ≠ 路由键）> 渠道 `model_mapping` > 路由键。
- 六键语义：`member_max_attempts` 每条成员最大尝试数；`member_retry_interval_seconds` 重试间隔；
  `member_non_stream_response_timeout_seconds` 非流式响应超时；
  `member_stream_first_event_timeout_seconds` 流式**首个有效 SSE 事件**超时（心跳/注释行不解除）；
  `member_cooldown_seconds` 失败冷却；`member_affinity_seconds` 亲和保持（默认 0 = 关闭）。
  **注意：`hard_auth` / `hard_quota`（鉴权失败、欠费）类失败的冷却 = 2 × `member_cooldown_seconds`**
  （硬故障倍率 2）；软故障（`soft_transient` / `soft_rate_limit`）按原值。
- 省略 `config` = 用系统 `lane_defaults`（见 §6.3）。

### 5.3 `GET /lanes/{name}/health` 形状

```json
{
  "lane": "model-1", "source": "explicit", "mode": "failover",
  "current_member": "ch-a/model-1", "probe_member": "", "affinity": null,
  "members": [{
    "member": "ch-a/model-1", "channel": "ch-a", "upstream_model": "model-1",
    "circuit": "closed", "consecutive_failures": 0, "failure_score": 0,
    "rolling_success_rate": 1, "cooldown_until": null, "circuit_open_until": null,
    "current": true, "probing": false, "available": true
  }]
}
```

- `circuit` ∈ `closed` / `open` / `half_open`；`cooldown_until` / `circuit_open_until` 是
  **RFC3339 字符串或 null**（不是 unix 毫秒 0）。`affinity` 是对象或 null。
- `last_error_kind` 出现在有过失败的成员上，取值如 `hard_auth` / `soft_transient` /
  `soft_rate_limit` / `client_error` / `network_error`。
- `rolling_success_rate` **冷启动为 0**（无样本），不要把它当成"上游有问题"。
- **`events[]`** 是近期运行态事件（`type` = `cooldown` / `reset` / 熔断三态；`ts` 毫秒；
  `member` = `<渠道id>:<上游真名>`；`detail` 如 `cooldown_until=<毫秒>`），排障价值高；
  **无事件时是 `null`（不是 `[]`）**。
- **判定"不可用/降级"的规则**：成员 `available=false`（或 `cooldown_until` 非 null、
  `circuit=open`）即不可选；全部成员不可选时 `GET /api/models` 的 `degraded=true`、
  `healthy_member_count=0`，但 **`routable` 仍为 true**（车道存在 ≠ 现在可用）。
  停用车道（`source:disabled`）在 `GET /api/models` 里 `available_member_count=0`，
  但 `health_member_count` 仍等于成员数（运行态统计照常给出）。
- **运行态是进程内的**（冷却/熔断/亲和），**重启即清空**，不持久化。

### 5.4 请求日志与 attempts 链

`GET /api/logs` 每项含：`id` / `ts`（RFC3339）/ `lane` / `channel` / `key_name` / `request_model` /
`upstream_model` / `inbound_format` / `route_source` / `success` / `http_status` / `is_stream` /
`total_attempts` / `total_ms` / `ttft_ms` / `prompt_tokens` / `completion_tokens` /
`cache_read_tokens` / `cache_write_tokens` / `reasoning_tokens` / `estimated_cost` / `error_kind` /
`error_summary`，以及 **`attempts[]`**：每段含 `attempt_num` / `member`（`渠道/上游真名`）/
`status`（`success` / `failed` / **`cooldown`** / `canceled`）/ `duration_ms` / `error_kind` / `msg`。
**`cooldown` = 该成员正在冷却、本轮根本没打上游**（不是上游失败），据此可区分"上游挂了"与"被冷却短路"。

**排障时 `attempts` 链是最强证据**：它按顺序列出"打给谁、成没成、为什么"。

模型面成功响应的 `X-Served-By` 响应头 = 本次实际服务者，形态固定为
`channel=<渠道id>:<渠道名>, model=<上游真名>`（失败/503/快抛时该头被清除，不会残留）。
这是"这次到底谁回的"最直接证据。

### 5.5 动作型端点的响应形状（照抄用）

| 端点 | 成功体 |
|---|---|
| `POST /api/channels/{name}/test` | `{"channel":"..","upstream_model":"..","status":"success|failed","duration_ms":n,"error_kind":"..","msg":"..","ok":bool,"status_code":n,"latency_ms":n}` |
| `POST /api/lanes/{name}/probe` | `{"lane":"..","probed":n,"results":[<同 test 的成员对象>]}` |
| `POST /api/lanes/{name}/circuits/reset` | `{"lane":"..","reset":<清理计数>}` |
| `GET /api/channels/{name}/key` | `{"key":"<上游明文>"}`（唯一明文入口，排障用） |
| `POST /api/keys` / `POST /api/keys/{name}/rotate` | 完整密钥对象，含 `key` 明文 |
| `DELETE /api/channels/{name}` | `{"deleted":true,"name":".."}` |
| `DELETE /api/keys/{name}` | `{"deleted":true,"name":".."}` |
| `DELETE /api/lanes/{name}` | `{"deleted":true,"name":".."}` |

`status:"failed"` 时 `error_kind` 与 `status_code` 直接给出归类，`msg` 是上游原文。

## 6. 典型工作流（可直接照抄）

### 6.1 起步三步：建渠道 → 建车道 → 发密钥 → 端到端验证

```bash
AK=$(printf '%s' '<口令>' | openssl dgst -sha256 -binary | openssl base64 -A)
BASE=http://127.0.0.1:5700
A=(-H "Authorization: Bearer $AK" -H 'Content-Type: application/json')

# 1) 渠道（key 只写不读；models 是路由键；model_mapping 解决上游改名）
curl -s "${A[@]}" -X PUT "$BASE/api/channels/ch-a" -d '{
  "type":"openai","base_url":"https://vendor.example/v1","key":"sk-...",
  "models":["model-1"],"model_mapping":{"model-1":"vendor-a/model-1"},"enabled":true}'

# 2) 车道（唯一路由入口；只声明 models 不建车道则该键不可调用）
curl -s "${A[@]}" -X PUT "$BASE/api/lanes/model-1" -d '{
  "enabled":true,"mode":"failover",
  "config":{"member_max_attempts":2,"member_retry_interval_seconds":3,
            "member_non_stream_response_timeout_seconds":120,
            "member_stream_first_event_timeout_seconds":30,
            "member_cooldown_seconds":60,"member_affinity_seconds":0},
  "members":[{"channel":"ch-a","priority":10}]}'

# 3) 客户端密钥（明文入库，创建响应与详情都可回读）
#    最稳的取法：先创建，再 GET 回读（避免管道/流被提前消费导致拿到空值）。
curl -s "${A[@]}" -X POST "$BASE/api/keys" -d '{"name":"my-key"}' > /tmp/key.json
CK=$(curl -s "${A[@]}" "$BASE/api/keys/my-key" | python3 -c 'import sys,json;print(json.load(sys.stdin)["key"])')
[ -n "$CK" ] || { echo "取密钥失败"; exit 1; }

# 4) 回读校验 + 端到端调用
curl -s "${A[@]}" "$BASE/api/routes/model-1"        # routable=true 才能调
curl -s -H "Authorization: Bearer $CK" -H 'Content-Type: application/json' \
  -X POST "$BASE/v1/chat/completions" \
  -d '{"model":"model-1","messages":[{"role":"user","content":"ping"}]}'
```

### 6.2 排障：某路由键为什么失败

按顺序查，每一步都能独立定位一类问题：

1. `GET /api/models` → 该 `model` 的 `source` 与 `routable`。
   `unconfigured` = 没配车道；`disabled` = 车道被停用；两者都不可调用。
2. `GET /api/routes/{model}` → 成员链、`source`、`mode`、以及**解析后的上游真名**。
   成员链为空 = 必然 503。
3. `GET /api/lanes/{model}/health` → `circuit` / `cooldown_until` / `consecutive_failures` /
   `last_error_kind` / `available`。全部 `available:false` 或 `degraded:true` = 上游全挂。
4. `GET /api/logs?success=false&model={model}` → 读 `attempts` 链，看每段失败原因。
5. 仍异常：`POST /api/lanes/{model}/probe` 逐成员真实探活（`results[].status` / `error_kind` / `msg`）；
   确认上游恢复后 `POST /api/lanes/{model}/circuits/reset` 立即清冷却/熔断。

### 6.3 改系统选项（取代拷库改表）

`PUT /api/system/options`（**部分更新**，body 里缺席的键保持原值）。可写键：

| 键 | 含义 |
|---|---|
| `circuit_failure_threshold` | 熔断打开的连续失败阈值 |
| `circuit_open_seconds` | 熔断初始打开时长 |
| `circuit_max_open_seconds` | 熔断打开时长上限（指数退避封顶） |
| `circuit_rolling_min_samples` | 滚动窗口最小样本数 |
| `circuit_rolling_failure_rate` | 滚动窗口失败率阈值 |
| `log_retention_days` | 明细日志保留天数（默认 30） |
| `probe_concurrency` | 逐成员探活并发上限（默认 4） |
| `automatic_enable_channel_enabled` | 自动恢复渠道开关 |
| `automatic_disable_channel_enabled` | 自动禁用渠道开关 |
| `automatic_disable_keywords` | 触发自动禁用的上游错误关键词 |
| `lane_defaults` | 新建车道 / 未显式配置车道的默认六键 |

**完整**选项（站点/内容/运维）用 `GET /api/system/options/all`，单键写入用
`PUT /api/system/options/all` body `{"key":"...","value":...}`；值校验失败 → **422 `validation_failed`**。

**取值范围**：整数类键必须 ≥ 0（`log_retention_days` ≥ 1 才有意义）；`circuit_rolling_failure_rate`
是 0–1 的浮点；`automatic_disable_keywords` 是字符串数组；`lane_defaults` 是六键对象。
**写错只会拿到 422 而不会告诉你合法范围**——拿不准就先 `GET` 当前值，只改要改的键（部分更新）。

### 6.4 配置快照与恢复（不碰文件与数据库）

```bash
curl -s "${A[@]}" "$BASE/api/export" -o backup.json
curl -s "${A[@]}" -X POST --data-binary @backup.json "$BASE/api/import?dry_run=true"   # 先预览 diff
curl -s "${A[@]}" -X POST --data-binary @backup.json "$BASE/api/import"               # 再落库
```

- **`/export` 不含密钥明文/哈希**；导入只用于**同版本 PBR 实例间还原配置**，不是旧系统迁移工具。
- 字段缺席 = 保持原值；显式空值（`{}` / `[]` / `""`）= 清空。只有"bundle 与库里都没有"的成员才跳过，
  在 `warnings` / `diff.skipped` 列出。
- **形状提醒**：`/export` 顶层是 `{version, exported_at, channels, lanes, client_keys, system_options}`
  （`system_options` 是扁平标量 map，`lane_defaults` 是其中的对象）；
  而 `/import` 的 `diff` 按资源分组：`channels` / **`keys`** / `lanes` 三组是
  `{add:[], update:[], unchanged:[], remove:[], skipped:[]}`，但 **`options` 组是
  `{"changed":[...]}`**（不是五数组）。**导出叫 `client_keys`，diff 里叫 `keys`**。
- **最稳妥的备份是直接拷 `pbr.db`（连同 `-wal` / `-shm`）**；DB 文件是 `600`，渠道 key 明文落库属预期。

### 6.5 Webhook 事件通知（PBR → 你的接收端）

PBR 把路由运行态事件（熔断/冷却/恢复）异步 POST 到你配置的 URL；**PBR 只定义契约并投递**，
接收端自行验签/路由/呈现。

- `PUT /api/webhooks` body：`{"targets":[{"name":"notify","url":"http://127.0.0.1:8645/hook",
  "secret":"<随机长串>","enabled":true,"events":[]}]}`（`events` 空 = 全部；`secret` 留空 = 保留原值，
  回显只给掩码 `****`+末 4 位）。
- 事件体：`{"type":"pbr","text":"...","event":{"ts":<毫秒>,"type":"circuit_open|circuit_half_open|circuit_closed|cooldown",
  "lane":"...","member":"<channelId>:<upstreamModel>","detail":"..."}}`。
- **验签（接收端必做）**：读头 `X-Webhook-Timestamp`（Unix 秒）与 `X-Webhook-Signature-V2`，
  对**原始请求体字节**计算 `HMAC_SHA256(secret, "{ts}.{body}")` 的 hex 并常数时间比较；
  `ts` 与本地时间偏差 > ±300s 拒收。
- 投递语义：8s 超时；非 2xx/超时按 5s/30s/120s 退避重试 3 次，耗尽记死信；
  同一 `(target,lane,member,event)` 60s 窗口只发一条；`2xx` 即送达。

### 6.6 安全清理 SOP（避免删错、避免动到别人的资源）

**删除顺序必须是：先删车道 → 再删渠道 → 最后删密钥。**

1. 车道引用渠道，渠道引用上游；反过来删会拿到 409（这是保护，不是 bug）。
2. 删之前先 `GET` 确认引用关系：
   - 删渠道前：`GET /api/channels/{name}`（若 409 会给出 `details.lanes`）；
   - 删车道前：`GET /api/routes/{name}`；
   - 删密钥前：确认没有消费者还在用它（`GET /api/keys/{name}` 看 `last_used_at`）。
3. 破坏性操作优先 `?dry_run=true` 预览（注意：`DELETE /api/channels/disabled?dry_run=true`
   仍会走引用检查并返回同样的 409，**它没有真正的预览分支**）；该端点是**整批**操作，
   任一被引用即整批 409（`details.blocked` 列出 渠道名 → 车道名），不会部分删除。
4. **不要假设只有你在改**：写前 `GET` 复核、写后立刻回读；出现"删了却 200 / 该存在却 404"
   先查 `GET /api/audit` 对账（可能被其他操作者并发改动）。
5. 收尾后回读 `GET /api/channels` / `/api/lanes` / `/api/keys` / `/api/models`，确认无残留、
   预置资源未被误删。

## 7. 错误码表（按 `error.code` 分支）

| code | 典型状态 | 触发 |
|---|---|---|
| `validation_failed` | 400 / 422 | 参数非法；lane_policy 未知键、name_rule 越界、选项值校验失败为 422 |
| `invalid_request` | 400 | 请求体非法/互斥字段（如同时给 `channels` 与 `ids`） |
| `unauthorized` | 401 | 管理密钥错误/缺失，或客户端密钥失效 |
| `forbidden_scope` | 403 | 客户端密钥的 `lane_policy` 拒绝了该路由键 |
| `channel_not_found` | 404 | 渠道名不存在（`details.unknown` 列出全部未知名） |
| `lane_not_found` | 404 | 车道名不存在 |
| `key_not_found` | 404 | 客户端密钥名不存在 |
| `model_not_found` | 404 | 模型目录记录不存在 |
| `task_not_found` | 404 | 系统任务 id 不存在 |
| `prefill_group_not_found` | 404 | 预填组 id 不存在 |
| `log_not_found` | 404 | 日志 id 不存在 |
| `lane_has_no_members` | 422 | 车道 PUT 省略/清空 `members`（车道必须有成员） |
| `member_channel_missing` | 422 | 车道成员引用了不存在的渠道（`details` 无，看 message） |
| `conflict` | 409 | 重名、被车道引用、上游空清单、同类任务已在跑 |
| `not_initialized` | 409 | 未设口令就调管理面（先 `POST /api/setup`） |
| `upstream_error` | 502 | 上游调用失败（Codex/Ollama/拉模型等） |
| `internal_error` | 500 | 服务端错误（含日志文件部分删除失败） |

**`details` 是机器可判定明细**，当前稳定用途：

- `details.blocked`：渠道名 → 引用它的车道名列表（批量删渠道、`DELETE /channels/disabled` 的 409）；
- `details.lanes`：引用被移除模型的车道名列表（收窄渠道 `models`、删渠道的 409）；
- `details.unknown`：不存在的名字列表（404）；
- `details.failed_files`：日志文件清理时删除失败的文件名（500）；
- `details.upstream_status`：上游 HTTP 状态码（Codex 系列 502）。

## 8. 坑备忘（按踩坑频率排序）

1. **没车道 = 不可调用**：渠道声明 `models` **绝不**自动建/改车道；删车道会立刻让该键 503。
   `POST /api/lanes/seed` 与"一键固化"**已按 ADR 0005/0006 整体移除**，不要调用。
2. **`GET /api/routes/{model}` 未配车道返回 200 + 推荐成员链 + `routable:false`**（不是 404）；
   模型面请求才是 503。**推荐链不代表已可调用**，也不限制成员可选范围；
   **没有同名车道时推荐链在 `members[]`、`candidates` 为空**（别记反，见 §4.4）。
3. **`sync-models` 有保护**：上游返回空清单默认 409（需 `?force=1`）；要移除的键仍被车道引用也 409。
   `?dry_run=true` 只预览。
4. **渠道删除**：被车道引用时 409，引用它的车道名在 `error.details.lanes`（不解析 message）。
5. **上游改名**：优先在渠道配 `model_mapping`；车道成员 `upstream_model` 是**可选覆盖**，留空即用映射。
6. **`/export` 不含密钥明文/哈希**；备份请直接备份 `pbr.db`（含 `-wal`/`-shm`）或 `POST /import`。
7. **运行态是进程内的**（冷却/熔断/亲和），重启即清空；不要期望它持久化。
8. **DB 文件 600**：渠道 key 明文落库，属预期；不要把它拷到共享位置。
9. **模型面 `503` 是路由键级别的**：同一路由键的多个成员全挂与"没配车道"同形；
   用管理面 `GET /api/models` 区分（`source` 字段）。
10. **管理面无全局限流**（自用网关）；限流只作用于客户端密钥与模型面（`rate_limit_rpm` / `max_concurrency`）。
11. **不存在的路径返回的是 gin 默认 404**（`{"error":{"message":"Invalid URL (POST /api/..)","type":"invalid_request_error","param":"","code":""}}`），
   **不是** §7 的 PBR 错误包络。看到这种 `code:""` 的形状 = 路径本身不存在，先查 `GET /api/openapi.json` 确认端点。
12. **同一实例可能有多个操作者/会话并发写**（控制台、其他 AI、脚本）：写前 `GET` 复核、
   写后立刻回读，异常时用 `GET /api/audit` 对账；不要假设"只有我在改"。

## 9. 交付前自检清单

- [ ] 用 `GET /api/health` + `GET /api/version` 确认在线。
- [ ] 每次写后 `GET` 回读并断言最终状态。
- [ ] 错误按 `error.code` + HTTP 状态分支，不解析 message。
- [ ] 失败时先看 `GET /api/models` 的 `source`/`routable`，再看 `health`，最后看 `logs` 的 `attempts`。
- [ ] 破坏性操作（删渠道/删车道/删密钥）前先 `GET` 确认引用关系；必要时 `?dry_run=true`。
- [ ] 不把口令、管理密钥、渠道 key 写进日志或命令历史。
