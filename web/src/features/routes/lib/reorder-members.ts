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
车道成员拖拽重排的纯函数（ui-spec §6.3、ADR 0006）。

成员链顺序即故障切换顺序（priority 数字大者优先，保存时按数组下标生成），
所以"拖拽"只是对**数组顺序**的一次纯变换：不碰 React、不碰 DOM、不碰网络，
便于按 test-spec §3.1 的边界用例逐条单测（拖到首位/末位/相邻/自身/未知 id）。
原生 HTML5 DnD 的接线在 lane-composer.tsx（参照 param-override-editor-dialog.tsx
的 handleDragStart/Over/Drop + resetDragState），**不引入任何 dnd 依赖**。
*/

/**
 * 把 `sourceId` 行移动到 `targetId` 行的前/后。
 *
 * 边界语义（都返回**原数组引用**，让调用方能靠引用相等跳过无意义的 setState）：
 * - 任一侧为空、两侧相同、或任一 id 不在列表里 → 原样返回；
 * - `targetId` 就是 `sourceId` → 原样返回（拖到自身是 no-op，不得抖动顺序）。
 *
 * 注意 `insertIdx` 在**移除 source 之后**的数组上计算，因此向下拖（target 在
 * source 之后）时不需要额外减一：移除已经让下标左移了。
 */
export function reorderMembers<T extends { id: string }>(
  list: T[],
  sourceId: string,
  targetId: string,
  position: 'before' | 'after' = 'before'
): T[] {
  if (!sourceId || !targetId || sourceId === targetId) return list
  const sourceIdx = list.findIndex((item) => item.id === sourceId)
  if (sourceIdx < 0) return list
  const next = [...list]
  const [moved] = next.splice(sourceIdx, 1)
  let insertIdx = next.findIndex((item) => item.id === targetId)
  if (insertIdx < 0) return list
  if (position === 'after') insertIdx += 1
  next.splice(insertIdx, 0, moved)
  return next
}

/**
 * 由数组顺序生成成员 `priority`（routing-spec §1.2：数字大者优先）。
 *
 * 现存规则是"priority = 成员数 - 下标"，即数组首位拿到最大数字。抽成纯函数
 * 是为了让"拖拽后提交的 priority 与新下标严格一一对应"可被单测直接断言
 * （test-spec §3.1：断言生成结果，不是内部 state）。
 */
export function priorityForIndex(index: number, total: number): number {
  return total - index
}
