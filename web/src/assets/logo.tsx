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
import type { ImgHTMLAttributes } from 'react'

import { cn } from '@/lib/utils'

/**
 * 品牌主图标：与 favicon / 顶栏 / 侧边栏共用同一张 `/logo.png`。
 * 图形本身带明亮渐变底色，因此用圆角裁切而不是 currentColor 线性图标；
 * 这样控制台内所有入口的 mark 完全一致。
 */
export function Logo({
  className,
  ...props
}: ImgHTMLAttributes<HTMLImageElement>) {
  return (
    <img
      id='pbr-logo'
      src='/logo.png'
      alt='PowerBarRations'
      className={cn('size-6 rounded-md object-cover', className)}
      {...props}
    />
  )
}
