# 更新日志

本文件记录公开版本的重要变更，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；本项目遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Added
- 单二进制 + SQLite 的单用户 LLM 聚合网关：渠道声明模型、**车道为唯一路由入口**（ADR 0005）。
- 上游协议选择器：OpenAI 兼容 / OpenAI Responses / Anthropic / Gemini 四种协议；API 地址填到 `/v1`（或 `/v1beta`）即可，网关自动补全端点路径。
- 故障转移与容错：成员顺序故障转移、冷却、亲和、熔断三态与半开复通、429 与硬故障分类。
- 管理面完整 HTTP API + OpenAPI：`/api/v1/{channels,lanes,routes,keys,logs,stats,export,import,system/options}`。
- 控制台：渠道管理、模型管理（按名推断并自动采用图标）、路由与故障切换、系统任务、令牌、用量与成本看板、系统设置。
- 上游模型探测：渠道编辑器内手动探测，探测结果用居中选择弹窗按需合并。
- 成本视图：按渠道 x 模型的请求数、token、成功率与上游折算花费（元）。
- CLI：`pbr auth reset`。

### Changed
- 控制台首页重做为 PBR 真实能力：模型即路由键、同名车道、优先级故障转移、冷却 / 亲和 / 熔断；新增「一次请求如何被路由」区块；统计口径修正为 **4** 个上游协议。
- 控制台图标换为新的闪电电源徽标（AI 生成母版 + 可复现生成脚本）。
- **管理面响应信封统一**（api-spec §2/§3）：成功一律裸资源（列表 `{items,next_cursor}`、动作类 `{changed}`/`{deleted}` 等语义化最小对象），失败一律 `{error:{code,message,hint,details?}}` 并带真实 HTTP 状态码；不再有 `{success,message,data}` 包装与"200 承载业务失败"。稳定契约端点与控制台内部端点共用同一信封。
- **运维端点按名寻址**：`channels/batch/{status,tag}` 接受 `channels:[渠道名]`（`ids` 仍兼容，二者只能给一个）；`model-catalog/batch-delete` 接受 `models:[模型名]`；`channels/batch/copy` 改为 body `{channel}`。修复 `batch/copy` 与 `batch/fetch-models` 此前因参数不一致而不可用的问题。
- **运维端点路由与响应策略同表声明**（design-v1 §16.3）：新增 `internal/api/ops_routes.go` 与 `internal/apiresp`，守卫测试强制每条已注册管理路由都有信封覆盖。

### Security
- 客户端密钥由服务端随机生成并明文入库，管理 API 可随时回读复制；鉴权仍走 `sha256` 哈希索引。管理密钥由登录口令派生、服务端只存其哈希、不落明文。
- 请求日志只存元数据，不存请求/响应正文与密钥明文。
- 管理面（`/api`、`/api/v1`）统一请求体上限（默认 2MB，`ANONYMOUS_REQUEST_BODY_LIMIT_KB`）；模型面仍为 128MB。
- CORS 不再对任意来源同时放行凭据；模型面不再接受 `?key=` 查询串凭据，访问日志去除查询串；上游错误响应体脱敏后再入日志；登录失败退避改为按来源返回 429 + `Retry-After`。

### Removed
- 删除死代码 `createRootAccountIfNeed`（`root`/`123456` 默认账号）与内部施工文档。
- 移除旧 `new-api`/`octopus` 两层网关的迁移机制：`internal/legacy` 包、`pbr migrate` 子命令与 `MIGRATION.md`。新实例只使用新建的 PBR 数据库（design-v1 §11）。

[Unreleased]: https://github.com/zzyyyds88/PowerBarRations/commits/main
