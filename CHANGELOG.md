# 更新日志

本文件记录公开版本的重要变更，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；本项目遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Added
- **渠道退役编排**（design-v1 §7.9）：`POST /api/channels/{name}/retire` 把"停用 → 解除引用 → 删除 → 历史标记"做成一个显式编排——控制台"删除"入口改为两步确认（先预览引用/清理/历史改写清单，再输入渠道全名解锁），确认后系统替运维摘除散成员与成员组条目（成员清空的车道整条删除，**组本身保留**并把变空组单独列出），历史请求记录与 `channel`/`channel_model` 统计改写为 `原名（已退役 时间）`，避免同名新渠道与退役历史混在一起。API 的 `DELETE /api/channels/{name}` 严格语义不变（仍被引用即 409）。退役是**唯一允许变更车道/成员组的渠道动作**，必须预览 + 二次确认 + 逐项审计。
- **脱敏推荐基线**：新增 `GET /api/privacy/recommended`（`privacy.Recommended()`），把「打开开关后应该长什么样」变成后端唯一真源——`kinds` 按 magpie 类别默认（六项高置信度默认勾选）、`actions` 空（全 `mask`）、`min_score=0.80`；**三个总开关恒为 false**，脱敏仍须手动开启。控制台脱敏页新增「应用推荐配置」，只重置检测口径、不动总开关，并在未启用时给出显式提示。
- **Anthropic Files 网关侧存储**（ADR 0011）：`anthropic-beta: files-api-*` 的 `/v1/files*` 由网关暂存字节，不要求 `X-PBR-Lane`、不参与选路、不转发上游；`/v1/messages` 引用 `file_id` 时展开为内联 base64。单文件 64 MiB、总量 1 GiB、按 `expires_after` 过期，归属绑定客户端密钥，进 `pbr_files` 表 + `pbr-files` 目录，导出不含字节。与 ADR 0010 的 OpenAI Files 语义按 beta 头分流并存。
- **OpenAI Responses 入站**：`POST /v1/responses`（流式/非流式）与 chat 共用车道语义；`/v1/messages/count_tokens` 恢复。
- **Files / Batch 异步对象**（ADR 0010）：`X-PBR-Lane` 显式寻址、创建即固定单渠道、流式代理与 512 MiB 上限、跨车道隔离、元数据保留与清理；不参与故障转移。
- **可观测性**：`GET /metrics`（Prometheus，管理密钥 Bearer，含请求/耗时/首字/token/成本/上游错误/熔断/渠道可用）；请求日志新增 `price_band`。
- **导出**：`GET /api/logs/export`（CSV/NDJSON，≤31 天/10 万行）；`GET /api/audit|channels|keys/export`（CSV，不含任何明文密钥）。
- **峰谷计价**：渠道单价支持 `timezone` + 有序 `bands`（跨午夜、兜底、重叠先命中），`stats/rebuild` 按 `ts` 重算。
- **控制台**：日志导出与实时 tail、命令面板实体搜索、渠道/密钥/审计导出入口、空态 CTA 与错误态重试、统一视觉 token/页面壳/图表；新增 Playwright L3-E2E。
- **工程守卫**：routeTree import 与 `src` gitignore 检查前置到 typecheck；6 个超大文件拆分；删除无引用组件与未用依赖。
- 单二进制 + SQLite 的单用户 LLM 聚合网关：渠道声明模型、**车道为唯一路由入口**（ADR 0005）。
- 上游协议选择器：OpenAI 兼容 / OpenAI Responses / Anthropic / Gemini 四种协议；API 地址填到 `/v1`（或 `/v1beta`）即可，网关自动补全端点路径。
- 故障转移与容错：成员顺序故障转移、冷却、亲和、熔断三态与半开复通、429 与硬故障分类。
- 管理面完整 HTTP API + OpenAPI：`/api/v1/{channels,lanes,routes,keys,logs,stats,export,import,system/options}`。
- 控制台：渠道管理、模型管理（按名推断并自动采用图标）、路由与故障切换、系统任务、令牌、用量与成本看板、系统设置。
- 上游模型探测：渠道编辑器内手动探测（**唯一入口**），探测结果用居中选择弹窗按需合并；已保存渠道复用服务端已存密钥。
- 成本视图：按渠道 x 模型的请求数、token、成功率与上游折算花费（元）。
- **车道成员自由编排**（ADR 0006）：成员可任选任意启用渠道的任意已声明模型，可跨渠道跨模型、无需同名、同一渠道可多次出现；`/routes` 以卡片网格展示车道、运行态与成员顺序。
- **模型调用分析**：按模型的调用次数 / 成功率 / Token / 上游花费汇总，默认按调用次数倒序；成本统计按上游花费倒序。
- CLI：`pbr auth reset`。

### Changed
- **侧边栏 IA**：数据概览升为一级入口，固定排在实时路由之后（第二位）；「用量」分组只保留 请求/会话/上下文。脱敏从「用量」旁边移到系统区（它是实例级配置，不是日志视图）。
- **实时路由页不再有时间区间与问题筛选**：拓扑与渠道速度都是实时量，车道统计固定最近 24 小时；旧链接里的 `range`/`from`/`to`/`window` 被丢弃。
- **渠道速度只显示实时值**：固定 30s 窗口（每 5s 刷新），移除 `30s/5m/15m/1h` 档位；「实时流量」仍固定近 15 分钟，卡片上分别标注口径。
- **流内错误帧识别覆盖全部入站格式**：OpenAI chat/Responses、Anthropic、Gemini 共用同一探测器；提交前不转发并换成员，提交后透传并记 `upstream_reject`。
- **导入事务全部走 tx 查询**：修复 SQLite 单连接（生产默认）下 `POST /api/import` 自锁的既有缺陷。
- **渠道价格页签**改为周视图 + 当前生效价 + 批量粘贴导入；旧单层价自动视为全天兜底，无需迁移。
- **控制台页面壳收敛**为唯一 `ConsolePage`；`SectionPageLayout` 删除。
- **渠道名唯一性成为硬约束**：渠道是管理面与车道成员的按名寻址对象；创建、改名、复制、批量添加与导入遇到重名一律 `409 conflict`，历史重名在按名寻址时返回 `409 channel_name_ambiguous`，不再静默取第一条。启动时扫描历史重名并在日志列出；数据清理干净后自动建立唯一索引。
- **熔断恢复增加半开成功次数**：半开探测需连续成功 `circuit_half_open_successes`（默认 2）次才闭合，避免上游抖动导致"恢复—再熔断"循环。
- **webhook 告警降噪**：`cooldown` / `circuit_half_open` / `skip` 仅保留在 health/events 排障面，不再推送；`circuit_open` 按成员做 30 分钟滞回，`circuit_closed` 只对曾被通知的打开事件发送。
- **连续上游拒绝的短期回避**：同一成员连续 `upstream_reject` 达 5 次后进入 300 秒短期冷却（`detail` 标注 `reason=consecutive_rejects`），避免每请求重复白打一个必然拒绝的成员；成功一次即清零。
- 控制台：车道成员目录按渠道名聚合重复项并显式提示重名，不再对同名渠道静默取第一条而误报"未声明模型"。
- **渠道状态并入渠道管理**：侧边栏只保留「渠道管理」一个入口，标题栏「渠道状态」按钮在同页切换状态视图；旧 `/channel-status` 路径重定向到 `/channels`。
- **渠道编辑器改为单 key**：移除批量添加、多 key 策略与追加/替换模式；key 输入框右侧新增「小眼睛」，按需读取并显示服务端已存 key；保存新 key 即整体替换。
- **渠道名唯一（表单即时校验）**：保存前精确查重（编辑时排除自身），重名直接在名称字段报错并阻止提交；后端 409 继续兜底。
- **渠道列表内联运行状态**：渠道表新增「运行状态（24h）」列（可用成员/总数、成功率、最近错误分类），点击打开状态抽屉；工具栏新增「运行状态」筛选。
- **渠道状态回归侧边栏并改为紧凑卡片**：`/channel-status` 恢复为独立菜单；一个卡片对应一个「渠道 × 上游模型」，显示模型图标/名称/渠道/复制、OK% 分段条、LAT、TPS；渠道管理页移除状态列与状态视图按钮。
- **试打台模型选择**：模型下拉与客户端密钥并列到输入框上方；候选只保留可路由且具备 chat 能力的路由键，过滤 embedding / rerank / 语音 / 图像模型，避免默认选中 `bge-m3` 后收到 “does not support chat”。
- **移除侧边抽屉**：渠道状态改为桌面端行内展开、移动端居中弹窗；主题设置、API Key 编辑、渠道测试失败详情、试打台移动端参数面板统一改为居中弹窗。
- **渠道状态页新增「实时路由」**（magpie 风格）：车道 → 网关 → 成员的可视化拓扑，
  运行态来自 SSE（30s 轮询兜底），逐成员显示当前成员、就绪、限流/冷却/熔断倒计时、
  半开、探活、人工停用与渠道停用；底部展示首选成员、车道窗口计数与最近一次请求的
  attempts 链；支持 `?lane=` 深链。原汇总指标改为扁平条，渠道明细表与展开模型状态保留。
- **实时路由迁入概览，概览重排**：概览改为「时间区间 + 指标条 → 实时路由（全宽）→
  模型数据分析 / 服务状态 → 全宽统计表」；`/overview?lane=` 承接车道深链。Token
  排行榜并入统计表的「按 Token」排序，不再单独占卡；服务状态的吞吐量并入延迟列，
  只显示干净样本（成功且单次尝试）TPS，缺失时标注「无干净样本 / 无输出 Token」，
  不渲染孤立 `-`。

### Changed
- **服务商硬性请求头由网关自动补齐**：新增 `relay/channel/provider_quirks.go`，按上游 host + 路径前缀内置已知要求（目前是 OpenCode Zen/Go 的 `x-opencode-session`），组装请求时自动补齐；客户端带了就转发原值，渠道显式 `header_override` 仍优先。现有渠道和 API 创建的渠道都自动生效，用户不必再为某个服务商手写 `param_override`。渠道新建页同时内置 15 个服务商预设（地址/协议/名称一键填好，只需填 key）。
- **控制台信息架构重写为 5+1**：概览 / 路由 / 连接 / API 密钥 / 日志 + 设置；渠道状态、模型管理、系统任务、审计日志退出侧边栏，分别并入「连接」或「日志」工作区；Playground 改为顶栏全局动作。旧路径（`/dashboard`、`/channels`、`/channel-status`、`/models`、`/usage-logs`、`/system-tasks`）在路由匹配前 302 到新路径并保留 search/hash。
- **渠道状态与模型目录拆为左侧独立菜单**：控制台入口由 5 项变为 7 项（概览 / 路由 / 渠道 / 渠道状态 / 模型目录 / API 密钥 / 日志 + 设置）；渠道三页不再显示「连接」工作区页签，`/connections/*` 路径和旧链接保持不变；渠道 / 渠道状态按 `admin.channel`、模型目录按 `admin.models` 独立显隐。
- **渠道状态页重做并恢复可发现性**：连接页签由含糊的 "Health" 改为「渠道状态 / Channel status」；页面从「模型卡片墙」改为「汇总指标 → 渠道表 → 展开模型明细」，先回答渠道是否可用，再下钻到具体上游模型；渠道表按不可用/降级/服务中/已暂停/未接入路由排序，展开行复用模型状态表；移动端切换为卡片，不横向滚动。
- **移动端三处信息密度收口**：路由页的成员组面板改为可折叠区（默认收起，车道卡片先出现）；概览的六个时间档位改为下拉选择器；渠道列表的搜索/模型/协议/状态/标签筛选收进「Filters」右侧 Sheet，工具栏只保留一行快捷动作，已选筛选数显示在按钮上。
- **主编辑面改为右侧 Sheet**：渠道、API 密钥、路由编排器、共享链编辑器不再使用居中大弹窗；标题/描述/页签/操作/内容统一走 `ConsolePage`。
- **状态词汇统一**：手工停用改为「已暂停（Paused）」，自动停用为「自动暂停」；不再把人工动作画成故障红。
- **表格与移动端可读性**：Keys/Audit 表格取消逐字换行、固定关键列宽；概览 KPI 手机两列；日志筛选栏单行化；系统任务移动端改卡片。

### Fixed
- **Anthropic 入站首字恒为空**：`/v1/messages` 的流式主出口 `ClaudeData` 写出 `data:` 帧却没打提交标记，导致首字耗时（`ttft_ms`）永远为 0、控制台显示"不适用"；同一缺陷还让提交后收到的流内 error 帧被误判成"提交前"。补齐该出口与 zhipu/xunfei/cohere/palm 四个 legacy 适配器的提交标记。
- **流式空响应被记成成功**：上游建流后零有效帧（首事件超时、立即 EOF、只有 `[DONE]`）时，各 handler 把末帧解析失败只写日志就吞掉，最终落库 `success=true` + `completion=0`，客户端拿到 200 空流。现按 `bad_response`/`empty_response` 判成员级失败，覆盖 6 个流式 handler。
- 修复 `web/.gitignore` 的 `logs` 规则吞掉 `src/routes/_authenticated/logs/`，导致日志工作区路由从未入库、源码重建后整块消失的问题（已补 routeTree import 守卫）。
- **manual 车道点名成员不再悬空**：渠道/模型清理、成员组编辑/删除、孤儿清理、上游模型应用、渠道改名都会同步重写或清空失效的 `active_member`；空 pin 进入“待点名”状态并在路由卡片/编排器显著告警，运行期健康计数不再把 manual 车道未点名的成员算作可用。
- **渠道模型清理与渠道写入同事务**：`PUT /channels`、`PUT /api/channel`、`sync-models`、上游 apply、模型目录删除不再先提交渠道再清理车道；`?force=1` 与成员组硬拦语义统一，模型目录删除的 force 不再是空操作。
- **成员组冲突详情完整返回**：基座/稳定面响应保留 `groups`/`blocked_groups`，控制台不再把组名当车道名并反复弹出无效的“删除车道”确认；`models_referenced_by_groups` 正确映射为 409。
- **上游更新弹窗按渠道重置**：关闭后重新打开另一个渠道不再继承上一个渠道的勾选/搜索/Tab 状态，避免把 A 的选择应用到 B。
- **旧版渠道更新 `models` 省略即清空**：只有显式提交 `models` 才修改模型清单；显式空数组仍可清空。partial lane PUT 也不会再把 `manual`/六键/启停重置为默认值。
- **新增 manual 车道会覆盖同名既有车道**：新建页先查现有关键字并阻止保存；车道编辑器请求失败不再渲染空 composer；成员组编辑器增加未保存离开确认。
- **清理结果与缓存一致性**：渠道、模型、车道、成员组变更后同时失效渠道状态、路由、编排器目录与组引用缓存；清理成功提示补充 manual 车道名单；批量启停按实际变更数报告。
- **第二轮接口一致性修复**：`sync-models` 复用渠道编辑器的厂商 URL/代理/请求头/模型发现逻辑；legacy 渠道更新省略 `models` 不再清空清单；partial lane PUT 不再重置 mode/config/enabled；`InitChannelCache` 读库失败不再清空线上缓存；`GET /routes/{model}` 补回 `active_member`；渠道状态 `available_count` 排除停用渠道、引用清单补上成员组与停用车道；渠道名禁止 `/`；channel name 歧义在 ops 端点返回 409 而非 500；batch copy 的 body `suffix`/`reset_balance` 现在生效。
- **并发与命名空间**：渠道模型收窄在写事务内重查引用，新增引用不会再被静默 force 清理；单删/批量删除/停用渠道清理均在事务内重查引用；`GetLaneByAlias` 遇到历史重复别名返回歧义错误而非任取第一条；散成员/组内成员别名与车道名增加双向冲突校验。
- **原子操作补强**：`CleanupOrphanMembers` 改为单事务（散成员、组内孤儿、空组、空车道一起提交/回滚）；系统选项批量写入改走 `UpdateOptionsBulk` 单事务；导入真实执行前先跑与 dry-run 相同的完整校验，配置错误不再半写；渠道批量删除/停用清理也在事务内重查引用。
- **别名库级约束**：SQLite 下为 `lane_members.public_alias` 与 `member_group_items.public_alias` 建立部分唯一索引（空别名不参与）；有历史重复时只告警、不阻塞启动。
- **车道导入三态**：`LaneConfig` 的 `enabled` / `mode` / `config` 改为指针，字段缺席时保持原值；`members` 缺席保留现有链。
- **导入字段三态与单事务**：渠道 `base_url` / `proxy` / `enabled`、车道 `enabled` / `mode` / `config` 改为指针三态；渠道/成员组/车道/密钥/系统选项的全部写入现在在同一个 DB 事务内提交，运行时 OptionMap 在提交后统一刷新；导入收窄模型时拦截 bundle 外引用。
- **最后一批架构项**：SQLite 成员别名部分唯一索引；单删/批量删/停用渠道清理事务内重查引用；`CleanupOrphanMembers` 单事务；`sync-upstream` dry-run 复用源版本与选择存在性校验；options dry-run 复用通用选项值校验；渠道编辑器未保存确认。
- **批量与删除明细**：`apply-all` 的 dry-run 返回逐渠道 blocked/lanes/groups，并支持 `force` 清理散成员；by-tag dry-run 也跑引用守卫；模型目录删除的冲突明细带 `models`，cleaned/deleted 结果去重；渠道状态引用清单包含停用车道。
- **导入与 dry-run**：渠道导入的 `base_url`/`proxy`/`enabled` 改为三态（缺席保持原值）；导入收窄模型时会拦截 bundle 外仍被引用的车道/成员组；`apply-all` dry-run 增加逐渠道 blocked/lanes/groups，并支持 force 清理散成员；by-tag dry-run 也跑引用守卫。
- **渠道编辑草稿**：关闭有未保存修改的渠道编辑器前先确认；模型目录删除的 cleaned/deleted 结果去重。
- **导入保护**：导入车道会校验 `active_member`，导入后统一清理悬空点名；成员组/车道空成员的导入仍按既有语义处理。
- **移动端细节**：标签聚合行不再渲染不可选复选框；成员组/车道成员操作按钮放大触控面积；新建车道会先查重，避免覆盖同名既有车道；成员组编辑器增加未保存离开确认。
- **请求日志落库 `usage_semantic`**：此前只在算价时使用、未写入 `pbr_request_logs`，日志里恒为空；现在按上游语义写入 `openai` / `anthropic` / `gemini`，保证以后接入非 OpenAI 上游时缓存折算口径可追溯、可复算。
- **成员组引用纳入渠道/模型删除守卫**（ADR 0009）：删除渠道、收窄渠道 `models`、上游同步移除模型、模型目录删除现在同时检查 `lane_members` 与 `member_group_items`；错误明细同时返回 `lanes` 与 `groups`，不再出现"渠道只在成员组里被使用却被直接删掉"。
- **模型移除按成员所选模型判定**：不再只按车道名匹配；`?force=1` 只清理本次被移除模型的散成员，不再误删同一渠道在车道里的其他模型；成员组引用一律要求先编辑组。
- **清理悬空成员不再误删组引用**：`channel_id=0` 的组引用行不再被当成悬空渠道成员；组内孤儿会一并清理，组被清空则整组删除并级联清理空车道。
- **成员组编辑弹窗 footer 固定**：去掉外层 `overflow-y-auto`，保存/取消按钮不再随内容滚出弹窗。
- 修复同名渠道导致车道成员保存被静默绑定到错误渠道（按名 `.First()`）的问题。
- 控制台首页重做为 PBR 真实能力：模型即路由键、同名车道、优先级故障转移、冷却 / 亲和 / 熔断；新增「一次请求如何被路由」区块；统计口径修正为 **4** 个上游协议。
- 控制台图标换为新的闪电电源徽标（AI 生成母版 + 可复现生成脚本）。
- **管理面响应信封统一**（api-spec §2/§3）：成功一律裸资源（列表 `{items,next_cursor}`、动作类 `{changed}`/`{deleted}` 等语义化最小对象），失败一律 `{error:{code,message,hint,details?}}` 并带真实 HTTP 状态码；不再有 `{success,message,data}` 包装与"200 承载业务失败"。稳定契约端点与控制台内部端点共用同一信封。
- **运维端点按名寻址**：`channels/batch/{status,tag}` 接受 `channels:[渠道名]`（`ids` 仍兼容，二者只能给一个）；`model-catalog/batch-delete` 接受 `models:[模型名]`；`channels/batch/copy` 改为 body `{channel}`。修复 `batch/copy` 与 `batch/fetch-models` 此前因参数不一致而不可用的问题。
- **运维端点路由与响应策略同表声明**（design-v1 §16.3）：新增 `internal/api/ops_routes.go` 与 `internal/apiresp`，守卫测试强制每条已注册管理路由都有信封覆盖。
- **路由页只列真实车道**（ADR 0007）：`/routes` 只渲染 `source ∈ {explicit, disabled}` 的车道，渠道声明但未配车道的路由键不再以卡片出现——删车道即卡片消失，渠道声明与车道彻底分列；新建车道一律手填路由键。
- **数据看板收敛为单一入口**：侧边栏只保留一个「数据看板」（落地 `/dashboard/overview`），概览 / 模型调用分析 / 成本统计为页内 Tab。
- **成员默认上游真名按所选模型查渠道映射**（ADR 0006 §5）：从「渠道 × 模型 m」加入成员时以 m 为键解析 `model_mapping`，池化车道（车道名 ≠ m）也能命中映射。
- 控制台探测入口去重：移除与「探测上游模型」重复的「获取模型列表」入口（其在已保存渠道上会发无密钥草稿探测而必然 401）。

### Security
- 客户端密钥由服务端随机生成并明文入库，管理 API 可随时回读复制；鉴权仍走 `sha256` 哈希索引。管理密钥由登录口令派生、服务端只存其哈希、不落明文。
- 请求日志只存元数据，不存请求/响应正文与密钥明文。
- 管理面（`/api`、`/api/v1`）统一请求体上限（默认 2MB，`ANONYMOUS_REQUEST_BODY_LIMIT_KB`）；模型面仍为 128MB。
- CORS 不再对任意来源同时放行凭据；模型面不再接受 `?key=` 查询串凭据，访问日志去除查询串；上游错误响应体脱敏后再入日志；登录失败退避改为按来源返回 429 + `Retry-After`。

### Removed
- 删除死代码 `createRootAccountIfNeed`（`root`/`123456` 默认账号）与内部施工文档。
- 移除旧 `new-api`/`octopus` 两层网关的迁移机制：`internal/legacy` 包、`pbr migrate` 子命令与 `MIGRATION.md`。新实例只使用新建的 PBR 数据库（design-v1 §11）。
- 物理删除 API 信息面板全链（概览面板、系统设置「API 地址」分节、`console_setting.api_info` 与 `/api/status` 的 `api_info` 字段）。
- 移除超范围死代码：计费执行链、认证基座残留、旧选路链与渠道 pin、渠道亲和机制、任务插件与厂商营销文案等。

[Unreleased]: https://github.com/zzyyyds88/PowerBarRations/commits/main
