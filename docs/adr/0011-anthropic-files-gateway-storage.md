# ADR 0011: Anthropic Files 由网关侧存储，不改路由语义

- 状态：已接受（2026-11-20）
- 关联：[ADR 0010](0010-async-objects-lane-header.md)（OpenAI Files/Batch 并存）、[ADR 0005](0005-lane-required-and-channel-model-mapping.md)、[design §4.1](../design-v1.md)、[design §4.2](../design-v1.md)、[routing-spec §4.5](../routing-spec-v1.md)

## 背景

Anthropic 客户端（Claude Code、DSH 等）在消息里引用图片/PDF 时有两条路：内联 `source.type=base64`，
或先 `POST /v1/files` 上传、再用 `source.type=file` + `file_id` 引用。第二条是 Anthropic Files API
（`anthropic-beta: files-api-2025-04-14`）。

PBR 此前只实现 OpenAI Files 语义（ADR 0010）：`/v1/files*` 必须带 `X-PBR-Lane`，并把请求**透传到
上游渠道**。Anthropic 客户端不会发这个自定义头，因此每次上传都被 `400 lane_header_required` 拒绝。

线上的实测后果（2026-11-20，v1.1.7）：一天 4319 次 `/v1/messages` 对 428 次 `/v1/files`，
后者**全部 400、零成功**。客户端随后在同一个对话轮内降级为内联 base64——对话功能未受损，
但每次传图都白付一次上游往返、并往错误统计里灌 400 噪音。

上游 new-api（`main@3a407cc9`，2026-11-18）**没有**这块能力：`/v1/files` 四条路由全部是
`controller.RelayNotImplemented`（501），`ClaudeMessageSource` 也没有 `file_id` 字段。
因此本决策没有上游可参照，必须自主设计。

## 决策

### 1. 上传不产生路由，因此不需要车道

核心判断：**Anthropic Files 上传不是模型调用**。

- 上传只把字节暂存起来，供后续 `/v1/messages` 引用；
- 真正决定「打哪个渠道」的是那条 messages 请求，它照常按 `model` → 车道选路；
- 文件字节从不上行到厂商（展开成 base64 内联进 messages 请求体），因此不存在
  「这个对象固定在哪家上游」的问题，也就不需要 ADR 0010 的创建即固定语义。

所以：**Anthropic Files 路径不要求 `X-PBR-Lane`，也不参与选路**。这不违反 ADR 0005
「车道是唯一路由入口」——那条规则约束的是**模型调用**，而这里没有模型调用发生。

### 2. 两种 Files 语义按 beta 头分流，共存于同一组路由

| 请求 | 语义 | 寻址 |
|---|---|---|
| `anthropic-beta` 含 `files-api-` | Anthropic Files（本 ADR） | 不需要头，归属按客户端密钥 |
| 其它 | OpenAI Files（ADR 0010 原样保留） | **必须** `X-PBR-Lane` |

对象 ID 用 `pbrfile-<24 hex>` 前缀，与上游 OpenAI 对象 ID 天然隔离；retrieve / delete / content
按前缀分流，无歧义。归属不匹配一律 `404 file_not_found`——不泄露对象是否存在。

### 3. 这是「不落盘」的唯一显式例外，边界写死

ADR 0010 与 design §4.2 承诺「上传/下载按流代理，不落盘」。Anthropic Files 必须打破它：
字节要跨请求存活，才能被后续 messages 引用。

例外边界（任一越界即视为设计漂移，必须重新走 ADR）：

- **只存客户端显式上传的字节**，不做任何隐式抓取（不下载 URL、不缓存上游响应）；
- 单文件上限 **64 MiB**，总量上限 **1 GiB**；超限返回 `413`（总量超限前先跑一次过期清理）；
- 过期时间取自客户端 `expires_after`，缺省 **24h**、上限 **7d**；
- 归属绑定**客户端密钥**（`token_id`），跨密钥读取一律 404；
- 字节**不进数据库**（只存元数据行）、**不进日志正文**、**不进 `/api/export`**；
- 过期清理走既有系统任务（`file_cleanup`），不引入 cron、不引入常驻 worker；
- MIME 白名单：`image/*`、`application/pdf`、`text/plain`、`text/csv`；其余 `415`。

存储目录默认在 `<SQLITE_PATH 所在目录>/pbr-files`（与 `privacy/` 模型目录同一约定），
可用 `PBR_FILES_DIR` 覆盖。目录 `0710`、文件 `0600`，与 `pbr.db` 的权限收紧口径一致。

## 后果

- **file_id 必须被展开**：`/v1/messages` 收到 `source.type=file` 时，网关按 `file_id` 查出字节、
  原地改写成 `source.type=base64` 再走协议转换。这要求 `dto.ClaudeMessageSource` 增 `file_id` 字段
  ——在此之前该字段在反序列化阶段就被丢弃，转换器连看都看不到。
- **展开失败必须是明确错误**：file_id 不存在/已过期/跨密钥 → `400 invalid_request_error`，
  message 指明对象与原因。Anthropic 客户端的约定是收到这类 400 后重传并重试；
  静默省略会让客户端拿到缺少图片的回答而毫无察觉（这正是本 ADR 之前的现状）。
- **转换器的丢弃分支保留**：`to_oai_chat_req.go` 里 `source.Type == "file"` 的「省略 + 诊断」是
  兜底防御——展开后的请求不该再出现 `file`；若出现说明宿主漏展开，此时宁可记诊断也不崩溃。
- **upstream 能力无关**：所有 13 个渠道都是 `openai` 类型、都没有 Anthropic Files API，
  但这不影响本方案——字节在上行前已被展开成内联 base64。
- 新增 `pbr_files` 表与 `pbr-files` 目录，需要备份说明（数据卷 `/data` 已覆盖）与清理任务。
- 与 ADR 0010 的 `pbr_async_objects` **完全独立**：两套表、两套清理任务、两套归属判定，互不复用。

## 备选方案（未采用）

- **要求客户端改发内联 base64**：等于让客户端关掉 Files API。能消除噪音，但 PBR 作为
  「Anthropic 兼容」网关仍然缺这个能力，换任何别的客户端（Claude Code、Cursor）都会再踩一次。
- **`X-PBR-Lane` 改成可选、缺省取最近使用车道**：与 ADR 0005「不做隐式选路」直接冲突，
  且多车道时不可预测。
- **透传给上游**：现有 13 个渠道全是 OpenAI 兼容协议，没有 Anthropic Files API，必然 4xx/404。
- **存 SQLite BLOB**：备份更简单（单文件即完整），但大二进制会放大 WAL 与备份体积；
  文件是缓存性质，丢失可由客户端重传，不值得进主库。
- **进程内 LRU**：完全不落盘，但重启即失效，等于要求客户端每次重启后重传全部 file_id。
