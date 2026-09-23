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
import type { LaneHealthSnapshot } from '@/lib/route-events'

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
  /**
   * 车道存在时：成员链里被**人工停用**（成员 `enabled=false`）的成员数。
   * 用来把"我自己关掉的"与"上游挂了的"分开：`degraded && disabled_member_count
   * === member_count` 是"成员全被我关了"（api-spec §5.7、ui-spec §6.3）。
   */
  disabled_member_count?: number
}

/** 成员级六键覆盖（六个可空数值，缺席 = 继承车道，api-spec §4.2）。 */
export type PBRMemberOverrides = Record<string, number>

/** GET /api/v1/routes/{model} 返回的成员。 */
export interface PBRRouteMember {
  channel_id: number
  channel: string
  /** 该渠道是否启用（供界面标灰，与"成员被人工停用"是两件事）。 */
  channel_enabled?: boolean
  /**
   * 成员**所选**模型（成员身份与唯一键的一半，保存写回用它）。
   *
   * 成员不再存上游真名：真名恒由 `渠道 model_mapping[model] ?? model` 推导
   * （ADR 0008），因此改渠道映射对该车道所有成员立即生效。
   */
  model: string
  /**
   * **派生只读**的上游真名 = `渠道 model_mapping[model] ?? model`（ADR 0008）。
   * 供展示与排障，**不可写回**（写回会被服务端忽略，且写端点根本不接受该字段）。
   */
  upstream_model: string
  public_alias?: string
  /** 车道内顺序：数字大者优先（保存时按数组位置生成）。 */
  priority: number
  /**
   * 成员当前库 id，仅供排障与审计定位。**不承诺稳定**（成员写入是整体替换，
   * 保存一次所有 member_id 都会变），因此禁止用作 React key 或拖拽身份
   * （api-spec §5.7）——行标识一律用前端自有的草稿 id。
   */
  member_id?: number
  /** 成员级人工停用开关（恒回，含 false；false = 不参与选路，api-spec §4.2）。 */
  enabled?: boolean
  /** 成员级六键覆盖，**恒回**，无覆盖时为 `{}`（api-spec §5.7）。 */
  overrides?: PBRMemberOverrides
}

export interface PBRRouteDetail {
  model: string
  source: string
  routable?: boolean
  mode?: string
  route_key?: string
  active_member?: string
  /** 车道六键（后端 GET /api/v1/routes/{model} 一并返回）。 */
  config?: Record<string, number>
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

/**
 * 成员选择器的候选目录：每个启用渠道 + 它声明的模型清单（ADR 0006）。
 *
 * 成员候选 = **任意启用渠道的任意已声明模型**，与路由键是否同名无关；
 * 数据源直接复用 `GET /api/v1/channels`（已返回每渠道 models 与 model_mapping），
 * 不新增专用端点。`model_mapping` 供界面派生成员的上游真名展示。
 */
export interface PBRChannelCatalogEntry {
  name: string
  enabled: boolean
  models: string[]
  /** 模型名 → 上游真名；成员的上游真名恒由它按成员所选模型推导（ADR 0008）。 */
  model_mapping: Record<string, string>
}

/** 拉取全部渠道的模型目录（成员选择器左栏）。 */
export async function listPBRChannelCatalog(): Promise<
  PBRChannelCatalogEntry[]
> {
  // 自用规模一页取全；limit 有上限时按 next_cursor 继续翻。
  interface ChannelListResponse {
    items?: {
      name: string
      enabled?: boolean
      models?: string[]
      model_mapping?: Record<string, string>
    }[]
    next_cursor?: string | null
  }
  const entries: PBRChannelCatalogEntry[] = []
  let cursor: string | null = null
  for (let page = 0; page < 50; page += 1) {
    const res: { data: ChannelListResponse } =
      await api.get<ChannelListResponse>('/api/v1/channels', {
        params: { limit: 200, cursor: cursor ?? undefined },
      })
    for (const item of res.data.items ?? []) {
      entries.push({
        name: item.name,
        enabled: item.enabled !== false,
        models: item.models ?? [],
        model_mapping: item.model_mapping ?? {},
      })
    }
    cursor = res.data.next_cursor ?? null
    if (!cursor) break
  }
  return entries
}

/**
 * 车道成员的唯一键 = `(渠道, 所选模型)`（ADR 0008）。
 *
 * 同一渠道的**不同**模型可在一条车道内出现多次，因此**不能按渠道去重**；
 * 同一渠道的同一模型在一条车道内只能出现一次（后端另有 `422 duplicate_member` 兜底）。
 * 键用**成员所选模型**而非派生真名：真名会随渠道 `model_mapping` 改动漂移，
 * 用它去重会让"同一模型换了个映射"被误判成另一个成员。
 */
export function memberKey(member: { channel: string; model: string }): string {
  return `${member.channel}\u0000${member.model}`
}

/** GET /api/v1/lanes 的精简项：车道名 + 有序成员（成员数组顺序即故障切换顺序）。 */
export interface PBRLaneSummaryMember {
  channel: string
  /** 成员**所选**模型（成员身份，ADR 0008）。 */
  model: string
  /** **派生只读**的上游真名 = `渠道 model_mapping[model] ?? model`，供卡片展示解析结果。 */
  upstream_model: string
  /** 成员别名（可选）。后端**已在返回**，此前前端未声明、未消费（api-spec §5.7）。 */
  public_alias?: string
  /** 该渠道是否启用（供卡片标灰；与"成员被人工停用"是两件事）。 */
  channel_enabled?: boolean
  /**
   * 成员是否参与选路（恒回）。`false` = 被人工停用，卡片标灰（ui-spec §6.3）。
   * 注意与 `PBRLaneSummary.enabled`（车道级停用）同名不同层级（api-spec §5.7）。
   */
  enabled?: boolean
}

export interface PBRLaneSummary {
  name: string
  members: PBRLaneSummaryMember[]
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
      members?: PBRLaneSummaryMember[]
    }[]
  }>('/api/v1/lane-summaries')
  return (res.data.items ?? []).map((lane) => ({
    name: lane.name,
    orphan_member_count: lane.orphan_member_count ?? 0,
    members: (lane.members ?? []).map((m) => ({
      channel: m.channel,
      model: m.model,
      upstream_model: m.upstream_model,
      public_alias: m.public_alias,
      channel_enabled: m.channel_enabled,
      // 字段缺席按"参与选路"处理：老后端不返回时不得把成员误标成已停用。
      enabled: m.enabled !== false,
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

/**
 * 成员链编辑提交项。
 *
 * **读写闭环（api-spec §4.2 通则，强制）**：成员写入是**全量替换**（先删光再重插），
 * 所以草稿里读到的每个成员级字段都必须在保存时原样带回，否则一次控制台保存就会
 * 把它们清空——`public_alias` / `overrides` 都因此丢过数据。
 * `enabled` 是三态：省略 = 新建成员启用、既有成员保留原值，**绝不能**把"未读到"
 * 当成 `false`（那会把整条成员链关掉）。
 *
 * `upstream_model` **不属于**这条闭环：它是服务端由渠道映射推导的只读派生值
 * （ADR 0008），写端点不接受该字段，前端只展示、不回传。
 */
export interface PBRMemberInput {
  channel: string
  /** 成员所选模型，**必填**（缺了后端返回 400 `validation_failed`）。 */
  model: string
  /** 成员别名；空串 = 无别名。不带上会被全量替换清空。 */
  public_alias?: string
  /** 车道内顺序：数字大者优先；数组位置即顺序。 */
  priority: number
  /** 人工停用开关（三态：省略 = 新建启用 / 既有保留原值）。 */
  enabled?: boolean
  /** 成员级六键覆盖；不带上会被全量替换清空。 */
  overrides?: PBRMemberOverrides
}

export type PBRLaneMode = 'failover' | 'manual'

/** 成员链保存选项：模式、manual 的 active member 与六键。 */
export interface PBRSaveLaneOptions {
  mode?: PBRLaneMode
  /** 仅 manual 模式：成员别名，或 "channel/model" 标签（model = 成员所选模型）。 */
  activeMember?: string
  /**
   * 车道六键。编排器显式传入（含用户在「高级」区改过的值）；
   * 不传则沿用该车道已有的六键，再回落 `lane_defaults`。
   */
  config?: Record<string, number>
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
  // 已有车道：保留其自身六键，只提交顺序（避免用默认值覆盖自定义配置）。
  const existing = await getPBRLaneDetail(model)
  // 六键来源优先级：调用方显式传入 > 车道已有 > lane_defaults。
  const config =
    options?.config ?? existing?.config ?? (await loadLaneDefaults())
  await api.put(`/api/v1/lanes/${encodeURIComponent(model)}`, {
    // 保存即让这条车道生效：被停用的车道在用户点保存后重新启用。
    enabled: true,
    mode: options?.mode ?? existing?.mode ?? 'failover',
    active_member: options?.activeMember ?? existing?.activeMember ?? '',
    config,
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

/** 已存在车道的模式 / active_member / 六键（编辑弹窗初始化用）。 */
export interface PBRLaneDetail {
  mode: PBRLaneMode
  activeMember: string
  config: Record<string, number> | null
}

/** 读取某车道的模式与六键；车道不存在返回 null。 */
export async function getPBRLane(model: string): Promise<PBRLaneDetail | null> {
  const detail = await getPBRLaneDetail(model)
  if (detail.mode === null && detail.config === null) return null
  return {
    mode: detail.mode ?? 'failover',
    activeMember: detail.activeMember ?? '',
    config: detail.config,
  }
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

/** 单车道运行态快照（GET /api/v1/lanes/{name}/health，api-spec §6.5 形状）。 */
export async function getPBRLaneHealth(
  lane: string
): Promise<LaneHealthSnapshot> {
  const res = await api.get<LaneHealthSnapshot>(
    `/api/v1/lanes/${encodeURIComponent(lane)}/health`
  )
  return res.data
}

/**
 * 轮询兜底用：并发拉取多条车道的 health 快照。
 *
 * 单条车道失败不影响其余车道（allSettled）：兜底源本就允许不完整的对账结果，
 * 失败车道下一轮（30s）自动重来；SSE 正常时这里的结果只做对账不主导渲染。
 */
export async function pollPBRLaneHealth(
  lanes: string[]
): Promise<LaneHealthSnapshot[]> {
  const results = await Promise.allSettled(lanes.map(getPBRLaneHealth))
  return results.flatMap((r) => (r.status === 'fulfilled' ? [r.value] : []))
}
