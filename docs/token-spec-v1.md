# PowerBarRations 令牌与认证规格 v1

> 规范性文件，从属于 [`design-v1.md`](design-v1.md) §5、§9 与 [`api-spec-v1.md`](api-spec-v1.md) §1。
> 本文用占位符示意，**不含部署私有数据**；文中出现的密钥值一律为示例。
> 关键词：**砍掉多用户 ⇒ 没有账号体系**；管理面只有"登录口令"；模型面只有"客户端密钥"。

---

## 1. 两类凭据总览

| | 管理凭据（登录口令派生） | 客户端密钥 ClientKey |
|---|---|---|
| 面向 | 控制台登录 + `/api/v1/*` | `/v1/*` 模型流量 |
| 数量 | 一个口令（无账号、无用户名） | 多个，每消费者一把（用于分账） |
| 存储 | 只存 `sha256(管理密钥)` | 只存 `sha256(明文密钥)` + 展示前缀 |
| 生成 | **由登录口令派生**（§2） | 服务端随机生成，明文只回显一次 |
| 权限 | 全量 | 默认全部车道；可显式拒绝 |
| 额度/过期 | 无 | 无（可选安全阀，见 §4.4） |

管理凭据与客户端密钥**分属两套表/来源**，互不混用：管理凭据不在数据库里（见 §2），客户端密钥在库里。

---

## 2. 管理登录口令与派生管理密钥

### 2.1 规则（应用户钦定）

```
管理密钥 = Base64( SHA256( 登录口令 ) )      // 标准 Base64，32 字节摘要
```

- 用户设置的只是**一个登录口令**；网关不存口令明文，也不存管理密钥明文。
- 数据库只存 `admin_key_sha256 = hex( SHA256(管理密钥) )`，用于校验。
- 校验任一路径都归一到同一比较：口令登录 → 先派生管理密钥 → 比对哈希；`Bearer` 调用 → 直接比对哈希。

### 2.2 首次设置（无账号，只设口令）

| 端点 | 说明 |
|---|---|
| `GET /api/v1/setup/status` | `{ "initialized": false }` |
| `POST /api/v1/setup` | body `{"password":"<口令>"}`；仅在未初始化时可用，否则 `409 conflict` |

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
| `POST /api/v1/auth/login` | body `{"password":"<口令>"}` → `{"token":"<管理密钥>"}`；前端存本地并作 `Bearer` |
| `POST /api/v1/auth/password` | body `{"current":"...","new":"..."}`；**变更后管理密钥随之变化**，旧密钥立即失效 |

- 无用户名、无注册、无找回、无 OAuth/2FA/passkey、无会话 cookie/refresh token。
- 登录接口做**本地退避**（连续失败指数延迟，上限 ~30s），避免口令被离线爆破；不退避成"锁号"（个人系统不锁自己）。

### 2.4 口令丢失的恢复

两条本地途径（都需要机器访问权限，符合"自用"定位）：

1. `PBR_ADMIN_KEY` 环境变量显式指定管理密钥（**覆盖**口令派生，便于无头/AI 部署）；
2. CLI：`pbr auth reset` 清除库内 `admin_key_sha256`，使网关回到未初始化状态，下次重新设置口令。

### 2.5 安全取舍（须在 README 与代码注释写明）

- SHA256 单次、无盐：抗离线爆破弱于 Argon2/bcrypt。**这是应用户指定的派生规则**；缓解手段是**启用 HTTPS** + 使用较长随机口令（建议 ≥16 字符，非强制），且数据库文件保持 600 权限、仅本机。
- **监听 `0.0.0.0` 对局域网开放**，不做来源限制（业主决定）；可用 `PBR_BIND=127.0.0.1` 收紧。
- **HTTPS**：`TLS_ENABLED=true` 时加载证书（`TLS_CERT_FILE`/`TLS_KEY_FILE`）；无证书则首次启动自动生成自签证书到 `TLS_DIR`。支持 `PUT /api/v1/tls/certificate` 导入自有证书并**热加载**（无需重启），见 README §8。
- 管理密钥不写日志、不返回（除 §2.2 设置成功时的一次）。

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
- 明文**只在创建 / 轮换响应中出现一次**；此后任何读取只返回 `key_prefix`。
- 轮换（`POST /keys/{name}/rotate`）立即作废旧密钥。

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

- 为兼容下游，**旧的 `sk-` 前缀密钥不做格式拦截**（线上路由层曾用 `sk-<app>-` 前缀判断，PBR 不用前缀做鉴权，只做哈希查表），从而保证存量密钥值原样可用。
- 模型面不查"余额/额度"——不存在该项。

### 3.5 生命周期与管理

| 动作 | 端点 | 说明 |
|---|---|---|
| 创建 | `POST /api/v1/keys` | 响应含一次性明文 |
| 列表/详情 | `GET /api/v1/keys` `/{name}` | 只含前缀 |
| 更新 | `PUT /api/v1/keys/{name}` | 权限/限流/备注；不含明文 |
| 轮换 | `POST /api/v1/keys/{name}/rotate` | 返回新明文一次 |
| 停用/启用 | `PUT`（`enabled`） | 立即生效 |
| 删除 | `DELETE /api/v1/keys/{name}` | 立即失效 |

### 3.6 统计与"令牌面板"

- 每个请求记 `key_id/key_name`；按月/日与车道/渠道聚合，供控制台"令牌面板"（`ui-spec-v1.md` §6.7，迁移自线上 `apikey-dashboard`）。
- 统计只读，不反哺准入（不做"超支停用"）。

---

## 4. 迁移与兼容

### 4.1 存量密钥导入（迁移阶段，见 design-v1 §11）

- 导入现存客户端凭据，**密钥值不变**（下游零改动），因此：
  - `KeyHash = sha256(原值)`；
  - `KeyPrefix = 原值前 12 字符`；
  - 格式不做规整（保留原 `sk-` 等前缀）。
- 权限一律置为 `Mode="all"`（去掉原白名单/模型限制），并在对账报告中列出被放宽的项，供人工确认。
- 原"限制"多用于分账，而分账由 `KeyID` 身份承载即可，无需白名单。

### 4.2 与管理密钥的边界

- 存量客户端密钥**不能**用于管理面；管理面只认登录口令派生的管理密钥。
- 管理密钥**不能**用于模型面（避免一把密钥通吃）；如系统需要"AI 自己跑模型"的场景，为它单独建一把 ClientKey。

---

## 5. 明确不做

- 不做用户/账号/注册/登录名，不做角色与授权（casbin），不做 2FA/passkey/OAuth。
- 不做额度、余额、扣费、`unlimited_quota`、`group`、`cross_group_retry`。
- 不做"密钥过期即停服务"以外的额度语义；`ExpiresAt` 仅作安全阀。
- 不做密钥明文入库；`KeyHash` 之外的任何字段都不得含明文。
