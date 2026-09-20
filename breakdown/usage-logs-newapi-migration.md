# 使用日志：迁移到 new-api UI + 适配 PBR 数据源

> 大任务拆分文档。实现完成后由 `chore:` 删除。基线见 `docs/ui-spec-v1.md §6.6`、
> `docs/api-spec-v1.md §5.5/§4.4`、`docs/test-spec-v1.md`。
>
> 目标：把继承但未启用的 new-api `UsageLogsTable` + `common-logs-columns` 接到
> PBR 统一日志表 `/api/v1/logs`，替换自写的 `PBRLaneLogsSection`。

## 背景与已定决策

- 前端 `web/src/features/usage-logs/components/` 下已继承 new-api 的完整表格
  （`UsageLogsTable` + `common-logs-columns` + `DetailsDialog` + `CommonLogsFilterBar`），
  但 `index.tsx:143` 把 `pbr` 分节硬切到自写的 `PBRLaneLogsSection`，继承的表格读
  基座 `/api/log/`（PBR 已停写，空表），是死路径。
- 分页：`/api/v1/logs` 加偏移分页（`page`/`page_size`/`total`），游标保留。
- 列适配：前端 mapper 把 `PBRRequestLog` 塑成 `UsageLog` 形状 + 构造 `other` JSON
  （含 `other.pbr` 扩展块），复用 `common-logs-columns` 骨架；Cost 列改读
  `estimated_cost`；PBR 独有"车道列"插入；attempts 链进 `DetailsDialog`。

## 子任务

### A. 后端偏移分页 + logResponse 补字段
**分支**：`feat/usage-logs-offset-pagination`
**依赖**：无（契约已定，先行）
**文件**：`internal/api/logs.go`、`model/pbr_request_log.go`

1. `model/pbr_request_log.go`
   - `PBRRequestLogFilter` 加 `Page int`、`PageSize int`。
   - 抽公共 `applyPBRLogFilter(*gorm.DB) *gorm.DB`，把 lane/channel/key/model/
     success/since/until/beforeId 的 where 构造收口（`ListPBRRequestLogs` 与
     `CountPBRRequestLogs` 共用，避免重复 + 口径分叉）。
   - `ListPBRRequestLogs`：`filter.Page > 0` 走偏移
     （`Offset((page-1)*pageSize).Limit(pageSize)`，不加 +1）；否则原游标
     （`BeforeId` + `Limit+1`）。偏移模式上限 `page_size ≤ 100`（在 handler 层校验）。
   - 新增 `CountPBRRequestLogs(filter PBRRequestLogFilter) (int64, error)`：用
     `applyPBRLogFilter` 后 `Count`。
2. `internal/api/logs.go`
   - `ListLogs`：读 `page` query（`pageParams` 扩展或新增 helper）。传了 `page` →
     偏移分支：`filter.Page`/`filter.PageSize`（默认 page=1/page_size=50，上限 100），
     `CountPBRRequestLogs` 得 `total`，`ListPBRRequestLogs` 偏移，返回
     `{items, total, page, page_size}`。未传 `page` → 原游标分支不动。
   - `logResponse`：补输出 `type`、`channel_id`(=MemberChannelId)、`token_id`、
     `user_id`、`ip`（model 已有，当前漏输出）。
3. 验收
   - `go build ./...`、`go vet ./...` 过。
   - `go test ./internal/api/ ./model/` 覆盖：偏移（page=2/page_size=10 切片+total）、
     游标回归（不传 page 走 next_cursor）、`logResponse` 新字段、filter 组合。

### B. 前端数据层：mapper + 数据源 + 类型
**分支**：`feat/usage-logs-pbr-mapper`
**依赖**：A 的 API 契约（mapper 可独立先写，API 客户端方法按契约写，A 完成后联调）
**文件**：新建 `web/src/features/usage-logs/lib/pbr-mapper.ts`；
`web/src/features/usage-logs/types.ts`、`lib/utils.ts`、`pbr/pbr-logs-api.ts`

1. `pbr/pbr-logs-api.ts`：新增
   `listPBRRequestLogsPaged(params: {page;page_size;lane;channel;key;model;success;since;until}):
   Promise<{items: PBRRequestLogListItem[]; total: number; page: number; page_size: number}>`。
   游标方法 `listPBRRequestLogs` 保留。
2. `lib/pbr-mapper.ts`：
   - `mapPBRLogToUsageLog(log: PBRRequestLogListItem): UsageLog`：
     `created_at=Date.parse(ts)/1000`、`token_name=key_name`、`model_name=request_model`、
     `use_time=total_ms/1000`、`channel=channel_id`、`channel_name=channel`、`quota=0`、
     `content=error_summary`、`other=JSON.stringify({cache_tokens←cache_read_tokens,
     cache_creation_tokens←cache_write_tokens, frt←ttft_ms,
     is_model_mapped/upstream_model_name←upstream_model,
     admin_info.use_channel←attempts.map(a=>a.member),
     pbr:{lane,route_source,upstream_model,error_kind,error_summary,http_status,total_ms,
     estimated_cost,attempts,total_attempts,inbound_format,success}})`。
   - `mapPBRLogsResponse(res): GetLogsResponse`：拼 `{items: UsageLog[], total, page, page_size}`。
3. `types.ts`：`LogCategory = 'common' | 'pbr'`；`LogOtherData` 加可选
   `pbr?: {lane;route_source;upstream_model;error_kind;error_summary;http_status;total_ms;
   estimated_cost;attempts:PBRAttempt[];total_attempts;inbound_format;success}`。
   `LOG_CATEGORY_LABELS` 加 `pbr`。
4. `lib/utils.ts` `fetchLogsByCategory`：加 `logCategory === 'pbr'` 分支，调
   `listPBRRequestLogsPaged`，`mapPBRLogsResponse` 后返回。common 分支不动。
5. 验收：`pnpm -C web typecheck` 过；mapper 单测覆盖字段映射 + other JSON 结构。

### C. 前端 UI 层：分节/列/筛选/详情/路由
**分支**：`feat/usage-logs-newapi-ui`
**依赖**：B（mapper/类型/数据源）
**文件**：`index.tsx`、`components/usage-logs-table.tsx`、`lib/columns.ts`、
`components/columns/common-logs-columns.tsx`、新建 `components/pbr-logs-filter-bar.tsx`、
`components/dialogs/details-dialog.tsx`、`constants.ts`、
`routes/_authenticated/usage-logs/$section.tsx`

1. `index.tsx:143`：`pbr` 分节改用 `<UsageLogsTable logCategory='pbr' />`，
   删 `PBRLaneLogsSection` import。
2. `usage-logs-table.tsx`：`useTableUrlState` 的 `columnFilters` 按 `logCategory`
   派发——pbr 用 `[{model_name→model},{token_name→key},{lane→lane},{success→success}]`；
   `toolbar` 按 logCategory 派发（pbr 用 `PBRLogsFilterBar`，common 用原 `CommonLogsFilterBar`）。
3. `lib/columns.ts` `useColumnsByCategory`：加 pbr 分支，返回 `useCommonLogsColumns`
   + 在 Model 列后插 **Lane 列**（读 `other.pbr.lane`）。
4. `common-logs-columns.tsx`：
   - **Cost 列**改读 `parseLogOther(log.other)?.pbr?.estimated_cost`（元，`toFixed(4)`），
     不再读 `quota`/`formatLogQuota`。
   - **Details 列** `buildDetailSegments`：PBR 错误日志（`other?.pbr?.error_summary`）
     优先显示（红色）。
   - **Channel 列**：`use_channel` 已从 mapper 的 `admin_info.use_channel` 读
     （成员名字数组），重试链 Popover 复用；`channel`(id) 着色用 `channel_id`。
5. 新建 `pbr-logs-filter-bar.tsx`：复用 `LogsFilterToolbar` +
   `CompactDateTimeRangePicker`。字段：时间范围 + Lane + Channel(名) + Key + Model +
   Success(All/Success/Failed)。navigate 到 `section:'pbr'`，search 用 PBR 维度。
   不含敏感显隐/统计头（PBR 单用户无脱敏；rpm/tpm 留 TODO 接 `/api/v1/stats`）。
6. `details-dialog.tsx`：识别 `other.pbr` 时渲染 PBR 区块（字段网格 +
   `PBRAttemptTimeline`（复用 `pbr/components/pbr-attempt-timeline.tsx`，数据从
   `other.pbr.attempts` 取）+ error_summary）。否则原 new-api 形态。
7. `routes/_authenticated/usage-logs/$section.tsx`：`validateSearch` 的 Zod schema
   加 PBR search key（`lane`/`key`/`success` 可选）；`beforeLoad` 的 type 清理按 pbr 调整。
8. 验收：`pnpm -C web typecheck && pnpm -C web build` 过；L3 走查日志页能渲染、
   跳页、筛选、详情弹窗含 attempts。

### D. 清理
**分支**：`chore/usage-logs-cleanup`
**依赖**：C
**文件**：删 `web/src/features/usage-logs/pbr/components/pbr-lane-logs-section.tsx`；
删基座 `/api/log/` 路由（`router/api-router.go:83-86`）+ `controller/log.go`；
删 `web/src/features/usage-logs/api.ts`（基座客户端，common 分支无数据源）

1. 删 `pbr-lane-logs-section.tsx`（被 `UsageLogsTable` 取代）。保留
   `pbr-attempt-timeline.tsx`（DetailsDialog 复用）、`pbr-logs-api.ts`（数据层用）。
2. grep 确认无外部 consumer 依赖基座 `/api/log/` 后删路由 + controller；不确定则
   保留路由改 handler 返回 410 Gone。`controller/log.go` 删前确认无其它引用。
3. 删 `api.ts`（基座 `getAllLogs`/`getUserLogs`）；`fetchLogsByCategory` 的 common
   分支若已无引用一并删，否则标注 deprecated。
4. 验收：`go build ./...`、`pnpm -C web build` 过；`/api/log/` 返回 404 或 410。

### E. 测试
**分支**：`test/usage-logs-migration`
**依赖**：A-D
**文件**：后端 `_test.go`、前端 mapper 单测、L4 脚本

1. 后端 L1：`internal/api/logs_test.go` + `model/pbr_request_log_test.go`——
   偏移分页、游标回归、logResponse 字段、filter 组合。
2. 前端 L1：`lib/pbr-mapper.test.ts`——字段映射 + other JSON 结构。
3. L4：起实例，发含逃逸请求（P1 失败→P2 成功），验证跳页/总数/车道列/Cost 元/
   DetailsDialog attempts/lane+success 筛选。沿用 test-spec 步骤 7/5/6/6b/12。
4. 验收：`go test ./...` + `pnpm -C web test` 过；L4 实测结论记入 verify README。

## 并行度

- A（后端）与 B 的 mapper/类型部分可并行（B 用契约，不依赖 A 实现）。
- B 的 API 客户端方法联调要等 A 完成；mapper 单测可先行。
- C 依赖 B；D 依赖 C；E 贯穿 A-D。
- 建议：A 单独跑；B 起步后，C 等 B 的类型/mapper 落地即可起；D/E 最后。

## 完成后

`chore:` 删除本文件，merge 全部分支进 main（每个 `--no-ff`），清理分支，
main 干净。
