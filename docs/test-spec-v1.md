# PowerBarRations 测试规格 v1

> 规范性配套文件（与 `docs/design-v1.md` 及 api/routing/token/ui/hermes 各 spec 同级）。
> 它回答一个问题：**这套系统"怎么测"**。变更测试方法、闸口或证据口径时改本文；
> `verify/` 下的脚本是实现，必须与本文一致，冲突以本文为准。
> 本文不含任何部署私有数据（渠道名/模型名/地址/凭据/单价），只用占位符。

---

## 1. 目的与范围

PBR 是单二进制 + 单 SQLite 的本机网关，模型面 `/v1/*` 与管理面 `/api/v1/*` 同端口。
测试要证明两件事：

1. **用户真的能用**：像人一样在控制台里完成"建渠道 → 建车道 → 发密钥 → 调用 → 看日志"。
2. **运维真的能管**：只用管理 API 就能完成日常运维（排障、探活、熔断重置、密钥轮换、备份恢复）。

范围：`cmd/ internal/ pkg/ relay/ controller/ model/ service/ setting/ router/ middleware/ web/`。
不含 `reference/`（只读上游，不是本项目实现）。

## 2. 测试原则（铁律）

1. **真实优先**：能用真实用户操作验证的，不用 API 断言替代；能真实调用管理 API 的，不 mock。
2. **写后回读**（design §5）：任何写操作后必须 `GET` 回读并断言最终状态，不得只看写响应。
3. **用户可见流程必须由 UI 触发**：浏览器测试里，渠道/车道/密钥等用户能点的操作要真的点，
   不得"后台用 API 建好、只断言页面能显示"。
4. **隔离**：一律独立端口 + 独立 SQLite + 内置假上游；绝不打真实厂商；不碰现网容器与数据卷。
5. **契约优先**：错误按 `error.code` 分支，不解析 message。
6. **不静默**：环境缺失（无 chromium/docker）记 SKIP 并退出码 2，不得包装成 PASS。
7. **失败即失败**：任一断言失败退出码非 0，脚本末尾打印 `PASS=n FAIL=m`。
8. **证据可复现**：每次运行写带时间戳的日志/截图；`verify/**/*.log` 与截图不入库。

## 3. 分层与职责

| 层 | 名称 | 内容 | 入口 | 典型耗时 |
|---|---|---|---|---|
| L0 | 静态闸口 | build/vet/test、typecheck/lint/test/build/format/copyright | `go ...`、`pnpm ...` | 分钟级 |
| L1 | 单元/集成 | Go 包内测试、契约守卫（openapi/envelope 覆盖） | `go test ./...` | 分钟级 |
| L2 | 真实 API 运维 | 管理面 runbook（建/排障/轮换/备份） | `verify/e2e-api/ops_runbook.sh` | 分钟级 |
| L3 | 真实用户操作 | 无头浏览器按用户路径操作控制台 | `verify/e2e-ui/user_journey.py` | 分钟级 |
| L4 | 端到端验收 | 端点全量、故障注入、长稳、部署、导入幂等、回滚 | `verify/final/*`、`verify/deploy/*` | 十分钟级 |
| L5 | Hermes 档案 | §2 档案形态 + §6 五项闭环 | `verify/final/hermes_acceptance.sh` | 分钟级 |

L2 与 L3 是本文新增的两层，专门回答"真实用户操作"与"真实 API 运维"。

**L3 有两种运行方式**：

- **L3-auto**（`verify/e2e-ui/user_journey.py`）：无头 Chromium + CDP 脚本驱动，可回归、可 CI。
- **L3-manual**（`verify/e2e-ui/live_browser.sh`）：**可见的真实图形浏览器**（Xvfb + openbox + 有头
  Chromium），经 x11vnc + noVNC 暴露成网页，供**人用真实鼠标键盘亲手操作**。用于探索性测试、
  验收演示、以及脚本覆盖不到的视觉/交互判断。两者都跑真实浏览器引擎，区别只在"谁在操作"。

### 3.1 L1 最小用例集：车道成员开关与成员顺序

改「成员契约」的任何一层（`model/lane.go` 的成员字段、`internal/route` 的成员选路、
`internal/api/lanes.go` / `routes.go` / `lane-summaries` / `config_lifecycle.go` 的成员形状、
`web/src/features/routes` 的编排器）都必须落下面这组用例；它们都是 **L1**（跑在 L0 的
`go test ./...` / `pnpm test` 里），**不替代** §4 的 L3，也不替代 §5 的 L2。语义真源：api-spec §4.2/§5.7/§6.5、routing-spec §1.2/§2/§9、ui-spec §6.3。

**Go：选路（`internal/route/`）**

| 用例 | 断言 | 备注 |
|---|---|---|
| failover 跳过被关闭成员 | 成员链 A(关)→B(开)：请求只打到 B；`attempts[].status == "disabled"`，与 `cooldown`/`circuit_break`/`skipped` 不混用 | api-spec §4.4；与 `route_test.go` 的 `TestMaxRetriesForLoopCoversAllMembers` 同族（成员链遍历夹具） |
| manual 的 `active_member` 被关闭 | 返回"无可用"（`503 no_available_member`），**绝不静默换人** | routing-spec §2.1；落 `manual_mode_test.go` |
| 开关切换不污染运行态 | 关→开往返前后，该成员的 `consecutive_failures` / `cooldown_until` / 熔断状态逐字段不变；写开关**不产生**冷却条目 | 配置态与运行态不得互相污染 |
| 快照签名对开关字段敏感 | 断言"只改成员开关"会使 `routeSnapshotSignature` 输出**变化**（前后两串不等） | **按契约规定，不按事故风险规定**：保存成员链是"删光重插"，主键与秒级 `Lane.UpdatedAt` 本来就会变，漏入签名未必立刻表现得"改了没反应"——但那是隐式耦合，同秒两次保存即失效（routing-spec §1.3）；落 `hot_reload_test.go` 同族 |
| 被关闭成员不占探测槽 | 车道内只有一个"半开/冷却到期"的可探测成员 + 一个被关闭成员时，探测请求放行的是前者；关闭成员**绝不**取得每轮唯一的那个探测槽 | routing-spec §2.2；漏判会让一个关着的成员饿死整条链的恢复探测（槽位断言同 `TestCooldownSkipsThenAllowsSingleProbe`） |
| 关闭当前成员即打破亲和 | 亲和/当前成员指向被关闭的成员时，选路**立即**按顺序重选（不等亲和到期）；重新打开不自动回粘，需由选路重新确立当前成员 | routing-spec §2.2；`TestConfiguredAffinitySticksWithinWindow` 同族 |
| 全部成员被关闭 | 成员链仍解析得到；选路返回 `503 no_available_member`；该模型仍出现在 `GET /models` | api-spec §5.7 |
| 健康快照形状含 `enabled` | `TestHealthSnapshotJSONShape` 断言成员条目含 `enabled`（恒回），人工关闭时 `enabled=false` 且 `available=false` | 既有形状守卫测试必须同步扩展，不得绕过 |

**Go：管理 API（`internal/api/`）**

| 用例 | 断言 | 备注 |
|---|---|---|
| 写入开关 + 写后回读 | `PUT /lanes/{name}/members` 置 `enabled=false` → 响应体即落库终态；再 `GET /lanes/{name}` 与 `GET /routes/{model}` **恒回** `enabled` | §2.2 写后回读 |
| `enabled` 三态默认 | ①省略 `enabled` 的新成员 → 回读为启用；②省略 `enabled` 的既有成员（按 `(channel, upstream_model)` 匹配）→ **保留原值**（原本关闭的仍关闭）；③显式 `true`/`false` 生效 | `TestPutLaneOmittedMembersPreservesExisting` 同族；防"漏发字段即全站关掉成员" |
| 全量替换未被放宽 | 漏发成员仍等于删除该成员 | 新字段不得把全量合同变成补丁合同 |
| `GET /routes/{model}` 成员字段齐全 | 恒回 `model` / `upstream_model`（只读派生）/ `enabled` / `overrides`（无覆盖时为 `{}`）/ `member_id`；`unconfigured` 推荐项 `member_id=0`、`enabled=true` | 防"读不到即清空"这类回归（[ADR 0008](adr/0008-member-stores-selected-model.md)） |
| 成员唯一键 `(渠道, 模型)` | 同一 `(channel, model)` 重复提交 → `422 duplicate_member`；同渠道不同模型仍可共存 | 去重键 = 成员所选模型 |
| 上游真名由渠道映射推导 | 改渠道 `model_mapping` 后，**不重存车道**再 `GET /routes/{model}`，成员 `upstream_model` 立即变化；池化车道（车道名 ≠ 模型名）下查表键是**模型名**而非路由键 | ADR 0008 背景第 2/3 条；落 `routes_member_model_test.go` 同族 |
| dry-run 不落库 | `PUT /lanes/{name}` 与 `PUT /lanes/{name}/members` 带 `?dry_run=true` 改开关 → 响应 `dry_run:true` 且车道名出现在 `diff.lanes.update`，随后 `GET` 回读**逐字段与调用前一致** | diff 是名级的（api-spec §5.9），"未落库"只能靠回读比对断言 |
| 导出/导入保留开关 | `GET /export` 的成员对象带 `enabled`；导出→导入往返后关闭状态仍为关闭 | api-spec §5.6 |
| `/api/models` 聚合口径 | 关掉一个成员 → `available_member_count` 减一、`disabled_member_count` 加一；全部关闭 → `degraded=true` 且 `disabled_member_count == member_count`（而 `disabled_member_count=0` 的全挂仍为 `degraded=true`，两条路径可区分）；模型始终在清单里 | routing-spec §7；落 `models_health_test.go` 同族 |
| lane-summaries 成员形状 | `GET /lane-summaries` 的成员条目含 `enabled`，供路由页卡片标灰（不经该字段无法渲染） | api-spec §5.7 |
| 审计 | 开关写入落 `resource=lane`（整条）/ `resource=lane_members`（仅成员）的 `action=update` 条目，`after_digest` 与改动前不同 | api-spec §2.7、§5.2 |

**web（`web/src/features/routes/`）**

| 用例 | 断言 | 备注 |
|---|---|---|
| `reorderMembers` 纯函数 | 拖到首位 / 末位 / 相邻交换 / 拖到自身 / `before` 与 `after` 两种落点 / 未知 id 时**原样返回** | 放 `lib/__tests__/`（同 `lane-member-upstream.test.ts`）；组件不测排序，只测接线 |
| 拖拽后 `priority` 生成正确 | 重排后提交载荷的 `priority` 是**新下标**的严格降序（现规则 `priority = 成员数 - 下标`，数字大者优先），逐成员与数组位置一一对应 | 断言的是生成结果，不是内部 state |
| 开关草稿读回写往返 | 从 `GET /routes/{model}` 回填草稿时 `enabled` 正确入草稿；保存载荷带回同一个值（关的不会变开）；`overrides`/`public_alias` 同样不丢 | api-spec §4.2 通则 |
| 成员行 React key 稳定 | 行 key 只用草稿 `id`：改上游真名、改开关都**不重挂载**该行（输入框保持焦点与 DOM 节点） | 代码里有实测 bug 记录（ui-spec §6.3） |
| 运行态徽章 | `laneMemberStates` 对 `enabled=false` 的成员产出「人工关闭」（`neutral`）徽章，与 Circuit open（`danger`）/Cooldown（`warning`）**并列且 variant 不同** | `__tests__/lane-runtime.test.tsx` 同族 |
| i18n 覆盖 | 新增 `t()` 字面量在 7 个 locale 全齐 | `literal-key-coverage` 红 = L0 阻断 |

**隔离**：以上 Go 用例一律独立内存/独立文件 SQLite，禁止指向真实 `pbr.db`；开关相关断言不得依赖
真实厂商或共享运行态单例（`route.Default` 需在用例内重置）。

## 4. 真实用户操作测试（L3）

**方法**：`chromium --headless` + CDP（DevTools 协议）驱动真实页面，用登录口令在页面内登录
（服务端下发 HttpOnly 会话 Cookie），逐步点击/输入，断言页面可见结果；关键写操作再由后端回读佐证。

**必须由 UI 触发的用户路径**（不得用 API 代劳）：

| # | 用户动作 | UI 入口 | 断言 |
|---|---|---|---|
| 1 | 首启设口令 | `/setup` 表单 | 进入控制台；`localStorage` 无管理密钥 |
| 2 | 登出/登录 | `/sign-in` 表单 | 会话 Cookie 生效；错误口令被拒 |
| 3 | 新建渠道 | 渠道管理 → 新建渠道（表单） | 列表出现该渠道；回读 `GET /channels/{name}` 一致 |
| 4 | 编辑车道成员链 | 路由页 → 编辑成员链（**上移/下移**、删除、改上游真名、保存；拖拽见 4d、启停见 4e） | `GET /routes/{model}` 顺序一致；上移/下移作为独立重排路径必须仍被覆盖（拖拽不是唯一途径） |
| 4b | 自由编排（池化） | 路由页 → 页头「新建车道」：手填任意路由键 + 跨渠道跨模型挑选成员 | `PUT /lanes/{name}` 成功；`GET /routes/{name}` 成员与所选一致（含同一渠道多个模型）；渠道声明该键并非前提 |
| 4c | 路由页只列真实车道 | 路由页 + 模型管理页 | 渠道声明但未配车道的路由键**不在路由页出现**；删除车道后该卡片立即消失（[ADR 0007](adr/0007-routes-lists-lanes-only.md)）；模型管理页仍以「不可调用」徽章表达 |
| 4d | **拖拽重排成员顺序** | 路由页 → 编辑成员链 → **真实鼠标拖动**成员行（至少覆盖：拖到末位、拖回首位、落在目标行上半与下半两种判定）→ 保存 | 拖动中出现落点指示线；保存后 `GET /routes/{model}` 的成员顺序与 `priority` 降序与拖拽结果一致；**上移/下移按钮仍然存在且有效**（拖拽不得是唯一途径）；页面成员摘要顺序同步变化 |
| 4e | **人工停用成员并验证选路** | 路由页 → 编辑成员链 → **真实点击**某成员的开关（行变灰 + 「已停用」徽章 + 后果提示）→ 保存 → 发一次端到端请求 → 再打开开关保存 → 再发一次请求 | `GET /routes/{model}` 该成员 `enabled=false` **且仍在成员链里、位置不变**；关闭期间端到端请求不再命中它（`GET /logs/{id}` 的 `attempts[].status` 出现 `disabled`）；车道健康/卡片把它显示为「人工关闭」（灰）而非冷却/熔断；重新打开后端到端请求重新命中它（**必须实测恢复**，不能只测关闭） |
| 5 | 新建客户端密钥 | 令牌页 → 新建（表单） | 一次性明文可见；回读一致 |
| 6 | 试打台对话 | 试打台 | 页面显示回复与 `X-Served-By` |
| 7 | 查看请求日志 | 请求日志页 | 该请求出现且含车道/渠道/客户端/会话；Cost 列显示元（非额度）；详情弹窗含 attempts 链与上下文卡片（无窗口时只显示 token，`context_status != ok` 显示原因）；偏移跳页可用且总数正确 |
| 7b | 查看会话 | 日志 → 会话页 → 打开某会话 | 列表/筛选/排序/分页可用；详情含摘要、上下文卡片、填充曲线与请求时间线；「查看该会话全部请求」回到请求页且过滤生效；`session_not_found` 显示可读错误 |
| 7c | 概览用量增强 | 概览页 | 缓存命中率在无输入时显示 `—`；客户端分布有数据/空态；53 周日历有数据/空态，无数据日期为空白格；概览**不再**出现实时路由卡 |
| 7d | 查看实时路由 | 侧边栏「实时路由」→ `/live-routing` | 单页上半部渲染车道→网关→成员拓扑与 Live/轮询徽章；时间区间与 `lane` 写入 URL，刷新后可恢复；无车道时给「打开路由」。下半部「渠道速度」面板默认 15m，展示活跃渠道卡（输出速度/实时流量/请求速率/p50/p95/成功率）与空闲渠道条带；实时流量固定近 15 分钟，不随 5m/1h 切换；切窗口写入 URL 并重新取数；空闲渠道显示 `—` 而非 0；窗口内无请求给空态提示。页面不得再出现独立「实时仪表盘」入口 |
| 8 | 修改系统设置 | 系统设置页 | 保存后回读一致 |
| 9 | 查看渠道状态 | 侧边栏「渠道状态」页 → 某渠道行「详情」 | 列表先渲染（渠道/协议/成员/成功率等列）；点「详情」抽屉打开且四块内容在（当前状态 / 窗口概览 / 最近失败 / 探活历史）；切 24h↔7d 重新取数；点「立即探活」后探活历史出现新记录（ui-spec §6.4A） |
| 10 | 日志导出 | 请求日志页 → 导出 CSV / NDJSON | 下载文件列与 api-spec §5.5.1 一致；超 31 天/10 万行给可读错误 |
| 11 | 日志实时 tail | 请求日志页 → 档位 2s → 发一次端到端请求 | 新行自动出现；页面隐藏时暂停、可见时恢复 |
| 12 | 价格峰谷 | 渠道 Sheet → 价格页签 → 新增时段 + DeepSeek 预设 + 批量粘贴 | 保存后回读 `prices[].bands` 一致；当前生效价与预览一致；非法时段被拦截 |
| 13 | 命令面板实体搜索 | ⌘K → 输入渠道/Key/模型/Request ID | 结果分组正确、键盘可选、跳转带过滤 |
| 14 | 空态与错误态 | 清空筛选 / 后端报错 | 空态给 CTA 或「清除筛选」；错误显示 `ErrorState` + 重试，不显示空表 |

**断言口径**：优先用用户可见文本/角色定位（按钮、标签、表格行）；不用内部 state 或 class 快照。
每次失败截图存档。

**注意**：`verify/final/console_flow.py` 现为"UI 走查 + API 代建"混合；L3 的要求是**用户路径由 UI 触发**。
`verify/e2e-ui/user_journey.py` 覆盖上表（含试打台展示 `X-Served-By`，ui-spec §6.8）；
`verify/final/a3_console.sh` 仍保留做逐页渲染走查。

**L3-E2E（Playwright，补充冒烟层）**：`verify/e2e-ui/playwright/` 用 `@playwright/test` 覆盖
登录 → 7+1 路由可达 → 日志四页签 → 导出 → 命令面板 → 渠道行动作降噪（启停在菜单、行内无常驻启停）
→ 日志窗口 KPI / 表格-卡片切换与 localStorage 记忆 → 模型目录价格/健康列头；它不替代 `user_journey.py` 的"写后回读"
断言，只负责把"路由/页面不存在"这类构建期漏网挡在合入前。缺失 Playwright 时 SKIP（退出码 2）。

**4d 的拖拽必须"真拖"**：原生 HTML5 DnD 不响应合成 `click`，也不接受"JS 直接改数组再断言"。必须用 CDP
派发真实鼠标事件序列（`Input.dispatchMouseEvent` 的 move → down → move → up，或
`Input.setInterceptDrags` + `Input.dispatchDragEvent`）。若该路径在无头 Chromium 下确实驱动不了，
**唯一允许的处理是显式 SKIP（退出码 2）并改用 L3-manual（`verify/e2e-ui/live_browser.sh`，真人鼠标）补证**，
并在该目录 README 记录；**禁止**把"对 `reorderMembers` 纯函数打单测"或"用 API 改 `priority` 后断言页面显示"
包装成 4d 通过（§2.1、§2.3、§2.6）。4e 的开关没有这个技术障碍，一律真实点击。

## 5. 真实 API 运维测试（L2）

**方法**：只用 `Authorization: Bearer <管理密钥>` 调管理 API，模拟运维人员一天的活。
管理密钥由登录口令派生（`Base64(SHA256(口令))`，token-spec §2.1）。

**runbook 场景**（每步"调用 → 回读 → 断言"）：

| # | 运维动作 | 端点 | 断言 |
|---|---|---|---|
| 1 | 探活/版本/能力 | `GET /health /version /capabilities` | 200；能力枚举含协议 |
| 2 | 建渠道 | `PUT /channels/{name}` | 回读 key_prefix、models 一致 |
| 3 | 建车道 | `PUT /lanes/{model}` | 回读成员顺序=priority 降序 |
| 3b | 池化车道（跨渠道跨模型） | `PUT /lanes/{name}`（成员任选，含同一渠道多次） | 回读成员 = 提交的 `(渠道, 上游真名)` 列表；未声明该键的成员同样写入 |
| 3c | 声明模型不自动建车道 | `PUT /channels/{name}` 新增模型后 `GET /models` | 新模型 `source=unconfigured`、`routable=false`；`GET /lanes` 不新增任何车道，路由页也不出现新卡片 |
| 3d | **成员开关：停用 → 回读 → 预览 → 端到端后果** | `GET /routes/{name}` 取全链 → `PUT /lanes/{name}/members`（目标成员 `enabled=false`，整链原样带回）→ `GET /lanes/{name}` 回读 → 同载荷带 `?dry_run=true` 再调一次 → `POST /v1/chat/completions` → `GET /lanes/{name}/health` | 回读该成员 `enabled=false` **且其余成员的 `overrides` / `public_alias` / `model` 未被清空**；dry-run 后再次回读**与调用前逐字段一致**且响应含 `dry_run:true`；端到端请求不再命中被关闭成员（`attempts[].status` 出现 `disabled`）；健康快照该成员 `disabled=true` 且 `available=false`；把 `enabled` 改回 `true` 后重新命中 |
| 4 | 路由总览 | `GET /models`、`GET /routes/{model}` | `routable=true` |
| 4b | 模型目录上下文窗口 | `PUT /model-metadata/{model}`（含 `context_window`）→ `GET /model-metadata` | 回读值一致；负数/非整数 → 422；只影响用量窗口填充率，不改变选路结果 |
| 5 | 端到端调用 | `POST /v1/chat/completions`（客户端密钥） | 200；日志出现 |
| 5b | 会话与上下文 | 带 `X-PBR-Session` 与 tools/messages 调 `POST /v1/chat/completions` → `GET /logs` → `GET /logs/sessions` → `GET /logs/sessions/{key}` → `GET /logs/{id}` | 日志行 `session_id`/`session_key`/`client` 非空；会话列表含该会话且请求数=1；详情 `requests` 含该条；单条详情 `context.status=ok` 且分区 token 之和=`context_tokens`（校准后）；无会话头的请求按指纹兜底仍能归属 |
| 5c | 统计客户端维度与缓存归一 | `GET /stats?group_by=client`；对比带缓存请求的 `cache_read_tokens`/`input_tokens` | 该客户端桶存在；`input_tokens` 按 `usage_semantic` 归一（Anthropic 含 cache_read+cache_write，OpenAI/Gemini 等于 prompt）；`input_tokens=0` 时调用方不显示命中率 |
| 6 | 排障 | `GET /lanes/{name}/health`、`GET /logs?success=false` | 冷却/熔断/attempts 可读 |
| 6b | 日志分页双模式 | `GET /logs?page=2&page_size=10`；`GET /logs?cursor=<base64>` | 偏移返回 `total`+`page`+`page_size`+正确切片；游标返回 `next_cursor`；偏移响应含 `type`/`channel_id`/`token_id`/`user_id`/`ip`（对齐 UsageLog 形状） |
| 7 | 逐成员探活 | `POST /lanes/{name}/probe` | `probed` 与成员级 `status` |
| 8 | 故障注入与转移 | 假上游 `/__control` | 首成员失败逃逸到次成员 |
| 9 | 重置熔断 | `POST /lanes/{name}/circuits/reset` | `reset` 计数 |
| 10 | 密钥轮换 | `POST /keys/{name}/rotate` | 旧明文失效、新明文可用 |
| 11 | 导出/导入幂等 | `GET /export`、`POST /import?dry_run=true` | dry_run 无变更；审计有记录；**成员开关随导出/导入往返不丢**（先按 3d 关掉一个成员，导出 → 导入 → 回读仍为 `enabled=false`） |
| 12 | 日志保留 | `POST /logs/prune` | 明细与 `pbr_request_contexts` 清空、聚合不变；会话列表不再含已清理窗口 |
| 13 | 审计 | `GET /audit` | 上述写操作均有记录 |
| 14 | dry-run 不落库 | 每个 `preview` 端点带 `?dry_run=true` 调一次，再 `GET` 回读 | **状态与调用前一致**（无副作用），且响应含 `dry_run:true` |
| 15 | dry-run 拒绝式端点不执行 | 每个 `reject` 端点带 `?dry_run=true` | 400 `dry_run_not_supported`，且**回读无变化** |

**故障注入**一律用内置假上游 `internal/testutil/fakeupstream` 的 `POST /__control`。

## 6. 端到端与故障验收（L4/L5）

沿用既有脚本，本文不重复其断言清单（见各自 README）：
`verify/final/{e2e,fault_injection,longrun,rollback,a4_import_idempotent,a3_console}.sh`、
`verify/deploy/smoke.sh`、`verify/final/hermes_acceptance.sh`、`verify/w0..w5/smoke.sh`。

**选路语义改动（含车道成员开关，§8）必须重跑 `fault_injection.sh`**：它覆盖"首成员失败 → 逃逸到次成员"，
人工停用某成员后该脚本既有断言不得被动改变（开关只是把该成员移出候选，**不产生冷却、不改熔断**）。

## 7. 证据与目录

- L2：`verify/e2e-api/run-<时间戳>.log`（本目录 README 记录结论）
- L3：`verify/e2e-ui/run-<时间戳>.log` + `verify/e2e-ui/shots/*.png`
- L4/L5：`verify/final/*.log`、`verify/deploy/*.log`
- 规则：日志与截图不入库（`verify/**/*.log` 已忽略）；README 里写最近一次实测结论与时间。

## 8. 闸口与复跑触发矩阵

| 改动类型 | 必跑 |
|---|---|
| 任意代码 | L0 全量 + L1 |
| 路由/故障转移（`internal/route`、`relay/channel/api_request.go`） | L1 + L2 + L4（`e2e`/`fault_injection`/`w2`） |
| 车道成员编排与顺序（`internal/api/lanes.go`、`model/lane.go`、`web/src/features/routes`） | L0（pnpm）+ L1（§3.1「web」表）+ L2（建车道/池化）+ L3（自由编排 + **4d 拖拽重排**）+ L4（`a3`） |
| **车道成员启停开关**（`model/lane.go` 成员字段 + `internal/route`（选路、快照签名、健康形状）+ `internal/api/lanes.go`、`routes.go`、`config_lifecycle.go`、lane-summaries + `web/src/features/routes`） | L0 全量（Go + pnpm）+ L1（§3.1 三张表**全跑**）+ L2（**3d**）+ **L3（4e）** + L4（`fault_injection` 回归：开关不得改变故障转移语义、不得写冷却/动熔断）+ L4（`a4` 导入幂等，验开关往返） |
| **成员组**（`model/member_group*.go`、`model/lane.go` 解析展开与快照签名、`internal/api/member_groups.go`、`lanes.go` 两态写入、导出/导入 + `web/src/features/routes` 组管理与组引用行） | 同「车道成员启停开关」行的**并集**，另加：L1 组展开/两层开关/展开后判重单测（含"只改组配置、车道 `UpdatedAt` 不变时快照签名必须变化"的断言）+ L2 组 CRUD 与级联删除 runbook + L3 组引用编排（加入组、关组全局开关、只关一条车道的引用开关、改组内顺序两条车道同步生效）+ L4（`fault_injection` 回归：组关闭不得写冷却/动熔断） |
| **计价口径或统计口径**（`model/log.go` 的 `estimateRequestCost`、`model/pbr_request_log.go` 聚合维度、`internal/api/model_status.go`、`channels_status.go`、stats、`POST /api/stats/rebuild`） | L0 + L1（口径单测：计价键按成员所选模型匹配、`model`/`channel_model`/`key_model` 按 `upstream_model` 聚合、分位数与干净样本 tps 的边界——样本 <5、`tps_samples=0`、多尝试请求不计入）+ L2（rebuild 的 dry-run 与实跑：幂等、明细不被改动）+ L3 看板（维度列是真模型名、令牌视图、无样本显示 `-`） |
| 管理 API / 契约（`internal/api`、`controller`、`apiresp`） | L1 + L2 + L4（`e2e`/`a4`） |
| **dry-run 语义**（`internal/api/ops_routes.go` 的 dry-run 声明、任何写端点） | L1（守卫 + 行为测试）+ L2（runbook）+ L4（`e2e` 的 dry-run 专项） |
| 控制台（`web/`） | L0（pnpm）+ L3 + L4（`a3`） |
| 会话/认证（`common/session_cookie.go`、`middleware/pbr_auth.go`） | L1 + L3 + L4（`w3`） |
| 日志/记账（`model/pbr_request_log.go`、`internal/api/logs.go`） | L1 + L4（`w5`） |
| **用量/会话/上下文**（`internal/usage/**`、`model/pbr_request_log.go`、`model/pbr_request_context.go`、`model/model_meta.go`、`internal/api/logs.go`、`router/`、`web/src/features/{usage-logs,dashboard,models}`） | L0 全量（Go + pnpm）+ L1（会话提取优先级/指纹稳定性/UA 映射；四协议分区解析/校准后总和/物品裁剪/64 KiB 上限/状态；统计归一与命中率边界；模型目录 `context_window` 校验）+ L2（**4b/5b/5c/12**：会话列表/详情/过滤/分页、上下文随 prune 清理、client 维度与 rebuild 幂等）+ L3（**7/7b/7c**）+ L4（`e2e`/`w5` 回归；上下文解析不得影响转发与故障转移） |
| **隐私脱敏**（`internal/privacy/**`、`model/pbr_request_log.go`、`controller/relay*.go`、`internal/api/privacy*.go`、`web/src/features/privacy/**`） | L0 全量（Go + pnpm）+ L1（移植的 redact 上游测试全绿；设置归一/校验；占位符稳定、仓库淘汰、JSON/SSE 恢复、block/required/engine 失败策略；日志命中元数据不含原值）+ L2（设置/预览/模型状态/命中记录/summary）+ L3（脱敏页开关/预览/记录详情 + 请求日志徽标）+ L4（`e2e`/`fault_injection`；默认关闭时转发字节不变、开启后流式仍可用） |
| **日志导出**（`internal/api/logs.go`、`router/`、`web/src/features/usage-logs`） | L0 + L1（列顺序/31 天与 10 万行限制/审计）+ L2（CSV 与 NDJSON 实拉）+ L3（导出）+ L4（`w5`） |
| **审计/渠道/密钥导出**（`internal/api/*_export.go`、`model/pbr_audit*`、`web/src/features/{channels,keys,usage-logs/audit}`） | L0 + L1（列/无明文/限制）+ L2（实拉）+ L3（导出入口） |
| **指标导出**（`/metrics`、`internal/metrics`） | L0 + L1（计数器/标签/未鉴权 401）+ L2（抓取文本格式）+ 不影响 L4 |
| **价格峰谷**（`model/log.go`、`setting/pricing_setting`、`internal/api/channels.go`、`web/src/features/channels`） | L0 + L1（band 匹配/跨午夜/兜底/校验/rebuild 幂等/`price_band`）+ L2（rebuild dry-run/实跑）+ L3（价格页签）+ L4（`a4` 导入导出往返） |
| **入站 `/v1/responses`**（`router/relay-router.go`、`relay/`） | L0 + L1（流式/非流式/503/能力枚举）+ L2（模型面调用）+ L4（`e2e`） |
| **流内错误分类**（`relay/helper/stream_reject.go`、各格式管道） | L0 + L1（四格式判定/提交前换人/提交后透传）+ L4（`fault_injection`） |
| **异步对象 Files/Batch**（`relay/`、`model/`、`router/`） | L0 + L1（`X-PBR-Lane` 必填/跨车道隔离/不支持协议/上限/映射表）+ L2（创建/查询/取消 runbook）+ L4（`e2e`，含缺失 lane 头 400） |
| **Anthropic Files 网关侧存储**（`internal/filestore`、`relay/async`、`model/pbr_file.go`、`relaykit/dto/claude.go`、`relay/claude_handler.go`） | L0 + L1（beta 头分流/配额/过期/跨密钥隔离/路径穿越拒绝/`file_id` 展开为 base64/展开失败返回 400）+ L2（上传→引用→清理 runbook）+ L4（`e2e`，含"上游收到的请求是 base64 而非 file_id"断言） |
| **流式提交与空响应**（`relay/helper`、各流式 handler、`relay/common/relay_info.go`） | L0 + L1（每个写出 `data:` 帧的入口必须打提交标记与首字耗时/零帧空流判 `bad_response`/正常流不被误判）+ L4（`fault_injection` 空流用例） |
| **前端工程清理 / 视觉基线**（`web/src/routes`、`web/src/components`、`web/src/features`） | L0（pnpm 全量 + routeTree/ignore 守卫）+ L3 + L3-E2E + L4（`a3`） |
| 部署/镜像 | L4（`deploy`） |
| 文档 | 无需 |

**收尾闸口**（合入 main 前）：L0 全量 + L1 全量 + L2 + L3 全绿。

**取并集，不取最轻**：一次改动同时命中上表多行时（成员开关就同时命中「车道成员编排与顺序」
「车道成员启停开关」「管理 API / 契约」「dry-run 语义」「控制台」五行），必跑集合是这些行的**并集**。
其中 L3 会 `pnpm build` 重建 `web/dist/`，与任何 `go build` 类脚本（L2/L4/L5）**必须串行**（§8.1）。

## 8.1 并发与构建产物冲突（重要）

`web/dist/index.html` 是 tracked 构建产物，且 `main.go` 用 `go:embed web/dist` 内嵌它。
L3（`user_journey.py`）会先 `pnpm build`，构建过程会**清空并重写** `web/dist/`；若此刻
其他脚本正在 `go build`，会命中 `pattern web/dist/index.html: no matching files found` 而假失败。

**规则**：L3 与任何 `go build` 类脚本（L2/L4/L5）**不得并发运行**。CI/本地应串行，
或先 `pnpm build` 一次、再串行跑其余脚本。L3 结束时用 `git checkout -- web/dist/index.html`
恢复 tracked 产物，保持工作区干净。

## 9. 环境与工具

| 工具 | 用途 | 缺失时 |
|---|---|---|
| `go` / `pnpm` | L0/L1 | 失败（必需） |
| `python3` + `websocket-client` | L3（CDP） | SKIP 退出 2 |
| `chromium` | L3 | SKIP 退出 2 |
| `@playwright/test` + 已缓存 Chromium | L3-E2E | SKIP 退出 2 |
| `node`（routeTree / gitignore 守卫脚本） | L0 | 失败（必需） |
| `curl` / `jq` / `openssl` | L2 | 失败（必需） |
| `docker` | `deploy/smoke.sh` | SKIP 退出 2 |
| `Xvfb` `openbox` `x11vnc` `websockify` `novnc` `xdotool` `imagemagick` | L3-manual | SKIP（该方式不可用） |

## 10. 失败判定与豁免

- 断言失败即失败；禁止用 `|| true` 吞掉。
- 已知偶发（须在 README 记录）：`longrun.sh` 曾有时序竞态，已修为"连续稳定才判 PASS"。
- 环境不支持必须显式 SKIP（退出码 2），不计入 PASS，也不计入 FAIL。
- 每次运行结果写入证据日志尾部：`=== 结果：PASS=n FAIL=m ===`。
- **构建期守卫**：`web/scripts/check-route-imports.mjs` 校验 `routeTree.gen.ts` 的每个相对 import 都存在；`web/scripts/check-gitignore.mjs` 用 `git check-ignore` 校验没有源码目录被忽略。两者在 `pnpm typecheck` 前执行，失败即 L0 阻断（2026-11-06 日志路由被 `logs` 规则吞掉的回归守卫）。
