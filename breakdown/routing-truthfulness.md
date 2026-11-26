# 路由真值收口任务拆分（2026-11-26）

> 一次性拆分文档；全部任务合并后删除。真源：`docs/design-v1.md` + `docs/routing-spec-v1.md` + `docs/api-spec-v1.md` + `docs/usage-spec-v1.md` + `docs/ui-spec-v2.md`。
> 本文只写任务、owner、文件边界与验收；不重复规范正文。

## 目标

- 修掉“正常渠道被冷却/熔断跳过”的路由误判（Batch A）。
- 修掉日志/统计的安全、脱敏、数据真相与保留问题（Batch B）。
- 修掉 DB/备份/auth reset/迁移/健康观测的韧性缺口（Batch C）。
- 不做：重写 new-api 基座、宿主机存储修复、计费/多用户/多节点。

## Batch A：路由正确性与状态机

| ID | 任务 | 主要文件 | 验收 |
|---|---|---|---|
| A1 | `State.Next` 显式中止；429 `Retry-After>60s` 原样透传 + `Retry-After` 头 | `internal/route/route.go`、`controller/relay.go`、`middleware/pbr_route.go` | 单测：超 cap 不 wait、不换人、返回 429；cap 内仍等待/重试 |
| A2 | timeout 一次性耗尽本请求预算；panic 跳过已试/ConfigError/OverrideError | `internal/route/route.go` | 单测：同请求不重选 timeout 成员；panic 不双记账 |
| A3 | 亲和/正常/panormal/manual 共用可用性 helper；`recordAffinitySkips` continue；manual 查 ConfigError/ChannelEnabled | `internal/route/route.go`、`runtime.go` | 单测：亲和不直选坏配置；manual 无可用；skipped 链完整 |
| A4 | 424→upstream_reject；请求级 400 不计 `reject_streak`；阈值/冷却可配 | `internal/route/route.go`、`runtime.go`、`internal/api/system_options.go` | 单测：424 换人不冷却；content filter 不累计；配置越界拒绝 |
| A5 | FailsByKind 严格连续；滚动窗口恢复清零；在飞成功不关 open；冷却/减速单调；member_recovered 配对 | `internal/route/runtime.go` | 单测：交替失败、恢复后首败、在飞成功、并发短失败、半开两次成功 |
| A6 | stale key 迁移删除旧键；过期亲和投影 null；Health 增 `fails_by_kind`/`override_error`；manual ProbeLease | `internal/route/runtime.go`、`internal/api/route_health.go` | 单测：二次迁移不复活；health 字段/available；前端类型 |
| A7 | 事件发布移出锁；`CurrentCircuitSettings` 入口取一次；配置上界/NaN 校验 | `internal/route/runtime.go`、`internal/api/system_options.go`、`middleware/pbr_wiring.go` | 单测：订阅者回调不自死锁；越界值拒绝/回落 |
| A8 | 跨格式流式统一 `StreamOutcomeError`；`failResponsesStream` 不吞错 | `relay/channel/openai/chat_via_responses.go`、`responses_via_chat.go` | 单测：timeout/cancel/scanner 分类正确；未提交不 200 |
| A9 | 自动禁用白名单先于 `IsChannelError`；timeout 不自动禁用 | `service/channel.go` | 单测：invalid_key true；response_time_exceeded/5xx false |
| A10 | 其他流式适配器接入 `StreamOutcomeError`（可列债务） | `relay/channel/{dify,baidu,xai,openai}` | 按实际排期；至少记录 |

## Batch B：安全与数据真相

| ID | 任务 | 主要文件 | 验收 |
|---|---|---|---|
| B1 | `error_summary`/stream reject/channel-test 统一脱敏 + UTF-8 截断 | `controller/relay.go`、`channel-test.go`、`relay/helper/stream_reject.go`、`relaykit/types/error.go` | 单测：SetMessage 后仍掩码；日志/导出无 sk-/内网 URL |
| B2 | 明细+上下文同事务；outbox 表 + 单 worker 异步聚合（幂等） | `model/pbr_request_log.go`、`pbr_request_context.go`、新 outbox model/worker、`main.go` | 单测：单连接不锁死；崩溃/重试不重复计数；backlog 指标 |
| B3 | stats rebuild 先 drain outbox；只覆盖有明细的桶，保留已 prune 历史；`preserved_buckets` | `model/stats_rebuild.go`、`internal/api/logs.go` | 单测：prune 后 rebuild 不丢旧桶；dry-run 同口径 |
| B4 | 保留：log-cleanup 改清 `pbr_request_logs`/contexts；批删 + 每日 ScheduledSystemTaskHandler | `service/system_task.go`、`model/pbr_request_log.go`、`web` 设置页 | 单测：250 行分批/断点/取消；UI 目标表正确 |
| B5 | 读路径不再吞错：keys/channel status/model status/channel speed | `internal/api/{keys,channels_status,model_status,channel_speed}.go`、前端 | 错误注入返回 5xx/degraded；前端 `—` |
| B6 | `total_ms` 毫秒；attempts 上界；成功标签统一；提交后 reject 过滤 | `model/pbr_request_log.go`、`controller/relay.go`、`model/lane.go` | 单测：1900ms 不被覆盖；attempts 截断标记；标签一致 |
| B7 | stats 索引 + sargable 下界；keys 成本时间界；webhook secret 掩码；同步错误体上限；DSN 校验 | `model/pbr_request_log.go`、`model/option.go`、`controller/option.go`、`service/error.go`、`common/*` | 索引存在/查询计划；options/all 无 secret；超长错误体截断 |
| B8 | usage 物品名 ≤256B；统一 `TruncateUTF8` | `internal/usage/prompt.go`、`common`、各截断点 | 单测：中文/emoji 边界；长度 ≤256 |

## Batch C：DB 与运维韧性

| ID | 任务 | 主要文件 | 验收 |
|---|---|---|---|
| C1 | 统一 SQLite DSN/path helper；`auth reset` 复用 DSN + 停服检测 | `common/*`、`internal/authutil/cli.go` | 单测：路径优先级/DSN 参数/运行中拒绝 |
| C2 | 备份恢复 runbook + `scripts/backup_pbr_db.sh`（`.backup`+quick_check）；删热 tar | `deploy/README.md`、`verify/deploy/README.md`、`README.md`、`scripts/` | smoke：在线备份→停服恢复→数据在 |
| C3 | SQLite `ConnMaxLifetime=0`；`LOG_SQL_DSN` 同文件防呆 | `model/main.go` | 单测/启动检查 |
| C4 | DB 写失败观测：atomic + `/api/health` degraded + metrics | `model/*`、`internal/api/system.go`、`internal/metrics` | 单测：写失败后 health degraded；指标契约 |
| C5 | price_band 迁移消除启动整表重建；schema 漂移测试 | `model/pbr_price_band_migration.go`、`pbr_request_log.go` | 旧库迁移→二次启动 no-op；新库列一致 |
| C6 | 结构质量：SSE `For`→`Get`、死代码、注释漂移、clock、配置上界 | `internal/api/route_events.go`、`internal/route/*` | `-race`；定向单测 |
| C7 | 配置上界/NaN 校验集中到 `model.ValidateOptionValue`；导入路径复用 | `model/option.go`、`internal/api/system_options.go` | 越界/NaN 拒绝；PUT options/all 不能绕过 |

## 执行顺序

1. Batch A（A1→A2→A3→A4→A5→A6→A7→A8→A9），每完成 2–3 项跑 `go test ./internal/route ./service ./relay/channel/openai`。
2. Batch B（B1 先，B2/B3/B4 一起，B5–B8）。
3. Batch C（C1/C2/C3/C4/C5/C6/C7）。
4. 收尾：L0 + L1 + L2 + L3 + L4 全绿；删除本文；`merge --no-ff`。

## 验收命令

```bash
go build ./... && go vet ./... && go test ./... -count=1
cd relaykit && go test ./... -count=1
cd web && pnpm typecheck && pnpm lint && pnpm test && pnpm format:check && pnpm copyright:check && pnpm build
bash verify/e2e-api/ops_runbook.sh
python3 verify/e2e-ui/user_journey.py
bash verify/final/e2e.sh
bash verify/final/fault_injection.sh
```
