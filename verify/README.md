# 验收证据（verify/）

本目录是分层测试脚本与结论，测试方法以 [`docs/test-spec-v1.md`](../docs/test-spec-v1.md) 为准。

- `e2e-api/`（L2）：真实管理 API 运维 runbook（建/排障/轮换/备份）。
- `e2e-ui/`（L3）：真实用户操作测试（无头浏览器按用户路径操作控制台）。
- `w0`–`w5`、`final/`、`deploy/`（L4/L5）：分波次端到端、故障注入、长稳、部署、Hermes 档案验收。

- 每个子目录（`w0`–`w5`、`final`、`deploy`）的 README 记录检查项与最近一次实测结论。
- 脚本默认**从自身位置推导仓库根**，可用环境变量覆盖：

  ```bash
  PBR_REPO=/path/to/PowerBarRations bash verify/w0/smoke.sh
  ```

- 运行日志（`verify/**/*.log`）不入库。
- **并发限制**：L3（`e2e-ui/`）会重建 `web/dist/`，与任何 `go build` 脚本并发会导致
  `go:embed web/dist` 找不到文件而假失败；请串行执行（见 `docs/test-spec-v1.md` §8.1）。
- 需要 Go 与 pnpm 工具链；`deploy/` 的容器验收需要 Docker。
- 脚本使用的都是假上游与占位凭据，不会打真实厂商。
