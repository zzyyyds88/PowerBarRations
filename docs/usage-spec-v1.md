# PowerBarRations 用量与会话规格 v1

> 规范性文件，从属于 [`design-v1.md`](design-v1.md) §8 与 §16.5。
> 本文定义**网关视角的用量元数据**：每条请求归属哪个客户端、哪次会话，请求的 prompt
> 由哪些部分组成，以及会话/上下文如何聚合、存储与对外暴露。
>
> **只存元数据**：不存 prompt/回复正文，不存工具参数原值，不存文件内容。上下文分区只保留
> **名称、标签与 token 数**。本规格不引入额度、计费、订阅或用户体系。
>
> 脱敏（发给上游的正文替换与还原）不在本规格范围内，见后续独立规格。

---

## 1. 目标与非目标

### 1.1 目标

- 让控制台回答四个问题：**谁在调用（客户端）**、**这次/这次会话用了多少**、**花在哪个模型上**、
  **上下文窗口被什么占满**。
- 所有派生数据必须可追溯到 `pbr_request_logs` 明细；聚合表长期保留，明细按既有保留策略清理。
- 会话与上下文都是**尽力而为的观测**：解析失败绝不影响转发；拿不到就留空并给出明确状态，
  不允许伪造成 0 或猜测。

### 1.2 非目标

- 不读取客户端本机的 agent 会话文件（Claude Code JSONL、Codex rollout 等）；PBR 只从经过
  网关的请求推导会话。magpie 的「读取本机会话、恢复会话、导出对话」不在本规格内。
- 不做会话内容回放：没有 prompt/回复正文，就没有"打开会话看聊天记录"。
- 不做额度/套餐/订阅窗口；magpie 的 Allowances 卡片不属于 PBR 产品边界（design-v1 §1.3）。
- 不做多节点汇总；会话与上下文只来自本实例 SQLite。
- 不做意图路由或按上下文自动选模；选路仍只由车道与 `internal/route` 决定（ADR 0005）。

---

## 2. 术语

| 术语 | 含义 |
|---|---|
| 请求 | 一条 `pbr_request_logs` 行，对应一次模型面请求（含重试链，但只有一行） |
| 会话 | 客户端自己命名的一段连续对话；由 `session_id` 标识，跨请求、跨模型、跨渠道 |
| 客户端 | 发起请求的 agent/工具，按 `User-Agent` 归一到稳定标识；只用于展示与聚合 |
| 上下文 | 一次请求的 prompt 的组成，按 7 个分区统计 token |
| 分区 | `system` / `tools` / `skills` / `memory` / `files` / `results` / `chat` |
| 物品 | 分区内的一个可命名项：一个工具、一个 MCP server、一个文件、一轮对话等 |
| 计数 | 上游返回的 prompt token 数（`usage`）；上下文据此校准 |
| 上下文窗口 | 上游模型的最大上下文 token 数；来自模型目录元数据，0 = 未知 |

---

## 3. 会话归属

### 3.1 提取顺序（先命中先得）

对每个模型面请求，按以下顺序取会话标识：

1. **PBR 显式头** `X-PBR-Session`（trim；≤128 字节）。**该头只在本机消费，不转发给上游**；其他会话头按客户端原样转发，网关只读取不修改。
2. **客户端自报的会话头**（按顺序）：
   `x-opencode-session`、`x-session-affinity`、`x-session-id`、`session_id`、`session-id`、
   `x-claude-code-session-id`、`x-conversation-id`、`x-deepseek-harness-session-id`。
3. **请求体 `prompt_cache_key`**（OpenAI Chat / Responses）：取 trim 后的非空值，加前缀
   `pck:` 作为会话标识（与 header 同名值不混淆）。
4. **会话 opening 指纹**：`pbr-` + `sha256(opening)[:24 hex]`。opening 的取法与 magpie
   `conversationID` 一致：从 body 的 `messages`/`input`/`contents` 取对话项，找到第一条
   user 消息（Gemini 缺省 role 视为 user），从它开始归一化；若首条消息是 agent 代写的
   （指令文件、环境块、提醒等，`userWords` 判为空），则把其后的 user 消息一并纳入 opening，
   直到第一条真正含用户内容的 user 消息。opening 只有一条消息时哈希其原始字节；多条时按
   `writeOpening` 归一（对象键排序、去 `cache_control`、单文本 part 取文本）后以 `\n` 连接。
   body 无法解码时哈希整个 body。首条消息只有图片/文件也构成 opening（不要求文本）。

`session_id` 原样保存（UTF-8 字节截断到 128）；`session_key = hex(sha256(session_id))[:32]`
（16 字节，URL 安全），用于索引与 API 路径。两者都为空表示"未归属"，该请求不进入会话聚合。

> 显式头优先于指纹：客户端明确说了是哪次会话，就不猜。指纹与 magpie 同义：同一次对话的
> 每一轮都重发同一段开头，因此哈希稳定；首条用户消息完全相同的两次对话会合并，这是已知
> 取舍，客户端可通过显式头发送精确的会话 ID 来避免。agent 代写的首条消息（同一文件夹内
> 每条会话都一样）不会把不同会话合并：opening 会继续到用户真正说的那句。

### 3.2 opening 归一

按 magpie `conversationID`/`writeOpening` 的语义：

- 从 body 的 `messages`、`input`、`contents` 取对话项；`messages` 为空时用 `contents`
  （Gemini，缺省 role 视为 user），再为空时用 `input`。
- 从第一条 user 消息（或 Gemini 缺省 role 的 content）开始；其后的 `system`/`developer`
  消息跳过。
- 若 opening 目前只有首条消息，且该消息不含用户自己的内容（`userWords` 为空：指令文件、
  `<environment_context>`、`<system-reminder>`、插件/任务通知等 agent 代写内容），则把
  后续 user 消息加入 opening，直到遇到第一条含用户内容的 user 消息；遇到 assistant、
  工具调用/结果、或 tool_result 结束。
- 归一化：单条 opening 直接使用该消息的原始 JSON 字节（去掉首尾空白）；多条 opening 按
  `writeOpening` 写入：对象成员按 key 排序、去掉任意深度的 `cache_control`、`content`
  只有一个文本 part 时取其 `text` 原始字符串、数组保序、字符串/数字保留原拼写，消息之间以
  `\n` 分隔。
- body 无法解码时，哈希整个 body。
- 首条消息只有图片/文件也构成 opening；不要求存在文本。

opening 为空（body 无 `messages`/`input`/`contents` 可识别项）→ 不生成指纹会话。

### 3.3 子代理与父会话

- 子代理：`x-claude-code-agent-id` → `subagent`；`x-claude-code-parent-agent-id` →
  `parent_subagent`（各 ≤128 字节）。子代理请求归属**同一个会话**，不另开会话。
- 父会话：`x-codex-parent-thread-id` → `parent_session_id`（≤128 字节），用于展示派生关系；
  不改变本请求的会话归属。
- 以上字段缺失为空，不做推断。

### 3.4 适用范围

- 只有模型面请求参与会话；管理动作（`route_source=test`）与本地拒绝路径不参与。
- 会话头/`prompt_cache_key` 对所有入站格式生效；指纹兜底只对四种 chat-like 格式
  （OpenAI Chat、OpenAI Responses、Anthropic Messages、Gemini generateContent）生效。
- WebSocket realtime、图片、音频、嵌入、重排、审核只可能有显式会话头，不做上下文分析。

---

## 4. 客户端标识

`client` 的取值遵循 magpie 的 `AgentOf` 语义：先按已知 agent 的 id/别名匹配，再按 UA 前缀匹配；
未命中时保留 UA 的第一个 token 作为产品名。它只作展示与聚合，**不参与鉴权、选路或限流**。

1. 取 UA 的第一个 token（按 `/` 与空格切分，转小写）。
2. **已知 agent 的名字**（id 与别名）精确命中 → 对应 id。别名按 magpie 目录：例如
   `cc`/`claude-code` → `claude`，`qwen-code`/`qwen-cli` → `qwen`，`kimi-code`/`kimi-cli` → `kimi`，
   `gh-copilot` → `copilot`，`cursor-agent` → `cursor`，`factory`/`factory-droid` → `droid`，
   `gemini-cli` → `gemini`，`z-code` → `zcode`。
3. **已知 agent 的 UA 前缀**命中 → 对应 id（如 `claude-cli`/`claude-code` → `claude`，
   `codex`/`acp-extension-codex` → `codex`，`geminicli`/`gemini-cli` → `gemini`，
   `qwencode` → `qwen`，`kimicli`/`kimi-code-cli` → `kimi`，`github-copilot`/`copilot` → `copilot`，
   `cursor` → `cursor`，`zcode` → `zcode`，`hermes-agent` → `hermes`）。
4. **HTTP 库/运行时/SDK** 的 UA 保留其产品名（如 `curl`、`python-httpx`、`go-http-client`、
   `openai`），**不改写成 `script`**；`script` 是展示分组，不是存储值。
5. 其他非空 UA → 第一个 token 原样（小写，≤64 字节）。
6. UA 为空 → `other`。

**展示分组**：`client_group = script` 当 `client ∈ {other}` 或 `client` 属于 HTTP 库/运行时/SDK
（`IsLibrary`）；否则 `client_group = client`。`client_name` 是已知 agent 的显示名（如
`Claude Code`、`Codex`），库保留产品名，`other` 显示 `Other`。这两个字段由后端计算，控制台
不得自行维护第二张客户端表。

`client` 为空只出现在管理动作上；模型面请求至少是 `other`。已知客户端表是**代码内常量**，
新增客户端只需扩表；不得把 UA 原文写入日志（只存归一后的 `client`）。

---

## 5. 上下文组成

### 5.1 分区

按固定顺序展示：

| 分区 | 含义 |
|---|---|
| `system` | agent 自己的系统提示、developer 消息、harness 指令 |
| `tools` | 工具定义；同一 MCP server 的工具归一个物品（`tag=mcp`） |
| `skills` | agent 列出的技能目录（名称 + 用途），不展开技能正文 |
| `memory` | 指令文件（CLAUDE.md / AGENTS.md 等）、系统提醒 |
| `files` | 工具读取的文件、图片、文档 |
| `results` | 工具调用的其他输出（shell 输出、搜索结果等） |
| `chat` | 用户与模型的对话回合、模型的工具调用 |

`skills` 与 `memory` 的区分按 agent 的表述启发式判断；判断不了归 `memory`。分区规则只影响
展示，不影响转发。

### 5.2 解析

- 只解析四种 chat-like 入站格式；其他格式 `context_status=unsupported`。
- 解析**入站原始 body**（协议转换前），只读、不修改、不影响转发与重试。
- 会话指纹与上下文各自只读一次 body，互不依赖；两者都不修改 body，也不影响转发与重试。
- 解析失败、body 过大、格式不支持都只写状态，不让请求失败；状态值见 §5.6。

### 5.3 token 估算

- 文本：`ceil(ASCII 字节数 / 4) + 非 ASCII 字符数`（CJK 约 1 token/字符）。
- 原始 JSON 片段：`ceil(字节数 / 4)`。
- 图片：固定 1600 token。
- 文档/附件：可提取文本的按文本估算；否则按 `ceil(字节数 / 4)`。
- 分区 token = 其物品 token 之和；上下文 token = 各分区之和。
- 估算值写入 `estimated_tokens`，不覆盖上游计数。

### 5.4 校准

请求结束时，若上游返回的 prompt token 数 `counted > 0`：

1. 按 `scale = counted / estimated_tokens` 等比缩放每个分区与物品的 token（四舍五入）。
2. 缩放后的舍入误差全部加到**最大的分区**上，保证各分区之和恰好等于 `counted`。
3. 写 `tokens=counted`、`counted=true`。
4. 上游未返回 prompt token（流中断、错误、非 chat）→ 保留估算值，`counted=false`。

上下文窗口 `window` 在选路完成后按模型目录元数据解析：**优先用成员所选模型名（`member_model`），为空时回退 `upstream_model`、再回退 `request_model`**；匹配规则沿用模型目录的 **精确 > 前缀 > 后缀 > 包含**。查不到为 0。窗口只影响展示，不参与选路或拒绝。

上游持有会话（Responses 的 `previous_response_id` 等）时 `held=true`；此时各分区仍是本地可见
部分的估算，校准后总和等于上游计数，差值由 `chat` 分区吸收（校准规则已覆盖）。

### 5.5 物品与上限

- 每个分区最多 24 个物品；超出时：
  - `chat`：保留最近 23 轮 + 一个 `{name:"earlier", tag:"turns", n:<省略轮数>}` 汇总物品。
  - 其他分区：保留 token 最大的 23 个 + 一个 `{name:"other", tag:"more", n:<省略数>}` 汇总物品。
- 物品名称（工具名、MCP server 名、文件路径、轮次）≤256 字节，按 UTF-8 边界截断。
- 上下文 JSON ≤64 KiB；超出时丢弃物品明细（保留分区总数），置 `truncated=true`。
- 只存名称/标签/计数/token，**绝不存文本、工具参数、文件内容**。

### 5.6 状态

`context_status` 取值：

| 值 | 含义 |
|---|---|
| `ok` | 解析成功（可能仍为估算值） |
| `unsupported` | 非 chat-like 格式，未解析 |
| `too_large` | body 超过分析上限，未解析 |
| `unavailable` | 解析失败或 body 不可读；不影响请求 |

分析上限取 `min(PBR 请求体上限, 16 MiB)`；超限时 `too_large`，不截断后猜测。

---

## 6. 存储

### 6.1 `pbr_request_logs` 新增列

| 列 | 类型 | 说明 |
|---|---|---|
| `session_id` | varchar(128) | 原始会话标识；空 = 未归属 |
| `session_key` | varchar(32), index | `sha256(session_id)` 前 16 字节的十六进制 |
| `subagent` | varchar(128) | `x-claude-code-agent-id` |
| `parent_subagent` | varchar(128) | `x-claude-code-parent-agent-id` |
| `parent_session_id` | varchar(128) | `x-codex-parent-thread-id` |
| `client` | varchar(64), index | §4 的归一客户端 |
| `context_tokens` | int | 校准后的上下文 token；0 = 无 |
| `context_window` | int | 模型上下文窗口；0 = 未知 |
| `context_counted` | bool | 是否已按上游计数校准 |
| `context_status` | varchar(16) | §5.6 |

索引：`(session_key, ts)` 供会话详情与聚合；`(client, ts)` 供客户端过滤。

### 6.2 `pbr_request_contexts` 新表

一条日志最多一行，主键 `log_id`：

```
log_id, version, status, tokens, estimated_tokens, window, counted, held, turns,
parts_json, body_bytes, truncated, created_at
```

`parts_json` 是 §5.1 的分区数组（分区 → 物品）。明细日志被 `POST /api/logs/prune` 或内置每日
`log_cleanup` 任务清理时，按 `log_id` 同批删除对应上下文行；聚合表不受影响。明细与上下文必须
在**同一事务**内写入，避免"日志存在但上下文缺失"的静默缺口。上下文不进入日志导出
（CSV/NDJSON 都不含），避免把文件路径等明细带进导出文件；需要时经 `GET /api/logs/{id}` 读取。

### 6.3 聚合表扩展

`pbr_stats_hourly` 新增列（全部为 sum）：

| 列 | 说明 |
|---|---|
| `input_tokens` | 归一输入总量：`prompt + (usage_semantic=anthropic ? cache_read + cache_write : 0)` |
| `cache_read_tokens` | 缓存命中 token |
| `cache_write_tokens` | 缓存写入 token |
| `reasoning_tokens` | 推理 token |
| `ttft_ms_sum` | 流式首字耗时之和（毫秒） |
| `ttft_samples` | 有首字耗时的样本数 |

新增维度 `group_kind=client`（键 = `client`，空值跳过）。`client` 存 `AgentOf` 的原始结果
（库保留 `curl`/`python-httpx` 等产品名）；API 的 `client_group` 把库与 `other` 归为
`script`，`client_name` 给已知 agent 的显示名——两者都是展示字段，不改变存储键。存量聚合行
的新列为 0；
`POST /api/stats/rebuild` 从明细重算后补齐。

**写入与重建**：明细事务只写 `pbr_stats_outbox`，常驻 worker 逐行在独立事务内 upsert 全部维度
并删除 outbox 行（崩溃安全、不重复计数）；`/api/stats` 因此最多滞后约 2s + 处理时间。
`POST /api/stats/rebuild` 先 drain outbox，再只重算仍有明细覆盖的小时桶；prune 边界及更早的
桶整桶保留（响应 `preserved_buckets`），不会因为明细被清理而抹掉历史聚合。

**缓存命中率**（只在 UI 计算，API 只给原始和）：

```
hit_rate = cache_read_tokens / input_tokens   (input_tokens > 0)
```

`input_tokens` 已按 `usage_semantic` 归一：
- `openai` / `gemini` / 空值：prompt 已包含 cache_read，`input_tokens = prompt`。
- `anthropic`：prompt（input_tokens）不含 cache_read/cache_write，三者相加才是总输入。

**输出速度**沿用现有口径（成功、单次尝试、流式扣首字），本规格不新增聚合列。

---

## 7. API 契约

端点、参数、响应字段以 [`api-spec-v1.md`](api-spec-v1.md) §4.4/§5.5/§5.5.1/§5.7 为准，
本节只固定语义：

- `GET /api/logs`：新增过滤 `client`、`session`（`session` 接受 `session_key` 或完整
  `session_id`）；列表项新增 `client`、`session_id`、`session_key`、`subagent`、
  `parent_subagent`、`parent_session_id`、`context_tokens`、`context_window`、`context_status`；
  不含上下文分区明细。
- `GET /api/logs/{id}`：在列表项基础上返回完整 `context`（分区 + 物品）或 `null`。
- `GET /api/logs/sessions`：按 `session_key` 聚合；时间范围默认最近 30 天；支持
  `client`/`model`/`lane`/`q`/`sort`/`order`/`page`/`page_size`；按 `last_ts` 降序默认。
- `GET /api/logs/sessions/{key}`：会话摘要 + 分页请求列表（每项含紧凑上下文：分区 token，
  不含物品）+ `context_series`（最多 500 个点，超出置 `context_series_truncated=true`）。
- `GET /api/stats`：`group_by` 新增 `client`；桶新增 `input_tokens`、`cache_read_tokens`、
  `cache_write_tokens`、`reasoning_tokens`、`ttft_ms_sum`、`ttft_samples`。
- `PUT /api/model-metadata/{model}`：body 新增 `context_window`（整数 ≥0，0=未知）；
  `GET /api/model-metadata` 返回该字段。

---

## 8. UI 语义

以 [`ui-spec-v2.md`](ui-spec-v2.md) §6 为准：

- 「概览」增加缓存命中率、客户端分布、用量日历；不新增一级导航。
- 「日志」增加「会话」页签；请求列表增加客户端/会话列与筛选；请求详情增加上下文卡片。
- 会话详情包含摘要、上下文卡片、上下文填充曲线、请求时间线。
- 模型目录编辑面增加「上下文窗口」；0 显示 `—`，不显示 0%。
- 没有完整上下文时显示 `—` 与原因（估算/不支持/过大/不可用），不得显示 0% 或伪造窗口。

---

## 9. 验收

闸口与复跑矩阵见 [`test-spec-v1.md`](test-spec-v1.md) §8。最低覆盖：

- 会话提取优先级（显式头 > 客户端头 > `prompt_cache_key` > 指纹）与指纹稳定性。
- 同一会话跨模型/渠道/格式（Chat ↔ Anthropic）保持同一 `session_key`。
- 客户端 UA 映射（含库 → `script`、未知 UA、空 UA）。
- 四种协议的上下文分区解析、token 估算、校准后总和等于上游计数、物品裁剪与 64 KiB 上限。
- 解析失败/超大 body 不影响转发，状态可区分。
- 会话聚合分页、排序、时间范围默认值、`404 session_not_found`。
- 统计归一（anthropic vs openai）与缓存命中率边界（input_tokens=0 不给 0%）。
- `logs/prune` 同时清理上下文，聚合不变。
- 模型目录 `context_window` 写入/回读/规则匹配。
