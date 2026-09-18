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
PowerBarRations —— PBR 路由（成员链/故障切换）管理 API（api-spec §5.7）

用户心智：渠道管理填上游与模型（命名不一致时配渠道映射）→ 模型管理里为每个模型定
"这个模型优先打谁、再打谁" → 令牌允许该模型。车道是唯一路由入口（ADR 0005）：
没有车道 = 模型不可调用（503）。本模块封装 /api/v1/models、/api/v1/routes/{model}、
把成员链固化为车道的 PUT /api/v1/lanes/{model}。成员链只支持手工添加/删除。
*/
import { api } from '@/lib/api'

// 路由键列表的共享 queryKey：「路由与故障切换」页与成员链面板/抽屉共用同一
// 缓存条目，抽屉里保存/删除成员链后 invalidate 该前缀即可同时刷新两者。
export const pbrModelsQueryKey = ['pbr-routable-models'] as const

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
  /**
   * 已配车道时，后端额外返回「声明了该模型但不在成员链里」的候选渠道，
   * 让新增渠道声明后可以直接加成员，不必删掉车道重建（ui-spec §6.3）。
   */
  candidates?: PBRRouteMember[]
}

interface ListResponse<T> {
  items: T[]
}

/** 全部路由键（含是否可调用与成员数）。 */
export async function listPBRModels(): Promise<PBRModelSummary[]> {
  const res = await api.get<ListResponse<PBRModelSummary>>('/api/v1/models')
  return res.data.items ?? []
}

/** GET /api/v1/lanes 的精简项：车道名 + 有序成员（成员数组顺序即故障切换顺序）。 */
export interface PBRLaneSummary {
  name: string
  members: { channel: string; upstream_model: string }[]
}

/**
 * 车道列表摘要，供「路由与故障切换」页的「成员数与顺序摘要」列使用。
 *
 * GET /api/v1/lanes 是 cursor 分页（api-spec §5.2）：这里取最大页 200 条，
 * 超出部分该列退化为只显示成员数（GET /api/v1/models 的 member_count 不受影响）。
 */
export async function listPBRLaneSummaries(): Promise<PBRLaneSummary[]> {
  const res = await api.get<{
    items?: {
      name: string
      members?: { channel: string; upstream_model: string }[]
    }[]
    next_cursor: unknown
  }>('/api/v1/lanes', { params: { limit: 200 } })
  return (res.data.items ?? []).map((lane) => ({
    name: lane.name,
    members: (lane.members ?? []).map((m) => ({
      channel: m.channel,
      upstream_model: m.upstream_model,
    })),
  }))
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

// 一键固化（POST /api/v1/lanes/seed）已按产品要求整体移除：成员链只支持手工
// 添加/删除，不再提供批量生成入口。
