# W5 验收证据：元数据日志与成本折算

## 目标（goal-prompt §四 W5 / design-v1 §8、api-spec §5.5）

日志只存元数据（含 `route_source`、`attempts` 链）；成本只折算，复用 `ratio_setting` 作单价表；
不做扣费。

验收门三条：

1. 20 条混合请求 → 日志行数 = 请求数；
2. `attempts` 完整；
3. 单条平均 < 2KB。

## 做了什么

| 层 | 文件 | 内容 |
|---|---|---|
| 日志模型 | `model/pbr_request_log.go` | `pbr_request_logs` 单表 + 请求作用域载体 `PBRLogCarrier` + 小时级聚合表 `pbr_stats_hourly`（按 lane/channel/key/model 四维增量 upsert） |
| 填充 | `middleware/pbr_client_auth.go`、`middleware/pbr_route.go`、`controller/relay.go` | 鉴权时建载体并记密钥归属；路由阶段记车道/来源/入站协议/成员；转发收尾记结果、错误分类、尝试链、耗时 |
| 用量回填 | `model/log.go` | 基座 `RecordConsumeLog` 把 token 用量与折算金额写回载体 |
| 查询 | `internal/api/logs.go` | `GET /logs`（lane/channel/key/model/success/since/until + cursor）、`GET /logs/{id}`（含 attempts）、`GET /stats`（hour/day × lane/channel/key/model） |

单一写入点：只有转发收尾写日志，保证「一请求一行」且字段完整；`Written` 去重防止重复。
`estimated_cost` 由基座折算出的 quota 除以 `QuotaPerUnit` 得到——**只折算，不参与准入、不扣余额**。

## 复现

```bash
bash verify/w5/smoke.sh          # 独立端口 6795 + 独立 SQLite + 内置假上游 6806
```

## 结果（run-20260914-141022.log，PASS=21 FAIL=0）

| 验收门 | 结论 | 证据 |
|---|---|---|
| 日志行数 = 请求数 | 20 条请求 → 20 行日志（12 成功、2 未知模型 503、2 缺字段 400、2 故障转移、2 被拒车道 403） | 日志第 19、23 行 |
| 失败也留痕 | 503 快抛（中间件阶段结束）与被拒车道 403 都有行，且 `error_kind` 分别正确 | 日志第 24–27 行 |
| attempts 完整 | 故障转移那条为 `[failed hard_auth @channel-a, success @channel-b]`；一次成功的为单段 `success` | 日志第 32–37 行 |
| 字段归属 | `route_source=explicit`、`channel=channel-b`、`upstream_model=w5-model`、`key_name=client-w5` | 日志第 35–36 行 |
| 用量与折算 | `prompt_tokens`/`completion_tokens`/`estimated_cost`/`total_ms` 均落库 | 日志第 38 行 |
| 单条平均 < 2KB | 20 行载荷合计 2566 字节，**平均 128.3 字节** | 日志第 42 行 |
| 只存元数据 | 表里没有 `quota`/`remain_quota`/`content`/`body` 列，`error_summary` 里 grep 不到请求正文 | 日志第 45–47 行 |
| /stats 聚合 | 按车道聚合出 `w5-model`，成功数 ≤ 请求数；`success=false` 过滤有效 | 日志第 51–53 行 |

回归：`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全绿（41 个包）；
`verify/w1` PASS=22、`verify/w2` PASS=31、`verify/w3` PASS=44 均未回退。

脚本暴露并修掉一个真实缺陷：客户端身份在鉴权中间件里写入日志载体时载体尚未建立
（那时还没进路由阶段），导致 `key_name` 永远为空；改为鉴权时即建载体。

## 本波未做（按波次划分/与设计的偏差，明说）

- **`ttft_ms`**：字段已落库，但首字时间需要流式路径埋点，当前恒为 0；W8 的端到端金标准
  会覆盖流式，届时一并补上（记录在此以免被当成"已实现"）。
- **`cache_read_tokens`/`cache_write_tokens`/`reasoning_tokens`**：基座 `RecordConsumeLog`
  当前只回传 prompt/completion 两项，其余三列先落 0；W8 需要时从 `usage` 明细补齐。
- **小时聚合表的读取**：`/stats` 目前直接对 `pbr_request_logs` 按需聚合（结果与聚合表同源），
  聚合表已在写入侧维护，读取侧切换留到 W8 长稳测试按实测性能决定。
- **限流** `rate_limit_rpm` / `max_concurrency`：仍在 W3 遗留项中，未在本波接入。
