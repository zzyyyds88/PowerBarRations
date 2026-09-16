# PowerBarRations 设计基线 v1

> 立项：2026-09-14。本文是 PowerBarRations 项目唯一有效的产品与技术设计基线（SSOT），取代 `/root/.hermes/notes/gateway-rebuild-brief-v1.md`。
> 项目性质：对开源项目 new-api 的**二次开发**——复用其转发管道、厂商适配层与前端控制台，重写路由核心与访问面，**只做"去计费 + 单用户化"裁剪**。
> 配套规范（均为规范性文件，与本文同级）：
> - 管理 API 完整契约：[`api-spec-v1.md`](api-spec-v1.md)
> - 路由与故障转移：[`routing-spec-v1.md`](routing-spec-v1.md)
> - 令牌与认证：[`token-spec-v1.md`](token-spec-v1.md)
> - UI 控制台：[`ui-spec-v1.md`](ui-spec-v1.md)
> 冲突时：本文管架构与取舍，各 spec 管本领域细节契约。
>
> **文档纪律（重要）**：本文及配套规范只描述系统本身，**不含任何部署私有数据**——不写具体渠道名、模型名、车道名单、厂商地址、凭据、成本单价。这类内容属"运维私有台账"，不入仓库，只在迁移阶段作为输入提供（见 §11）。

---

## 0. 相对 brief v1 的改动摘要

| # | brief v1 的做法 | 本文改法 | 为什么 |
|---|---|---|---|
| 1 | 保留令牌表语义，车道用 `lane_allowlist` 限流，白名单"二选一"留悬念 | **取消"令牌网关"复杂度**：管理面只认一把静态管理密钥；客户端密钥**默认允许全部车道、可显式拒绝**，永不出现"忘了加白名单 → 故障那一刻 400" | 用户判定令牌网关设计过度复杂；白名单是现网两代网关的反复踩坑源 |
| 2 | AI 运维需拷库改 options、抓 SPA 反汇编接口 | **管理面按"AI 可调用"重设计**，并给出完整契约与示例（api-spec-v1.md） | 用户要求"开放 API 给 AI 调用，不再改文件" |
| 3 | 逐文件手删 100+ 计费/前端文件 | **三段式减脂**：先逻辑停用（不注册路由、计费惰性化）→ 测试全绿 → 最后物理清除 | 适配器接口 `Adaptor` 每个方法吃 `*RelayInfo`，而它内嵌计费字段；硬删会逼停适配器（§2.6） |
| 4 | 以同名分叉仓库为移植源 | **更正上游**：线上路由层真实上游是 `bestruirui/octopus`；六键/冷却/亲和以它为准；熔断器等属**新增** | 镜像 label 记录了 source/revision；分叉仓库 schema 已分叉（§2.7） |
| 5 | 车道模式来源存疑 | 实现 **failover（默认）/ manual / weighted / round_robin 四种**，后两者为新增 | 用户确认需要 |
| 6 | 未明确 UI 去留 | **保留 UI 控制台**，以线上路由层前端为蓝本迁移功能原理（个人自用仍要能人看） | 应用户纠正：只砍计费，不砍功能；并指定 UI 蓝本 |
| 6b | 控制台以**线上路由层前端**（octopus）为蓝本 | **改为以 new-api 前端为蓝本整体搬迁**（`web/` 直接取自上游源码），只删多用户/计费页面 | 应用户二次纠正：new-api 的渠道管理/模型管理与其后端接口天然配套（PBR 已保留 93 个基座路由，路径一致），而 octopus 蓝本需大量改造；直接在 new-api 前端上做减法风险与工作量都更低 |
| 7 | 未明确"开放 API 如何被调用" | 新增 api-spec 的 **AI 调用手册**：发现、认证、典型工作流 curl 示例 | 应用户要求写实 |
| 8 | 车道是"必须先创建的对象" | **路由键 = 模型名**：渠道声明 `Models` 即自动成链，任意模型零配置可路由；显式车道降级为可选覆盖层 | 用户澄清的目标流程：下游请求模型1 → 上游1的模型1 → 上游2的模型1，且"这个模型可以是所有模型" |

---

## 1. 定位、目标与非目标

### 1.1 定位

**个人纯自用、单用户的 LLM 聚合网关。** 使用者只有一个人（以及代表他工作的 AI）；下游是 AI 客户端（harness）与少量人工排障。因此：去掉一切多用户/计费设施，但**保留全部与"把请求正确转发出去"有关的功能**，包括管理控制台。

### 1.2 目标

- **G1 单层**：一个二进制、一个 SQLite、一条管道。车道（Lane）是唯一路由入口，成员 = `(渠道, 上游真名, 优先级)`。不再有第二层网关，不再需要任何模型映射表。
- **G2 AI 友好**：全部管理能力以版本化 HTTP API 暴露，自描述（OpenAPI）、幂等、可回读、错误可机器判定；AI 不改文件、不查库、不抓前端。完整契约见 api-spec-v1.md。
- **G3 下游零改动**：存量车道名与 `/v1/*` 协议、错误语义一律不变；下游只改 base_url。
- **G4 自愈**：保留现行路由层已验证的成员冷却 + 亲和，并补上厂商层"只禁不通、无半开自愈"的缺陷（new-api 上游 issue #5420）。软故障（限流）不得误判为硬故障。
- **G5 保留厂商适配层**：new-api `relay/channel/` 下 40 家适配器原样复用。
- **G6 有人看的控制台**：提供个人控制台，覆盖车道/渠道/密钥/日志/统计/设置/试打。规格见 ui-spec-v1.md。
- **G7 只看不扣**：日志只存元数据，成本只做折算记账，不做任何计费/扣费/余额拒服务。

### 1.3 删除范围（**只裁计费与多用户**，其余不裁）

**计费/额度**：配额扣减、预扣费、结算、钱包、充值、兑换码、订阅与套餐、账单、阶梯计费表达式、价格倍数（ModelRatio/GroupRatio/CompletionRatio）、按量扣费逻辑。

**支付**：全部第三方支付通道（stripe / creem / waffo / epay 等）。

**多用户**：用户管理、注册、多用户登录、角色与授权（casbin/authz）、2FA、passkey、OAuth 登录、邮箱验证、签到、排行榜、用户分组。

**对应 UI 页面删除**：wallet、pricing、redemption-codes、subscriptions、users、rankings、legal、about、security(2FA/passkey)、profile、部分 home 营销页。完整清单见 ui-spec-v1.md §4。

### 1.4 保留范围（**默认全部保留**，不因个人自用而裁剪）

渠道、车道（新）、模型目录与厂商适配层、客户端密钥、请求日志、用量与记账统计、Dashboard、Playground、系统设置（relay 相关）、初始化向导、错误页、**图像/视频/音乐生成任务（路由与页面均保留并暴露）**、JS 任务插件、WebSocket 上游池。

> 与"正确转发"无关的重型子系统**不物理删除**，也不在控制台隐藏；默认保持可用。
> **部署形态**：仅 Docker（单容器）。不考虑桌面端，不 vendor electron。
> **思考参数**：各厂商等价字段**沿用适配器现状**，不重新梳理映射表（详见 §16.1）。

### 1.5 唯一部署约束

只支持 **SQLite** 单节点部署（多数据库/多节点代码保留，但不在部署矩阵内），以避免接口分叉。

---

## 2. 现状结构基线（不含部署数据）

### 2.1 拓扑

现行部署为两层：AI 客户端 → 路由层（按"分组/车道"做优先级与故障切换）→ 厂商与账务层（持有厂商 key、做协议适配与用量记账）→ 厂商。目标是把两层塌成一层。

### 2.2 路由层结构（决定单层化的动机）

- 路由对象为"分组"，成员表仅有 `(group_id, channel_model_id, priority)`；成员指向的"渠道-模型"记录只有 `(channel_id, name)`，**没有别名列**；渠道本身只有整渠道级的模型名字符串。
- 结论：路由层**不具备"成员级改模型名"的能力**。因此"用某渠道的某真实模型名"无法在路由层表达，被迫下沉到厂商层的映射表。
- 这就是本项目的立项目标：给成员加 `upstream_model` 与可选 `public_alias` 两列，把三层塌成一层。

### 2.3 路由层容错能力（现行）

- 模式仅 `manual | failover` 两种。
- 自愈机制为**成员冷却 + 亲和**：成员耗尽尝试后写入冷却截止，到期自动重新可用；切换成功后按亲和时间粘住当前成员。状态为进程内，重启清空。
- 六键配置（attempts / retry_interval / non_stream_timeout / stream_first_event_timeout / cooldown / affinity）是分组的 JSON 字段。
- **没有**熔断器、没有滚动成功率离群窗口、没有逐尝试链日志。→ 这些是本项目要新增的能力。

### 2.4 厂商/账务层结构

- `channels` 持有厂商 base_url / key / param_override / 协议类型；`abilities` 是 `(group, model, channel_id, priority, weight)` 选择表；`tokens` 是客户端准入；`options` 是全局开关。
- 模型改名由 `channels.model_mapping`（请求名 → 真名）承担，这是"映射工序"所在。
- 全局重试由 `options.RetryTimes` 单一值控制，**无车道级差异**。
- `auto_ban` 只禁不通：自动禁用后无半开探测，必须人工启用（上游 issue #5420 未修）。

### 2.5 适配器规模

`relay/channel/` 下 **40 家**厂商适配器，覆盖主流 OpenAI 兼容、Anthropic、Gemini、各家国内厂商与本地推理。这是二次开发要保留的核心资产。

### 2.6 关键耦合（约束减脂与迁入策略）

适配器**不是自包含的**。实测 `relay/channel/**` 对本仓库内部包的引用分布：

| 包 | 引用数 | 性质 |
|---|---|---|
| `relaykit/dto` / `relay/common` / `relaykit/types` | 112 / 103 / 80 | 请求响应类型与 `RelayInfo` |
| `common` / `constant` / `relay/helper` / `relay/constant` | 67 / 37 / 32 / 35 | 基础类型与常量 |
| **`service`** | **51** | 绝大多数是**传输/IO 辅助**：`CloseResponseBodyGracefully`(49)、`IOCopyBytesGracefully`(23)、`ResponseText2Usage`(21)、`ConvertRequest`(12)、`InitHttpClient`(9)、`ConvertResponse`(8)、`ValidUsage`、`ShouldCopyUpstreamHeader`、`CountTokenRealtime`…**并非计费** |
| **`setting/reasoning`** | ~60 | **思考后缀三态与 effort 解析**（即"思考参数沿用现状"的现成实现） |
| `setting/model_setting` / `operation_setting` | 37 / 24 | 全局设置与 `SelfUseModeEnabled` |
| `setting/ratio_setting` | ~40 | 单价/倍率（**成本折算可直接复用**） |
| `logger` / `model` / `pkg/jsplugin` / `plugins` / `pkg/billingexpr` | 23 / 4 / 6 / 2 / 2 | 日志、任务与插件类型 |

同时 `relay/common/relay_info.go` 的 `RelayInfo` 内嵌 `BillingSettler`、订阅、预扣费、配额钳制、阶梯计费快照等字段。

**结论**：既不能把计费类型从 `RelayInfo` 里摘掉（会改动 40 家适配器），也不能把适配器从 `service`/`setting` 里剥出来（要重写传输辅助、思考后缀解析、任务基础设施、单价表）。→ **不做净室 vendor，改以 new-api 源码为基座迁入**（§10.5）；计费只做惰性化，不清空 `service`/`setting` 包。

### 2.7 上游 provenance 更正

- 线上路由层镜像的 label 记录了 `source = github.com/bestruirui/octopus` 与具体 revision。**它才是线上行为的定义源**：六键、成员冷却、亲和、failover 语义均以它为准。
- 调研中同时出现的同名分叉仓库（Hureru/octopus）schema 已分叉：`Group.Mode` 改为整数、引入 `GroupPreset`、**不含六键**。其熔断器/离群窗口/健康快照/逐尝试日志是它独有的。
- 含义：**车道成员语义移植自 bestruirui；熔断器等属 PBR 新增能力，可参考分叉仓库的三态算法按本项目模型重写。**

### 2.8 前端结构（**直接搬迁 new-api 前端**）

控制台**整体取自 new-api 上游 `web/`**（Rsbuild + React + TanStack Router + Base UI + Tailwind），在其上做减法与接线改造：删多用户/计费页面、把认证换成 PBR 口令会话、把"模型管理"接入 PBR 的成员链（故障切换）。

**为什么直接搬**：PBR 已保留 new-api 的管理面后端（渠道 `/api/channel/**`、模型元数据 `/api/models/**`、厂商、部署、任务插件、系统任务、性能、日志、`/api/option`、系统信息等 **93 个路由**），路径与前端调用**一一对应**；而删除的多用户/计费接口，恰好是本项目明确不要的部分。反过来，octopus 蓝本只有少量页面与 PBR 对应，其余都要重写。因此**在前端源码上做减法**比"以另一上游为蓝本重做"风险与工作量都更低，也更符合用户"其他功能全都要"的要求。

- **构建**：Rsbuild（上游默认），产物交 Go `embed`；包管理沿用上游 `bun.lock`（如环境不便可用 pnpm）。
- **保留**：渠道、模型、令牌、日志、仪表盘（数据看板）、试打台、系统设置、任务插件、系统信息、性能指标、关于/法律页等**除多用户/计费外全部**。
- **删除**：用户/注册/登录（换 PBR 认证）、钱包、充值、订阅、兑换码、排名、定价同步、签到、个人中心、2FA/passkey 等。
- **改造点**（唯一实质改造）：**模型管理页内嵌成员链（故障切换）**——见 §7.7。

---

## 3. 架构

### 3.1 一句话

**一个二进制、一个 SQLite、一条管道**：**请求里的模型名就是路由键**。渠道声明自己提供哪些模型，网关对每个模型名自动形成一条成员链（按渠道优先级），于是任何模型都天然获得"上游1失败 → 上游2"的故障转移，零配置即可用；显式命名的车道只是可选的覆盖层（自定义顺序、成员改名、池化不同上游的不同模型）。中间不再有第二层网关，也不再需要任何模型映射表。

### 3.2 结构示意（占位符为示意，非真实数据）

```
渠道/vendor-a（priority 1）—— model-1, model-2, model-3
渠道/vendor-b（priority 2）—— model-1, model-2, model-3

下游请求 model-1  →  vendor-a 的 model-1  →（失败）→  vendor-b 的 model-1
下游请求 model-2  →  vendor-a 的 model-2  →（失败）→  vendor-b 的 model-2
（model-3 亦然；任意模型名都按同一规则自动成链）
```

```
客户端 --model=<模型名>--> PowerBarRations
                              ├─ 隐式车道 "model-1"：成员 = 所有声明提供 model-1 的渠道，按渠道优先级排序
                              ├─ 隐式车道 "model-2" / "model-3" / …（任意模型，无需配置）
                              ├─ 显式车道（可选覆盖层）：自定义顺序 / 成员改名 / 池化
                              └─ 渠道 Channel: 厂商 base_url + key + 模型清单 + param_override + 协议类型(40 家适配器原样复用)
                                        ▲
                    管理面(API) ────────┘
                    控制台(UI) ─────────┘  同一个二进制，UI 产物 embed 进二进制
```

### 3.3 概念映射（旧 → 新）

| 旧概念 | 新概念 | 说明 |
|---|---|---|
| 路由层 `group` / 厂商层 `group` | **Lane 车道（键 = 模型名）** | 请求 `model` 即路由键；渠道模型清单自动成隐式车道，显式车道为可选覆盖层 |
| 厂商层 `abilities` 行 | **成员** | `(渠道, 上游模型名, 优先级)`；隐式成员由 `Channel.Models` + `Channel.Priority` 派生 |
| 厂商层 `channels` | **Channel 渠道** | 厂商 base_url + key + param_override + 协议类型 |
| 厂商层 `model_mapping` | **`LaneMember.UpstreamModel`** | 映射整层消失，改名能力下沉到成员级 |
| 厂商层 `tokens` / 路由层 `api_keys` | **ClientKey 客户端密钥** | 瘦身，见 §9 |
| 厂商层 `users` | 无 | 单用户系统，管理员身份只由管理密钥承载 |
| 厂商层 `logs` | **RequestLog 元数据日志** | 只存元数据，见 §8 |

### 3.4 数据模型（SQLite，GORM）

```go
type Channel struct {                 // 厂商渠道
    ID        int
    Name      string   // 唯一
    Type      string   // 协议族：openai|anthropic|gemini|ollama|...（映射 40 家适配器）
    BaseURL   string   // 只存到版本根（如 https://host/v1），路径拼接交给适配器
    Priority  int      // 隐式成员链排序依据，数字大者优先
    Models    []string // 本渠道提供的模型名（路由键）；声明即可路由
    Key       string   // 只写不读：响应脱敏；不打印进日志
    ParamOverride string          // JSON，统一各厂商思考参数差异的落点
    Enabled   bool
    Proxy     string
    CreatedAt, UpdatedAt time.Time
}

type Lane struct { // 显式车道 = 可选覆盖层；未显式定义的模型走隐式链
    ID      int
    Name    string   // 可路由的模型名（唯一）
    Mode    string   // failover(默认) | manual | weighted | round_robin
    Config  LaneRelayConfig        // 六键，见 §7.3
    Members []LaneMember           // 有序
    Enabled bool
}

type LaneMember struct {
    ID            int
    LaneID        int
    ChannelID     int
    UpstreamModel string  // 发给厂商的真实模型名 —— 映射在此终结
    PublicAlias   string  // 可选：客户端可点名该成员的名字（空 = 不可点名）
    Priority      int     // 数字大者优先（与渠道优先级语义一致）
    Weight        int     // weighted 模式使用
    // 成员级覆盖（nil = 继承车道）
    MaxAttempts                     *int
    RetryIntervalSeconds            *int
    NonStreamResponseTimeoutSeconds *int
    StreamFirstEventTimeoutSeconds  *int
    CooldownSeconds                 *int
}

type ClientKey struct {               // 客户端准入
    ID        int
    Name      string   // 唯一，账务归属标识
    KeyHash   string   // sha256，不存明文
    KeyPrefix string   // 便于展示，如 pbr-xxxx
    Enabled   bool
    DenyLanes []string // 显式拒绝的车道；空 = 允许全部（默认放行）
    CreatedAt time.Time
    LastUsedAt *time.Time
}

type RequestLog struct { /* 见 §8 */ }
type AuditLog   struct { /* 见 §5 */ }
type Option     struct { Key, Value string }
```

**约束**：不允许出现 `users` 表外键；不允许出现配额字段（`quota`/`remain_quota`/`used_quota`）；不允许出现"余额不足拒服务"逻辑。

---

## 4. 对外模型 API（下游契约）

| 端点 | 说明 |
|---|---|
| `POST /v1/chat/completions` | OpenAI 兼容，流式 SSE / 非流式 |
| `POST /v1/responses` | OpenAI Responses |
| `POST /v1/messages` | Anthropic 入口 |
| `POST /v1/embeddings` | OpenAI 兼容嵌入 |
| `GET /v1/models` | 列出当前密钥可见的车道 |

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

1. **认证**：管理面无账号体系，只有**一个登录口令**，支撑两条等价通道：
   - **控制台（人）** → `POST /api/auth/login` 换取 **HttpOnly 会话 Cookie**，浏览器不再保存管理密钥；
   - **AI/脚本** → 用 `管理密钥 = Base64(SHA256(登录口令))` 作 `Authorization: Bearer`，可自行计算，无需人工复制。
   首启时设置口令（详见 [`token-spec-v1.md`](token-spec-v1.md) §2）。模型面用客户端密钥。**模型面与管理面同端口全部监听 `0.0.0.0` 对局域网开放**（§18），凭凭据鉴权，不做来源限制；口令必须是长随机串。
2. **幂等写**：`PUT /api/{resource}/{name}` 收全量对象 upsert。
3. **写后回读**：任何写操作响应前重新读库，响应体即最终状态；集成测试断言"写完 GET == 提交值"。
4. **`?dry_run=true`**：返回变更 diff，不落库。
5. **统一错误包络**：`{"error":{"code","message","hint"}}`，`code` 为稳定字符串。
6. **分页**：cursor 分页，`limit` 有上限。
7. **审计**：每次变更写 `audit_logs`（只记元数据）。
8. **导出/导入**：`GET /export` / `POST /import?dry_run=` 取代"拷库备份/回滚"。
9. **自描述**：`GET /openapi.json`、`GET /capabilities`。

### 5.2 为什么这样设计（回答"AI 怎么用"）

AI 侧的全部运维动作——建渠道、建/改车道、调成员顺序、探活、重置熔断、读日志、导出配置——都是一次 HTTP 调用，且响应可判定（HTTP 状态 + 稳定 `code` + 回读后的对象）。不需要解析 prose，不需要读库，不需要抓前端。典型工作流与可直接照抄的 curl 示例见 api-spec-v1.md §7。

---

## 6. UI 控制台

> 完整页面清单、逐页字段/交互/状态、模块映射与验收见 **[`ui-spec-v1.md`](ui-spec-v1.md)**。本节只定架构性约定。

- **蓝本 = new-api 上游前端**（Rsbuild + React + TanStack Router + Base UI + Tailwind）：**直接整体搬迁，做减法（删多用户/计费）+ 接线（认证、成员链）**，而非另起炉灶。
- **认证极简**：无账号，只有登录口令；首启设置口令，之后登录换取 **HttpOnly 会话 Cookie**（浏览器不存管理密钥）。无注册/找回/OAuth/passkey/2FA。
- **页面集合**（保留上游页面，删多用户/计费）：数据看板、渠道管理、模型管理（含成员链/故障切换）、令牌、请求日志、任务插件、系统信息、性能指标、系统设置、试打台、关于/法律页等。
- **实时机制**：车道运行态经 SSE 推送 + 30s 轮询兜底（PBR 自有 `/api/route-events`）。
- **产物形态**：Rsbuild 构建产物 embed 进二进制，单进程同时服务 `/v1/*`、`/api/*` 与静态控制台。
- **品牌**：全量替换为 PowerBarRations（见 §17）。

---

## 7. 路由与容错

### 7.1 车道与成员

- **路由键 = 模型名**（[routing-spec-v1.md](routing-spec-v1.md) §1.1）：渠道声明 `Models` + `Priority` 即自动形成隐式成员链；显式车道同名时覆盖之。
- 隐式成员：`(渠道, 同名模型, 渠道优先级)`；显式成员：`(渠道, 上游模型名, 优先级)`，支持改名与 `PublicAlias` 点名。
- 成员直接写上游真名 → `model_mapping` 整层删除。
- 「模型名写错」与「上游全挂」对下游同形：`503 No available channel for model <X>`。

### 7.2 选择模式（四种）

| 模式 | 语义 |
|---|---|
| `failover`（默认） | 按 priority 降序选；失败按预算尝试后冷却并逃逸到下一成员 |
| `manual` | 只用手工选中的成员（沿用现行路由层语义） |
| `weighted` | 按成员 `Weight` 加权随机；失败同样走尝试预算与冷却 |
| `round_robin` | 在可用成员间轮询 |

四种模式必须复用同一套冷却/熔断/日志基础设施，不许为某模式另起一套。

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

默认值取 upstream `DefaultGroupRelayConfig`（2 / 3 / 120 / 30 / 60 / **0**）；**逐车道覆盖值属部署数据**。六键取代厂商层全局 `RetryTimes`：车道级可覆盖、成员级可再覆盖。

> **`member_affinity_seconds` 默认改为 0（相对上游的 300）**：上游的亲和是为"多用户共享车道时压低抖动"而设；本项目是单用户自用网关，亲和会让"高优先级成员恢复后仍被低优先级成员粘住"，直接掩盖 priority 的语义。默认 0 = 不做粘滞，每次请求都从优先级最高的可用成员开始；需要压抖动时按车道显式配置（六键本来就支持车道级覆盖）。

**超时算术（文档必写）**：最坏判定耗时 = 成员数 × attempts ×（超时 + 重试间隔），端到端 = 客户端重试次数 × 该数。

### 7.4 成员冷却 + 亲和（移植现行路由层语义）

- 成员耗尽 attempts → 写入冷却截止 `now + CooldownSeconds`，期间被跳过；到期自动重新可用。
- 切换成功后按 `AffinitySeconds` 粘住当前成员，避免抖动；成员失败立即结束亲和。
- 进程内保存；重启清空属可接受语义，README 写明。

### 7.5 熔断 + 半开自愈（新增）

- 键为**成员粒度** `channelID:keyID:modelName`。
- **软/硬故障分离**：`429`/限流类为软故障，不得与硬故障同罚。
- 连续失败达阈值 → 打开；退避期到 → 半开放一个真实请求；成功即自动复通并写恢复事件日志。
- 触发证据除关键词外，补"滚动窗口失败率"（定长环形缓冲：数据面非阻塞上报、控制面评估）。
- **禁用语义 = "熔断中"**；保留开关名 `AutomaticEnableChannelEnabled` 兼容现网口径，**默认 true**（README 写明）。
- 阈值/半开周期进 `system/options` 可配，经 API 修改。

### 7.6 自动禁用关键词（行为继承，条目属部署数据）

自动禁用依赖关键词表命中上游错误体。某些欠费类上游以 **HTTP 400** 到达，不在默认重试状态码集内，关键词是唯一捕获路径；机制必须保留，部署侧自定义条目完整继承（条目见私有台账，不写入本文）。关键词表读写一律走 `PUT /api/system/options`。

### 7.7 模型管理内的成员链（故障切换）

用户的操作心智只有三步：**渠道里填上游与模型 → 模型管理里定"这个模型优先打谁、再打谁" → 令牌里允许这个模型**。因此"成员链/故障切换"不再要求用户理解独立的"车道"概念，而是**在模型管理页内联配置**：

- 模型管理页对每个模型展示：声明了它的渠道（隐式链）、显式配置的成员链（若有）、当前解析顺序。
- 用户可把某模型的成员链固化为显式顺序（拖拽/设 priority），即 PBR 的 `Lane`（模式默认 `failover`）。
- 未固化时保持 PBR 现有语义：**渠道声明即自动成链**（按渠道 priority 降序），零配置可路由。
- "车道"作为**可选覆盖层**仍然存在（四种模式、六键、成员级覆盖），但不作为用户的必经入口；`/api/lanes/**` 契约不变，模型管理页只是它的友好视图。

**验收**：在模型管理页为"模型1"设定"上游1 → 上游2"后，`GET /api/routes/模型1` 的成员顺序与之一致；把上游1 打成故障后，请求自动逃逸到上游2（fault_injection 覆盖）。

### 7.8 探活

`POST /api/lanes/{name}/probe`（逐成员）与 `POST /api/channels/{name}/test`（单渠道）；默认不内置定时器，由 API 或熔断器驱动。

---

## 8. 日志与记账（元数据 only）

单表 `request_logs`：

```
id, ts, lane_name, request_model, route_source, member_channel_id, member_channel_name, upstream_model,
token_id, token_name, key_label, inbound_format, success, http_status, error_kind,
error_summary(≤2KB 截断), prompt_tokens, completion_tokens, cache_read_tokens,
cache_write_tokens, reasoning_tokens, ttft_ms, total_ms, is_stream,
attempts(JSON), total_attempts, estimated_cost(仅折算)
```

`attempts` 元素：`attempt_num`、`member`、`status ∈ success|failed|cooldown|circuit_break|skipped`、`duration_ms`、`error_kind`、`msg`。另建每日/每小时聚合表供 `/stats` 与 UI 图表。

> 路由为模型名键控：隐式路由时 `lane_name` 即等于 `request_model`；日志另记 `route_source ∈ implicit|explicit`，便于区分"走的是自动链还是显式覆盖层"。

**明确不许有**：`quota` 扣减、余额变更、请求/响应正文、任何"余额不足拒服务"逻辑。

**成本折算**是可选能力：按请求模型先取**渠道级上游单价**（渠道 `setting.pbr_prices`，同一模型在不同上游可配不同采购价），没有再看**全局默认单价表**（`system/options` 的 `PBRModelPrices`）；两者都没有则不折算。**这是记账不是计费**——不参与准入、不扣余额；单价属部署数据。

---

## 9. 密钥模型（取代"令牌网关"）

> 完整设计见 **[`token-spec-v1.md`](token-spec-v1.md)**。本节只列要点。

| | 管理凭据 | 客户端密钥 ClientKey |
|---|---|---|
| 面向 | 控制台登录 + `/api/*` | `/v1/*` 模型流量 |
| 数量 | 一个登录口令（无账号） | 每消费者一把（分账） |
| 生成 | 由登录口令派生（`Base64(SHA256(口令))`） | 服务端随机生成，明文只回显一次 |
| 存储 | 只存 `sha256(管理密钥)` | 只存 `sha256(明文)` + 展示前缀 |
| 默认权限 | 全量 | **允许全部车道**，只能显式拒绝 |
| 额度/过期 | 无 | 无（`ExpiresAt` 仅安全阀） |

- **删除** `remain_quota`、`unlimited_quota`、`used_quota`、`cross_group_retry`、`group`、`supported_models` 白名单。
- **不接受"白名单模式"**：默认放行 + 显式拒绝，消灭"忘了加名单 → 故障时才 400"。
- 存量凭据导入（迁移阶段）：**密钥值不变**（下游零改动），权限一律 allow-all；名单见私有台账。

---

## 10. 源码复用与工程骨架

### 10.1 保留什么（来自 new-api）

- `relay/channel/**`：40 家厂商适配器**原样复用**。
- `relay/` 转发管道：协议转换、SSE 流式、`relay/helper`（价格相关函数除外）、`relaykit/`、`dto/`、`constant/`、`common/`（必要部分）、`i18n/`。
- `web/**`：**直接搬迁 new-api 上游前端**（见 §2.8 / ui-spec-v1.md），全量替换品牌；删除计费/多用户页面，其余保留。
- 图像/视频/任务/插件/WS 池代码：保留，不删。

### 10.2 三段式减脂

1. **逻辑停用**：不注册用户/计费/充值/订阅/兑换/排行榜路由；`BillingSettler` 换 no-op；额度恒无限，准入不查余额。此阶段不碰适配器。
2. **测试全绿**：金标准用例（§12.4）全过。
3. **物理清除**：删除计费/多用户代码与其 UI 页面；`relaycommon` 中被适配器引用的计费字段保留并注释"惰性遗留"。

#### 10.2.1 W7 执行口径（已定，按此实施）

**范围红线**：只裁 §1.3 的计费/支付与多用户/账号安全两类，**基座其余管理面一律保留**（渠道运维
`/api/channel/**`、模型元数据 `/api/models/**`、厂商 `/api/vendors/**`、部署 `/api/deployments/**`、
任务插件 `/api/plugin/task/**`、系统任务、性能、预填组、管理员日志 `/api/log`、`/api/option`、
静态内容页）。

**物理删除（连路由、handler、model/service/middleware/setting 文件与其测试）**

- 计费/支付面：`/api/pricing`、`/api/ratio_config`、`/api/ratio_sync/*`、
  `/api/option/model_pricing*`、`/api/option/rest_model_ratio`、`/api/option/waffo-pancake/*`、
  `/api/option/payment_compliance`、全部 `/api/user/topup|pay|amount|stripe|creem|waffo*|aff*`、
  `/api/subscription/**`、`/api/redemption/**`、`/api/data/**`、`/api/usage/token`、
  `/dashboard/billing/*` 与 `/v1/dashboard/billing/*`。
- 多用户/账号安全面：全部 `/api/user/**`（注册/登录/2FA/passkey/会话/OAuth 与邮箱绑定/签到/
  排行榜/用户 CRUD/分组）、`/api/oauth/**`、`/api/verify*`、`/api/verification`、
  `/api/reset_password`、`/api/user/reset`、`/api/custom-oauth-provider/**`、`/api/authz/catalog`、
  `/api/group`、`/api/log/self*`、`/api/audit/self`、`/api/mj/self`、`/api/task/self`、
  `/api/task/:id/artifacts`。
- 计费执行链：`BillingSettler` 的**实现**（预扣费/结算/退款/钱包/订阅/额度钳制/阶梯结算）与
  `PostConsumeQuota` 一类扣减路径；转发管道改为不预扣、不结算、不退费。
- 基座 `logs` 表的计费口径统计（`/api/log/stat`）与 `controller/billing.go`。

**保留（惰性遗留，必须带注释说明"W7 保留"）**

- `relay/common/relay_info.go` 中被适配器读写的计费字段（`Billing`、订阅快照、`QuotaClamp`、
  `PriceData`、`TieredBillingSnapshot` 等）与 `relay/common/billing.go` 的接口：删它们要改 40 家
  适配器，违反 §10.1"适配器原样复用"。
  > 实测修正（W7 勘察）：`relay/channel/**` 对 `RelayInfo` 计费字段的引用只有 **1 个文件 4 行**
  > （`relay/channel/ali/image.go:62,63,65` 用 `TieredBillingSnapshot` 与 `PriceData`）；§2.6
  > 的"会改动 40 家适配器"过于宽泛。但这些字段同时被 `relay/relay_task.go`、`relay/helper/price.go`
  > 与 `controller/relay.go` 的保留链路密集使用，W7 仍按"保留类型与接口、把实现降级为 no-op"处理。
- `setting/ratio_setting`：既提供路由用的 `RoutingMatchModelName`（模型名归一化），也仍是基座
  管道惰性遗留比例价的存储，不能删。
- `model/pbr_system.go` 的系统用户锚点：基座管道仍需要一个 userId 才能工作。

**改造（不是删除）**

- 保留的基座路由原先挂 `UserAuth`（如 `/api/models`、`/api/token/**`），改挂 PBR 原生凭据
  （管理密钥）；实现形态是 `middleware/pbr_auth.go` 的 `PBRAuth`——基座的 `AdminAuth`/`RootAuth`
  依赖用户会话与角色，W7 随多用户面一并删除。`POST /api/channel/:id/key` 去掉已永远无法满足的
  2FA/passkey 安全证明（PBR 无用户体系，该证明只会让路由不可用），只保留 `PBRAuth`。
- `service/authz` 的权限常量与 `middleware.RequirePermission` 保留——它们仍挂在保留的渠道/插件/
  审计路由上；删除的只是"角色-用户授权"体系与 `/api/authz/catalog`。
- 成本折算改由 PBR 单价表承担（§16.9#7），`estimated_cost` 不再从基座 quota 反算。

**验收**：`go build ./...`、`go vet ./...`、`go test ./...`、`cd web && pnpm build && pnpm lint`
全绿；§12.4 金标准全过；全波次 `verify/` 与 `verify/final/` 脚本全绿；控制台无计费/多用户入口。

### 10.3 从 upstream 取什么

| 能力 | 取法 |
|---|---|
| 六键 `relay_config` 语义 | 照搬 bestruirui 上游字段与默认值/Normalize |
| 成员冷却 + 亲和 | 移植语义（进程内 cooldowns + affinity） |
| failover 排序 | 移植语义 |
| 熔断三态 + 半开 | **仅算法参考**（同名分叉仓库），按 PBR 模型重写 |
| 健康快照 / 逐尝试日志 | 参考形状，落到 probe 与 `request_logs.attempts` |

### 10.4 工程骨架（待建）

```
powerbar-rations/
├── AGENTS.md                 # 协作规则（已建）
├── README.md                 # 部署/车道配置/超时算术/与旧系统差异
├── docs/
│   ├── design-v1.md          # 本文（架构与取舍）
│   ├── api-spec-v1.md        # 管理 API 契约 + AI 调用手册
│   └── ui-spec-v1.md         # 控制台规格
├── cmd/pbr/main.go           # 入口
├── internal/
│   ├── api/                  # 管理 API（/api）+ OpenAPI
│   ├── relay/                # 路由核心：lane 选择、冷却、熔断、转发编排
│   │   └── circuit/          # 熔断器（新增）
│   ├── channel/              # 适配器注册与调用（vendor 自 new-api relay/channel）
│   ├── model/                # Channel/Lane/LaneMember/ClientKey/RequestLog/AuditLog
│   ├── store/                # SQLite + migration
│   ├── log/                  # 元数据日志与聚合
│   └── migrate/              # 旧库迁移（后期）
├── pkg/                      # 可复用工具（vendor 自 relaykit/dto/common）
├── web/                      # 控制台（embed 进二进制）
├── testdata/                 # 金标准请求样本（脱敏）
└── reference/                # 上游只读参考（gitignored）
```

### 10.5 开工前置：以 new-api 为基座迁入（不是净室重写）

§2.6 的实测证明适配器与 `service` / `setting` / `model` 深度绑定，净室剥离不可行。因此开工第一步固定为：

1. **迁入**：把 `reference/new-api` 的 Go 源码复制进本仓库，**保留其包布局**（`relay/ relaykit/ dto/ common/ constant/ setting/ service/ model/ controller/ middleware/ router/ logger/ pkg/ i18n/`），改 module path 为 `pbr`、全量替换 import 路径。该步单独一个提交（`feat: 以 new-api 为基座迁入`）。
2. **不迁**：`electron/`、`docs/`、`e2e/` 等非代码资产（`web/` **要迁**，见 §2.8）。
3. **§10.4 的目标布局是演进终点，不是起点**：新写的路由核心放 `internal/`；旧包逐步改造或删除，不要求一次性重排目录。
4. **计费与多用户先惰性化**（§10.2），W7 才物理清除；中间阶段允许 `service`/`setting` 包继续存在。
5. **许可证**：保留 new-api 的 AGPL 头、`LICENSE`、`NOTICE`、`THIRD-PARTY-LICENSES.md`（前端既然只来自 new-api 一家上游，无需再另附其他蓝本清单）。品牌可替换，版权不可替换。

> 关键取舍：**迁入的代码在中间阶段会包含计费与多用户代码**（只是不注册路由、不执行计费）。这是为"能编译、能转发"付的必要代价；W7 再清。

---

## 11. 迁移机制（后期阶段，输入为私有台账）

> 迁移**不在当前阶段实施**。本节只定义机制与边界；任何具体渠道名/模型名/车道名单/凭据都不写入本仓库。

### 11.1 算法

现行路由层成员记录的是**发给厂商层的请求名**，可能再经厂商层映射改名。对每个成员：

1. 取路由层成员的请求名 `M`。
2. 在厂商层能力表中查 `model=M` 的行，按 `priority DESC, weight DESC` 选渠道 `C`。
3. 若 `C.model_mapping` 含 `M`，真名 = 映射右值；否则真名 = `M`。
4. 产出 PBR 成员 `(Channel=C, UpstreamModel=真名, Priority=原优先级)`。
5. 无法唯一定位 → 进对账报告"待人工裁决"，**不许静默丢弃**。

### 11.2 通用规则

- **幂等**：以 `name` 为键 upsert，可重复跑。
- **对账报告**：输出旧对象 → 新对象计数与未归属项。
- **并列优先级**：同一 `(model, priority)` 落在多个渠道时，最终选择依赖源系统内部排序，必须报人工裁决，不得脚本猜测。
- **凭据**：运维注入真实 key；agent 只留占位符，不读写任何凭据文件。
- **读旧库**：路由层库必须带 WAL 三件套（`-wal`/`-shm`）并 `PRAGMA wal_checkpoint(FULL)` 后再读；单拷主库会看到旧数据。

### 11.3 输入输出边界

- **输入**：运维私有台账（渠道清单、车道成员、六键覆盖值、凭据注入点位、单价表）。**不入仓库。**
- **输出**：PBR 配置（经 `POST /api/import` 落库）+ 对账报告。

---

## 12. 施工波次与验收

> 端口可配置，默认取现网空闲端口；施工期与现网两套实例并行，**绝不许停现网容器**。

### W0 立骨架（gate：能转发成功，不是能编译）
vendor new-api 转发管道与适配层；Go module `pbr`；先让它原样转发一发真实请求。
**验收**：`go build ./...` 通过；一条 curl 经 PBR 转发成功并回显上游响应。

### W1 单层化路由
建 Channel/Lane/成员模型与 migration；**模型名解析为路由键**（显式车道 → 隐式链）；响应 `model` 回填请求名；`X-Served-By`；最小管理 API（`GET/PUT /channels`、`GET/PUT /lanes`、`GET /models`、`GET /routes/{model}`）。
**验收**：
- **隐式**：两个渠道各声明同一模型名（不同 priority）→ 直接请求该模型名 → 走 P1 渠道；把 P1 的 key 改成坏值 → 自动落到 P2；响应 `model` 始终等于请求名。
- **显式**：建一条显式车道并加两个成员（不同上游模型名）→ 请求该名字 → 日志显示实际成员链。
- 两个渠道都不声明某模型名时，请求该名字返回 `503 No available channel for model <X>`。

### W2 容错
六键、冷却、亲和、failover；熔断三态（软硬分离、成员粒度）；关键词机制；weighted / round_robin。
**验收**：唯一成员指向必然 500 的假端点 → 熔断打开、错误快抛；修好端点 → 半开窗口内**自动复通**（时间戳证据）；probe 返回逐成员结果；429 不误判硬故障；四模式各跑通一条链路。

### W3 访问与 AI 管理面完备
api-spec 全部端点 + OpenAPI + 错误码 + 审计 + 导出/导入 + dry-run。
**验收**：每类资源"写→GET 回读"断言一致；`/openapi.json` 可被工具解析；`export→import(dry_run)` diff 为空；库内 grep 不到任何请求正文。

### W4 UI 控制台
ui-spec 全部页面；`pnpm build` 零报错；产物 embed 进二进制。
**验收**：ui-spec §6 的逐页验收点全过（含空态/错误态/加载态）；无计费与多用户残留入口。

### W5 日志与记账
`request_logs` + 聚合表 + `/logs` `/stats` + UI 图表。
**验收**：20 条混合请求（含失败与逃逸），日志行数=请求数、`attempts` 完整、`cache_read_tokens` 有非零、单条平均 < 2KB。

### W6 迁移（后期）
`pbr migrate`；§11 全部；对账报告。

### W7 减脂清除（测试门禁）
按 §10.2.1 的执行口径物理删除计费/多用户代码与其 UI 页面；基座其余管理面保留，其中
依赖用户会话的鉴权改挂 PBR 原生凭据。
**验收**：`go build ./...`、`go vet ./...`、`go test ./...`、`cd web && pnpm build && pnpm lint`
全绿；§12.4 金标准全过。

### W8 成熟度验收（成品门，最后一关）

> 最终交付要求是**可长期无人值守运行的成熟产品**，不是"能编译、能跑通"的 demo。W8 把前七波串起来做产品级验收，**A–J 任一项不过即整体未完成**。逐项证据落 `verify/final/`。

- **A 功能完整性**：A1 对照 `openapi.json` 逐端点实跑（含 dry-run、导入导出、审计），无 5xx、无未实现桩；A2 四模式+冷却+亲和+熔断半开在真实链路（假上游）复现并留时间戳；A3 控制台逐页走查（ui-spec §8），空/加载/错三态齐备，拖拽排序与 SSE 实时状态实测；A4 迁移脚本在旧库副本上跑两次幂等、对账数字正确。
- **B 端到端金标准**：§12.4 八项在接近生产形态下全过。
- **C 故障注入**：上游 500 / 429 / 超时 / 流中途断流 / 坏响应 / 欠费关键词 / 凭据失效，逐项验证错误分类、冷却、换人、快抛、半开自动复通。
- **D 长稳与并发**：连续 ≥2 小时或 ≥5 万请求混合流量，无内存/goroutine 泄漏、SQLite 体积无异常增长、聚合表正确；高并发下无串号与跨请求状态污染，探活并发上限生效。
- **E 持久化与重启**：重启后配置与令牌不丢；冷却/熔断按设计清空；日志连续不重复。
- **F 安全**：未初始化时白名单接口正确、其余拒绝；错误密钥 401、被拒车道 403；密钥明文不入库不入日志；OpenAPI 不泄漏敏感字段；弱口令有明确提示。
- **G 部署验收**：从零用 `docker-compose.yml` 起容器 → 首启设口令 → 建渠道/车道 → 跑通一发真实请求 → 重启容器数据仍在；镜像可重建，`docker run` 说明可照抄，卷挂载与备份步骤实测。
- **H 回滚演练**：按 `MIGRATION.md` 在测试实例上演练切流与回滚（不动生产）。
- **I 文档一致性**：README / MIGRATION / ADR / OpenAPI / `verify/` 与实现一致，无"文档说 A、实现是 B"的漂移。
- **J 代码质量**：`go vet ./...`、`go test ./...`、前端 lint 与 build 全绿；无未处理 TODO（有则列清单说明）；死代码已清。
- **成品门**：A–J 全过，且 `verify/final/` 给出证据索引（每项一行：检查项 → 命令 → 结论 → 证据路径）。

### 12.4 金标准用例（W1 起每节都跑，不许只做单测）
① 工具调用 5/5（一票否决）② 多模态小图（尺寸 ≥10，过小图会被部分厂商拒）③ 思考参数半开时**由上游**返回 400，网关原样透传 ④ 长流式 ≥100K 输入 token ⑤ Anthropic `/v1/messages` 一发 ⑥ `embeddings` ⑦ 车道全挂时错误快抛不静默 ⑧ 429 限流不被误判为硬故障。

---

## 13. 提交与分支纪律

见 `AGENTS.md`。落地要点：大任务先改设计文档（`docs:`）→ 生成 breakdown（`breakdown:`）→ 并行实现（`feat:`/`fix:`）→ 删除 breakdown（`chore:`）；每波次一个主题的原子 commit，message 写"做了什么 + 实测证据路径"；不许顺手改 vendor 进来的适配器逻辑；默认 `main`，功能分支 `--no-ff` 合并后清理；**提交前自查 diff 不得出现渠道名/模型名/车道名单/凭据/厂商地址/单价**。

---

## 14. 风险与已知坑

1. **适配器与计费的耦合**是最大工程风险（§2.6）。对策：三段式减脂。
2. **假成功前科**：路由层写接口曾回 success 但库不动；厂商层创建接口缺包装会静默假写。对策：写→回读→断言。
3. `model_mapping` 必须是 JSON dict（迁移只需读）。
4. 验证 `sk-` 令牌只能用 curl / node fetch；Python `urllib` 会被指纹过滤误判 401。
5. 读旧库必带 WAL 三件套 + `wal_checkpoint(FULL)`。
6. 欠费类上游以 HTTP 400 到达，不在默认重试状态码集，只有关键词命中才抓得到。
7. 自动禁用后无半开自愈（上游 issue #5420）——本项目要修的就是它。
8. 车道全挂必须"错误快抛、无静默兜底、不跨车道逃逸"。
9. 同渠道同时承载车道流量与点名流量时，整渠道级故障会让点名请求 100% 撞墙——故 `PublicAlias` 点名也必须过冷却/熔断。
10. **切流会打断外部告警链路**：现网告警依赖监听旧库文件事件，切流后需改指新库并验证出口存活；链路见运维私有笔记。
11. **AGPL-3.0**：上游均 AGPL。本系统仅本机自用（无公网入站、不分发），不触发开源义务；但**不许把仓库推到公开可见位置**。

---

## 15. 交付物

① 可运行二进制 `pbr`（含 embed 的控制台）+ `Dockerfile`（多阶段：前端构建 → Go 构建）。
② `README.md`：部署 / 车道配置 / 超时算术 / 管理密钥管理 / 与旧系统差异。
③ `MIGRATION.md`：§11 算法 + 切流与回滚（含告警链路改造）。
④ `docs/adr/`：至少 4 条——为什么以 new-api 转发管道为基座、为什么删 `model_mapping`、为什么默认开自动复通、为什么保留控制台但认证极简化。
⑤ `verify/`：每波次的命令与输出证据，含时间戳。
⑥ `GET /api/openapi.json`（机器可读契约）+ `GET /doc` / `GET /llms.txt`（面向 AI 的纯文本手册）+ `GET /doc/ui`（复用 Scalar 的交互式文档）。

### 切流（交付后由运维执行）
1. 下游逐个把 base_url 从旧路由层改指 PBR（模型名不变），每次改一个并跑一发真实会话验证。
2. 全量切完后旧两套实例停容器但保留数据作归档。
3. 回滚 = 把 base_url 指回旧路由层，一秒级。

---

## 16. 实现约定（消除歧义）

> 本节把 goal 模式最容易跑偏的实现细节固定下来。凡本节有约定，实现不得自行发挥。

### 16.1 param_override 与思考参数

- `param_override` 为 JSON 对象，在适配器构造完上游请求体之后、发送之前做**深度合并**：对象递归合并，标量覆盖，`null` 删除该键，数组整体替换。
- 合并发生在**协议转换之后**，即覆盖的是"最终发给厂商的体"，从而让同一份请求语义能驱动所有厂商。
- **思考参数**：沿用适配器现状（`setting/reasoning` 与 `model_setting.ShouldPreserveThinkingSuffix` 是现成实现，直接复用）。**网关不新增校验、不拦截**：`enable_thinking` 与 `reasoning_effort` 半开时由**上游**返回 400，网关原样透传；各厂商等价字段（如 `thinking:{type}`）的映射由**适配器**负责，PBR 不新增第二张映射表。金标准③测的是"上游会 400"，不是"网关要造一个 400"。
- 约定：渠道的 `param_override` 只放"该厂商差异项"；通用项放全局默认，避免逐渠道重复。

### 16.2 测试用假上游（failover / 熔断验收的基础设施）

- 仓库内置 `internal/testutil/fakeupstream`：可编程 HTTP server，支持按路径/模型名返回预设响应、可控延迟、可控 429/500、流式首包延迟与中途断流。
- W1/W2 的验收（"唯一成员指向必然 500 的假端点""半开窗口内自动复通""429 不被误判硬故障"）**一律用该 fixture，不打真实厂商**。
- fixture 需记录收到的请求体，用于断言 param_override 与思考参数的合并结果。

### 16.3 OpenAPI 生成

- **以代码为源**：在路由注册处维护端点表，`GET /api/openapi.json` 返回运行时结果。当前实现是 `internal/api/config_lifecycle.go` 的手写 `openAPIPaths()` 表（尚未改为结构体标签自动生成），靠下述守卫测试把"漏登记"变成构建期失败，效果等价于验收断言。
- 验收断言：`openapi.json` 可被标准工具解析，且**所有已注册路由都出现在文档中**。落地为 `router/openapi_coverage_test.go` 的 `TestOpenAPICoversEveryRegisteredRoute`：对比 `engine.Routes()` 与端点表，双向校验（既不漏档、也不登记不存在的路由）。
- 若将来改为标签生成，保留该守卫测试即可；漂移口径不变。
- **给人/AI 的入口**：`GET /doc`（默认 `text/markdown`，浏览器 `Accept: text/html` 返回说明页）、`GET /llms.txt`（`text/plain`）、`GET /doc/ui`（复用 GitHub 项目 Scalar 渲染 `/api/openapi.json`）。三者与 `/api/openapi.json` 均免鉴权，便于 AI 先读手册再自行派生管理密钥。
- **前缀**：管理面规范前缀为 `/api`，`/api/v1` 为兼容别名（注册相同处理器）。与 AI 契约冲突的控制台内部资源（模型目录、审计）收在 `/api/console/*`；其余控制台内部接口仍在 `/api/*` 下同权限可用，但不属于稳定契约。

### 16.4 存储与迁移版本

- 带版本的 migration 列表（`internal/store/migrations/NNN_*.go`），启动时按序执行并记录到 `schema_migrations` 表。
- 迁移必须幂等、可在事务内执行；失败即启动失败，不允许半可用状态。

### 16.5 日志保留

- 不引入 cron。保留策略由 `system/options.log_retention_days` 配置（默认 30），并提供 `POST /api/logs/prune?before=<ts>&dry_run=` 由外部按需触发。
- 聚合表长期保留（体积小）；明细表按上述策略清理。

### 16.6 并发与超时

- 每次尝试的超时由车道六键与成员级覆盖决定；流式用"首事件超时"与"整请求超时"双闸控制。
- 探活并发上限可配（默认 4），避免对上游造成突发压力。

### 16.7 管理密钥的数量语义

- 支持配置多把管理密钥（轮换过渡用），任一把均可全量操作，不区分权限；由 `PBR_ADMIN_KEY`/`PBR_ADMIN_KEYS` 提供，**不入库**。两变量同时存在时合并，任一匹配即通过（实现见 `middleware/pbr_auth.go`）。
- 口令恢复的另一条途径 `pbr auth reset --db <pbr.db> --yes` 清库内凭据回到未初始化（实现见 `internal/authutil/cli.go`）。

### 16.8 决策记录（原"待确认"项已落实）

| 项 | 决定 |
|---|---|
| 控制台登录凭据 | **登录口令**（无账号）；首启设置口令，管理密钥由口令派生（token-spec §2） |
| 图像/视频/任务页面 | **暴露**，不隐藏 |
| electron 桌面端 | **不考虑**；仅 Docker 部署 |
| 思考参数的厂商等价字段 | **沿用适配器现状**，不重新梳理（见 §16.1） |
| UI 蓝本 | **new-api 上游前端整体搬迁**，删多用户/计费页面（ui-spec-v1.md） |
| 品牌 | 全量替换为 PowerBarRations（§17） |

实现细节已定，见 §16.9。

### 16.9 实现细节（已定，按此执行）

以下均为实现细节，已直接定案，不再逐项征询；如对某条有异见再单独提出。

| # | 项 | 决定 |
|---|---|---|
| 1 | SSE 鉴权 | 前端用 `fetch` + `ReadableStream` 读 SSE，带 `Authorization` 头；**不用 `EventSource`**（无法带头），密钥不进 URL |
| 2 | 图像/视频/任务路由 | **与 chat 同一套**：`model` 按 §1.1 解析到成员链，选成员走同一路由核心；任务适配器仍自行负责 action/轮询/结果解析，只是"打给谁"由路由核心决定 |
| 3 | 车道粘滞 | 车道级共享当前成员（照搬线上）；隐式车道即"按模型名各自粘滞"，模型之间互不影响 |
| 4 | Docker | 镜像/容器名 `pbr`；数据卷挂 `/data`（含 `pbr.db`）；随仓库提供 `docker-compose.yml` 样例 |
| 5 | Playground | **保留**（控制台内，排障用） |
| 6 | `round_robin` 游标 | 每车道一个全局游标（与车道级粘滞一致） |
| 7 | 上游单价与成本折算 | **两层单价**：①**渠道级上游单价**（渠道 `setting.pbr_prices`，人民币 / 百万 token，字段 `input`/`output`/`cache_read`/`cache_write`），在渠道编辑页配置；②**全局默认单价表**（`options` 表的 `PBRModelPrices`），在"系统设置 → 模型 → 单价表"配置。折算优先级：渠道价 > 全局默认 > 不折算。只用于日志 `estimated_cost` 与看板成本统计，**不参与准入、不扣额度**。基座 `setting/ratio_setting` 不再充当单价表（它仍是惰性遗留：提供路由用的模型名归一化 `RoutingMatchModelName`） |
| 8 | 旧库日志 | **不迁移**；旧库整体归档保留，不额外导出 |
| 9 | 请求头兼容 | 管理面仅收 `Authorization`；模型面 `Authorization` 与 `X-Api-Key` 都收（兼容存量客户端） |
| 10 | 渠道模型清单来源 | 手工录入 + 可选"从上游拉取"（`POST /channels/{name}/sync-models`，即原蓝本的模型同步，收敛为渠道上的一个动作）。**保护性约束**：上游返回空清单默认拒绝清空（`?force=1` 覆盖）；要移除的模型仍被显式车道成员引用时返回 409（同样 `?force=1` 覆盖），避免一次上游抖动摘掉在用成员 |
| 11 | 开工基座 | **以 new-api 源码迁入为基座**，非净室重写；前端**同样直接搬迁上游 `web/`**（见 §2.8 / §10.5） |
| 12 | 亲和默认值 | 默认 `member_affinity_seconds=0`（**与现网一致，避免故障切换后长时间粘在备用成员**），可配 |
| 13 | 前端包管理与适配范围 | 前端直接搬迁 new-api 上游 `web/`（Rsbuild + Bun 锁文件；环境不便时可用 pnpm）；保留除多用户/计费外全部页面；唯一实质改造 = **模型管理页内联成员链（故障切换）**，并把认证接到 PBR 口令会话（见 §7.7） |

---

## 17. 品牌与命名

- **产品名统一为 PowerBarRations**，全量替换为自有品牌：界面标题、logo/favicon、i18n 文案、`/version` 与 OpenAPI `info.title`、错误页、空态文案。
- 工程标识：二进制 `pbr`、Go module `pbr`、Docker 镜像/容器名 `pbr`、环境变量前缀 `PBR_`、本地存储键前缀 `pbr_`、日志前缀 `[pbr]`。
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
| 3 | 旧端点兼容 | **一律不做兼容层**：旧管理端点全部翻新重构，切流后重写 ops 技能与告警脚本 |
| 4 | 仓库远程 | **只留本地**，不配置 remote |

### 非阻塞事项（切流阶段才需要，不挡开工）
- 下游 base_url 的切换顺序与时间；旧两套实例停容器的时间。
- 成本折算单价表的实际数值（属部署数据，运行期经 `/system/options` 注入）。
- 旧库归档保留时长。
- 切流策略可选**分步走**：PBR 先只接管路由层、渠道暂指旧厂商层，验证后再把渠道直连厂商（比一次性替换风险低）。默认按"一次性替换"设计，若要分步需在切流前告知。

