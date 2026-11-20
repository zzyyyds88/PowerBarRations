# 隐私脱敏规格（v1）

> 本文件是 PBR 隐私脱敏的规范性真源，与 [`design-v1.md`](design-v1.md) 同级有效。
> 语义基线是 [magpie](https://github.com/yetone/magpie) 的 `internal/redact`、`internal/masking`
> 与 `internal/pii`（AstrLink Guard），PBR 只做单用户网关所需的收敛，不改变其判定逻辑。
> 与 magpie 的差异集中在 §10，任何新增差异都必须先改本文件。

## 1. 目标与非目标

### 1.1 目标

- 请求离开 PBR 之前，把其中的**密钥与个人数据**换成占位符；厂商回复里的占位符再换回原值。
- 同一个值在同一实例内始终得到同一个占位符，跨请求、跨会话、重启后一致（占位符键持久化）。
- 只记录**命中的类型、来源、占位符、长度与首字符**，不落原值、不落请求正文。
- 规则命中不依赖网络；本地 ML 模型可选，失败时按明确策略降级或拦截，绝不静默放行该拦的内容。
- 配置与观测都在控制台「日志 › 脱敏」页，并在请求日志中可检索。

### 1.2 非目标

- 不做多用户/按密钥的脱敏策略（PBR 没有多用户；策略是实例级）。
- 不保存请求/响应正文，不提供 magpie 的「厂商实际收到什么」左右对照。
- 不移植 magpie 的 1.6 GB Privacy Filter / native `pf-serve`；PBR 只使用纯 Go 的 AstrLink Guard。
- 不做 DLP、合规审计、数据分类或出站内容审查；脱敏只服务于"别把本机秘密发给厂商"。
- 不改变路由、协议转换、错误语义与上下文统计口径；脱敏是转发前的正文变换与回复后的逆变换。

## 2. 术语

| 术语 | 含义 |
|---|---|
| 命中（hit） | 一处被识别并处理的敏感值；只记录元数据，不记录值 |
| 占位符 | `{{KIND_xxxxxxxx}}`，`KIND` 为大写类型，`xxxxxxxx` 是 8 位 base32 小写摘要 |
| 仓库（vault） | 进程内 `placeholder → value` 映射，仅内存，重启即失；上限与淘汰见 §5.3 |
| 脱敏键 | HMAC-SHA256 的 32 字节密钥，决定占位符；持久化在 `options` 表，不导出 |
| 动作 | `mask`（默认）、`warn`（原样发送但记录）、`block`（替换并拦截请求） |
| 规则命中 | 内置规则、用户规则、用户词；来源 `rule` / `yours` / `word` |
| 模型命中 | AstrLink Guard 找到的 span；来源 `model` |
| 已知命中 | 之前被脱敏过的值再次出现；来源 `known` |

## 3. 检测器

### 3.1 内置规则（`internal/privacy/redact`）

逐文件移植 magpie `internal/redact` 的规则引擎，保留其全部判定与边界处理，包括：

- 密钥：私钥块、常见厂商前缀（`sk-`、`ghp_`、`AKIA`、`AIza`、`xox*`、`hf_`、`gsk_`、`xai-` 等）、JWT、
  URL 内密码、`password=` / `API_KEY:` / `Authorization:` / `Cookie:` 等字段、中文/日文/韩文
  「密码/密钥/令牌」写法。
- 个人数据：邮箱、中国身份证、手机号、银行卡（Luhn），以及可单独开关的 SSN、护照、IBAN、
  生日、MAC、序列号、家目录用户名、`user@host`、公网 IP、bucket。
- 上下文排除：代码里的变量/函数名、文件路径、行号引用、版本号、时间戳、示例域名
  （`example.com` / `.test` / `.invalid`）等不误伤；细节以移植代码的测试为准。
- 已知值：一个值一旦被脱敏，后续请求中无论上下文如何都继续脱敏（magpie `known.go`）。
- 用户规则：`prefix` 或 `regex` 二选一，最多 32 条；`regex` 必须可编译且不匹配空串。

规则引擎的输入是 JSON body 中的字符串值；字段级豁免（`model`、`id`、`*_id`、`signature`、
`encrypted_content`、工具定义结构等）沿用 magpie `internal/redact/json.go`。

### 3.2 本地 ML 模型（`internal/privacy/pii`）

- 模型：AstrLink Guard（Apache-2.0），int8 ONNX token 分类器，约 37 MB，纯 Go CPU 推理。
- 文件固定到 magpie 使用的 Hugging Face revision 与 SHA-256；默认从 `hf-mirror.com` 下载（可用
  `PBR_PII_MODEL_BASE_URL` 指向官方源或内网镜像），首次启用时下载到
  `<SQLITE_PATH 所在目录>/privacy/astrlink-guard/<revision>/`，三个文件全部校验后才算可用。
- **官方 Docker 镜像内置模型**：构建期把工作区已下载的
  `privacy/astrlink-guard/<revision>/` 三个文件逐个校验 SHA-256 后放到
  `/app/privacy/astrlink-guard/<revision>/`，镜像默认
  `PBR_PII_MODEL_DIR=/app/privacy/astrlink-guard/<revision>`；启用模型不需要运行时网络。
  部署方可用 `-e PBR_PII_MODEL_DIR=...` 改回数据卷或自定义预置目录。模型文件不进入 Git，
  发布脚本会在构建前检查它们是否存在。
- `PBR_PII_MODEL_DIR` 可指向预置目录（离线/内网部署）；目录内文件名与校验和必须一致。
- 同时最多 2 个请求使用模型；单次模型调用 2 秒预算，超出后已读部分生效、其余交给规则，
  请求继续（`model` 状态记为 `partial`）。
- 模型按 4 KiB 分块读取并按块内容缓存：一次请求超时只留下未读完的块，下一次请求从缓存
  继续，不会每次从头重读整段历史。
- 单请求送给模型的总文本上限 256 KiB、单字符串 64 KiB；超出部分只用规则，状态记 `truncated`。
- 模型只补规则找不到的自由文本姓名/地址/账号；模型命中按 `min_score`（默认 0.80）过滤。
- `model.required=true` 且模型不可用（未下载、加载失败、超时未产生任何结果）时，请求以
  `503 privacy_model_unavailable` 拦截；`required=false` 时规则继续脱敏并记录 `model_unavailable`。

## 4. 设置模型

设置是实例级单例，持久化在 `options` 表（键前缀 `PBRPrivacy`），管理 API 读写。
`GET /api/privacy/settings` 返回：

```json
{
  "secrets": false,
  "personal": false,
  "kinds": {"ssn": true, "passport": true, "iban": true, "birthday": true,
            "mac": true, "serial": true, "home": false, "userhost": false,
            "ip": false, "bucket": false},
  "words": [],
  "words_any_case": false,
  "words_whole": false,
  "rules": [],
  "actions": {},
  "allow": [],
  "shape": false,
  "model": {"enabled": false, "required": false, "min_score": 0.8},
  "active": false
}
```

- `secrets` / `personal` / `words` / `model.enabled` 任一开启即 `active=true`；`active=false` 时
  转发路径完全不做脱敏（零开销）。
- `kinds` 缺省值按 magpie `Categories.On`：前六项默认开，后四项默认关；未知 kind 忽略。
- `words`：用户自己的名字/代号/主机名；最多 200 个，每个 ≤256 字符，去重（忽略大小写）。
- `words_any_case`：大小写不敏感；`words_whole`：仅整词匹配（Unicode 词边界，数字不拆小数）。
- `rules`：`{kind, prefix?, regex?}`；`kind` 归一为占位符可用的 `[A-Z][A-Z0-9_]{0,23}`；
  `prefix` 长度 3–64，`regex` 长度 ≤300；最多 32 条；空规则忽略；重复规则去重。
- `actions`：`kind → warn|block`；`mask` 是默认值，不写入。未知 kind 或动作 → 422。
- `allow`：原样放行的值，最多 200 个、每个 ≤256 字符，忽略大小写去重。
- `shape`：为 true 时密钥类值生成同形态的假值（如 `sk-` 开头）而不是占位符；仍登记仓库，
  回复可逆。
- `model.min_score`：0.50–0.99；`model.enabled` 打开才下载/加载模型；`model.required` 只在
  `enabled` 时有效。

### 4.1 推荐基线（默认不启用）

发布方维护一份**推荐基线**（`privacy.Recommended()`，`GET /api/privacy/recommended`）：

- `kinds` 按 magpie `Categories.On`：`ssn` / `passport` / `iban` / `birthday` / `mac` / `serial`
  默认勾选；`home` / `userhost` / `ip` / `bucket` 这些高噪声项默认不勾。
- `actions` 为空（全部 `mask`）、`words` / `rules` / `allow` 为空、`shape=false`、
  `model.min_score=0.80`。
- **三个总开关 `secrets` / `personal` / `model.enabled` 恒为 `false`**：脱敏必须由用户在
  设置页手动开启。新实例的 `Defaults()` 就是这份基线；`active=false` 时转发路径零开销。

控制台的「应用推荐配置」只写检测口径（`kinds` / `words` / `rules` / `actions` / `allow` /
`shape` / `min_score`），**不改三个总开关**——避免一次「恢复推荐」把正在生效的脱敏悄悄关掉，
也避免把关闭中的实例擅自打开。该动作走 `PUT /api/privacy/settings`，仍是全量替换 + 写后回读。

## 5. 占位符与仓库

### 5.1 占位符

```
{{KIND_xxxxxxxx}}
```

`xxxxxxxx = base32lower(HMAC-SHA256(key, KIND + "\x00" + value))[:8]`。
同一个值得到同一个占位符；不同值碰撞概率可忽略。恢复时接受模型改写后的形式：大小写变化、
缺一侧花括号、`{{ X }}` 的空格（magpie `restore` 的容错）。

### 5.2 脱敏键

- 首次需要时生成 32 字节随机值，以 base64 存入 `options.PBRPrivacyKey`。
- 不通过任何 API 回显，不进导出/导入，不写日志。
- 键丢失（重建库）后占位符会变化；旧回复中的旧占位符无法恢复，按未知占位符原样保留。

### 5.3 仓库（vault）

- 进程内 `map[placeholder]value`，读写加锁；**永不落盘**。
- 上限 200,000 个值；超出时按最近使用时间淘汰最旧一半（magpie `makeRoom` 语义）。
- 重启后仓库为空；后续请求带回的值会重新登记。回复中出现未知占位符时原样保留。

## 6. 转发管道

### 6.1 请求

在 `GetAndValidateRequest` **之前**、鉴权与日志载体建立之后执行：

1. 读取入站 body（沿用基座 body storage，最大 16 MiB 既有上限）。
2. 仅当 `Content-Type` 为 JSON 或入站格式是已知 JSON 协议时，调用 `redact.MaskJSON`；
   multipart、二进制、音频/图像上传不脱敏，`mask_status=unsupported`。
3. 命中动作 `block` → 不转发，返回 `400 privacy_blocked`，消息只含 kind/首字符/长度。
4. 否则把脱敏后的 body 写回 storage，再解析成请求结构；后续协议转换、上游发送、
   用量/上下文解析看到的都是脱敏后的正文。
5. 渠道测试（`route_source=test`）不脱敏。

脱敏后的 body 必须仍是合法 JSON；`MaskJSON` 失败（解析错误、编码错误）时按
`503 privacy_unavailable` 拦截，绝不发送原文。

### 6.2 响应

转发开始时用包装后的 `gin.ResponseWriter` 替换 `c.Writer`，在最终写出前恢复：

- `Content-Type: text/event-stream`：按 SSE 事件逐条恢复；跨事件的半个占位符先扣留，
  下一事件补齐后再写，保持事件边界与字节内容可拼接。
- `Content-Type` 为空但首块以 `event:` / `data:` / `:` 开头：按流处理。
- 其他文本/JSON：整块缓冲，结束时恢复。
- `audio/*`、`image/*`、`video/*`、`application/octet-stream`、`application/pdf`、`application/zip`
  或非 identity 的 `Content-Encoding`：原样透传。
- 恢复失败不得吞掉响应：包装器内部记 `mask_status=restore_failed`，仍写出可写内容。

### 6.3 失败策略

| 情况 | 行为 |
|---|---|
| 脱敏开启但引擎 panic / body 无法解析 | `503 privacy_unavailable`，不发送 |
| `actions[kind]=block` 命中 | `400 privacy_blocked`，不发送 |
| `model.required=true` 且模型未就绪 | `503 privacy_model_unavailable`，不发送 |
| 模型未启用/未下载/超时 | 规则继续，请求发送；`mask_status=model_unavailable` / `partial` |
| 脱敏关闭 | 零改动 |

## 7. 观测

### 7.1 日志字段

`pbr_request_logs` 新增：

| 字段 | 含义 |
|---|---|
| `masked_count` | 本请求替换的处数（`warn` 不计） |
| `mask_blocked` | 是否因 block 被拦截 |
| `mask_status` | `ok` / `partial` / `model_unavailable` / `truncated` / `unsupported` / `restore_failed` / `off` |
| `mask_hits` | JSON 数组，见下；无命中为 `[]` |
| `mask_took_ms` | 请求脱敏耗时（毫秒） |

`mask_hits` 元素：

```json
{"kind":"API_KEY","by":"rule","placeholder":"{{API_KEY_k3v9x2mq}}",
 "head":"sk-","len":51,"n":2,"action":""}
```

- `n` 是同一占位符在本请求中出现的次数；数组按首次出现排序。
- `head` 是首字符；密钥类取可识别前缀（最多 4 字符）。`len` 是字符数。
- **永不写原值、正文、工具参数或文件内容。**

### 7.2 查询

- `GET /api/logs?masked=true|false` 过滤；列表项带 `masked_count` / `mask_blocked` / `mask_status`。
- `GET /api/privacy/records`：按 `from/to/kind/by/action/blocked/client/model` 分页，
  数据源是日志表 `masked_count > 0`，返回 `mask_hits` 与请求摘要。
- `GET /api/privacy/summary?days=`：窗口内命中处数、受影响请求数、被拦截请求数、按 kind 计数。
- 清理沿用 `POST /logs/prune`；脱敏记录不单独保留。

## 8. 管理 API

所有端点挂管理密钥；信封与错误码遵循 [`api-spec-v1.md`](api-spec-v1.md) §2/§3。
新增端点必须登记 `internal/api/ops_routes.go` 并声明 dry-run 类别。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/privacy/settings` | 读设置（不含脱敏键） |
| PUT | `/api/privacy/settings?dry_run=` | 全量替换；`dry_run=true` 返回归一化设置与 diff，不落库 |
| GET | `/api/privacy/model` | 模型状态：`installed/downloading/done/total/error/loaded/device/min_score/size` |
| POST | `/api/privacy/model/download` | 触发下载（`dry_run` 拒绝，属外部动作） |
| POST | `/api/privacy/preview` | `{text}` → `{text, hits, active}`；不登记仓库、不落库 |
| GET | `/api/privacy/records` | 脱敏请求分页 |
| GET | `/api/privacy/summary` | 脱敏汇总 |

`PUT` 校验规则：

- `secrets`、`personal`、`model.enabled`、`model.required` 为布尔；
- 未知 `kinds` 键忽略；`actions` 的 kind 必须是已知/用户规则 kind，动作只能是 `warn`/`block`；
- `words` / `allow` / `rules` 超限或格式错误 → `422 validation_failed`；
- 成功返回落库后的设置（写后回读）；`dry_run=true` 返回 `{dry_run:true, settings, diff}`，不写库。

## 9. 控制台

- 新增「日志 › 脱敏」页签（`/logs/masking`），侧边栏可见；不新增一级导航。
- 页面结构：
  - 顶部 KPI：窗口内命中处数、受影响请求、被拦截请求、模型状态；
  - 设置卡：密钥/个人数据/模型开关、kinds 复选、words、rules、actions、allow、shape、
    预览输入框；
  - 记录表：时间、客户端、模型/车道、命中处数、状态、blocked；详情对话框展示 hits
    （kind / by / placeholder / head+len / n / action），不展示原值。
- 请求日志表格新增「脱敏」列：`—` / `N 处` / `已拦截`；筛选栏新增 `masked` 开关。
- 会话页的请求时间线显示脱敏徽标，复用同一日志字段。
- 文案走 i18n，7 个 locale 全量同步。

## 10. 与 magpie 的差异（必须保持可见）

| magpie | PBR | 原因 |
|---|---|---|
| Privacy Filter（1.6 GB GGUF + native `pf-serve`） | 不移植 | PBR 是纯 Go 单二进制；1.6 GB 原生进程与单二进制/部署边界冲突 |
| CoreML 加速 | 不移植 | 只在 macOS 有意义；PBR 生产是 Linux 容器 |
| 本地模型默认开启 | 默认关闭 | 下载 37 MB 与 CPU 开销必须由用户显式启用 |
| 记录请求正文并在页面左右对照 | 只记录命中元数据 | PBR 不存正文（design §8） |
| 按客户端密钥覆盖脱敏开关 | 实例级 | PBR 单用户，无按 key 策略 |
| 设置随配置备份携带 | 设置与模型文件是实例本地，不进 `/export` / `/import` | 模型路径与脱敏键是本机状态；跨实例导入不承诺占位符或模型可用 |
| 模型故障时严格拦截（用户开启） | `model.required` 显式开启才拦截 | 避免模型下载失败拖垮整个网关 |
| 模型分析全量长文本 | 单请求 256 KiB / 单字符串 64 KiB 上限 | 有界延迟与内存；超出部分规则仍生效 |

除上述差异外，占位符、规则判定、已知值传播、恢复容错、设置语义均以 magpie 为准，
不得自行简化或重写。

## 11. 验收

- L1：移植的 `internal/privacy/redact` 上游测试全绿；新增 `internal/privacy` 集成测试覆盖
  设置校验、占位符稳定性、仓库淘汰、JSON/SSE 恢复、失败策略。
- L2：`verify/e2e-api/ops_runbook.sh` 增加脱敏设置、预览、模型状态、日志命中与 summary。
- L3：`verify/e2e-ui/user_journey.py` 增加打开脱敏页、切换开关、预览、查看命中详情。
- L4：`verify/final/e2e.sh` 与 `fault_injection.sh` 回归，确认默认关闭时转发字节不变；
  开启后请求/流式回复仍可用。
- 隐私自查：仓库无原值、无请求正文、无脱敏键；`mask_hits` 只含元数据。
