# 任务拆分：车道成员拖拽排序 + 成员级启停开关

> **一次性过程文档**，任务闭合后删除（AGENTS.md 大任务流程第④步）。
> 契约真源不在这里——在 `docs/design-v1.md` §7、`docs/routing-spec-v1.md` §1.2/§1.3/§2/§5.3/§7/§9、
> `docs/api-spec-v1.md` §4.2/§4.4/§5.2/§5.7/§6.4/§6.5、`docs/ui-spec-v1.md` §6.3、`docs/test-spec-v1.md` §3.1/§4/§5/§8。
> 本文只回答「谁改哪些文件、按什么顺序、跑什么命令收口」。冲突时以上述规范为准。
> 基线：`main` @ `8f9b306`。

## 0. 为什么这么切

后端是一条**紧耦合垂直切片**：`model` 的字段 → `internal/route` 的选路与健康形状 → `internal/api` 的契约接线，
逐层依赖，拆开并行会让两个代理在同一个语义上各写一半。所以后端 **单一 owner 串行做完**，
并行只用在真正互不干扰的两处：**前端（`web/**`）** 与 **L2 运维脚本（`verify/e2e-api/`）**。

L3 用户路径**不并行**：它要点真实 DOM，开关控件与拖拽手柄的选择器由前端包决定，
提前写只能靠猜选择器，等于给验收埋假通过。L3 由主 agent 在前端落地后写并实跑。

## 1. 工作包与 owner 边界

| 包 | owner | 独占文件（他人禁止触碰） | 依赖 |
|---|---|---|---|
| **W1 后端垂直切片** | 主 agent | `model/lane.go`、`internal/route/**`、`internal/api/**` + 对应 `*_test.go` | 无 |
| **W2 前端** | 子代理 | `web/src/features/routes/**`、`web/src/features/usage-logs/**`（attempts 状态色）、`web/src/i18n/locales/*.json` | 契约已定，可与 W1 并行 |
| **W3 L2 运维脚本** | 子代理 | `verify/e2e-api/ops_runbook.sh` | 契约已定，可与 W1 并行 |
| **W4 L3 用户路径** | 主 agent | `verify/e2e-ui/user_journey.py` | **W1 + W2 之后** |

W1/W2/W3 文件集互不相交，可并行。集成与全部闸口由主 agent 串行执行。

## 2. W1 后端（顺序固定：model → route → api）

### 2.1 `model/lane.go`

1. `LaneMember`（:192）加 `Disabled bool`。**不加 gorm `default`**（GORM 忽略零值；`Lane.Enabled` :180 的注释就是同一条约束）。
   零值 = 启用 → 存量行天然正确，`AutoMigrate`（`model/main.go:334`）自动加列，**不写迁移脚本**。
2. `RouteMember`（:203）加 `Enabled bool`（正向）。
3. 两处填充：`resolveExactRoute`（:641，成员循环约 :683-700）、`displayMembersForLane`（:806）。
   `Enabled = !m.Disabled`，取反只做在这两处，下游不得再取反。
4. `countAvailableMembers`（:904）：`available_member_count` 口径加 `&& !m.Disabled`。
5. `ListModelSummaries` 的摘要结构（`ModelSummary`，:263 附近）加 `DisabledMemberCount`，`/api/models` 靠它出 `disabled_member_count`。
6. 关闭成员**不影响** `resolveExactRoute` 是否返回该成员——它必须在成员链里（`/routes`、`/lanes`、导出都照常给出）。

### 2.2 `internal/route/`

1. `routeSnapshotSignature`（`route.go` :340-370）：成员段加 `m.Enabled`。
2. `recordSkipLocked`（:483）拆出 `recordSkipWithStatusLocked(key, member, status)`；原函数委托并传 `Runtime.SkipReason(key)`。
   **`Runtime.SkipReason` / `availabilityOf` 不改**——它们只看得见 Circuits/Cooldowns，配置态判定不得下沉（routing-spec §5.3/§9）。
3. `chooseFailoverLocked`（:536）：
   - 亲和复用分支（:548-558）的前置条件加 `s.Route.Members[idx].Enabled`。
   - `for _, idx := range s.order` 循环里，在 `availabilityOf` **之前**判 `!Enabled` →
     `recordSkipWithStatusLocked(key, label, "disabled")` + `continue`。**必须在 `takeProbe` 之前**，
     否则关闭的成员会吃掉每轮唯一的探测槽。
4. `pickManualLocked`（约 :598）：`active_member` 指向 `!Enabled` → 返回无可用，不换人，不写冷却。
5. `MemberHealth`（`runtime.go` :712）加 `Enabled bool`（json `enabled`，恒回）；健康快照填充处对人工关闭者给
   `Enabled=false` 且 `Available=false`，其余运行态字段（`circuit`/`cooldown_until`/`consecutive_failures`/…）**照实保留，不归零不省略**。

### 2.3 `internal/api/`

| 文件 | 改什么 |
|---|---|
| `lanes.go` | `laneMemberPayload`（:21）加 `Enabled *bool`；`buildLaneMember`（:312）按三态解析（`nil` → 新建启用 / 既有按 `(channel, upstream_model)` 保留原值），落 `Disabled = !生效值`；`laneResponse`（:39）成员恒回 `enabled` |
| `routes.go` | `GetRoute`（:72）成员恒回 `enabled` / `overrides`（无覆盖 `{}`）/ `member_id`；`unconfigured` 推荐项 `enabled=true`、`member_id=0`；`ListModels`（:22）加 `disabled_member_count`，`degraded` 判定**不动** |
| `model_metadata.go` | `ListLaneSummaries`（:26，成员段 :47-52）加 `enabled`（后端字段，前端消费属 W2） |
| `config_lifecycle.go` | `LaneMemberConfig`（:49）加 `enabled`；导出（:127）带上；导入（:410）按三态处理 |
| `route_health.go` | `ProbeLane`（:145）**不改**——已核实它不经选路、不写运行态，天然覆盖停用成员 |

**三态默认值是本轮最易写错的地方**：漏发 `enabled` 绝不能把既有成员关掉。
`buildLaneMember` 拿不到「是不是新建」，需要在 `buildLane` 层面用 `existing.Members` 按
`(channel, upstream_model)` 匹配后回填——参考 :300-304 既有的 `active_member` 保留写法。

### 2.4 W1 收口命令

```bash
go build ./... && go vet ./... && go test ./... -count=1
```
新增单测按 `docs/test-spec-v1.md` §3.1 的「选路 8 条 / 管理 API 9 条」表逐条落，不接受只测 happy path。
必须包含的三条硬断言：① 只改开关 → 签名变化；② 关→开往返前后 `consecutive_failures`/`cooldown_until`/熔断状态逐字段相等；
③ 省略 `enabled` 保存既有成员 → 该成员仍 `enabled=true`。

## 3. W2 前端（子代理）

契约按 api-spec，**不等后端**。改 `GET /routes/{model}` 的 TS 类型即可开工。

1. `web/src/features/routes/api.ts`：`PBRMemberInput`（:228）加 `enabled`；`PBRRouteMember`（:56）加
   `enabled` / `overrides` / `member_id`；`listPBRLaneSummaries`（:200-215）的类型补 `enabled`、`channel_enabled`、
   `public_alias`（后端**已经在返回**，是前端没声明，别去加后端字段）。
2. **新文件** `web/src/features/routes/lib/reorder-members.ts`：纯函数
   `reorderMembers(list, sourceId, targetId, position)`，参照 `param-override-editor-dialog.tsx:526` 的 `reorderOperations`。
   **不引任何 dnd 依赖**，用原生 HTML5 DnD（同文件 :1461 的 `handleDragStart/Over/Drop` + `resetDragState` 模式）。
3. `components/lane-composer.tsx`：
   - `ComposerMember`（:81）加 `enabled` / `overrides` 草稿字段；`save`（:313-321）载荷补齐
     `{channel, upstream_model, public_alias, priority, enabled, overrides}` —— **现状连 `public_alias` 都没回传**
     （草稿 :89 已读到），这是第 4 例「保存即清空」，本轮一起收。
   - 成员行加开关控件（先在 `web/src/components/` 检索既有 `Switch`，禁止在 feature 内重造，见 web/AGENTS.md）。
   - 关闭态：整行降透明度 + 「已停用」标记 + 后果提示；`manual` 的 `active_member` 指向停用成员时行内告警**不拦保存**。
   - 拖拽：落点按行高一半判 before/after、被拖行与落点指示线有视觉反馈、结束置脏；**保留上移/下移按钮**（:635/:646）。
   - React key 继续用 `ComposerMember.id`；**不得改用 `member_id`**（整体替换后必然变），也不得把 channel/upstream 拼进 key。
4. `lib/lane-runtime-display.ts`（`laneMemberStates` :60）加「人工关闭」徽章，variant 与 Cooldown（`warning`）/
   Circuit open（`danger`）**不同**（用 `neutral`）。
5. `components/lane-card.tsx`（成员 `<ol>` :112-125）给停用成员加灰化 + 短标记（现状无任何逐成员灰化分支）。
6. `usage-logs` 的 attempts 状态色加 `disabled`，与故障跳过视觉可区分（ui-spec §6.6）。
7. i18n：**7 个 locale 全同步**（`zh` 源 + `en`/`zh-TW`/`ja`/`fr`/`ru`/`vi`），缺键会让
   `literal-key-coverage.test.ts` 直接红。

### W2 收口命令（**禁止跑 `pnpm build`**，dist 重建留到集成阶段串行做）

```bash
cd web && pnpm typecheck && pnpm lint && pnpm test && pnpm format:check
```
`lint` 必须 0 error（warning 可留）。补 `reorderMembers` 边界单测与开关往返单测（test-spec §3.1 web 表 6 条）。

## 4. W3 L2 脚本（子代理）

只改 `verify/e2e-api/ops_runbook.sh`，加：成员开关写入 → 回读一致 → `enabled` 省略时不被关闭 →
`?dry_run=true` 不落库 → 导出/导入保留开关 → `/routes/{model}` 回传 `enabled`/`overrides`/`member_id` →
`/api/models` 的 `disabled_member_count` 与 `degraded` 组合可区分「全关」与「全挂」。
独立端口 + 独立 SQLite + `internal/testutil/fakeupstream`，**绝不打真实厂商**。
**本轮跑不通是正常的**（后端未完成）：写完即报告，不得为凑 PASS 放松断言，环境缺失按 SKIP + 退出码 2。

## 5. 集成顺序与闸口（主 agent，**全部串行**）

1. 收 W2/W3 产出，逐 diff 审（子代理结论不是真源）。
2. `go build ./... && go vet ./... && go test ./... -count=1`。
3. `cd web && pnpm typecheck && pnpm lint && pnpm test && pnpm format:check && pnpm copyright:check`。
4. W4：写 L3 两条真实用户路径（拖拽改序 → 保存 → `/routes` 顺序一致；点开关关成员 → 端到端不再命中 → 打开恢复），
   然后**串行**：`pnpm build`（重建 `web/dist`）→ `bash verify/console-assets.sh` →
   `bash verify/e2e-api/ops_runbook.sh` → `python3 verify/e2e-ui/user_journey.py` →
   `bash verify/final/fault_injection.sh` → `bash verify/final/e2e.sh`。
   **L3 与任何 `go build` 类脚本不得并发**（`go:embed web/dist` 会假失败）。
5. L3 结束 `git checkout -- web/dist/index.html` 恢复 tracked 产物。

## 6. 停止条件

- W1 任一硬断言写不出来（尤其三态默认值）→ 停，报告，不要绕。
- 发现规范与代码实况矛盾 → 停，报冲突与证据，等裁决，不得自行改规范口径。
- 需要动 `reference/**`、加 dnd 依赖、给成员加 `weight`、把开关做成删除别名 → 一律禁止。
- 部署私有数据（真实渠道名/模型名/车道名/凭据）不得进入任何提交、脚本、日志或结论。
