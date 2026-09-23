# 任务拆分：成员只存所选模型（ADR 0008）

> **一次性过程文档**，任务闭合后删除（AGENTS.md 大任务流程第④步）。
> 契约真源不在这里——在 `docs/adr/0008-member-stores-selected-model.md` 与
> `docs/routing-spec-v1.md` §1.1/§1.2、`docs/api-spec-v1.md` §3/§4.2/§5.2/§5.7、
> `docs/ui-spec-v1.md` §6.3、`docs/test-spec-v1.md` §3.1。
> 本文只回答「谁改哪些文件、按什么顺序、跑什么命令收口」。冲突时以上述规范为准。
> 基线：`main` @ `75b7971` + `fd8c2f8`（docs 已提交）。

## 0. 为什么这么切

这是一条**垂直切片**：`model` 的字段语义 → `internal/route` 的解析与标签 → `internal/api`
的契约 → 前端编排器。逐层依赖，**单一 owner 串行做完**；前端与验收脚本在契约冻结后可并行。

**关键点：契约是破坏性变更**（写端点 `upstream_model` → `model`；读端点去掉
`upstream_override`）。本项目无对外兼容包袱，**不做双写、不留旧字段兼容入口**。

## 1. 工作包与 owner 边界

| 包 | owner | 独占文件 | 依赖 |
|---|---|---|---|
| **W1 后端** | 主 agent | `model/lane.go`、`model/*migration*.go`、`internal/route/**`、`internal/api/**` + 对应 `*_test.go` | docs 已冻结 |
| **W2 前端** | 子代理 | `web/src/features/routes/**`、`web/src/features/channels/**`（映射文案）、`web/src/i18n/locales/*.json` | 契约已冻结，可与 W1 并行 |
| **W3 验收脚本** | 子代理 | `verify/**`（成员载荷） | 契约已冻结，可与 W1 并行 |

W1/W2/W3 文件集互不相交。集成与全部闸口由主 agent 串行执行。

## 2. W1 后端（顺序固定：model → route → api）

### 2.1 `model/lane.go`

1. `LaneMember.UpstreamModel` → **`Model`**（列名 `model`），语义 = 成员所选、由该渠道声明的模型名。
   gorm tag 保持 `not null`；**不加 default**（同 `Disabled` 的教训）。
2. `RouteMember.UpstreamModel` **保留**（它是解析后的真名，供展示/转发/日志），
   `UpstreamOverride` **删除**。
3. `effectiveUpstreamModel(channel, model)`：签名去掉 `routeKey`，规则改为
   `ModelMappingMap()[model] ?? model`。**查表键恒为 model**。
4. 两处填充点（`resolveExactRoute`、`displayMembersForLane`）改传 `m.Model`。
5. `suggestedMembers` 的推荐项：`UpstreamModel = effectiveUpstreamModel(c, modelName)`
   （推荐项的"所选模型"就是路由键本身）。
6. **新增 `migrateLaneMemberModelSemantics(db)`**（`model/lane_member_model_migration.go`）：
   幂等、失败只记日志不阻塞启动（对齐 `migrateDropAbilitiesTable` 口径）。逐成员：
   - 值命中某映射**右值** → 写回对应**左键**；
   - 未命中 → 原值写回；
   - 空值 → 写回车道路由键（复现旧"回落 `mapping[路由键]`"行为）。
   同批重写 `lanes.active_member`：`channel/真名` → `channel/模型`。
   迁移后必须让 `resolveExactRoute` 结果与迁移前逐位一致（除"当前值恰是映射左键"的碰撞）。
7. 在 `model/main.go` 的迁移序列里注册（AutoMigrate 之前）。

### 2.2 `internal/route/`

1. `memberKeyOf` 不变（键 = `channelId:upstream_model`，即解析后真名）。
2. `memberLabel`（`channel/真名`）→ **`channel/模型`**（成员身份，供 `active_member` 与 attempts）。
   **已知影响（必须写进交付说明，不是隐患）**：`attempts[].member` 是对外留痕，改标签后
   **历史日志仍是旧标签 `channel/真名`**，与新日志的 `channel/模型` 并存。本项目无对外兼容
   包袱、历史日志只作追溯（design §11），故不回溯改写；`ops_runbook.sh` 里按 `channel` 前缀
   匹配的断言需同步改成新标签。
3. `routeSnapshotSignature` 成员段：`UpstreamOverride` 字段消失，改含 `m.UpstreamModel`（真名）
   与渠道映射变化。**渠道映射改动必须让签名变化**（新增断言）。
4. `activeMemberIndex` 的 `memberLabel` 匹配随之改。

### 2.3 `internal/api/`

| 文件 | 改什么 |
|---|---|
| `lanes.go` | `laneMemberPayload.UpstreamModel` → **`Model`**；`buildLaneMember` 去掉 `enabled` 三态之外的字段解析，新增**去重校验**（`(channelId, model)` 重复 → `422 duplicate_member`）；`laneResponse` 成员回 `model` + `upstream_model`（派生） |
| `routes.go` | `GetRoute` 成员恒回 `model` / `upstream_model`（派生）/ `enabled` / `overrides` / `member_id`；**删除 `upstream_override`**；推荐项 `model` = 路由键 |
| `model_metadata.go` | `ListLaneSummaries` 成员加 `model`（`upstream_model` 保留为派生真名） |
| `config_lifecycle.go` | `LaneMemberConfig.UpstreamModel` → **`Model`**（JSON `model`）；导出**只给 `model`**，不给真名——真名可由 `model_mapping` 重算，导出冗余值反而会让"改映射后导入旧文件"把旧真名带回来；导入按 `model` 解析 |
| `apierr` | 新增 `CodeDuplicateMember = "duplicate_member"` |

### 2.4 W1 收口命令

```bash
go build ./... && go vet ./... && go test ./... -count=1
```

必须包含的硬断言：① 改渠道映射后**不重存车道**，`GET /routes` 的 `upstream_model` 立即变化；
② 池化车道（车道名 ≠ 模型名）下解析用的是**模型名**查表；③ `(channel, model)` 重复 → 422；
④ 迁移函数：构造"真名=映射右值/非映射值/空值"三种存量行，迁移后解析结果与迁移前一致。

## 3. W2 前端（子代理）

1. `features/routes/api.ts`：`PBRRouteMember` 加 `model`；`PBRMemberInput` 的
   `upstream_model` → `model`，删 `upstream_override`。
2. `lib/lane-member-upstream.ts`：`defaultUpstreamForModel` 语义变为**展示用派生**
   （保留函数名或改为 `resolveUpstreamForModel`），不再用于写入。
3. `components/lane-composer.tsx`：
   - `ComposerMember`：删 `upstreamOverride`，保留 `resolvedUpstream`（只读展示）。
   - 成员行**删除改名输入框**，改为只读「解析后的上游」文本。
   - 左栏 `ModelPickerItem` 的 `added` 判定改为按 **`(channel, model)`**（不再是解析后真名）。
   - 保存载荷 `{channel, model, public_alias, priority, enabled, overrides}`。
4. `components/lane-editor-dialog.tsx`：草稿映射改 `model`。
5. `features/channels/**`：`model_mapping` 的文案从「路由键 → 上游真名」改为「模型名 → 上游真名」。
6. i18n：删除「使用渠道映射」等失效键；**7 个 locale 全同步**。

### W2 收口命令（**禁止跑 `pnpm build`**）

```bash
cd web && pnpm typecheck && pnpm lint && pnpm test && pnpm format:check
```

## 4. W3 验收脚本（子代理）

**闸口内脚本**（AGENTS.md「验收命令」列出的 5 个）的成员载荷 `"upstream_model"` → `"model"`：
`verify/e2e-api/ops_runbook.sh`、`verify/final/e2e.sh`、`verify/final/fault_injection.sh`、
`verify/final/hermes_acceptance.sh`（`verify/e2e-ui/user_journey.py` 不直接构造成员载荷，走 UI）。

**闸口外但同一改动面的脚本**（`verify/final/a4_import_idempotent.sh`、`rollback.sh`、`a3_console.sh`、
`longrun.sh`）：它们当前不在 AGENTS.md 的验收命令里，但**载荷形状同样过期**。一并改，
避免留下"改了契约、脚本静默失效"的尾巴；若某脚本已不可运行，在交付说明里记明，不伪装成通过。

**禁改**：`verify/w2/`、`verify/w3/` 是历史归档（AGENTS.md 禁止触碰），不因其载荷过期而修改。

`ops_runbook.sh` 里 `upstream_override` 相关断言改为 `model`（写回）与 `upstream_model`（派生真名）；
attempts 标签断言由 `channel/真名` 改为 `channel/模型`。

## 5. 集成顺序与闸口（主 agent，**全部串行**）

1. 收 W2/W3 产出，逐 diff 审。
2. `go build ./... && go vet ./... && go test ./... -count=1`。
3. `cd web && pnpm typecheck && pnpm lint && pnpm test && pnpm format:check && pnpm copyright:check`。
4. **串行**：`pnpm build` → `bash verify/console-assets.sh` → `bash verify/e2e-api/ops_runbook.sh`
   → `python3 verify/e2e-ui/user_journey.py` → `bash verify/final/fault_injection.sh`
   → `bash verify/final/e2e.sh` → `bash verify/final/hermes_acceptance.sh`。
   **L3 与任何 `go build` 类脚本不得并发**。
5. L3 结束 `git checkout -- web/dist/index.html`。

## 6. 停止条件

- 迁移无法做到"行为保持"（三种存量情形任一解析结果改变且非碰撞）→ 停，报告。
- 发现规范与代码新矛盾 → 停，报冲突与证据，等裁决。
- 需要动 `reference/**`、加 dnd 依赖、恢复成员级覆盖 → 一律禁止。
- 部署私有数据不得进入任何提交、脚本、日志或结论。
