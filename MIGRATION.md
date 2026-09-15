# MIGRATION：从旧两层网关切到 PowerBarRations

> 本文只写**机制**，不含任何部署私有数据（渠道名、地址、单价、凭据一律不写入仓库）。
> 输入是运维私有台账。切换前请通读并在测试实例上演练一遍。

---

## 1. 迁移算法（旧配置 → PBR 配置）

旧部署是两层：**路由层**（按分组做优先级与故障切换）+ **厂商层**（持有 key、做协议适配与
模型改名）。PBR 把两层塌成一层。对路由层的每个成员记录迁移如下：

```
旧路由层成员: (group, channel_model, priority)
  channel_model = (channel_id, 发给厂商层的请求名 M)

旧厂商层渠道: channel_id -> (厂商地址, key, group, protocol, model_mapping)

对每条成员记录:
  1. 取实体渠道 = channels[channel_id]
  2. 真实上游模型名 U:
       U = model_mapping[M]        # 若厂商层把请求名再映射一次
       否则 U = M
  3. 落成 PBR 渠道（按厂商地址去重，同名渠道只建一次）:
       PUT /api/v1/channels/{渠道名}
         {type: 协议族, base_url: 厂商地址, key: <原 key>, enabled: true,
          models: [该渠道会真正提供的路由键集合]}    # 见第 4 步
  4. 落成 PBR 车道成员:
       若该 group 的成员顺序需要保留（自定义顺序 / 成员改名 / 池化不同上游的不同模型名）:
         PUT /api/v1/lanes/{group 名}
           members: [{channel, upstream_model: U, priority}]
       否则不建车道：直接把 U 加进对应渠道的 models，即可靠隐式链自动成链。
```

**关键取舍**：旧 group 名不一定是模型名。PBR 的路由键是**下游请求里的 model**，所以：

- 如果旧部署是"下游请求 `M` → 路由层按 group 选成员 → 厂商层把 `M` 映射成 `U`"，
  且 `M` 就是下游真实请求名，那么把 `U` 声明进渠道 `models` 即可（隐式链），
  下游请求名 `M` 与 `U` 相同时甚至连车道都不用建。
- 如果 `M ≠ U`（同一路由键在不同上游叫不同名字），必须建**显式车道**：车道名 = `M`，
  成员 `upstream_model = U`。这正是 PBR 取代 `model_mapping` 的地方。

### 客户端凭据

- 存量客户端密钥**值不变**（下游零改动）：`KeyHash = sha256(原值)`，`KeyPrefix = 原值前 12 字符`，
  格式不做规整（保留 `sk-` 等前缀）。
- 权限一律置 `lane_policy.mode = "all"`（去掉原白名单/模型限制），
  **并在对账报告里列出被放宽的项**，供人工确认。
- 存量密钥可以继续沿用基座令牌鉴权路径过渡，但最终应全部导入为 PBR 客户端密钥。

### 对账

迁移脚本或人工都必须在**旧库副本**上跑两次，证明幂等，并输出对账报告：

| 对账项 | 旧 | 新 | 判定 |
|---|---|---|---|
| 渠道条数 | N | N' | 相同（同地址渠道已去重） |
| 路由键集合 | 旧 group×成员可达的请求名 | `GET /api/v1/models` | 新 ⊇ 旧（不允许变少） |
| 每个路由键的成员链 | 旧 group 成员顺序 | `GET /api/v1/routes/{model}` | 一致 |
| 客户端凭据 | 旧令牌数 | `GET /api/v1/keys` | 条数一致、哈希一致 |

`GET /api/v1/export` 可作为**切流前的配置快照**，`POST /api/v1/import?dry_run=true` 用于比对。

### 一键迁移命令：`pbr migrate`

机制已固化为命令：输入旧两层库的**只读副本**，输出 PBR 配置与对账报告。

```bash
pbr migrate \
  --routing /path/to/octopus/data.db \
  --vendor  /path/to/new-api/one-api.db \
  --target  /path/to/pbr.db \
  --report  /tmp/migrate-report.json \
  --keys    both          # octopus | newapi | both
```

- **算法**：design-v1 §11.1（路由层成员请求名 M → 厂商层能力表按 priority/weight 择渠道 C →
  应用 C 的 `model_mapping` 得到真名 U → PBR 成员）。
- **幂等**：以 `name` 为键 upsert，可重复跑；同一输入两次产出的计划逐字节一致。
- **对账报告**：`unresolved`（无归属，必须人工裁决，不许静默丢弃）、`ambiguous`
  （同优先级平局）、`widened`（旧白名单/模型限制被放宽为全部车道）。
- **凭据**：渠道 key 与客户端密钥明文只写目标库；拿不到真实 key 时留 `__INJECT_BY_OPERATOR__`。
- **只读旧库**：命令对旧库只读；读之前请自行拷贝副本（含 `-wal`/`-shm`）并 checkpoint。
- **验收证据**：`ROUTING_DB=<octopus.db> VENDOR_DB=<new-api.db> bash verify/final/a4_migrate.sh`
  （在旧库副本上跑两次并证明幂等、目标库可被 PBR 加载）。

---

## 2. 切流

1. **并行期**：PBR 起在**另一个端口**、用**独立 SQLite**，不碰旧网关的容器与数据卷。
2. 用私有台账把配置灌进 PBR（第 1 节），跑对账。
3. 用真实下游做**单点试切**：把某个客户端的 `base_url` 从旧网关改到 PBR，观察
   `/api/v1/logs` 里的 `success`、`attempts`、`http_status`。
4. 逐客户端切换；每切一个观察 15 分钟（错误率、503 形态、上游是否正常）。
5. 下游零改动的前提是**存量密钥值不变**；若无法保持不变，则属于"需要下游配合"的变更，
   必须单独评估。

判定标准：`GET /api/v1/logs?success=false` 里不应出现预期之外的 `error_kind`；
`503 No available channel for model X` 只应出现在确实没有渠道声明该模型的时候。

---

## 3. 回滚

回滚是"把 `base_url` 改回去"，不需要动数据：

1. 把已切客户端的 `base_url` 指回旧网关（旧网关全程未被修改，仍在运行）。
2. 确认旧网关日志恢复正常。
3. PBR 侧保留现场：`GET /api/v1/export` 导出配置、备份 `pbr.db`，供事后分析。

**演练要求**：在测试实例上完整走一遍"切流 → 观察 → 回滚 → 再切流"，并记录时间戳；
不动生产容器的数据卷。

---

## 4. 回滚之后

- PBR 与旧网关的**协议面**一致（`/v1/*` 与错误语义），因此可以随时再切。
- 运行态（冷却/熔断/亲和）是进程内的，重启即清空——切流前后都不需要保存。
- 如果切流期间发现某个上游需要"半开自动复通"，旧网关没有这个能力（只禁不通），
  这正是回滚后仍然值得继续推进 PBR 的理由。
