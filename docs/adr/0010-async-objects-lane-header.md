# ADR 0010: 异步对象（Files / Batch）以 `X-PBR-Lane` 显式寻址

- 状态：已接受（2026-11-06）
- 关联：[design §4.2](../design-v1.md)、[routing-spec §4.5](../routing-spec-v1.md)、[api-spec §5.1](../api-spec-v1.md)

## 背景

PBR 的唯一选路键是**模型名**（ADR 0001/0005）：`model` 解析为车道，车道再按成员链选择
渠道 × 模型。OpenAI Files / Batch 是异步对象接口，请求体里没有 `model`：

- `POST /v1/files` 只有 multipart 文件与 `purpose`；
- `POST /v1/batches` 只有 `input_file_id`、`endpoint`、`completion_window`；
- 后续 retrieve / content / cancel 只带对象 ID。

没有模型名就无法套用车道路由；而异步任务一旦提交就固定在某个上游（输入文件、批处理
状态、输出文件都在那一家），不能像同步请求一样换成员。把异步对象硬塞进"按模型选路"
会产生两个不可接受的结果：一是无法确定该打哪个渠道，二是故障转移会制造重复对象或丢对象。

## 决策

1. **显式寻址**：`/v1/files*` 与 `/v1/batches*` 的全部请求必须带 `X-PBR-Lane: <车道名>`。
   缺失 → `400 lane_header_required`；名字不存在 → `404 lane_not_found`。该头**不转发**给上游。
   这是"路由键 = 模型名"的**唯一显式例外**，不引入第二条隐式选路路径。
2. **创建即固定**：创建 file / batch 时，按车道当前可用成员选第一个（与同步请求同一套
   冷却 / 熔断 / 人工停用过滤），把 `(lane, upstream_object_id) → channel_id` 落
   `pbr_async_objects`；后续请求只回该渠道。
3. **创建阶段也不 failover**：上传体不可重放，重试可能产生重复对象；创建只打一个成员，
   失败直接返回，由客户端决定是否重发。已有的异步对象不受成员冷却或车道变更影响。
4. **跨车道隔离**：对象只在创建它的车道可见；用别的车道寻址 → `404 async_object_not_found`；
   记录渠道不可用 → `503 async_object_channel_unavailable`，不静默换渠道。
5. **协议边界**：只支持 OpenAI 兼容协议渠道；Claude / Gemini / 签名类适配器 →
   `400 unsupported_protocol_for_async`。
6. **不落盘**：上传/下载按流代理，单请求上限 512 MiB（可由 `system/options.upload_max_mb` 收紧），
   batch JSON 上限 8 MiB。元数据行在上游对象过期后保留 30 天，清理走既有系统任务，不引入 cron。

## 后果

- 客户端（或 SDK 包装层）必须能为 Files/Batch 附加一个请求头；标准 OpenAI SDK 支持自定义
  header，但需要显式配置。这是可接受的一次性接入成本。
- 异步对象**没有故障转移**，这是语义要求而非缺陷；渠道故障时返回明确错误，由运维恢复。
- 新增 `pbr_async_objects` 元数据表（不含文件内容），需要新的清理项与导出边界说明。
- 日志只记请求元数据，`estimated_cost=0`（上游异步计费不可见），不参与成本看板口径。

## 备选方案（未采用）

- **`?lane=` 查询参数**：不污染 header 生态，但会进入 URL/访问日志，且网关必须在转发前
  剔除参数；错误率更高。
- **路径前缀 `/v1/lanes/{lane}/…`**：REST 最干净，但不是 OpenAI 标准路径，SDK 需要改写
  base_url 或代理，破坏"OpenAI 兼容"卖点。
- **隐式按最近使用车道 / 唯一车道推断**：与"不做隐式选路"（ADR 0005）冲突，多车道时不可预测。
- **不做 Batch/Files**：可行，但会留下一个同类网关普遍支持的明确能力缺口。
