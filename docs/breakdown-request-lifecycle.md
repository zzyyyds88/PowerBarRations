# 请求生命周期重构拆分（临时）

> 本文件仅作为本轮实施的任务拆分，任务闭合后删除。部署私有数据不进入本文件。

## 目标

- 统一请求资源所有权：probe lease、admission ticket、client key lease、stream cancel。
- 消除授权/选路双快照、请求锁内 DB、配置 getter 写库、准入取消误报、流式监督失效等系统性问题。
- 保持单用户、单节点、SQLite、无计费/多用户/多节点边界。

## 阶段与 owner

### 0. 生产只读证据（主线程）

- [x] 目标模型当前配置、车道/成员组引用、7 天终态与 attempts、probe/webhook/audit。
- [ ] 形成 remediation 建议；生产配置写入另行确认。

### 1. 文档契约（主线程 / 文档 agent）

- [ ] ADR 0014：资源租约、ProbeLease、不可变路由快照、取消/超时与准入 outcome。
- [ ] routing-spec：运行态键、探测槽归属、迁移/Prune/Reset、取消/超时、准入生命周期。
- [ ] api-spec：health/probe/reset/config error 形状。
- [ ] design-v1：§7.4/§7.5/§7.10/§16.6。
- [ ] test-spec §8：新增复跑矩阵。
- [ ] CHANGELOG/README 行为变化。

### 2. 核心路由生命周期（Agent A）

- [ ] 类型化 `MemberKey`。
- [ ] `ProbeLease`（owner/generation/expiry）。
- [ ] `State.FinishSuccess/FinishFailure/Cancel/Close`。
- [ ] `recordSuccess/recordFailure` 不再隐式释放 probe。
- [ ] `ClearMemberPenalty` 显式 scope，负数 MemberId 精确匹配。
- [ ] 迁移不转移 live lease；Prune 不清 live lease；Reset generation。
- [ ] Health 只读，维护动作移到解析/配置写入口。
- [ ] 单测：非 owner、迁移、ABA、正负零 ID、lease 超时、并发。

### 3. 路由快照、配置与准入（Agent B）

- [ ] `PBRServe` 单次 ResolveRoute，policy 与选路共用快照。
- [ ] `expandLaneChainWithVersion` 批量加载渠道。
- [ ] `RouteMember` 携带渠道快照、准入配置、渠道版本/配置错误（json:-）。
- [ ] `routeSnapshotSignature` 纳入渠道启用态/版本/准入配置。
- [ ] 纯 `ParseChannelSettings`；getter 不写库；请求路径 fail-closed。
- [ ] `PBRNextChannel` 请求路径零 DB。
- [ ] 准入状态机 fixed/adaptive/disabled、outcome、请求级 deadline、waiter 失效/驱逐。
- [ ] system user base 缓存。
- [ ] 单测：查询次数、TOCTOU、坏配置、取消、AIMD 中性出口、模式切换。

### 4. Relay/流式/客户端密钥/后台（Agent C）

- [ ] Relay 入口最早注册资源清理；WS 失败释放全部资源。
- [ ] scanner ping/写失败立即终止；cleanup 有界。
- [ ] client key lease、RPM Retry-After 修复、touch worker/TTL。
- [ ] perf 采样安全值快照；请求/流路径有界池。
- [ ] channel cache 竞态修复。
- [ ] 单测：handshake、ping fail、cancel/panic、重复 release、队列饱和、race。

### 5. 目标模型 remediation（主线程）

- [ ] 根据当前生产证据给出兜底/优先级方案。
- [ ] 获批准后应用并回读；不把私有配置写入仓库。

### 6. 验收与发布（主线程）

- [ ] `go build/vet/test`、relaykit、`go test -race`。
- [ ] L2/L3/L4/L5/deploy smoke 串行。
- [ ] 原子提交：docs / route / admission / relay / auth / test。
- [ ] 删除本拆分文档。
- [ ] 生产部署单独获批准后执行。
