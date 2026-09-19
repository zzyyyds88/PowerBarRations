# 拆分：数据看板收敛与路由页只列车道

> 依据：`docs/adr/0007-routes-lists-lanes-only.md`、`docs/ui-spec-v1.md` §5/§6.2/§6.3/§6.6、
> `docs/design-v1.md` §7.7、`docs/api-spec-v1.md` §9、`docs/test-spec-v1.md` §4。
> 完成后删除本文件（chore 提交）。

## 目标（用户裁决）

1. **路由页只列真实车道**：渠道声明与车道彻底分开，删车道即卡片消失；新建车道一律手填路由键。
2. **模型调用分析以调用次数为主**：卡片顺序 = 调用次数 / 成功率 / Token / 上游花费，表格默认按调用次数倒序。
3. **删除 API 信息面板全链**（后端 option + `/api/status` 字段 + 前端面板 + 系统设置分节 + i18n）。
4. **侧边栏只保留一个「数据看板」入口**，落地 `/dashboard/overview`，模型/成本为页内 Tab。

## 包 A：后端删除 console_setting.api_info 全链

- `setting/console_setting/`：整包删除（`config.go`、`validation.go`）——删后无任何消费者。
- `controller/misc.go`：去掉 `api_info_enabled` 字段与 `api_info` 注入，去掉 `console_setting` import。
- `controller/option.go`：删除 `case "console_setting.api_info"` 校验分支与 import。
- `model/frontend_option_migration.go`：删除 `ApiInfo → console_setting.api_info` 迁移项与
  `transformLegacyAPIInfo`；保留 `theme.frontend` 归一化。迁移函数在 target 已删后仍要能清理
  历史遗留的 `ApiInfo` / `console_setting.api_info` 两个 option 行（幂等删除）。
- `model/frontend_option_migration_test.go`：删除 ApiInfo 相关用例，保留 theme 用例。
- 验证：`go build ./... && go vet ./...` 零输出；`go test ./model/... ./controller/... ./setting/... -count=1`。

## 包 B：前端 /routes 只渲染真实车道

- `web/src/features/routes/index.tsx`：`models` 过滤 `source !== 'unconfigured'`；空态文案改为
  "还没有车道"；`openCard` 去掉 unconfigured 分支（只剩编辑）。
- `web/src/features/routes/components/lane-card.tsx`：`editing` 恒为 true，删除未配车道分支与
  「新建车道」按钮文案；删除「未配车道 · 不可调用」提示。
- `web/src/features/routes/__tests__/routes-page.test.tsx`：更新为"unconfigured 不渲染"。
- i18n：清理仅此处使用的键（`No lane · not callable`、`{{count}} candidate channels` 等），
  7 locale 同步。

## 包 C：模型调用分析以调用次数为主

- `web/src/features/dashboard/components/cost/cost-dashboard.tsx`：
  - 卡片顺序改为 调用次数 / 成功率 / Token / 上游花费。
  - 表格默认排序按 `requests` 倒序（成本节仍按 cost 倒序）。
  - 空态文案按分节区分：模型调用分析用"暂无调用数据"，成本统计用"配置上游单价后显示成本"。
- i18n 新增 `Call count`（调用次数）；`Requests` 文案保留给其它页。
- 测试：`cost-dashboard.test.tsx` 补卡片顺序与默认排序断言。

## 包 D：删除 API 信息面板

- 删 `web/src/features/dashboard/components/overview/api-info-panel.tsx`、`api-info-item.tsx`、
  `web/src/features/dashboard/lib/api-info.ts`。
- `overview-dashboard.tsx`：去掉 `ApiInfoPanel`、`useApiInfo`、`useDashboardContentVisibility`，
  布局收敛为"性能健康面板 + 首个 API 请求预览"。
- `web/src/features/dashboard/hooks/use-status-data.ts`：整文件删除（唯一消费者已删）。
- `types.ts`：删 `ApiInfoItem`/`PingStatus`/`PingStatusMap`。
- 删系统设置「API 地址」分节：`api-info-section.tsx`、`section-registry.tsx` 的 `api-info` 项、
  `content/index.tsx` 的 `console_setting.api_info*` 默认值与 legacy 映射、`types.ts` 的
  `ContentSettings` 字段。
- `overview-layout.test.tsx`：去掉 api_info 桩与相关断言。
- i18n：清理 `API Info`、`API Addresses`、`API URL` 等孤儿键（7 locale）。

## 包 E：侧边栏单一数据看板入口 + 概览页补 Tab

- `web/src/hooks/use-sidebar-data.ts`：删 `Overview` 与 `Dashboard` 两条，合并为一条
  `title: t('Dashboard')`、`url: '/dashboard/overview'`。
- `web/src/features/dashboard/index.tsx`：概览不再提前 return；三节（概览 / 模型调用分析 /
  成本统计）统一走 Tab，落地 `/dashboard/overview`。这样删掉侧边栏的
  `/dashboard/models` 条目后，模型与成本仍可达（页内 Tab）。
- `use-sidebar-config.ts`：`/dashboard/models` 等映射保留（Tab 路由仍需命中 console 模块）。
- `web/src/hooks/__tests__/`：补"只有一个数据看板入口且落地 overview"断言。

## 包 F：验收

- 闸口：`go build ./... && go vet ./... && go test ./... -count=1`；
  `cd web && pnpm typecheck && pnpm lint && pnpm test`。
- L2/L3：`verify/e2e-api/ops_runbook.sh`、`verify/e2e-ui/user_journey.py` 按 ADR 0007 更新
  （user_journey 动作 4 改为从页头「New lane」手填路由键；断言 unconfigured 不在路由页）。
- 重建 `web/dist`、部署到 5700 实机验证：删车道卡片消失、看板入口/标题一致、概览无 API 信息。
