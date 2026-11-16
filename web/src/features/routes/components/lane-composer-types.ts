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
import type { PBRLaneMode, PBRMemberOverrides } from '../api'

/** 编排器里的一个已选链元素（草稿态，两态：散成员 | 组引用，ADR 0009）。 */
export interface ComposerMember {
  /**
   * 稳定标识：仅用于 React key / 拖拽身份。不能把 channel 或 model 拼进 key
   * ——同一渠道可含多个模型，拼进去会让行身份随选择变化漂移。
   * 也**不得**用后端的 `member_id`：成员写入是整体替换，保存一次全部重算
   * （api-spec §5.7）。
   */
  id: string
  channel: string
  /**
   * 成员**所选**模型（成员身份与唯一键的一半，保存写回用它）。
   * 必须来自渠道声明的模型清单——它是成员表唯一落库的模型字段。
   */
  model: string
  /**
   * 组引用元素：非空 = 本元素引用一个成员组（channel/model 为空串）。
   * 组内成员的增删/顺序/停用在组管理里维护，编排器只能移动组引用位置、
   * 开关本车道的引用或移除引用（ui-spec §6.3）。
   */
  group?: string
  /** 成员别名（可选，仅用于 manual 的 active_member 取值）。 */
  publicAlias?: string
  /**
   * 成员级六键覆盖草稿。**必须原样回传**：成员写入是全量替换，读不到即保存清空
   * （api-spec §4.2 读写闭环）。
   */
  overrides?: PBRMemberOverrides
  /** 人工停用开关草稿；`false` = 不参与选路（仍留在链里，可逆）。 */
  enabled: boolean
}

export interface LaneComposerProps {
  /** 新建时为空；编辑既有车道时为该车道名（路由键只读）。 */
  model?: string
  /** 打开时从后端载入的初始成员（编辑既有车道用）。 */
  initialMembers?: ComposerMember[]
  initialMode?: PBRLaneMode
  initialActiveMember?: string
  initialConfig?: Record<string, number> | null
  /** 编辑的是一条已停用车：保存会按现有语义重新启用它，界面必须明示。 */
  initiallyDisabled?: boolean
  onDirtyChange?: (dirty: boolean) => void
  onSaved: () => Promise<void> | void
  onCancel: () => void
}
