# 任务拆分：dry-run 契约全量修复

> 大任务拆分文档，实施完成后删除（AGENTS.md）。规范见 `docs/api-spec-v1.md` §2.4 + §5.9、
> `docs/design-v1.md` §5.1#4 与 §16.3、`docs/test-spec-v1.md` §5/§8。

## 背景

实测（独立实例 + 回读）发现 `internal/api/ops.go` 的运维适配层**没有 dry_run 分支**，
复用的 `controller/*` handler 也**没有一处读 dry_run**，导致 15 个破坏性写端点在
`?dry_run=true` 时**静默落库**——与 api-spec §2.4 承诺直接冲突，且 AI 手册正教人
"先 dry-run 再写"。这是安全缺陷，不是文档问题。

## 目标（Definition of Done）

1. 每个稳定面写端点**显式声明** dry-run 类别，且声明可执行（不靠人记）。
2. `preview` 端点：带 `?dry_run=true` 返回 diff，**回读零变化**。
3. `reject` 端点（如有）：带 `?dry_run=true` 返回 400 `dry_run_not_supported`，**不执行**。
4. `irrelevant` 端点：明确豁免并给出理由，带参数照常执行。
5. 守卫测试：写方法必须声明；`preview` 必须有行为测试。
6. L2 runbook + L4 e2e 增加 dry-run 无副作用断言。
7. 技能与 `/doc` 手册与实现一致。

## 工作包

### W1 契约与声明表（基础，先做）
- `internal/apierr`：新增 `CodeDryRunNotSupported = "dry_run_not_supported"`。
- `internal/api/ops_routes.go`：`OpsRoute` 增加 `DryRun` 字段（枚举 `preview`/`reject`/`irrelevant`）+
  `Reason`（`reject`/`irrelevant` 必填）；42 条逐条声明。
- `RegisterOpsRoutes`：为 `reject` 注册前置中间件（带 `?dry_run=true` 直接 400 并 abort）。
- 新增 `internal/api/ops_dryrun.go`：`previewOps` 辅助（写 `{dry_run,valid,diff}` + 审计）。

### W2 运维端点预览实现（按端点补 `?dry_run=true` 分支）
- 渠道批量：`batch/status`、`batch/tag`、`batch/copy`、`by-tag`、`by-tag/status`、`disabled`。
- 上游变更：`{name}/upstream-updates/apply`、`upstream-updates/apply-all`。
- 多密钥：`{name}/multi-keys`（`get_key_status` 除外）。
- 系统：`system/options/all`、`system-tasks/log-cleanup`、`system/log-files`。
- 预填组：`prefill-groups` 三条。
- 模型目录：`model-catalog/sync-upstream`、`model-catalog/batch-delete`。

### W3 测试
- 守卫：`ops_dryrun_guard_test.go`（每条写方法已声明；`reject`/`irrelevant` 有理由）。
- 行为：`ops_dryrun_test.go`（每个 `preview` 端点带 `?dry_run=true` 调一次 + 回读断言无副作用）。
- 实跑：`verify/final/endpoint_sweep.py` 增加 dry-run 全量探针；`e2e.sh` 断言。

### W4 文档与技能同步
- `internal/api/docs/api-guide.md`（内嵌 `/doc`）更新 dry-run 段。
- `.agents/skills/powerbar-rations-ops/SKILL.md` 更新铁律与 §5.9 摘要。

## 验收命令

```bash
go build ./... && go vet ./... && go test ./... -count=1
bash verify/e2e-api/ops_runbook.sh
bash verify/final/e2e.sh
cd web && pnpm typecheck && pnpm lint && pnpm test
```
