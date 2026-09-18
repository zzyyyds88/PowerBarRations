# PowerBarRations Hermes 适配规格 v1

> 规范性配套文件，从属于 [`design-v1.md`](design-v1.md) §1、§3、§4 与
> [`api-spec-v1.md`](api-spec-v1.md)。
> 本文定义 Hermes 的固定接入方式；PBR 仍是通用的本机多模型聚合网关，其他工具和模型使用同一模型面。
> 文中 `<hermes-lane>` 是车道名占位符，由部署方自行命名（遵循 design-v1 文档纪律：不写具体车道/模型名）。

## 1. Hermes 在 PBR 中的位置

PBR 的首要验收消费者是本机 Hermes。PBR 替代 Hermes 原先依赖的 `new-api + octopus`
两层网关，向 Hermes 提供一个稳定的 OpenAI Chat Completions 入口，向 Hermes 或其运维代理
提供管理 API。

Hermes 使用 `<hermes-lane>` 作为一条显式车道；其他模型各自使用自己的路由键和车道，
不得被 Hermes 的适配约束限制：

```text
Hermes -> PBR /v1/chat/completions, model=<hermes-lane>
       -> Lane <hermes-lane>
       -> channel-a/upstream-model-a -> channel-b/upstream-model-b -> ...

其他工具 -> PBR /v1/*, model=<自己的路由键>
         -> Lane <自己的路由键>
```

PBR 的目标不是兼容旧网关的数据文件。开发阶段只使用新建的 PBR SQLite 数据库，
不提供旧 `new-api`/`octopus` 数据库迁移、双写、自动回填、历史记录恢复或旧数据兼容兜底。

## 2. Hermes 固定接入档案

Hermes 使用一个命名自定义 provider；PBR 不要求 Hermes 增加专用代码或专用协议。
配置字段遵循 Hermes 当前的 `providers` 形态：

```yaml
providers:
  pbr:
    api: http://127.0.0.1:5700/v1
    transport: chat_completions
    key_env: PBR_HERMES_CLIENT_KEY
    default_model: <hermes-lane>
    models:
      <hermes-lane>: {}
```

- `api` 指向 PBR 的 `/v1`，不得指向管理面 `/api`。
- `transport` 固定为 `chat_completions`。
- `PBR_HERMES_CLIENT_KEY` 是仅供 Hermes 使用的 ClientKey；管理密钥不得填入这里。
- ClientKey 默认可设为只允许 `<hermes-lane>`，避免 Hermes 意外调用其他车道。
- `<hermes-lane>` 同时是 Hermes 的模型名、PBR 的路由键和 Lane 名；不使用隐式车道。

## 3. `<hermes-lane>` 车道语义

- 车道必须显式创建并启用；渠道声明模型只提供候选成员，不会自动成为 Hermes 的路由。
- 成员顺序就是故障转移顺序。成员可使用不同渠道、不同上游真名，解析优先级仍遵循
  成员 `upstream_model` > 渠道 `model_mapping` > `<hermes-lane>`。
- 故障转移只发生在 `<hermes-lane>` 车道内部；请求不得跨到其他车道或隐式选择其他模型。
- 429/限流等软故障不得误触发硬故障熔断；超时、连接失败和上游硬错误按
  [`routing-spec-v1.md`](routing-spec-v1.md) 分类并记录 attempts。
- PBR 成功响应中的 `model` 必须回填为 `<hermes-lane>`，上游真名仅通过
  `X-Served-By` 和管理日志暴露。

## 4. Hermes 数据面兼容要求

`POST /v1/chat/completions` 必须覆盖 Hermes 日常工作流所需的 OpenAI 兼容语义：

- 非流式与 SSE 流式响应；
- `tools`、`tool_choice`、并行工具调用、工具结果消息与多轮消息；
- 常用生成参数、结构化输出字段和上游适配器已支持的多模态消息；
- 流式文本、reasoning、tool-call delta、`finish_reason`、usage 的稳定转发；
- 上游错误的 HTTP 状态与可诊断错误信息；网关不伪造成功响应；
- 长上下文请求不因网关自身的固定短 body 限制被截断，超时使用车道六键配置。

PBR 不为 Hermes 另造 `/hermes/*` 数据面，也不在网关内维护第二套工具调用或思考参数
转换表；协议转换和厂商差异继续由现有 relay adapter 负责。

## 5. Hermes 运维 API

Hermes 或其运维代理只通过 PBR 管理 API 操作，不读写 SQLite、不解析控制台、不调用上游
渠道接口。管理密钥与 Hermes ClientKey 分离。

| 运维目标 | 稳定 API |
|---|---|
| 检查网关 | `GET /api/health`、`GET /api/capabilities` |
| 查看/维护渠道 | `GET/PUT /api/channels/{name}`、`POST /api/channels/{name}/test` |
| 查看/维护 Hermes 车道 | `GET/PUT /api/lanes/<hermes-lane>`、`GET /api/lanes/<hermes-lane>/health` |
| 逐成员探活 | `POST /api/lanes/<hermes-lane>/probe` |
| 立即复通 | `POST /api/lanes/<hermes-lane>/circuits/reset` |
| 查看故障链 | `GET /api/logs?lane=<hermes-lane>` |
| 管理 Hermes 凭据 | `GET/PUT /api/keys/{name}`、`POST /api/keys/{name}/rotate` |

写操作必须使用完整对象、写后回读，并依据 HTTP 状态和稳定 `error.code` 判断结果。
具体字段和错误契约以 [`api-spec-v1.md`](api-spec-v1.md) 为准；本表不新增 Hermes 专用
管理端点，避免形成第二套运维面。

## 6. Hermes 验收

开发阶段的最小验收闭环：

1. 用两个假上游渠道创建 `<hermes-lane>`，首成员返回硬失败时请求能落到次成员；
2. Hermes 风格的带 tools 请求在流式和非流式模式都能完成，工具调用消息不丢失；
3. 429 不会把成员永久禁用，超时和连接错误能在 attempts 中定位；
4. 通过管理 API 修改成员顺序、探活、查看健康状态和重置熔断，不依赖数据库文件；
5. 用一个只允许 `<hermes-lane>` 的 ClientKey 调用成功，调用其他模型返回稳定拒绝。

