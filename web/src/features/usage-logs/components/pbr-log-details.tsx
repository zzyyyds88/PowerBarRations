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

import type { UsageLog } from '../data/schema'
import { parseLogOther } from '../lib/format'
import { PBRAttemptTimeline } from '../pbr/components/pbr-attempt-timeline'
import { DetailRow, DetailSection } from './dialogs/log-detail-layout'

/**
 * PBR 日志详情内容（ui-spec §6.6 详情行展开）：车道/route_source/upstream_model/
 * http_status/total_ms/estimated_cost 字段网格 + attempts 逐尝试时间线 + error_summary。
 * 行展开子行与 details-dialog 弹窗共用此组件。
 */
export function PBRLogDetailsContent({ log }: { log: UsageLog }) {
  const { t } = useTranslation()
  const other = parseLogOther(log.other)
  if (!other?.pbr) return null
  const pbr = other.pbr
  return (
    <>
      <DetailSection label={t('Request Details')}>
        {pbr.lane && <DetailRow label={t('Lane')} value={pbr.lane} mono />}
        {pbr.route_source && (
          <DetailRow label={t('Route Source')} value={pbr.route_source} mono />
        )}
        {pbr.upstream_model && (
          <DetailRow
            label={t('Upstream Model')}
            value={pbr.upstream_model}
            mono
          />
        )}
        {pbr.inbound_format && (
          <DetailRow
            label={t('Inbound Format')}
            value={pbr.inbound_format}
            mono
          />
        )}
        <DetailRow
          label={t('HTTP Status')}
          value={String(pbr.http_status)}
          mono
        />
        {pbr.total_ms > 0 && (
          <DetailRow
            label={t('Total Time')}
            value={`${pbr.total_ms} ms`}
            mono
          />
        )}
        {pbr.estimated_cost > 0 && (
          <DetailRow
            label={t('Estimated Cost')}
            value={pbr.estimated_cost.toFixed(4)}
            mono
          />
        )}
        {pbr.error_summary && (
          <DetailRow label={t('Error Summary')} value={pbr.error_summary} />
        )}
      </DetailSection>
      {pbr.attempts.length > 0 && (
        <DetailSection label={t('Attempt Chain')}>
          <PBRAttemptTimeline attempts={pbr.attempts} />
        </DetailSection>
      )}
    </>
  )
}
