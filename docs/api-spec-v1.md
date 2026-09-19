# PowerBarRations 管理 API 契约 v1

> 规范性文件，从属于 [`design-v1.md`](design-v1.md) §5。
> 目标读者：实现者（Go 后端 / 前端）与**调用方 AI**。
> 本文中的 `$PBR` 代指网关地址（如 `http://127.0.0.1:5700`），`$ADMIN_KEY` / `$CLIENT_KEY` 为凭据，**均为占位符**。

---

## 1. 认证

| 面 | 前缀 | 凭据 |
|---|---|---|
| 管理面 | `/api/*` | **二选一**：`Authorization: Bearer <管理密钥>`（AI/脚本）或 HttpOnly 会话 Cookie（浏览器，登录后自动携带） |
| 模型面 | `/v1/*` | `Authorization: Bearer <客户端密钥>`（同时兼容 `X-Api-Key`） |

**管理密钥 = `Base64(SHA256(登录口令))`**（无账号体系），由调用方自行计算，服务端只存其哈希；首次使用先 `POST /api/setup` 设置口令（未初始化时除 `/health`、`/version`、`/setup*`、`/auth/login`、`/auth/logout`、`/auth/session`、`/openapi.json` 与根路径 `/doc*`、`/llms.txt` 外一律 `409`/`401`）；浏览器登录 `POST /api/auth/login` 换 HttpOnly Cookie（不写 localStorage），AI/脚本走 Bearer，两通道等价。派生规则、会话属性、失败退避与恢复手段（`PBR_ADMIN_KEY(S)`、`pbr auth reset`、`PBR_BIND` 与明文 HTTP 边界）详见 [`token-spec-v1.md`](token-spec-v1.md) §2；认证失败的响应形态见 §3（401 `unauthorized`）。

> 前缀说明：`/api` 是规范前缀；`/api/v1` 保留为**兼容别名**（注册完全相同的处理器），既有脚本无需改动。控制台内部资源的边界见 §9。面向 AI 的手册：`GET /doc`（`text/markdown`）、`GET /llms.txt`（`text/plain`）、交互式文档 `GET /doc/ui`（§5.1）。

---

## 2. 通用约定

1. **全量幂等写**：`PUT /api/{resource}/{name}`，body 为完整对象，upsert 语义；同名重复提交结果一致。
2. **写后回读**：响应体是**落库后重新读取**的最终状态。实现必须在 handler 内 re-read 再返回。
3. **成功响应无信封**：成功一律返回**裸资源**——单对象就是该对象本身（如 `GET /api/channels/{name}` 直接返回渠道对象），列表是 `{"items":[...],"next_cursor":...}`，动作类端点返回**语义化最小对象**（如 `{"changed":3}`、`{"deleted":true,"name":"..."}`、`{"reset":true}`）。**没有 `{success,message,data}` 包装，也没有"HTTP 200 + 业务失败"**；失败一律见 §3。字段名由各端点表逐条固定，不额外套壳。
4. **dry-run**：配置类 `PUT/POST/DELETE` 支持 `?dry_run=true`，返回将发生的 diff 而不落库：

   ```json
   { "dry_run": true, "valid": true,
     "diff": { "lanes": { "add": ["lane-beta"], "update": [], "remove": [] } } }
   ```

   **例外（不支持 dry-run，也不会写库）**：`POST /api/channels/{name}/test`、`POST /api/lanes/{name}/probe`（探活本身就是只读的真实请求）、`POST /api/auth/*`（认证没有"预览"语义）。
5. **分页**：列表用 cursor。请求 `?limit=50&cursor=<opaque>`，响应 `{"items":[...], "next_cursor":"<opaque|null>"}`；`limit` 上限 200，默认 50。**非法/损坏的 cursor 返回 400 `validation_failed`**（不得静默回退到第一页，否则调用方会陷入翻页死循环）。
6. **时间**：RFC3339 UTC（`2026-09-14T12:00:00Z`）。
7. **审计**：所有变更写 `audit_logs`（`ts, actor, action, resource, name, before_digest, after_digest, dry_run`），只记元数据，不记密钥与请求正文。
8. **管理面不做全局限流**：PBR 是自用单用户网关，管理面 `/api/*` 与静态控制台**默认关闭**基座遗留的全局限流
   （`GLOBAL_API_RATE_LIMIT_ENABLE`/`GLOBAL_WEB_RATE_LIMIT_ENABLE` 默认 `false`）。控制台一次页面加载会并发多个
   管理请求，基座默认的 360/120 次窗口（继承自 new-api 的多租户公网假设）会把正常浏览打成 429。
   限流只作用于**客户端密钥与模型面**（`rate_limit_rpm`/`max_concurrency`，见 token-spec §3.4）与登录失败退避。
   公网部署如确需，可显式打开上述环境变量。

---

## 3. 错误模型

```json
{ "error": { "code": "lane_not_found", "message": "lane 'lane-alpha' not found", "hint": "GET /api/lanes" } }
```

`details` 为**可选**结构化明细（对象），只在能给出机器可判定信息时出现。当前稳定用途是"车道引用守卫"：

- `details.blocked`：渠道名 → 引用它的车道名列表（`POST /api/model-catalog/batch-delete`、`DELETE /api/channels/disabled` 的 409）。
- `details.lanes`：引用被移除模型的车道名列表（`PUT /api/channels/{name}` 收窄 `models`、`DELETE /api/channels/{name}` 的 409）。

调用方**应按 `details` 判定**，不要解析 `message`。控制台内部路径（`PUT /api/channel/` 等）同样携带这些明细。

**失败一律带真实 HTTP 状态码**（不再有"200 + `success:false`"）：调用方按 `status` + `error.code` 分支，二者都不得被忽略。

| HTTP | code | 触发场景 |
|---|---|---|
| 400 | `invalid_request` | JSON 解析失败 / 字段类型错 / 互斥字段同时给出 |
| 400 | `validation_failed` | 字段校验失败（`details` 给出字段级原因） |
| 401 | `unauthorized` | 密钥缺失或错误 |
| 403 | `forbidden_scope` | 客户端密钥访问了被 deny 的车道（仅模型面） |
| 404 | `lane_not_found` / `channel_not_found` / `key_not_found` / `log_not_found` | 对象不存在 |
| 404 | `model_not_found` | 模型目录记录不存在（`PUT/DELETE /api/model-metadata/{model}`）；按名批量操作时 `details.unknown` 列出未知名 |
| 404 | `task_not_found` | `GET /api/system-tasks/{id}` 的任务不存在 |
| 404 | `prefill_group_not_found` | `DELETE /api/prefill-groups/{id}` 的组不存在 |
| 404 | `webhook_target_not_found` | `POST /api/webhooks/test` 的 name 不在配置里（§5.8） |
| 500 | `internal_error` | 未归类的内部失败（DB 故障等） |
| 409 | `conflict` | 唯一名冲突 / 乐观锁冲突 / 车道名与成员别名冲突 / **环境变量管理密钥生效时变更口令**（`PBR_ADMIN_KEY`/`PBR_ADMIN_KEYS` 优先，口令变更不生效） |
| 409 | `not_initialized` | 未设置登录口令就调用管理接口（先 `POST /api/setup`） |
| 422 | `lane_has_no_members` | 启用车道但无成员 |
| 422 | `member_channel_missing` | 成员引用的渠道不存在 |
| 422 | `invalid_mode` | 模式不在 failover/manual（`weighted`/`round_robin` 已删） |
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
  "models": ["model-1", "model-2", "model-3"],
  "param_override": {},
  "enabled": true,
  "proxy": "",
  "prices": [
    { "model": "model-1", "input": 10, "output": 20, "cache_read": 1, "cache_write": 2 }
  ],
  "model_mapping": { "model-1": "vendor-a/model-1" },
  "key_set": true,
  "key_prefix": "sk-a",
  "created_at": "2026-09-14T12:00:00Z",
  "updated_at": "2026-09-14T12:00:00Z"
}
```

- `models`：本渠道提供的**路由键候选**（模型名）。声明只是"候选成员来源"，**不等于可调用**：必须存在同名启用车道才可路由（[routing-spec-v1.md](routing-spec-v1.md) §1.1、ADR 0005）；未配车道的模型请求返回 `503`。可用 `POST /api/channels/{name}/sync-models` 从上游拉取模型清单（渠道编辑器内手动触发）。
- **渠道没有 `priority` 与 `weight`**（已物理删除）：路由顺序完全由车道成员顺序决定。请求体里出现这两个字段会被忽略（不报 400），旧导出文件导入时同样忽略。
- **写**：body 可含 `"key": "<明文>"`；**读**：一律不含 `key`，只有 `key_set` 与 `key_prefix`（明文前 4 字符，短密钥不整串回显）。`PUT` 时若省略 `key` 则保留原值。
- `type` 取值见 `GET /api/capabilities` 的 `adapters`。
- `base_url`：**允许带版本段或完整端点结尾**。对 OpenAI/Anthropic/Gemini 渠道，网关拼接上游路径前会**剥掉结尾的完整端点**（`/chat/completions`、`/responses[/compact]`、`/messages`、`/completions`、`/embeddings`）**与版本段**（`/v1`、`/v1beta`、`/v1alpha`），因此下面三种写法等价、不会出现 `/v1/v1` 或 `…/v1/chat/completions/v1/…`：
  `https://host` ≡ `https://host/v1` ≡ `https://host/v1/chat/completions`。
  旧数据（只填 host 或版本段）行为不变；Custom(8) 的 `base_url` 原样保留（支持 `{model}` 变量与完整端点）。
- `prices`：**渠道级上游单价**（人民币 / 百万 token），只用于成本折算；同一模型在不同渠道可配不同采购价。**渠道未配价即不折算（0）——没有全局单价层**。省略该字段时保持原值。
- `model_mapping`：**渠道模型映射**（JSON dict，路由键 → 上游真名），用于上游命名与路由键不一致的情况。车道成员解析上游名时：成员级 `upstream_model`（非空且≠路由键）> 本映射 > 路由键。省略该字段时保持原值。

### 4.2 Lane

```json
{
  "name": "lane-alpha",
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
    { "channel": "channel-a", "upstream_model": "model-x", "public_alias": "", "priority": 2 },
    { "channel": "channel-b", "upstream_model": "model-x", "public_alias": "", "priority": 1,
      "overrides": { "member_max_attempts": 1 } }
  ],
  "orphan_member_count": 0,
  "created_at": "2026-09-14T12:00:00Z",
  "updated_at": "2026-09-14T12:00:00Z"
}
```

- 成员在请求/响应中用 `channel`（渠道名）引用，不暴露内部 ID。
- **`mode` 只有 `failover`（默认）与 `manual`**；`weighted` / `round_robin` 已删除，传入返回 `422 invalid_mode`。
- **`priority` 是车道内顺序，数字大者优先**（示例中的 2/1 表示 channel-a 先试）。控制台用"上移/下移"维护，写库即该值；成员数组顺序与 `priority` 降序一致。
- **成员没有 `weight` 字段**（随 `weighted` 模式一并删除）；旧配置里出现会被忽略。
- `overrides` 为成员级六键覆盖，省略字段表示继承车道。
- **`active_member` 仅 `manual` 模式使用**：人工指定的成员（成员的 `public_alias`，或 `channel/upstream_model` 标签；空串表示未指定）。`orphan_member_count` 是成员中"渠道已不存在"的悬空数量（只读，不落库）；`created_at` / `updated_at` 为车道时间戳。
- **每条车道都是显式对象**：`GET /lanes` 就是全部路由入口，不存在隐藏的自动链（ADR 0005）；没建车道的模型一律 `503`。

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
  "key": "pbr-a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
  "key_prefix": "pbr-a1b2c3d4",
  "created_at": "2026-09-14T12:00:00Z",
  "updated_at": "2026-09-14T12:00:00Z",
  "last_used_at": null
}
```

- `lane_policy.mode ∈ all | allow`；生效车道 = `(all ? 全部 : allow_lanes) - deny_lanes`。默认 `all` 且不拒绝任何车道（见 [token-spec-v1.md](token-spec-v1.md) §3.2）。
- 每项含 `"key"`：当前 PBR 实例创建或轮换时生成的明文密钥；`key_prefix` 为前 12 字符展示前缀，恒有值。配置快照不包含明文密钥，恢复后需重新创建或轮换。

### 4.4 RequestLog（见 §5.5 观测端点的响应）

```json
{
  "id": 12345,
  "ts": "2026-09-14T12:00:00Z",
  "lane": "lane-alpha",
  "request_model": "lane-alpha",
  "route_source": "explicit",
  "channel": "channel-b",
  "upstream_model": "model-x",
  "key_name": "client-a",
  "inbound_format": "openai",
  "success": true,
  "http_status": 200,
  "error_kind": "",
  "error_summary": "",
  "prompt_tokens": 1200,
  "completion_tokens": 340,
  "cache_read_tokens": 0,
  "cache_write_tokens": 0,
  "reasoning_tokens": 0,
  "ttft_ms": 420,
  "total_ms": 3100,
  "is_stream": true,
  "total_attempts": 2,
  "estimated_cost": 0.02,
  "attempts": [
    { "attempt_num": 1, "member": "channel-a/model-x", "status": "failed",
      "duration_ms": 800, "error_kind": "timeout", "msg": "stream first event timeout" },
    { "attempt_num": 2, "member": "channel-b/model-x", "status": "success", "duration_ms": 2300 }
  ]
}
```

`status ∈ success | failed | cooldown | circuit_break | skipped`。

`route_source` 为路由来源（`explicit` / `unconfigured` / `disabled`）；`inbound_format` 为入站协议（`openai` / `anthropic` / `gemini` / `embeddings` / `openai_responses`）；`error_kind` 与 `error_summary` 记录最终失败分类与摘要（成功时为空）；`estimated_cost` 为该请求按渠道级 `prices` 折算的上游花费（单位元；渠道未配价即 `0`）。

---

## 5. 端点总表

### 5.1 系统 / 发现

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | 存活与依赖状态（**免鉴权**） |
| GET | `/api/version` | 版本与构建信息（**免鉴权**） |
| GET | `/api/setup/status` | `{initialized}`（免鉴权） |
| POST | `/api/setup` | 首次设置登录口令，**签发会话 Cookie** 并返回派生管理密钥（仅未初始化时可用） |
| POST | `/api/auth/login` | 口令校验通过→**签发会话 Cookie**，并返回派生管理密钥 |
| POST | `/api/auth/logout` | 清除会话 Cookie |
| GET | `/api/auth/session` | 查询会话状态（**免鉴权**）：`{authenticated, stale}`；`stale=true` 表示携带了已失效的会话 Cookie（口令已变更），服务端同时下发清除 Cookie（详见 token-spec §2.5.1） |
| POST | `/api/auth/password` | 修改口令（会改变管理密钥；旧会话随之失效，当前会话自动续签）。**`PBR_ADMIN_KEY`/`PBR_ADMIN_KEYS` 生效时返回 409 `conflict`**：环境变量管理密钥优先，口令变更不影响实际生效的密钥 |
| GET | `/api/capabilities` | 适配器、模式、能力枚举 |
| GET | `/api/openapi.json` | OpenAPI 3 文档（**免鉴权**） |
| GET | `/doc` | 面向 AI 的管理 API 手册（`text/markdown`；浏览器 `Accept: text/html` 时返回说明页。**免鉴权**） |
| GET | `/llms.txt` | 与 `/doc` 同源的纯文本手册（**免鉴权**） |
| GET | `/doc/ui` | 交互式 OpenAPI 文档（复用 Scalar，指向 `/api/openapi.json`。**免鉴权**） |
| GET | `/api/system/options` | 全局选项 |
| PUT | `/api/system/options` | 更新全局选项（按字段部分更新：body 中缺席的键保持原值）。可写键：`circuit_failure_threshold`、`circuit_open_seconds`、`circuit_max_open_seconds`、`circuit_rolling_min_samples`、`circuit_rolling_failure_rate`、`log_retention_days`、`probe_concurrency`、`automatic_enable_channel_enabled`、`automatic_disable_channel_enabled`、`automatic_disable_keywords`、**`lane_defaults`**（默认六键，见 §4.2；只影响新建车道与未显式配置的车道） |

### 5.2 车道

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/lanes` | 列表（cursor） |
| GET | `/api/lanes/{name}` | 详情（含成员） |
| PUT | `/api/lanes/{name}` | 全量 upsert（含成员，按数组顺序即优先级） |
| POST | `/api/lanes/cleanup-members` | **清理悬空成员**：移除"渠道已不存在"的车道成员（历史数据修复入口），成员清空的车道整条删除；`?dry_run=true` 只返回受影响车道名 |
| DELETE | `/api/lanes/{name}` | 删除 |
| PUT | `/api/lanes/{name}/members` | 仅替换成员列表（有序全量） |
| POST | `/api/lanes/{name}/probe` | 逐成员探活 |
| GET | `/api/lanes/{name}/health` | 当前冷却/熔断/亲和快照 |
| POST | `/api/lanes/{name}/circuits/reset` | 清除该车道全部熔断与冷却 |

- **成员别名点名**：`/api/lanes/{name}/health`、`/api/lanes/{name}/probe`、`/api/lanes/{name}/circuits/reset` 的 `{name}` 除车道名外也接受成员 `public_alias`（点名到该别名所属车道，并标记被点名成员，`model.GetLaneByAlias` 解析）；`GET`/`PUT`/`DELETE /api/lanes/{name}` 只按车道名。模型面按路由键解析时同样优先命中同名车道，未命中再按成员别名点名。

### 5.3 渠道

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/channels` | 列表 |
| GET | `/api/channels/{name}` | 详情 |
| PUT | `/api/channels/{name}` | 全量 upsert（`key` 只写不读）；`model_mapping` 为"路由键 → 上游真名"的 JSON dict。**收窄 `models` 时若被移除的路由键命中同名车道且该车道有本渠道成员**：默认 409 `conflict`，**被引用的车道名在 `error.details.lanes`**（机器可判定，不要解析 message）；`?force=1` 继续，并在落库后把这些车道上的本渠道成员移除（成员清空的空车道整条删除）。发生清理时响应附带 `cleaned_lanes` / `deleted_lanes` |
| DELETE | `/api/channels/{name}` | 删除；被车道引用时 409 `conflict`，引用它的车道名在 `error.details.lanes` |
| POST | `/api/channels/{name}/test` | 单渠道探活 |
| POST | `/api/channels/{name}/sync-models` | 从上游拉取模型清单并回写 `models`（`?dry_run=` 只返回差异）。**上游返回空清单时默认拒绝清空**（需 `?force=1`）；若被移除的路由键正是某条车道的名字、且该车道有本渠道成员，则返回 409 并给出车道清单。`?force=1` 覆盖时**同样执行成员清理**（从命中车道移除本渠道成员，空车道删除），响应附带 `cleaned_lanes` / `deleted_lanes` |

### 5.3.1 渠道批量运维

这些是"完全运维"所必需、此前只在控制台基座路径可用的动作，现提升为稳定契约。
**响应信封**：本节与 §5.3.2–§5.3.4 与 §5.1–§5.8 的**其余端点完全一致**——成功返回下方逐条列出的裸对象（动作类为语义化最小对象），失败为 §3 的错误包络 + 真实 HTTP 状态码。**没有 `{success,message,data}` 包装。**

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/channels/batch/status` | 批量启用/停用：body `{channels:[name], status:1\|2}` 或 `{ids:[int], status}`（**二者只能给一个**）；成功 `{"changed":n}`；空目标或 status 非法 → 400；名字不存在 → 404 `channel_not_found`（`details.unknown`） |
| POST | `/api/channels/batch/tag` | 批量设置标签：body `{channels:[name], tag:string\|null}` 或 `{ids, tag}`；成功 `{"changed":n}`；错误同上 |
| POST | `/api/channels/batch/copy` | 复制渠道：body `{channel:name, suffix?, reset_balance?}`；成功 = **写后回读的渠道对象**（与 §4.1 同形）；名字不存在 → 404 |
| POST | `/api/channels/batch/fetch-models` | 拉取上游模型清单（不落库）：body `{channel:name}`、`{channel_id:int}` 或 `{base_url, key, type}`；成功 `{"models":[...]}`；上游失败 → 502 `upstream_error` |
| PUT | `/api/channels/by-tag` | 按标签批量改配置（改 `models` 时同样受车道引用守卫）；成功 `{"tag":"t","updated":true}`；被引用 → 409 + `details.blocked` |
| POST | `/api/channels/by-tag/status` | 按标签批量启停：body `{tag, status}`；成功 `{"tag":"t","enabled":bool}`；tag 空或 status 非法 → 400 |
| GET | `/api/channels/by-tag/models` | 按标签取模型清单：`?tag=`；成功 `{"tag":"t","models":[...]}`；tag 空 → 400 |
| DELETE | `/api/channels/disabled` | 删除**全部已停用**渠道；成功 `{"deleted":n}`；被车道引用时整批拒绝 409 + `details.blocked` |
| POST | `/api/channels/upstream-updates/detect-all` | 探测全部渠道的上游模型变更；成功 `{"task_id":"..","status":".."}`；已有同类任务 → 409 |
| POST | `/api/channels/upstream-updates/apply-all` | 应用全部渠道的上游模型变更；成功 `{processed_channels, added_models, removed_models, failed_channel_ids, results}` |
| GET | `/api/channels/{name}/key` | 读取渠道上游密钥明文（运维排障）；成功 `{"key":"..."}` |
| POST | `/api/channels/{name}/multi-keys` | 多密钥管理：body `{action, key_index?, page?, page_size?, status?}`；`get_key_status` 成功为分页对象（`keys/total/page/page_size/total_pages/enabled_count/manual_disabled_count/auto_disabled_count`），其余动作成功 `{"applied":true,"message":".."}`；非多密钥渠道 → 409 |
| POST | `/api/channels/{name}/upstream-updates/detect` | 探测该渠道的上游模型变更；成功 `{channel_id, channel_name, add_models, remove_models, last_check_time, auto_added_models}` |
| POST | `/api/channels/{name}/upstream-updates/apply` | 应用该渠道的上游模型变更（车道引用守卫 + `force`）；成功 `{id, added_models, removed_models, ignored_models, remaining_models, remaining_remove_models, models, settings}`；被引用 → 409 + `details.blocked` |
| POST | `/api/channels/{name}/codex/refresh` | 刷新 Codex 凭据；成功 `{expires_at, last_refresh, account_id, email, channel_id, channel_type, channel_name}`；刷新失败 → 502 |
| GET | `/api/channels/{name}/codex/usage` | Codex 用量；成功 `{"upstream_status":n,"body":<上游 JSON>}`；上游非 2xx → 502 `upstream_error` |
| GET | `/api/channels/{name}/codex/reset-credits` | Codex 限额重置额度；成功/失败同上 |
| POST | `/api/channels/{name}/codex/reset` | 重置 Codex 用量；成功/失败同上 |
| POST | `/api/channels/{name}/ollama/pull` | Ollama 拉取模型（非流式）；body `{model_name}`；成功 `{"channel":"..","model":"..","pulled":true}`；非 Ollama 渠道 → 400；上游失败 → 502 |
| POST | `/api/channels/{name}/ollama/pull/stream` | Ollama 拉取模型（**SSE 进度**，`text/event-stream`，逐条 `data: {..}`，以 `data: [DONE]` 结束）。**此端点不套任何信封**；进入流之前的参数/渠道错误仍为 §3 错误包络 |
| DELETE | `/api/channels/{name}/ollama/models` | Ollama 删除模型；body `{model_name}`；成功 `{"channel":"..","model":"..","deleted":true}`；非 Ollama → 400；上游失败 → 502 |
| GET | `/api/channels/{name}/ollama/version` | Ollama 版本；成功 `{"channel":"..","version":".."}`；非 Ollama → 400；取版本失败 → 502 |

### 5.3.2 系统选项、任务与性能

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/system/options/all` | **完整**系统选项（站点/内容/运维）；成功 `{"items":[{"key":"..","value":".."}]}` |
| PUT | `/api/system/options/all` | 更新**单个**系统选项：body `{key: string, value: any}`（与基座同形，非子集对象）；成功 `{"key":"..","updated":true}`；值校验失败 → 422 `validation_failed` |
| GET | `/api/system-tasks` | 系统任务列表；成功 `{"items":[...],"next_cursor":null}` |
| GET | `/api/system-tasks/current` | 某类型当前运行中的任务：**必须带** `?type=`（`log_cleanup` / `channel_test` / `model_update` / `async_task_poll`）；成功 `{"task":<对象或 null>}`；缺 `type` → 400 |
| GET | `/api/system-tasks/{id}` | 单任务详情（成功为任务对象）；不存在 → 404 `task_not_found` |
| POST | `/api/system-tasks/log-cleanup` | 创建"清理日志文件"任务：`?target_timestamp=`（必填）；成功为任务对象；缺参数 → 400 |
| GET | `/api/system/performance` | 性能统计；成功为统计对象 |
| POST | `/api/system/performance/reset` | 重置性能统计；成功 `{"reset":true}` |
| POST | `/api/system/performance/gc` | 强制 GC；成功 `{"collected":true}` |
| DELETE | `/api/system/performance/disk-cache` | 清除磁盘缓存；成功 `{"cleared":true}` |
| GET | `/api/system/log-files` | 日志文件列表；成功 `{log_dir, enabled, file_count, total_size, oldest_time?, newest_time?, files[]}` |
| DELETE | `/api/system/log-files` | 清理日志文件：`?mode=by_count\|by_days&value=`；成功 `{"deleted_count":n,"freed_bytes":n,"failed_files":[]}`；参数非法 → 400；部分删除失败 → 500 + `details.failed_files` |

### 5.3.3 预填组

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/prefill-groups` | 列表（可按 `?type=` 过滤）；成功 `{"items":[...],"next_cursor":null}` |
| POST | `/api/prefill-groups` | 创建；成功 = **写后回读**的组对象；名称或类型为空 → 400；重名 → 409 `conflict` |
| PUT | `/api/prefill-groups/{id}` | 更新（全量 upsert）；成功 = 写后回读的组对象；`{id}` 非数字 → 400；重名 → 409 |
| DELETE | `/api/prefill-groups/{id}` | 删除；成功 `{"deleted":true,"id":n}`；不存在 → 404 `prefill_group_not_found` |

### 5.3.4 模型目录运维

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/model-catalog/sync-upstream/preview` | 预览上游同步的模型变更（不落库）；成功 `{source, candidates}` |
| POST | `/api/model-catalog/sync-upstream` | 应用上游同步：body `{locale?, source_version, selections}`；成功为应用结果；缺 `selections`/`source_version` → 400；上游版本变化或选中项消失 → 409 `conflict` |
| GET | `/api/model-catalog/missing` | "渠道声明但无目录记录"的模型；成功 `{"models":[...]}` |
| POST | `/api/model-catalog/batch-delete` | 批量删除目录记录：body `{models:[name], remove_from_channels?, remove_pricing?}` 或 `{model_ids:[int], ...}`（**二者只能给一个**）；成功 `{"deleted_count":n,"updated_channels":n}`；名字不存在 → 404 `model_not_found`（`details.unknown`）；被车道引用 → 409 + `details.blocked` |

### 5.4 客户端密钥

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/keys` | 列表（每项含只读 `cost` 与明文 `key`） |
| POST | `/api/keys` | 创建（生成明文入库，响应含 `key`） |
| GET | `/api/keys/{name}` | 详情（含只读 `cost` 与明文 `key`） |
| PUT | `/api/keys/{name}` | 更新（权限/限流/备注；不改密钥与哈希） |
| DELETE | `/api/keys/{name}` | 删除 |
| POST | `/api/keys/{name}/rotate` | 轮换（换新明文入库并返回，旧密钥立即失效） |

客户端密钥对象（`GET /api/keys` 元素与详情）字段：`id` / `name` / `enabled` /
`lane_policy{mode,allow_lanes,deny_lanes}` / `ip_allowlist` / `rate_limit_rpm` /
`max_concurrency` / `expires_at` / `notes` / `key_prefix` / `created_at` /
`updated_at` / `last_used_at` / **`cost`**。

- **`cost`（只读，number，单位元）**：该令牌的**上游折算花费**，等于
  `GET /api/stats?group_by=key` 中同名 `key_name` 的 `estimated_cost` 跨时间总计。
  数据源是**小时聚合表**（与看板/日志同源），因此 `POST /logs/prune` 清理明细后该值不变。
  从无请求或聚合表为空时为 `0`。该字段是**统计展示**，不参与任何鉴权、限额或拒绝逻辑。
- **`lane_policy.allow_lanes` / `deny_lanes` 必须是存在的路由键**：写入既无同名车道、也无任何启用渠道
  声明的键会被拒绝（422 `validation_failed`，message 列出未知键）。否则令牌表面"允许/拒绝了模型 X"，
  实际是死键——请求得到 503 而非 403，用户分不清是权限还是没配车道（token-spec §3.2）。两个字段对称校验。
- **`lane_policy.mode` 非法值在写入时报错**（422）：只接受 `all` / `allow`。读取历史数据仍宽容回落
  `all`（避免存量脏数据让密钥不可用），但写入不得静默放宽权限。
- PBR **没有额度语义**：不存在 `remain_quota` / `used_quota` / `unlimited_quota` /
  钱包 / 订阅字段（design-v1 §1.3、token-spec-v1.md §5）。创建/更新请求体也**不接受**额度字段。

### 5.5 观测

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/logs` | 过滤：`lane` `channel` `key` `model` `success` `since` `until` `cursor` `limit` |
| GET | `/api/logs/{id}` | 单条（含 attempts 链） |
| POST | `/api/logs/prune?before=&dry_run=` | 按需清理**明细**日志（`before` 省略则按 `system/options.log_retention_days`，默认 30 天）；聚合表长期保留，**清理后 `/api/stats` 的历史数值不变** |
| GET | `/api/stats` | 聚合：`granularity=hour\|day` `from` `to` `group_by=lane\|channel\|key\|model\|channel_model`；数据源是**小时聚合表**（day 由小时桶上卷），与明细清理互不影响。`channel_model` 的 `group` 形如 `渠道␟模型` |
| GET | `/api/route-events` | **SSE**：车道运行态增量（当前成员/探测占用/亲和/冷却表），供控制台实时显示 |
| GET | `/api/audit` | 变更审计 |

### 5.6 配置生命周期

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/export` | 导出完整配置 JSON（不含密钥明文与哈希）。渠道对象**逐字段导出**（含 `models` / `param_override` / `proxy` / **`prices`** / **`model_mapping`** / `enabled`，密钥只有 `key_set`）；客户端密钥含 `key_prefix` 与全部策略字段（不含明文与哈希）；含 `lanes`（含成员与 `overrides`）与 `system_options`。 |
| POST | `/api/import?dry_run=` | 导入并可选 dry-run，返回 diff。字段缺席 = 保持原值；显式空值（`{}` / `[]` / `""`） = 清空。**导出文件只用于同版本 PBR 实例间还原配置**（bundle 内声明的渠道在构造车道时即视为存在，成员按名字回填真实渠道 id）；只有"bundle 与库里都没有"的成员才被跳过，并在 `warnings` / `diff.skipped` 列出，成员清空的启用车道转为停用。**它不是旧系统迁移或数据恢复工具；密钥明文不在文件里** |

### 5.7 模型路由（车道）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/models` | 全部路由键：`{model, source: explicit\|unconfigured\|disabled, routable: bool, member_count, available_member_count}`（`available_member_count` 只计渠道存在且启用的成员，供界面标注"含不可用"）。explicit 车道额外给出运行态：`healthy_member_count` / `health_member_count` / `degraded`（全部成员当前不可选时为 true）——"车道存在"不等于"现在可用"（routing-spec §7）。`unconfigured` = 渠道声明了但没有车道；`disabled` = 有同名车道但被停用（成员数照常给出）——两者都**当前不可调用**，但仍要在管理面可见，否则"只有一条停用车道的模型"会从控制台消失 |
| GET | `/api/routes/{model}` | 该模型的成员链：每名成员含 `channel` / `channel_enabled`（该渠道是否启用，供界面标灰）/ `upstream_model` / `priority`，成员级显式改名时给出 `upstream_override`、设了成员别名时给出 `public_alias`；响应顶层含 `mode` / `config`（车道模式与六键）。无车道时返回**候选成员**（渠道声明，按渠道 id 升序）并标 `source: unconfigured`、`routable: false`——候选只用于界面上"添加成员"，不代表已可调用；已配车道时额外返回 `candidates`（**声明或 `model_mapping` 映射**了该模型、但不在成员链里的渠道），让新增渠道声明后无需删车道重建。停用车道返回 `source: disabled` 与**真实成员链**（供界面查看/编辑），`routable=false`；运行期路由仍视为不可调用（`ResolveRoute` 返回空链） |
| PUT | `/api/lanes/{model}` | **把某模型的成员链固化为顺序**：车道名 = 模型名，成员按数组顺序即优先级。`mode` 为 `failover`（默认，按顺序逃逸）或 `manual`（只走 `active_member` 指定的成员：成员别名或 `channel/upstream_model` 标签）。**手动建车道**（含无任何渠道声明的自定义路由键）也走这里：控制台路由页「新建车道」即调用它 |
| GET | `/api/lane-summaries` | **全部车道的成员顺序摘要**（不分页）：`{items: [{name, enabled, mode, active_member, orphan_member_count, members: [{channel, upstream_model, priority}]}]}`。供路由页一次取全量顺序，避免 `GET /api/lanes` 的 cursor 上限（200）在大部署下让摘要列退化 |
| GET | `/api/model-metadata` | **模型目录元数据**（不分页）：`{items: [{model, description, icon, tags, endpoints, status, name_rule, has_metadata, configured_channel_count}]}`。这是"模型管理页"的稳定只读面；`has_metadata=false` 表示仅由渠道声明、尚无目录记录。**本面保持轻量：不返回 `matched_count` / `matched_models`**（命中集只在控制台面 `/api/console/models/**` 计算，那里本就要遍历渠道模型做全量填充）。`name_rule != 0` 的条目，`configured_channel_count` **按规则命中的模型名集合**统计去重后的渠道数（用一次内存预计算，与 `MatchesName` 同口径；不得按精确名查表，也不得逐条查库） |
| PUT | `/api/model-metadata/{model}` | **写入模型目录元数据**（全量幂等 upsert）：body `{description, icon, tags, endpoints, status, name_rule}`；响应为写后回读。**`name_rule` 允许 `0`（精确）/ `1`（前缀）/ `2`（包含）/ `3`（后缀）**，由 `model.ValidateMetadataValues` 统一校验，越界返回 422 `validation_failed`。语义：非精确条目是一条**匹配规则**——`model` 字段是规则串而非真实模型名，运行期按 **精确 > 前缀 > 后缀 > 包含** 的优先级命中渠道声明的模型名（`model/model_meta.go` 的 `MatchesName` / `resolveModelMetadata`），即控制台上的"自动匹配"。命中数与命中清单只在控制台面返回 |
| DELETE | `/api/model-metadata/{model}` | **删除模型目录记录**（按 `model_name` 精确匹配这条记录本身，非精确规则条目同样可删——删的只是规则，不影响被它命中的模型）：`?remove_from_channels=true` 同时把该模型从渠道声明里移除，**仅对 `name_rule=0` 允许**（规则条目的命中集是"一批"模型名，批量摘除渠道声明语义不明确，一律 422 拒绝）；被车道引用时 409（返回 `blocked` 渠道→车道清单），`?force=1` 覆盖并清理成员 |

**UI 心智**（design-v1 §7.7）：渠道管理填上游与模型（并在渠道上配 `model_mapping`）→ 模型管理页为该模型设定成员顺序（写 `PUT /lanes/{model}`）→ 令牌允许该模型。**没有车道就没有路由**：未固化的模型请求与"成员全挂"同形返回 `503`。

```bash
curl -s $PBR/api/routes/model-1 -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{
  "model": "model-1",
  "source": "explicit",
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
    { "channel": "channel-a", "channel_enabled": true, "upstream_model": "model-1", "priority": 2 },
    { "channel": "channel-b", "channel_enabled": true, "upstream_model": "model-1", "priority": 1 }
  ]
}
```

- 未声明该模型的渠道 → `members: []`；模型面请求此时返回 `503 No available channel for model model-1`（与全挂同形）。

---

### 5.8 Webhook 通知

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/webhooks` | 读配置：targets 数组（`secret` 回显掩码 `****+末4位`） |
| PUT | `/api/webhooks` | 写配置（同形状；`secret` 留空 = 保留原值） |
| POST | `/api/webhooks/test` | 向指定 target 同步发一条测试事件，返回投递结果 |
| GET | `/api/webhooks/deliveries` | 投递记录（cursor 分页，按 ts 倒序） |

**这是通用推送接口，PBR 只定义契约并投递；接收方的验签、路由、呈现由消费方自行实现**（PBR 不内置针对特定接收端的集成）。

事件请求体（`Content-Type: application/json`）：

```json
{
  "type": "pbr",
  "text": "[PBR] lane-a/ch-a:model-1 熔断打开（60s）：连续失败 hard_auth",
  "event": {
    "ts": 1789600000000,
    "type": "circuit_open",
    "lane": "model-1",
    "member": "ch-a:model-1",
    "detail": "open_seconds=60 score=2"
  }
}
```

- `type`（外层）固定 `pbr`；`text` 为人类可读摘要；`event.type` 取值：`circuit_open`（熔断打开）、`circuit_half_open`（半开探测开始）、`circuit_closed`（恢复）、`cooldown`（进入冷却）。`ts` 为毫秒时间戳，`member` 为 `channelId:upstreamModel`。

**验签（消费方必做）**：
1. 读头 `X-Webhook-Timestamp`（Unix 秒）与 `X-Webhook-Signature-V2`；
2. 对**原始请求体字节**计算 `HMAC-SHA256(secret, "{ts}.{body}")` 的 hex，与其比较（常数时间比较）；
3. `ts` 与本地时间偏差超过 ±300s 拒收（防重放）。

失败语义见 design-v1 §16.10（8s 超时、5s/30s/120s 三次退避、60s 防风暴合并、投递日志随日志保留期清理）。`2xx` 视为送达；其他状态码/超时进入重试。

## 6. 关键请求/响应示例

### 6.1 健康与能力

```bash
curl -s $PBR/api/health
```
```json
{ "status": "ok", "version": "0.1.0", "uptime_s": 12345, "db": "ok" }
```

```bash
curl -s $PBR/api/capabilities -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{
  "api_version": "v1",
  "lane_modes": ["failover", "manual"],
  "adapters": ["openai", "anthropic", "gemini", "ollama", "…共 40 家，完整列表见实际实现…"],
  "inbound_formats": ["openai", "openai_responses", "anthropic", "embeddings"]
}
```

### 6.2 建渠道（写后回读）

```bash
curl -s -X PUT $PBR/api/channels/channel-a \
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
  "prices": [{ "model": "model-1", "input": 10, "output": 20 }],
  "model_mapping": { "model-1": "vendor-a/model-1" },
  "created_at": "2026-09-14T12:00:00Z", "updated_at": "2026-09-14T12:00:00Z" }
```

### 6.3 建车道（含两成员不同渠道不同上游名）

```bash
curl -s -X PUT $PBR/api/lanes/lane-alpha \
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
      { "channel": "channel-a", "upstream_model": "model-x", "priority": 2 },
      { "channel": "channel-b", "upstream_model": "model-y", "priority": 1 }
    ]
  }'
```

响应 = 回读后的完整 Lane 对象（§4.2）。`config` 省略时用默认值。

### 6.4 探活（逐成员）

```bash
curl -s -X POST $PBR/api/lanes/lane-alpha/probe \
  -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{
  "lane": "lane-alpha",
  "probed": 2,
  "results": [
    { "channel": "channel-a", "upstream_model": "model-x", "status": "success", "duration_ms": 640,
      "ok": true, "status_code": 200, "latency_ms": 640 },
    { "channel": "channel-b", "upstream_model": "model-y", "status": "failed",
      "duration_ms": 8000, "error_kind": "timeout", "msg": "non-stream response timeout",
      "ok": false, "status_code": 0, "latency_ms": 8000 }
  ]
}
```

`probed` 为本次实际探测的成员数；每个成员结果除 `status` / `duration_ms` / `error_kind` / `msg` 外，还保留早期实现的兼容字段 `ok`（是否成功）、`status_code`（上游 HTTP 状态，未拿到为 0）、`latency_ms`（耗时，与 `duration_ms` 同值）。

### 6.5 车道健康快照（冷却/熔断/亲和）

```bash
curl -s $PBR/api/lanes/lane-alpha/health -H "Authorization: Bearer $ADMIN_KEY"
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
curl -s -X POST $PBR/api/lanes/lane-alpha/circuits/reset \
  -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{ "lane": "lane-alpha", "reset": 2 }
```

### 6.7 创建客户端密钥（明文持久化）

```bash
curl -s -X POST $PBR/api/keys \
  -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{ "name": "client-a", "enabled": true,
        "lane_policy": { "mode": "all", "allow_lanes": [], "deny_lanes": [] } }'
```
```json
{ "name": "client-a", "key": "pbr-<明文>", "key_prefix": "pbr-a1b2c3d4",
  "enabled": true, "lane_policy": { "mode": "all", "allow_lanes": [], "deny_lanes": [] },
  "created_at": "2026-09-14T12:00:00Z", "last_used_at": null }
```

### 6.8 读日志（含逐尝试链）

```bash
curl -s "$PBR/api/logs?lane=lane-alpha&success=false&limit=1" \
  -H "Authorization: Bearer $ADMIN_KEY"
```
```json
{ "items": [ /* §4.4 的 RequestLog 对象 */ ], "next_cursor": null }
```

### 6.9 更新全局选项（关键词表等；取代拷库改表）

```bash
curl -s -X PUT $PBR/api/system/options \
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
    "lane_defaults": {
      "member_max_attempts": 2,
      "member_retry_interval_seconds": 3,
      "member_non_stream_response_timeout_seconds": 120,
      "member_stream_first_event_timeout_seconds": 30,
      "member_cooldown_seconds": 60,
      "member_affinity_seconds": 0
    }
  }'
```

- 字段名以本表为准（`automatic_disable_keywords`，不是 `auto_disable_keywords`）。
- **未在 body 中出现的键保持不变**（字段级补丁，不是全量替换）：只想改熔断阈值时
  不会把关键词表清空。
- 车道六键（`member_max_attempts` 等）默认是**车道级**配置，在
  `PUT /api/lanes/{name}` 的 `config` 里设置。`lane_defaults` 是它们的**全局默认值**
  （选项键 `PBRLaneDefaults`，数值默认值单处规范见 routing-spec §1.2）：作用于新建车道，
  以及自身未显式配置六键的车道；**已显式配置的车道仍以自身为准**。
  校验：四个时长/预算键必须 > 0，两个间隔键必须 ≥ 0；否则 422。
- `log_retention_days`（默认 30）只作配置；实际清理由 `POST /api/logs/prune` 触发。
- `probe_concurrency`（默认 4）限制 `POST /lanes/{name}/probe` 对上游的并发压力。
- **没有全局单价选项**：成本折算只用渠道级 `prices`（§4.1），渠道未配价即不折算（0）。

### 6.10 导出 / 导入（同版本配置快照）

```bash
curl -s $PBR/api/export -H "Authorization: Bearer $ADMIN_KEY" -o pbr-config.json

curl -s -X POST "$PBR/api/import?dry_run=true" \
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
curl -sf $PBR/api/setup/status
curl -sfX POST $PBR/api/setup -H 'Content-Type: application/json' \
  -d '{"password":"<16 位以上随机口令>"}'      # 响应含 admin_key；浏览器另得到会话 Cookie

# 1) 探活（无需鉴权）
curl -sf $PBR/api/health || echo "gateway down"

# 2) 发现能力（模式、适配器枚举）
curl -sf $PBR/api/capabilities -H "Authorization: Bearer $ADMIN_KEY"

# 3) 拉取 OpenAPI（自描述，供工具注册）
curl -sf $PBR/api/openapi.json -H "Authorization: Bearer $ADMIN_KEY" -o openapi.json
```

### 7.2 新增一条"优先 A 渠道再用 B 渠道"的车道

```bash
# 1) 建两个渠道（key 由运维注入，AI 只填占位符）
curl -sfX PUT $PBR/api/channels/channel-a -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"type":"openai","base_url":"https://a.example/v1","key":"__INJECT_BY_OPERATOR__"}'
curl -sfX PUT $PBR/api/channels/channel-b -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"type":"openai","base_url":"https://b.example/v1","key":"__INJECT_BY_OPERATOR__"}'

# 2) 建车道并排好成员优先级
curl -sfX PUT $PBR/api/lanes/lane-beta -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"mode":"failover","members":[
        {"channel":"channel-a","upstream_model":"model-x","priority":2},
        {"channel":"channel-b","upstream_model":"model-y","priority":1}]}'

# 3) 回读确认（写接口自带回读，但首次接入仍建议显式 GET）
curl -sf $PBR/api/lanes/lane-beta -H "Authorization: Bearer $ADMIN_KEY"
```

### 7.3 排障：某车道"断断续续"

```bash
# 1) 看车道当前冷却/熔断/亲和
curl -sf $PBR/api/lanes/<lane>/health -H "Authorization: Bearer $ADMIN_KEY"

# 2) 拉最近失败日志，看 attempts 链定位是哪个成员在挂
curl -sf "$PBR/api/logs?lane=<lane>&success=false&limit=20" \
  -H "Authorization: Bearer $ADMIN_KEY"

# 3) 主动逐成员探活
curl -sfX POST $PBR/api/lanes/<lane>/probe -H "Authorization: Bearer $ADMIN_KEY"

# 4) 确认上游已恢复后，清掉冷却立即复通（正常情况下应等自动半开）
curl -sfX POST $PBR/api/lanes/<lane>/circuits/reset -H "Authorization: Bearer $ADMIN_KEY"
```

### 7.4 配置快照与恢复（不碰文件与数据库）

```bash
curl -sf $PBR/api/export -H "Authorization: Bearer $ADMIN_KEY" -o backup-$(date +%s).json

# 恢复前先 dry-run 看 diff
curl -sfX POST "$PBR/api/import?dry_run=true" -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' --data-binary @backup.json
# 确认后去掉 dry_run 执行
curl -sfX POST "$PBR/api/import" -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' --data-binary @backup.json
```

### 7.5 调用约定（给 AI 的硬规则）

- **只走 API**：不读/写 SQLite 文件、不解析前端、不直接编辑配置。
- **写操作必须能判定成功**：以 HTTP 状态 + `error.code` 判定；响应体即最终态，可直接用作断言。
- **先 dry-run 再写**：批量或破坏性变更（`import`、`DELETE`、成员重排）先 `?dry_run=true`。
- **凭据**：只在请求头传 `Bearer`；不把密钥写进日志/输出/提交；客户端密钥明文仅存于网关数据库，不落盘到仓库、导出文件或日志。
- **错误分支**：按 §3 的 `code` 处理。`409 conflict` 先 GET 再决定；`422 lane_has_no_members` 说明车道未配成员；`503 no_available_member` 是模型面语义，不是管理面错误。

---

## 8. 与 UI 的对应关系

控制台每个页面只调用本契约的端点（详见 [`ui-spec-v1.md`](ui-spec-v1.md) §3 的"数据来源"列）。若某页面需要的数据在本契约中缺端点，**先补契约再实现页面**，不许前端直连数据库或自造接口。

---

## 9. 能力边界（AI 稳定契约 vs 控制台内部接口）

§5 是本契约的全部稳定端点。控制台前端另有一批 new-api 基座接口：它们同样用 PBR 管理密钥
（`Authorization: Bearer <管理密钥>`）鉴权，**可以调用、但随控制台实现变动，不属于稳定契约**。

> **信封不再区分两类接口**：稳定端点与控制台内部接口共用 §2.3 的裸资源成功形态与 §3 的错误模型（含真实 HTTP 状态码）；差别只在**字段是否稳定**——稳定端点字段由本文固定，内部接口字段随控制台实现变动。控制台内部接口的"200 + `success:false`"写法已全部移除。

| 能力 | 仍属控制台内部（非稳定契约） |
|---|---|
| 仪表盘/状态聚合视图 | `/api/status`、`/api/status/test`、`/api/console/models/**`（展示聚合与模型目录控制台面） |
| 变更审计（控制台视图） | `/api/console/audit` |
| 基座兼容别名（与 §5.3.1 稳定端点等价，参数/响应随控制台变动） | `/api/channel/**` |
| 系统选项/性能/日志文件/系统任务/预填组的基座路径 | `/api/option/*`、`/api/performance/*`、`/api/log/*`、`/api/system-task/*`、`/api/prefill_group/*` |
| 内部性能明细 | `/api/perf-metrics/**` |
| 静态内容 | `/api/about`、`/api/user-agreement`、`/api/privacy-policy`、`/api/home_page_content` |

> **已提升为稳定契约**（原属本表，现见 §5）：渠道批量启停/标签/复制/上游同步、Codex 与 Ollama
> 渠道专用动作（§5.3.1）；完整系统选项、系统任务、性能与日志文件（§5.3.2）；
> 预填组（§5.3.3）；模型目录同步与缺失检测（§5.3.4）。

**结论**：核心网关能力（渠道、车道与故障转移、客户端密钥、请求日志、统计、路由六键选项、
导出导入、TLS、审计）都在稳定契约 `/api` 内。

**价格/单价无需新端点**：单层化后唯一价格来源是**渠道级上游单价**，已由
`GET/PUT /api/channels/{name}` 的 `prices` 字段（`[{model, input, output, cache_read, cache_write}]`，
人民币/百万 token）读写并回读，属稳定契约。

**模型元数据**已提升：`GET /api/model-metadata`、`PUT /api/model-metadata/{model}`、
`DELETE /api/model-metadata/{model}`（§5.7）。**车道顺序摘要**新增 `GET /api/lane-summaries`（§5.7），
使路由页无需受 `GET /api/lanes` 的 cursor 上限影响。

**运维闭环已完整**：§5 的稳定端点覆盖渠道（含批量与上游同步）、车道、客户端密钥、
请求日志与统计、系统选项（含完整选项）、系统任务、性能与日志文件、预填组、模型目录、
导出导入与 webhook，AI/脚本无需触碰控制台内部路径即可完成全部日常运维。

上表剩余项属控制台展示层与基座兼容别名，随控制台实现变动；要把某一项提升为稳定契约，
先在 §5 补端点再实现。
