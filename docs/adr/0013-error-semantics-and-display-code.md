# ADR 0013: 错误语义与展示码分离

- 状态：已采纳（2026-11-24）
- 依据：`docs/routing-spec-v1.md` §4.1/§4.1.1/§5.2/§5.4/§7；`docs/api-spec-v1.md` §4.1/§4.4/§6.4/§6.5；`docs/ui-spec-v2.md` §7.2/§9.1
- 关联：[ADR 0002](0002-fast-fail-503.md)、[ADR 0012](0012-outbound-admission-control.md)

## 背景

2026-11-24 对 `internal/route`、`controller/relay.go`、`relay/`、`internal/api` 约 5600 行代码做了逐行只读审计，
发现错误语义层与设计文档脱节，且存在"失败被记成成功"的真实缺陷：

- HTTP 429 仍按成员故障写满档冷却、计熔断分（`runtime.go:683-689,732-733`）；
- `ErrOptionWithHideErrMsg` 与 `WithOpenAIError/WithClaudeError` 把底层 error 重建成字符串，
  `errors.Is(context.DeadlineExceeded)` 在生产路径失效（`error.go:342,416`）；
- `status_code_mapping` 可把失败映射成 2xx，且映射结果参与 `Classify`（`service/error.go:158-167`）；
- Gemini 非流式适配器自己写出错误响应后 `return &usage, nil`，controller 记 `success=true/200` 并清熔断
  （`relay-gemini.go:409-431`）；
- 流式首事件超时/客户端取消被扫描器吞掉，统一以 `EmptyStreamError` 收尾归 `bad_response`；
- 自动禁用完全绕过错误分类，用户加一个 `rate limit` 关键词即可让 429 永久停用整条渠道。

## 决策

1. **429 = 减速，不是故障**（routing-spec §4.1.1）：`soft_rate_limit` 只写 `PacedUntil`，
   不写 `Cooldowns`、不计 `Score`/`FailsByKind`/滚动窗口、不发 `cooldown` 事件；
   成员保持 `available=true`。`hard_quota`（欠费/额度关键词命中）仍走长冷却。
2. **保留错误链，展示文案与 cause 分离**：`NewAPIError` 增加展示字段；
   `Error()`/`Unwrap()` 继续返回/暴露原始 `Err`；`ToOpenAIError`/`ToClaudeError` 优先使用展示字段。
   `ErrOptionWithHideErrMsg`、`SetMessage` 只改展示字段，不再替换 `Err`。
   `NewOpenAIError`/`WithOpenAIError`/`WithClaudeError` 必须保留原始 cause。
3. **状态码映射只影响对外状态码**：`NewAPIError` 增加 `MappedStatusCode`；
   `Classify` 继续读原始 `StatusCode`。映射目标限定 `400-599`，**禁止 2xx 与 503**
   （503 是模型面"无可用渠道"的专属契约，ADR 0002）。后端在渠道写入口做同等校验。
4. **流式收尾按 `StreamStatus.EndReason` 分类**：`Timeout` → `timeout`、`ScannerErr` → 按其错误、
   `ClientGone` → `canceled`、零内容帧 → `bad_response`；不得用 `EmptyStreamError` 顶替超时或取消。
5. **探活按真实响应判定**：200 + `{"error":...}` 必须判失败；`unsupported` 是独立 `error_kind`，
   不是 `network_error`；探活成功按 routing-spec §5.4 清除该成员惩罚态。
6. **自动禁用按分类白名单**：只有 `hard_auth` / `hard_quota` 允许进入自动禁用；
   `soft_rate_limit` / `upstream_reject` / `canceled` / `client_error` 一律跳过。
   状态码范围校验禁止 429/5xx；关键词读取加锁（或原子快照）。
7. **运行态键带 memberId**（ADR 0009 修订）：`channelId:model:memberId`，
   使同车道内"组内成员 + 散成员"重复同一 `(渠道, 模型)` 时各自独立冷却/熔断/预算；
   对外事件与日志的 `member` 标签仍是 `channel/model`。
8. **假成功一律修正**：任何"已向客户端写出错误响应但仍返回 nil error"的路径必须改成
   返回错误（未提交）或返回"已提交错误"标记（已提交：记失败、不重试、不二次写）。

## 后果

- `soft_rate_limit` 行为变更属于**破坏性语义变更**：健康快照新增 `paced_until`，
  `cooldown_until` 不再因 429 出现；`circuit_allowed_fails.soft_rate_limit` 键保留但不再生效。
  需写入 CHANGELOG 的「破坏性变更」并在 README 升级说明中提示。
- 运行态键变化使同车道重复成员的冷却/熔断互相独立；`attempts` 迁移按 memberId 保留。
- `status_code_mapping` 不再改变重试/冷却语义；只改客户端看到的状态码。
- 新增/修改的测试必须覆盖：429 只写 pace、超时/取消分类、状态码映射不改分类、
  Gemini 假成功、探活 200+error、自动禁用白名单、重复成员独立运行态。
- **明确不做**：为 `soft_rate_limit` 恢复冷却；用展示文案替代结构化错误码；
  在 `status_code_mapping` 中允许 2xx/503。