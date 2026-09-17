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

/**
 * 居中弹窗统一规范（ui-spec §6.9）。
 *
 * 关键约束：**外框尺寸恒定**——同一档位在任何内容长度、任何页签、任意增删行下
 * 都渲染相同的外框；内容超出只在正文区滚动。禁止调用方再用 ad-hoc 的
 * `max-w-*` / `contentHeight` 撑高弹窗（那正是"一个弹窗一种样式"的根因）。
 *
 * 独立于 `dialog.tsx`：该文件只导出组件以保持 fast refresh，尺寸 token 与
 * 类型集中在这里，仍是"尺寸只有一个来源"。
 */
export type DialogSize = 'sm' | 'md' | 'lg' | 'xl'

/**
 * 各档位固定外框。`sm` 是唯一的自适应档（确认/告警短内容），但仍有最小高度
 * 与统一宽度，避免短文案弹窗忽大忽小。
 *
 * 导出给需要自定义 header/footer 结构、因而直接使用 `ui/dialog` 的少数调用方。
 */
export const DIALOG_SIZE_CLASS: Record<DialogSize, string> = {
  sm: 'w-[min(92vw,480px)] min-h-[160px] max-h-[min(86vh,560px)]',
  md: 'w-[min(92vw,720px)] h-[min(78vh,560px)]',
  lg: 'w-[min(94vw,960px)] h-[min(82vh,640px)]',
  xl: 'w-[min(96vw,1200px)] h-[min(86vh,720px)]',
}
