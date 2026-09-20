# Webhook 架构完善（v1.1）任务拆分

> 设计依据：`docs/design-v1.md` §16.10（commit 128ae1c）。完成后本文档删除。

## 任务 1：route 层 EventReset 常量（单文件）

- `internal/route/runtime.go`：常量块加 `EventReset = "reset"`；`Reset()` 的 `appendEvent("reset", ...)` 改用常量。
- 顺手修 `Reset()` 上方两行重复矛盾的注释（一行说"清空熔断与冷却"、一行说"清空全部运行态"，保留后者）。

## 任务 2：webhook 投递隔离 + reset 白名单 + 计数出口（单文件）

- `internal/webhook/webhook.go`：
  - `dispatch`：对每个匹配 target，风暴窗口检查（同步）通过后改为 `go deliver(...)` 独立投递；加 `deliverWG` 计数，暴露 `WaitPendingDeliveries()` 供测试等待。
  - `allowedEventTypes` 加 `route.EventReset`；`eventTitles` 加 `EventReset: "熔断与冷却已清空"`。
  - 新增 `DroppedEvents() int64` 出口。
- 包头注释的投递语义描述同步（"worker 读配置"改为"每个命中 target 独立投递"）。

## 任务 3：deliveries 响应加 dropped_events（单文件）

- `internal/api/webhooks.go` `ListWebhookDeliveries`：外层加 `"dropped_events": webhook.DroppedEvents()`。

## 任务 4：测试

- `internal/webhook/webhook_test.go`：
  - `TestDispatchIsolatesSlowTarget`：target A 挂起（首个响应悬挂直到信号），target B 正常 200；断言 B 收到投递（A 的重试不阻塞 B）。
  - `TestResetEventWhitelisted`：reset 事件入队→投递成功，`summaryText` 文案正确（member 空、lane 有值）。
  - `TestDroppedEventsExposed`：缓冲满后 `DroppedEvents()` 计数正确。
- `internal/route/runtime_subscribe_test.go`：`TestResetNotifiesSubscriber`。

## 任务 5：验收闸口

- `go build ./...`；`go test ./internal/webhook/ ./internal/route/ ./internal/api/`。
- 临时实例端到端（独立端口 + 独立 SQLite）：配 target 指向本地假接收器 → 熔断成员收 `circuit_open` → `circuits/reset` 收 `reset` → `GET /api/webhooks/deliveries` 读 `dropped_events`。

## 提交序列

docs:（已完成 128ae1c）→ breakdown:（本提交）→ feat: 任务 1+2+3 → test: 任务 4 → chore: 删本文档 → merge --no-ff 回 main。
