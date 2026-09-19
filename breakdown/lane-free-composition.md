# 拆分：车道成员自由编排（ADR 0006）

> 依据 [ADR 0006](../docs/adr/0006-lane-free-member-composition.md) 与
> design/routing/ui/api/test 各 spec。完成并验收后本文件按流程删除（chore:），只留 Git 历史。

## 目标

1. 渠道声明/新增模型**绝不自动建车道**（已是现状，补测试固化）。
2. 车道成员候选放开为**任意启用渠道的任意已声明模型**；跨渠道跨模型、无需同名；
   **同一渠道可出现多次**（去重键 = `(渠道, 上游真名)`）。
3. `/routes` 页改为 **octopus 式卡片网格**；新建/编辑共用**两栏编排器**：
   左栏「渠道 → 模型」可搜索选择器，右栏已选成员有序列表（拖拽/上下移、删除、
   改上游真名、清空）；**不提供「自动添加」**；默认 `upstream_model` 显式填所选模型名。
4. 部署新二进制到线上 `:5700`。

## 契约结论（已定，实现不得发挥）

- 后端 `PUT /api/v1/lanes/{name}` **不校验**成员是否声明了该路由键——已确认无需改校验；
  成员去重/候选范围是**前端**职责。
- 候选数据源 = `GET /api/v1/channels`（每渠道 `models`）+ `GET /api/v1/models`；
  **不新增端点**。
- `GET /api/v1/routes/{model}` 的 `members`/`candidates` 仅作展示/推荐，不限制范围。
- 成员唯一键 = `(channel, upstream_model)`。

## 工作流

### WS-A 后端：固化"自由编排 + 不自动建车道"（Go 测试）
- [ ] `internal/api/lanes_test.go`（或就近新增）：
  - 同一渠道两个不同 `upstream_model` 可作为两个成员写入并回读；
  - 成员 `channel` 未声明该路由键也能建车道（池化）；
  - `PUT /channels/{name}` 新增模型后 `GET /models` 该模型 `source=unconfigured`、
    `routable=false`，且 `GET /lanes` 不新增车道。
- [ ] 无需改 handler 逻辑；若发现校验阻断则最小修正。

### WS-B 前端 API 层
- [ ] `web/src/features/routes/api.ts`：
  - 新增 `listPBRChannelCatalog()`：`GET /api/v1/channels`，返回
    `{name, enabled, models: string[]}[]`（分页参数按需；自用规模一页取全）。
  - 成员去重工具：`memberKey({channel, upstream_model})`。

### WS-C 前端编排器（新建/编辑共用）
- [ ] 新组件 `web/src/features/routes/components/lane-composer.tsx`（替换
      `new-lane-dialog.tsx` + `model-routing-panel.tsx` 的编辑区）：
  - 字段：路由键（新建可编辑 / 编辑只读）、模式下拉、六键折叠区（取 `lane_defaults`）。
  - 左栏：渠道折叠 + 搜索 + 模型项；点击加入右栏；已加入项禁用。
  - 右栏：有序成员列表，上移/下移、删除、改上游真名、清空。
  - 保存：`PUT /api/v1/lanes/{name}`，`priority` 按位置生成（首位最大）。
  - manual 模式：`active_member` 必选，未选拦截保存。
  - 空成员链拦截保存。
  - 草稿/放弃确认（沿用现有 ConfirmDialog 模式）。
- [ ] 移除旧 `new-lane-dialog.tsx` 与 `model-routing-panel.tsx`（或改造为复用编排器）。

### WS-D 前端 /routes 卡片网格
- [ ] `web/src/features/routes/index.tsx` 改为卡片网格（`VirtualizedGrid` 或 CSS grid）：
  卡片展示路由键、状态徽章、成员顺序摘要、运行态（复用 `LaneRuntimeCell`）；
  卡片操作：编辑成员链、删除车道；页头「新建车道」；保留悬空成员清理条。
- [ ] 数据源不变：`GET /api/v1/models` + `GET /api/v1/lane-summaries` + 运行态。

### WS-E 前端测试
- [ ] 编排器单测：跨渠道跨模型加入、同一渠道两个模型、去重、排序、改名、空链拦截、
      默认 `upstream_model` = 模型名、无「自动添加」。
- [ ] 卡片页测试：卡片渲染、状态徽章、编辑/删除入口、空态。
- [ ] 更新 `routes-page.test.tsx` / `new-lane-dialog.test.tsx` 至新结构。

### WS-F 验收与部署
- [ ] L0：`pnpm typecheck`、受影响文件 oxlint、`pnpm test`（受影响）、`go build ./...`。
- [ ] L1：`go test ./internal/api/... ./model/... ./internal/route/...`。
- [ ] L2/L3：按 test-spec §8 跑受影响层（可先 L2 runbook + L3 user_journey）。
- [ ] 构建新二进制，备份 `/root/pbr-data/pbr.db`，替换 `/root/pbr-data/pbr` 并重启 `:5700`；
      回读 `/api/v1/health`、`/api/v1/models`，用真实渠道建两条车道验证可调用。

## 风险

- `web/dist` 是 tracked 构建产物，构建与 `go build` 不得并发（test-spec §8.1）。
- 线上库有真实渠道与密钥，**只读回读为主**；建车道前先确认是否会影响现有调用。
- 卡片网格若引入拖拽库需评估依赖（优先用现有组件/原生 DnD 或上移下移）。
