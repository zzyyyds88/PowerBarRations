# W1 验收证据：单层化路由（模型名键控）

> **修订（ADR 0005 之后，2026-09-17）**：本页记录的是 W1 当波实现的历史形态，
> 其中"渠道 Models + Priority 自动成链、零配置可路由"已被
> [ADR 0005](../docs/adr/0005-lane-required-and-channel-model-mapping.md) **推翻**。
> 现行语义：**车道是唯一路由入口**（存在同名启用车道才可调用，否则 503 同形；
> 渠道无 priority/weight；成员顺序由车道决定）。验收脚本已同步现行语义，最近一次
> 实测 **PASS=25 / FAIL=0**（verify/w1/smoke.sh）。下文标注"隐式链/零配置"的部分仅作历史追溯。


## 目标（goal-prompt §四 W1 / routing-spec §1.1）

请求体里的 `model` **就是路由键**：渠道声明 `Models` + `Priority` 即自动形成隐式成员链，
任意模型零配置可路由；显式车道是可选覆盖层。

验收门三条：

1. 两渠道声明同一模型名 → 走 P1；
2. 改坏 P1 的 key → 落 P2，且响应 `model` 不变；
3. 都不声明 → `503 No available channel for model <X>`。

## 做了什么

| 层 | 文件 | 内容 |
|---|---|---|
| 路由核心 | `model/lane.go` | 路由键解析：显式车道 → 成员别名点名 → 隐式链（渠道 `Models` × `Priority` 降序，同优先级按 id 定序）；思考后缀未命中时按 new-api 既有规则归一化再试一次；车道六键与成员级覆盖 |
| 选路运行态 | `internal/route/` | 请求内尝试游标、单成员尝试预算、错误分类（§4.1）、换人/不换人判定、503 固定 body |
| 管道接入 | `middleware/pbr_route.go`、`middleware/distributor.go`、`controller/relay.go` | 首次选路（`Distribute`）与重试选路（`getChannel`）共用同一请求态；不复用旧 `group`/`abilities` 选渠道 |
| 改名与回填 | `constant/context_key.go`、`relay/common/relay_info.go`、`relay/helper/response_model.go`、`relay/channel/openai/relay-openai.go` | 成员 `upstream_model` 作为上行模型名；响应 `model` 回填请求名；`X-Served-By` 暴露真实服务者 |
| 最小管理 API | `internal/api/`、`internal/apierr/`、`middleware/pbr_auth.go`、`router/pbr-router.go` | `/api/v1` 的 setup/login、channels、lanes、models、routes；管理密钥 = `Base64(SHA256(口令))`，只存其 sha256 |
| 测试基础设施 | `internal/testutil/fakeupstream/` | 可编程假上游：按 key/model 定制行为、记录请求体；进程内（Go 测试）与独立进程（验收脚本）共用一份实现 |

不适用 PBR 的请求（任务插件、显式渠道 pin、非模型面路径）由 `PBRServe` 返回 false，仍走迁移前的旧链路，
以保证"只裁计费与多用户、其余功能保留"（design-v1 §1.4）。

## 复现

```bash
bash verify/w1/smoke.sh          # 独立端口 6792 + 独立 SQLite + 内置假上游 6803
```

脚本流程：构建 `pbr` 与 `fakeupstream` → 起假上游（要求 key `sk-w1-good`）→ 起 pbr →
`POST /api/v1/setup` 取管理密钥 → 建两渠道（同声明 `wire-model`，P1 priority 20 / P2 priority 10）→
建 PBR 客户端密钥（`POST /api/v1/keys`）→ 跑三条验收门与改名回填。

> W7 更新：基座用户/令牌链路已物理删除，脚本的模型面凭据改为 PBR 客户端密钥；
> 此前"迁移期令牌"的写法仅存于历史日志。

单元测试：

```bash
go test ./internal/route/ ./model/ -run 'Chain|Lane|Route|Alias|Pinned|Failover|Classify|NoAvailable|MaxRetries|Member' -count=1
```

## 结果（run-20260914-132708.log，PASS=22 FAIL=0）

| 验收门 | 命令 | 结论 | 证据 |
|---|---|---|---|
| 门 1：两渠道同声明 → 走 P1 | `POST /v1/chat/completions` model=wire-model | 200，`X-Served-By: channel=1:channel-a`；计费日志 `use_channel:["1"]` | 日志第 41–44 行 |
| 门 2：改坏 P1 key → 落 P2 | 同上，`PUT /api/v1/channels/channel-a` 换 `sk-w1-bad` | P1 返回 401（`channel error (channel #1, status code: 401)`），P2 服务成功，响应 `model` 仍为 `wire-model`；`use_channel:["1","2"]` | 日志第 49–52 行 |
| 门 3：无渠道声明 → 503 | `POST /v1/chat/completions` model=ghost-model | 503，body 恰为 `{"error":{"message":"No available channel for model ghost-model"}}` | 日志第 58–60 行 |
| 成员链与零配置 | `GET /api/v1/routes/wire-model`、`GET /api/v1/models` | 隐式链按 priority 降序 `channel-a@20 → channel-b@10`；未建任何车道对象即可路由 | 日志第 29–33 行 |
| 成员改名 + 响应回填 | 显式车道 `alias-model` → `vendor-real-name` | 上游收到 `"model":"vendor-real-name"`，响应 `"model":"alias-model"`，`X-Served-By` 含 `model=vendor-real-name`（design-v1 §3.3/§4.1） | 日志第 66–72 行 |
| 写后回读 | `PUT` 渠道后 `GET` | 响应为落库后重读值；不回显 key 明文，只有 `key_set`/`key_prefix` | 日志第 23–24 行 |
| 管理面鉴权 | 无凭据 / 错误密钥访问 `/api/v1/lanes` | 均 401 | 日志第 15–16 行 |
| 私有数据自查 | `grep -rn 'sk-w1-...' --exclude-dir=verify` | 源码无测试密钥残留 | 日志第 75 行 |

回归：`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全绿（43 个包 ok）；
`bash verify/w0/smoke.sh` 仍 PASS（W0 的转发链路现在实际走的就是 PBR 路由）。

脚本首次运行暴露并修掉两个真实缺陷：

1. `Distribute` 已选好的首个成员被转发循环又推进一次，导致 P1 被跳过（计费日志显示请求打到了 P2）；
2. `503` 写出后未中止 gin 处理链，响应体被写了两次（固定 body 后追加了一段通用错误 JSON）。

## 本波未做（按波次划分，非遗漏）

- **冷却 / 亲和 / 熔断**、`retry_interval` 等待、欠费关键词捕获：W2。
- **客户端密钥 ClientKey**：模型面暂沿用迁移期的 new-api 令牌（`/api/setup` + `/api/token`），W3 换成
  `pbr-` 前缀密钥与 `lane_policy`；届时 `TokenAuth` 一并替换。
- **请求日志 `attempts` 链**：`internal/route.State` 已在记录，落库在 W5。
- **OpenAPI / 审计 / 导入导出 / keys / logs / stats / SSE**：W3、W5。
- 四种选择模式里本波只启用 `failover` 顺序；`manual`/`weighted`/`round_robin` 的选择分支在 W2 与冷却一起接入。
