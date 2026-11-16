# 贡献指南

感谢你有兴趣为 PowerBarRations（PBR）做贡献。本项目是一个**单用户、自建**的 LLM 聚合网关，改动会直接影响线上转发路径，所以流程比一般小项目严格一些。

## 开始之前

- 先读 [AGENTS.md](AGENTS.md)：它是本项目的协作规则（任务分级、文档先行、提交规范、仓库边界），也是维护者与 AI 助手共用的规则。
- 设计文档是**唯一事实来源**：[docs/design-v1.md](docs/design-v1.md) 及其配套 [api-spec-v1.md](docs/api-spec-v1.md)、[routing-spec-v1.md](docs/routing-spec-v1.md)、[token-spec-v1.md](docs/token-spec-v1.md)、[ui-spec-v1.md](docs/ui-spec-v1.md)。代码、测试、配置与文档冲突时，以文档为准。
- 不要提交任何真实凭据、真实用户数据或带凭据的 URL。仓库里出现的 key 一律用占位符（如 `__INJECT_BY_OPERATOR__`）。

## 环境要求

| 组件 | 版本 |
|---|---|
| Go | 1.25+（见 `go.mod`） |
| Node.js | 22+（控制台构建） |
| pnpm | 11+（锁定于 `web/package.json` 的 `packageManager`） |
| SQLite | 随二进制内置，无需单独安装 |

## 本地开发与验证

在提交前，至少跑一遍你改动会影响的检查；门禁全绿再开 PR：

```bash
# Go：构建、静态检查、测试
go build ./...
go vet ./...
go test ./... -count=1

# 控制台：类型、lint、测试、构建
cd web
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint          # 必须 0 error
pnpm test
pnpm format:check
pnpm copyright:check
pnpm build
```

端到端验收脚本在 [verify/](verify/)（`w0`-`w5`、`final/`、`deploy/`），每个目录的 README 写明了检查项与最近一次实测结论。改动路由/故障转移/管理面时，请对应复跑相关脚本。

## 提交规范

- 提交必须**原子化**：一次提交只做一件事，类型用 `feat` / `fix` / `docs` / `refactor` / `test` / `chore` / `breakdown` 前缀，信息用清晰的中文或约定式英文。
- **文档先行**：跨模块、改公共契约、改状态机属于大任务，先改设计文档（单独的 `docs:` 提交）再改代码；中等任务允许文档与代码同一提交。
- 不要顺带改 vendor（`relay/channel/**` 等）进来的上游适配器逻辑；确需改动时在提交信息里说明理由。
- 提交后工作区必须干净；不要留构建产物、日志或临时文件。
- 变更涉及行为时**同步更新测试**；Bug 修复请先补一个能稳定复现的失败用例。

## Pull Request

- 从 `main` 切分支开发，PR 里说明：动机、改动范围、验证方式与结果。
- PR 模板会提示检查项；请如实勾选，未跑的项写明原因。
- 涉及 UI 的改动请附前后对比截图（若有）。

## 报告问题

- 功能缺陷、功能建议：用 [Issue 模板](https://github.com/zzyyyds88/PowerBarRations/issues/new/choose)。
- **安全漏洞：不要开公开 Issue**，见 [SECURITY.md](SECURITY.md)。

## 许可

提交即表示你同意你的贡献按本仓库的 [AGPL-3.0](LICENSE) 许可发布。请保留文件原有的版权头（上游 `Copyright (C) QuantumNous` 不可替换）。
