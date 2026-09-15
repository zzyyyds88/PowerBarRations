# PowerBarRations 控制台规格 v1

> 规范性文件，从属于 [`design-v1.md`](design-v1.md) §6。数据来源一律为 [`api-spec-v1.md`](api-spec-v1.md)；令牌相关见 [`token-spec-v1.md`](token-spec-v1.md)；运行态见 [`routing-spec-v1.md`](routing-spec-v1.md) §7。
> **设计基线**：控制台以线上路由层的前端（`bestruirui/octopus@e7a1455` 的 `web/`）为蓝本迁移其**功能原理**，换品牌、换 API、按车道模型适配；不是重画，也不是照搬 new-api 的巨型控制台。
> 文中占位符（`lane-alpha` 等）示意，**不含部署私有数据**。

---

## 1. 技术栈（照蓝本，不换栈）

| 维度 | 选型 |
|---|---|
| 构建 | Vite + TypeScript |
| 框架 | React 19 |
| 组件 | Radix UI 原语 + 自建 `components/ui`（Tailwind 4 + CVA + tailwind-merge） |
| 数据 | TanStack Query（查询缓存/失效）+ TanStack Virtual（长列表虚拟化） |
| 状态 | Zustand（少量全局态） |
| 国际化 | use-intl（zh-Hans / zh-Hant / en 三语，与蓝本一致） |
| 图表 | Recharts |
| 拖拽 | @hello-pangea/dnd（成员排序，蓝本已用于分组） |
| 提示 | sonner（toast） |
| 图标 | lucide-react |

**目录结构**（照蓝本）：

```
web/src/
├── api/            # 每个资源一个文件：client.ts / group.ts / channel.ts / model.ts / apikey.ts / log.ts / stats.ts / setting.ts / queries.ts
├── components/
│   ├── ui/         # Radix 封装的基础件
│   ├── common/     # CopyButton / PageActions / AnimatedNumber / VirtualizedGrid
│   └── modules/    # 业务模块，每个模块一个目录（见 §5）
├── hooks/
├── locales/        # en.json / zh_hans.json / zh_hant.json
├── provider/
├── stores/
└── lib/
```

**关键约定**：`api/` 只封装对 [`api-spec-v1.md`](api-spec-v1.md) 端点的调用；缺端点先补契约，禁止模块内直连或自造接口。

---

## 2. 品牌与命名（全量替换）

- 所有面向用户的品牌标识替换为 **PowerBarRations**：页面标题、logo/favicon、i18n 文案、`/api/v1/version` 自述、OpenAPI `info.title`、错误页脚、空态文案、`localStorage` 键名前缀（`pbr_*`）、CSS 变量前缀。
- 不残留 `new-api` / `Octopus` / 蓝本的中文别称等品牌字样。**代码注释里对来源文件的引用可以保留**（如 "移植自某上游某文件"），但产品名与界面文案不得出现第三方品牌。
- **许可证义务**：保留 AGPL-3.0 版权头、`LICENSE`、`NOTICE`、第三方许可清单；不得声称重新授权。品牌替换 ≠ 版权替换。
- 品牌替换范围包括：二进制名 `pbr`、Docker 镜像/容器名、数据库/配置前缀 `PBR_`、日志前缀。

---

## 3. 认证与首次启动

- 无账号、无用户名、无注册。只有**一个登录口令**（见 [token-spec-v1.md](token-spec-v1.md) §2）。
- 首启流程：
  1. `GET /api/v1/setup/status` 若 `initialized=false` → 强制进入**设置口令页**；
  2. 提交 `POST /api/v1/setup`；成功后展示一次派生管理密钥（可复制），并说明"登录口令变更后该密钥随之变化"；
  3. 进入 Dashboard。
- 常规登录页：单输入框（登录口令）→ `POST /api/v1/auth/login` → 取回管理密钥存入 `localStorage`，之后所有管理请求带 `Bearer`。
- 401 处理：清空本地密钥并跳登录页；设置页需可重新进入（未初始化时）。
- 移植蓝本的 `login` 模块并**删除**其账号/注册相关分支。

---

## 4. 实时数据（照蓝本机制）

- 车道运行态（当前成员 / 探测占用 / 亲和截止 / 各成员冷却）通过 **SSE** `GET /api/v1/route-events` 增量推送，合并进前端缓存，**不写回**持久配置。
- **鉴权已定**：用 `fetch` + `ReadableStream` 读 SSE 并带 `Authorization` 头（**不用 `EventSource`**，它无法带自定义头），管理密钥不进 URL。
- 另有 **30s 轮询**兜底（对齐蓝本 `refetchInterval`）；SSE 仅作加速，不作为唯一数据源。
- SSE 断开自动重连，重连后先取一次快照再接受增量。

---

## 5. 模块清单与来源映射

蓝本模块 → PBR 模块（"功能原理迁移"的落点）：

| 蓝本模块 | PBR 模块 | 改动 |
|---|---|---|
| `login` | 设置口令 + 登录 | 删账号/注册分支；加首启设口令 |
| `home`（activity/chart/metric-tabs/rank/total） | Dashboard 概览 | 指标口径改为 Token/成本折算/成功率；去计费 |
| `group`（Card/Create/Editor/ItemList/MemberStatus） | **车道 Lanes** | 核心改造：模式扩到四种；成员行加 `upstream_model`/`public_alias`/成员级覆盖 |
| `channel`（Card/Form/Stats） | 渠道 Channels | 基本照搬；key 只写不读 |
| `model`（Create/index/Item/ItemOverlays） | 模型路由 Models | 改为"可路由模型 + 解析出的成员链"，验证渠道声明即自动成链 |
| `log`（index/Item） | 请求日志 Logs | 详情页改为 attempts 逐尝试链 |
| `apikey-dashboard` | 令牌面板 Keys | 权限改为车道策略；明文只显一次 |
| `setting`（Account/APIKey/Appearance/Backup/Info/LLMPrice/Log/System） | 设置 Settings | 见 §6.9；`LLMSync` 不迁移 |
| `logo` | 品牌标识 | 换 PowerBarRations |

**PBR 增补（蓝本无）**：Playground 试打页（已定保留）、模型路由页 `/models`（展示解析出的成员链）。

**不迁移**：蓝本的更新检查/自升级、站点托管/签到同步、计费与额度相关全部页面。

**迁移工作量说明（重要，别按"零改造"排期）**：
- `group` 模块**需实打实改造**：成员行加上游模型名/别名/成员级覆盖、模式扩到四种、对接 `/routes/{model}` 与运行态 SSE、渠道选择改为 PBR 渠道。
- `channel` 模块需加**模型清单**与 `sync-models` 动作。
- 其余模块以"换 API + 换品牌 + 删计费"为主。
- 包管理沿用蓝本的 **pnpm**（`pnpm-lock.yaml`），不要换成 bun/npm。

---

## 6. 逐页规格

> 统一要求：加载态（骨架）、空态（引导 + 主操作）、错误态（展示 `error.code` 与 `message`）、成功 toast。列表页沿用蓝本的虚拟化与筛选进 URL 的做法。

### 6.1 设置口令 / 登录 `/setup`、`/login`

- **设置口令页**（仅未初始化）：口令输入（≥16 字符，强度提示）+ 确认；提交后弹一次性管理密钥，附复制按钮与"如何重算"说明。
- **登录页**：单输入框 + 提交；失败按退避提示"请稍后重试"。
- **验收**：未初始化时访问任何页面都被重定向到设置口令页；初始化后该页不可再进入（409）。

### 6.2 Dashboard `/`

- **区块**（照蓝本 home）：顶部总量卡（请求数/成功率/token/折算成本）、时间范围切换（metric-tabs）、活动曲线（chart）、成员/模型排行（rank）、最近失败列表。
- **数据**：`GET /stats`、`GET /lanes`、`GET /lanes/{n}/health`、`GET /logs?success=false`。
- **验收**：异常车道（有冷却/熔断成员）在概览有醒目入口；成本卡标注"仅折算、非计费"。

### 6.3 车道列表 `/lanes`

- 照蓝本 `group/index`：卡片网格，每卡显示车道名、模式、成员数、当前成员、异常成员数；操作：新建、编辑、删除、探活。
- **新建**（照蓝本 `Create`）：名称 + 模式（**四选一**）+ 六键 + 成员。
- **验收**：模式下拉含 failover/manual/weighted/round_robin；创建后列表出现且可回读一致。

### 6.4 车道编辑器 `/lanes/$name`

照蓝本 `group/Editor` + `ItemList` + `MemberStatus`：

- **成员列表**：**拖拽排序**（`@hello-pangea/dnd`）即改 priority；每行显示渠道、上游模型名、别名、权重、成员级覆盖入口、实时状态徽标（`closed`/`open`/冷却倒计时/探测中）。
- **运行态**：来自 SSE + health 快照；显示当前成员、亲和倒计时、逐成员连续失败数与滚动成功率。
- **操作**：探活（逐成员结果）、重置熔断、删除车道。
- **验收**：拖拽后端 member priority 与顺序一致；被熔断成员显示倒计时且刷新后仍在；重置后倒计时消失。

### 6.5 渠道 `/channels`、`/channels/$name`

- 照蓝本 `channel`：卡片列表 + 表单（协议类型下拉来自 `/capabilities`、base_url、**优先级**、**模型清单（路由键，多值编辑）**、key（只写）、`param_override` JSON 编辑、代理、启用）+ 单渠道 `Stats`（成功/失败/等待时长）。
- **模型清单**：手工增删；另提供"从上游拉取"按钮（`POST /channels/{name}/sync-models?dry_run=`，先看差异再确认），即原蓝本的模型同步收敛为渠道上的一个动作。
- **验收**：列表与详情只显示 `key_prefix`；新增模型名后该模型立刻可路由（`GET /routes/{model}` 可见本渠道）；删除被显式车道引用的渠道返回 409 并给出引用清单。

### 6.6 模型路由 `/models`

- 列出**全部可路由模型**（`GET /models`）：模型名、来源（`implicit` 隐式 / `explicit` 显式车道）、成员数；点开显示解析出的**成员链**（`GET /routes/{model}`，含渠道与优先级），一眼看清"这个模型现在按什么顺序打哪些上游"。
- 作用：验证"渠道声明即自动成链"是否符合预期；为显式车道编辑器提供模型名候选；为日志/统计提供模型维度。
- **验收**：新增一个渠道模型声明后，该模型立即出现在列表且成员链正确；全挂时成员链与日志的 `attempts` 顺序一致。

### 6.7 请求日志 `/logs`、`/logs/$id`

- 照蓝本 `log/index` + `Item`：筛选（车道/渠道/令牌/成功与否/时间范围）、虚拟滚动列表。
- **详情**：attempts 逐尝试时间线（成员、状态、耗时、`error_kind`、摘要），区分 `cooldown`/`circuit_break`/`skipped` 状态色。
- **验收**：一次含逃逸的请求能完整复现 `failed → success`；被跳过的成员有 `skipped` 说明。

### 6.8 令牌面板 `/keys`

- 照蓝本 `apikey-dashboard`：每把令牌的用量卡（请求数/token/折算成本/最后使用）；列表操作：创建、轮换、启用停用、删除。
- 权限编辑：默认"允许全部车道"；可切到"仅允许指定车道"并叠加拒绝列表。
- **验收**：明文只在创建/轮换时出现一次；页面明确显示"允许全部车道"或具体车道集。

### 6.9 设置 `/settings`

照蓝本 `setting/*` 迁移，删计费项：

| 分节 | 内容 |
|---|---|
| 账户 | 修改登录口令（说明会改变管理密钥） |
| API 密钥 | 展示派生管理密钥的算法与当前前缀；提供"如何重算"说明与复制 |
| 外观 | 主题深浅色、语言（zh-Hans/zh-Hant/en） |
| 备份 | 导出配置 / 导入（含 dry-run diff） |
| 信息 | 版本、构建时间、运行时长、数据库路径（不含密钥） |
| 单价 | 成本折算单价表（记账，非计费） |
| 日志 | 日志保留天数、手动清理（`logs/prune`） |
| 系统 | 自动禁用/自动恢复开关、失败阈值、半开周期、默认六键、自动禁用关键词表 |

- **不迁移**：蓝本的模型列表同步（LLMSync）、更新检查。
- **验收**：关键词增删回读一致；"自动恢复"默认开启且页面写明与旧系统相反。

### 6.10 Playground `/playground`（已定保留）

- 车道选择 + 多轮消息 + 流式开关 + 思考参数（`enable_thinking`/`reasoning_effort` 成对）+ 温度/最大 token；展示流式输出、TTFT、用量与响应头 `X-Served-By`。
- **验收**：可见真实服务成员；思考参数半关时给出 400 的明确提示。

---

## 7. 构建与集成

- 构建 `npm run build`（Vite）→ 产物交 Go `embed`，单二进制同时服务 `/v1/*`、`/api/v1/*` 与控制台静态资源；SPA 路由回退 `index.html`。
- 开发：`npm run dev` 经代理转发 `/api`、`/v1` 到本地 `pbr`。
- i18n：新增文案必须同时补三语；禁止硬编码中文到组件。
- 品牌：构建时注入应用名与版本，供标题/关于页/`/version` 使用，避免散落硬编码。

---

## 8. 控制台验收总纲

1. 全部页面可加载、可操作、无 console 报错；`npm run build` 零报错。
2. 每个写操作刷新后可从 API 回读到一致结果。
3. 成员拖拽排序、实时冷却/熔断徽标、SSE 重连三件事均实测通过。
4. 无任何第三方品牌残留；无计费/多用户残留入口。
5. 加载/空/错误三态齐备；错误态展示稳定 `error.code`。
6. 页面数据全部来自 api-spec 端点；缺端点先补契约。
