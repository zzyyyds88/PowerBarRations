/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/*
渠道状态（api-spec §6.12 正本 / §5.3 端点表）：

- `GET /api/channels/{name}/status?window=24h|7d` 单渠道详情（当前状态 + 车道引用 +
  成员运行态 + 窗口统计与逐日序列 + 最近失败 + 探活历史 + 上游模型变动提示）；
- `GET /api/channels/status?window=24h|7d` 全量汇总（只读徽章；不含 daily /
  recent_failures / probe_history）；
- `POST /api/channels/{name}/test` 单渠道探活——每次探活写一条探活记录
  （`pbr_probe_logs`），是 `probe_history` 的唯一写入者。

成功一律读**裸对象**（api-spec §2.3）；失败由 axios 拒绝并交由既有
`handleServerError` / `getServerErrorMessage` 机制处理，不自造信封。
`/api/v1/...` 是同一批处理器的兼容别名，这里用规范前缀。
*/
import { api, type ApiRequestConfig } from '@/lib/api'

/** 窗口取值域：其余值后端回 400 validation_failed（查询参数取值校验走 400，api-spec §6.12）。 */
export type ChannelStatusWindow = '24h' | '7d'

/** 渠道配置摘要（详情 `channel` 字段；只读，`key` 只暴露前缀）。 */
export interface ChannelStatusChannelInfo {
  name: string
  enabled: boolean
  /** 稳定面适配器名（如 `openai`），不是控制台基座面的数字 type。 */
  type: string
  base_url: string
  key_set: boolean
  key_prefix: string
  models: string[]
  model_mapping: Record<string, string>
}

/** 引用该渠道的成员（详情 `reference.lanes` 元素）。 */
export interface ChannelStatusLaneReference {
  lane: string
  /** 成员所选模型（路由键）。 */
  model: string
  /** 该成员推导出的上游真名（渠道映射决定）。 */
  upstream_model: string
  priority: number
  /** 该成员是否参与选路；false = 被人工停用（配置态，不是故障）。 */
  enabled: boolean
}

/**
 * 成员运行态（详情 `runtime.members` 元素，进程内运行态，重启即清空）。
 *
 * 字段与车道健康快照同义（api-spec §6.5）：`available=false` 不蕴含故障结论，
 * 只有 `enabled=false` 才说明"是运维关的"。
 */
export interface ChannelStatusMemberRuntime {
  lane: string
  /** 成员标签（`channelId:upstreamModel`）。 */
  member: string
  enabled: boolean
  available: boolean
  /** 熔断三态：closed | open | half-open。 */
  circuit: string
  /** 冷却截止（RFC3339；null = 不在冷却）。 */
  cooldown_until: string | null
  /** 熔断打开到期（RFC3339；circuit 非 open 时为 null）。 */
  circuit_open_until: string | null
  consecutive_failures: number
  failure_score: number
  /** 滚动成功率，0~1 的小数（与 `summary.success_rate` 同口径）。 */
  rolling_success_rate: number
  /** routing-spec §4.1 失败分类；空串 = 无留痕。 */
  last_error_kind: string
}

export interface ChannelStatusRuntime {
  member_count: number
  /** 只计"渠道启用、成员未被人工停用、且不在冷却/熔断中"的成员。 */
  available_count: number
  members: ChannelStatusMemberRuntime[]
}

/** 窗口汇总（`requests` 为 0 时 `success_rate` 为 0）。 */
export interface ChannelStatusSummary {
  requests: number
  successes: number
  failures: number
  /** 0~1 的小数（`successes / requests`）。 */
  success_rate: number
  /** 明细日志均值（会被 `POST /logs/prune` 影响；无样本为 0）。 */
  avg_total_ms: number
  /** 明细日志均值，只计流式请求（无样本为 0）。 */
  avg_ttft_ms: number
  /** 该窗口按渠道级 `prices` 折算的上游花费（元）。 */
  estimated_cost: number
}

/** 窗口按自然日（本地时区）切分的桶：`24h` 最多 2 个、`7d` 最多 7 个。 */
export interface ChannelStatusDailyPoint {
  date: string
  requests: number
  successes: number
  failures: number
  estimated_cost: number
}

/** 最近失败明细（最多 20 条；`route_source=test` 的探活记录不计入）。 */
export interface ChannelStatusRecentFailure {
  id: number
  ts: string
  lane: string
  model: string
  upstream_model: string
  http_status: number
  /** routing-spec §4.1 失败分类。 */
  error_kind: string
  error_summary: string
}

/** 探活历史（最多 20 条；每渠道只保留最近 200 条）。 */
export interface ChannelStatusProbeRecord {
  ts: string
  upstream_model: string
  /** success | failed（探活结论）。 */
  status: string
  duration_ms: number
  http_status: number
  error_kind: string
  msg: string
}

/** 上次"检测上游模型变动"的快照；从未探测过时 `last_check_ts` 为 null。 */
export interface ChannelStatusUpstreamUpdates {
  last_check_ts: string | null
  pending_add: string[]
  pending_remove: string[]
}

/**
 * 单模型在窗口内的表现（详情 `models[]` 元素，渠道状态页的主表；按请求数降序）。
 *
 * `output_tps` 的口径是"吐字速度"：`Σcompletion_tokens / Σ生成耗时`，生成耗时 =
 * 流式 `max(total_ms − ttft_ms, 1)`、非流式 `total_ms`——首字等待被排除在外
 * （api-spec §6.12）。数据源是明细日志，`POST /logs/prune` 后该表为空数组。
 */
export interface ChannelStatusModelStat {
  /** 上游真名（渠道映射后的实际调用名）。 */
  model: string
  requests: number
  successes: number
  failures: number
  /** 0~1 小数；`requests = 0` 时为 0（无样本，展示侧按 '-' 处理）。 */
  success_rate: number
  /** 明细日志均值；无样本为 0。 */
  avg_total_ms: number
  /** 明细日志均值，只计流式请求；无样本为 0。 */
  avg_ttft_ms: number
  /** 输出速度（tokens/s）；无样本为 0（展示侧按 '-' 处理，不显示 0）。 */
  output_tps: number
  /** 输出 token 总数。 */
  tokens_out: number
}

/** `GET /api/channels/{name}/status` 的裸响应。 */
export interface ChannelStatusDetail {
  channel: ChannelStatusChannelInfo
  reference: { lanes: ChannelStatusLaneReference[] }
  runtime: ChannelStatusRuntime
  window: ChannelStatusWindow | string
  summary: ChannelStatusSummary
  /** 按上游模型的窗口表现（渠道状态页与抽屉「模型状态」块的主表）。 */
  models: ChannelStatusModelStat[]
  daily: ChannelStatusDailyPoint[]
  recent_failures: ChannelStatusRecentFailure[]
  probe_history: ChannelStatusProbeRecord[]
  upstream_updates: ChannelStatusUpstreamUpdates
}

/** `GET /api/channels/status` 的 items 元素（全量汇总，无副作用）。 */
export interface ChannelStatusSummaryItem {
  channel: string
  enabled: boolean
  type: string
  /** 引用该渠道的车道数（去重）。 */
  lane_count: number
  member_count: number
  available_count: number
  /** 至少一个成员当前不可选（冷却/熔断/人工停用）。 */
  degraded: boolean
  requests: number
  failures: number
  success_rate: number
  estimated_cost: number
  last_error_kind: string
  /** 最近一条探活记录；从未探活时省略该字段。 */
  last_probe?: { ts: string; status: string; duration_ms: number }
}

/** `GET /api/channels/status` 的裸响应。 */
export interface ChannelStatusSummaryResponse {
  window: ChannelStatusWindow | string
  items: ChannelStatusSummaryItem[]
}

/** `POST /api/channels/{name}/test` 的探活结论（api-spec §6.4 的成员结果形状）。 */
export interface ChannelProbeResult {
  channel: string
  upstream_model: string
  status: string
  duration_ms: number
  error_kind?: string
  msg?: string
  ok: boolean
  status_code: number
  latency_ms: number
  error?: string
}

// 探活是"管理动作"，失败要由调用方决定展示什么（与 api.ts 的 channelActionConfig 同约定）。
const probeRequestConfig: ApiRequestConfig = { skipErrorHandler: true }

/** 单渠道状态详情（`GET /api/channels/{name}/status`）。 */
export async function getChannelStatus(
  name: string,
  window: ChannelStatusWindow
): Promise<ChannelStatusDetail> {
  const res = await api.get<ChannelStatusDetail>(
    `/api/channels/${encodeURIComponent(name)}/status`,
    { params: { window } }
  )
  return res.data
}

/** 全部渠道的状态汇总（`GET /api/channels/status`）。 */
export async function listChannelStatus(
  window: ChannelStatusWindow
): Promise<ChannelStatusSummaryResponse> {
  const res = await api.get<ChannelStatusSummaryResponse>(
    '/api/channels/status',
    {
      params: { window },
    }
  )
  return res.data
}

/**
 * 单渠道探活（`POST /api/channels/{name}/test`）：每次调用落一条探活记录，
 * 供状态详情的「探活历史」展示；模型缺省时后端取该渠道声明的第一个模型。
 */
export async function probeChannelStatus(
  name: string
): Promise<ChannelProbeResult> {
  const res = await api.post<ChannelProbeResult>(
    `/api/channels/${encodeURIComponent(name)}/test`,
    undefined,
    probeRequestConfig
  )
  return res.data
}
