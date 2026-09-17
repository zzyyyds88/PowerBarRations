# W0 验收证据：以 new-api 源码为基座迁入

## 目标（design-v1 §10.5 / goal-prompt §三）

把 `reference/new-api` 的 Go 源码迁入本仓库、保留包布局、module 改为 `pbr`，并让它**原样转发一发真实请求**。完成标志是"能转发成功"，不是"能编译"。

## 做了什么

1. 复制 new-api 的 Go 包（`common constant controller dto i18n logger middleware model oauth pkg plugins relay relaykit router service setting types`）与 `main.go go.mod go.sum LICENSE NOTICE THIRD-PARTY-LICENSES.md VERSION`，共 981 个 `.go` 文件。
2. **不迁** `web/`、`electron/`、`docs/`、`e2e/`、上游 README/AGENTS/CLAUDE、Dockerfile/makefile。
3. module path 曾用 `pbr`；公开发布版已统一为 `github.com/zzyyyds88/PowerBarRations`，嵌套模块 `relaykit` 对应 `github.com/zzyyyds88/PowerBarRations/relaykit`，同步 go.mod 的 require/replace。
4. `main.go` 有 `//go:embed web/dist`，故补一个**构建占位** `web/dist/index.html`（真正的控制台在 W4 迁入）。
5. 保留 AGPL 版权头与许可文件。

## 复现

```bash
bash verify/w0/smoke.sh          # 独立端口 6790 + 独立 SQLite + 假上游 6801
```

脚本流程：构建 → 起假上游 → 起 pbr（`/tmp/pbr-w0`，独立库）→ `POST /api/setup` 建管理员 → 登录取 access_token → 写比率选项 → 建渠道（指向假上游）→ 建令牌 → 取令牌明文 → `POST /v1/chat/completions`。

## 结果（run-20260914-125101.log）

- `go build ./...` 通过。
- 网关返回：`{"id":"chatcmpl-w0",...,"model":"test-model","choices":[{"message":{"content":"pong from fake upstream"}}]}`。
- 假上游日志：`[fake-upstream] chat model=test-model bytes=68` —— 证明确实经过网关转发到上游。
- 判定：`PASS: 网关已把请求转发到假上游并回传响应`。

## 边界

- 全程独立端口/独立库/假上游，未触碰任何现网容器或凭据。
- 假上游仅本地 `127.0.0.1:6801`，不联任何真实厂商。
