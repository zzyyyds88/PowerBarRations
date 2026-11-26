# PowerBarRations 设计基线 v1

> 立项：2026-09-14。本文是 PowerBarRations 项目唯一有效的产品与技术设计基线（SSOT）。
> 项目性质：通用的本机多模型聚合与运维网关。它替代 Hermes 原先依赖的
> `new-api + octopus` 两层网关；复用 new-api 的转发管道、厂商适配层与前端基础，
> 用 PBR 的显式车道把多组散装 token 组织成故障转移模型。Hermes 是首要消费者，
> 但其他工具和模型使用同一模型面。
> 配套规范（均为规范性文件，与本文同级）：
> - 管理 API 完整契约：[`api-spec-v1.md`](api-spec-v1.md)
> - 路由与故障转移：[`routing-spec-v1.md`](routing-spec-v1.md)
> - 令牌与认证：[`token-spec-v1.md`](token-spec-v1.md)
> - UI 控制台：[`ui-spec-v2.md`](ui-spec-v2.md)（信息架构）；组件级细节见 [`ui-spec-v1.md`](ui-spec-v1.md)
> - 用量与会话：[`usage-spec-v1.md`](usage-spec-v1.md)
> - Hermes 适配：[`hermes-spec-v1.md`](hermes-spec-v1.md)
> - 测试规格：[`test-spec-v1.md`](test-spec-v1.md)
> 冲突时：本文管架构与取舍，各 spec 管本领域细节契约。
>
> **文档纪律（重要）**：本文及配套规范只描述系统本身，**不含任何部署私有数据**——不写具体渠道名、模型名、车道名单、厂商地址、凭据、成本单价。这类内容属于部署方自己的运维配置，不入仓库。

---

## 0. 相对 brief v1 的改动摘要

> 立项期的决策过程与逐项理由已归档至 [`archive/design-v1-history.md`](archive/design-v1-history.md) §0；其中"隐式链自动成链"一条已被 [ADR 0005](adr/0005-lane-required-and-channel-model-mapping.md) 推翻。现行结论体现在本文 §1（目标/删除/保留范围）与各配套 spec。

---

## 1. 定位、目标与非目标

### 1.1 定位

**个人纯自用、单用户的通用 LLM 聚合网关。** 使用者只有一个人（以及代表他工作的 AI）；
下游包括 Hermes 和其他模型工具。PBR 保留 new-api 中与渠道、协议适配、模型管理、日志、统计、
控制台和正确转发有关的能力，只去掉本项目明确不需要的多用户与计费部分。

### 1.2 目标

- **G1 单层**：一个二进制、一个 SQLite、一条管道。车道（Lane）是唯一路由入口，成员 = `(渠道, 所选模型, 优先级)`。不再有第二层网关；上游改名**只由渠道级 `model_mapping` 承担**（成员级改名已移除，[ADR 0008](adr/0008-member-stores-selected-model.md)）。
- **G2 AI 友好**：全部管理能力以版本化 HTTP API 暴露，自描述（OpenAPI）、幂等、可回读、错误可机器判定；AI 不改文件、不查库、不抓前端。完整契约见 api-spec-v1.md。
- **G3 稳定模型面**：车道名与 `/v1/*` 协议、错误语义保持稳定；客户端只需按配置使用 PBR 的 base_url。
- **G4 自愈**：保留现行路由层已验证的成员冷却 + 亲和，并补上厂商层"只禁不通、无半开自愈"的缺陷（new-api 上游 issue #5420）。软故障（限流）不得误判为硬故障。
- **G5 保留厂商适配层**：new-api `relay/channel/` 下 40 家适配器原样复用。
- **G6 有人看的控制台**：提供个人控制台，覆盖车道/渠道/密钥/日志/统计/设置/试打。规格见 ui-spec-v2.md（信息架构）与 ui-spec-v1.md（组件细节）。
- **G7 只看不扣**：日志只存元数据，成本只做折算记账，不做任何计费/扣费/余额拒服务。
- **G8 Hermes 首要验收**：Hermes 使用一条**由部署方命名**的显式故障转移车道（规范中以
  `<hermes-lane>` 占位，见 `hermes-spec-v1.md`）；Hermes 的 Chat Completions、工具调用、
  流式响应和 API 运维工作流是首要验收对象。
- **G9 隐私脱敏**：可选地把请求中的密钥与个人数据换成占位符，回复再换回；只记录命中元数据、
  不落原值/正文。规则引擎移植 magpie，本地模型使用纯 Go AstrLink Guard；默认关闭。完整语义见
  [`privacy-spec-v1.md`](privacy-spec-v1.md)。

### 1.3 删除范围（**只裁计费与多用户等下列项**，其余不裁）

> 本清单是全项目"已删除范围"的**单一汇总处**；其他章节不重复"已移除"补丁注记，只引用本节。

**计费/额度**：配额扣减、预扣费、结算、钱包、充值、兑换码、订阅与套餐、账单、阶梯计费表达式、价格倍数（ModelRatio/GroupRatio/CompletionRatio）、按量扣费逻辑、基座 `logs` 计费口径统计与计费执行链实现（W7 执行口径已归档）。

**支付**：全部第三方支付通道（stripe / creem / waffo / epay 等）。

**多用户**：用户管理、注册、多用户登录、角色与授权（casbin/authz）、2FA、passkey、OAuth 登录、邮箱验证、签到、排行榜、用户分组。

**路由与数据面**：`weighted` / `round_robin` 模式与成员 `weight`（§7.2）；渠道 `priority` / `weight` 字段（含 DB 列）；隐式成链与"声明模型自动建车道"（[ADR 0005](adr/0005-lane-required-and-channel-model-mapping.md)、[ADR 0006](adr/0006-lane-free-member-composition.md)）。

**基座子系统**：任务插件/异步生成任务子系统（JS 插件、`/v1/tasks`、任务日志、任务插件管理页）；Midjourney 全链路（`/mj` 转发、`/api/mj` 管理端点、Drawing 日志分节、轮询 handler、图片免鉴权代理）；厂商（Vendors）与 io.net 部署（Deployments）前后端；系统更新检查（直连上游 releases）；**系统信息页与多节点实例视图**（`features/system-info` 页面、`/api/system-info/**` 端点、`system_instances` 表与实例上报 reporter）——PBR 只支持单节点 SQLite（§1.5），多实例"角色/节点"面板无意义。

**单价层**：全局默认单价表（原 `PBRModelPrices`，§16.9#7）。

**对应 UI 页面删除**：wallet、pricing、redemption-codes、subscriptions、users、rankings、security(2FA/passkey)、profile、部分 home 营销页、task-plugins、model-pricing、system-update、**system-info**。About/Legal 保留用于许可证说明；完整清单见 ui-spec-v1.md §5。

### 1.4 保留范围（**默认全部保留**，不因个人自用而裁剪）

渠道、车道（新）、模型目录与厂商适配层、客户端密钥、请求日志、用量与记账统计、Dashboard、Playground、系统设置（relay 相关）、初始化向导、错误页、WebSocket 上游池。已删除的子系统清单见 §1.3。

> 除 §1.3 明确删除的范围外，保留 new-api 中有实际运维价值的能力；开发阶段不为旧功能增加兼容入口。
> **部署形态**：仅 Docker（单容器）。不考虑桌面端，不 vendor electron。
> **思考参数**：各厂商等价字段**沿用适配器现状**，不重新梳理映射表（详见 §16.1）。

### 1.5 唯一部署约束

只支持 **SQLite** 单节点部署（多数据库代码保留，但不在部署矩阵内），以避免接口分叉。

**多节点实例视图已删除**：PBR 不做多实例部署，基座继承的实例上报与系统信息页不属于产品范围。

**开发阶段数据边界**：以新建的 PBR SQLite 数据库为准。不提供旧 `new-api`/`octopus` 数据库迁移、
双写、自动回填、历史数据恢复或旧数据兼容兜底；schema 发生破坏性变化时允许重建开发库。
同版本配置导入导出若保留，只用于 PBR 配置快照，不承担旧系统迁移。

---

## 2. 现状结构基线（不含部署数据）

> 立项时对旧两层网关（路由层 + 厂商/账务层）的结构勘察——拓扑、路由层缺成员级改名、六键来源、40 家适配器规模、适配器对 `service`/`setting` 的引用分布实测、上游 provenance 更正与前端搬迁论证——已整体归档至 [`archive/design-v1-history.md`](archive/design-v1-history.md) §2。对本设计仍然有效的现行约束只有两条：**适配器不做净室剥离（§10.5）**与**前端蓝本约束（已并入 §6）**。

---

## 3. 架构

### 3.1 一句话

**一个二进制、一个 SQLite、一条管道**：**请求里的模型名就是路由键**。渠道声明自己能提供哪些模型、以及**模型名 → 上游真名**的映射（改名只在这一层，[ADR 0008](adr/0008-member-stores-selected-model.md)）；**每个模型必须有一条同名车道（成员链）才能被调用**——车道是唯一路由入口，成员按优先级 failover，宁可 503 也不直连某个上游渠道。控制台在「路由与故障切换」页手动新建车道并排定成员链；**成员可来自任意渠道的任意模型**（[ADR 0006](adr/0006-lane-free-member-composition.md)），对外模型名由人工命名、与上游真名解耦。中间不再有第二层网关。

### 3.2 结构示意（占位符为示意，非真实数据）

```
渠道/channel-a（提供 model-1, model-2, model-3）
渠道/channel-b（提供 model-1, model-2, model-3）

固化后（逐条 PUT /lanes/{model}，顺序由人工在界面上排定）：
下游请求 model-1  →  channel-a 的 model-1  →（失败）→  channel-b 的 model-1
下游请求 model-2  →  channel-a 的 model-2  →（失败）→  channel-b 的 model-2
（model-3 亦然；未固化的模型名不可调用，一律 503）

池化示例（成员可跨渠道、跨模型，无需同名；ADR 0006）：
下游请求 pool-fast → channel-a 的 vendor-a/model-1 →（失败）→ channel-b 的 vendor-b/model-2
```

```
客户端 --model=<模型名>--> PowerBarRations
                              ├─ 车道 "model-1"（唯一入口）：成员 = 渠道A(model-1) → 渠道B(model-1)
                              ├─ 车道 "model-2" / "model-3" / …（逐个固化；未固化 = 503）
                              ├─ 车道 "pool-fast"：成员可任选任意渠道的任意模型（池化；ADR 0006）
                              ├─ 车道可选高级能力：自定义顺序 / 成员改名 / 池化 / 两种模式 / 六键覆盖
                              └─ 渠道 Channel: 厂商 base_url + key + 模型清单 + model_mapping + param_override + 协议类型(40 家适配器原样复用)
                                        ▲
                    管理面(API) ────────┘
                    控制台(UI) ─────────┘  同一个二进制，UI 产物 embed 进二进制
```

### 3.3 概念映射（旧 → 新）

| 旧概念 | 新概念 | 说明 |
|---|---|---|
| 路由层 `group` / 厂商层 `group` | **Lane 车道（键 = 模型名）** | 车道是唯一路由入口；渠道声明只是候选，成员链由人工在界面上排定 |
| 厂商层 `abilities` 行 | **成员** | `(渠道, 上游模型名, 优先级)` |
| 厂商层 `channels` | **Channel 渠道** | 厂商 base_url + key + param_override + 协议类型 + `model_mapping`（路由键→上游真名） |
| 厂商层 `model_mapping` | **`Channel.ModelMapping`** | 改名**只在渠道配置一次**、对所有成员生效（成员级覆盖已移除，[ADR 0008](adr/0008-member-stores-selected-model.md)） |
| 厂商层 `tokens` / 路由层 `api_keys` | **ClientKey 客户端密钥** | 瘦身，见 §9 |
| 厂商层 `users` | **User 纯锚点行（保留）** | 多用户面已删，仅保留内部系统用户一行作记账/归属锚点（§3.4）；管理员身份只由管理密钥承载，该行不可登录 |
| 厂商层 `logs` | **RequestLog 元数据日志** | 只存元数据，见 §8 |

### 3.4 数据模型（SQLite，GORM）

```go
type Channel struct {                 // 上游渠道
    ID        int
    Name      string   // 唯一：管理面与成员按名寻址；写路径拒绝重名，启动扫描历史重名
    Type      string   // 适配器/协议族：openai|anthropic|gemini|ollama|...（映射 40 家适配器）
    Protocol  string   // 渠道级上游协议（other_settings.protocol）：openai-chat|openai-responses|anthropic|gemini；
                       // 空=按 Type 推断（旧数据）。控制台只暴露这 4 个协议（ui-spec §6.4）
    BaseURL   string   // 只存到版本根（如 https://host/v1），路径拼接交给适配器；
                       // 网关读取时会剥掉结尾版本段，故填 /v1 与不填等价（api-spec §4.1）
    Models    []string // 本渠道提供哪些路由键（候选）；声明只是候选，必须固化成车道才可调用（ADR 0005）
    ModelMapping map[string]string // 路由键 → 上游真名；上游命名不一致时在渠道上配置一次（ADR 0005）
    Key       string   // 渠道对象响应不回显（只给 key_set/key_prefix）；运维可经
                       // GET /api/channels/{name}/key 明文回读（api-spec §5.3.1）；不打印进日志
    ParamOverride string          // JSON，统一各厂商思考参数差异的落点
    Enabled   bool
    Proxy     string
    CreatedAt, UpdatedAt time.Time
}

type Lane struct { // 车道 = 唯一路由入口；没有同名启用车道 ⇒ 503（ADR 0005）
    ID      int
    Name    string   // 可路由的模型名（唯一）
    Mode    string   // failover(默认) | manual
    Config  LaneRelayConfig        // 六键，见 §7.3
    Members []LaneMember           // 有序：数组顺序即故障切换顺序
    Enabled bool
}

type LaneMember struct {
    ID            int
    LaneID        int
    ChannelID     int
    Model         string  // 该成员所选、由该渠道声明的模型名；上游真名由 Channel.ModelMapping[Model] ?? Model 推导（ADR 0008）
    PublicAlias   string  // 可选：客户端可点名该成员的名字（空 = 不可点名）
    Priority      int     // 车道内顺序，数字大者优先；界面以"上/下移"维护，写库即该值
    Disabled      bool    // 人工停用（可逆）：仍留在成员链里但不参与选路。存储与解析口径以 routing-spec §1.2 为单处规范
    // 成员级覆盖（nil = 继承车道）
    MaxAttempts                     *int
    RetryIntervalSeconds            *int
    NonStreamResponseTimeoutSeconds *int
    StreamFirstEventTimeoutSeconds  *int
    CooldownSeconds                 *int
}

type ClientKey struct {               // 客户端准入
    ID        int
    Name      string   // 唯一；分账与日志归属标识
    KeyHash   string   // hex(sha256(明文))，鉴权索引
    KeyPlain  string   // 明文入库（管理 API 可回读，token-spec §3.3）
    KeyPrefix string   // 便于展示，如 pbr-xxxx
    Enabled   bool
    LanePolicy LanePolicy // Mode=all(默认)|allow + AllowLanes - DenyLanes（token-spec §3.2）
    CreatedAt time.Time
    LastUsedAt *time.Time
}

type User struct {                    // 多用户面删除后保留的纯锚点行（内部系统用户 pbr-system）
    ID           int
    Username     string   // 唯一
    Password     string   // 不可逆 HMAC，不可登录
    Role         int
    Status       int
    Group        string
    Setting      string
    RequestCount int      // 保留列但已无写方（不再承载多用户统计语义）
    CreatedAt    time.Time
    // quota / used_quota / aff_code 三列已随多用户面物理删除（model/user_quota_migration.go）
}

type RequestLog struct { /* 见 §8 */ }
type RequestContext struct { /* 上下文分区明细，见 usage-spec §6.2 */ }
type AuditLog   struct { /* 见 §5 */ }
type Option     struct { Key, Value string }
```

**约束**：不允许出现指向 `users` 表的外键（`users` 表只保留纯锚点行，见上）；不允许出现配额字段（`quota`/`remain_quota`/`used_quota`）；不允许出现"余额不足拒服务"逻辑。

---

## 4. 对外模型 API（下游契约）

| 端点 | 说明 |
|---|---|
| `POST /v1/chat/completions` | OpenAI 兼容，流式 SSE / 非流式 |
| `POST /v1/responses` | OpenAI Responses 入口（流式 SSE / 非流式）；与 chat 共用车道语义 |
| `POST /v1/responses/compact` | Responses 压缩入口 |
| `POST /v1/messages` | Anthropic 入口 |
| `POST /v1/messages/count_tokens` | Anthropic token 估算（本地计算，不回源、不参与选路） |
| `POST /v1/embeddings` | OpenAI 兼容嵌入 |
| `POST /v1/images/generations`、`/v1/images/edits`、`/v1/images/variations` | 图像生成 / 编辑 / 变体 |
| `POST /v1/moderations` | 内容审核（透传；独立 relay mode，不混入 chat 语义） |
| `POST /v1/audio/speech`、`/v1/audio/transcriptions`、`/v1/audio/translations` | 语音合成 / 转写 / 翻译 |
| `POST /v1/rerank` | 重排 |
| `/v1/files*`、`/v1/batches*` | 异步对象（OpenAI 兼容）；**`X-PBR-Lane` 必填**，见 §4.2 |
| `GET /v1/models` | 列出当前密钥可见的车道 |

> 本表仅列常用子集；全量端点以 `router/relay-router.go` 的实注册与 `/api/openapi.json` 为准（另有 `/v1/completions`、`/v1/realtime`、Gemini 兼容等入口）。

### 4.1 必须保持的语义

- `model` 字段先按车道名解析；保留思考后缀归一化行为（`ShouldPreserveThinkingSuffix`）。
- **无可用成员时返回 `503 No available channel for model X`**（body 形态不变）。下游 fallback 分类依赖这个体，禁止改成 400/404。
- 请求成功时**响应体 `model` 回填请求名（车道名）**，不填上游真名；真实服务者通过响应头 `X-Served-By: channel=<id>:<name>, model=<upstream>` 与日志暴露。
- 点名：`model` 也可命中某成员的 `PublicAlias`，语义为"锁定该成员"，但仍**必须过冷却/熔断**。
- 车道全挂必须"错误快抛、无静默兜底、不跨车道逃逸"。
- **跨协议转换的内容损失必须记诊断**：入口协议与上游协议不同时，无法表达的内容不得静默丢弃；
  转换器把损失写进请求日志的 `admin_info.conversion_diagnostics`（`code` / `path` / `severity` /
  `message`，超过 32 条截断并标记 `conversion_diagnostics_truncated`）并发一条警告日志。
  `/v1/messages` → OpenAI Chat 的文档规则：`source.type=text` 转为文本块；`source.type=base64`
  且 `media_type=application/pdf` 转为 Chat `file` 数据 URL；`source.type=file`（`file_id` 引用）
  由网关按 [ADR 0011](adr/0011-anthropic-files-gateway-storage.md) 先行展开为 base64 再转换；
  其余无法表达的来源（非 PDF 的 base64 文档、没有可引用地址的图片）记 `unsupported_media_source`
  并省略该块；消息没有剩余内容时整条丢弃。
- **流式提交与空响应**：提交判定用数据帧口径——写出第一个 `data:` 帧才算提交，`: PING` 注释帧不算。
  一切写出 `data:` 帧的入口都必须打提交标记，否则提交后收到的流内错误会被误判成"提交前"
  （导致在已交付内容后再试故障转移），且首字耗时永远记不到。上游建流后**零有效帧**
  （读超时、立即 EOF、只有 `[DONE]`）属于上游空响应，必须按成员级失败处理，
  不得以 `200` + 成功落库——那会让客户端拿到空流且车道运行态记不到这次失败。

### 4.2 异步对象（Files / Batch）的路由例外

Files / Batch 的请求体没有 `model` 字段，无法按"路由键 = 模型名"选路；同时异步任务一旦提交就固定在某个上游，不能像同步请求那样换成员。合同见 [ADR 0010](adr/0010-async-objects-lane-header.md)，要点：

- **显式寻址**：`/v1/files*` 与 `/v1/batches*` 的请求必须带 `X-PBR-Lane: <车道名>`。缺失 → `400 lane_header_required`，名字不存在 → `404 lane_not_found`；该头不转发给上游。**唯一例外是 Anthropic Files 语义**（见下一条）。
- **创建即固定**：创建 file / batch 时，按车道当前可用成员选第一个（与同步请求同一套冷却/熔断/人工停用过滤），把 `(lane, upstream_object_id) → channel_id` 落 `pbr_async_objects`；后续 retrieve / content / cancel 只回该渠道。**不重试、不换成员、不 failover**——异步任务换上游会破坏对象归属。
- **跨车道隔离**：同一 `upstream_object_id` 只在创建它的车道可见；用别的车道寻址 → `404 async_object_not_found`，不得回落到"随便找一个渠道"。
- **渠道不可用**：记录的渠道被删除或停用 → `503 async_object_channel_unavailable`，由运维恢复渠道；不静默换渠道。
- **协议边界**：只支持 OpenAI 兼容协议渠道；其它适配器（Claude / Gemini / 签名类）→ `400 unsupported_protocol_for_async`。
- **Anthropic Files 是并存的第二套语义**（[ADR 0011](adr/0011-anthropic-files-gateway-storage.md)）：`anthropic-beta`
  含 `files-api-` 时按 Anthropic Files 处理——**不要求 `X-PBR-Lane`、不参与选路、不转发上游**，
  字节由网关自己暂存，归属绑定客户端密钥，后续 `/v1/messages` 引用 `file_id` 时展开为内联 base64。
  依据：上传不产生模型调用，因此不违反「车道是唯一路由入口」（ADR 0005）。对象 ID 带 `pbrfile-`
  前缀与上游 OpenAI 对象隔离。这是本项目**唯一**打破"不落盘"的显式例外，边界见下一条。
- **传输与上限**：OpenAI Files/Batch 上传/下载按流代理，不落盘；单请求上限 512 MiB，batch JSON 上限 8 MiB。Anthropic Files 单文件 64 MiB、总量 1 GiB、按 `expires_after`（缺省 24h、上限 7d）过期，字节不进数据库、不进日志正文、不进 `/api/export`。错误摘要按 §8 脱敏。
- **保留与清理**：OpenAI 异步对象的元数据行在上游对象过期后保留 30 天供审计；Anthropic Files 按过期时间直接删除（行 + 字节）。两者都走既有系统任务（`async_task_poll` / `file_cleanup`），不引入 cron。
- **记账**：文件/批处理不做 token 折算（上游异步计费不可见），日志只记请求元数据与成功/失败；`estimated_cost=0`。Anthropic Files 的日志 `lane`/`route_source` 为空（无选路）。

---

## 5. 管理 API（AI-first）

> 完整端点、字段、请求/响应示例、错误码、分页、AI 调用手册全部见 **[`api-spec-v1.md`](api-spec-v1.md)**。本节只定架构性约定。

### 5.1 核心约定

1. **认证**：管理面无账号体系，只有**一个登录口令**（浏览器换 HttpOnly 会话 Cookie；AI/脚本用 `管理密钥 = Base64(SHA256(登录口令))` 作 `Authorization: Bearer`，两通道等价；首启先设口令）。派生规则、会话、恢复详见 [`token-spec-v1.md`](token-spec-v1.md) §2。模型面用客户端密钥。**模型面与管理面同端口全部监听 `0.0.0.0` 对局域网开放**（§18），凭凭据鉴权，不做来源限制；口令必须是长随机串。
2. **幂等写**：`PUT /api/{resource}/{name}` 收全量对象 upsert。
3. **写后回读**：任何写操作响应前重新读库，响应体即最终状态；集成测试断言"写完 GET == 提交值"。
4. **`?dry_run=true`（强制声明，禁止静默写入）**：每个管理面写端点必须显式声明 dry-run 类别——`preview`（返回变更 diff，不落库）或 `reject`（不支持预览，带 `?dry_run=true` 时返回 400 `dry_run_not_supported` 且**不执行**），二选一。**没有第三个类别**：有副作用但无预览实现的写端点（运行态重置/GC、上游动作、任务触发、探活、改口令等）一律声明 `reject`，让调用方明确知道"要预览就得换做法"，而不是收到 200 却已改库、发请求、建任务。只读端点（GET）不参与声明。**不存在"声明不支持却仍按真实请求执行"的端点**。安全网中间件 `DryRunMiddleware` 挂在**整个管理面**（稳定面 + 控制台内部面）：写方法 + `?dry_run=true` + 未声明 `preview` ⇒ 400 且 handler 不可能绕过；守卫测试 `TestEveryManagementWriteRouteDeclaresDryRun` 遍历 `engine.Routes()` 要求每个写方法都声明、`preview` 有无副作用行为测试。逐端点清单见 api-spec §5.9。
5. **成功响应无信封**：成功即**裸资源对象**——单对象就是该对象本身，列表就是 `{"items":[...],"next_cursor":...}`；动作类端点（批量启停、清缓存、GC 等）返回**语义化最小对象**（如 `{"changed":3}`、`{"deleted":true}`）。**不存在"HTTP 200 承载业务失败"的管理端点**。
6. **统一错误包络**：`{"error":{"code","message","hint","details"}}`，`code` 为稳定字符串、失败一律带**真实 HTTP 状态码**（`details` 为可选结构化明细，如车道引用的 `blocked` 映射）。
7. **全管理面同一信封**：§5 的稳定契约端点与控制台内部接口（§9）共用上述成功形态与错误模型；两者的差别只在"字段是否随控制台实现变动"，不在信封。基座遗留路径（`/api/channel/**`、`/api/console/**`、`/api/option/**`、`/api/performance/**`、`/api/system-task/**`、`/api/prefill_group/**`、`/api/log/**`）同样收敛到该信封。
8. **分页**：cursor 分页，`limit` 有上限。
9. **审计**：每次变更写 `audit_logs`（只记元数据）。
10. **配置快照**：`GET /export` / `POST /import?dry_run=` 用于同版本 PBR 配置的导出、预览和恢复，不承担旧系统迁移。
11. **自描述**：`GET /openapi.json`、`GET /capabilities`。

### 5.2 为什么这样设计（回答"AI 怎么用"）

AI 侧的全部运维动作——建渠道、建/改车道、调成员顺序、探活、重置熔断、读日志、导出配置——都是一次 HTTP 调用，且响应可判定（HTTP 状态 + 稳定 `code` + 回读后的对象）。不需要解析 prose，不需要读库，不需要抓前端。典型工作流与可直接照抄的 curl 示例见 api-spec-v1.md §7。

---

## 6. UI 控制台

> 完整页面清单、逐页字段/交互/状态、模块映射与验收见 **[`ui-spec-v2.md`](ui-spec-v2.md)**（信息架构）与 [`ui-spec-v1.md`](ui-spec-v1.md)（组件细节）。本节只定架构性约定。

- **蓝本 = new-api 上游前端**（Rsbuild + React + TanStack Router + Base UI + Tailwind）：**直接整体搬迁，做减法（删多用户/计费）+ 接线（认证、成员链）**，而非另起炉灶。
- **认证极简**：无账号，只有登录口令；首启设置口令，之后登录换取 **HttpOnly 会话 Cookie**（浏览器不存管理密钥）。无注册/找回/OAuth/passkey/2FA。
- **页面集合**（保留上游页面，删多用户/计费）：数据看板、渠道管理、模型管理、路由与故障切换（**独立页 `/routes`**）、令牌、请求日志、系统任务（**独立页 `/logs/tasks`**）、性能指标、系统设置、试打台、关于/法律页等。**侧边栏采用分区 + 分组下拉**：控制中心放「实时路由」、路由、「渠道与模型」（渠道/渠道状态/模型目录）、API 密钥、「用量」（概览/请求/会话/上下文）、「脱敏」；系统区块放审计日志、系统任务、「设置」（站点与品牌/模型与路由/控制台内容/运维）。**实时路由是单一观察入口**：页内同时展示车道→网关→成员拓扑、各渠道短窗输出速度与近 15 分钟实时流量；路由与渠道与模型是配置面，两者不混用同一入口。**设置不再是替换侧边栏的 drill-in 页面**，进入设置域后主侧边栏保持常驻；页面内不再重复侧边栏页签。**「系统信息」页已删除**（多节点实例视图，单节点部署不需要），其任务面板提为「系统任务」独立页。
- **构建**：Rsbuild（上游默认），产物交 Go `embed`；包管理使用仓库现行的 pnpm。
- **保留/删除**：除多用户/计费外全部上游页面保留；保留与删除的完整清单见 §1.3/§1.4 与 ui-spec §5。
- **实时机制**：车道运行态经 SSE 推送 + 30s 轮询兜底（PBR 自有 `/api/route-events`）。
- **产物形态**：单进程同时服务 `/v1/*`、`/api/*` 与静态控制台。
- **品牌**：全量替换为 PowerBarRations（见 §17）。

---

## 7. 路由与容错

### 7.1 车道与成员

- **路由键 = 模型名**（[routing-spec-v1.md](routing-spec-v1.md) §1.1）：渠道声明 `Models` 只是候选来源；**没有车道就不能调用**。
- 成员：`(渠道, 所选模型, 顺序)`，支持 `PublicAlias` 点名。上游真名解析（单处规则）：`Channel.ModelMapping[成员所选模型] ?? 成员所选模型`——**没有成员级覆盖**（[ADR 0008](adr/0008-member-stores-selected-model.md)），查表键与路由键无关。
- **成员候选 = 任意启用渠道的任意已声明模型**（[ADR 0006](adr/0006-lane-free-member-composition.md)）：一条车道可跨渠道、跨模型自由组链，不要求成员声明了该路由键；**同一渠道的不同模型可出现多次**（按 `(渠道, 所选模型)` 去重，同一模型不可重复，[ADR 0008](adr/0008-member-stores-selected-model.md)）。渠道声明 `Models` 仍只是"该渠道能提供什么"的目录，**绝不自动建车道**。
- **「临时停用某一个成员」是一等人工操作**（成员级 `Disabled` 开关）：不想让某渠道承载流量时，人工关掉它，而不是删掉它、也不是调数值。它是**人工干预选路**，与六键覆盖不同类，因此**不进 `overrides`**（那是六键数值容器，把布尔开关塞进去语义就坏了）。字段口径、跳过算法与留痕见 [routing-spec-v1.md](routing-spec-v1.md) §1.2、§2、§9。
- **配置态 vs 运行态**：停用是**配置态**（落库、重启不丢），冷却与熔断是**运行态**（进程内、会自己恢复）。停用某成员**不写冷却、不改变熔断器的三态与计数**，重新打开即立即恢复参与选路——不因为"关过"而背上一个冷却或半开状态。
- **停用 ≠ 删除 ≠ 车道停用**：被关闭的成员仍留在成员链里（顺序、别名、成员级覆盖一律不动），只是不被选中，也不占用恢复探测槽；车道整体停用仍是 `Lane.Enabled=false`，其语义不受成员开关影响。
- 「模型名写错」「没建车道」「上游全挂」对下游同形：`503 No available channel for model <X>`。
- **明确不做**（防止后续漂移）：不引入成员级 `weight`，不恢复 `weighted` / `round_robin`（§7.2、[ADR 0005](adr/0005-lane-required-and-channel-model-mapping.md)，传入即 `422`）；不把成员开关做成"删除成员"的别名（需求是**可逆的临时停用**）；不改变 `Lane.Enabled`（车道级停用）的语义；不给成员加"自动停用"之类的运行态写回路径（渠道/成员的运行态回避已由 §7.5 熔断承担）。

### 7.2 选择模式（两种）

| 模式 | 语义 |
|---|---|
| `failover`（默认） | 按成员顺序选；失败按预算尝试后冷却并逃逸到下一成员 |
| `manual` | 只用手工选中的成员（沿用现行路由层语义） |

> **已删除 `weighted` / `round_robin`**：本项目是单用户自用网关，"这次为什么走了另一个上游"没有任何收益，只增加排障成本。需要打散负载时，正确做法是拆车道或调成员顺序，而不是引入随机/轮询模式。两种模式必须复用同一套冷却/熔断/日志基础设施。

### 7.3 车道六键

```go
type LaneRelayConfig struct {
    MemberMaxAttempts                     int // 单成员含首发的总尝试次数（failover 生效）
    MemberRetryIntervalSeconds            int // 同成员相邻尝试间隔
    MemberNonStreamResponseTimeoutSeconds int // 非流式整响应超时
    MemberStreamFirstEventTimeoutSeconds  int // 流式首个有效事件超时
    MemberCooldownSeconds                 int // 成员耗尽尝试后被跳过的秒数
    MemberAffinitySeconds                 int // 切换成功后保持当前成员的秒数
}
```

默认值取 upstream `DefaultGroupRelayConfig`；**六键数值默认值与超时算术以 [`routing-spec-v1.md`](routing-spec-v1.md) §1.2、§8 为单处规范**。六键取代厂商层全局 `RetryTimes`：车道级可覆盖、成员级可再覆盖。

**默认六键可经 `GET/PUT /api/system/options` 的 `lane_defaults` 调整**（option 键 `PBRLaneDefaults`）：它只决定"新建车道时写入的初值"与"车道未显式配置时的回落值"；已存在的车道若自带六键则不受影响（车道六键仍是权威，成员级覆盖仍在最上层）。

> **`member_affinity_seconds` 默认改为 0（相对上游的 300）**：上游的亲和是为"多用户共享车道时压低抖动"而设；本项目是单用户自用网关，亲和会让"高优先级成员恢复后仍被低优先级成员粘住"，直接掩盖 priority 的语义。默认 0 = 不做粘滞，每次请求都从优先级最高的可用成员开始；需要压抖动时按车道显式配置（六键本来就支持车道级覆盖）。

**超时算术（单处规范见 routing-spec §8）**：成员数 × attempts ×（超时 + 重试间隔），端到端按客户端重试次数相乘。

### 7.4 成员冷却 + 亲和（移植现行路由层语义）

- 成员耗尽 attempts → 写入冷却截止 `now + CooldownSeconds`，期间被跳过；到期自动重新可用。
- **恢复探测使用 `ProbeLease`**（[ADR 0014](adr/0014-request-resource-leases-and-route-snapshots.md)）：租约记录 owner、generation 与 expiry；只有 owner 的请求 State 幂等收口或 State 的到期回收可释放，`recordSuccess` / `recordFailure` 不隐式释放。迁移不转移 live lease，Prune 不清 live lease，Reset 递增 generation 使旧租约失效。
- **限流（429）不写冷却**（2026-11-23 立，routing-spec §4.1.1）：`429` 表示"你发得太快"而非
  "成员故障"，只写 `PacedUntil` 减速（默认 3s，尊重 `Retry-After`，上限 60s），
  期间该成员仍 `available=true`、只在本轮排到后面。`Retry-After` 超过 60s 时不等待：
  原样透传该 429 与 `Retry-After` 头，不换成员、不合成 503。关键词命中欠费/额度的 429 归
  `hard_quota`，仍走长冷却（账号状态，与并发无关）。
- 切换成功后按 `AffinitySeconds` 粘住当前成员，避免抖动；成员失败立即结束亲和。
- 进程内保存；重启清空属可接受语义，README 写明。

### 7.5 熔断 + 半开自愈（新增）

- 键为类型化 `MemberKey`（渠道 + 上游模型 + 成员条目；车道身份由 Registry 承载），同车道内重复 `(渠道, 所选模型)` 的成员各自独立（ADR 0009/0013）；对外标签仍为 `channel/model`。
- **半开与冷却到期探测共用 `ProbeLease`**：generation 隔离 Reset/迁移前后的所有权，Health 只读投影 `probe_since` / `probe_lease_until`，不修复运行态（ADR 0014）。
- **软/硬故障分离**：`429`/限流类为软故障，**不计熔断分、不写冷却**（routing-spec §4.1.1）；
  其余软故障（5xx/超时）按分级冷却，硬故障（401/403/欠费）按倍率冷却。
- 连续失败达阈值 → 打开；退避期到 → 半开放一个真实请求；**连续 `circuit_half_open_successes` 次探测成功才复通**并写恢复事件日志（单次成功只解除本次冷却，不直接恢复全量流量）；熔断打开前已发出、随后成功的普通在飞请求不得关闭 open。
- 触发证据除关键词外，补"滚动窗口失败率"（定长环形缓冲：数据面非阻塞上报、控制面评估）。
- **禁用语义 = "熔断中"**；保留开关名 `AutomaticEnableChannelEnabled` 兼容现网口径，**默认 true**（README 写明）。注意：该开关只是"允许写回 enabled"的许可，真正执行复通的是渠道巡检任务（`monitor_setting.auto_test_channel_enabled`，默认 **false**）或人工触发"测试全部渠道"；即默认部署下不会自动复通，README 必须如实写明。
- 阈值/半开周期进 `system/options` 可配，经 API 修改。

### 7.6 自动禁用关键词（行为继承，条目属部署数据）

自动禁用依赖关键词表命中上游错误体。某些欠费类上游以 **HTTP 400** 到达，按车道 §4.1 故障分类不属可重试/可判罚形态，关键词是唯一捕获路径；机制必须保留，部署侧自定义条目完整继承（条目见私有台账，不写入本文）。关键词表读写一律走 `PUT /api/system/options`。

> 关键词表在 PBR 里的实际消费者是**熔断器的错误分类**（`route.Classify`，§7.5）；基座遗留的"渠道自动禁用"（全局 `AutomaticDisableChannelEnabled` + 渠道 `auto_ban`）默认关闭且已被成员级熔断/半开自愈取代，控制台渠道表单不再暴露该开关。

### 7.7 路由与故障切换（成员链管理，侧边栏独立页 `/routes`）

用户的操作心智只有三步：**渠道里填上游与模型（上游命名不一致时配渠道映射）→ 路由与故障切换页（`/routes`）里为每个模型定"优先打谁、再打谁" → 令牌里允许这个模型**。

- `/routes` 页以**卡片网格**展示全部**真实车道**（`source ∈ {explicit, disabled}`，对齐线上 octopus 分组页），卡片上给出成员顺序与运行态；新建/编辑进入**两栏编排器**：左栏「渠道 → 模型」可搜索选择器，右栏已选成员有序列表（排序 / 启停 / 删除 / 改上游真名 / 清空）。成员的启停开关（§7.1）就在这里，被关闭的成员仍留在列表里、明显标为已停用，不会被误认为还在承载流量。
- **渠道声明与车道彻底分列**（[ADR 0007](adr/0007-routes-lists-lanes-only.md)）：渠道声明但未配车道的路由键（`source: unconfigured`）**不在路由页出现**——删车道即卡片消失；新建车道一律手工输入路由键。`GET /api/models` 契约不变（仍返回 `unconfigured`，供模型管理页与 `/v1/models` 消费）。
- **成员候选 = 任意启用渠道的任意已声明模型**（[ADR 0006](adr/0006-lane-free-member-composition.md)）：可跨渠道、跨模型组链，不要求同名，**同一渠道的不同模型可出现多次**（按 `(渠道, 所选模型)` 去重，同一模型不可重复，[ADR 0008](adr/0008-member-stores-selected-model.md)）；路由键可任意命名，无需任何渠道声明过它。**没有「自动添加」**，成员全部人工挑选，左栏对已加入的 `(渠道, 模型)` 直接禁用。
- **给渠道声明/新增模型绝不自动生成车道**：渠道 `models` 增删只改声明，车道必须人工显式创建；未固化的模型不可调用（503）。用户保存顺序即固化成 PBR 的 `Lane`（模式默认 `failover`）。
- **成员字段的"读回写"通则（强制，与 §5.1 第 3 条同源）**：成员的写入是**全量替换**——保存车道时删光该车道全部成员再整体重插（`UpsertLane`），`PUT /api/lanes/{name}/members` 同样是整条成员链替换。因此**漏发一个成员就是删掉它，漏发一个字段就是清空它**。任何新加到成员上的字段，必须**同批**满足三件事，缺一即"字段在每次控制台保存后被静默清空"：① 管理面**读端点回传**该字段；② 前端把它**读进草稿态**；③ 保存时**带回**。本仓已在此处丢过一次成员的上游真名（读端点条件恒假导致前端读不到），成员级六键覆盖也仍在踩这条路（控制台从不回传 → 每次保存即清空）；**成员开关是第三个走这条路的字段，必须与 `overrides` 同批收口**，不接受"先落地字段、以后再补读回"。
- "车道"是**唯一路由入口**；两种模式、六键、成员级覆盖/别名仍是可选的高级能力，`/api/lanes/**` 契约不变，`/routes` 页是它的友好视图。

**验收**：在 `/routes` 页为"模型1"设定"上游1 → 上游2"后，`GET /api/routes/模型1` 的成员顺序与之一致；把上游1 打成故障后，请求自动逃逸到上游2（fault_injection 覆盖）；关掉上游1 的成员开关后，请求不再命中上游1 且 attempts 链把它记为 `disabled`（§8），重新打开后立即恢复参与选路，全程不产生冷却、不改变其熔断状态。

### 7.8 探活

`POST /api/lanes/{name}/probe`（逐成员）与 `POST /api/channels/{name}/test`（单渠道）；默认不内置定时器，由 API 或熔断器驱动。

### 7.9 渠道退役（显式编排，唯一允许改车道/成员组的渠道动作）

渠道退役 = **停用 → 解除引用 → 删除 → 历史标记** 四步编排（`POST /api/channels/{name}/retire`）。
它与 §7.1"停用 ≠ 删除"的关系：停用是可逆的日常操作；退役是**不可逆的终态清理**，控制台的
"删除"入口直接走退役，API 的 `DELETE /api/channels/{name}` 保持严格语义（仍被引用即 409）不变。

- **编排语义**：事务内先置 `status=手动停用`（中途失败也留下"已停用"的安全态）→ 移除本渠道的
  全部散成员（成员清空的**车道整条删除**，与 `PUT /channels` 收窄语义一致）→ 从成员组移除本渠道
  条目（**组本身保留**：组是复用模板；变空的组在响应里单独列出，由运维决定去留）→ 通过组变空的
  车道一并删除 → 删除渠道行 → 按历史标记改写（下条）→ 发布渠道删除事件并刷新运行态缓存。
- **改车道/成员组的许可边界**：§7.7 与 [ADR 0009] 规定渠道写操作不得**静默**变更车道链；退役是
  唯一例外，且必须满足三件事——①确认前先 `?dry_run=true` 拿到完整预览（引用清单、将被清理/删除
  的车道、受组影响的车道、历史改写条数）；②控制台二次确认（输入渠道全名解锁，GitHub 式）；
  ③每步结果进审计与响应（`lanes_cleaned` / `lanes_deleted` / `groups_affected` / `groups_emptied` /
  `manual_lanes_cleared`）。
- **历史标记**：退役把该渠道的历史请求记录与统计改写为 `原名（已退役 YYYY-MM-DD HH:MM）`——
  `pbr_request_logs` 按 `member_channel_id` 精确圈定（改名/重建同名渠道都不受影响），
  `pbr_stats_hourly` 的 `channel` / `channel_model` 维度按组键改写。这样历史账保留、且**同名新渠道
  不会与退役历史混在同一聚合键下**。退役事务提交后新写入的残留行（在途请求）保持原名，是有界
  竞态，不追改。
- **异步对象**：ADR 0010 的 file/batch 固定到渠道，退役后 retrieve 将 `503 async_object_channel_unavailable`
  并由过期清理兜底——预览里列出未过期对象数作提示，**不阻断**退役。

### 7.10 出站准入控制（在飞上限 + RPM + 自适应，有界排队）

上游按"同时请求数"或"每分钟请求数"限流时，网关在发往上游之前先做准入判定；不满足的请求在
网关内部 FIFO 排队，而不是全部打过去再靠 429 冷却补救（[ADR 0012](adr/0012-outbound-admission-control.md)、
routing-spec §3.2）。要点：

- 运行态按 `(渠道, 成员所选模型)` 进程内全局共享（跨车道、跨成员组），配置挂在渠道上；
- 参数 `max_concurrency` / `max_rpm` / `queue_limit` / `queue_wait_seconds`；
- **默认不限制**（实测现有流量在飞峰值仅 9，默认闸门只增加延迟）；需要时按渠道显式开启；
- `max_concurrency: -1` = **自适应（AIMD + 延迟辅助）**：成功慢涨（+1/limit）、429 减半并暂停
  1 秒、TTFT 超 2× 基线降 10%、超时降 10%、5xx/4xx 不动、5 分钟空闲衰减；下限 1、上限 32；
  用户不需要填数字；
- 就绪 = 有空槽 **且** 窗口未满；就绪的成员优先（低优先级可顶上）；都不就绪时在"预计最早
  就绪"的成员队列里等，超界才换成员；
- 排队不是失败（不写冷却、不计熔断）；全部候选都不就绪且排队超界 → 模型面 **429 + `Retry-After`**，
  与"车道真正不可用"的 503 区分；
- **启用限流/自适应的成员，上游 429 不写冷却**（只降并发 + 暂停 1 秒）——避免"网关自己超发"
  把整条车道打成 503；`hard_quota` 仍走长冷却；
- 探活/渠道测试不排队、不计 RPM；运行态进程内，重启清空。
- **请求级准入与资源所有权**（ADR 0014）：`PBRServe` 单次 `ResolveRoute`，policy、选路与转发共用不可变快照；`RouteMember` 携带渠道/准入快照，配置纯解析且请求路径 fail-closed。准入结果区分 `success/failure/neutral/canceled`，waiter 可被取消、deadline 或 generation 驱逐；请求 State 最早注册并幂等释放 probe、槽位、密钥 lease 与流式监督资源。

### 7.11 错误语义与展示码

错误"是什么"与"给客户端看什么"必须分离（[ADR 0013](adr/0013-error-semantics-and-display-code.md)）：

- **错误链保留**：展示文案只能改展示字段，不得替换底层 `Err`；`errors.Is(DeadlineExceeded/Canceled)`
  必须可用，否则超时会被降级成可重试的软故障、客户端取消会被记成成员故障。
- **状态码映射只改对外状态码**：分类读原始上游码，映射读独立字段；目标码限定 400–599，
  禁止 2xx 与 503（503 是"无可用渠道"专属）。
- **429 = 减速，不是故障**（§7.4）：只写 `PacedUntil`，`available` 仍为 true，不写冷却、不计熔断分。
- **流式按结束原因分类**：`Timeout`→timeout、`ScannerErr`→按错误、`ClientGone`→canceled、
  零内容帧→bad_response；不得用"空流"顶替超时或取消。
- **任何"已写错误响应却返回成功"的路径必须修正**：未提交的返回错误走换人，已提交的
  标记为已提交失败（记失败、不重试、不二次写）。
- **自动禁用按分类白名单**：只有 `hard_auth` / `hard_quota` 可触发，且状态码范围禁止 429/5xx；
  该判定必须先于 `IsChannelError`/`channel:` 前缀匹配，`timeout`（`channel:response_time_exceeded`）
  与 5xx/429 一律不得自动禁用渠道。

---

## 8. 日志与记账（元数据 only）

单表 `request_logs`（**所有日志统一写这张表**：模型面请求、渠道测试等管理动作、错误——基座 `logs` 表停写，历史数据保留只读）：

```
id, ts, lane_name, request_model, route_source, member_channel_id, member_channel_name, upstream_model,
token_id, key_name, user_id, username, type, ip, inbound_format, success, http_status, error_kind,
error_summary(≤2KB 截断), prompt_tokens, completion_tokens, cache_read_tokens,
cache_write_tokens, reasoning_tokens, ttft_ms, total_ms, is_stream,
session_id, session_key, subagent, parent_subagent, parent_session_id, client,
context_tokens, context_window, context_counted, context_status,
masked_count, mask_blocked, mask_status, mask_hits(JSON，仅类型/占位符/长度), mask_took_ms,
attempts(JSON), total_attempts, estimated_cost(仅折算), price_band(命中时段下标，-1=兜底)
```

模型面请求的 `user_id/username/ip` 为空（PBR 无用户体系，调用方身份是客户端密钥 `key_name`）；渠道测试等管理动作记录管理员（`user_id/username/ip`）。`type` 区分消耗（2）与错误（5）——控制台一个日志视图按类型渲染徽章，**不再分两个分节两张表**。

`attempts` 元素：`attempt_num`、`member`、`status ∈ success|failed|cooldown|circuit_break|skipped|disabled`（`disabled` = 该成员被人工停用而跳过，§7.1；取值口径与留痕规则以 routing-spec §9 为单处规范）、`duration_ms`、`error_kind`、`msg`。另建每日/每小时聚合表供 `/stats` 与 UI 图表。

**客户端与会话**：`client` 由 `User-Agent` 归一（[usage-spec](usage-spec-v1.md) §4），只用于展示与聚合，不参与鉴权/选路；`session_id` 按「显式会话头 > `prompt_cache_key` > 首条用户消息指纹」提取（usage-spec §3），`session_key` 是它的 URL 安全摘要，供索引与会话聚合。子代理/父会话标识分别落 `subagent` / `parent_subagent` / `parent_session_id`。管理动作没有会话与客户端。

**上下文组成**：每条 chat-like 请求在转发前按 7 个分区（system/tools/skills/memory/files/results/chat）统计 prompt 的 token 组成，**只存名称、标签与 token 数**（usage-spec §5），正文、工具参数与文件内容不落库；完整分区明细在 `pbr_request_contexts`，随明细日志一起清理，`pbr_request_logs` 只留 `context_tokens` / `context_window` / `context_counted` / `context_status` 供列表与会话聚合。上下文解析失败、body 过大或格式不支持都不影响转发，只写状态。

**隐私脱敏**：开启后，入站 JSON body 在协议解析前把密钥/个人数据/用户词替换为占位符，回复在最终写出前恢复；`mask_hits` 只存 kind/by/placeholder/head/len/n/action，**不存原值与正文**。默认关闭；规则引擎与本地模型语义、失败策略、API 与页面见 [`privacy-spec-v1.md`](privacy-spec-v1.md)。脱敏后的正文才进入上下文解析与上游发送，因此上下文统计看到的是占位符而不是原值。

**写入协议（2026-11-26 收口）**：明细、上下文与聚合 outbox 必须在**同一 SQLite 事务**内落库；任一失败整条回滚，`carrier.Written` 只在提交成功后置位。`pbr_stats_outbox` 只存聚合所需字段，由常驻 worker 逐行在独立事务内 upsert 全部维度并删除 outbox 行——崩溃后要么整条未生效（重试），要么整条已生效（不重复计数）。SQLite `BUSY/LOCKED` 做有限重试；磁盘满/损坏立即暴露为写入失败并计入 `pbr_log_write_failures_total{stage}`。因此 `/api/stats` 的聚合在请求结束后**短期滞后**（worker 轮询 ≤2s），明细/日志视图仍即时一致。

**聚合口径**：`pbr_stats_hourly` 新增 `client` 维度，并记录归一输入总量、缓存读写、推理 token 与首字耗时之和（usage-spec §6.3）。**缓存命中率 = `cache_read_tokens / input_tokens`**，其中 `input_tokens` 已按 `usage_semantic` 归一：Anthropic 的 input 不含缓存，需加上 `cache_read + cache_write`；OpenAI/Gemini 的 prompt 已包含 `cache_read`。

> 路由为模型名键控：配了车道时 `lane_name` 等于车道名（通常等于 `request_model`）；日志另记 `route_source ∈ explicit|unconfigured`，用于区分"已固化可调用"与"渠道声明但没建车道"。

**明确不许有**：`quota` 扣减、余额变更、请求/响应正文、任何"余额不足拒服务"逻辑。

**事件通知（Webhook）**：路由运行态、渠道状态与上游健康事件可配置异步推送到外部 webhook 目标（本机通知中心等），投递语义与管理面见 §16.10 与 api-spec §5.8。

**成本折算**是可选能力：按**成员所选模型名**匹配**渠道级上游单价**（渠道 `setting.pbr_prices`，同一模型在不同上游可配不同采购价；计价键口径见 §16.9#7——渠道 × 模型，与流量从哪条车道进来无关）；渠道未配价则**不折算（0）**。**没有全局默认单价层**（原 `PBRModelPrices` 全局表已移除——单用户自用每渠道自己定价即可）。**这是记账不是计费**——不参与准入、不扣余额；单价属部署数据。看板（概览/模型分析/成本统计）统一读 `GET /api/stats` 的聚合（`requests`/`successes`/`token`/`estimated_cost`）。

**请求日志导出**：`GET /api/logs/export` 按与 `/api/logs` 相同的过滤条件导出 CSV / NDJSON。必须给 `from`/`to`：区间上限 31 天、行数上限 100,000，流式返回、不落临时文件。CSV 只含扁平列（含 `price_band`，**不含** `attempts` JSON 与 `error_summary`）；NDJSON 含完整行（含 `attempts`）。导出属读操作，仍写一条审计（`action=export`、`resource=logs`、行数与过滤摘要）。列定义与限制见 api-spec §5.5.1。

**指标导出**：`GET /metrics` 暴露 Prometheus 文本格式，鉴权复用管理密钥 Bearer（不新增密钥类型）。指标只含低基数标签（lane / channel / model / status / error_kind），来源是进程内计数器与路由运行态快照，**抓取时不查数据库**；端点与指标清单见 api-spec §5.1。

---

## 9. 密钥模型（取代"令牌网关"）

> 完整设计见 **[`token-spec-v1.md`](token-spec-v1.md)**，本节只留架构性两句：

- 管理凭据 = **一个登录口令**（无账号），派生管理密钥 `Base64(SHA256(口令))`，服务端只存其哈希；客户端密钥服务端随机生成并明文持久化（管理 API 可回读），**默认允许全部车道、只能显式拒绝**（ADR 0004）。
- 额度/配额/白名单/多用户字段一律删除；客户端密钥由当前 PBR 实例创建和管理（详见 token-spec-v1.md）。

---

## 10. 源码复用与工程骨架

### 10.1 保留什么（来自 new-api）

- `relay/channel/**`：40 家厂商适配器**原样复用**。
- `relay/` 转发管道：协议转换、SSE 流式、`relay/helper`（价格相关函数除外）、`relaykit/`（含原上游 `dto/`，现为 `relaykit/dto/`）、`constant/`、`common/`（必要部分）、`i18n/`。
- `web/**`：**直接搬迁 new-api 上游前端**（见 §6；信息架构以 ui-spec-v2.md 为准，组件细节见 ui-spec-v1.md），全量替换品牌；删除计费/多用户页面，其余保留。
- WS 池代码：保留，不删。任务插件/异步任务子系统、JS 插件基座与 Midjourney 全链路已删除（§1.3，个人自用不接文生视频/文生图服务）。

### 10.2 当前裁剪边界

计费、多用户及其 UI 已从当前产品范围移除；适配器只保留正确转发所需的字段和代码。
成本折算仍是只读观测能力，不恢复额度、扣费或余额拒服务。历史裁剪过程和验证记录仅供追溯，
不构成当前实现要求。

### 10.3 从 upstream 取什么

| 能力 | 取法 |
|---|---|
| 六键 `relay_config` 语义 | 照搬 bestruirui 上游字段与默认值/Normalize |
| 成员冷却 + 亲和 | 移植语义（进程内 cooldowns + affinity）；车道成员顺序/优先级变更时立即清除旧亲和，按新配置选路 |
| failover 排序 | 移植语义 |
| 熔断三态 + 半开 | **仅算法参考**（同名分叉仓库），按 PBR 模型重写 |
| 健康快照 / 逐尝试日志 | 参考形状，落到 probe 与 `request_logs.attempts` |

### 10.4 工程骨架

> 立项期的"待建"骨架树已归档（[`archive/design-v1-history.md`](archive/design-v1-history.md)）；**实际目录以仓库为准**。唯一仍有效的取向：上游包布局保留（含 `types/`），新写的路由核心放 `internal/`，新档案进 `docs/`，分波次验收证据进 `verify/`，上游只读参考放 `reference/`（不入仓库）。

### 10.5 工程基座：复用 new-api 转发与适配能力

适配器与 `service` / `setting` / `model` 深度绑定，净室剥离会破坏转发能力。因此工程基座固定为：

1. **复用**：把 `reference/new-api` 的 Go 源码纳入本仓库实现，**保留其包布局**（`relay/ relaykit/ common/ constant/ setting/ service/ model/ controller/ middleware/ router/ logger/ pkg/ i18n/ types/`；上游 `dto/` 已并入 `relaykit/dto/`），改 module path 为 `github.com/zzyyyds88/PowerBarRations`（与公开仓库 URL 一致；二进制名仍为 `pbr`）、全量替换 import 路径。其中 `relaykit/` 是**独立 Go module**（自带 `relaykit/go.mod`，路径 `github.com/zzyyyds88/PowerBarRations/relaykit`），主模块通过 `require` + `replace … => ./relaykit` 引用。
2. **不迁**：`electron/`、`docs/`、`e2e/` 等非代码资产（`web/` **要迁**，见 §6）。
3. **目标目录布局是演进终点，不是起点**（历史骨架树见归档 §10.4）：新写的路由核心放 `internal/`；旧包逐步改造或删除，不要求一次性重排目录。
4. **许可证**：保留 new-api 的 AGPL 头、`LICENSE`、`NOTICE`、`THIRD-PARTY-LICENSES.md`（前端既然只来自 new-api 一家上游，无需再另附其他蓝本清单）。品牌可替换，版权不可替换。

---

## 11. 开发阶段数据边界

- 不实现旧 `new-api`/`octopus` 数据库迁移、`pbr migrate`、双写、历史数据回填或旧数据兼容兜底。
- 新实例从空 PBR SQLite 数据库开始，渠道、各模型车道和 Hermes ClientKey 通过管理 API 或控制台创建。
- `/api/export` 与 `/api/import` 若保留，仅表示同版本 PBR 配置快照；不得声称支持旧系统数据恢复。
- 运行态故障转移属于请求路由能力，不属于数据兜底；它只在显式车道的成员链内执行。

---

## 12. 金标准用例

Hermes 专用数据面、运维 API 和假上游验收见 [`hermes-spec-v1.md`](hermes-spec-v1.md) §6。
通用路由、认证和管理契约仍分别以 routing/token/api spec 为准；其他模型车道复用同一套验收。

---

## 13. 协作与提交

提交、分支、测试和敏感数据规则统一遵循仓库根目录 `AGENTS.md`，本文不重复维护。

---

## 14. 风险与已知坑

1. **适配器与历史基座字段的耦合**是最大工程风险（实测分布见归档基线 §2.6，[`archive/design-v1-history.md`](archive/design-v1-history.md)）。对策：只移除不影响转发的产品面，适配器保留必要兼容字段。
2. **假成功前科**：路由层写接口曾回 success 但库不动；厂商层创建接口缺包装会静默假写。对策：写→回读→断言。
3. `model_mapping` 必须是 JSON dict，用于渠道级路由键到上游真名的运行时解析。
4. 验证 `sk-` 令牌只能用 curl / node fetch；Python `urllib` 会被指纹过滤误判 401。
5. 欠费类上游以 HTTP 400 到达，按车道 §4.1 故障分类不属可重试形态，只有关键词命中才抓得到。
6. 自动禁用后无半开自愈（上游 issue #5420）——本项目要修的就是它。
7. 车道全挂必须"错误快抛、无静默兜底、不跨车道逃逸"。
8. 同渠道同时承载车道流量与点名流量时，整渠道级故障会让点名请求 100% 撞墙——故 `PublicAlias` 点名也必须过冷却/熔断。
9. **隐私脱敏的误伤与延迟**：规则会误判代码/路径，模型会占用 CPU；默认关闭，所有阈值/词表可调，模型有 2 秒预算与 256 KiB 上限，失败按 privacy-spec §6.3 降级。任何放宽都必须先补回归 fixture，禁止用"先放行"绕过。
10. **AGPL-3.0 与公开发布**：上游均 AGPL-3.0，本仓库按公开发布决策（§18.4）维护。AGPL 义务自发布起持续生效，不再是"仅自用不分发"：必须保留上游版权头与 `LICENSE`/`NOTICE`/`THIRD-PARTY-LICENSES.md`，不得声称重新授权或变更许可。同时，**仓库只要公开就意味着内容可被永久抓取**——部署私有数据（渠道名、模型名、车道名单、厂商地址、凭据、成本单价、内网地址）一律不得进入仓库、issue 或 PR 描述（文档纪律见本文开头）。仓库可见性由业主在远端控制，实现侧一律按"随时可能公开"的标准执行。

---

## 15. 交付物

① 可运行二进制 `pbr`（含 embed 的控制台）+ `Dockerfile`（多阶段：前端构建 → Go 构建）。
② `README.md`：部署 / 车道配置 / 超时算术 / 管理密钥管理。
③ `docs/adr/`：记录以 new-api 转发管道为基座、保留渠道级 `model_mapping`、自动复通和轻量认证等关键决策。
④ `verify/`：每波次的命令与输出证据，含时间戳。
⑤ `GET /api/openapi.json`（机器可读契约）+ `GET /doc` / `GET /llms.txt`（面向 AI 的纯文本手册）+ `GET /doc/ui`（复用 Scalar 的交互式文档）。

---

## 16. 实现约定（消除歧义）

> 本节把 goal 模式最容易跑偏的实现细节固定下来。凡本节有约定，实现不得自行发挥。

### 16.1 param_override 与思考参数

- `param_override` 为 JSON 对象，在适配器构造完上游请求体之后、发送之前做**深度合并**：对象递归合并，标量覆盖，`null` 删除该键，数组整体替换。
- 合并发生在**协议转换之后**，即覆盖的是"最终发给厂商的体"，从而让同一份请求语义能驱动所有厂商。
- **思考参数**：沿用适配器现状（`setting/reasoning` 与 `model_setting.ShouldPreserveThinkingSuffix` 是现成实现，直接复用）。**网关不新增校验、不拦截**：`enable_thinking` 与 `reasoning_effort` 半开时由**上游**返回 400，网关原样透传；各厂商等价字段（如 `thinking:{type}`）的映射由**适配器**负责，PBR 不新增第二张映射表。金标准③测的是"上游会 400"，不是"网关要造一个 400"。
- 约定：渠道的 `param_override` 只放"该厂商差异项"；通用项放全局默认，避免逐渠道重复。

### 16.2 测试用假上游（failover / 熔断验收的基础设施）

- 仓库内置 `internal/testutil/fakeupstream`：可编程假上游，进程内用 `New`（Go 测试）、独立进程见其 `cmd/fakeupstream`（verify 脚本）。按请求里的 `model` + `Authorization` 决定行为并记录每次请求体；可按模型注入 `status`（任意状态码，如 429/500）、`body`（原样返回，用于坏响应/欠费关键词）、`delay`（首包延迟）与 `mode`（`bad_json` / `empty` / `disconnect_stream`），并支持 `RequireKey`（不匹配 401）与 `Models`（清单外 404）。正常回包时：带 `tools` 回 `tool_calls`、`/embeddings` 回确定性向量、`/models` 回模型清单（供 sync-models 验收）、`stream=true` 回 SSE；运行中可 `POST /__control {"model","status","body","delay_ms","mode"}` 动态注入/修复（`model:"*"` 为全部）。
- 车道与容错的验收（"唯一成员指向必然 500 的假端点""半开窗口内自动复通""429 不被误判硬故障"）**一律用该 fixture，不打真实厂商**。
- fixture 需记录收到的请求体，用于断言 param_override 与思考参数的合并结果。

### 16.3 OpenAPI 生成

- **以代码为源**：在路由注册处维护端点表，`GET /api/openapi.json` 返回运行时结果。当前实现是 `internal/api/config_lifecycle.go` 的手写 `openAPIPaths()` 表（尚未改为结构体标签自动生成），靠下述守卫测试把"漏登记"变成构建期失败，效果等价于验收断言。
- 验收断言：`openapi.json` 可被标准工具解析，且**所有已注册路由都出现在文档中**。落地为 `router/openapi_coverage_test.go` 的 `TestOpenAPICoversEveryRegisteredRoute`：对比 `engine.Routes()` 与端点表，双向校验（既不漏档、也不登记不存在的路由）。
- 若将来改为标签生成，保留该守卫测试即可；漂移口径不变。
- **响应契约以表为源**（与端点表同一取向）：管理端点的**路由、处理器、响应策略与 dry-run 类别同处声明**——稳定面在 `internal/api/ops_routes.go` 的 `opsRoutes` 表（注册即登记，不可能漏）；基座遗留面按 `METHOD + c.FullPath()` 在 `internal/apiresp` 登记**需要定制成功体的路由**（与 `middleware/audit.go` 既有 `auditRouteActions` 同法），其余路由由 `apiresp.Default` 兜底。守卫测试 `router/envelope_coverage_test.go` 校验：稳定面策略不得登记不存在的路由、不得漏声明成功/失败/直通三者之一；基座面登记不得漂移。响应归一化（基座 `{success,message,data}` → 契约形态）由 `apiresp.Middleware` 按该表机械执行，**handler 不重复实现信封**；SSE 在写头/Flush 时立即直通，不缓冲。
- **给人/AI 的入口**：`GET /doc`（默认 `text/markdown`，浏览器 `Accept: text/html` 返回说明页）、`GET /llms.txt`（`text/plain`）、`GET /doc/ui`（复用 GitHub 项目 Scalar 渲染 `/api/openapi.json`）。三者与 `/api/openapi.json` 均免鉴权，便于 AI 先读手册再自行派生管理密钥。
- **前缀**：管理面规范前缀为 `/api`，`/api/v1` 为兼容别名（注册相同处理器）。与 AI 契约冲突的控制台内部资源（模型目录、审计）收在 `/api/console/*`；其余控制台内部接口仍在 `/api/*` 下同权限可用，但不属于稳定契约。

### 16.4 存储与当前 schema

- 开发阶段只保证当前 PBR schema 在新建 SQLite 数据库上初始化成功；破坏性 schema 变化允许重建开发库。
- 不提供旧 `new-api`/`octopus` 数据库导入、历史数据回填、双写、旧字段兼容或数据恢复兜底。
- 同版本 `/api/export` 与 `/api/import` 只处理 PBR 配置快照；它们不是数据库迁移工具。
- **SQLite 运行纪律**：`SQLITE_PATH` 只写文件路径即可，`busy_timeout(30000)`/`journal_mode(WAL)`/`_txlock=immediate` 由 `common.SQLiteDSN` 强制补齐并覆盖冲突值；`LOG_SQL_DSN` 指向同一文件时启动失败。SQLite 主库连接生命周期默认 0（不周期性重建连接），连接池为单连接。
- **备份/恢复**：生产用镜像内 `pbr backup`（SQLite 在线快照 + `quick_check`；本机开发可用同命令或 `scripts/backup_pbr_db.sh`），不得热 tar `pbr.db`/`-wal`/`-shm`；恢复必须先停服务、替换主库并删除旧 WAL/SHM。`pbr auth reset` 同样要求先停服务（检测到 WAL/SHM 默认拒绝，崩溃残留可 `--force`）。

### 16.5 日志保留

- 保留策略由 `system/options.log_retention_days` 配置（默认 30）。**内置每日分批压缩**：系统任务调度器每 24h 创建一个 `log_cleanup` 任务，对 `ts < now - retention_days` 的行删除上下文分区、清空 `attempts`/`error_summary`/`mask_hits`，但**永久保留 `pbr_request_logs` 汇总行**（时间、车道/渠道/模型、token、成本、首字/耗时、状态、会话、总尝试数等）；保留期内的细节不会被提前清理。`POST /api/logs/prune?before=<ts>&dry_run=` 仍是手动入口。
- 压缩只动排障细节，聚合表（`pbr_stats_hourly`）**长期保留**（体积小）；一次压缩不得改变 `/api/stats` 的历史数值，也不得让历史明细行从日志列表/会话页消失。
- 上下文明细表（`pbr_request_contexts`）按 `created_at`/`log_id` 分批删除，不单独保留；会话与上下文曲线因此只覆盖保留期内的上下文细节，但会话汇总仍可由保留的日志行重算。
- **`GET /api/stats` 必须读聚合表**（hour 直读、day 由小时桶上卷），不得读明细表——否则一次 prune 就把历史统计抹掉，与"聚合长期保留"自相矛盾。
- `POST /api/stats/rebuild` 先 drain `pbr_stats_outbox`；重建时保留**已无明细来源**的小时桶（prune 边界及更早），只重算仍有明细覆盖的桶。响应含 `preserved_buckets`。仍有待处理/死信 outbox 行时 rebuild 失败，避免在聚合缺口上继续。
- `/api/health` 暴露 `stats_outbox.pending/dead` 与最近一次 `db_last_write_error`；`dead > 0` 或 outbox 不可读时整体 `degraded`。

### 16.6 并发与超时

- 每次尝试的超时由车道六键与成员级覆盖决定；非流式使用整响应超时，流式使用首事件超时，提交后不设总超时。
- 每个请求在入口建立准入绝对 deadline，只约束排队与 waiter 唤醒；`queue_wait_seconds` 只能缩短该 deadline。客户端取消、准入超时与上游成员超时严格区分（ADR 0014、routing-spec §8）。
- 探活并发上限可配（默认 4），避免对上游造成突发压力。
- 请求 State 是最早注册、幂等收口的资源 owner；probe lease、准入槽位/waiter、客户端密钥 lease、流式监督与后台任务都必须向它注册。迟到回调只能观察已关闭状态，不得二次释放。
- 流式心跳、读与写共享统一监督；`: PING` 不算提交，但心跳或正文写失败必须立即按客户端断开终止。
- 后台任务只接收安全值快照，使用有界 worker 池与有界队列；禁止持有请求上下文、可变配置指针或无界创建 goroutine。进程优雅停机必须停止 touch/性能采样 worker、drain 已接收任务并执行最终 flush；持续写库失败的性能桶必须有重试上界与显式丢弃计数。
- `/metrics` 的渠道 gauge 必须与本轮路由运行态快照对齐：已删除/消失的渠道标签要同步删除，不能继续显示陈旧 `pbr_channel_up` 或让标签序列无界增长。

### 16.7 管理密钥的数量语义

- 支持配置多把管理密钥（轮换过渡用），任一把均可全量操作，不区分权限；由 `PBR_ADMIN_KEY`/`PBR_ADMIN_KEYS` 提供，**不入库**。两变量同时存在时合并，任一匹配即通过（实现见 `middleware/pbr_auth.go`）。
- 口令恢复的另一条途径 `pbr auth reset --db <pbr.db> --yes` 清库内凭据回到未初始化（实现见 `internal/authutil/cli.go`）。

### 16.9 实现细节（已定，按此执行）

以下均为实现细节，已直接定案，不再逐项征询；如对某条有异见再单独提出。
（原 1/2/5/10/14/15 号条目已删除：划销的与被实现推翻的不再保留，其余内容分别在 ui-spec §4/§5/§6.3/§6.4/§6.8 与 api-spec §5.3 中作为现行规范。）

| # | 项 | 决定 |
|---|---|---|
| 3 | 车道粘滞 | 车道级共享当前成员（照搬线上）；每个模型一条车道，模型之间互不影响 |
| 4 | Docker | 镜像/容器名 `pbr`；数据卷挂 `/data`（含 `pbr.db`）；随仓库提供 `docker-compose.yml` 样例 |
| 6 | 车道模式收敛 | **只保留 `failover`（默认）与 `manual`**；`weighted` / `round_robin` 与其成员 `weight` 已删（单用户网关不需要随机/轮询）。需要打散负载时拆车道或调顺序 |
| 7 | 上游单价与成本折算 | **单价 + 可选峰谷**：价格来源只有**渠道级上游单价**（渠道 `setting.pbr_prices`，人民币 / 百万 token，字段 `input`/`output`/`cache_read`/`cache_write`），在渠道编辑「上游单价」页签配置；渠道未配价 → 不折算（0）。**全局默认单价表（`PBRModelPrices`）已移除**。只用于日志 `estimated_cost` 与看板成本统计（含"渠道 × 模型"维度），**不参与准入、不扣额度、与下游计费无关**（本项目无计费）。基座 `setting/ratio_setting` 不再充当单价表（它仍是惰性遗留：提供路由用的模型名归一化 `RoutingMatchModelName`）。**计价键 = 成员所选模型名（渠道 × 模型，2026-10-25 变更）**：单价表的 key 按该渠道声明/成员所选的模型名匹配（如该渠道提供的 `model-x`），与流量从哪条车道进来无关——同一渠道一份价、服务多少条车道都生效；按车道名计价的旧口径已废除（同一渠道服务多车道时需逐车道重复配价，且与统计的"渠道 × 模型"口径错位）。**缓存折算按上游用量语义区分（2026-10-25 修复缓存重复计费）**：OpenAI/Gemini 口径的 `prompt_tokens` 包含缓存命中（cached 是子集），折算输入价时须先从 prompt 扣掉 `cache_read`，否则缓存命中被按全价+缓存价收两次（实测长上下文高缓存场景单条虚高约 30 倍）；Anthropic 口径的 `input_tokens` 不含缓存，prompt 全额计输入价、缓存各按自己的价。明细 `usage_semantic` 列记录语义（anthropic/gemini/openai，历史行为空按 openai），`stats/rebuild` 按行语义重折算。**峰谷（可选）**：每条价格可带 `timezone`（默认 `Asia/Shanghai`，二进制内嵌 tzdata）与有序 `bands`（`days` 1–7=周一至周日，**指时段起始日**：周一 22:00–02:00 覆盖周二凌晨；`start`/`end` 为 HH:MM，`start` 含、`end` 不含；`start > end` 表示跨午夜；未填的价格字段继承行级兜底价）。按**请求开始时间**在该时区匹配第一条命中；未命中用行级 `input`/`output`/`cache_read`/`cache_write` 兜底（即旧单层价，天然兼容，无需迁移）。每行最多 64 个 band；band 之间允许重叠，先命中者生效。明细新增 `price_band`（命中下标，-1=兜底）；`POST /api/stats/rebuild` 按每条日志的 `ts` + 当前价格表重新匹配并重折算 |
| 8 | 请求头兼容 | 管理面仅收 `Authorization`；模型面 `Authorization` 与 `X-Api-Key` 都收 |
| 11 | 开工基座 | **以 new-api 源码迁入为基座**，非净室重写；前端**同样直接搬迁上游 `web/`**（见 §6 / §10.5） |
| 12 | 亲和默认值 | 默认 `member_affinity_seconds=0`（**与现网一致，避免故障切换后长时间粘在备用成员**），可配 |
| 13 | 前端包管理与适配范围 | 前端复用上游 `web/`（Rsbuild + pnpm）；保留除多用户/计费外的运维页面；实质改造 = **成员链（故障切换）管理**（落位为侧边栏独立页 `/routes`，见 §7.7），并把认证接到 PBR 口令会话 |

### 16.10 Webhook 事件通知（已定）

**定位**：把路由运行态的故障事件推送给**任意外部消费方**，**只推事件、不承载指令**。这是管理 API 的一部分：PBR 只负责**定义推送契约并投递**，接收方的验签、路由、呈现一律由消费方自行实现（本机对接——如推给某 agent 的通知通道——由该 agent 侧做，PBR 不内置任何针对特定接收者的集成）。事件量低频（分钟级偶发），选型为 HTTP webhook 推送——不做 WebSocket/SSE 订阅面（日后若需实时全量订阅再评估 SSE，控制台仪表盘已有 SSE 先例）。

**事件源**：共享事件合同包含 `internal/route` 运行态事件（`circuit_open` / `circuit_half_open` / `circuit_closed` / `cooldown` / **`member_recovered`** / `reset` / `skip` / **`lane_unavailable`** / **`lane_recovered`**）、渠道状态事件（`channel_disabled` / `channel_enabled` / `channel_deleted`）和上游错误爆发事件（`upstream_burst`）。`reset` / `lane_unavailable` / `lane_recovered` 为车道级事件（`member` 为空）；渠道事件为非车道事件（`lane` 为空，`member` 为渠道 ID）；删除事件 detail 保留删除前的渠道 ID、名称与状态，不含密钥。事件经共享订阅钩子异步旁路投递，绝不阻塞请求路径。

**配对语义（合同的一部分，不是消费方自行约定）**：`cooldown` ↔ `member_recovered`（成员冷却→第一次成功服务）、`circuit_open` ↔ `circuit_closed`（熔断→恢复）、`lane_unavailable` ↔ `lane_recovered`（车道全成员不可选→第一次成功服务）。每个"开始"事件都有配对的"结束"事件，消费方据此做 ⚠/✅ 配对通知；`reset` 是人工终止态、`skip`/`upstream_burst` 是过程信号，均不配对。事件的判定与产生点以 routing-spec §7 为单处规范，投递契约为 api-spec §5.8。

`upstream_burst` 由模型面选路失败归属点统一观测，只统计同一渠道在 60 秒内的 HTTP 5xx，默认达到 5 次时发送一次事件；同一窗口内不重复发送，窗口内计数降到阈值以下后重新武装。detail 包含渠道 ID、计数、阈值、窗口、状态码和成员；它与成员级熔断事件并存，分别表达渠道级错误爆发和单成员熔断。

**配置**（system/options 键 `PBRWebhookTargets`，JSON 数组）：`[{name, url, secret, enabled, events[]}]`；`events` 为事件类型白名单（空 = 全部）。管理面读配置时 `secret` 只回显掩码。

**投递语义**：
- 请求体 JSON：`{"type":"pbr","text":"<人类可读摘要>","event":{ts,type,lane,member,detail}}`——带 `text` 字段使"只展示文本"的消费方（通知中心红色档）零改造接入。
- 签名采用**通用 HMAC-SHA256 方案**：头 `X-Webhook-Timestamp`（Unix 秒）+ `X-Webhook-Signature-V2`（`HMAC-SHA256(secret, "{ts}.{raw_body}")`，hex）。验签步骤与偏差窗口写入 api-spec §5.8 与 /doc 手册，消费方照文档实现即可，不依赖任何具体接收端。
- 单次投递超时 8s；失败按 5s/30s/120s 退避重试 3 次，耗尽记入投递日志（`webhook_deliveries` 表：ts、target、event、status、http_status、error、attempt）。**投递含重试按 (target, event) 各自独立进行**——一个不可达目标的重试不阻塞其他目标或其他事件的投递。
- **防风暴**：同一 (target, lane, member, event) 在 60s 窗口内只发一条（合并计数），避免抖动上游刷屏。
- **观测**：事件缓冲满被丢弃的累计计数经 `GET /api/webhooks/deliveries` 响应外层 `dropped_events` 暴露（进程重启清零）。
- 投递日志随 `PBRLogRetentionDays` 由 prune 一并清理。

**部署注意（容器化）**：webhook 投递从 PBR 进程所在网络发起——容器化部署时，目标 URL 里的 `127.0.0.1` 指**容器自身**而非宿主机；推给宿主机上的接收端须写宿主可达地址（docker 网关 IP 或宿主 LAN IP），且接收端须监听非 loopback 接口。

**已知局限**：运行态（冷却/熔断）是进程内的，重启即清空且**不发**恢复类事件——消费方不能靠 webhook 感知 PBR 重启；防风暴窗口内的同类事件直接丢弃（v1 不做合并计数文本）。

**管理面**：`GET/PUT /api/webhooks`、`POST /api/webhooks/test`、`GET /api/webhooks/deliveries`（规范前缀 `/api`，`/api/v1` 为兼容别名；契约见 api-spec §5.8）。

**验收**：手动熔断一个成员 → 目标秒级收到签名正确的 JSON；`circuits/reset` → 目标收到 `reset` 事件；一个目标不可达时不影响其他目标的投递；重试与死信符合上表；投递日志与 `dropped_events` 可查；控制台可编辑并回读。

### 16.11 隐私脱敏（已定）

**语义正本**：[`privacy-spec-v1.md`](privacy-spec-v1.md)。本节只定架构落点，不重复规则细节。

- **唯一 owner**：`internal/privacy` 拥有设置解析、规则引擎、占位符仓库、模型生命周期与请求/响应变换；`controller/relay.go` 只在协议解析前调用它与在收尾时提交日志，不另写判定。
- **两个引擎**：`internal/privacy/redact` 逐文件移植 magpie `internal/redact`（MIT）；`internal/privacy/pii` 移植 AstrLink Guard 的纯 Go CPU 推理与下载校验。**不移植** 1.6 GB Privacy Filter、native `pf-serve` 与 CoreML。
- **默认关闭**：`active=false` 时转发路径零改动；开启后失败策略按 privacy-spec §6.3（引擎失败 503、block 400、模型 required 503、模型可选降级）。
- **不存正文**：只写 `masked_count/mask_blocked/mask_status/mask_hits/mask_took_ms`；仓库只在内存，脱敏键只存 `options` 且不导出。请求日志、导出、会话视图、上下文表都不得出现原值。
- **模型文件**：下载到 `<SQLITE_PATH 所在目录>/privacy/astrlink-guard/<revision>/`，校验 SHA-256；`PBR_PII_MODEL_DIR` 可指向离线预置目录。模型文件不进 Git、不进镜像、不进导出。
- **UI**：日志工作区新增「脱敏」页签（`/logs/masking`），配置与观测同页；请求日志与会话时间线显示命中徽标。不新增一级导航。
- **验收**：privacy-spec §11 的 L1–L4；默认关闭时 L4 转发字节必须与开启前一致。

---

## 17. 品牌与命名

- **产品名统一为 PowerBarRations**，全量替换为自有品牌：界面标题、logo/favicon、i18n 文案、`/version` 与 OpenAPI `info.title`、错误页、空态文案。
- 工程标识：二进制 `pbr`、Go module `github.com/zzyyyds88/PowerBarRations`、Docker 镜像/容器名 `pbr`、环境变量前缀 `PBR_`、本地存储键前缀 `pbr_`、日志前缀 `[pbr]`。
- **规则**：面向用户的文案与标识中不得出现任何第三方品牌名（含蓝本的中文别称）；代码注释中标注"移植自某上游文件"属于溯源，允许保留。
- **许可证义务（不可随品牌一起替换）**：保留 AGPL-3.0 版权头、`LICENSE`、`NOTICE`、`THIRD-PARTY` 许可清单；不得声称重新授权或变更许可。
- 替换清单（实现时逐项过）：① 前端标题/logo/favicon ② i18n 三语文案 ③ `index.html` 与构建注入的应用名 ④ 后端 `/version`、健康检查、OpenAPI ⑤ Dockerfile/镜像标签 ⑥ compose 样例 ⑦ README 与 docs ⑧ 环境变量与存储键 ⑨ 日志与错误消息中的产品名 ⑩ 数据库文件默认名（`pbr.db`）。

---

## 18. 业主已确认事项（2026-09-14）

> 原"需业主确认"4 项已全部拍板，记录如下；开工不再征询。

| # | 事项 | 决定 |
|---|---|---|
| 1 | 网络暴露 | **全部监听 `0.0.0.0` 对局域网开放**（模型面与管理面同端口，凭登录口令/管理密钥鉴权，不做来源限制）。可用 `PBR_BIND=127.0.0.1` 收紧。局域网为明文 HTTP，因此**要求长随机口令** |
| 2 | 部署形态 | **docker compose 样例 + 手动 docker run**；不做面板类应用商店打包 |
| 3 | 旧端点兼容 | **一律不做兼容层**：管理面按 PBR 当前契约重构，不为旧系统保留迁移期入口 |
| 4 | 仓库远程 | **公开发布**（GitHub，AGPL-3.0）。原"只留本地、不配置 remote"的决策**已被推翻**：仓库对公众可见，接受外部 issue/PR；因此必须持续满足 AGPL 署名义务（§14.9），并严守"不含部署私有数据"的文档纪律 |

> **决策变更记录（2026-09-23）**：第 4 项由"只留本地"改为"公开发布"。触发原因是业主明确本项目为开源软件并要求推送公开仓库。连带影响：① §14.9 的 AGPL 义务从"不触发"改为"持续生效"；② 仓库内容（含全部 Git 历史）可被永久抓取，故历史中的部署私有数据必须在推送前清理；③ 对外贡献流程按 `CONTRIBUTING.md` 与 `.github/` 模板执行。

### 非阻塞事项（不挡开工）
- 成本折算单价表的实际数值（属部署数据，运行期在**各渠道的 `pbr_prices`** 里配置，无全局层）。
