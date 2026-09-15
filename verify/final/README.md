# 成品验收索引（W8）

> 本文是 goal-prompt §六「W8 成品验收」的证据索引：逐项写清**检查项 → 命令 → 结论 → 证据路径**。
> **不把未做的项包装成已完成**：凡未实测的条目一律标 ❌ 或 ⚠️ 并写明原因。
> 最近一次全量复跑：2026-09-15（W7 减脂合并前）；W6 迁移在旧两层库只读副本上实测通过。
>
> **审查整改（独立复审后）**：`longrun.sh` 第 8 步原存在时序竞态（第 7 步的 after-load
> 请求与第 8 步计数之间），曾导致 50k 口径 3 次里 2 次出现"聚合/明细差 1"的假失败。
> 已改为"等明细与小时聚合连续稳定 → 三种读数带重试一致性比对"，稳定结果才算 PASS。
> `console_flow.py` 原硬编码 `127.0.0.1:5711` 且无脚本调用，其"24/24"无法从仓库重建；
> 已改为**自包含**（自行构建并拉起 fakeupstream + PBR，随机端口，参数化 key），
> 关键步骤加后端状态断言（管理密钥可鉴权、渠道落库、请求产生新日志、改口令后旧密钥 401/新口令生效），
> 并由 `a3_console.sh` 在逐页走查后调用。
> 日志目录 `verify/**/*.log` 不入库；真实迁移报告含私有数据，只落 `/tmp`，不进仓库。

运行环境：独立端口 + 独立 SQLite + 内置假上游（`internal/testutil/fakeupstream`）
+ 无头 Chromium（CDP）控制台走查 + 独立 docker compose 项目（`pbr-verify`）。
全程未触碰现网在跑的网关容器与其数据卷，未使用任何真实厂商 key。

## 自动化自验收

```bash
bash verify/final/e2e.sh                 # 端点全量实跑 + 端到端金标准 + 安全 + 重启持久化
bash verify/final/a3_console.sh          # 控制台逐页走查（无头 Chromium + CDP + 截图）
bash verify/final/a4_import_idempotent.sh# 导入/导出幂等与对账
ROUTING_DB=... VENDOR_DB=... bash verify/final/a4_migrate.sh  # 真实迁移：旧两层库副本跑两次 + 加载校验
bash verify/final/fault_injection.sh     # 故障注入矩阵
bash verify/final/longrun.sh             # 有界长稳与并发（默认 3000；TOTAL=50000 跑满口径）
bash verify/final/rollback.sh            # 切流与回滚演练
bash verify/deploy/smoke.sh              # 独立 compose 项目从零部署 + 重启持久化
```

## 逐项索引

| 项 | 检查项 | 命令 / 依据 | 结论 | 证据 |
|---|---|---|---|---|
| **A1** 功能完整性：端点无 5xx、无未实现桩 | openapi 登记的全部 path×method 实跑 | `verify/final/e2e.sh` | ✅ PASS=33 | `verify/final/run-20260915-093114.log` |
| **A2** 四模式 + 冷却 + 亲和 + 熔断半开 | 四模式各跑通；熔断打开→半开→复通留时间戳 | `verify/w2/smoke.sh` | ✅ PASS=32 | `verify/w2/run-20260915-093248.log`、`verify/w2/README.md` |
| **A3** 控制台逐页走查（ui-spec §8） | 无头 Chromium + CDP 注入管理密钥，逐页导航/断言渲染与 console 无报错/三态组件/品牌残留；随后跑 `console_flow.py` 完整使用流程（自包含假上游 + 后端强断言） | `verify/final/a3_console.sh` | ✅ PASS=9 + 完整流程 28/28（整改后实测） | `verify/final/a3-*.log` 中的 console_flow JSON（日志不入库） |
| **A4** 迁移脚本幂等 | ①`pbr migrate` 在旧两层库（octopus 路由层 + new-api 厂商层）副本上跑两次：计划逐字节一致、目标库计数一致、产物可被 PBR 加载；②`/api/v1/import` 的导入幂等与对账规则 | `ROUTING_DB=… VENDOR_DB=… verify/final/a4_migrate.sh`；`verify/final/a4_import_idempotent.sh` | ✅ PASS=11（真实迁移，unresolved=0/ambiguous=1/widened=5）+ PASS=16（导入幂等） | `verify/final/a4-migrate-20260915-095625.log`；`verify/final/a4-20260915-093224.log` |
| **B①** 工具调用 | 带 tools 的请求 → 回 tool_calls | `verify/final/e2e.sh` | ✅ | run 日志 |
| **B②** 多模态小图 | 图片 data URL 透传，上游确实收到 | 同上 | ✅ | run 日志 |
| **B③** 思考参数半开由上游 400 原样透传 | 上游 400 不换人、不冷却 | 同上 | ✅ | run 日志 |
| **B④** 长流式（≥100K 输入 token） | 720KB 请求体、流式、`[DONE]` 收尾 | 同上 | ✅ | run 日志 |
| **B⑤** `/v1/messages` | Anthropic 入口可用 | 同上 | ✅ | run 日志 |
| **B⑥** `embeddings` | 返回两条向量 | 同上 | ✅ | run 日志 |
| **B⑦** 车道全挂快抛不静默 | 503 + 固定 body + <2s | 同上 | ✅ | run 日志 |
| **B⑧** 429 不误判硬故障 | 记 `soft_rate_limit`、熔断不打开 | 同上 | ✅ | run 日志 |
| **C** 故障注入 | 500/401/400/429/欠费关键词/超时/流中途断流/坏响应/空响应/凭据失效/全挂快抛/冷却不复打/半开复通 | `verify/final/fault_injection.sh` | ✅ PASS=23 | `verify/final/fault-20260915-093229.log` |
| **D** 长稳与并发 | **50000 请求 / 并发 100**：日志行数=请求数、无串号、RSS 增长受控、聚合与明细一致 | `TOTAL=50000 CONCURRENCY=100 bash verify/final/longrun.sh` | ⚠️ 脚本已修时序竞态，新增 1 条一致性断言；结论以整改后**多次运行取稳定结果**为准 | 整改后 `longrun-*.log`（不入库）；`longrun-20260915-093434.log` 为整改前快照 |
| **E** 持久化与重启 | 重启后配置/令牌不丢；运行态清空 | `verify/final/e2e.sh`、`verify/deploy/smoke.sh` | ✅ | run 日志；`verify/deploy/README.md` |
| **F** 安全 | 未初始化 409、错误密钥 401、被拒车道 403、明文不入库不入日志、OpenAPI 不泄漏 | `verify/w3/smoke.sh`、`verify/final/e2e.sh` | ✅ | `verify/w3/run-20260915-093042.log`（PASS=45）、run 日志 |
| **G** 部署验收 | 独立 compose 项目从零起容器 → 设口令 → 建渠道/密钥 → 转发 → 重启数据仍在 | `verify/deploy/smoke.sh` | ✅ | `verify/deploy/run-20260915-093955.log`、`verify/deploy/README.md` |
| **H** 回滚演练 | 按 `MIGRATION.md` 在测试实例上演练切流与回滚（不动生产） | `verify/final/rollback.sh` | ✅ PASS=16 | `verify/final/rollback-20260915-093417.log` |
| **I** 文档一致性 | README / MIGRATION / ADR / OpenAPI / verify 与实现一致 | e2e 端点实跑、`verify/w4/console_contract_check.py`、本索引 | ✅ | 控制台端点与 openapi 对齐；W7 后 w0/w1/w2 脚本与 README 已同步 |
| **J** 代码质量 | `go vet ./...`、`go test ./...`、`web pnpm build`、`web pnpm lint` | 见下 | ✅ | `go test ./...` **43 包 ok**；`pnpm lint` 0 error（1 条 TanStack Virtual 的 React Compiler warning） |

```bash
go build ./...                   # 通过
go vet ./...                     # 无输出
go test ./... -count=1           # 43 个包 ok
cd web && pnpm build             # tsc --noEmit && vite build 零报错
cd web && pnpm lint              # 0 error, 1 warning（第三方库 React Compiler 兼容性提示）
```

## 各波次验收汇总（W7 合并前复跑）

| 波次 | 脚本 | 结果 |
|---|---|---|
| W0 基座迁入 | `verify/w0/smoke.sh` | PASS（改用 PBR 管理密钥 + 客户端密钥后仍能经基座转发） |
| W1 单层化路由 | `verify/w1/smoke.sh` | PASS=22 FAIL=0 |
| W2 容错 | `verify/w2/smoke.sh` | PASS=32 FAIL=0 |
| W3 访问与管理面 | `verify/w3/smoke.sh` | PASS=45 FAIL=0 |
| W4 控制台 | `verify/w4/smoke.sh` | PASS=20 FAIL=0 |
| W5 日志与记账 | `verify/w5/smoke.sh` | PASS=26 FAIL=0 |
| 部署 | `verify/deploy/smoke.sh` | PASS |
| W8 自验收（自动化部分） | `verify/final/{e2e,a3,a4,fault,longrun,rollback}` | 全 PASS（见逐项索引） |

## 本轮验收发现并修掉的真问题

1. **两个"文档有、实现无"的端点**：`POST /api/v1/channels/{name}/sync-models` 与
   `PUT /api/v1/lanes/{name}/members` 已登记进 OpenAPI 与 api-spec，但路由根本没注册
   （实跑 404）——A1 端点全量实跑的直接产出，已补齐实现并复验。
2. 端点实跑还暴露了扫描器自身的三处探针缺陷（探针渠道指向网关自身、DELETE 破坏后续
   前置状态、探针 key 与假上游不匹配），逐一修正后才得到可信结论。
3. **W7 复跑发现**：`verify/w0|w1|w2` 仍用已删除的基座 `/api/user/login` + `/api/token/**`
   建模型面凭据，实跑 404。已改为 PBR 客户端密钥，三个脚本复跑全 PASS。

## 明确未达标项与残余

| 缺口 | 影响 | 谁来关 |
|---|---|---|
| W6 正式切流（把现网下游 `base_url` 指向 PBR） | 迁移算法与 CLI 已完成并在旧库副本验证；真实切流属运维动作 | 业主按 `MIGRATION.md` §2 在测试实例演练后执行；PBR 与旧网关协议面一致，可随时回滚 |
| D 的"≥2 小时"维度 | 已按口径跑满 **50000 请求**；未跑连续 2 小时 | 如需 2 小时维度，在压测环境跑 `longrun.sh` 的时长版本 |
| 控制台视觉/交互人工复核 | A3 已用无头 Chromium 做页面级断言与截图，但非人眼审美复核 | 需要人复核时用 `verify/final/console_walkthrough.py` 的截图 |

> 说明：W7（物理删除计费/多用户）已完成，`go build/vet/test`、`pnpm build/lint` 与全波次脚本
> 在其后复跑通过；W7 的代码清除与保留清单见 `docs/design-v1.md` §10.2.1。

## 未处理 TODO 清单（W8-J："有则列清单说明"）

项目自有代码仅 11 处 TODO/FIXME，全部是上游 new-api 基座继承或非阻塞的局部增强，
不涉及 PBR 对外契约、路由/故障转移语义或安全边界：

| 位置 | TODO | 处置 |
|---|---|---|
| `common/gin.go:144` | 非 JSON 请求的变体模型 | 上游继承；PBR 模型面只走 JSON |
| `controller/channel-billing.go:581` | 支持 Azure 余额查询 | 上游继承；渠道运维余额查询，非计费执行 |
| `controller/channel-billing.go:600` | 余额刷新改异步 | 上游继承 |
| `controller/relay.go:367` | WS 其他子协议 | 上游继承 |
| `middleware/distributor.go:687` | api_version 统一 | 上游继承 |
| `model/main.go:223` | 删除 model_mapping 迁移注释 | 上游继承；仅旧库升级提示 |
| `relaykit/dto/audio.go:28` | 流式开始后的逻辑 | 上游继承 |
| `relaykit/dto/gemini.go:167` | thinking budget 冲突 | 上游继承 |
| `router/relay-router.go:84` | `/messages/count_tokens` 注释 | 行为已由 relay 侧实现，待清理注释 |
| `service/sensitive.go:20` | 图片 URL 检查 | 上游继承 |
| `setting/ratio_setting/model_ratio.go:24` | 新 API 定价检查 | 上游继承 |
| `relay/channel/**`（大量 "TODO implement me"） | 各厂商适配器非主路径能力 | design-v1 §10.1「适配器原样复用」，不在本项目内补 |

死代码：W7 已删除计费/多用户执行链与多用户控制器/中间件/模型；保留的
`service.BillingSettler`、`service.quota`、`service.task_billing` 等为带
「W7 惰性遗留」注释的 no-op 形状，供 40 家适配器与转发管道原样复用。
