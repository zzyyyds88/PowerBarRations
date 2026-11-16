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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { CONSOLE_ROUTES } from '@/config/routes'

import type { UsageLog } from '../data/schema'
import { parseLogOther } from '../lib/format'
import { PBRAttemptTimeline } from '../pbr/components/pbr-attempt-timeline'
import { getPBRRequestLog } from '../pbr/pbr-logs-api'
import { DetailRow, DetailSection } from './dialogs/log-detail-layout'
import { PBRContextCard } from './pbr-context-card'

/**
 * PBR 日志详情内容（ui-spec §6.6 详情行展开）：车道/route_source/upstream_model/
 * http_status/total_ms/estimated_cost 字段网格 + attempts 逐尝试时间线 + error_summary
 * + 客户端/会话与上下文卡片（usage-spec §4–§5）。行展开子行与 details-dialog 弹窗共用。
 */
export function PBRLogDetailsContent({ log }: { log: UsageLog }) {
  const { t } = useTranslation()
  const other = parseLogOther(log.other)
  const pbr = other?.pbr
  const detailQuery = useQuery({
    queryKey: ['pbr-log-detail', log.id],
    queryFn: () => getPBRRequestLog(log.id),
    enabled: Boolean(pbr),
    staleTime: 60_000,
  })
  if (!pbr) return null
  return (
    <>
      <DetailSection label={t('Request Details')}>
        {pbr.lane && <DetailRow label={t('Route')} value={pbr.lane} mono />}
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
        {pbr.client_name && (
          <DetailRow label={t('Client')} value={pbr.client_name} mono />
        )}
        {pbr.session_id && (
          <DetailRow
            label={t('Session')}
            value={
              <a
                href={`${CONSOLE_ROUTES.logsSessions}?session=${encodeURIComponent(pbr.session_key || pbr.session_id)}`}
                className='text-primary font-mono underline-offset-4 hover:underline'
              >
                {pbr.session_id}
              </a>
            }
          />
        )}
        {pbr.subagent && (
          <DetailRow label={t('Subagent')} value={pbr.subagent} mono />
        )}
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
      {((detailQuery.data?.mask_hits?.length ?? 0) > 0 ||
        (pbr.masked_count ?? 0) > 0) && (
        <DetailSection label={t('Masking')}>
          <DetailRow
            label={t('Masked')}
            value={String(pbr.masked_count ?? 0)}
            mono
          />
          <DetailRow
            label={t('Status')}
            value={pbr.mask_blocked ? t('Blocked') : pbr.mask_status}
            mono
          />
          {(detailQuery.data?.mask_hits ?? []).map((hit) => (
            <DetailRow
              key={`${hit.kind}-${hit.placeholder}`}
              label={hit.kind}
              value={`${hit.placeholder} · ${hit.head ? `${hit.head}…` : ''}${hit.len} ×${hit.n}${hit.action ? ` · ${hit.action}` : ''}`}
              mono
            />
          ))}
        </DetailSection>
      )}
      <DetailSection label={t('Context')}>
        <PBRContextCard
          context={detailQuery.data?.context}
          status={pbr.context_status}
          tokens={pbr.context_tokens}
          window={pbr.context_window}
          counted={pbr.context_counted}
        />
      </DetailSection>
      {pbr.attempts.length > 0 && (
        <DetailSection label={t('Attempt Chain')}>
          <PBRAttemptTimeline attempts={pbr.attempts} />
        </DetailSection>
      )}
    </>
  )
}
