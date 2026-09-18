# 任务拆分：控制台按 New API 本地化（任务二剩余部分）

> 中间过程文档，实现完成后按流程删除（chore:），不入最终状态。
> 规范来源：`docs/ui-spec-v1.md` §6.3/§6.4（`2bc857a` 已覆盖旧禁令）、`docs/api-spec-v1.md` §5.7、仓库根 `AGENTS.md`、`web/AGENTS.md`。

## 用户原始要求（验收口径）

1. 渠道编辑有「获取模型列表」按钮 → 居中弹窗勾选回填 —— **已完成**（`1dc020e`）。
2. 渠道管理页列表/工具栏按 New API 样式 —— **本轮 Workstream B**。
3. 使用日志 + 任务日志更详细形态 —— **已完成**（`be07f14`）。
4. 模型管理页支持"自动匹配"（New API 可以，PBR 为什么不行）—— **本轮 Workstream D**。

通用要求：与 PBR 无关的选项一律不出现（多用户/角色、计费/额度/钱包/充值/兑换码/订阅、分组、厂商 vendors、task-plugins、drawing/MJ、system-info 页、优先级/权重）。

## Workstream 边界

| | 范围 | 可改文件 | 不可碰 |
|---|---|---|---|
| **B** | 渠道管理页表格与工具栏（协议列 4 项，删 ID 列） | `web/src/features/channels/components/channels-table.tsx`、`channels-columns.tsx`、`channels-primary-buttons.tsx`、`data-table-bulk-actions.tsx`、`channel-card.tsx`、`channels-provider.tsx`、`constants.ts`、`lib/**`、`__tests__/**`（含 `channel-type-badge.test.tsx`）；**独占** `web/src/i18n/locales/*.json` | `features/models/**`、`features/usage-logs/**`、任何 Go 代码、`docs/**` |
| **D** | 模型自动匹配（后端放开 + 前端匹配类型列/选择器） | `internal/api/model_metadata.go`、`internal/api/model_metadata_test.go`；`web/src/features/models/**`（含 `__tests__`） | `features/channels/**`、`web/src/i18n/locales/**`、`docs/**`、`model/**`、`controller/**` |

**i18n 归属（防并发写坏同一 JSON）**：只有 B 能改 7 个 locale 文件。D 一律用英文原句作为 `t()` 字面量（i18next 缺键回退到键本身，等于英文文案，测试不受影响），并在最终报告里**逐条列出新增的 `t()` 字面量**，由主线统一补 7 语种翻译。

## 各自的硬性约束

- **响应契约**：前端按裸响应写。成功读裸对象/`{items}`；失败靠 axios 拒绝 + `handleServerError`；**不得**引入 `response.success` 判断。稳定面 `{items,next_cursor}`，基座面 `{items,total,page,page_size}`，按实际调用的端点分别读。冲突读 `error.details.lanes` / `error.details.blocked`，`code` 可能是 `conflict` 或 `models_referenced_by_lanes`。
- **B 的协议列**：取值只能是 4 种协议（OpenAI 兼容 / OpenAI Responses / Anthropic / Gemini），**不是**约 50 个厂商类型；协议判定必须复用编辑弹窗已有的那一套映射（`type` + `other_settings.protocol`），禁止在列里另写一份。不在 4 协议内的旧渠道显示「自定义」，且列表不得静默改写其类型。
- **D 的后端改动**：删 `internal/api/model_metadata.go` 里 `name_rule != model.NameRuleExact` 的硬拒，改调 `model.ValidateMetadataValues`（越界 → 422 `validation_failed`）。稳定面 `GET /api/model-metadata` **不返回** `matched_count` / `matched_models`（保持轻量），但非精确条目的 `configured_channel_count` 必须按 `MatchesName` 命中集统计去重渠道数——用**一次内存预计算**，禁止逐条查库造成 N+1。控制台面 `/api/console/models/**` 已有命中集计算（`controller/model_meta.go`），不要重复实现。
- **D 的删除语义**：规则条目可以删目录记录本身；`?remove_from_channels=true` 仍只对 `name_rule=0` 允许（422 拒绝规则条目），与 `model/model_meta.go` 既有守卫一致——不要改动 `model/` 层。
- 新增/修改行为必须同步补测试（`web/AGENTS.md` §3.14）；改 TS/TSX 后跑 typecheck 与涉及文件的 lint，0 error。

## 验收（主线执行，子代理不要提交）

- 前端：`cd web && pnpm typecheck && pnpm lint && pnpm format:check && pnpm copyright:check && pnpm test && pnpm build`。
- 后端（D 影响时）：`gofmt -l $(git ls-files '*.go')` 为空、`go build ./...`、`go vet ./...`、`go test ./... -count=1`。
- 契约冒烟（隔离实例，端口 5798，勿动线上 5700）：`PUT /api/model-metadata/qwen3-` 带 `name_rule:1` 应 200 写后回读；`name_rule:4` 应 422；`GET /api/model-metadata` 应含该条目且无 `matched_count`；`GET /api/console/models/` 应给出该条目的 `matched_count`。
- 子代理**禁止 `git commit`**，禁止 `git add -A`（工作区可能同时有另一路改动）。
