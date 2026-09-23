# PowerBarRations 协作规则（AGENTS.md）

> 本文约束 agent 在本仓库的工作方式，与 [`CONTRIBUTING.md`](CONTRIBUTING.md)（面向外部贡献者）同级有效。
> 本文由通用模板改写而来，模板中的适配占位符已按本项目实际情况就地填实（见下表），不再保留未替换的占位符。
> 任何时候使用中文，除非用户明确要求英文输出。

## 适配标识（本项目取值）

| 标识 | 本项目取值 |
|---|---|
| 项目名 | **PowerBarRations（PBR）**——个人自用、单用户的通用 LLM 聚合与运维网关 |
| 真源入口 | [`docs/design-v1.md`](docs/design-v1.md) |
| 技术栈 | Go 1.25+（Gin + GORM + 纯 Go SQLite）· React 19 + TypeScript + Rsbuild · pnpm 11 |
| 唯一 owner | `internal/route`（选路运行态）· `internal/api` + `internal/apiresp`（管理面契约）· `model/`（持久化与 schema）· `relay/` + `relaykit/`（转发管道与厂商适配） |
| 产品边界 | 单二进制 + 单 SQLite 的**单用户单节点**网关；**无计费、无多用户、无自研 TLS、无多节点、无旧系统迁移** |
| 验收命令 | 见本文「验收命令」一节，闸口矩阵见 `docs/test-spec-v1.md` §8 |

## 设计文档与真源

- 本项目采用「**设计文档是唯一事实来源**」的模式。
- [`docs/design-v1.md`](docs/design-v1.md) 是当前有效的产品与技术设计基线（SSOT）。以下规范性配套文件**与基线同级有效**：

  | 文件 | 管什么 |
  |---|---|
  | [`docs/api-spec-v1.md`](docs/api-spec-v1.md) | 管理 API 完整契约 + AI 调用手册 |
  | [`docs/routing-spec-v1.md`](docs/routing-spec-v1.md) | 路由、冷却、亲和、熔断与故障转移 |
  | [`docs/token-spec-v1.md`](docs/token-spec-v1.md) | 令牌与认证（登录口令 / 管理密钥 / 客户端密钥） |
  | [`docs/ui-spec-v1.md`](docs/ui-spec-v1.md) | 控制台逐页规格与模块保留/删除清单 |
  | [`docs/hermes-spec-v1.md`](docs/hermes-spec-v1.md) | Hermes 适配与首要验收对象 |
  | [`docs/test-spec-v1.md`](docs/test-spec-v1.md) | 测试分层、方法与闸口（**验收的规范源**） |

- 冲突时：`design-v1.md` 管架构与取舍，各 spec 管本领域细节契约。
- 需要用户做出抉择时，使用 `AskUserQuestion` 工具询问用户，不要用正文里的问句代替。
- 代码、测试、配置与设计文档冲突时，以当前设计文档为准；发现冲突后应立即修正（无法当场裁决时先报告，见「必须停止并询问的情况」）。
- 历史实施记录（`docs/archive/`、`verify/**/README.md` 的历史结论、一次性交接文档）只用于追溯，不得覆盖当前设计。
- `reference/` 是上游只读参考（不入仓库），仅供调研，不属于本项目规范，也不作为实现依据。
- 真源优先级：
  1. 当前设计文档 `docs/design-v1.md` 及其同级规范文件。
  2. 当前源码、测试、脚本、生成契约（`/api/openapi.json`）、schema、运行日志、实机/用户侧证据、当前 git 状态。
  3. 本仓库 agent 宪法文件：`AGENTS.md`（根）与 [`web/AGENTS.md`](web/AGENTS.md)（前端细则）。
  4. 当前 active 的 ADR（`docs/adr/`）、`CHANGELOG.md`、`README.md`、`CONTRIBUTING.md`、`SECURITY.md`。
  5. 用户仍确认有效的 issue / review comments / commit history。
  6. `docs/archive/`、旧会话、旧记忆只能作为模式证据，不能覆盖当前项目事实。
- 如果真源缺失、互相矛盾或技术上明显错误，先报告冲突、证据和推荐处理方式，再等用户确认。

## 核心原则

- 架构优先、设计优先、真源优先、严格验收。
- 开始编码前必须先想清楚：为什么要这样做、这样做是否正确、是否存在更简单且更符合当前系统的做法。
- agent 必须敢于指出不合理的需求、错误的架构方向、冲突的规则和缺失的验收条件，帮助用户完善系统，而不是机械执行。
- 禁止补丁式开发：不得用散落 `if`、临时兼容分支、硬编码、`sleep`、全局 flag、prompt 文案、mock 或假数据绕过架构问题。
- 禁止无意义向后、向下兼容：旧字段、旧接口、旧产品残影若污染当前合同，必须先向用户说明取舍，不得擅自保留双路径。本项目**明确不做**旧 `new-api` / `octopus` 数据库迁移、双写、回填或兼容入口（design §11、§16.4）。
- 禁止选项剧场：用户要求架构或方案时，给出一个推荐主线；备选方案只用于解释为什么不选。
- 禁止偷懒、糊弄和留尾巴。完成定义必须包含可验证结果、未闭合风险和下一步边界。
- 用户明确否定的概念必须从代码、文档、计划和命名中删除，禁止换个名字继续保留（计费/额度、多用户与账号体系、`weighted`/`round_robin`、多节点实例视图等均已物理删除，见 design §1.3）。
- 当用户说「先看」「不动代码」「先给判断」时，必须保持只读审查，直到用户明确要求执行。
- **产品边界**：PBR 是**个人纯自用、单用户、单节点 SQLite** 的 LLM 聚合网关，一个二进制同时服务模型面 `/v1/*`、管理面 `/api/*` 与控制台静态资源（embed）。实现不得把项目带回已否定方向——不得引入计费/额度/扣费、账号体系与多用户、多节点实例视图、旧系统数据迁移、自研门禁或 TLS 终结、第二层网关。需要收紧暴露时用 `PBR_BIND=127.0.0.1`，而不是自研鉴权层。
- 最小改动不是半截改动。修 bug 不顺手重构、加功能不扩散清理，但本轮触达的调用链必须闭合到 owner、测试和文档。

## 功能变更流程

按任务规模分级执行，避免小任务套用大流程。

### 任务规模分级

| 级别 | 标准 | 例子 |
|------|------|------|
| 大任务 | 跨模块、架构变更、新增子系统、改状态机、改公共契约 | 改车道/成员模型、改路由故障转移语义、新增对外契约、改管理面信封 |
| 中任务 | 单模块改动、2-4 文件、不改变跨模块契约 | 修某控制台页面、调整配置项、单模块功能增强 |
| 小任务 | 单文件、bug 修复、文案/常量/配色调整、不改行为契约 | 改文案、修崩溃、调常量、修 lint、格式整理 |

### 各级别流程

> PS：派子代理的目的是快速完成任务和减少上下文占用。

- **大任务**：①先改设计文档，单独提交 `docs:` → ②生成任务拆分文档，`breakdown:` → ③并行实现代码，`feat:` / `fix:` → ④删除拆分文档，`chore:`。
- **中任务**：①先改设计文档 → ②实现代码，docs + code 可同提交，`feat:` / `fix:`，免拆分文档。
- **小任务**：①直接改代码，`fix:` / `chore:`，无需改设计文档，除非改了行为契约。

### 通用规则

- 修 bug 是「让实现符合设计」，不是「改设计」。小任务 bug 修复无需先改设计文档。
- 纯文案错别字、格式、不影响行为的注释，可不升级设计版本；若改变需求解释或程序行为，仍按功能变更处理。
- 确认变更影响到的设计文档、配置 schema、代码、CLI 和测试（`docs/test-spec-v1.md` §8 的复跑触发矩阵就是这张影响表）。
- 同一功能的设计文档、代码和测试原则上放在同一个 Git 提交中，避免只改代码不改设计。大任务因拆分文档流程除外。
- 运行受影响测试；不能运行的测试必须在交付说明中明确原因和风险。
- 任务拆解后尽可能并行分派子代理执行，审查其产出，不合格则打回重做，直至全部通过。
- 编写代码前先检查工作区是否干净；不干净先提交再写。
- 报错优先网络搜索，禁止瞎猜；修改一次仍失败则转搜索。

## 必需工作流

对立项、接管、功能、重构、修 bug、发布和文档任务，默认遵循：

```
当前真源审计
→ 推理闸
→ 唯一 owner 与合同设计
→ 测试 / fixture / 门禁计划
→ 核心实现
→ 薄 adapter / UI / CLI / API 接线
→ 针对性验收
→ 文档回写
→ git 边界复核
```

- 空项目启动时，禁止直接写代码或选择框架；必须先完成产品定义、唯一主线、第一闭环、技术栈确认和验收标准。
- 半路接管项目时，禁止把已有项目当空项目重写；必须先做只读接管审计，确认已有代码、入口、调用链、接口契约、生成规则、测试脚本、配置、部署习惯、未提交变更和文档线索。

## 推理闸

编码前至少回答以下问题：

1. 实际要解决的问题是什么？
2. 谁创建这个概念，谁调用它，谁消费它？（例：车道由控制台或管理 API 创建，由 `internal/route` 消费，由 `request_logs` 与看板呈现）
3. 当前真源在哪里？是否已经有同职责模块、合同、schema、service、状态机或文档？（先查 `docs/` 六份规范 + `docs/adr/`，再查 `internal/` 与 `model/`）
4. 唯一 owner 是哪一层或哪一个模块？哪些层禁止成为 owner？（选路语义只在 `internal/route`；管理面信封只在 `internal/api/ops_routes.go` + `internal/apiresp`；前端不得私造业务真相）
5. 是否跨 UI / 管理 API / 模型面 / CLI 等多个入口？如果跨入口，语义必须先进入共享合同或核心层（如 `model/` 的持久化模型、`internal/route` 的运行态），再各自接线。
6. 更简单、更保守的设计是什么？为什么不够？
7. 最大回归风险是什么？用什么测试、日志、命令、截图、实机证据或用户侧证据阻断？（对照 `docs/test-spec-v1.md` §8 选闸口）

推理闸没有闭合，不开始实现。

## 设计规则

- 共享语义必须单一真源：跨入口复用的概念进入 **`internal/`**（新写的路由核心与鉴权）、**`model/`**（持久化模型与 schema）或 **`relay/` + `relaykit/`**（转发管道），不要散落在 `controller/`、`router/`、`web/` 或脚本里。
- `controller/`、`router/`、`middleware/`、`web/` 只做协议映射、展示和接线，不拥有核心业务语义。**路由键 = 模型名、车道是唯一路由入口**（ADR 0005）：没有同名车道一律 `503 No available channel for model <X>`，不得在适配层、handler 或前端补「隐式成链」。
- 每个阶段必须有目的、收益、边界、验收命令和停止条件。
- 不为了速度删除对账、身份、权限、审计、验证、实机证据、用户可见状态或数据完整性字段（`request_logs.attempts`、`audit_logs`、dry-run 声明都是契约，不是装饰）。
- 已有系统风格、组件、目录、命名、状态管理和错误处理方式时，优先复用；只有在现有模式错误或不足时才提出替换方案。
- 抽象只在能减少真实复杂度、定义必要边界或匹配已有模式时引入。禁止为了「看起来架构化」过度封装。
- 如果修复会牺牲 UX、可见性、数据完整性、安全性或产品边界，必须先讨论取舍。
- UI、renderer、页面和协议层只能消费 owner 提供的结构化状态、文案、projection、命令和错误原因；禁止私造业务真相、状态机、路由结论或用户语义。
- 生成物只读不手改。`web/dist/**`、`web/src/routeTree.gen.ts`、`/api/openapi.json` 都是生成物：必须找到源文件与生成命令（`pnpm build`、TanStack Router 插件、`internal/api/config_lifecycle.go` 的端点表），改源后重新生成。
- 所有外部输入、队列、缓存、请求体、响应体、会话窗口、重试、超时和后台 worker 都应有上界、背压、释放或降级策略；禁止无界增长和永久常驻的隐性运行面（design §16.6）。

## 错误分级

审查、实现和验收时必须把问题分级：

- **阻断问题**：破坏核心功能、owner 边界、安全/隐私、数据真相、构建测试、用户关键体验或当前目标链路；当轮必须收掉。
- **设计风险**：不一定立即破坏，但会导致架构漂移、运行面失控或后续难维护；必须说明取舍和验收入口。
- **可记录债务**：不影响本轮目标链路，可暂缓；必须说明不处理原因和后续入口。
- **无关优化**：不进入本轮范围，禁止借机扩大改造。

禁止用情绪强度替代工程优先级。没有证据的担忧不能写成阻断；有证据的阻断也不能降级成「后面再说」。

## 适配块

以下适配块仅在本项目实际使用对应技术时生效；它们**低于**本文件通用条款，**高于**普通建议。

### ADAPTER:language-runtime/go

applies_when: "改动任何 `.go` 文件"
authority: "低于通用宪法，高于普通建议"
verification: "`go build ./... && go vet ./... && go test ./... -count=1`"

- 主模块 `github.com/zzyyyds88/PowerBarRations`（`go.mod` 声明 `go 1.25.1`）；`relaykit/` 是**独立 Go module**，经 `require` + `replace … => ./relaykit` 引用，**改它要单独跑 `cd relaykit && go test ./... -count=1`**。
- 生产路径必须用项目统一错误类型、结构化错误、`context` 或等价机制返回失败；禁止用 `panic`、裸异常、固定字符串或静默吞错处理可恢复问题。
- 命名与标识遵循 design §17：二进制 `pbr`、日志前缀 `[pbr]`、环境变量前缀 `PBR_`、本地存储键前缀 `pbr_`、数据库文件 `pbr.db`。
- 上游适配器（`relay/channel/**`、`relaykit/**`）是**移植自 new-api 的 AGPL 代码**：保留溯源注释与 AGPL 版权头，不要顺手重构、不要改格式；确需改动时在提交信息里说明理由。
- `web/dist/index.html` 是 tracked 构建产物且被 `main.go` 的 `go:embed web/dist` 内嵌：**并发跑 `go build` 与前端构建会命中 `pattern web/dist/index.html: no matching files found` 假失败**（test-spec §8.1），必须串行。

### ADAPTER:database/sqlite-gorm

applies_when: "改动 `model/`、schema、迁移或查询"
authority: "低于通用宪法，高于普通建议"
verification: "`go test ./... -count=1`；涉及 schema 时另跑相关 `verify/w*` 脚本"

- **唯一支持的部署数据库是 SQLite**（`github.com/glebarez/sqlite`，纯 Go，无需 CGO）。MySQL / PostgreSQL / ClickHouse 驱动保留在 `go.mod`，但**不在部署矩阵内**，不得作为新功能的前提。
- 不写旧系统迁移、双写、回填或兼容兜底；破坏性 schema 变化允许重建开发库（design §11、§16.4）。`model/*_migration.go` 形式的存量迁移只服务 PBR 自身版本演进。
- 写路径必须遵守「**写后回读**」：handler 内 re-read 再返回，响应体即落库后的最终状态。
- 统计读取走聚合表（`pbr_stats_hourly`），不要直读明细表（design §16.5）。
- 测试用独立 SQLite 文件或内存库，禁止指向真实数据卷。

### ADAPTER:framework/react-rsbuild

applies_when: "改动 `web/**`"
authority: "低于通用宪法，高于普通建议；细则以 `web/AGENTS.md` 为准"
verification: "`cd web && pnpm typecheck && pnpm lint && pnpm test && pnpm format:check && pnpm copyright:check`"

- **[`web/AGENTS.md`](web/AGENTS.md) 是前端细则的真源**（组件复用强制检索、i18n、错误处理、测试、依赖与构建），改前端前先读它；本节只列不可违反的硬约束。
- 栈：React 19 + TypeScript + Rsbuild + TanStack Router/Query/Table + Base UI + Tailwind；包管理 **pnpm 11**（`packageManager` 锁定，跟踪 `pnpm-lock.yaml`）。
- **全仓 `pnpm lint` 必须 0 error**；`.oxlintrc.json` 中对 `react/incompatible-library` / `react/set-state-in-effect` / `react/refs` 的降级是**有理由的既有决定**，改动它们需同步更新 `web/AGENTS.md`。
- 面向用户的文案一律走 i18n（zh / zh-Hant / en），禁止硬编码自然语言后续再补。
- 新增或修改 UI 前必须先在 `src/components/` 与相关 `src/features/` 检索复用，禁止在 feature 内重新拼装已有的通用交互。
- 前端只能消费 `docs/api-spec-v1.md` 定义的端点；缺端点先补契约，禁止模块内自造接口或直连第三方。
- **发布前必须重建 `web/dist`**（`pnpm build`），并注意与 `go build` 的串行约束。

### ADAPTER:platform/new-api-upstream

applies_when: "涉及 `relay/**`、`relaykit/**`、`common/**`、`constant/**`、`setting/**`、`service/**`、`controller/**`、`middleware/**` 等移植自 new-api 的基座代码"
authority: "低于通用宪法，高于普通建议"
verification: "`go build ./... && go test ./... -count=1`；转发路径改动另跑 `verify/final/e2e.sh` 与 `verify/final/fault_injection.sh`"

- 基座是**移植 new-api 源码**（design §10.5），保留其包布局；适配器与 `service`/`setting`/`model` 深度绑定，**不做净室剥离**。
- 改动基座代码前先确认：这是「转发正确性必需」还是「PBR 产品面」？后者应落到 `internal/`，不要把新语义塞进适配器。
- 许可证义务不可随品牌替换：保留 `LICENSE`、`NOTICE`、`THIRD-PARTY-LICENSES.md` 与 AGPL 版权头；品牌可替换，版权不可替换（design §17）。
- 面向用户的文案与标识中不得出现任何第三方品牌名；代码注释中标注「移植自某上游文件」属于溯源，允许保留。
- **AGPL-3.0 且本仓库已公开发布**：署名义务持续生效，保留版权头与许可文件；同时禁止把部署私有数据写入任何会被公开的内容（design §14.9）。

## 实现规则

- 从合同、schema、service 或核心模型开始，而不是从 UI、HTTP handler、CLI 或临时脚本倒推核心。
- 添加测试、fixture 或 gate 脚本时，应放在风险真正所属的层级（分层见 `docs/test-spec-v1.md` §3）。
- 使用结构化 API、解析器、生成器和本地 helper，而不是脆弱的字符串拼接和临时路径判断。
- 不手改生成文件；项目有生成器时，运行生成器并检查输出。
- 不用旧记忆、旧输出或「应该是这样」代替当前命令和当前代码证据。
- 修改代码、接口、配置、架构边界、用户行为或产品语义后，必须同步项目文档（按影响面选择 design / api-spec / routing-spec / token-spec / ui-spec / hermes-spec / test-spec）。
- 遇到 review 反馈或用户质疑时，先查当前实现、框架底层和可复现证据，再决定是否接受修改。
- 修改 agent 行为规则、宪法、skill、系统提示或工作流模板前，必须说明为什么现有规则不够、改动会影响哪些行为，并用至少一个真实场景或压力测试验证改动有效。
- 配置、密钥和权限必须显式下传或通过项目既有配置系统读取；禁止全局可变静态、硬编码密钥、日志明文输出敏感信息或把安全字段写入普通 artifact。
- 观测、指标、健康状态、诊断报告必须来自真实数据源。拿不到数据就返回 N/A、空集合或结构化不可用原因，禁止固定值、占位值和伪健康状态。
- **管理面写端点必须显式声明 dry-run 类别**（`preview` 或 `reject`，无第三类），并遵守「成功无信封、失败带真实 HTTP 状态码 + 稳定 `error.code`」（api-spec §2/§3、design §5.1）。新增端点必须登记进 `internal/api/ops_routes.go` 的端点表，漏登记由守卫测试变成构建期失败。

## 多 Agent 规则

- 优先使用多 agent 执行模式，但只在任务能拆成互不干扰的独立域时使用。
- 每个子 agent 必须有明确范围、输入、禁止事项和期望输出。
- 不把当前关键路径上的阻塞任务交给子 agent 后原地等待；主 agent 必须继续做不重叠的工作。
- 多 agent 修改代码时必须分配不重叠的文件或模块 owner，并在集成时复核冲突。
- 子 agent 的结论不是最终真源；最终结论仍要由主 agent 对照当前代码、测试和文档验收。
- 任务拆解后尽可能并行分派子代理执行，审查其产出，不合格则打回重做，直至全部通过。

## 验收命令

> 完整分层、方法与闸口矩阵以 [`docs/test-spec-v1.md`](docs/test-spec-v1.md) 为准；本节只列命令。

**L0 静态闸口 + L1 单元/集成**

```bash
# Go 主模块
go build ./...
go vet ./...            # 须零输出
go test ./... -count=1  # 全绿

# relaykit 独立模块
cd relaykit && go test ./... -count=1

# 控制台
cd web
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint               # 必须 0 error
pnpm test
pnpm format:check
pnpm copyright:check
pnpm build              # 重建 web/dist（tracked 产物）
```

**L2–L5 端到端验收**（脚本默认从自身位置推导仓库根，可用 `PBR_REPO=` 覆盖）

```bash
bash verify/e2e-api/ops_runbook.sh        # L2 真实管理 API 运维
python3 verify/e2e-ui/user_journey.py     # L3 真实用户操作（无头 Chromium + CDP）
bash verify/final/e2e.sh                  # L4 端到端
bash verify/final/fault_injection.sh      # L4 故障注入
bash verify/final/hermes_acceptance.sh    # L5 Hermes 档案
bash verify/deploy/smoke.sh               # L4 部署（需 Docker）
```

**收尾闸口**（合入 main 前）：L0 全量 + L1 全量 + L2 + L3 全绿。

## 验收规则

- 验收必须匹配改动风险，不能只跑最轻命令就宣布完成。**必跑项按 `docs/test-spec-v1.md` §8 的复跑触发矩阵选取**。
- 声称「完成」「修好」「通过」前，必须提供本轮实际运行过的命令、测试、日志、截图、构建或用户侧证据。
- 如果无法运行某项关键验收，必须说明原因、影响范围和可替代证据，不能隐瞒（环境缺失记 SKIP 并退出码 2，不得包装成 PASS）。
- 发现失败时，先复现、收集证据、建立可证伪假设，再修改。禁止无复现地猜测原因。
- UI/前端改动必须检查真实渲染、交互、加载态、错误态和移动/桌面约束；不得只看代码（L3 是规范要求的验收层）。
- 性能、安全、数据、权限、并发、迁移、发布相关改动必须扩大验收面。
- warning、lint warning、类型告警、格式漂移、生成物漂移、文档索引缺失和本轮 TODO/TBD 都按缺陷处理；除非明确记录为非本轮债务，否则不能带着它们声称完成。
- 测试若依赖隔离状态，必须显式构造隔离边界；基于缓存、最近窗口、截断列表或有限快照的测试，不能伪装成全局累计事实。
- 端到端验收一律**独立端口 + 独立 SQLite + 内置假上游**（`internal/testutil/fakeupstream`），**绝不打真实厂商**，不碰现网容器与数据卷。
- **并发禁忌**：L3（会重建 `web/dist/`）与任何 `go build` 类脚本（L2/L4/L5）**不得并发运行**，必须串行。
- 验收产生的日志与截图不入库（`verify/**/*.log`、`verify/**/shots/` 已忽略）；脱敏结论写进对应目录的 `README.md`。
- L3 结束时用 `git checkout -- web/dist/index.html` 恢复 tracked 产物，保持工作区干净。

## 文档分层

- 写文档前必须先判断受众：内部真源、外部用户、发布说明、一次性复盘不能混写。
- 内部架构、owner 边界、验收门禁、治理历史、审计结论、阶段计划进入 `docs/`（基线 + 同级 spec + `docs/adr/`）。
- 外部用户文档（部署、配置、公开 API、集成、运维、排障）只写进 `README.md` / `CHANGELOG.md` / `CONTRIBUTING.md` / `SECURITY.md`。
- **文档纪律（强制）**：`docs/design-v1.md` 及配套规范只描述系统本身，**不含任何部署私有数据**——不写具体渠道名、模型名、车道名单、厂商地址、凭据、成本单价。这类内容属部署方自己的运维配置，不入仓库。
- 同一变更同时影响用户行为和内部机制时，必须分别写入对应层。
- 一次性交接/复盘文档在任务闭合后应删除或归档，不能留在主目录误导后续 agent。
- 新增当前真源文档必须被真源索引引用；失效、被吸收或一次性的文档必须归档或标注非当前。

## Git 与安全要求

- 默认分支使用 **main**，最终只保留 main 一个分支。
- **分支约定**：改动切临时分支 → 完成后用 `git merge --no-ff` 合入 main 以保留分支分叉 → 删除临时分支。分支名用 `<type>/<slug>`（如 `fix/price-decimal-input`），合并提交信息用 `merge <分支名>: <说明>`。
- 提交必须原子化、工作区绝对干净，一次提交只做一件事，类型用 `feat` / `fix` / `docs` / `refactor` / `test` / `chore` / `breakdown` 等。
- 提交信息使用清晰的中文或约定式前缀。
- 提交后工作区须回到干净状态，不留任何过程垃圾；仓库当前版本只保留设计文档和最终成品，中间过程仅存于 Git 历史。
- 除可生成或运行时产物（模型文件、临时输出、日志、上传资源、`.env`、依赖缓存等）外，所有源码、配置、迁移脚本、插件源码等唯一知识来源一律追踪；不确定时，能由已追踪文件生成则不追踪。
- 例外：`web/dist/index.html` 是**刻意追踪的构建产物**（`go:embed` 需要它才能编译），`web/dist/*` 其余内容忽略。
- 禁止把 API Key、访问令牌、私钥、签名口令、真实用户数据和带凭据的远程 URL 提交到任何 Git 历史。仓库里出现的 key 一律用占位符（如 `__INJECT_BY_OPERATOR__`）。
- 公钥可以提交；私钥只能保存在被 `.gitignore` 排除的本地路径中。
- 提交或发布前确认 git root、`git status --short`、ignored 文件、staged file list 和嵌套仓库边界。
- 禁止 `git add .`。只 stage 与本任务相关的文件。
- 禁止 force-add ignored 文件，除非用户明确点名路径并要求。
- dirty worktree 中不得回滚、覆盖或吸入用户未授权的改动。
- **嵌套仓库**：`reference/` 下的 `new-api`、`octopus-bestrui`、`octopus-hureru-fork` 各自是独立 Git 仓库，且**整体被 `.gitignore` 排除**、不纳入根仓库。父仓与子仓分别审计、分别验收；`reference/` 内一律只读，不提交、不改动。
- 不使用破坏性 git 命令，除非用户明确要求并确认风险。

## 仓库边界

- **根仓库**：`github.com/zzyyyds88/PowerBarRations`，跟踪主项目：`main.go`、`internal/`、`pkg/`、`relay/`、`relaykit/`（嵌套 Go module）、`model/`、`service/`、`setting/`、`controller/`、`middleware/`、`router/`、`common/`、`constant/`、`i18n/`、`logger/`、`types/`、`web/`、`docs/`、`verify/`、`Dockerfile`、`docker-compose.yml`。
- **`reference/`**：上游源码只读参考，**不纳入根仓库**（`.gitignore` 已排除），仅供调研。当前含：
  - `new-api`（github.com/QuantumNous/new-api，main@04c64734）——厂商适配层与转发管道的复用来源；
  - `octopus-bestrui`（github.com/bestruirui/octopus，钉 live 镜像 revision e7a1455）——线上章鱼网关的真实上游，车道/冷却/亲和语义以它为准；
  - `octopus-hureru-fork`（github.com/Hureru/octopus，dev@0e1a7ee）——仅作熔断器三态算法的设计参考，其 schema 与线上已分叉。
- 本项目不设独立子仓库（`relaykit/` 是嵌套 Go module，不是独立 Git 仓库）。
- `.uploads/`、`tmp/`、`spike/`、`logs/`、`data/`：临时输入与运行时产物，不纳入版本管理。
- `.agents/skills/`：仓库内的 AI 运维 skill（`powerbar-rations-ops`），属于受追踪的项目资产；改动它需与 `docs/api-spec-v1.md` 保持一致。
- **远程**：本仓库**已公开发布**（GitHub，AGPL-3.0；design §18.4，2026-09-23 决策）。因此：① AGPL 署名义务持续生效，不得声称重新授权；② **仓库内容与全部 Git 历史对公众永久可见**，部署私有数据（渠道名、模型名、车道名单、厂商地址、凭据、成本单价）一律不得进入仓库、issue 或 PR 描述；③ 推送前必须自查 `git status`、staged 列表与新增提交的信息。

## 对外贡献规则

- 对外提交 PR、patch、issue 回复或发布前，必须先读 `CONTRIBUTING.md`、`.github/pull_request_template.md`、已有 open/closed PR、issue 和维护者反馈。
- 必须确认这是用户真实遇到的问题或当前代码可复现的问题；禁止用理论审查、猜测或编造的问题描述发起贡献。
- 一次只解决一个问题，禁止混入无关改动、顺手重构、格式化风暴或项目私货。
- 变更若只服务某个团队、领域、工具、fork 或个人习惯，不应塞进通用核心；应放到配置或项目私有层。
- 提交前必须向用户说明完整 diff 范围、验证证据和剩余风险，得到明确确认后再发布。
- 安全漏洞不开公开 issue，走 `SECURITY.md` 的私密报告通道。

## 必须停止并询问的情况

- 用户需求、当前代码、agent 宪法、真源文档或验收结果互相冲突。
- 需要无意义兼容、保留旧产品残影或同时支持互斥路线（例如恢复计费/多用户/`weighted` 模式）。
- 需要创建、改写或废弃 agent 宪法、真源索引、公开文档、产品命名、核心 API、schema、权限模型或部署流程。
- 需要删除旧 API、字段、路由、配置、缓存、生成合同、部署脚本或迁移历史。
- 当前证据表明用户的目标会损害架构、数据、安全、权限、用户体验或长期可维护性。
- 上下文不足以判断 owner 边界，并且猜测会造成不可逆或大范围影响。

停止时不要只说「无法继续」。必须给出：冲突证据、可选处理方向、推荐方向、需要用户确认的问题。

## 交接规则

当上下文过大、需要换窗口、需要交给另一个 agent，或任务尚未闭合时，必须产出可直接复制的交接文本，包含：

1. 当前目标和已确认的产品/架构边界。
2. 当前 git 状态、已改文件、未提交文件和嵌套仓库状态。
3. 已完成工作、实际验收命令和结果。
4. 未闭合风险、漂移警告和禁止触碰的用户改动。
5. 下一步最安全命令和停止条件。

---

任何时候使用中文，除非用户明确要求英文输出。
