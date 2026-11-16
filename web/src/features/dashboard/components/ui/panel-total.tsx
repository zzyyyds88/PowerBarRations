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
// 卡片标题右侧的窗口汇总（ui-spec §6.2A）。
//
// 外部看板的做法：标题右边挂一个与卡片内容同源的总量（`Quota Distribution Total: ¥8.44`），
// 读者不必回到指标条就能对上"这张卡的数加起来是多少"。
//
// 文案走 `Total:`（i18n 键，各语言自带冒号/空格规则），再接等宽数字。
// 数据未到位（加载中/失败）时必须传 `value={null}` 而不是 0——"Total: 0" 是伪造数据。

import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

export function PanelTotal(props: {
  value: string | null
  className?: string
}) {
  const { t } = useTranslation()

  if (props.value == null) return null

  return (
    <span
      data-slot='panel-total'
      className={cn(
        'text-muted-foreground shrink-0 text-xs whitespace-nowrap',
        props.className
      )}
    >
      {t('Total:')}{' '}
      <span className='text-foreground font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
    </span>
  )
}
