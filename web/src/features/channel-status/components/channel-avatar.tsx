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
import { getAvatarColorClass } from '@/lib/colors'
import { cn } from '@/lib/utils'

/** 渠道头像：名称首字 + 按名称稳定取色（同一渠道在表格与实时路由里颜色一致）。 */
export function ChannelAvatar(props: {
  name: string
  className?: string
  size?: 'md' | 'lg'
}) {
  const size = props.size ?? 'md'
  return (
    <span
      aria-hidden='true'
      className={cn(
        'flex shrink-0 items-center justify-center rounded-lg font-semibold uppercase',
        size === 'lg' ? 'size-9 text-sm' : 'size-8 text-xs',
        getAvatarColorClass(props.name),
        props.className
      )}
    >
      {props.name.slice(0, 1)}
    </span>
  )
}
