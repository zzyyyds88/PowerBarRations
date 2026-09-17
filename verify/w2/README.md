# W2 验收证据：容错（冷却 / 熔断 / 亲和 / 四模式 / 错误分类 / 超时）

> **修订（2026-09-17）**：本页记录 W2 当波的实现历史。其中 weighted / round_robin
> 两种模式与成员 weight 已被**物理删除**（routing-spec §2.3 / design-v1 §1.3）：
> 现在只有 failover（默认）与 manual，传入已删模式返回 422 invalid_mode。
> 验收脚本已同步，最近一次实测 **PASS=37 / FAIL=0**（verify/w2/smoke.sh）。


## 目标（goal-prompt §四 W2 / routing-spec §4–§8）

四种模式共用一套冷却/熔断/超时；错误分类按 routing-spec §4（429 软故障不误伤、
client_error 不冷却不换人、欠费 400 由关键词捕获）；成员耗尽快抛 503 不轮询等待；
熔断三态 + 半开 + 指数退避；亲和默认 0。

验收门五条：

1. 指向必然 500 的假端点 → 熔断打开、错误快抛；
2. 修好 → 半开窗口内自动复通（留时间戳证据）；
3. probe 逐成员；
4. 四模式各跑通一条；
5. **上游挂起（不发响应头）→ 按真实超时换人**，不得被判成"客户端取消"（审查 F1 回归门）。

## 做了什么

| 层 | 文件 | 内容 |
|---|---|---|
| 运行态 | `internal/route/runtime.go` | 按路由键分车道的进程内运行态：冷却、熔断三态（含半开与 `open_seconds × 2^k` 指数退避）、亲和、单探测槽、带时间戳事件 |
| 选择算法 | `internal/route/route.go` | 四模式共用可用性判断（`可选 = 不在冷却 && 熔断 != open`）；failover 按 priority 降序、manual 只用 `active_member`、weighted 加权随机、round_robin 环形推进 |
| 错误分类 | `internal/route/route.go`、`middleware/pbr_wiring.go` | 欠费关键词**优先于状态码**（条目属部署数据，接基座 `AutomaticDisableKeywords` 表）；client_error 不冷却不换人不记分；429 计分权重仅 0.2 |
| 超时 | `relay/channel/api_request.go` | 非流式整响应超时、流式首事件超时（首字节到达即解除，提交后不设总超时）；取值车道默认叠加成员级覆盖。**等待响应头阶段的超时也翻译成 deadline**（否则与客户端取消同形，见门 5） |
| 观测与管理面 | `internal/api/route_health.go`、`internal/api/system_options.go`、`router/pbr-router.go` | `GET /lanes/{n}/health`、`POST /lanes/{n}/probe`、`POST /lanes/{n}/circuits/reset`、`POST /channels/{n}/test`、`GET/PUT /system/options` |
| 测试基础设施 | `internal/testutil/fakeupstream` | 新增 `POST /__control` 运行时故障注入与"修好" |

设计变更：`member_affinity_seconds` 默认由上游的 300 改为 **0**（单独 `docs:` 提交，理由见 design-v1 §7.3）。

## 复现

```bash
bash verify/w2/smoke.sh          # 独立端口 6793 + 独立 SQLite + 内置假上游 6804
go test ./internal/route/ -count=1
```

脚本流程：构建 → 起假上游（要求 key `sk-w2-good`）→ 起 pbr → `/api/v1/setup` →
收紧熔断参数（阈值 1、打开 2s、退避上限 10s）→ 建两渠道 → 建各模式车道 →
按验收门逐项实测。

## 结果（run-20260914-134334.log，PASS=31 FAIL=0；门 5 见下方"审查整改"）

| 验收门 | 命令 | 结论 | 证据 |
|---|---|---|---|
| 门 1：必然 500 → 熔断打开、错误快抛 | `POST /__control {"model":"solo-model","status":500}` 后连打 | 两轮后 `circuit=open`（failure_score 1.2 ≥ 阈值 1，`open_until` 有值）；打开期间连打 3 发，上游命中计数 2 → 2 不变（不轮询等待、不静默兜底） | 日志第 24–33 行 |
| 门 2：修好 → 半开自动复通 | `POST /__control {...status:200}` 后等 2s 再打 | 半开探测成功（200，响应 `model` 回填请求名）；`circuit=closed`；事件时间戳 `circuit_open@1789393423856 → circuit_half_open@1789393426143 → circuit_closed@1789393426149` 严格递增 | 日志第 39–46 行 |
| 门 3：probe 逐成员 | `POST /api/v1/lanes/mode-failover/probe` | `probed:2`，两个成员均 `status:"success"`（api-spec §6.4）；`POST /channels/channel-a/test` 亦成功 | 日志第 50–53 行 |
| 门 4：四模式各一条 | 四条车道各打一发 | failover→`channel=1:channel-a`；manual→`channel=2:channel-b`（active_member）；weighted→权重 1 的成员；round_robin→两发交替 channel-a/channel-b | 日志第 57–66 行 |
| 429 不误伤 | 注入 429 连打 3 发 | `circuit=closed`、score 0.6、`last_error_kind=soft_rate_limit`（硬故障 3 次即打开，429 需 5 次） | 日志第 70–71 行 |
| client_error 不冷却不换人 | 注入 400（无关键词） | 原样返回 **400**（不是 503）；该成员 `cooldown_until=0`、`failure_score=0` | 日志第 73–76 行 |
| 欠费关键词 | 注入 400 + `Your credit balance is too low` | 归类 `hard_quota`，进冷却；单成员车道返回固定 503 | 日志第 78–80 行 |
| 清除熔断/冷却 | `POST /lanes/solo-model/circuits/reset` | `{"reset":<清除的熔断器条目数>}`（api-spec §6.6） | 日志第 84 行 |

### 审查整改（代码审查 F1，2026-09-16）

- 缺陷：`startPBRAttemptTimeout` 用 `context.WithCancel`，`fired` 只在**读响应体**阶段翻译成
  `DeadlineExceeded`。上游接受连接但迟迟不发响应头时 `Do()` 直接失败，错误链是 `context.Canceled`，
  被 `Classify` 判成 `canceled` ⇒ **不重试、不换人、不冷却**——最典型的挂起型上游反而没有故障转移。
- 修复：`pbrAttemptTimeout.wrap()` 统一两处翻译，`Do` 失败路径也走它；`Classify` 优先判
  `DeadlineExceeded`（我方翻译后的错误链两者都命中）。
- 回归证据（run-20260916-051012.log，PASS=37 FAIL=0）：门 5 让 channel-a 的上游模型
  `hang-a` 4s 不发响应头（车道流式首事件超时 1s）→ 请求最终 `X-Served-By: channel=2:channel-b`，
  响应体不是错误包，`hang-model` 健康快照里 `channel-a/hang-a` 为
  `last_error_kind=soft_transient` 且 `cooldown_until>0`（修复前会是 `canceled` + 无冷却 + 直接把
  `do_request_failed` 500 透给下游）。
- 单测：`internal/route/probe_ownership_test.go::TestClassifyPrefersDeadlineOverCanceled`、
  `relay/channel/pbr_attempt_timeout_test.go`。

回归：`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全绿（43 个包）；
`bash verify/w1/smoke.sh` 仍 PASS=22 FAIL=0（W2 未破坏 W1 的门）。

脚本首次运行暴露并修掉一个真实缺陷：尝试轮数上界少算一轮，导致"成员预算耗尽"时
把上游的原始 500 直接透给下游，而不是契约要求的 `503 No available channel for model X`。
（另有单探测槽允许多个请求同时探测同一成员的问题，由单测抓出并修正。）

## 本波未做（按波次划分，非遗漏）

- **客户端密钥 ClientKey / 令牌策略**：W2 当时模型面仍用迁移期令牌；W3 起改为 PBR `pbr-` 前缀密钥，W7 移除基座用户/令牌链路后脚本已改用 PBR 客户端密钥。
- **`/v1/models` 按密钥可见性过滤**、OpenAPI、审计、导入导出：W3。
- **`attempts` 链落库与聚合**：运行态已在记录，落库在 W5。
- **SSE `GET /api/v1/route-events`**：控制台实时显示随 W4 一起做；当前以 `health`
  快照 + 30s 轮询即可满足观测。
- **探活对原生协议渠道**（Anthropic/Gemini/Vertex 等）：明确返回 `unsupported`，
  不假装成功；待 W4/W8 接入适配器级探活。
