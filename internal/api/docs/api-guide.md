# PowerBarRations 管理 API 手册（给 AI / 脚本）

基址：`http(s)://<host>/api`（兼容旧前缀 `/api/v1`）。
模型面（OpenAI 兼容推理）：`/v1`，用**客户端密钥**。
控制台内部接口：`/api/console/*`，只给控制台前端用，**不是**本手册的契约。

## 1. 认证：人机分离，两条通道等价

- **人**：浏览器打开控制台 → 输入登录口令 → 服务端下发 HttpOnly 会话 Cookie。
- **AI / 脚本（你）**：用登录口令自行计算管理密钥，放在 `Authorization: Bearer`：

  管理密钥 = Base64( SHA256( UTF-8( 登录口令 ) ) )（标准 Base64，带 `=` 填充，44 字符）

  ```bash
  AK=$(printf '%s' '<登录口令>' | openssl dgst -sha256 -binary | openssl base64 -A)
  A=(-H "Authorization: Bearer $AK" -H 'Content-Type: application/json')
  BASE=http://127.0.0.1:5700         # 明文 HTTP；需要 HTTPS 请自加反向代理终结 TLS
  ```

  **只要知道登录口令就能自己算，不需要人工复制密钥。** 口令变更后旧密钥立即失效。

## 2. 先发现，再控制

| 目的 | 调用 |
|---|---|
| 存活/依赖/版本（免鉴权） | `GET /api/health`、`GET /api/version` |
| 是否已初始化（免鉴权） | `GET /api/setup/status` |
| 适配器/模式/熔断枚举 | `GET /api/capabilities` |
| 完整机器可读契约 | `GET /api/openapi.json`（免鉴权） |

## 3. 端点总表

> **响应形状（全管理面统一）**：成功 = **裸资源**（单对象即对象本身；列表 `{"items":[...],"next_cursor":...}`；
> 动作类返回语义化最小对象，如 `{"changed":3}`、`{"deleted":true}`）。失败 = `{"error":{"code","message","hint","details"?}}`
> **加真实 HTTP 状态码**。**没有 `{success,message,data}` 包装，也没有"200 承载失败"**——按状态码 + `error.code` 分支。

- 渠道：`GET /api/channels`、`GET|PUT|DELETE /api/channels/{name}`、
  `POST /api/channels/{name}/test`、`POST /api/channels/{name}/sync-models`
- 车道（唯一路由入口，ADR 0005）：`GET /api/lanes`、`GET|PUT|DELETE /api/lanes/{name}`、
  `GET /api/lanes/{name}/health`、`POST /api/lanes/{name}/probe`、
  `POST /api/lanes/{name}/circuits/reset`、`POST /api/lanes/cleanup-members`
- 模型路由：`GET /api/models`（全部路由键）、`GET /api/routes/{model}`（成员链；
  未配车道时返回候选建议链，只作"可添加成员"，不参与运行期路由）
- 客户端密钥：`GET|POST /api/keys`、`GET|PUT|DELETE /api/keys/{name}`、
  `POST /api/keys/{name}/rotate`（权限用 `lane_policy:{mode:"all"|"allow",allow_lanes,deny_lanes}`，
  **没有 `allowed_models` 字段**；allow/deny 里的键必须是真实存在的路由键，否则 422）
- 观测：`GET /api/logs`、`GET /api/logs/{id}`、`POST /api/logs/prune`、
  `GET /api/stats`（`group_by=lane|channel|key|model|channel_model`）、`GET /api/route-events`（SSE）
- Webhook 事件通知：`GET|PUT /api/webhooks`、`POST /api/webhooks/test`、`GET /api/webhooks/deliveries`（见 §5）
- 系统：`GET|PUT /api/system/options`
- 配置生命周期：`GET /api/export`、`POST /api/import?dry_run=true`
- 审计：`GET /api/audit`
- 认证：`POST /api/setup`、`POST /api/auth/login`、`POST /api/auth/logout`、
  `GET /api/auth/session`、`POST /api/auth/password`

## 4. 典型工作流

### 4.1 建渠道 → 建车道 → 发密钥 → 端到端验证

```bash
curl -s "${A[@]}" -X PUT "$BASE/api/channels/ch-a" -d '{
  "type":"openai","base_url":"https://vendor.example/v1","key":"sk-...",
  "models":["model-1"],"enabled":true}'

# 可选：先探测上游模型清单，看差异后再落库
curl -s "${A[@]}" -X POST "$BASE/api/channels/ch-a/sync-models?dry_run=true"

# 车道是唯一路由入口：不建车道该模型不可调用（503）。
# members 数组顺序即故障切换顺序，priority 由控制台按位置生成（首位最大）。
curl -s "${A[@]}" -X PUT "$BASE/api/lanes/lane-a" -d '{
  "enabled":true,"mode":"failover",
  "config":{"member_max_attempts":2,"member_retry_interval_seconds":3,
            "member_non_stream_response_timeout_seconds":120,
            "member_stream_first_event_timeout_seconds":30,
            "member_cooldown_seconds":60,"member_affinity_seconds":0},
  "members":[{"channel":"ch-a","upstream_model":"model-1","priority":10}]}'

KEY=$(curl -s "${A[@]}" -X POST "$BASE/api/keys" \
  -d '{"name":"my-key","lane_policy":{"mode":"allow","allow_lanes":["model-1"]}}' \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["key"])')

curl -s -H "Authorization: Bearer $KEY" "$BASE/v1/chat/completions" \
  -d '{"model":"model-1","messages":[{"role":"user","content":"ping"}]}'
```

### 4.2 排障：某模型为什么失败

1. `GET /api/routes/{model}` 看成员链与来源（`explicit`=已配车道可调用，
   `unconfigured`=渠道声明但未配车道，不可调用）。
2. `GET /api/lanes/{name}/health` 看 `circuit` / `cooldown_until` / `last_error_kind`。
3. `GET /api/logs?success=false&model={model}` 看 `attempts` 链。
4. `POST /api/lanes/{name}/probe` 逐成员真实探活；必要时 `POST .../circuits/reset`。

## 5. Webhook 事件通知（PBR → 你的接收端，出站推送）

PBR 把路由运行态的故障事件（熔断/冷却/恢复）异步 POST 到你配置的 URL。
**PBR 只负责投递与契约**；你的接收端自行验签、路由、呈现——本机对接（如推给某 agent 的通知通道）在该 agent 侧配置，PBR 不内置任何特定接收端。

### 5.1 配置与端点

- `GET /api/webhooks`：读配置，`targets` 数组（`secret` 只回显掩码 `****`+末 4 位）。
- `PUT /api/webhooks`：写配置，同形状。每条 target：
  `{"name":"notify","url":"http://127.0.0.1:8645/webhooks/xxx","secret":"<随机长串>","enabled":true,"events":[]}`
  （`events` 为事件白名单，空 = 全部；`secret` 留空 = 保留原值。）
- `POST /api/webhooks/test`：向指定 target 同步发一条测试事件，响应即投递结果。
- `GET /api/webhooks/deliveries`：投递记录（cursor 分页，按 ts 倒序），排障用。

### 5.2 请求体

```json
{
  "type": "pbr",
  "text": "[PBR] lane-a/ch-a:model-1 熔断打开（60s）：连续失败 hard_auth",
  "event": {"ts":1789600000000,"type":"circuit_open","lane":"model-1",
            "member":"ch-a:model-1","detail":"open_seconds=60 score=2"}
}
```

`event.type`：`circuit_open`（熔断打开）、`circuit_half_open`（半开探测开始）、
`circuit_closed`（恢复）、`cooldown`（进入冷却）。`ts` 毫秒时间戳，`member` = `channelId:upstreamModel`。

### 5.3 验签（接收端必做）

请求头：`X-Webhook-Timestamp`（Unix 秒）、`X-Webhook-Signature-V2`（hex）。

```
expected = HMAC_SHA256(secret, "{X-Webhook-Timestamp}.{原始请求体字节}").hex
```

用常数时间比较；`X-Webhook-Timestamp` 与本地时间偏差 >±300s 拒收（防重放）。
Python：`hmac.new(secret.encode(), f"{ts}.".encode()+raw_body, hashlib.sha256).hexdigest()`。

### 5.4 投递语义

- 超时 8s；非 2xx/超时按 5s/30s/120s 退避重试 3 次，耗尽记 `deliveries` 死信。
- 防风暴：同一 (target, lane, member, event) 60s 窗口只发一条。
- `2xx` 即送达，响应体不参与语义。

## 6. 错误模型与硬规则

- 成功体是**裸资源**（无信封）；动作类端点是 `{"changed":n}` / `{"deleted":true}` / `{"reset":n}` 这类语义化最小对象。
- **`?dry_run=true`（强制声明）**：每个配置类写端点必须显式声明三分类之一——
  `preview`（返回 `{dry_run:true,valid:true,diff:{...}}`，**绝不落库**）、
  `reject`（**400 `dry_run_not_supported`** 且不执行）、
  `irrelevant`（非配置类：只读/运行态/上游动作/任务触发，参数被忽略）。
  **不存在"声明不支持却仍按真实请求执行"的端点**。批量/破坏性操作请先 `?dry_run=true` 看 diff。
  注意：探活（`test`/`probe`）与 `/auth/*`、`/setup` 不适用本约定。
- **两个面的错误体不同**：管理面 `/api/*` 是 `{"error":{"code","message","hint"?,"details"?}}`，**按 `code` 分支**；
  模型面 `/v1/*` 是 OpenAI 兼容体（`{"error":{"message":"..."}}`），通常**没有** PBR `code`，按状态码判定。
  管理面 `details` 用于机器可判定明细：车道引用守卫的 `blocked`（渠道名 → 车道名）、`lanes`（引用被移除模型的车道名）、`unknown`（不存在的名字）、`failed_files`（日志清理失败文件）。
- 模型面 `503` = `No available channel for model <X>`（没有可用渠道，或该键未配车道）；
  本机繁忙是 `529`，两者不要混。
- **PUT 语义**：渠道 PUT 是**部分合并**（字段缺席 = 保持原值）；车道 PUT 的 `enabled`/`mode`/`config`
  也是部分合并，但 **`members` 是整体替换且省略即空**——`{"enabled":false}` 会 200 并**清空成员链**，
  结果仍为启用且没带 `members` 才报 `422 lane_has_no_members`。改开关/六键要保留成员时，务必带上完整数组。
- `hard_auth` / `hard_quota` 类失败的成员冷却 = **2 × `member_cooldown_seconds`**。
- `GET /api/routes/{model}` 对不存在的模型返回 `200` + `source:"unconfigured"` + `routable:false`（**不是 404**）。
  **无同名车道时推荐链在 `members[]`、`candidates` 为空**；有显式车道时 `members` 才是真实成员链、
  `candidates` 是"已启用、声明该键、非成员"的渠道。
- 日志 `attempts[].status` 取值含 **`cooldown`**（成员正在冷却、本轮未打上游），与 `failed`（上游失败）要区分。
- `sync-models` 上游返回空清单默认 `409`（需 `?force=1`）；被显式车道引用的模型移除也 `409`。
- 渠道删除被显式车道引用时 `409`，message 给出车道名。
- 写操作响应前服务端已回读，但跨请求仍应 `GET` 校验最终状态。
- `/api/export` 不含密钥明文/哈希；备份恢复直接备份 `pbr.db`（含 `-wal`/`-shm`）或 `POST /api/import`。
- 运行态（冷却/熔断/亲和）是进程内的，重启即清空。
