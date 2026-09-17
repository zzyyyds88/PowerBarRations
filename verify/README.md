# 验收证据（verify/）

本目录是分波次的端到端验收脚本与结论，供改动路由 / 故障转移 / 管理面 / 部署时复跑。

- 每个子目录（`w0`–`w5`、`final`、`deploy`）的 README 记录检查项与最近一次实测结论。
- 脚本默认**从自身位置推导仓库根**，可用环境变量覆盖：

  ```bash
  PBR_REPO=/path/to/PowerBarRations bash verify/w0/smoke.sh
  ```

- 运行日志（`verify/**/*.log`）不入库。
- 需要 Go 与 pnpm 工具链；`deploy/` 的容器验收需要 Docker。
- 脚本使用的都是假上游与占位凭据，不会打真实厂商。
