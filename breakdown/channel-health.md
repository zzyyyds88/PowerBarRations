# 任务拆分：渠道健康详情（feat/channel-health）

> 规格正本：`docs/api-spec-v1.md` §5.3 + §6.12、`docs/ui-spec-v1.md` §6.4、`docs/test-spec-v1.md` §4 行 9。
> 本文件按 AGENTS.md 大任务流程生成，任务闭合后删除（`chore:` 提交）。

## 目标与边界

- 给运维/董事长一个"这个渠道现在通不通、最近表现怎么样"的只读监控面：管理面两个只读端点 + 控制台渠道页的「健康」抽屉。
- 明确不做：任何写动作（探活除外，走既有 `POST /channels/{name}/test`）；不访问上游（除探活按钮）；不改模型面/路由语义；不引入计费/余额语义。

## 工作流与文件属主（互不重叠）

| # | 范围 | 属主 | 文件 |
|---|---|---|---|
| W1 | 探活留痕（表 + 写 + 读 + 限额裁剪 + prune 联动） | 主 agent | `model/pbr_probe_log.go`（新）、`internal/api/route_health.go`（TestChannel 写记录）、`internal/api/logs.go`（prune 联动） |
| W2 | 两个只读端点 + 路由登记 + L1 测试 | 主 agent | `internal/api/channels_health.go`（新）、`internal/api/ops_routes.go`、`internal/api/channels_health_test.go`（新） |
| W3 | 控制台：API 客户端 + 「健康」入口 + 详情抽屉 + i18n(7) + vitest | 子代理 | `web/src/features/channels/**`、`web/src/i18n/locales/*.json` |
| W4 | 验收：L0/L1/L2（runbook 新步骤）/L3（第 9 条动作）/L4 复跑 | 主 agent | `verify/**`（除 W3 范围） |

## 契约要点（实现必须逐条对上 api-spec §6.12）

1. `GET /api/channels/health?window=24h|7d` → `{window, items:[{channel,enabled,type,lane_count,member_count,available_count,degraded,requests,failures,success_rate,estimated_cost,last_error_kind,last_probe?}]}`。
2. `GET /api/channels/{name}/health?window=` → `{channel{...},reference{lanes[]},runtime{member_count,available_count,members[]},window,summary{...},daily[],recent_failures[],probe_history[],upstream_updates{last_check_ts,pending_add,pending_remove}}`。
3. 数据源：运行态=进程内（`internal/route` 注册表）；窗口统计与 daily=小时聚合表；平均耗时=明细日志（prune 后回落 0）；探活历史=`pbr_probe_logs`（每渠道最近 200 条）；上游变动=渠道设置里上次探测快照；**两个端点都不访问上游、都无副作用**；`window` 非法 → 422 `validation_failed`；未知名 → 404 `channel_not_found`。
4. 渠道未被任何车道引用时 `runtime.members` 为空数组、`reference.lanes` 为空数组（不得省略字段）。

## 验收（W4 执行）

- L0：`go build ./... && go vet ./... && go test ./... -count=1`；`cd web && pnpm typecheck && pnpm lint && pnpm test`。
- L1：新端点的契约守卫（`router/openapi_coverage_test.go`、`envelope_coverage_test.go` 必须自动覆盖新路由）+ 行为测试（形状、窗口校验、404、探活记录写入与上限裁剪）。
- L2：`verify/e2e-api/ops_runbook.sh` 增补"渠道健康"步骤（两个端点 200 + 形状断言 + 探活后历史出现新记录）。
- L3：`verify/e2e-ui/user_journey.py` 增补第 9 条用户动作（打开抽屉 → 四块内容 → 切窗口 → 立即探活 → 历史新增）。
- L4：`verify/final/e2e.sh`、`fault_injection.sh`（回归）、`endpoint_sweep.py`（若按 openapi 自动覆盖则确认含新端点）。

## 停止条件

L0/L1/L2/L3 全绿 + L4 回归通过 → 删本文件（`chore:`）→ 合入 main。
