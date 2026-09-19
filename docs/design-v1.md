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
> - UI 控制台：[`ui-spec-v1.md`](ui-spec-v1.md)
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

- **G1 单层**：一个二进制、一个 SQLite、一条管道。车道（Lane）是唯一路由入口，成员 = `(渠道, 上游真名, 优先级)`。不再有第二层网关；渠道级 `model_mapping` 与成员级 `upstream_model` 共同负责上游改名。
- **G2 AI 友好**：全部管理能力以版本化 HTTP API 暴露，自描述（OpenAPI）、幂等、可回读、错误可机器判定；AI 不改文件、不查库、不抓前端。完整契约见 api-spec-v1.md。
- **G3 稳定模型面**：车道名与 `/v1/*` 协议、错误语义保持稳定；客户端只需按配置使用 PBR 的 base_url。
- **G4 自愈**：保留现行路由层已验证的成员冷却 + 亲和，并补上厂商层"只禁不通、无半开自愈"的缺陷（new-api 上游 issue #5420）。软故障（限流）不得误判为硬故障。
- **G5 保留厂商适配层**：new-api `relay/channel/` 下 40 家适配器原样复用。
- **G6 有人看的控制台**：提供个人控制台，覆盖车道/渠道/密钥/日志/统计/设置/试打。规格见 ui-spec-v1.md。
- **G7 只看不扣**：日志只存元数据，成本只做折算记账，不做任何计费/扣费/余额拒服务。
- **G8 Hermes 首要验收**：Hermes 使用一条**由部署方命名**的显式故障转移车道（规范中以
  `<hermes-lane>` 占位，见 `hermes-spec-v1.md`）；Hermes 的 Chat Completions、工具调用、
  流式响应和 API 运维工作流是首要验收对象。

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

**一个二进制、一个 SQLite、一条管道**：**请求里的模型名就是路由键**。渠道声明自己能提供哪些模型、以及**路由键 → 上游真名**的映射；**每个模型必须有一条同名车道（成员链）才能被调用**——车道是唯一路由入口，成员按优先级 failover，宁可 503 也不直连某个上游渠道。控制台在「路由与故障切换」页手动新建车道并排定成员链；**成员可来自任意渠道的任意模型**（[ADR 0006](adr/0006-lane-free-member-composition.md)），对外模型名由人工命名、与上游真名解耦。中间不再有第二层网关。

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
| 厂商层 `model_mapping` | **`Channel.ModelMapping` + `LaneMember.UpstreamModel`** | 改名默认在渠道配置一次；成员级 `UpstreamModel` 作为覆盖 |
| 厂商层 `tokens` / 路由层 `api_keys` | **ClientKey 客户端密钥** | 瘦身，见 §9 |
| 厂商层 `users` | **User 纯锚点行（保留）** | 多用户面已删，仅保留内部系统用户一行作记账/归属锚点（§3.4）；管理员身份只由管理密钥承载，该行不可登录 |
| 厂商层 `logs` | **RequestLog 元数据日志** | 只存元数据，见 §8 |

### 3.4 数据模型（SQLite，GORM）

```go
type Channel struct {                 // 上游渠道
    ID        int
    Name      string   // 唯一
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
    UpstreamModel string  // 可选：成员级显式改名（非空且 ≠ 路由键）；留空则用渠道 ModelMapping，再退回路由键
    PublicAlias   string  // 可选：客户端可点名该成员的名字（空 = 不可点名）
    Priority      int     // 车道内顺序，数字大者优先；界面以"上/下移"维护，写库即该值
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
type AuditLog   struct { /* 见 §5 */ }
type Option     struct { Key, Value string }
```

**约束**：不允许出现指向 `users` 表的外键（`users` 表只保留纯锚点行，见上）；不允许出现配额字段（`quota`/`remain_quota`/`used_quota`）；不允许出现"余额不足拒服务"逻辑。

---

## 4. 对外模型 API（下游契约）

| 端点 | 说明 |
|---|---|
| `POST /v1/chat/completions` | OpenAI 兼容，流式 SSE / 非流式 |
| `POST /v1/responses/compact` | OpenAI Responses 压缩入口（实注册名，见 `router/relay-router.go`） |
| `POST /v1/messages` | Anthropic 入口 |
| `POST /v1/embeddings` | OpenAI 兼容嵌入 |
| `GET /v1/models` | 列出当前密钥可见的车道 |

> 本表仅列常用子集；全量端点以 `router/relay-router.go` 的实注册与 `/api/openapi.json` 为准（另有 `/v1/completions`、图像、音频、rerank、Gemini 兼容等入口）。

### 4.1 必须保持的语义

- `model` 字段先按车道名解析；保留思考后缀归一化行为（`ShouldPreserveThinkingSuffix`）。
- **无可用成员时返回 `503 No available channel for model X`**（body 形态不变）。下游 fallback 分类依赖这个体，禁止改成 400/404。
- 请求成功时**响应体 `model` 回填请求名（车道名）**，不填上游真名；真实服务者通过响应头 `X-Served-By: channel=<id>:<name>, model=<upstream>` 与日志暴露。
- 点名：`model` 也可命中某成员的 `PublicAlias`，语义为"锁定该成员"，但仍**必须过冷却/熔断**。
- 车道全挂必须"错误快抛、无静默兜底、不跨车道逃逸"。

---

## 5. 管理 API（AI-first）

> 完整端点、字段、请求/响应示例、错误码、分页、AI 调用手册全部见 **[`api-spec-v1.md`](api-spec-v1.md)**。本节只定架构性约定。

### 5.1 核心约定

1. **认证**：管理面无账号体系，只有**一个登录口令**（浏览器换 HttpOnly 会话 Cookie；AI/脚本用 `管理密钥 = Base64(SHA256(登录口令))` 作 `Authorization: Bearer`，两通道等价；首启先设口令）。派生规则、会话、恢复详见 [`token-spec-v1.md`](token-spec-v1.md) §2。模型面用客户端密钥。**模型面与管理面同端口全部监听 `0.0.0.0` 对局域网开放**（§18），凭凭据鉴权，不做来源限制；口令必须是长随机串。
2. **幂等写**：`PUT /api/{resource}/{name}` 收全量对象 upsert。
3. **写后回读**：任何写操作响应前重新读库，响应体即最终状态；集成测试断言"写完 GET == 提交值"。
4. **`?dry_run=true`**：返回变更 diff，不落库。
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

> 完整页面清单、逐页字段/交互/状态、模块映射与验收见 **[`ui-spec-v1.md`](ui-spec-v1.md)**。本节只定架构性约定。

- **蓝本 = new-api 上游前端**（Rsbuild + React + TanStack Router + Base UI + Tailwind）：**直接整体搬迁，做减法（删多用户/计费）+ 接线（认证、成员链）**，而非另起炉灶。
- **认证极简**：无账号，只有登录口令；首启设置口令，之后登录换取 **HttpOnly 会话 Cookie**（浏览器不存管理密钥）。无注册/找回/OAuth/passkey/2FA。
- **页面集合**（保留上游页面，删多用户/计费）：数据看板、渠道管理、模型管理、路由与故障切换（**独立页 `/routes`**）、令牌、请求日志、系统任务（**独立页 `/system-tasks`**）、性能指标、系统设置、试打台、关于/法律页等。**「系统信息」页已删除**（多节点实例视图，单节点部署不需要），其任务面板提为「系统任务」独立页。
- **构建**：Rsbuild（上游默认），产物交 Go `embed`；包管理使用仓库现行的 pnpm。
- **保留/删除**：除多用户/计费外全部上游页面保留；保留与删除的完整清单见 §1.3/§1.4 与 ui-spec §5。
- **实时机制**：车道运行态经 SSE 推送 + 30s 轮询兜底（PBR 自有 `/api/route-events`）。
- **产物形态**：单进程同时服务 `/v1/*`、`/api/*` 与静态控制台。
- **品牌**：全量替换为 PowerBarRations（见 §17）。

---

## 7. 路由与容错

### 7.1 车道与成员

- **路由键 = 模型名**（[routing-spec-v1.md](routing-spec-v1.md) §1.1）：渠道声明 `Models` 只是候选来源；**没有车道就不能调用**。
- 成员：`(渠道, 上游真名, 顺序)`，支持 `PublicAlias` 点名。上游真名解析：成员级 `UpstreamModel`（覆盖）> `Channel.ModelMapping[路由键]` > 路由键。
- **成员候选 = 任意启用渠道的任意已声明模型**（[ADR 0006](adr/0006-lane-free-member-composition.md)）：一条车道可跨渠道、跨模型自由组链，不要求成员声明了该路由键；**同一渠道可出现多次**（按 `(渠道, 上游真名)` 去重）。渠道声明 `Models` 仍只是"该渠道能提供什么"的目录，**绝不自动建车道**。
- 「模型名写错」「没建车道」「上游全挂」对下游同形：`503 No available channel for model <X>`。

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
- 切换成功后按 `AffinitySeconds` 粘住当前成员，避免抖动；成员失败立即结束亲和。
- 进程内保存；重启清空属可接受语义，README 写明。

### 7.5 熔断 + 半开自愈（新增）

- 键为**成员粒度**：车道内 `channel_id : upstream_model`（同一渠道的同一上游模型在一条车道里就是一个成员，与成员改名解耦）。
- **软/硬故障分离**：`429`/限流类为软故障，不得与硬故障同罚。
- 连续失败达阈值 → 打开；退避期到 → 半开放一个真实请求；成功即自动复通并写恢复事件日志。
- 触发证据除关键词外，补"滚动窗口失败率"（定长环形缓冲：数据面非阻塞上报、控制面评估）。
- **禁用语义 = "熔断中"**；保留开关名 `AutomaticEnableChannelEnabled` 兼容现网口径，**默认 true**（README 写明）。注意：该开关只是"允许写回 enabled"的许可，真正执行复通的是渠道巡检任务（`monitor_setting.auto_test_channel_enabled`，默认 **false**）或人工触发"测试全部渠道"；即默认部署下不会自动复通，README 必须如实写明。
- 阈值/半开周期进 `system/options` 可配，经 API 修改。

### 7.6 自动禁用关键词（行为继承，条目属部署数据）

自动禁用依赖关键词表命中上游错误体。某些欠费类上游以 **HTTP 400** 到达，按车道 §4.1 故障分类不属可重试/可判罚形态，关键词是唯一捕获路径；机制必须保留，部署侧自定义条目完整继承（条目见私有台账，不写入本文）。关键词表读写一律走 `PUT /api/system/options`。

> 关键词表在 PBR 里的实际消费者是**熔断器的错误分类**（`route.Classify`，§7.5）；基座遗留的"渠道自动禁用"（全局 `AutomaticDisableChannelEnabled` + 渠道 `auto_ban`）默认关闭且已被成员级熔断/半开自愈取代，控制台渠道表单不再暴露该开关。

### 7.7 路由与故障切换（成员链管理，侧边栏独立页 `/routes`）

用户的操作心智只有三步：**渠道里填上游与模型（上游命名不一致时配渠道映射）→ 路由与故障切换页（`/routes`）里为每个模型定"优先打谁、再打谁" → 令牌里允许这个模型**。

- `/routes` 页以**卡片网格**展示全部车道（对齐线上 octopus 分组页），卡片上给出成员顺序与运行态；新建/编辑进入**两栏编排器**：左栏「渠道 → 模型」可搜索选择器，右栏已选成员有序列表（排序 / 删除 / 改上游真名 / 清空）。
- **成员候选 = 任意启用渠道的任意已声明模型**（[ADR 0006](adr/0006-lane-free-member-composition.md)）：可跨渠道、跨模型组链，不要求同名，**同一渠道可出现多次**（按 `(渠道, 上游真名)` 去重）；路由键可任意命名，无需任何渠道声明过它。**没有「自动添加」**，成员全部人工挑选。
- **给渠道声明/新增模型绝不自动生成车道**：渠道 `models` 增删只改声明，车道必须人工显式创建；未固化的模型不可调用（503）。用户保存顺序即固化成 PBR 的 `Lane`（模式默认 `failover`）。
- "车道"是**唯一路由入口**；两种模式、六键、成员级覆盖/别名仍是可选的高级能力，`/api/lanes/**` 契约不变，`/routes` 页是它的友好视图。

**验收**：在 `/routes` 页为"模型1"设定"上游1 → 上游2"后，`GET /api/routes/模型1` 的成员顺序与之一致；把上游1 打成故障后，请求自动逃逸到上游2（fault_injection 覆盖）。

### 7.8 探活

`POST /api/lanes/{name}/probe`（逐成员）与 `POST /api/channels/{name}/test`（单渠道）；默认不内置定时器，由 API 或熔断器驱动。

---

## 8. 日志与记账（元数据 only）

单表 `request_logs`：

```
id, ts, lane_name, request_model, route_source, member_channel_id, member_channel_name, upstream_model,
token_id, key_name, inbound_format, success, http_status, error_kind,
error_summary(≤2KB 截断), prompt_tokens, completion_tokens, cache_read_tokens,
cache_write_tokens, reasoning_tokens, ttft_ms, total_ms, is_stream,
attempts(JSON), total_attempts, estimated_cost(仅折算)
```

`attempts` 元素：`attempt_num`、`member`、`status ∈ success|failed|cooldown|circuit_break|skipped`、`duration_ms`、`error_kind`、`msg`。另建每日/每小时聚合表供 `/stats` 与 UI 图表。

> 路由为模型名键控：配了车道时 `lane_name` 等于车道名（通常等于 `request_model`）；日志另记 `route_source ∈ explicit|unconfigured`，用于区分"已固化可调用"与"渠道声明但没建车道"。

**明确不许有**：`quota` 扣减、余额变更、请求/响应正文、任何"余额不足拒服务"逻辑。

**事件通知（Webhook）**：路由运行态的故障事件（熔断/冷却/恢复）可配置异步推送到外部 webhook 目标（本机通知中心等），投递语义与管理面见 §16.10 与 api-spec §5.8。

**成本折算**是可选能力：按**请求模型名**匹配**渠道级上游单价**（渠道 `setting.pbr_prices`，同一模型在不同上游可配不同采购价）；渠道未配价则**不折算（0）**。**没有全局默认单价层**（原 `PBRModelPrices` 全局表已移除——单用户自用每渠道自己定价即可）。**这是记账不是计费**——不参与准入、不扣余额；单价属部署数据。看板（概览/模型分析/成本统计）统一读 `GET /api/stats` 的聚合（`requests`/`successes`/`token`/`estimated_cost`）。

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
- `web/**`：**直接搬迁 new-api 上游前端**（见 §6 / ui-spec-v1.md），全量替换品牌；删除计费/多用户页面，其余保留。
- WS 池代码：保留，不删。任务插件/异步任务子系统、JS 插件基座与 Midjourney 全链路已删除（§1.3，个人自用不接文生视频/文生图服务）。

### 10.2 当前裁剪边界

计费、多用户及其 UI 已从当前产品范围移除；适配器只保留正确转发所需的字段和代码。
成本折算仍是只读观测能力，不恢复额度、扣费或余额拒服务。历史裁剪过程和验证记录仅供追溯，
不构成当前实现要求。

### 10.3 从 upstream 取什么

| 能力 | 取法 |
|---|---|
| 六键 `relay_config` 语义 | 照搬 bestruirui 上游字段与默认值/Normalize |
| 成员冷却 + 亲和 | 移植语义（进程内 cooldowns + affinity） |
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
9. **AGPL-3.0**：上游均 AGPL。本系统仅本机自用（无公网入站、不分发），不触发开源义务；但**不许把仓库推到公开可见位置**。

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
- **响应契约以表为源**（与端点表同一取向）：管理端点的**路由、处理器与响应策略同处声明**——稳定面在 `internal/api/ops_routes.go` 的 `opsRoutes` 表（注册即登记，不可能漏）；基座遗留面按 `METHOD + c.FullPath()` 在 `internal/apiresp` 登记**需要定制成功体的路由**（与 `middleware/audit.go` 既有 `auditRouteActions` 同法），其余路由由 `apiresp.Default` 兜底。守卫测试 `router/envelope_coverage_test.go` 校验：稳定面策略不得登记不存在的路由、不得漏声明成功/失败/直通三者之一；基座面登记不得漂移。响应归一化（基座 `{success,message,data}` → 契约形态）由 `apiresp.Middleware` 按该表机械执行，**handler 不重复实现信封**；SSE 在写头/Flush 时立即直通，不缓冲。
- **给人/AI 的入口**：`GET /doc`（默认 `text/markdown`，浏览器 `Accept: text/html` 返回说明页）、`GET /llms.txt`（`text/plain`）、`GET /doc/ui`（复用 GitHub 项目 Scalar 渲染 `/api/openapi.json`）。三者与 `/api/openapi.json` 均免鉴权，便于 AI 先读手册再自行派生管理密钥。
- **前缀**：管理面规范前缀为 `/api`，`/api/v1` 为兼容别名（注册相同处理器）。与 AI 契约冲突的控制台内部资源（模型目录、审计）收在 `/api/console/*`；其余控制台内部接口仍在 `/api/*` 下同权限可用，但不属于稳定契约。

### 16.4 存储与当前 schema

- 开发阶段只保证当前 PBR schema 在新建 SQLite 数据库上初始化成功；破坏性 schema 变化允许重建开发库。
- 不提供旧 `new-api`/`octopus` 数据库导入、历史数据回填、双写、旧字段兼容或数据恢复兜底。
- 同版本 `/api/export` 与 `/api/import` 只处理 PBR 配置快照；它们不是数据库迁移工具。

### 16.5 日志保留

- 不引入 cron。保留策略由 `system/options.log_retention_days` 配置（默认 30），并提供 `POST /api/logs/prune?before=<ts>&dry_run=` 由外部按需触发。
- 聚合表（`pbr_stats_hourly`）长期保留（体积小）；明细表按上述策略清理。
- **`GET /api/stats` 必须读聚合表**（hour 直读、day 由小时桶上卷），不得读明细表——否则一次 prune 就把历史统计抹掉，与"聚合长期保留"自相矛盾。

### 16.6 并发与超时

- 每次尝试的超时由车道六键与成员级覆盖决定；流式用"首事件超时"与"整请求超时"双闸控制。
- 探活并发上限可配（默认 4），避免对上游造成突发压力。

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
| 7 | 上游单价与成本折算 | **单层单价**：只有**渠道级上游单价**（渠道 `setting.pbr_prices`，人民币 / 百万 token，字段 `input`/`output`/`cache_read`/`cache_write`），在渠道编辑「上游单价」页签配置，计价键为请求模型名；渠道未配价 → 不折算（0）。**全局默认单价表（`PBRModelPrices`）已移除**。只用于日志 `estimated_cost` 与看板成本统计（含"渠道 × 模型"维度），**不参与准入、不扣额度、与下游计费无关**（本项目无计费）。基座 `setting/ratio_setting` 不再充当单价表（它仍是惰性遗留：提供路由用的模型名归一化 `RoutingMatchModelName`） |
| 8 | 请求头兼容 | 管理面仅收 `Authorization`；模型面 `Authorization` 与 `X-Api-Key` 都收 |
| 11 | 开工基座 | **以 new-api 源码迁入为基座**，非净室重写；前端**同样直接搬迁上游 `web/`**（见 §6 / §10.5） |
| 12 | 亲和默认值 | 默认 `member_affinity_seconds=0`（**与现网一致，避免故障切换后长时间粘在备用成员**），可配 |
| 13 | 前端包管理与适配范围 | 前端复用上游 `web/`（Rsbuild + pnpm）；保留除多用户/计费外的运维页面；实质改造 = **成员链（故障切换）管理**（落位为侧边栏独立页 `/routes`，见 §7.7），并把认证接到 PBR 口令会话 |

### 16.10 Webhook 事件通知（已定）

**定位**：把路由运行态的故障事件推送给**任意外部消费方**，**只推事件、不承载指令**。这是管理 API 的一部分：PBR 只负责**定义推送契约并投递**，接收方的验签、路由、呈现一律由消费方自行实现（本机对接——如推给某 agent 的通知通道——由该 agent 侧做，PBR 不内置任何针对特定接收者的集成）。事件量低频（分钟级偶发），选型为 HTTP webhook 推送——不做 WebSocket/SSE 订阅面（日后若需实时全量订阅再评估 SSE，控制台仪表盘已有 SSE 先例）。

**事件源**：`internal/route` 运行态事件（`circuit_open` / `circuit_half_open` / `circuit_closed` / `cooldown`）。经订阅钩子**异步旁路**投递，绝不阻塞请求路径。v1 不含探活启停事件。

**配置**（system/options 键 `PBRWebhookTargets`，JSON 数组）：`[{name, url, secret, enabled, events[]}]`；`events` 为事件类型白名单（空 = 全部）。管理面读配置时 `secret` 只回显掩码。

**投递语义**：
- 请求体 JSON：`{"type":"pbr","text":"<人类可读摘要>","event":{ts,type,lane,member,detail}}`——带 `text` 字段使"只展示文本"的消费方（通知中心红色档）零改造接入。
- 签名采用**通用 HMAC-SHA256 方案**：头 `X-Webhook-Timestamp`（Unix 秒）+ `X-Webhook-Signature-V2`（`HMAC-SHA256(secret, "{ts}.{raw_body}")`，hex）。验签步骤与偏差窗口写入 api-spec §5.8 与 /doc 手册，消费方照文档实现即可，不依赖任何具体接收端。
- 单次投递超时 8s；失败按 5s/30s/120s 退避重试 3 次，耗尽记入投递日志（`webhook_deliveries` 表：ts、target、event、status、http_status、error、attempt）。
- **防风暴**：同一 (target, lane, member, event) 在 60s 窗口内只发一条（合并计数），避免抖动上游刷屏。
- 投递日志随 `PBRLogRetentionDays` 由 prune 一并清理。

**管理面**：`GET/PUT /api/webhooks`、`POST /api/webhooks/test`、`GET /api/webhooks/deliveries`（规范前缀 `/api`，`/api/v1` 为兼容别名；契约见 api-spec §5.8）。

**验收**：手动熔断一个成员 → 目标秒级收到签名正确的 JSON；目标不可达时重试与死信符合上表；投递日志可查；控制台可编辑并回读。

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
| 4 | 仓库远程 | **只留本地**，不配置 remote |

### 非阻塞事项（不挡开工）
- 成本折算单价表的实际数值（属部署数据，运行期在**各渠道的 `pbr_prices`** 里配置，无全局层）。
