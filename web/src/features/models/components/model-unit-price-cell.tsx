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
import { useTranslation } from 'react-i18next'

import { usePBRModelPrices } from '../pbr-model-prices'

// 模型页「上游单价」：展示 PBR 全局默认上游单价（人民币/百万 token）。
// 渠道里配置的上游单价优先；本列只展示全局默认口径，仅用于成本折算。
export function ModelUnitPriceCell(props: { modelName: string }) {
  const { t } = useTranslation()
  const { data } = usePBRModelPrices()
  const price = data?.get(props.modelName)
  if (!price) {
    return (
      <span className='text-muted-foreground text-xs'>
        {t('Not configured')}
      </span>
    )
  }
  return (
    <span className='font-mono text-xs tabular-nums'>
      ¥{price.input || 0} / ¥{price.output || 0}
      <span className='text-muted-foreground'> · 1M</span>
    </span>
  )
}
