/*
PowerBarRations —— PBR 路由（成员链/故障切换）管理 API（api-spec §5.7）

用户心智：渠道管理填上游与模型（命名不一致时配渠道映射）→ 模型管理里为每个模型定
"这个模型优先打谁、再打谁" → 令牌允许该模型。车道是唯一路由入口（ADR 0005）：
没有车道 = 模型不可调用（503）。本模块封装 /api/v1/models、/api/v1/routes/{model}、
把成员链固化为车道的 PUT /api/v1/lanes/{model}，以及一键固化 POST /api/v1/lanes/seed。
*/
import { api } from '@/lib/api'

/** GET /api/v1/models 的元素。 */
export interface PBRModelSummary {
  model: string
  /** explicit=已配车道可调用；unconfigured=渠道声明但没配车道，不可调用。 */
  source: 'explicit' | 'unconfigured' | string
  routable: boolean
  member_count: number
}

/** GET /api/v1/routes/{model} 返回的成员。 */
export interface PBRRouteMember {
  channel_id: number
  channel: string
  /** 解析后的上游真名（渠道映射/成员覆盖/路由键之一）。 */
  upstream_model: string
  /** 成员级显式改名原值；为空表示"用渠道映射"。 */
  upstream_override?: string
  public_alias?: string
  /** 车道内顺序：数字大者优先（保存时按数组位置生成）。 */
  priority: number
  member_id?: number
}

export interface PBRRouteDetail {
  model: string
  source: string
  routable?: boolean
  mode?: string
  route_key?: string
  active_member?: string
  members: PBRRouteMember[]
}

interface ListResponse<T> {
  items: T[]
}

/** 全部路由键（含是否可调用与成员数）。 */
export async function listPBRModels(): Promise<PBRModelSummary[]> {
  const res = await api.get<ListResponse<PBRModelSummary>>('/api/v1/models')
  return res.data.items ?? []
}

/** 解析某模型的成员链（已配车道返回真实链；未配返回建议链，routable=false）。 */
export async function getPBRRoute(model: string): Promise<PBRRouteDetail> {
  const res = await api.get<PBRRouteDetail>(
    `/api/v1/routes/${encodeURIComponent(model)}`
  )
  return res.data
}

/** 成员链编辑提交项。upstream_model 留空 = 用渠道映射。 */
export interface PBRMemberInput {
  channel: string
  upstream_model?: string
  /** 车道内顺序：数字大者优先；数组位置即顺序。 */
  priority: number
}

/**
 * 把某模型的成员链固化为顺序（故障切换）。
 *
 * 车道名 = 模型名，模式固定 failover：请求先打 priority 最高的成员，
 * 失败后按 routing-spec 的冷却/熔断逃逸到下一个。
 *
 * `config` 取系统设置里的**默认六键**（`GET /api/v1/system/options.lane_defaults`）：
 * 前端写死六键会在"用界面改一次成员顺序"时把该车道自定义过的超时/冷却/亲和
 * 静默重置（审查 F12）。取不到时回落到内置默认值。
 */
export async function savePBRFailover(
  model: string,
  members: PBRMemberInput[]
): Promise<void> {
  const config = await loadLaneDefaults()
  // 已有车道：保留其自身六键，只提交顺序（避免用默认值覆盖自定义配置）。
  const existing = await getPBRLaneConfig(model)
  await api.put(`/api/v1/lanes/${encodeURIComponent(model)}`, {
    enabled: true,
    mode: 'failover',
    config: existing ?? config,
    members,
  })
}

/** 读取系统设置里的默认六键；失败回落内置默认。 */
async function loadLaneDefaults(): Promise<Record<string, number>> {
  try {
    const res = await api.get<{ lane_defaults?: Record<string, number> }>(
      '/api/v1/system/options'
    )
    if (res.data.lane_defaults) {
      return res.data.lane_defaults
    }
  } catch {
    // 忽略：回落内置默认值
  }
  return BUILTIN_LANE_DEFAULTS
}

/** 取已存在车道的六键；不存在或读取失败返回 null。 */
async function getPBRLaneConfig(
  model: string
): Promise<Record<string, number> | null> {
  try {
    const res = await api.get<{ config?: Record<string, number> }>(
      `/api/v1/lanes/${encodeURIComponent(model)}`
    )
    return res.data.config ?? null
  } catch {
    return null
  }
}

/** 内置默认六键（与后端 model.BuiltinLaneRelayConfig 一致）。 */
export const BUILTIN_LANE_DEFAULTS = {
  member_max_attempts: 2,
  member_retry_interval_seconds: 3,
  member_non_stream_response_timeout_seconds: 120,
  member_stream_first_event_timeout_seconds: 30,
  member_cooldown_seconds: 60,
  member_affinity_seconds: 0,
} as const

/** 删除该模型的车道：删除后该模型不可调用（直到重新配置）。 */
export async function deletePBRFailover(model: string): Promise<void> {
  await api.delete(`/api/v1/lanes/${encodeURIComponent(model)}`)
}

export interface PBRSeedResult {
  created: string[]
  skipped: string[]
}

/**
 * 一键为所有"渠道已声明但无车道"的模型生成 failover 车道（初始顺序按渠道 id 升序）。
 * `dryRun` 只返回将创建的车道名，不落库。
 */
export async function seedPBRLanes(dryRun = false): Promise<PBRSeedResult> {
  const res = await api.post<PBRSeedResult>(
    `/api/v1/lanes/seed${dryRun ? '?dry_run=true' : ''}`
  )
  return res.data
}
