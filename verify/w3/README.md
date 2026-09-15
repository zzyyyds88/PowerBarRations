# W3 验收证据：访问与 AI 管理面

## 目标（goal-prompt §四 W3 / design-v1 §5、token-spec §2–§4、api-spec §2/§5）

无账号：首启 `POST /api/v1/setup` 设口令，管理密钥 = `Base64(SHA256(口令))`，只存哈希；
客户端密钥默认允许全部车道 + 可显式拒绝，明文只回显一次；实现 api-spec §5 的端点 +
OpenAPI + 错误码 + 审计 + export/import + dry_run。

验收门四条：

1. 每类资源"写→GET 回读"断言一致；
2. `openapi` 可被解析；
3. `export → import(dry_run)` diff 为空；
4. 库里 grep 不到请求正文。

## 做了什么

| 层 | 文件 | 内容 |
|---|---|---|
| 客户端密钥 | `model/clientkey.go` | `pbr-<32 位 base62>` 生成、`sha256` 存储、展示前缀；`LanePolicy` 默认 `all` + 可显式拒绝 |
| 模型面鉴权 | `middleware/pbr_client_auth.go` | 取 `Authorization` / `X-Api-Key` / `x-goog-api-key` / `?key` → 哈希查表 → Enabled → 过期 → IP 名单；PBR 密钥未命中时回落基座令牌（迁移期兜底，W7 删除） |
| 车道权限 | `middleware/pbr_route.go`、`model/lane.go` | 判定对象是**规范化后的路由键**（别名点名先归一到所属车道）；拒绝返回 `403 forbidden_scope` |
| 管理面 | `internal/api/keys.go`、`audit.go`、`config_lifecycle.go`、`system_options.go` | keys CRUD + 轮换、审计、export/import(dry_run)、OpenAPI 3、capabilities |
| 迁移期记账锚点 | `model/pbr_system.go` | 首次用 PBR 密钥访问模型面时懒建一个不可登录的内部用户，供「计费逻辑停用」阶段的记账代码工作（W7 随计费删除） |

设计取舍（已在代码注释与 README 写明）：首启把 `SelfUseModeEnabled` 置为 true，
未配置单价的模型也能直接转发——否则新装网关必须先配一张单价表才能用。

## 复现

```bash
bash verify/w3/smoke.sh          # 独立端口 6794 + 独立 SQLite + 内置假上游 6805
```

## 结果（run-20260914-140120.log，PASS=44 FAIL=0）

| 验收门 | 结论 | 证据 |
|---|---|---|
| 未初始化 | 管理面 409 `not_initialized`；`setup/status` 报 `initialized:false` | 日志第 8–10 行 |
| 弱口令 | 返回 `warning` 明确提示，仍可完成初始化；改口令后旧管理密钥立即 401 | 日志第 14–16 行 |
| 渠道写→回读 | `type`/`priority`/`models`/`param_override` 逐项一致；`key_set=true` 且不回显明文 | 日志第 21–26 行 |
| 车道写→回读 | 模式、成员顺序与优先级、六键、成员权重逐项一致 | 日志第 30–33 行 |
| 密钥生命周期 | 明文以 `pbr-` 开头且只在创建/轮换响应出现；再读只有 `key_prefix`；停用 401、轮换后旧明文 401 新明文 200 | 日志第 38–42、55–57 行 |
| 车道权限 | `deny_lanes` 命中返回 403 `forbidden_scope` | 日志第 45–46 行 |
| PBR 密钥驱动模型面 | 转发成功、响应 `model` 回填请求名；管理密钥打模型面 401 | 日志第 50–53 行 |
| openapi | `openapi.json` 可解析，含 `/channels/{name}`、`/keys/{name}/rotate` | 日志第 60 行 |
| export→import(dry_run) | 导出不含明文与哈希；dry_run 的 add/update 全为空、unchanged 覆盖 channels/lanes/keys/options | 日志第 65–70 行 |
| 审计 | 记录 channel/lane/client_key 变更与 rotate，且不含请求正文 | 日志第 74–77 行 |
| 零正文/零明文入库 | 数据库与运行日志均 grep 不到请求正文哨兵、客户端密钥明文、管理密钥明文 | 日志第 80–83 行 |

回归：`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全绿（43 个包）；
`verify/w1` PASS=22、`verify/w2` PASS=31 均未回退。

脚本暴露并修掉的真实缺陷：`/api/v1/setup` 提前创建"迁移期记账锚点用户"会把基座
`/api/setup` 的首次初始化挤掉（基座在已存在 root 用户时拒绝初始化），改为首次使用
PBR 客户端密钥时懒建；导出空 IP 名单写成 `null` 导致 `export→import` 摘要不可比，
统一为 `[]`。

## 本波未做（按波次划分，非遗漏）

- `/v1/models` 按密钥可见性过滤、令牌面板统计：随 W4/W5 的日志与聚合一起做。
- `GET /api/v1/logs`、`/logs/{id}`、`/stats`、`/route-events`（SSE）：W4/W5。
- 限流 `rate_limit_rpm` / `max_concurrency` 的实际执行：字段已落库并回读，执行在 W5
  与日志/统计一起接（避免两套限流口径）。
- 基座令牌鉴权的兜底路径：W7 随多用户代码一起删除。
