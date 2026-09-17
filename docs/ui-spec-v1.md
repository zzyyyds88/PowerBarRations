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

**关键约定**：`features/<module>/api.ts` 只封装对 [`api-spec-v1.md`](api-spec-v1.md) 端点（或 §6.4 说明的基座运维面 `/api/channel/**`、`/api/console/**`）的调用；缺端点先补契约，禁止模块内直连第三方或自造接口。

---

## 2. 品牌与命名（全量替换）

- 所有面向用户的品牌标识替换为 **PowerBarRations**：页面标题、logo/favicon、i18n 文案、`/api/version` 自述、OpenAPI `info.title`、错误页脚、空态文案、`localStorage` 键名前缀（`pbr_*`）、CSS 变量前缀、构建注入的应用名。
- 不残留 `new-api` / `New API` / `Octopus` 等品牌字样。**代码注释里对来源文件的引用可以保留**（"移植自某上游某文件"），但产品名与界面文案不得出现第三方品牌。
- **许可证义务**：保留 AGPL-3.0 版权头、`LICENSE`、`NOTICE`、第三方许可清单；不得声称重新授权。品牌替换 ≠ 版权替换。
- **例外（AGPL-3.0 §7(b) 法定义务）**：`NOTICE` 要求的上游署名句与项目链接**必须**出现在「关于 / 法律」页的醒目位置——这是强制义务，不受"界面不得出现上游品牌"约束；其余界面文案仍不得出现第三方品牌。
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
| `channels` | **保留** | 渠道管理（页面与菜单标题均为「渠道管理」）：上游地址、key、模型清单、探活；**没有优先级/权重/分组** |
| `models` | **保留 + 改造** | 模型管理：模型目录（元数据 + 按名推断并自动采用图标），见 §6.3；成员链在独立的「路由与故障切换」页维护。**侧边栏顺序：渠道管理 → 路由与故障切换 → 系统任务 → 模型管理 → 系统设置** |
| `keys` | **保留** | 令牌：车道权限与**消耗**（上游折算花费）展示，无额度语义 |
| `usage-logs` | **保留** | 请求日志（含 attempts 链） |
| `dashboard` / `home` | **保留** | 数据看板 |
| `playground` / `chat` | **保留** | 试打台 |
| `system-settings` | **保留** | 系统设置（删计费/支付子节） |
| `system-info` | **删除** | 系统信息页：内容是多节点「实例 / Role / 节点」面板，属基座多节点遗留；PBR 是单节点 SQLite（design-v1 §1.5）。页面、菜单、`/api/system-info/**` 与 `system_instances` 表一并物理删除 |
| `system-tasks` | **新增** | 系统任务独立页（`/system-tasks`）：原 `system-info` 页里的任务面板提为侧边栏独立页，替换原「系统信息」菜单位 |
| `performance-metrics` | **保留** | 性能指标 |
| `system-update` | **删除** | 上游版本更新检查（直连上游 releases）：自用部署不跟随上游发布，不出现「系统更新/最新版本/发布时间」入口与通告 |
| `task-plugins` | **删除** | JS 任务插件/异步生成任务（文生视频/音乐/图）：个人自用不接，整链路移除（管理页、渠道类型 61、插件绑定联动、任务日志 Task 分节、`/v1/tasks` 数据面） |
| `model-pricing` | **删除** | 全局默认单价表随「渠道价唯一」决策移除（设分节与 `PBRModelPrices` 均删）；死组件链（倍率/表达式编辑器）已批删 |
| `about` / `legal` | **保留** | 关于/法律页（品牌替换）；关于页空态**不指向上游仓库**，错误页**无上游 issue 反馈链接** |
| `errors` | **保留** | 错误页 |
| `setup` | **重写** | 改为 PBR 首启设口令 |
| `auth` | **重写** | 改为 PBR 口令登录/会话 |
| `users` / `profile` / `security` / `rankings` | **删除** | 多用户/账号安全 |
| `wallet` / `pricing` / `redemption-codes` / `subscriptions` | **删除** | 计费/支付/订阅/兑换 |

**删除的执行口径**：连同其 `routes/` 条目、菜单项、i18n 键、以及对应的前端 API 调用一起删；后端对应路由已在 W7 物理删除，不会残留可点入口。

---

## 6. 逐页规格

> 统一要求：加载态（骨架）、空态（引导 + 主操作）、错误态（展示 `error.code` 与 `message`）、成功 toast。列表页沿用上游的虚拟化与筛选进 URL 的做法。

### 6.0 公共首页 `/`（未配置自定义内容时的默认落地页）

未在 `/system-settings/site` 配置自定义首页内容（Markdown / HTML / URL）时，`/` 渲染内置落地页，分节固定为：

| 分节 | 内容 |
|---|---|
| Hero | PBR 定位（模型即路由键 / 同名车道 / 优先级故障转移）+ 客户端接入（Cherry Studio、CC Switch 等）+ 四协议终端示例 |
| 路由管线 | 「一次请求如何被路由」：`model` 路由键 → 同名车道 → 按 `priority` 的成员链 → 命中渠道；无车道一律 `503` |
| 统计条 | `40+` 厂商适配器复用、`4` 上游协议、`2` 车道模式、`6` 车道控制键 |
| 核心功能 | 模型即路由键、优先级故障转移、冷却/亲和/熔断、口令派生管理密钥 + 客户端密钥只存哈希、单二进制 + SQLite、上游成本人民币可见、四协议双向、单管理员 |
| 上手三步 | 建渠道（`base_url` + key + 模型清单 + `model_mapping`）→ 固化车道（`POST /api/lanes/seed`）→ 客户端密钥调 `/v1/*` 并看 attempts 链与成本 |
| CTA | 部署单二进制并接入上游渠道 |

- **文案只陈述 PBR 真实能力**：不得出现上游品牌（`new-api` / `New API` / `Octopus`），不得声称多用户、计费、多节点、全球部署等本项目不具备的能力。
- 终端示例覆盖四个对外协议：`/v1/chat/completions`、`/v1/responses`、`/v1/messages`、`/v1beta/models/{model}:generateContent`；成本口径为人民币，并在响应区展示 `X-Served-By` 响应头。
- 验收：默认落地页不出现上游品牌字样；统计数字与本节一致；7 个 locale 的新增文案齐全（`literal-key-coverage`）。

### 6.1 设置口令 / 登录 `/setup`、`/login`

- **设置口令页**（仅未初始化）：口令输入（强度提示，不设硬性最小长度）+ 确认；提交后签发会话 Cookie 直接进控制台，同时弹出**一次性管理密钥**（供 AI），附复制按钮与"如何重算"说明。
- **登录页**：单输入框 + 提交（成功后进控制台）；失败按退避提示"请稍后重试"。
- **验收**：未初始化时访问任何页面都被重定向到设置口令页；初始化后该页不可再进入（409）；localStorage 中不存在管理密钥。

### 6.2 数据看板 `/dashboard`

分节（全部读 PBR `GET /api/stats`，口径 = 请求数 / 成功率 / token / 上游花费）：

| 分节 | 内容 |
|---|---|
| `/dashboard/overview` | 用量速览（上游花费 / 请求数 / 成功率 / Token，24h）+「首个 API 请求」预览卡（curl 示例 + 服务信号）+ 健康面板/API 信息。**没有起步向导、推荐操作卡、公告、FAQ、顶部通知铃铛、Uptime Kuma 面板**（个人自用；侧边栏已可直达各页，重复入口一律移除；后端 `/api/notice`、`/api/uptime/status` 与 `console_setting.announcements/faq/uptime_kuma` 一并移除） |
| `/dashboard/models` | 模型分析：按模型的请求/token/花费汇总、花费趋势与分布 |
| `/dashboard/cost` | 成本统计：按渠道/模型/车道/密钥的上游花费 |

- **删除**：`/dashboard/flow`（依赖已删的 `/api/data/flow`）与 `/dashboard/users`（多用户分析）。
- **单价**：渠道级上游单价在渠道编辑"上游单价"页签配置；**没有全局默认单价层**，渠道未配价即不折算。
- **渠道 × 模型**：成本分节提供 `group_by=channel_model` 维度，直接回答"哪个渠道、哪个模型花了多少钱、用了多少 token"（列为 渠道 / 模型 / 请求数 / token / 折算成本）。
- **验收**：概览/模型/成本三节都有真实聚合；成本卡标注"仅折算、非计费"。

### 6.3 模型管理 `/models`（模型目录）

用户的完整心智只有三步：

1. **渠道管理**：填上游（base_url + key）与它提供的模型名；
2. **路由与故障切换**（独立页 `/routes`）：定"模型1 优先打上游1、再打上游2"；
3. **令牌**：允许请求模型1。

**本页只承担"模型目录"职责**：展示模型元数据（名称、描述、标签、图标），**不承担成员链编辑**（成员链统一在 `/routes`，见下）。

规格：

- **单一平面列表**（**没有分区 Tab**）：一页列出全部模型元数据（`GET /api/console/models/**`）。列集合收敛为四列——**模型（含图标）/ 描述 / 标签 / 操作**；不再出现「渠道与分组」「同步策略」「展示策略」「匹配类型」「自定义端点」「ID」「创建/更新时间」等基座遗留列（PBR 无用户分组，`enable_groups` 恒空；多用户/计费语义已删）。
- **图标按模型名推断，且编辑时自动采用**：列表与详情渲染图标时，取值顺序为 **显式配置的 `icon` > 按模型名推断（`resolveModelProvider(model_name).icon`）> 模型名首字符兜底**。渲染侧推断是只读兜底；**编辑弹窗在模型名能识别出厂商图标时自动写入 `icon` 字段**（如 `qwen3.8-flash` → `Qwen.Color`），保存即持久化，用户**不再需要点「生效图标」**。自动采用只在用户尚未手动改过图标时发生；用户手动改过/清空后不再覆盖。
- **编辑模型走居中弹窗**（与「编辑渠道」同一规范，见 §6.9）：分区为**基本信息（模型名 / 描述 / 图标 / 标签）+ 渠道关联**；不出现"同步策略/展示策略/匹配类型/自定义端点"等与本项目无关的开关。Escape/Cancel 关闭并丢弃未保存草稿。
- **渠道关联**为只读列表：列出声明该模型的渠道（名称、类型、可用状态）并给出跳转渠道编辑的入口。
- **入口收敛**：保留「同步资料」「缺失的模型」；**删除「预填充分组」（`Prefill Groups`）入口**——模型清单不做预设分组（§6.4），`/api/prefill_group/**` 前端不再调用。
- **常驻指路提示（避免"添加模型 = 模型可用"的误解）**：页面顶部固定一条信息条，说明**本页只维护元数据，可调用性由车道决定**，并给出两个跳转链接——「渠道管理」（先把模型声明到渠道）与「路由与故障切换」（再加成员并保存）。因为"在这页添加模型"并不会让它出现在路由页，也不会让它可调用。
- **「路由与故障切换」是侧边栏独立页**（`/routes`）：集中列出全部路由键（已配车道 + 未配车道），每行展示模型名、状态（explicit 可调用 / unconfigured 不可调用）、成员数与顺序摘要；行内「编辑成员链」打开该模型的成员链弹窗（`GET /api/routes/{model}`）。
- **成员链弹窗（手工管理，居中弹窗）**：
  - 成员列表来源：**已配车道 → 车道成员**；未配车道 → 空列表（提示"添加成员后保存即固化车道"）。渠道声明只作为**候选**（`source: unconfigured` 时返回，按渠道 id 升序），由用户点「添加成员」显式加入，**不自动填入**。
  - 可**添加成员**（选渠道，默认带该渠道解析后的上游真名）、**删除成员**、**上移/下移排序**、**编辑上游真名**（清空 = 用渠道映射）。
  - 顺序即数组顺序：保存时按位置生成 `priority`（首位最大）；界面**不暴露 priority 数字输入**，也不暴露 `weighted` 权重（该模式已删除）。
  - 保存即写入 PBR 车道（名称 = 模型名，模式 `failover`）；`PUT /api/lanes/{model}`，成员 `{channel, upstream_model, priority}`。
  - 保存时的 `config` 必须取 **`GET /api/system/options` 的 `lane_defaults`**，并保留该车道自身已有的六键；不得在前端写死六键——否则用界面改一次成员顺序就会把该车道自定义过的超时/冷却/亲和静默重置为前端硬编码值。
  - 空成员链保存被拦截（提示"至少一个成员，或删除该车道"）。
- **一键固化**：提供"为所有未配车道的模型生成车道"按钮（`POST /api/lanes/seed`，按渠道 id 升序生成初始顺序；先 dry-run 预览）。
- **验收**：模型页只有四列且无「渠道与分组」；未配图标的已知厂商模型（如 `glm-5.2`、`deepseek-v4-flash`、`bge-m3`）显示推断图标，未知模型回退首字符；编辑模型打开居中弹窗。为"模型1"添加"上游1 → 上游2"后，`GET /api/routes/模型1` 顺序一致；上游1 故障时请求逃逸到上游2；添加/删除/排序后的保存结果与后端 priority 降序一致；**未配车道的模型请求返回 503**，配好后立即可用。

### 6.4 渠道管理 `/channels`（页面与菜单标题均为「渠道管理」）

- **协议选择器（取代"厂商类型"下拉）**：渠道编辑器在「基本信息」分区只让用户选**上游协议**，共 4 项，**默认 OpenAI 兼容**：

  | 协议 | 上游端点 | 适配器 type | `other_settings.protocol` |
  |---|---|---|---|
  | OpenAI 兼容（默认） | `/v1/chat/completions` | 1 | `openai-chat` |
  | OpenAI Responses | `/v1/responses` | 1 | `openai-responses` |
  | Anthropic | `/v1/messages` | 14 | `anthropic` |
  | Gemini | `/v1beta/models/{model}:generateContent` | 24 | `gemini` |

  - 选中协议同时写渠道 `type`（1/14/24，决定适配器）与渠道级 `other_settings.protocol`（决定 base_url 自动补全的路径与该渠道的默认端点；**入站请求路径仍可覆盖**——OpenAI 兼容渠道收到 `/v1/responses` 时照常走 Responses）。
  - **不再按厂商分类**：控制台不暴露约 50 个厂商类型。**兼容旧数据**：编辑一个类型不在 1/14/24 的既有渠道时，把该渠道当前类型作为"当前值"选项保留在下拉里，保存不改动它（避免静默改写）。
- **API 地址填"版本段"或"完整端点"都行（全部协议型渠道）**：base_url 允许是以下任一种写法，网关会先归一化成"版本根 + 自定义前缀"再拼路径：
  `https://host` ≡ `https://host/v1` ≡ `https://host/v1/chat/completions`（也认 `/v1/responses[/compact]`、`/v1/messages`、`/v1/completions`、`/v1/embeddings`）。
  **为什么必须支持完整端点**：很多第三方中转只提供一个完整 chat 地址，用户会整条粘进来；旧实现会拼成 `…/v1/chat/completions/v1/chat/completions`，**转发与"探测上游模型"都会 404**。归一化后转发与探测都打到 `{host}/v1/chat/completions` 与 `{host}/v1/models`。旧数据（只填 host 或 /v1）行为不变。
  协议型渠道：OpenAI/Anthropic 到 `/v1`，Gemini 到 `/v1beta`。Custom（type 8）继续原样保留 base_url（支持 `{model}` 变量与完整端点）。
- **探测上游模型对 Custom 渠道也有兜底**：Custom(8) 若**没有配置** `/v1/models` 路由（`advanced_routes` 里没有该 incoming_path），旧逻辑直接报错、探测永远不可用；现在按 OpenAI 兼容约定从 base_url 推导模型清单地址（剥掉完整端点后 → `{base}/models` 或 `{base}/v1/models`）。配置了路由的仍优先走路由。
- **模型手动添加支持批量**：输入框可用逗号/顿号/空白分隔多个模型名一次加入；**换行分隔的一列可直接粘贴**（单行 input 会丢换行，由 onPaste 读剪贴板原文整批加入）。探测块去掉了"框套框"，改为单层卡片。
- 沿用上游 `channels`：卡片列表 + 表单（**协议**、**API 地址**、key（只写不读）、模型清单、**模型映射**、**上游单价**、参数覆盖 JSON、代理、启用）+ 探活 + 批量操作 + 标签。**渠道表单没有优先级与权重**（两字段已随路由收敛物理删除），**也没有分组**（列表、卡片、移动端、表单、筛选一律不出现；`group` 仅由提交适配层固定注入 `default` 以兼容基座结构，不在界面建模）。
- **列表没有厂商类型维度**：厂商 `type` 已降为内部适配器细节，用户面只区分协议。因此**移除列表的「类型」列与工具栏「全部类型」筛选**（搜索参数 `type` 一并移除）。原「类型」列承载的**多密钥轮询模式标记移到渠道名单元格**，信号不丢；旧渠道若类型不在 4 协议内，其适配器细节只在编辑弹窗的「协议」下拉以 `(Current)` 保留，列表不再展示。
- **创建/编辑走居中弹窗**（`ChannelMutateDialog`）：打开即表单可编辑，**没有厂商市场选择器、没有两步向导**。协议用**下拉**选择（4 项，见上表）；创建模式下名称为空时自动填协议名。Escape/Cancel 直接关闭弹窗并丢弃未保存草稿。**弹窗只有一层滚动**（Tab 分区内容滚动、Tab 栏钉住），不出现双层滚动条。
- **模型清单的填充只靠上游探测**（见下，手动按钮触发）：模型区**没有预设分组、「Fill Related Models」等快捷填充**（`/api/prefill_group` 前端不再调用），只保留「Copy All / Clear All / 手动添加单个模型」工具。
- **分区结构**：弹窗四个页签——**连接与模型 / 路由与映射 / 上游单价 / 其他设置**（请求处理、参数覆盖、字段透传、代理、检测与备注并入「其他设置」）。
- **上游单价的落点**：独立「上游单价」页签。**计价键是请求模型名（路由键/车道名）**，不是上游真名；表格默认跟随渠道模型清单，**并支持手动添加清单外的自定义计价行**（如车道成员引用的上游名、独立车道名），自定义行可删除。**计价表初始为空，行全部按需手动添加**：可搜索下拉列出渠道模型清单中尚未添加的模型，也允许键入自定义名（独立车道名等）；每行可删除。**单位：元 / 1M tokens（人民币），UI 明示；行内留空按 0 计，未添加行的模型不折算（免费）**。只有至少填了一项的行才写入 `pbr_prices`；全空行不落库。
- **数据面**：渠道页走基座 `/api/channel/**`（等价能力的运维面，PBRAuth 保护）；契约面 `GET/PUT /api/channels/{name}` 面向 AI/脚本。两侧读写同一张表，字段语义（`models` 数组 vs 逗号串、`status` vs `enabled`）由各自适配层转换。
- **模型清单（手动探测 + 居中选择弹窗）**：
  - **常态只显示"已选模型"列表**（每行可删）+「手动添加模型」单值输入 +「Copy All / Clear All」。**没有常驻的多选下拉**：输入框**不得**在聚焦/输入时弹出全量模型候选（那是基座遗留，候选会包含其他渠道声明过的无关模型）。
  - 探测入口**只在渠道编辑器的模型区**：连接信息填好后点**「探测上游模型」按钮**（手动触发，不自动拉取）调用上游 `/models`；成功后**打开居中弹窗**（§6.9，`size=lg/xl`），候选以勾选列表呈现（"已从上游发现 N 个 · 新增 M · 已存在 K"），**按需勾选**，另有「全部加入 / 仅加入新增」；点确认才回填模型清单，取消不写入。探测后连接信息再变更会标记过期并提示重新拉取。
  - 未保存的渠道用预览请求探测，已保存渠道用 `POST /api/channels/{name}/sync-models`。**渠道列表行不提供「Fetch Models」入口**。
  - 探测失败只给内联可重试提示，不阻断保存，**不覆盖用户手填的模型**。
- **模型映射**（`model_mapping`）：两列表格"路由键 → 上游真名"，用于上游命名与路由键不一致；车道成员默认用它解析上游名，成员级 `upstream_model` 可覆盖。
- **验收**：列表与详情只显示 `key_prefix`；表单不出现优先级/权重/分组；协议下拉只有 4 项且默认 OpenAI 兼容；base_url 填 `https://host/v1` 与填 `https://host` 最终都打到 `https://host/v1/chat/completions`（Anthropic/Gemini 同理），不出现 `/v1/v1`；编辑旧厂商类型渠道时其类型被保留、不被静默改写；新增模型名后立即出现在模型管理页（未固化时标注不可调用）；配好映射后车道成员的上游真名随之变化；删除被车道引用的渠道返回 409 并给出引用清单。
- **注意**：上游余额查询端点（`/api/channel/update_balance[/:id]`）与手动刷新/「Query Balance」入口已随计费语义一并删除；渠道列表仍**只读展示**上游返回的 `balance` 字段作运维参考——PBR 已移除"余额≤0 自动禁用渠道"逻辑。

### 6.5 令牌 `/keys`

- 沿用上游令牌面板：创建、启用停用、编辑、删除；用量卡（请求数/token/折算成本/最后使用）。
- **消耗列（取代上游"额度/剩余/已用"）**：列表的金额列显示该令牌的**消耗 = 上游折算花费**（元），数据来自 `GET /api/stats?group_by=key` 的 `SUM(cost_sum)`，与看板/日志同源；**不读明细**，故 `logs/prune` 后消耗不消失。无数据时为 `0`。
  - **不出现额度语义**：没有"额度/剩余/总额/无限制额度"、没有进度条与百分比、没有"钱包余额/订阅额度"提示语。PBR 无计费、无钱包、无订阅、无 `unlimited_quota`（design-v1 §1.3、token-spec §5）。
  - 令牌创建/编辑表单**不含额度字段**（上游 `remain_quota_dollars`/`unlimited_quota` 一并移除）。
- 权限（PBR 语义）：默认"**允许全部模型**"，可切"仅允许指定模型"并叠加拒绝；**判定对象是路由键（模型名）**。
- **验收**：明文只在创建/轮换时出现一次；消耗列金额等于 `GET /api/stats?group_by=key` 中该 `key_name` 的 `estimated_cost` 合计，且明细 prune 后不变；表单与列表无额度/钱包/订阅字样；权限候选来自 `GET /api/models`（不是显式车道），且支持手填。

### 6.6 请求日志 `/logs`、`/logs/$id`

- 保留上游 `usage-logs` 外壳：**Common 与 PBR 两个分节**（Common 数据源为基座 `/api/log/**`；PBR 分节走 `GET /api/logs`）。Task 分节随任务插件子系统移除，**Drawing 分节随 Midjourney 全链路移除**。
- **PBR 分节的数据源是 `GET /api/logs`**：筛选车道 / 渠道 / 令牌 / 请求模型 / 成功与否 + 游标翻页；列表展示时间、车道、渠道、上游真名、密钥名、结果、attempts 摘要、总耗时、折算成本。
- **详情**：`GET /api/logs/{id}` 的 `attempts` 逐尝试时间线（成员、状态、`duration_ms`、`error_kind`、`msg`），区分 `cooldown`/`circuit_break`/`skipped` 状态色；另展示 `lane`/`route_source`/`upstream_model`/`http_status`/token 用量/`total_ms`/`estimated_cost`。
- **验收**：一次含逃逸的请求能完整复现 `failed → success`；被跳过的成员有原因说明。

### 6.7 系统设置 `/settings`

设置区为**四页 registry 结构**（`web/src/features/system-settings/{site,models,operations,content}`，`/system-settings/<page>/<section>` 子路径路由 + 左侧分节导航）。各页分节以代码里的 `section-registry.tsx` 实注册为准：

| 页面 | 分节（实注册） |
|---|---|
| 站点 `/system-settings/site` | 站点信息（名称/Logo/页脚/关于/首页内容/服务器地址/法律页）、顶部导航、侧边栏模块 |
| 模型 `/system-settings/models` | 全局模型配置、路由可靠性（重试与自动重试状态码、定时渠道测试与自动禁用/恢复开关、失败阈值、失败关键词表）、默认六键（`lane_defaults`，`GET/PUT /api/system/options`）、Gemini、Claude、Grok、渠道亲和 |
| 运维 `/system-settings/operations` | 系统行为、监控与告警、SMTP 邮件、Worker 代理、日志维护（保留天数/手动清理）、性能 |
| 内容 `/system-settings/content` | API 地址、对话预设 |

- **待实现**（后端 API 已存在、前端未接线，接入前不得伪造入口）：**账户**——修改登录口令（说明会改变管理密钥与会话；`PBR_ADMIN_KEY(S)` 生效时接口 409，需禁用表单并说明）、**备份**——导出配置 / 导入（含 dry-run diff，api-spec §5.6）、**Webhook 通知**——多目标编辑 + 「发送测试」+ 最近投递记录（api-spec §5.8）。
- **删除**：支付设置、合规、OAuth/多用户相关子节；**「系统信息」独立页**（原 `features/system-info`）连同其多节点「实例 / Role / 节点」面板、菜单项、`/api/system-info/**` 与 `system_instances` 表一并物理删除（单节点 SQLite 部署不需要多实例视图）。
- **验收**：关键词增删回读一致；默认六键保存后回读一致，且新建/一键固化车道采用该值；"自动恢复"开关默认开启，页面必须写明：该开关只是允许写回 `enabled`，真正复通依赖"自动巡检"（默认关闭）或人工"测试全部渠道"；访问 `/system-info` 与 `/api/system-info/**` 一律 404。

### 6.8 试打台 `/playground`

沿用上游 `playground`：模型选择 + 多轮消息 + 流式开关 + 思考参数（`enable_thinking`/`reasoning_effort` 成对）+ 温度/最大 token；展示流式输出、TTFT、用量与响应头 `X-Served-By`。

- **数据面 = 模型面**：请求直连 `POST /v1/chat/completions`（流式/非流式同路径），鉴权用**使用者填入的客户端密钥**（`Authorization: Bearer pbr-...`），密钥存 `localStorage` 的 `pbr_playground_client_key`；**不得**用会话 Cookie 或合成 token 打模型面，也**不得**打已删除的 `/pg/chat/completions`。
- 模型下拉的候选来自 `GET /api/models`；密钥缺失/无效时给出"填入或去令牌页创建"的引导（401/403 分别提示）。
- **验收**：填入有效客户端密钥后能完成一次流式对话，页面显示 `X-Served-By` 与实际 TTFT/用量。

### 6.9 居中弹窗统一规范（**强制**）

所有"居中弹窗"（`@/components/dialog` 的 `Dialog`，区别于右下 `Sheet` 抽屉）必须遵循同一套规范；禁止在 feature 内各写一套尺寸与滚动策略（web/AGENTS §3.3「复用到行为层」）。

- **尺寸分档（`size` 属性，默认 `md`）**：弹窗**外框尺寸恒定**，不随内容长度、页签切换或列表增删而变化；内容超出在**内容区内部滚动**。

  | 档 | 宽度 | 高度 | 用途 |
  |---|---|---|---|
  | `sm` | `min(92vw, 480px)` | 高度自适应、设 `min-height` 下限，不变高跳动 | 确认 / 告警 |
  | `md` | `min(92vw, 720px)` | `min(78vh, 560px)` | 一般表单 / 简单选择 |
  | `lg` | `min(94vw, 960px)` | `min(82vh, 640px)` | 编辑渠道 / 编辑模型 / 成员链 |
  | `xl` | `min(96vw, 1200px)` | `min(86vh, 720px)` | 探测选择 / 多密钥 / 参数覆盖 |

- **结构**：header（标题 + 说明）与 footer（操作）`flex-shrink-0` 钉住；正文容器 `flex-1 min-h-0 overflow-y-auto`，**只有一层滚动**，不得出现双层滚动条。
- **切换页签/增删行不得改变外框**：Tab 内容区高度由外框决定，而非由内容撑开。
- **交互**：Escape / Cancel 关闭并**丢弃未保存草稿**；危险操作走 `ConfirmDialog`（`sm` 档）。
- **例外**：命令面板（`Command`，`top-1/3` 定位）不套用居中分档；`AlertDialog` 沿用 `sm` 档的固定小尺寸。
- **验收**：同一弹窗在空数据、单项、多项、超长文本下外框尺寸不变；正文可滚动且 header/footer 始终可见；移动端 ≤640px 不出现横向溢出与双层滚动。

### 6.10 系统任务 `/system-tasks`

- **侧边栏独立页**，替换原「系统信息」入口（`system-info`）。
- 内容 = 原 `system-info` 页里的任务面板：**活跃任务 / 历史任务**两节，展示类型、状态、进度、执行者、更新时间、详情；活跃任务存在时每 8s 自动刷新，并提供手动「刷新」。
- **数据源**：`GET /api/system-task/list`（控制台内部接口，api-spec §9）。
- 页面标题「系统任务」，**不出现 "Root" 徽标与角色门**（原页的 `role !== SUPER_ADMIN` 守卫是多用户遗留；PBR 单用户，管理面即全量权限）。
- **验收**：菜单可进入 `/system-tasks`；有活跃任务时自动刷新、无则暂停；日志清理任务可作为一条记录出现。

---

## 7. 构建与集成

- 构建 `pnpm build`（或 `bun run build`，Rsbuild）→ 产物交 Go `embed`，单二进制同时服务 `/v1/*`、`/api/*` 与控制台静态资源；SPA 路由回退 `index.html`。
- 开发：上游 `dev` 经代理转发 `/api`、`/v1` 到本地 `pbr`。
- i18n：新增文案必须同时补**全部 7 个 locale**（`zh` / `en` / `zh-TW` / `ja` / `fr` / `ru` / `vi`）；禁止硬编码中文到组件。改完运行 `pnpm i18n:sync` 归一化并确认无缺失/未翻译。**漏键会让 i18next 回退英文**，中文界面出现英文残留即视为缺陷。
- 品牌：构建时注入应用名与版本，供标题/关于页/`/version` 使用，避免散落硬编码。

---

## 8. 控制台验收总纲

1. 全部保留页面可加载、可操作、无 console 报错；`pnpm build` 零报错。
2. 每个写操作刷新后可从 API 回读到一致结果。
3. **模型管理的成员链（故障切换）实测**：设定"优先上游1 → 上游2"后，routes 顺序一致、上游1 故障时逃逸。
4. 无任何第三方品牌残留；无计费/多用户残留入口（菜单、路由、i18n 全清）。
5. 加载/空/错误三态齐备；错误态展示稳定 `error.code`。
6. 页面数据全部来自 api-spec 端点；缺端点先补契约。
7. **中文界面无英文残留**：所有 `t('...')` 字面量在 `zh.json` 有对应键；`pnpm i18n:sync` 的缺失扫描为 0。
8. **弹窗规范**：所有居中弹窗按 §6.9 分档尺寸渲染，外框尺寸不随内容/页签变化，正文单层滚动。
9. **无遗留语义**：控制台不出现"分组/额度/余额/钱包/订阅/Root/实例(多节点)"等基座残留字样与入口；"`priority` 数字输入"不出现。
