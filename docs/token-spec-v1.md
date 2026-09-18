# PowerBarRations 令牌与认证规格 v1

> 规范性文件，从属于 [`design-v1.md`](design-v1.md) §5、§9 与 [`api-spec-v1.md`](api-spec-v1.md) §1。
> 本文用占位符示意，**不含部署私有数据**；文中出现的密钥值一律为示例。
> 关键词：**砍掉多用户 ⇒ 没有账号体系**；管理面只有"登录口令"；模型面只有"客户端密钥"。

---

## 1. 两类凭据总览

管理面无账号体系，只有**一个登录口令**。同一个口令支撑两条互不依赖的通道：

| | 控制台会话（人登录） | 管理密钥（AI/脚本） | 客户端密钥 ClientKey |
|---|---|---|---|
| 面向 | 浏览器控制台 | `/api/*`（Bearer） | `/v1/*` 模型流量 |
| 凭据形态 | **HttpOnly 会话 Cookie**（服务端签发，JS 读不到） | `Base64(SHA256(登录口令))` | 服务端随机生成 |
| 数量 | 每个浏览器一个会话 | 由口令唯一决定（无状态） | 多个，每消费者一把（用于分账） |
| 存储 | 只存 `sha256(管理密钥)`（Cookie 里是 HMAC 签名，不含密钥） | 不存储；用时现算 | **明文存储**（另存 `sha256` 哈希作鉴权索引） |
| 生成 | 登录/首启时服务端签发 | **由登录口令派生**（§2） | 服务端随机生成；明文随时可看 |
| 权限 | 全量 | 全量 | 默认全部车道；可显式拒绝 |
| 失效 | 登出、或口令变更（签名指纹不匹配） | 口令变更即失效 | 删除/停用即失效 |

- **人机分离**：浏览器只持有 HttpOnly 会话 Cookie，**不在 localStorage 存放管理密钥**；AI/脚本拿到登录口令后自行计算管理密钥（§2.1），无需人工复制。
- 管理密钥与客户端密钥**分属两套表/来源**，互不混用：管理密钥不入库（口令的函数），客户端密钥在库里。
- 两条管理面通道**等价**：`PBRAuth` 同时接受会话 Cookie 与 `Authorization: Bearer <管理密钥>`。

---

## 2. 管理登录口令与派生管理密钥

### 2.1 规则（应用户钦定）

```
管理密钥 = Base64( SHA256( 登录口令 ) )      // 标准 Base64，32 字节摘要
```

- 用户设置的只是**一个登录口令**；网关不存口令明文，也不存管理密钥明文。
- 数据库只存 `admin_key_sha256 = hex( SHA256(管理密钥) )`，用于校验。
- **两条校验通道**：
  - `Bearer` 调用（AI/脚本）→ 直接比对 `sha256(管理密钥)`；
  - 会话 Cookie（浏览器）→ 校验 Cookie 的 HMAC 签名（§2.3），签名密钥由 `管理密钥` 派生的服务端密钥生成，因此**改口令后旧会话自动失效**。
- 会话 Cookie 里**不含**口令或管理密钥，只有"签发时间 + 口令指纹"及 HMAC 签名；它不能用来反推管理密钥。

### 2.2 首次设置（无账号，只设口令）

| 端点 | 说明 |
|---|---|
| `GET /api/setup/status` | `{ "initialized": false }` |
| `POST /api/setup` | body `{"password":"<口令>"}`；仅在未初始化时可用，否则 `409 conflict` |

- 设置成功后返回一次派生值，便于用户/运维直接配置 AI 调用：

  ```json
  { "initialized": true, "admin_key": "<Base64>", "algorithm": "base64(sha256(password))",
    "hint": "登录口令变更后该密钥随之变化" }
  ```
- 前端在未初始化时必须先进入"设置登录口令"页（`ui-spec-v1.md` §6.1）。
- 口令**不设最小长度**（业主指定"不限制位数"）：只拒绝空口令；低于 16 字符仍接受，但在响应里带 `warning` 弱口令提示。

### 2.3 登录与变更

| 端点 | 说明 |
|---|---|
| `POST /api/auth/login` | body `{"password":"<口令>"}`；校验通过后**设置 HttpOnly 会话 Cookie**，同时响应里返回 `admin_key`（便于 AI 首次取得，见下） |
| `POST /api/auth/logout` | 清除会话 Cookie |
| `POST /api/auth/password` | body `{"current":"...","new":"..."}`；**变更后管理密钥随之变化**，旧密钥与旧会话立即失效，并**续签当前浏览器会话** |

- 无用户名、无注册、无找回、无 OAuth/2FA/passkey。
- 登录响应形如 `{"token":"<管理密钥>","admin_key":"<管理密钥>"}`：`admin_key` 供 AI/脚本直接取用；浏览器**不使用它**，只用 Cookie。
- 登录接口做**本地退避**（连续失败指数延迟，上限 ~30s），避免口令被离线爆破；不退避成"锁号"（个人系统不锁自己）。
- **`PBR_ADMIN_KEY`/`PBR_ADMIN_KEYS` 生效时不允许改口令**：环境变量优先于库内派生值，改口令不会改变实际生效的密钥，也不会废掉旧会话。此时 `POST /api/auth/password` 返回 `409 conflict` 并说明原因（该端点不得返回 `updated: true` 制造"旧密钥已失效"的错觉）。

### 2.4 口令丢失的恢复

两条本地途径（都需要机器访问权限，符合"自用"定位）：

1. 环境变量显式指定管理密钥（**覆盖**口令派生，便于无头/AI 部署）：
   - `PBR_ADMIN_KEY`：单把；
   - `PBR_ADMIN_KEYS`：多把，逗号分隔（轮换过渡用，任一把均可全量操作，见 design-v1 §16.7）。
   两者同时存在时合并，任一匹配即通过。
2. CLI：`pbr auth reset --db <pbr.db> --yes` 清除库内 `admin_key_sha256`，使网关回到未初始化状态，下次重新设置口令。

   `--db` 缺省取 `$SQLITE_PATH`；`--yes` 为破坏性动作的显式确认。命令只改库内凭据，不碰渠道/密钥数据。

### 2.5 安全取舍（须在 README 与代码注释写明）

- SHA256 单次、无盐：抗离线爆破弱于 Argon2/bcrypt。**这是应用户指定的派生规则**；缓解手段是**使用较长随机口令**（建议 ≥32 字符），并把数据库文件收紧为 600 权限、仅本机（启动时对主库及 WAL/SHM 显式 `chmod 0600`，见 `model/main.go` 的 `hardenSQLiteFilePermissions`，不依赖进程 umask）。
- **本网关只提供 HTTP（明文），不提供 TLS/HTTPS 服务能力**（业主决定，与上游 new-api 口径一致）：不加载证书、不自签、不热加载，也没有 `/api/tls/*` 路由与 `TLS_*` 环境变量。需要 HTTPS 时**在外部反向代理（Nginx/Caddy/云负载均衡）终结 TLS**，代理到本服务的明文端口。
- **监听 `0.0.0.0` 对局域网开放**，不做来源限制（业主决定）；可用 `PBR_BIND=127.0.0.1` 收紧。**因为只有明文 HTTP，跨机部署必须收紧监听或加反向代理，否则口令/密钥/渠道 key 会明文过网。**
- **明文传输的风险由此显式承接**：管理口令派生弱（单次 SHA256）+ 传输明文；两者叠加后仅适用于受控网络。生产化的正确做法仍是由外部代理提供 HTTPS，而不是把本服务直接暴露。
- 管理密钥不写日志；仅在 `POST /api/setup` 与 `POST /api/auth/login` 的成功响应里返回（后者是为了让 AI/脚本能直接取得）。
- **会话 Cookie 属性**：`HttpOnly`（JS 不可读）、`SameSite=Lax`、`Path=/`、`Max-Age` 默认 7 天（可用 `PBR_SESSION_TTL_HOURS` 调整）。网关本身只跑 HTTP，**不再自动设置 `Secure`**；若由外部反向代理终结 TLS，用 `SESSION_COOKIE_SECURE=true` 显式开启。Cookie 名 `pbr_session`。

#### 2.5.1 凭据轮换与失效会话的收敛（强制）

会话令牌由**管理密钥哈希**派生签名（`internal/session/session.go` 的 `deriveSecret`），因此
**口令一改，所有既有会话立即失效**。这是期望的安全性质，但会制造一个必须显式处理的中间态：
浏览器仍持有旧 Cookie，服务端却已不认它。

**契约（前后端都必须遵守）**：

1. `GET /api/v1/auth/session` 不仅返回布尔 `authenticated`，还须区分"**带了 Cookie 但已失效**"：
   此时额外返回 `stale: true`（无 Cookie 时 `stale: false`）。前端据此给出**明确原因**
   （"凭据已变更，请重新登录"），而不是笼统的"未登录"。
2. **失效 Cookie 必须由服务端主动清除**（对齐上游 new-api 的 `RefreshAuth`：失败即
   `ClearRefreshCookie`）：
   - `GET /auth/session` 判定 stale 时，响应里**直接下发** `MaxAge=-1` 的同名 Cookie；
   - `PBRAuth` 因会话 Cookie 无效而返回 401 时，同样顺手作废它。

   **不得**把清理责任推给用户手动清浏览器数据。前端自己再清一次只作兜底：登录/初始化前
   先调 `POST /api/v1/auth/logout`（幂等）。
3. **任何管理面请求返回 401 且刷新会话失败时**，前端必须**清除本地会话态并跳转登录页**。
   **禁止**停留在"看起来已登录、实际每个请求都 401"的状态。
4. 登录/初始化成功响应必须**覆盖写入新 Cookie**（同名同路径），使旧值立即作废。

**反例（本次修复前的缺陷）**：登录返回 200，但浏览器继续携带失效的旧 Cookie，业务请求全部
401；前端把 401 当成"过期"静默重放后仍失败，用户只看到"登不进去"，无任何可操作提示。

---

## 3. 客户端密钥 ClientKey

### 3.1 字段

```go
type ClientKey struct {
    ID            int
    Name          string    // 唯一；分账与日志归属标识
    KeyHash       string    // hex(sha256(明文))
    KeyPrefix     string    // 前 12 字符，用于界面展示与人工识别
    Enabled       bool
    LanePolicy    LanePolicy // 见 3.2
    IPAllowlist   []string   // 空 = 不限制（局域网部署通常留空）
    RateLimitRPM  int        // 0 = 不限
    MaxConcurrency int       // 0 = 不限
    ExpiresAt     *time.Time // nil = 永不（安全阀，非额度）
    Notes         string
    CreatedAt     time.Time
    UpdatedAt     time.Time
    LastUsedAt    *time.Time
}

type LanePolicy struct {
    Mode      string   // "all"(默认) | "allow"
    AllowLanes []string // Mode="allow" 时生效
    DenyLanes  []string // 叠加扣减
}
```

> **`cost` 不是 `ClientKey` 的存储字段**：它是响应期由小时聚合表派生的只读统计值
> （`SUM(cost_sum) WHERE group_kind='key' AND group_key=key_name`，单位元），
> 与看板/日志同源，故明细 `logs/prune` 后不变。见 §3.7 与 api-spec §5.4。

### 3.2 车道权限解析（消歧义）

```
候选 = (Mode=="all") ? 全部车道 : AllowLanes
生效 = 候选 - DenyLanes
```

- **默认 `Mode="all"`、`DenyLanes=[]` ⇒ 允许全部车道**。
- 这直接消灭现网两个坑：路由层 `supported_models` 白名单（不加名单故障时 400）与厂商层 `model_limits`（限模型名而非车道）。
- 允许/拒绝的判定对象是**路由键（即请求的模型名）**；若请求命中的是某成员的 `public_alias`，先解析到其所属路由键再判定。
- 命中拒绝 → `403 forbidden_scope`，body 用 `error.code` 标明，**不是 400**（便于 AI 区分"权限"与"请求不合法"）。

### 3.3 明文格式与生成

- 新密钥格式：`pbr-<32 位 base62>`（熵约 190 bit），展示前缀取前 12 字符（如 `pbr-a1b2c3d4`）。
- **明文入库存储**（业主决定，对齐 New API 的"随时可看"手感）：列表/详情直接返回 `key` 明文，随时可复制；`sha256(明文)` 哈希列保留，鉴权仍走哈希索引 O(1) 查表（§3.4 不变）。轮换语义不变（`POST /keys/{name}/rotate` 立即作废旧密钥并换新明文）。
- 此决定的风险由业主显式承接：数据库文件泄露 ⇒ 客户端密钥全部暴露（可冒用打流量产生上游费用，**不能**进入管理面）。缓解仍是 §2.5 的库文件 0600 权限与受控网络。
- 客户端密钥不按前缀做格式拦截；PBR 只按哈希查表鉴权。

### 3.4 校验路径

```
1. 取 Authorization: Bearer <k>（或 X-Api-Key: <k>，两种都收，与线上兼容）
2. 命中客户端密钥表？ sha256(k) 查哈希索引，O(1)
   - 不存在 → 401
3. Enabled? 否则 401
4. ExpiresAt 已过? 否则 401
5. IPAllowlist 命中来源？否则 403
6. RateLimitRPM / MaxConcurrency 超限？否则 429（带 Retry-After）
7. LanePolicy 放行请求目标？否则 403 forbidden_scope
8. 通过 → 注入 key_id/key_name 供日志与统计
```

- 客户端密钥不按 `sk-` 等前缀判断，避免把密钥格式和鉴权语义绑定。
- 模型面不查"余额/额度"——不存在该项。

### 3.5 模型面鉴权边界（无免鉴权例外）

`/v1/**` 全部入口都必须先过 `PBRTokenAuth`（含 Gemini 兼容路径；任务/视频入口已随任务插件子系统移除，Midjourney 图片代理随 Midjourney 全链路移除）。改动前必须先改本节。

### 3.6 生命周期与管理

| 动作 | 端点 | 说明 |
|---|---|---|
| 创建 | `POST /api/keys` | 响应含明文（同时入库） |
| 列表/详情 | `GET /api/keys` `/{name}` | 含当前 PBR 密钥明文与 `key_prefix` |
| 更新 | `PUT /api/keys/{name}` | 权限/限流/备注；不改密钥与哈希 |
| 轮换 | `POST /api/keys/{name}/rotate` | 生成并存新明文，返回该明文 |
| 停用/启用 | `PUT`（`enabled`） | 立即生效 |
| 删除 | `DELETE /api/keys/{name}` | 立即失效 |

### 3.7 统计与"令牌面板"

- 每个请求记 `key_id/key_name`；按月/日与车道/渠道聚合，供控制台"令牌面板"（`ui-spec-v1.md` §6.5，迁移自线上 `apikey-dashboard`）。
- **「消耗」列 = 该令牌的上游折算花费**（元），读 `pbr_stats_hourly`（`group_kind='key'`）的 `SUM(cost_sum)`，经 `GET /api/keys` 的只读 `cost` 字段暴露（api-spec §5.4）。它取代上游面板的"额度/剩余/已用"——PBR 没有额度、余额、钱包与订阅。
- 统计只读，不反哺准入（不做"超支停用"）；**`cost` 绝不影响密钥是否可用**。

---

## 4. 新实例边界

### 4.1 密钥由当前实例创建

- 新实例的客户端密钥通过管理 API 或控制台创建；其明文只在创建或轮换响应中返回，服务端保存哈希与展示前缀。
- 不提供旧系统密钥导入、旧哈希兼容、双写或历史密钥恢复机制。部署方如需接入客户端，应在 PBR 中新建密钥并更新客户端配置。
- 同版本配置快照不包含密钥明文与哈希；恢复快照后需重新创建或轮换客户端密钥。

### 4.2 与管理密钥的边界

- 客户端密钥**不能**用于管理面；管理面只认登录口令派生的管理密钥。
- 管理密钥**不能**用于模型面（避免一把密钥通吃）；如系统需要"AI 自己跑模型"的场景，为它单独建一把 ClientKey。

---

## 5. 明确不做

- 不做用户/账号/注册/登录名，不做角色与授权（casbin），不做 2FA/passkey/OAuth。
- 不做额度、余额、扣费、`unlimited_quota`、`group`、`cross_group_retry`。
- 不做"密钥过期即停服务"以外的额度语义；`ExpiresAt` 仅作安全阀。
  - 令牌面板**只显示「消耗」（上游折算花费，元）**这一只读统计，不显示额度/剩余/余额/订阅；
    控制台令牌表单也不含额度字段。
- 客户端密钥按 §3.3 明文入库存储，`KeyHash` 仍作为鉴权索引；导出配置与日志不得包含明文。
