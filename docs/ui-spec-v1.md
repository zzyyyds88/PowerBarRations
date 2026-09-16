# PowerBarRations 控制台规格 v1

> 规范性文件，从属于 [`design-v1.md`](design-v1.md) §6。数据来源一律为 [`api-spec-v1.md`](api-spec-v1.md)；令牌相关见 [`token-spec-v1.md`](token-spec-v1.md)；运行态见 [`routing-spec-v1.md`](routing-spec-v1.md) §7。
> **设计基线**：控制台**直接搬迁 new-api 上游前端**（`reference/new-api/web`，Rsbuild + React + TanStack Router + Base UI + Tailwind），在其上做减法（删多用户/计费）与接线（PBR 认证、模型成员链）。
> 文中占位符（`model-1` 等）示意，**不含部署私有数据**。

---

## 1. 基线、技术与目录（照上游，不换栈）

| 维度 | 选型（上游一致） |
|---|---|
| 构建 | **Rsbuild**（`rsbuild.config.ts`） |
| 框架 | React 19 + TypeScript |
| 路由 | TanStack Router（`src/routes/` + `routeTree.gen.ts`） |
| 数据 | TanStack Query + TanStack Table + TanStack Virtual |
| 组件 | Base UI 原语 + 自建 `components/ui`（Tailwind） |
| 国际化 | 上游 i18n（zh / zh-Hant / en） |
| 图表 | VChart |
| 图标 | HugeIcons + lobehub icons |
| 包管理 | 上游 `bun.lock`（环境不便时允许 pnpm） |

**目录结构**（照上游）：`web/src/{assets,components,config,context,features,hooks,i18n,lib,routes,stores,styles}`，业务模块在 `features/<module>/`。

**关键约定**：`features/<module>/api.ts` 只封装对 [`api-spec-v1.md`](api-spec-v1.md) 端点的调用；缺端点先补契约，禁止模块内直连或自造接口。

---

## 2. 品牌与命名（全量替换）

- 所有面向用户的品牌标识替换为 **PowerBarRations**：页面标题、logo/favicon、i18n 文案、`/api/version` 自述、OpenAPI `info.title`、错误页脚、空态文案、`localStorage` 键名前缀（`pbr_*`）、CSS 变量前缀、构建注入的应用名。
- 不残留 `new-api` / `New API` / `Octopus` 等品牌字样。**代码注释里对来源文件的引用可以保留**（"移植自某上游某文件"），但产品名与界面文案不得出现第三方品牌。
- **许可证义务**：保留 AGPL-3.0 版权头、`LICENSE`、`NOTICE`、第三方许可清单；不得声称重新授权。品牌替换 ≠ 版权替换。
- 品牌替换范围包括：二进制名 `pbr`、Docker 镜像/容器名、配置前缀 `PBR_`、日志前缀。

---

## 3. 认证与首次启动（PBR 专有，上游无此模型）

- 无账号、无用户名、无注册。只有**一个登录口令**（见 [token-spec-v1.md](token-spec-v1.md) §2）。
- 首启流程：
  1. `GET /api/setup/status` 若 `initialized=false` → 强制进入**设置口令页**；
  2. 提交 `POST /api/setup`；成功后签发**会话 Cookie** 直接进控制台，并展示一次派生**管理密钥**（供 AI 使用，可复制）；
  3. 进入数据看板。
- 常规登录页：单输入框（登录口令）→ `POST /api/auth/login` → 服务端签发 **HttpOnly 会话 Cookie**，之后所有管理请求由浏览器自动携带；**不把管理密钥存 localStorage**。
- 登出：`POST /api/auth/logout` 清 Cookie 并跳登录页。
- 401 处理：清除本地会话标记并跳登录页。
- **上游 `features/auth`、`profile`、`security`、`wallet` 等登录/用户模块一律删除**，用 PBR 的 `setup/login` 替换。

---

## 4. 实时数据

- 车道运行态（当前成员 / 探测占用 / 亲和截止 / 各成员冷却）通过 **SSE** `GET /api/route-events` 增量推送，合并进前端缓存，**不写回**持久配置。
- 用 `fetch` + `ReadableStream` 读 SSE（Cookie 自动携带；**不用 `EventSource`** 以便统一错误处理与中断）。
- 另有 **30s 轮询**兜底；SSE 仅作加速，不作为唯一数据源。
- SSE 断开自动重连，重连后先取一次快照再接受增量。

---

## 5. 模块清单：保留 / 删除

上游 `web/src/features/` 共 27 个模块，逐个定去留：

| 上游模块 | 去留 | 说明 |
|---|---|---|
| `channels` | **保留** | 渠道管理：上游地址、key、模型清单、优先级、探活 |
| `models` | **保留 + 改造** | 模型管理：**内联成员链（故障切换）**，见 §6.3 |
| `keys` | **保留** | 令牌：分账与车道权限 |
| `usage-logs` | **保留** | 请求日志（含 attempts 链） |
| `dashboard` / `home` | **保留** | 数据看板 |
| `playground` / `chat` | **保留** | 试打台 |
| `system-settings` | **保留** | 系统设置（删计费/支付子节） |
| `system-info` | **保留** | 系统信息 |
| `performance-metrics` | **保留** | 性能指标 |
| `system-update` | **保留** | 更新检查 |
| `task-plugins` | **保留** | JS 任务插件 |
| `model-pricing` | **保留** | 单价表（只做成本折算，非计费） |
| `about` / `legal` | **保留** | 关于/法律页（品牌替换） |
| `errors` | **保留** | 错误页 |
| `setup` | **重写** | 改为 PBR 首启设口令 |
| `auth` | **重写** | 改为 PBR 口令登录/会话 |
| `users` / `profile` / `security` / `rankings` | **删除** | 多用户/账号安全 |
| `wallet` / `pricing` / `redemption-codes` / `subscriptions` | **删除** | 计费/支付/订阅/兑换 |
| （上游无） | **新增** | 若上游缺"录像/视频任务"独立页，按需补；否则随 `task-plugins` 暴露 |

**删除的执行口径**：连同其 `routes/` 条目、菜单项、i18n 键、以及对应的前端 API 调用一起删；后端对应路由已在 W7 物理删除，不会残留可点入口。

---

## 6. 逐页规格

> 统一要求：加载态（骨架）、空态（引导 + 主操作）、错误态（展示 `error.code` 与 `message`）、成功 toast。列表页沿用上游的虚拟化与筛选进 URL 的做法。

### 6.1 设置口令 / 登录 `/setup`、`/login`

- **设置口令页**（仅未初始化）：口令输入（强度提示，不设硬性最小长度）+ 确认；提交后签发会话 Cookie 直接进控制台，同时弹出**一次性管理密钥**（供 AI），附复制按钮与"如何重算"说明。
- **登录页**：单输入框 + 提交（成功后进控制台）；失败按退避提示"请稍后重试"。
- **验收**：未初始化时访问任何页面都被重定向到设置口令页；初始化后该页不可再进入（409）；localStorage 中不存在管理密钥。

### 6.2 数据看板 `/dashboard`

分节（全部读 PBR `GET /api/stats`，口径 = 请求数 / 成功率 / token / 上游花费）：

| 分节 | 内容 |
|---|---|
| `/dashboard/overview` | 用量速览（上游花费 / 请求数 / 成功率 / Token，24h）+ 品牌/公告/FAQ/服务状态面板 + 起步向导 |
| `/dashboard/models` | 模型分析：按模型的请求/token/花费汇总、花费趋势与分布 |
| `/dashboard/cost` | 成本统计：按渠道/模型/车道/密钥的上游花费 |

- **删除**：`/dashboard/flow`（依赖已删的 `/api/data/flow`）与 `/dashboard/users`（多用户分析）。
- **单价**：渠道级上游单价在渠道编辑页"上游单价"配置；全局默认单价在"系统设置 → 模型 → 单价表"。
- **验收**：概览/模型/成本三节都有真实聚合；成本卡标注"仅折算、非计费"。

### 6.3 模型管理 `/models`（**核心改造**）

用户的完整心智只有三步，本页承担第二步：

1. **渠道管理**：填上游（base_url + key）与它提供的模型名；
2. **本页**：定"模型1 优先打上游1、再打上游2"；
3. **令牌**：允许请求模型1。

规格：

- 列出**全部路由键**（`GET /api/models`）：模型名、状态（`explicit` 已配车道可调用 / `unconfigured` 未配车道**不可调用**）、成员数、当前顺序摘要。
- 点开某模型显示**成员链**（`GET /api/routes/{model}`）：渠道 + 顺序 + 优先级 + **解析后的上游真名**；未配车道时展示"建议成员链"（按渠道 priority）并标红不可调用。
- **故障切换编辑**：把成员链固化为显式顺序（拖拽或填 priority），保存即写入 PBR 车道（名称 = 模型名，模式默认 `failover`）；`PUT /api/lanes/{model}`，成员 `{channel, upstream_model, priority}`（`upstream_model` 留空即用渠道映射）。
- **一键固化**：提供"为所有未配车道的模型生成车道"按钮（`POST /api/lanes/seed`，先 dry-run 预览）。
- 保留上游模型元数据能力（来源 `/api/models/**`）：模型描述、标签、供应商等。
- **验收**：为"模型1"设定"上游1 → 上游2"后，`GET /api/routes/模型1` 顺序一致；上游1 故障时请求逃逸到上游2；拖拽顺序与后端 priority 一致；**未配车道的模型请求返回 503**，配好后立即可用。

### 6.4 渠道管理 `/channels`、`/channels/$id`

- 沿用上游 `channels`：卡片列表 + 表单（协议类型、**API 地址**、key（只写不读）、优先级、模型清单、**模型映射**、参数覆盖 JSON、代理、启用）+ 探活 + 批量操作 + 标签。
- **模型清单**：手工增删；另提供"从上游拉取"（`POST /api/channels/{name}/sync-models?dry_run=`，先看差异再确认）。
- **模型映射**（`model_mapping`）：两列表格"路由键 → 上游真名"，用于上游命名与路由键不一致；车道成员默认用它解析上游名，成员级 `upstream_model` 可覆盖。
- **验收**：列表与详情只显示 `key_prefix`；新增模型名后立即出现在模型管理页（未固化时标注不可调用）；配好映射后车道成员的上游真名随之变化；删除被车道引用的渠道返回 409 并给出引用清单。
- **注意**：上游 `update_balance` 相关 UI 保留但只作运维展示——PBR 已移除"余额≤0 自动禁用渠道"逻辑。

### 6.5 令牌 `/keys`

- 沿用上游令牌面板：创建、启用停用、编辑、删除；用量卡（请求数/token/折算成本/最后使用）。
- 权限（PBR 语义）：默认"**允许全部模型**"，可切"仅允许指定模型"并叠加拒绝；**判定对象是路由键（模型名）**。
- **验收**：明文只在创建/轮换时出现一次；权限候选来自 `GET /api/models`（不是显式车道），且支持手填。

### 6.6 请求日志 `/logs`、`/logs/$id`

- 沿用上游 `usage-logs`：筛选（车道/渠道/令牌/模型/成功与否/时间）、虚拟滚动。
- **详情**：attempts 逐尝试时间线（成员、状态、耗时、`error_kind`），区分 `cooldown`/`circuit_break`/`skipped` 状态色。
- **验收**：一次含逃逸的请求能完整复现 `failed → success`；被跳过的成员有原因说明。

### 6.7 系统设置 `/settings`

沿用上游 `system-settings`，**删除计费/支付相关子节**：

| 分节 | 内容 |
|---|---|
| 账户 | 修改登录口令（说明会改变管理密钥与会话） |
| API 密钥 | 管理密钥算法、当前前缀、"如何重算"说明与复制（供 AI） |
| 外观 | 主题、语言（zh/zh-Hant/en） |
| 备份 | 导出配置 / 导入（含 dry-run diff） |
| 信息 | 版本、构建时间、运行时长、数据库路径（不含密钥） |
| 单价 | 全局默认单价表（记账，非计费）；渠道级上游单价在渠道编辑页"上游单价"配置 |
| 日志 | 日志保留天数、手动清理 |
| 系统 | 自动禁用/自动恢复开关、失败阈值、半开周期、默认六键、自动禁用关键词表 |

- **删除**：支付设置、合规、OAuth/多用户相关子节。
- **验收**：关键词增删回读一致；"自动恢复"默认开启且页面写明与旧系统相反。

### 6.8 试打台 `/playground`

沿用上游 `playground`：模型选择 + 多轮消息 + 流式开关 + 思考参数（`enable_thinking`/`reasoning_effort` 成对）+ 温度/最大 token；展示流式输出、TTFT、用量与响应头 `X-Served-By`。

### 6.9 任务插件 `/task-plugins`

沿用上游 `task-plugins`：列出/管理 JS 任务插件；后端 `/api/plugin/task/**` 已保留。

---

## 7. 构建与集成

- 构建 `pnpm build`（或 `bun run build`，Rsbuild）→ 产物交 Go `embed`，单二进制同时服务 `/v1/*`、`/api/*` 与控制台静态资源；SPA 路由回退 `index.html`。
- 开发：上游 `dev` 经代理转发 `/api`、`/v1` 到本地 `pbr`。
- i18n：新增文案必须同时补三语；禁止硬编码中文到组件。
- 品牌：构建时注入应用名与版本，供标题/关于页/`/version` 使用，避免散落硬编码。

---

## 8. 控制台验收总纲

1. 全部保留页面可加载、可操作、无 console 报错；`pnpm build` 零报错。
2. 每个写操作刷新后可从 API 回读到一致结果。
3. **模型管理的成员链（故障切换）实测**：设定"优先上游1 → 上游2"后，routes 顺序一致、上游1 故障时逃逸。
4. 无任何第三方品牌残留；无计费/多用户残留入口（菜单、路由、i18n 全清）。
5. 加载/空/错误三态齐备；错误态展示稳定 `error.code`。
6. 页面数据全部来自 api-spec 端点；缺端点先补契约。
