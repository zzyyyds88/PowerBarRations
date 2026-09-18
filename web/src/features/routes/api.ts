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
  /**
   * explicit=已配车道可调用；unconfigured=渠道声明但没配车道；
   * disabled=有同名车道但被停用（两者都不可调用）。
   */
  source: 'explicit' | 'unconfigured' | 'disabled' | string
  routable: boolean
  /** 车道成员总数（含渠道已删/停用的悬空成员）；unconfigured 时为候选渠道数。 */
  member_count: number
  /** 当前真正可路由的成员数（渠道存在且启用）。 */
  available_member_count: number
  /** explicit 车道：当前未冷却且熔断非 open 的成员数（运行态健康）。 */
  healthy_member_count?: number
  /** explicit 车道：参与健康统计的成员总数。 */
  health_member_count?: number
  /** explicit 车道：所有成员当前都不可选（全冷却/熔断）时为 true。 */
  degraded?: boolean
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
  /** 渠道已不存在的悬空成员数（历史数据；可用「清理悬空成员」修复）。 */
  orphan_member_count: number
}

/** 清理渠道已不存在的悬空车道成员；成员清空的车道整条删除。 */
export async function cleanupPBROrphanMembers(): Promise<{
  cleaned_lanes: string[]
  deleted_lanes: string[]
}> {
  const res = await api.post<{
    cleaned_lanes?: string[]
    deleted_lanes?: string[]
  }>('/api/v1/lanes/cleanup-members')
  return {
    cleaned_lanes: res.data.cleaned_lanes ?? [],
    deleted_lanes: res.data.deleted_lanes ?? [],
  }
}

/**
 * 车道顺序摘要，供「路由与故障切换」页的「成员数与顺序摘要」列使用。
 *
 * 走稳定端点 GET /api/v1/lane-summaries（**不分页**）：此前用 GET /api/v1/lanes 的
 * cursor 上限 200，车道数超过后该列会退化为只显示数量（api-spec §5.7）。
 */
export async function listPBRLaneSummaries(): Promise<PBRLaneSummary[]> {
  const res = await api.get<{
    items?: {
      name: string
      orphan_member_count?: number
      members?: { channel: string; upstream_model: string }[]
    }[]
  }>('/api/v1/lane-summaries')
  return (res.data.items ?? []).map((lane) => ({
    name: lane.name,
    orphan_member_count: lane.orphan_member_count ?? 0,
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

export type PBRLaneMode = 'failover' | 'manual'

/** 成员链保存选项：模式与 manual 的 active member。 */
export interface PBRSaveLaneOptions {
  mode?: PBRLaneMode
  /** 仅 manual 模式：成员别名，或 "channel/upstream_model" 标签。 */
  activeMember?: string
}

/**
 * 把某模型的成员链固化为顺序。
 *
 * 车道名 = 模型名。`mode` 默认 failover（按 priority 逃逸）；
 * `manual` 时由 `active_member` 指定唯一生效成员（routing-spec §5）。
 *
 * `config` 取系统设置里的**默认六键**（`GET /api/v1/system/options.lane_defaults`）：
 * 前端写死六键会在"用界面改一次成员顺序"时把该车道自定义过的超时/冷却/亲和
 * 静默重置（审查 F12）。取不到时回落到内置默认值。
 */
export async function savePBRFailover(
  model: string,
  members: PBRMemberInput[],
  options?: PBRSaveLaneOptions
): Promise<void> {
  const config = await loadLaneDefaults()
  // 已有车道：保留其自身六键，只提交顺序（避免用默认值覆盖自定义配置）。
  const existing = await getPBRLaneDetail(model)
  await api.put(`/api/v1/lanes/${encodeURIComponent(model)}`, {
    // 保存即让这条车道生效：被停用的车道在用户点保存后重新启用。
    enabled: true,
    mode: options?.mode ?? existing?.mode ?? 'failover',
    active_member: options?.activeMember ?? existing?.activeMember ?? '',
    config: existing?.config ?? config,
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

/** 已存在车道的六键与模式；不存在或读取失败返回 null。 */
async function getPBRLaneDetail(model: string): Promise<{
  config: Record<string, number> | null
  mode: PBRLaneMode | null
  activeMember: string | null
}> {
  try {
    const res = await api.get<{
      config?: Record<string, number>
      mode?: string
      active_member?: string
    }>(`/api/v1/lanes/${encodeURIComponent(model)}`)
    const mode = res.data.mode
    let parsedMode: PBRLaneMode | null = null
    if (mode === 'manual' || mode === 'failover') {
      parsedMode = mode
    }
    return {
      config: res.data.config ?? null,
      mode: parsedMode,
      activeMember: res.data.active_member ?? null,
    }
  } catch {
    return { config: null, mode: null, activeMember: null }
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
