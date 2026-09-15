/*
PowerBarRations —— PBR 路由（成员链/故障切换）管理 API（api-spec §5.7）

用户心智：渠道管理填上游与模型 → 模型管理里定"这个模型优先打谁、再打谁" → 令牌允许该模型。
本模块封装 PBR 的 /api/v1/models 与 /api/v1/routes/{model}，以及把成员链固化为
显式顺序的 PUT /api/v1/lanes/{model}。
*/
import { api } from '@/lib/api'

/** GET /api/v1/models 的元素。 */
export interface PBRModelSummary {
  model: string
  source: 'implicit' | 'explicit'
  member_count: number
}

/** GET /api/v1/routes/{model} 返回的成员。 */
export interface PBRRouteMember {
  channel_id: number
  channel: string
  upstream_model: string
  public_alias?: string
  priority: number
  weight: number
  member_id?: number
}

export interface PBRRouteDetail {
  model: string
  source: string
  mode?: string
  route_key?: string
  active_member?: string
  members: PBRRouteMember[]
}

interface ListResponse<T> {
  items: T[]
}

/** 全部可路由模型（含来源与成员数）。 */
export async function listPBRModels(): Promise<PBRModelSummary[]> {
  const res = await api.get<ListResponse<PBRModelSummary>>('/api/v1/models')
  return res.data.items ?? []
}

/** 解析某模型的成员链（当前按什么顺序、打哪些上游）。 */
export async function getPBRRoute(model: string): Promise<PBRRouteDetail> {
  const res = await api.get<PBRRouteDetail>(
    `/api/v1/routes/${encodeURIComponent(model)}`
  )
  return res.data
}

/** 成员链编辑提交项。 */
export interface PBRMemberInput {
  channel: string
  upstream_model: string
  priority: number
  weight?: number
}

/**
 * 把某模型的成员链固化为显式顺序（故障切换）。
 *
 * 车道名 = 模型名，模式固定 failover：请求先打 priority 最高的成员，
 * 失败后按 routing-spec 的冷却/熔断逃逸到下一个。
 */
export async function savePBRFailover(
  model: string,
  members: PBRMemberInput[]
): Promise<void> {
  const config = {
    member_max_attempts: 2,
    member_retry_interval_seconds: 3,
    member_non_stream_response_timeout_seconds: 120,
    member_stream_first_event_timeout_seconds: 30,
    member_cooldown_seconds: 60,
    member_affinity_seconds: 0,
  }
  await api.put(`/api/v1/lanes/${encodeURIComponent(model)}`, {
    enabled: true,
    mode: 'failover',
    config,
    members,
  })
}

/** 删除该模型的显式成员链，回到"渠道声明即自动成链"的隐式语义。 */
export async function deletePBRFailover(model: string): Promise<void> {
  await api.delete(`/api/v1/lanes/${encodeURIComponent(model)}`)
}
