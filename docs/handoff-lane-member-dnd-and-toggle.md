# PBR 交接文档：车道成员拖拽排序 + 成员级启停开关

> **用途**：把这两项功能完整交接给另一个执行者（人或 AI）。本文自带全部事实、锚点、陷阱与闸口，
> 不需要再读一遍代码库即可开工。
> **基线**：`main` @ `0315a95`（工作区干净）。文中行号均以该提交为准，若已漂移请用函数名/符号名定位。
> **执行者请先读**：`AGENTS.md`（仓库协作规则）、`docs/design-v1.md` §7、`docs/routing-spec-v1.md` §1–§2、
> `docs/ui-spec-v1.md` §6.3、`docs/api-spec-v1.md` §4.2、`docs/test-spec-v1.md` §8。

---

## 0. 任务概述与验收标准

| # | 需求 | 验收标准（可测） |
|---|---|---|
| A | 车道成员**拖拽排序**（保留现有上移/下移按钮） | 在车道编辑器里拖动成员行可改变顺序；保存后 `GET /api/routes/{model}` 的成员顺序与拖拽结果一致（`priority` 降序） |
| B | 车道成员**单独开关**（临时不想用某渠道就关掉） | 关闭某成员后：该成员**不参与选路**（请求不会打到它）；`GET /api/lanes/{name}/health` 能看出它是"被人工关闭"而非冷却/熔断；重新打开后立即恢复参与选路；**关闭成员不写冷却、不影响熔断器状态** |

**关键约束**：A 与 B 都必须走**真实 UI 操作**验收（L3，`verify/e2e-ui/user_journey.py`），不得只用 API 代劳。

---

## 1. 已核准的现状事实（不要重新推导）

### 1.1 数据模型

| 符号 | 位置 | 要点 |
|---|---|---|
| `LaneMember` | `model/lane.go:192` | 字段：`Id` / `LaneId` / `ChannelId` / `UpstreamModel` / `PublicAlias` / `Priority` / `Overrides(string, JSON)`。**当前没有 `Enabled` 字段** |
| `LaneRelayOverrides` | `model/lane.go:51` | 成员级**六键**覆盖，6 个 `*int`；`nil` = 继承车道。**它不是通用开关容器** |
| `ParseLaneRelayOverrides` | `model/lane.go:163` | 空串 → 零值；非法 JSON → 零值（静默回落） |
| `RouteMember` | `model/lane.go:203` | 选路/展示共用的解析后成员；`Overrides string` 是 `json:"-"`（**不对外暴露**） |
| `ResolvedRoute.EffectiveConfig` | `model/lane.go:245` | 把成员六键覆盖叠加到车道六键上 |
| `resolveExactRoute` | `model/lane.go:641` | 车道 → `ResolvedRoute` 的唯一解析入口 |
| `UpsertLane` | `model/lane.go:524` | 保存车道时**先删光该车道全部 `LaneMember` 再整体重插**（不是逐条 diff 更新） |
| `AutoMigrate` | `model/main.go:334` | `&LaneMember{}` 已在列 → **新增列自动迁移**，无需手写迁移脚本 |

### 1.2 选路（改这里才会真的生效）

| 符号 | 位置 | 要点 |
|---|---|---|
| `routeSnapshotSignature` | `internal/route/route.go`（搜函数名） | **快照内容签名**，只含影响选路的字段。**新增影响选路的成员字段必须加进这个签名**，否则改配置不触发快照重建、运行期用旧成员链 |
| `chooseFailoverLocked` | `internal/route/route.go:536` | failover 主循环：按 `s.order` 遍历，用 `availabilityOf` 判定 |
| `pickManualLocked` | `internal/route/route.go`（搜函数名，约 :598） | manual 只走 `active_member`；**刻意不参与冷却**（人工指定的成员没有替代者），但仍过熔断 |
| `availabilityOf` | `internal/route/runtime.go:386` | 返回 `availOK` / `availProbeReady` / `availSkip` |
| `SkipReason` | `internal/route/runtime.go:417` | 返回 `circuit_break` / `cooldown` / `skipped`（写进 `attempts[].status`） |
| `recordSkipLocked` | `internal/route/route.go:483` | 记录"为什么没用这个成员" |
| `memberKeyOf` | `internal/route/runtime.go:356` | 运行态键 = `channelId:upstreamModel` |
| `MemberHealth` | `internal/route/runtime.go:712` | `GET /lanes/{name}/health` 的成员形状，含 `Available bool` |
| `laneHealthCounts` | `internal/api/routes.go:52` | 用 `MemberHealth.Available` 统计 → 喂给 `GET /models` 的 `degraded` / `healthy_member_count` |
| `countAvailableMembers` | `model/lane.go`（搜函数名，约 :903） | 只按"渠道存在且启用"统计 → 喂 `available_member_count` |

**语义要点**：`degraded=true` 的判定是 `total>0 && healthy==0`。成员开关**不能**让"全部成员被人工关闭"表现为"上游故障"——两者在排障时必须可区分。

### 1.3 管理 API 契约

| 符号 | 位置 | 要点 |
|---|---|---|
| `laneMemberPayload` | `internal/api/lanes.go:21` | 请求成员形状：`channel` / `upstream_model` / `public_alias` / `priority` / `overrides(json.RawMessage)` |
| `buildLaneMember` | `internal/api/lanes.go:312` | 成员构建与校验。**注意**：`overrides` 用 `json.Unmarshal` 解到 `LaneRelayOverrides`，**Go 默认忽略未知字段** → 往 overrides 塞新键但不改结构体会被**静默丢弃** |
| `laneResponse` | `internal/api/lanes.go:39` | `GET /lanes/{name}` 的成员形状，`overrides` 经 `jsonObject` 回传 |
| `GetRoute` | `internal/api/routes.go:72` | `GET /routes/{model}` 只回 `channel` / `channel_enabled` / `upstream_model` / `upstream_override` / `public_alias` / `priority`。**不回 `overrides`、不回 `member_id`** |
| `LaneMemberConfig` | `internal/api/config_lifecycle.go:49` | `/export` `/import` 的成员形状，**含 `overrides`** |
| 写端点 | `router/pbr-router.go:72-73` | `PUT /lanes/{name}` 与 `PUT /lanes/{name}/members` 都已注册 `DryRunPreview`（`?dry_run=true` 返回 diff，不落库） |
| 审计 | `internal/api/lanes.go:204` | `writeAudit(c, action, "lane", name, response)` |

### 1.4 前端现状

| 符号 | 位置 | 要点 |
|---|---|---|
| `ComposerMember` | `web/src/features/routes/components/lane-composer.tsx:81` | 草稿态成员：`id`（React key，**稳定标识，不能把 channel/upstream 拼进去**，否则改名会重挂载导致输入框失焦）/ `channel` / `upstreamOverride` / `resolvedUpstream` / `publicAlias?` |
| `moveMember` | 同文件 `:288` | 现在只有上移/下移（`delta` ±1） |
| `save` | 同文件 `:316` | 提交载荷：`upstream_model: m.upstreamOverride.trim()`、`priority: members.length - index`。**不发送 `overrides`** |
| `PBRMemberInput` | `web/src/features/routes/api.ts:228` | 前端提交类型：`channel` / `upstream_model?` / `priority`。**无 overrides 字段** |
| `laneMemberStates` | `web/src/features/routes/lib/lane-runtime-display.ts:60` | 成员运行态徽章（Cooldown / Circuit open / Half-open / Affinity / Probing / Current） |
| **可复用的原生拖拽实现** | `web/src/features/channels/components/dialogs/param-override-editor-dialog.tsx:1455-1512` | `handleDragStart` / `handleDragOver` / `handleDrop` / `resetDragState` + `reorderOperations`。**原生 HTML5 DnD，仓库没有装任何 dnd 库**——照这个模式写，不要引新依赖 |

---

## 2. 任务 A：成员拖拽排序

**纯前端改动**（后端顺序语义已完备：`priority` 数字大者优先，数组顺序即写库顺序）。

### 改动清单

1. `web/src/features/routes/components/lane-composer.tsx`
   - 给 `ComposerMember` 行加 `draggable`，接 `onDragStart` / `onDragOver` / `onDrop` / `onDragEnd`。
   - 抽一个纯函数 `reorderMembers(list, sourceId, targetId, position: 'before'|'after')` 放进 `web/src/features/routes/lib/`（**纯函数便于单测**），参照 `param-override-editor-dialog.tsx` 的 `reorderOperations`。
   - 拖拽落点用鼠标相对行高的一半判定 before/after（与参考实现一致）。
   - **保留**上移/下移按钮（无障碍与精细操作需要；且现有 L3 用例依赖它们）。
   - 拖拽过程中必须给视觉反馈（被拖行 + 落点指示线），否则用户不知道会插到哪。
   - 拖拽后要触发 `props.onDirtyChange?.(true)`（现有 `markDirty` 已封装，复用它）。

2. **不要动**后端与 API 契约。

### 验收

- 单测：`reorderMembers` 的边界（拖到首位/末位/自身/相邻交换）。
- L3：`verify/e2e-ui/user_journey.py` 增补"拖拽成员改变顺序 → 保存 → `GET /api/routes/{model}` 顺序一致"。
- 无障碍：拖拽不能是唯一途径（按钮必须保留），且拖动元素需可键盘聚焦。

---

## 3. 任务 B：成员级启停开关

### 3.1 方案选型（**必须二选一，推荐方案 1**）

#### 方案 1（推荐）：给 `LaneMember` 加独立字段 `Enabled`

- **理由**：这是**一等概念**（人工干预选路），不是六键数值覆盖。塞进 `LaneRelayOverrides` 语义错误（那个结构体是 `*int` 数值覆盖），且会踩 3.3 的坑。
- 语义：`Enabled=false` 的成员**不参与选路**，但**仍保留在成员链里**（不删、不写冷却、不动熔断器），随时可打开。

#### 方案 2（不推荐）：塞进 `overrides`

- 必须同时改 `LaneRelayOverrides` 结构体（否则被静默丢弃，见 §1.3），且会与"六键覆盖"语义混在一起。
- 且**必踩 3.3 的既有隐患**（控制台保存会清空 `overrides`）。

### 3.2 后端改动清单（按方案 1）

1. **模型** `model/lane.go`
   - `LaneMember` 加字段。**不要用裸 `bool`**（见 §4 陷阱 1）：推荐 `Disabled bool`（语义取反，零值 = 启用，存量行天然正确），或 `Enabled *bool`。
   - `RouteMember` 加对应字段（供选路与展示读取）。
   - `resolveExactRoute`（`:641`）与 `displayMembersForLane`（搜函数名）填该字段。
2. **选路** `internal/route/`
   - `routeSnapshotSignature`（`route.go`）：**必须把新字段加进签名**，否则开关切换不生效。
   - `chooseFailoverLocked`（`:536`）：在 `availabilityOf` 之前先判"人工关闭 → 跳过"，并调用 `recordSkipLocked` 留痕。
   - `pickManualLocked`（约 `:598`）：`active_member` 被人工关闭时应视为"无可用"（不要静默换人，与 manual 语义一致）。
   - `SkipReason`（`runtime.go:417`）：新增一个可区分的原因（如 `"disabled"`），**不要复用 `cooldown`/`skipped`**——排障时必须能区分"人工关闭"与"故障跳过"。
   - `MemberHealth`（`runtime.go:712`）：新增字段暴露开关状态（如 `Disabled bool`），并让 `Available` 对人工关闭的成员返回 `false`。
   - `availabilityOf`（`:386`）：**不要**在这里把开关并进冷却/熔断判定——它是运行态函数，开关是配置态。判定放调用方更清晰。
3. **管理 API** `internal/api/`
   - `laneMemberPayload`（`lanes.go:21`）：加 `enabled`（或 `disabled`）字段。
   - `buildLaneMember`（`:312`）：解析并落库该字段；注意**默认值**：请求未提供时对"新建成员"应默认启用，对"既有成员"应保留原值（避免控制台漏发字段就把成员关掉）。
   - `laneResponse`（`:39`）：回传该字段。
   - `GetRoute`（`routes.go:72`）：**回传该字段**——否则会重演本次刚修的 `upstream_override` 数据丢失 bug（前端读不到 → 保存时清掉）。
   - `LaneMemberConfig`（`config_lifecycle.go:49`）：`/export` `/import` 带上该字段（否则导出再导入会丢开关状态）。
4. **持久化**：`UpsertLane`（`model/lane.go:524`）整体重插成员，新字段随成员结构体自动带上，无需额外改动；`AutoMigrate` 自动加列。

### 3.3 ⚠️ 既有隐患：控制台保存会清空成员 `overrides`

**已核实**（`grep overrides web/src/features/routes/api.ts` 无结果）：

- `laneMemberPayload.Overrides` 是 `json.RawMessage`，前端**从不发送** `overrides` → 反序列化后为 `nil` → `buildLaneMember` 的 `len(m.Overrides) > 0` 为假 → 成员 `Overrides` 落库为空串。
- 因为 `UpsertLane` 是整体重插，所以**每次控制台保存车道，全部成员的六键覆盖都会被清空**。这与本次刚修的 `upstream_override` 是**同一类数据丢失路径**（前端读不到/不回传 → 保存即清空）。
- **当前线上无实际损失**（已核实：现网没有任何成员配过 `overrides`），但**做任务 B 时必然踩到**——如果按方案 2 把开关放进 `overrides`，保存一次就被清掉。

**执行者必须**：顺手把 `overrides` 纳入前端往返（`PBRMemberInput` 加字段、`ComposerMember` 保存带上、`GET /routes` 回传），或明确在交付说明里记为遗留风险。

### 3.4 前端改动清单

1. `web/src/features/routes/api.ts`
   - `PBRMemberInput`（`:228`）加开关字段（若按方案 2 还要加 `overrides`）。
   - `PBRRouteMember`（`:56`）加对应只读字段，让编辑器能回显。
2. `web/src/features/routes/components/lane-composer.tsx`
   - `ComposerMember`（`:81`）加草稿字段；`save`（`:316`）带上它。
   - 成员行加开关控件（建议 `Switch`，若仓库无该组件则用带 `aria-label` 的 `Checkbox`/`Button`）。
   - **关闭的成员要有明显视觉区分**（整行降透明度/加"已停用"徽章），否则用户会以为它还在参与选路。
   - 关闭成员时给出后果提示（"该成员不再参与故障转移"）。
3. 运行态展示：`lane-runtime-cell.tsx` / `lane-runtime-display.ts`（`:60`）增加"人工关闭"状态徽章，与 Cooldown / Circuit open 并列且**颜色可区分**。
4. `web/src/features/routes/components/lane-card.tsx`：卡片的成员摘要里把关闭的成员标灰（现在只对"渠道停用"标灰）。

### 3.5 验收

- 单测（Go，`internal/route/`）：关闭成员后 failover 跳过它并留痕；`manual` 的 `active_member` 被关闭时返回"无可用"；开关切换触发快照重建（`routeSnapshotSignature` 覆盖到新字段）。
- 单测（Go，`internal/api/`）：`PUT /lanes/{name}/members` 写入开关并回读；`GET /routes/{model}` 回传开关；`?dry_run=true` 不落库；导出/导入保留开关。
- 单测（web）：`ComposerMember` 开关往返、拖拽后 `priority` 生成正确。
- L2（`verify/e2e-api/ops_runbook.sh`）：车道成员开关写入 + 回读。
- L3（`verify/e2e-ui/user_journey.py`）：**真实 UI** 关掉某成员 → 端到端请求不再命中它 → 打开后恢复。
- L4：`verify/final/fault_injection.sh` 回归（确认开关不干扰故障转移语义）。

---

## 4. 陷阱清单（按踩坑概率排序）

1. **GORM 零值陷阱（高危）**：`Lane.Enabled` 上方有明确注释——**不要给 `Enabled bool` 加 gorm default，GORM 会忽略零值**。若给 `LaneMember` 加裸 `Enabled bool`，存量行迁移后全是 `false` = **所有成员被静默关闭、全站 503**。用 `Disabled bool`（取反）或 `*bool`，并为存量行显式写初值。
2. **快照签名遗漏（高危）**：新字段不进 `routeSnapshotSignature` → 开关改了但选路用旧快照，表现为"点了没反应"。
3. **前端不回传即清空（高危，本仓已发生两次）**：`upstream_override` 刚因此丢过数据（见 §3.3）。**任何加到成员上的新字段，都必须同时：`GET /routes` 回传 + 前端读入草稿 + 保存时带回**。
4. **`json.Unmarshal` 静默忽略未知字段**：往 `overrides` 塞新键但不改结构体，会被无声丢弃（`buildLaneMember:340`）。
5. **`members` 整体替换语义**：`PUT /lanes/{name}/members` 与 `UpsertLane` 都是**全量替换**。漏发一个成员就是删掉它；漏发字段就是清空它。
6. **`ComposerMember.id` 不能含 channel/upstream**：它被用作 React key，改名会重挂载行、输入框失焦（代码里有实测记录）。
7. **manual 模式的特殊性**：`pickManualLocked` 刻意不参与冷却。加开关时不要顺手把冷却逻辑也并进去。
8. **`degraded` 语义**：`/models` 的 `degraded=true` 现在表示"全部成员不可用"。人工关闭全部成员时，排障要能区分"我关的"和"上游挂了"。
9. **不要引 dnd 库**：仓库无 dnd 依赖，用原生 HTML5 DnD（参考 `param-override-editor-dialog.tsx`）。
10. **i18n**：所有新文案必须 `t()` 包裹并同步 `web/src/i18n/locales/*.json`（跑 `pnpm i18n:sync`）。缺键会让 `src/i18n/__tests__/literal-key-coverage.test.ts` 直接红。
11. **`web/dist/index.html` 是被跟踪的真实构建产物**：改完前端要 `pnpm build` 后提交它，否则 `verify/console-assets.sh` 会 FAIL。

---

## 5. 闸口与证据（`docs/test-spec-v1.md` §8）

改动涉及 `internal/api/lanes.go`、`model/lane.go`、`internal/route/`、`web/src/features/routes` → **必跑**：

```bash
# L0 静态（Go）
go build ./... && go vet ./... && go test ./...

# L0 静态（web）
cd web && pnpm typecheck && pnpm lint && pnpm test && pnpm build \
  && pnpm format:check && pnpm copyright:check

# 产物一致性（前端改动后必跑）
bash verify/console-assets.sh

# L2 真实 API 运维
bash verify/e2e-api/ops_runbook.sh

# L3 真实用户操作（需 chromium + python websocket-client）
python3 verify/e2e-ui/user_journey.py

# L4 故障注入（选路语义改动必跑）
bash verify/final/fault_injection.sh
```

**收尾闸口**（合入 main 前）：L0 全量 + L1 全量 + L2 + L3 全绿。日志写 `verify/**/run-<时间戳>.log`（不入库）。

---

## 6. 文档同步清单（与代码同一提交）

| 文档 | 要改什么 |
|---|---|
| `docs/design-v1.md` §7.1 / §7.7 | 成员结构加开关字段；说明"人工关闭"与冷却/熔断的关系 |
| `docs/routing-spec-v1.md` §1.2 / §2.2 | 持久配置加字段；failover 选择算法加"人工关闭 → 跳过"分支与 `SkipReason` 新取值 |
| `docs/api-spec-v1.md` §4.2 | 成员请求/响应加开关字段；示例更新 |
| `docs/ui-spec-v1.md` §6.3 | 编排器加"拖拽排序"与"成员开关"；运行态徽章加"人工关闭" |
| `docs/test-spec-v1.md` §4 / §5 | L3 用户路径加拖拽与开关两条；L2 加开关回读 |

**流程分级**：这是**大任务**（改公共契约 + 跨模块 + 改状态机）→ ①先改设计文档（单独 `docs:` 提交）→ ②生成拆分文档（`breakdown:`）→ ③并行实现（`feat:`/`fix:`）→ ④删拆分文档（`chore:`）。

---

## 7. 不要做的事

- **不要**改 `reference/` 下的上游源码。
- **不要**给成员加 `weight` 字段或恢复 `weighted`/`round_robin` 模式（已按 ADR 删除，传入返回 422）。
- **不要**用 `overrides` 承载开关（方案 2 的坑见 §3.3、§4.1）。
- **不要**把开关做成"删除成员"的别名——需求是**可逆的临时停用**。
- **不要**在没跑 L3 的情况下声称 UI 验收通过（本仓有"client 半验收止于服务端证据"的历史教训）。
- **不要**提交任何凭据/密钥；`.env` 与 `pbr.db` 不入库。

---

## 8. 附：本次会话已修复的相关 bug（避免重复劳动或误判）

以下已在 `main` 修复并部署，**执行者不必重做**，但要知道背景（任务 B 的 §3.3 隐患是同一类问题的第三例）：

| 提交 | 内容 |
|---|---|
| `23cfd95` | 价格输入框保留小数点草稿（受控归一化吃掉 `0.`，打不出 `0.1`） |
| `c5d6903` | `GET /routes` 有显式改名时无条件回传 `upstream_override`（**根因**：条件恒假 → 控制台读不到 → 保存清空全部成员上游真名 → 车道必 404/503） |
| `fb68f08` | 车道编排器成员行内联"未声明模型"告警（防车道名回落） |
| `740a883` / `438aa16` | 使用日志用户列按设计文档回退 + 列守卫测试对齐 |
| `46b1b95` / `3c9d363` / `45d0c63` | 使用日志 i18n 缺键补全、SelectValue i18n 守卫、过时测试对齐 |

**线上配置已恢复**：`<hermes-lane>` 8 个成员、`<model-x>` 3 个成员的上游真名已回填，全库审计受损成员 0。
