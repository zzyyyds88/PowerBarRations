# design-v1 历史归档

> **2026-09 立项与施工期历史叙事，已从 design-v1 归档，现行规范见 [`design-v1.md`](../design-v1.md) 与各配套 spec。**
> 本文件只增不减，仅供追溯"当时为什么这么做"；其中与现行规范冲突的表述（如隐式链、加权模式）一律以现行文档为准。

| 归档章节 | 原位（design-v1） | 说明 |
|---|---|---|
| §0 相对 brief v1 的改动摘要 | §0 | 立项期决策过程；其现行有效结论已并入 §1/§5/§9 与各 spec |
| §2 现状结构基线 | §2（§2.1–§2.8） | 旧两层网关的勘察记录；§2.8 三行现行前端约束已并入 design-v1 §6 |
| §10.2.1 W7 执行口径 | §10.2.1 | 已执行完毕，证据见 `verify/`（各波次与 `verify/final/`） |
| §10.4 工程骨架（待建） | §10.4 | 立项期目标布局；实际目录以仓库为准 |
| §11 迁移机制 | §11 | 现行机制唯一规范见 [`MIGRATION.md`](../../MIGRATION.md) |
| §12 施工波次 W0–W8 | §12 | 施工已完成的波次定义与验收门；金标准八项仍留在 design-v1 §12 |

---

## 0. 相对 brief v1 的改动摘要（归档）

| # | brief v1 的做法 | 本文改法 | 为什么 |
|---|---|---|---|
| 1 | 保留令牌表语义，车道用 `lane_allowlist` 限流，白名单"二选一"留悬念 | **取消"令牌网关"复杂度**：管理面只认一把静态管理密钥；客户端密钥**默认允许全部车道、可显式拒绝**，永不出现"忘了加白名单 → 故障那一刻 400" | 用户判定令牌网关设计过度复杂；白名单是现网两代网关的反复踩坑源 |
| 2 | AI 运维需拷库改 options、抓 SPA 反汇编接口 | **管理面按"AI 可调用"重设计**，并给出完整契约与示例（api-spec-v1.md） | 用户要求"开放 API 给 AI 调用，不再改文件" |
| 3 | 逐文件手删 100+ 计费/前端文件 | **三段式减脂**：先逻辑停用（不注册路由、计费惰性化）→ 测试全绿 → 最后物理清除 | 适配器接口 `Adaptor` 每个方法吃 `*RelayInfo`，而它内嵌计费字段；硬删会逼停适配器（§2.6） |
| 4 | 以同名分叉仓库为移植源 | **更正上游**：线上路由层真实上游是 `bestruirui/octopus`；六键/冷却/亲和以它为准；熔断器等属**新增** | 镜像 label 记录了 source/revision；分叉仓库 schema 已分叉（§2.7） |
| 5 | 车道模式来源存疑 | 实现 **failover（默认）/ manual 两种**；`weighted` / `round_robin` 属新增后**已删除**（单用户自用网关不需要随机/轮询负载均衡，见 §7.2） | 用户确认：车道按顺序故障切换即可 |
| 6 | 未明确 UI 去留 | **保留 UI 控制台**，以线上路由层前端为蓝本迁移功能原理（个人自用仍要能人看） | 应用户纠正：只砍计费，不砍功能；并指定 UI 蓝本 |
| 6b | 控制台以**线上路由层前端**（octopus）为蓝本 | **改为以 new-api 前端为蓝本整体搬迁**（`web/` 直接取自上游源码），只删多用户/计费页面 | 应用户二次纠正：new-api 的渠道管理/模型管理与其后端接口天然配套（PBR 已保留基座管理路由，路径一致；实注册数量以 `router/` 与 `/api/openapi.json` 为准），而 octopus 蓝本需大量改造；直接在 new-api 前端上做减法风险与工作量都更低 |
| 7 | 未明确"开放 API 如何被调用" | 新增 api-spec 的 **AI 调用手册**：发现、认证、典型工作流 curl 示例 | 应用户要求写实 |
| 8 | 车道是"必须先创建的对象" | **路由键 = 模型名**：渠道声明 `Models` 即自动成链，任意模型零配置可路由；显式车道降级为可选覆盖层 | 用户澄清的目标流程：下游请求模型1 → 上游1的模型1 → 上游2的模型1，且"这个模型可以是所有模型" |

> 注：#8 的"隐式链自动成链"已被 [ADR 0005](../adr/0005-lane-required-and-channel-model-mapping.md) 推翻——现行语义是"必须有同名显式车道才可调用"。

---

## 2. 现状结构基线（不含部署数据）（归档）

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
| `logger` / `model` / `pkg/billingexpr` | 23 / 4 / 2 | 日志与任务类型（任务插件子系统已移除） |

同时 `relay/common/relay_info.go` 的 `RelayInfo` 内嵌 `BillingSettler`、订阅、预扣费、配额钳制、阶梯计费快照等字段。

**结论**：既不能把计费类型从 `RelayInfo` 里摘掉（会改动 40 家适配器），也不能把适配器从 `service`/`setting` 里剥出来（要重写传输辅助、思考后缀解析、任务基础设施、单价表）。→ **不做净室 vendor，改以 new-api 源码为基座迁入**（§10.5）；计费只做惰性化，不清空 `service`/`setting` 包。

### 2.7 上游 provenance 更正

- 线上路由层镜像的 label 记录了 `source = github.com/bestruirui/octopus` 与具体 revision。**它才是线上行为的定义源**：六键、成员冷却、亲和、failover 语义均以它为准。
- 调研中同时出现的同名分叉仓库（Hureru/octopus）schema 已分叉：`Group.Mode` 改为整数、引入 `GroupPreset`、**不含六键**。其熔断器/离群窗口/健康快照/逐尝试日志是它独有的。
- 含义：**车道成员语义移植自 bestruirui；熔断器等属 PBR 新增能力，可参考分叉仓库的三态算法按本项目模型重写。**

### 2.8 前端结构（**直接搬迁 new-api 前端**）

控制台**整体取自 new-api 上游 `web/`**（Rsbuild + React + TanStack Router + Base UI + Tailwind），在其上做减法与接线改造：删多用户/计费页面、把认证换成 PBR 口令会话、把"模型管理"接入 PBR 的成员链（故障切换）。

**为什么直接搬**：PBR 已保留 new-api 的管理面后端（渠道 `/api/channel/**`、模型元数据 `/api/models/**`、系统任务、性能、日志、`/api/option`、系统信息等基座管理路由——路径与前端调用**一一对应**，实注册清单以 `router/` 与 `/api/openapi.json` 为准）；而删除的多用户/计费接口，恰好是本项目明确不要的部分。反过来，octopus 蓝本只有少量页面与 PBR 对应，其余都要重写。因此**在前端源码上做减法**比"以另一上游为蓝本重做"风险与工作量都更低，也更符合用户"其他功能全都要"的要求。

- **构建**：Rsbuild（上游默认），产物交 Go `embed`；包管理沿用上游 `bun.lock`（如环境不便可用 pnpm）。
- **保留**：渠道、模型、令牌、日志、仪表盘（数据看板）、试打台、系统设置、系统信息、性能指标、关于/法律页等**除多用户/计费外全部**。
- **删除**：用户/注册/登录（换 PBR 认证）、钱包、充值、订阅、兑换码、排名、定价同步、签到、个人中心、2FA/passkey 等。
- **改造点**（唯一实质改造）：**模型管理页内嵌成员链（故障切换）**——见 §7.7。

> 注：改造点后来落位为**侧边栏独立页 `/routes`**（模型页行内入口已移除），现行口径见 design-v1 §7.7 与 ui-spec §6.3。

---

## 10.2.1 W7 执行口径（归档；已执行完毕，证据见 `verify/`）

**范围红线**：只裁 §1.3 的计费/支付与多用户/账号安全两类，**基座其余管理面一律保留**（渠道运维
`/api/channel/**`、模型元数据 `/api/models/**`、
系统任务、性能、预填组、管理员日志 `/api/log`、`/api/option`、
静态内容页；**后续增补**：厂商 `/api/vendors/**` 与 io.net 部署 `/api/deployments/**` 已按
"本项目不需要"物理删除，见 §16 的收敛记录）。

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
- `service/authz` 的权限常量与 `middleware.RequirePermission` 保留——它们仍挂在保留的渠道/审计路由上；删除的只是"角色-用户授权"体系与 `/api/authz/catalog`。
- 成本折算改由 PBR 单价表承担（§16.9#7），`estimated_cost` 不再从基座 quota 反算。

**验收**：`go build ./...`、`go vet ./...`、`go test ./...`、`cd web && pnpm build && pnpm lint`
全绿；§12.4 金标准全过；全波次 `verify/` 与 `verify/final/` 脚本全绿；控制台无计费/多用户入口。

---

## 10.4 工程骨架（待建）（归档）

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

---

## 11. 迁移机制（归档；现行机制唯一规范见 [`MIGRATION.md`](../../MIGRATION.md)）

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

> 注：隐式链相关表述已被 ADR 0005 推翻，现行迁移算法是"旧 group 一律建显式车道"，见 `MIGRATION.md` §1。

---

## 12. 施工波次 W0–W8（归档）

> 端口可配置，默认取空闲端口；施工期在独立实例上验证。

### W0 立骨架（gate：能转发成功，不是能编译）
vendor new-api 转发管道与适配层；Go module `pbr`；先让它原样转发一发真实请求。
**验收**：`go build ./...` 通过；一条 curl 经 PBR 转发成功并回显上游响应。

### W1 单层化路由
建 Channel/Lane/成员模型与 migration；**模型名解析为路由键**（仅同名启用车道，见 ADR 0005）；响应 `model` 回填请求名；`X-Served-By`；最小管理 API（`GET/PUT /channels`、`GET/PUT /lanes`、`GET /models`、`GET /routes/{model}`、`POST /lanes/seed`）。
**验收**：
- **零配置不再可路由**：两个渠道各声明同一模型名（不同 priority）但没建车道 → 请求该模型名 → `503`；`POST /lanes/seed` 固化后 → 走 P1 渠道；把 P1 的 key 改成坏值 → 自动落到 P2；响应 `model` 始终等于请求名。
- **显式**：建一条车道并加两个成员（不同上游模型名）→ 请求该名字 → 日志显示实际成员链。
- 渠道不声明、也没有同名车道的模型名，请求返回 `503 No available channel for model <X>`。

### W2 容错
六键、冷却、亲和、failover；熔断三态（软硬分离、成员粒度）；关键词机制；manual 模式。
**验收**：唯一成员指向必然 500 的假端点 → 熔断打开、错误快抛；修好端点 → 半开窗口内**自动复通**（时间戳证据）；probe 返回逐成员结果；429 不误判硬故障；failover 与 manual 各跑通一条链路。

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
