# 控制台收敛 v2（W1–W9）验收记录

日期：2026-09-16
分支：feat/console-convergence（自 fdf06ed 设计基线 + 7a37290 拆分文档）
提交序列：5a81fef W1 → cd33d66 W3 → 370ae07 W4 → a5c64c8 W5 → 3e74005 W8 →
b2140a4 W9 → c3b9e28 W2 → b4652e9 W6 → 7a9f993 W7

## 1. 全量验证（最终提交后实跑）

| 检查 | 结果 |
|---|---|
| `go build ./...` | exit 0 |
| `go test -count=1 ./...` | 43 包 ok，0 FAIL |
| `cd web && pnpm typecheck` | exit 0（tsgo -b） |
| `cd web && pnpm build` | exit 0（dist 产物生成） |
| `cd web && pnpm test` | 107 passed / 3 failed（110 files）；6 failed / 1355 passed |
| `cd web && pnpm lint` | 201 errors / 45 warnings（全仓既有基线） |
| `cd web && pnpm i18n:sync` | 已同步（无 diff） |

### 既有基线失败（非本轮引入，已在 main@cd97364 复现）

| 失败项 | baseline（main） | 本轮 |
|---|---|---|
| `sidebar-config.test.tsx`（2） | 失败 | 失败（同一用例） |
| `server-error-notifications.test.ts`（2） | 失败 | 失败（同一用例） |
| `setup-guide.test.tsx`（2） | 失败 | 失败（同一用例） |
| `pnpm lint` 全仓 | 213 errors / 46 warnings | **201 errors / 45 warnings（净减 12/1）** |

结论：本轮未引入新的失败；全仓 lint 错误数反而下降。

## 2. 契约与开放 API 同步

- `internal/api/config_lifecycle.go`：`/capabilities` 的 `lane_modes` 收敛为
  `[failover, manual]`；`/lanes/seed` 描述改为"按渠道 id 升序"。
  `openAPIPaths()` 本就无 `/deployments`、`/vendors` 条目，`router`
  coverage 测试保持绿。
- `internal/api/docs/api-guide.md`（经 `//go:embed` 出现在 `/doc` 与 `/llms.txt`）：
  修正 `PUT /api/lanes/{name}/members`、`"隐式链"`、渠道 body 里的 `"priority":10` 三处
  文档漂移；观测小节含 `group_by=channel_model`；§4.1 增"先探测再落库"示例。
- `docs/api-spec-v1.md` / `routing-spec-v1.md` / `ui-spec-v1.md` / `README.md`：
  W0 提交（fdf06ed）已同步，本轮实现与之一致。

## 3. 数据库迁移（幂等、失败不阻塞启动）

- `model/channel_priority_weight_migration.go`：`DROP COLUMN channels.priority/weight`
- `model/model_vendor_migration.go`：`DROP COLUMN models.vendor_id`
- `model/main.go`：`BackfillPBRChannelModelStats()` 一次性回填渠道×模型小时桶，
  成功后写 option 标记防重放，无明细跳过。

## 4. 七条诉求落点

1. 渠道 `priority`/`weight` 彻底删除（DB/API/导出/表单/建议链排序）→ 5a81fef
2. 模型页单一平面列表 + 行内操作；侧边栏合并 → c3b9e28
3. 部署（io.net）前后端物理删除 → cd33d66
4. 供应商（Vendors）前后端物理删除 → 370ae07
5. 「广场展示」`square_state` 彻底删除 → a5c64c8
6. 定价＝上游成本口径（模型详情无下游计费编辑器） → b4652e9
7. 成员链手工增删排序 + 删 `weighted`/`round_robin` 与成员 `weight` → 5a81fef（后端）+ 7a9f993（前端）
8. 渠道模型自动探测 → 3e74005
9. 看板「渠道 × 模型」成本 → b2140a4
## 5. 真机部署与端到端核对（2026-09-16）

在监听 `127.0.0.1:5700` 的实例上重建二进制并重启，用管理密钥逐条核对：

| 诉求/契约 | 实测 |
|---|---|
| `GET /api/v1/capabilities` | `lane_modes = ['failover','manual']` |
| 渠道 PUT 带旧 `priority/weight` | HTTP 200，响应不含两字段 |
| 车道 `mode:"weighted"` | HTTP 422，`code=invalid_mode` |
| `GET /api/v1/routes/{model}` 成员 | 键为 `channel/priority/upstream_model`，无 `weight` |
| `GET /api/v1/stats?group_by=channel_model` | 200；非法值 400 `validation_failed` |
| `/api/deployments/`、`/api/vendors/` | 404（已物理删除） |
| `GET /api/v1/export` | 8 渠道，0 个含 priority/weight |
| `GET /llms.txt`（嵌入 api-guide） | 含 `channel_model`；无 `/members` 与"隐式链" |
| 控制台产物 | 嵌入 `index.c18da8540c.js`；含上游单价/渠道关联/成员链手工管理/渠道×模型；无部署/广场/weighted 串 |

数据库迁移在现网库上实际执行：`channels.priority`、`channels.weight`、`models.vendor_id`
三列已 DROP（7 渠道、12 车道数据保留）。旧二进制与数据库备份留在
旧二进制与迁移前数据库各留一份备份（路径随部署环境）。

### 部署中发现并修复的缺陷（提交 29134a1）

列 DROP 后仍有两处运行期 SQL 引用旧列，会在真机上抛 `no such column: priority`：
`model/channel.go` 的默认渠道排序、`controller/channel_upstream_update.go` 的批量 Select
字段。两者及 `ChannelTag` 载荷、字段分类表一并清理，修复后全量 Go 测试 44 包绿。
