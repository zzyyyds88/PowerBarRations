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

## 4. 真实用户操作测试（L3）

**方法**：`chromium --headless` + CDP（DevTools 协议）驱动真实页面，用登录口令在页面内登录
（服务端下发 HttpOnly 会话 Cookie），逐步点击/输入，断言页面可见结果；关键写操作再由后端回读佐证。

**必须由 UI 触发的用户路径**（不得用 API 代劳）：

| # | 用户动作 | UI 入口 | 断言 |
|---|---|---|---|
| 1 | 首启设口令 | `/setup` 表单 | 进入控制台；`localStorage` 无管理密钥 |
| 2 | 登出/登录 | `/sign-in` 表单 | 会话 Cookie 生效；错误口令被拒 |
| 3 | 新建渠道 | 渠道管理 → 新建渠道（表单） | 列表出现该渠道；回读 `GET /channels/{name}` 一致 |
| 4 | 编辑车道成员链 | 路由页 → 编辑成员链（上移/下移/保存） | `GET /routes/{model}` 顺序一致 |
| 5 | 新建客户端密钥 | 令牌页 → 新建（表单） | 一次性明文可见；回读一致 |
| 6 | 试打台对话 | 试打台 | 页面显示回复与 `X-Served-By` |
| 7 | 查看请求日志 | 请求日志页 | 该请求出现且含车道/渠道 |
| 8 | 修改系统设置 | 系统设置页 | 保存后回读一致 |

**断言口径**：优先用用户可见文本/角色定位（按钮、标签、表格行）；不用内部 state 或 class 快照。
每次失败截图存档。

**注意**：`verify/final/console_flow.py` 现为"UI 走查 + API 代建"混合；L3 的要求是**用户路径由 UI 触发**。
`verify/e2e-ui/user_journey.py` 覆盖上表（含试打台展示 `X-Served-By`，ui-spec §6.8）；
`verify/final/a3_console.sh` 仍保留做逐页渲染走查。

## 5. 真实 API 运维测试（L2）

**方法**：只用 `Authorization: Bearer <管理密钥>` 调管理 API，模拟运维人员一天的活。
管理密钥由登录口令派生（`Base64(SHA256(口令))`，token-spec §2.1）。

**runbook 场景**（每步"调用 → 回读 → 断言"）：

| # | 运维动作 | 端点 | 断言 |
|---|---|---|---|
| 1 | 探活/版本/能力 | `GET /health /version /capabilities` | 200；能力枚举含协议 |
| 2 | 建渠道 | `PUT /channels/{name}` | 回读 key_prefix、models 一致 |
| 3 | 建车道 | `PUT /lanes/{model}` | 回读成员顺序=priority 降序 |
| 4 | 路由总览 | `GET /models`、`GET /routes/{model}` | `routable=true` |
| 5 | 端到端调用 | `POST /v1/chat/completions`（客户端密钥） | 200；日志出现 |
| 6 | 排障 | `GET /lanes/{name}/health`、`GET /logs?success=false` | 冷却/熔断/attempts 可读 |
| 7 | 逐成员探活 | `POST /lanes/{name}/probe` | `probed` 与成员级 `status` |
| 8 | 故障注入与转移 | 假上游 `/__control` | 首成员失败逃逸到次成员 |
| 9 | 重置熔断 | `POST /lanes/{name}/circuits/reset` | `reset` 计数 |
| 10 | 密钥轮换 | `POST /keys/{name}/rotate` | 旧明文失效、新明文可用 |
| 11 | 导出/导入幂等 | `GET /export`、`POST /import?dry_run=true` | dry_run 无变更；审计有记录 |
| 12 | 日志保留 | `POST /logs/prune` | 明细清空、聚合不变 |
| 13 | 审计 | `GET /audit` | 上述写操作均有记录 |

**故障注入**一律用内置假上游 `internal/testutil/fakeupstream` 的 `POST /__control`。

## 6. 端到端与故障验收（L4/L5）

沿用既有脚本，本文不重复其断言清单（见各自 README）：
`verify/final/{e2e,fault_injection,longrun,rollback,a4_import_idempotent,a3_console}.sh`、
`verify/deploy/smoke.sh`、`verify/final/hermes_acceptance.sh`、`verify/w0..w5/smoke.sh`。

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
| 管理 API / 契约（`internal/api`、`controller`、`apiresp`） | L1 + L2 + L4（`e2e`/`a4`） |
| 控制台（`web/`） | L0（pnpm）+ L3 + L4（`a3`） |
| 会话/认证（`common/session_cookie.go`、`middleware/pbr_auth.go`） | L1 + L3 + L4（`w3`） |
| 日志/记账（`model/pbr_request_log.go`、`internal/api/logs.go`） | L1 + L4（`w5`） |
| 部署/镜像 | L4（`deploy`） |
| 文档 | 无需 |

**收尾闸口**（合入 main 前）：L0 全量 + L1 全量 + L2 + L3 全绿。

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
| `curl` / `jq` / `openssl` | L2 | 失败（必需） |
| `docker` | `deploy/smoke.sh` | SKIP 退出 2 |
| `Xvfb` `openbox` `x11vnc` `websockify` `novnc` `xdotool` `imagemagick` | L3-manual | SKIP（该方式不可用） |

## 10. 失败判定与豁免

- 断言失败即失败；禁止用 `|| true` 吞掉。
- 已知偶发（须在 README 记录）：`longrun.sh` 曾有时序竞态，已修为"连续稳定才判 PASS"。
- 环境不支持必须显式 SKIP（退出码 2），不计入 PASS，也不计入 FAIL。
- 每次运行结果写入证据日志尾部：`=== 结果：PASS=n FAIL=m ===`。
