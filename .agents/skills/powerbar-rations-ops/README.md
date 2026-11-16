# powerbar-rations-ops（技能包）

面向 AI / 脚本的 PBR 网关运维手册，是**受版本管理的知识源**：

- `SKILL.md` —— 唯一操作手册。只凭它 + 登录口令即可完成全部日常运维
  （渠道/车道/密钥/日志/统计/导出导入/系统选项/webhook），不需要读 Go 代码、
  不需要解析控制台、不需要打开 SQLite。

## 与规范文档的关系

本技能是 [`docs/api-spec-v1.md`](../../../docs/api-spec-v1.md)（管理 API 契约）与
[`docs/routing-spec-v1.md`](../../../docs/routing-spec-v1.md)（路由语义）的**操作化摘要**，
面向"拿着 HTTP 就能干活"的调用方。

- 契约有变更时，**先改规范文档，再同步本技能**（`AGENTS.md` 的功能变更流程）。
- 技能里的字段名、响应形状、错误码都可在运行中的实例上实测核对；
  发现与实测不符时，以规范文档 + 实测为准并立即修正技能。

## 同步与校验

技能同时被 `~/.agents/skills/`、`~/.claude/skills/`、`~/.hermes/skills/` 三处发现。
仓库内这一份（`.agents/skills/`，项目根）优先级最高，改完请同步到用户级目录：

```bash
cp .agents/skills/powerbar-rations-ops/SKILL.md ~/.agents/skills/powerbar-rations-ops/SKILL.md
```

内嵌在二进制里的 `/doc`、`/llms.txt` 手册（`internal/api/docs/api-guide.md`）与本技能
面向同一批读者，改动其中一份时应检查另一份是否也需要同步；
`internal/api/docs_guide_test.go` 对 `/doc` 的关键事实有内容守卫。
