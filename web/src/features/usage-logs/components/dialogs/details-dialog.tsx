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
import {
  AlertTriangle,
  Check,
  Copy,
  Globe,
  LogIn,
  Route,
  Settings2,
  ShieldCheck,
  UserCog,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { formatTokens, formatUseTime } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { UsageLog } from '../../data/schema'
import {
  getParamOverrideActionLabel,
  getReasoningEffortVariant,
  getResponseTimeColor,
  parseAuditLine,
  parseLogOther,
  renderAuditContent,
} from '../../lib/format'
import { getLogTypeConfig, isTimingLogType } from '../../lib/utils'
import { PBRAttemptTimeline } from '../../pbr/components/pbr-attempt-timeline'
import type { LogOtherData } from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

// Maps a channel-update changed-field token (as recorded by the backend audit)
// to its i18n label key for display in the audit details.
const CHANNEL_FIELD_LABELS: Record<string, string> = {
  status: 'Status',
  models: 'Models',
  group: 'Group',
  type: 'Type',
  base_url: 'Base URL',
  key: 'Key',
}

function timingTextColorClass(
  variant: 'success' | 'warning' | 'danger'
): string {
  if (variant === 'success') return 'text-emerald-600'
  if (variant === 'warning') return 'text-amber-600'
  return 'text-rose-600'
}

function quotaSaturationKindLabel(
  kind: 'overflow' | 'underflow' | 'nan',
  t: (key: string) => string
): string {
  if (kind === 'overflow') return t('Overflow')
  if (kind === 'underflow') return t('Underflow')
  return t('Invalid (NaN)')
}

/**
 * 通用日志详情弹窗（本地化 New API `details-dialog` 形态）。
 *
 * PBR 只存元数据：这里展示请求标识、渠道/重试链、模型映射、token 用量、
 * 流式状态、参数覆盖与内容。**不出现**额度/计费/分组/订阅/充值等无关区块；
 * PBR 请求日志的 attempts 链由「PBR 请求日志」分节的详情弹窗展示。
 */
function TokenBreakdown(props: { log: UsageLog; other: LogOtherData }) {
  const { t } = useTranslation()
  const log = props.log
  const other = props.other

  const promptTokens = log.prompt_tokens || 0
  const completionTokens = log.completion_tokens || 0
  const cacheRead = other.cache_tokens || 0
  const cacheWrite = other.cache_creation_tokens || 0
  const cacheWrite5m = other.cache_creation_tokens_5m || 0
  const cacheWrite1h = other.cache_creation_tokens_1h || 0
  const hasTokens = promptTokens > 0 || completionTokens > 0

  if (!hasTokens) return null

  const rows: Array<{ label: string; value: string }> = [
    { label: t('Input Tokens'), value: promptTokens.toLocaleString() },
    { label: t('Output Tokens'), value: completionTokens.toLocaleString() },
  ]

  if (cacheRead > 0) {
    rows.push({ label: t('Cache Read'), value: cacheRead.toLocaleString() })
  }
  if (other.image_cache_tokens !== undefined) {
    rows.push({
      label: t('Image Cache'),
      value: other.image_cache_tokens.toLocaleString(),
    })
  }
  if (cacheWrite > 0 && cacheWrite5m === 0 && cacheWrite1h === 0) {
    rows.push({ label: t('Cache Write'), value: cacheWrite.toLocaleString() })
  }
  if (cacheWrite5m > 0) {
    rows.push({
      label: t('Cache Write (5m)'),
      value: cacheWrite5m.toLocaleString(),
    })
  }
  if (cacheWrite1h > 0) {
    rows.push({
      label: t('Cache Write (1h)'),
      value: cacheWrite1h.toLocaleString(),
    })
  }
  if (other.image && other.image_output) {
    rows.push({
      label: t('Image Tokens'),
      value: other.image_output.toLocaleString(),
    })
  }

  return (
    <DetailSection label={t('Token Breakdown')}>
      {rows.map((row) => (
        <DetailRow key={row.label} label={row.label} value={row.value} mono />
      ))}
    </DetailSection>
  )
}

interface DetailsDialogProps {
  log: UsageLog
  isAdmin: boolean
  isRoot: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function DetailsDialog(props: DetailsDialogProps) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  const other = parseLogOther(props.log.other)
  const typeConfig = getLogTypeConfig(props.log.type)

  const isRefund = props.log.type === 6
  const isTopup = props.log.type === 1
  const isManage = props.log.type === 3
  const isLogin = props.log.type === 7
  const isDisplayableType = [0, 2, 5, 6].includes(props.log.type)
  const hasAudioTokens = other?.ws || other?.audio
  const showTiming = isTimingLogType(props.log.type)
  const showAdminIp =
    !!props.log.ip && (showTiming || (props.isAdmin && isTopup))
  const adminInfo = other?.admin_info

  const manageOperator = (() => {
    if (!isManage || !props.isAdmin || !adminInfo) return null
    const username = adminInfo.admin_username
    const id = adminInfo.admin_id
    const hasUsername = username != null && String(username).trim() !== ''
    const hasId = id != null && String(id).trim() !== ''
    if (!hasUsername && !hasId) return null
    if (hasUsername && hasId) return `${username} (ID: ${id})`
    if (hasUsername) return String(username)
    return `ID: ${id}`
  })()

  const authMethodLabel = (() => {
    if (!isManage || !props.isAdmin || !adminInfo?.auth_method) return ''
    if (adminInfo.auth_method === 'access_token') return t('Access Token')
    if (adminInfo.auth_method === 'session') return t('Session')
    return String(adminInfo.auth_method)
  })()

  const operationText = renderAuditContent(other, t)
  const details = (isTopup ? operationText : null) ?? props.log.content ?? ''
  const auditRoute = isManage && props.isAdmin ? other?.audit_info : undefined
  // Channel update records which fields changed (stable field tokens); render
  // them with their localized labels for admins.
  const changedFieldTokens =
    isManage &&
    props.isAdmin &&
    Array.isArray(other?.op?.params?.changed_fields)
      ? (other.op.params.changed_fields as string[])
      : []
  const changedFieldsText = changedFieldTokens
    .map((field) => t(CHANNEL_FIELD_LABELS[field] ?? field))
    .join(', ')
  const showManageAuditSection =
    isManage && props.isAdmin && (operationText != null || auditRoute != null)

  const loginAuditFields = isLogin
    ? ([
        other?.login_method && {
          label: t('Login Method'),
          value: String(other.login_method),
        },
        props.log.ip && {
          label: t('IP Address'),
          value: props.log.ip,
        },
        other?.user_agent && {
          label: t('User Agent'),
          value: String(other.user_agent),
        },
      ].filter(Boolean) as Array<{ label: string; value: string }>)
    : []

  const conversionChain =
    other && Array.isArray(other.request_conversion)
      ? other.request_conversion.filter(Boolean)
      : []
  const conversionLabel =
    conversionChain.length <= 1
      ? t('Native format')
      : conversionChain.join(' -> ')
  const showConversion =
    props.isAdmin &&
    props.log.type !== 6 &&
    (other?.request_path || conversionChain.length > 0)

  const useChannel = other?.admin_info?.use_channel
  const channelChain =
    useChannel && useChannel.length > 0 ? useChannel.join(' → ') : undefined
  const reasoningEffortVariant = getReasoningEffortVariant(
    other?.reasoning_effort
  )

  return (
    <Dialog
      size='xl'
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        <>
          {t('Log Details')}
          <StatusBadge
            label={t(typeConfig.label)}
            variant={typeConfig.color as StatusBadgeProps['variant']}
            size='sm'
            copyable={false}
          />
        </>
      }
      description={t('View the complete details for this log entry')}
      contentClassName={cn(
        'min-w-0 overflow-hidden',
        'max-sm:max-h-[calc(100dvh-1.5rem)] max-sm:w-[calc(100vw-1.5rem)] max-sm:max-w-[calc(100vw-1.5rem)] max-sm:p-4',
        'sm:max-w-lg'
      )}
      headerClassName='max-sm:gap-1'
      titleClassName='flex items-center gap-2 text-base'
      descriptionClassName='sr-only'
      bodyClassName='pr-2 sm:pr-4'
    >
      <div className='w-full max-w-full min-w-0 space-y-2.5 overflow-x-hidden py-1 sm:space-y-3'>
        {/* Overview section - key identifiers */}
        <div className='min-w-0 space-y-1'>
          {props.log.request_id && (
            <DetailRow
              label={t('Request ID')}
              value={props.log.request_id}
              mono
            />
          )}
          {props.log.upstream_request_id && (
            <DetailRow
              label={t('Upstream Request ID')}
              value={props.log.upstream_request_id}
              mono
            />
          )}

          {props.isAdmin && props.log.channel > 0 && (
            <DetailRow
              label={t('Channel')}
              value={
                <span>
                  {props.log.channel}
                  {props.log.channel_name && (
                    <span className='text-muted-foreground'>
                      {' '}
                      ({props.log.channel_name})
                    </span>
                  )}
                </span>
              }
              mono
            />
          )}

          {channelChain && props.isAdmin && (
            <DetailRow label={t('Retry Chain')} value={channelChain} mono />
          )}

          {props.log.token_name && (
            <DetailRow label={t('Token')} value={props.log.token_name} mono />
          )}

          {showAdminIp && (
            <DetailRow
              label={t('IP Address')}
              value={
                <span className='flex items-center gap-1'>
                  <Globe className='size-3 text-amber-500' aria-hidden='true' />
                  {props.log.ip}
                </span>
              }
              mono
            />
          )}

          {showTiming && props.log.use_time > 0 && (
            <DetailRow
              label={t('Response Time')}
              value={
                <span
                  className={cn(
                    'font-medium',
                    timingTextColorClass(
                      getResponseTimeColor(
                        props.log.use_time,
                        props.log.completion_tokens
                      )
                    )
                  )}
                >
                  {formatUseTime(props.log.use_time)}
                  {props.log.is_stream &&
                    other?.frt != null &&
                    other.frt > 0 && (
                      <span className='font-normal'>
                        {' '}
                        (FRT: {formatUseTime(other.frt / 1000)})
                      </span>
                    )}
                </span>
              }
            />
          )}
        </div>

        {/* PBR 请求详情：车道/route_source/upstream_model/http_status/耗时/折算成本 + attempts 链 */}
        {other?.pbr && (
          <>
            <DetailSection label={t('Request Details')}>
              {other.pbr.lane && (
                <DetailRow label={t('Lane')} value={other.pbr.lane} mono />
              )}
              {other.pbr.route_source && (
                <DetailRow
                  label={t('Route Source')}
                  value={other.pbr.route_source}
                  mono
                />
              )}
              {other.pbr.upstream_model && (
                <DetailRow
                  label={t('Upstream Model')}
                  value={other.pbr.upstream_model}
                  mono
                />
              )}
              {other.pbr.inbound_format && (
                <DetailRow
                  label={t('Inbound Format')}
                  value={other.pbr.inbound_format}
                  mono
                />
              )}
              <DetailRow
                label={t('HTTP Status')}
                value={String(other.pbr.http_status)}
                mono
              />
              {other.pbr.total_ms > 0 && (
                <DetailRow
                  label={t('Total Time')}
                  value={`${other.pbr.total_ms} ms`}
                  mono
                />
              )}
              {other.pbr.estimated_cost > 0 && (
                <DetailRow
                  label={t('Estimated Cost')}
                  value={other.pbr.estimated_cost.toFixed(4)}
                  mono
                />
              )}
              {other.pbr.error_summary && (
                <DetailRow
                  label={t('Error Summary')}
                  value={other.pbr.error_summary}
                />
              )}
            </DetailSection>
            {other.pbr.attempts.length > 0 && (
              <DetailSection label={t('Attempt Chain')}>
                <PBRAttemptTimeline attempts={other.pbr.attempts} />
              </DetailSection>
            )}
          </>
        )}

        {/* Request conversion (admin only, not for refund) */}
        {showConversion && (
          <DetailSection label={t('Request Conversion')}>
            <div className='relative min-w-0'>
              <Button
                variant='ghost'
                size='sm'
                className='absolute top-0 right-0 h-5 w-5 p-0'
                onClick={() => copyToClipboard(conversionLabel)}
                title={t('Copy to clipboard')}
                aria-label={t('Copy to clipboard')}
              >
                {copiedText === conversionLabel ? (
                  <Check className='size-3 text-green-600' />
                ) : (
                  <Copy className='size-3' />
                )}
              </Button>
              <div className='min-w-0 space-y-1 pr-6'>
                {other?.request_path && (
                  <DetailRow
                    label={t('Path')}
                    value={other.request_path}
                    mono
                  />
                )}
                <div className='flex min-w-0 items-center gap-1.5 text-xs'>
                  <Route
                    className='text-muted-foreground size-3'
                    aria-hidden='true'
                  />
                  <span className='min-w-0 break-all sm:wrap-break-word'>
                    {conversionLabel}
                  </span>
                </div>
              </div>
            </div>
          </DetailSection>
        )}

        {/* Quota saturation marker (admin only) */}
        {props.isAdmin && other?.admin_info?.quota_saturation && (
          <DetailSection
            icon={<AlertTriangle className='size-3.5' aria-hidden='true' />}
            label={t('Quota clamped')}
            variant='danger'
          >
            <p className='mb-1 text-xs wrap-break-word'>
              {t('Quota saturation protection triggered')}
            </p>
            <DetailRow
              label={t('Kind')}
              value={quotaSaturationKindLabel(
                other.admin_info.quota_saturation.kind,
                t
              )}
            />
            <DetailRow
              label={t('Original value')}
              value={String(other.admin_info.quota_saturation.original)}
              mono
            />
            <DetailRow
              label={t('Clamped to')}
              value={String(other.admin_info.quota_saturation.clamped)}
              mono
            />
            <DetailRow
              label={t('Operation')}
              value={other.admin_info.quota_saturation.op}
              mono
            />
          </DetailSection>
        )}

        {/* Reject reason (admin only) */}
        {props.isAdmin && adminInfo?.reject_reason && (
          <DetailSection
            icon={<AlertTriangle className='size-3.5' aria-hidden='true' />}
            label={t('Reject Reason')}
            variant='danger'
          >
            <p className='text-xs wrap-break-word'>{adminInfo.reject_reason}</p>
          </DetailSection>
        )}

        {/* Refund details (type=6) */}
        {isRefund && other?.reason && (
          <DetailSection label={t('Refund Details')}>
            <DetailRow label={t('Reason')} value={other.reason} />
          </DetailSection>
        )}

        {props.isRoot && other?.root_info?.node_name ? (
          <DetailSection label={t('Root Diagnostics')}>
            <DetailRow
              label={t('Node Name')}
              value={other.root_info.node_name}
              mono
            />
          </DetailSection>
        ) : null}

        {/* Manage operator (type=3, admin only) */}
        {manageOperator && (
          <DetailRow
            label={
              <span className='flex items-center gap-1.5'>
                <UserCog
                  className='text-muted-foreground size-3.5'
                  aria-hidden='true'
                />
                {t('Operator Admin')}
              </span>
            }
            value={manageOperator}
            mono
          />
        )}

        {/* Operation audit info (type=3, admin only) */}
        {showManageAuditSection && (
          <DetailSection
            icon={<ShieldCheck className='size-3.5' aria-hidden='true' />}
            iconTone='info'
            label={t('Operation Audit Info')}
          >
            {operationText != null && (
              <DetailRow label={t('Operation')} value={operationText} />
            )}
            {authMethodLabel !== '' && (
              <DetailRow
                label={t('Authentication Method')}
                value={authMethodLabel}
              />
            )}
            {changedFieldsText !== '' && (
              <DetailRow
                label={t('Changed Fields')}
                value={changedFieldsText}
              />
            )}
            {auditRoute?.method && auditRoute?.route && (
              <DetailRow
                label={t('Request')}
                value={`${auditRoute.method} ${auditRoute.route}`}
                mono
              />
            )}
            {auditRoute?.status != null && (
              <DetailRow
                label={t('Result')}
                value={
                  auditRoute.success
                    ? `${t('Success')} (${auditRoute.status})`
                    : `${t('Failed')} (${auditRoute.status})`
                }
                mono
              />
            )}
          </DetailSection>
        )}

        {/* Login audit info (type=7) */}
        {isLogin && loginAuditFields.length > 0 && (
          <DetailSection
            icon={<LogIn className='size-3.5' aria-hidden='true' />}
            iconTone='info'
            label={t('Login Info')}
          >
            {operationText != null && (
              <DetailRow label={t('Operation')} value={operationText} />
            )}
            {loginAuditFields.map((field) => (
              <DetailRow
                key={field.label}
                label={field.label}
                value={field.value}
                mono
              />
            ))}
          </DetailSection>
        )}

        {/* Audio/WebSocket token breakdown */}
        {hasAudioTokens && other && (
          <DetailSection
            icon={<Settings2 className='size-3.5' aria-hidden='true' />}
            iconTone='chart-4'
            label={t('Audio Tokens')}
          >
            {other.audio_input != null && other.audio_input > 0 && (
              <DetailRow
                label={t('Audio Input')}
                value={formatTokens(other.audio_input)}
                mono
              />
            )}
            {other.audio_output != null && other.audio_output > 0 && (
              <DetailRow
                label={t('Audio Output')}
                value={formatTokens(other.audio_output)}
                mono
              />
            )}
            {other.text_input != null && other.text_input > 0 && (
              <DetailRow
                label={t('Text Input')}
                value={formatTokens(other.text_input)}
                mono
              />
            )}
            {other.text_output != null && other.text_output > 0 && (
              <DetailRow
                label={t('Text Output')}
                value={formatTokens(other.text_output)}
                mono
              />
            )}
          </DetailSection>
        )}

        {/* Reasoning effort */}
        {other?.reasoning_effort && (
          <DetailRow
            label={t('Reasoning Effort')}
            value={
              <StatusBadge
                label={other.reasoning_effort}
                variant={reasoningEffortVariant}
                size='sm'
                copyable={false}
              />
            }
          />
        )}

        {/* System prompt override */}
        {other?.is_system_prompt_overwritten && (
          <DetailRow
            label={t('System Prompt')}
            value={
              <StatusBadge
                label={t('Overwritten')}
                variant='orange'
                size='sm'
                copyable={false}
              />
            }
          />
        )}

        {/* Model mapping */}
        {other?.is_model_mapped && other?.upstream_model_name && (
          <DetailSection label={t('Model Mapping')}>
            <DetailRow
              label={t('Request Model')}
              value={props.log.model_name}
              mono
            />
            <DetailRow
              label={t('Actual Model')}
              value={other.upstream_model_name}
              mono
            />
          </DetailSection>
        )}

        {/* Token breakdown (for consume/error types with token data) */}
        {isDisplayableType && other && (
          <TokenBreakdown log={props.log} other={other} />
        )}

        {/* Stream status details */}
        {other?.stream_status && other.stream_status.status !== 'ok' && (
          <DetailSection label={t('Stream Status')}>
            <DetailRow
              label={t('Status')}
              value={
                <StatusBadge
                  label={other.stream_status.status || t('Error')}
                  variant='red'
                  size='sm'
                  copyable={false}
                />
              }
            />
            {other.stream_status.end_reason && (
              <DetailRow
                label={t('End Reason')}
                value={other.stream_status.end_reason}
              />
            )}
            {(other.stream_status.error_count ?? 0) > 0 && (
              <DetailRow
                label={t('Soft Errors')}
                value={String(other.stream_status.error_count)}
              />
            )}
            {other.stream_status.end_error && (
              <DetailRow
                label={t('End Error')}
                value={other.stream_status.end_error}
              />
            )}
            {Array.isArray(other.stream_status.errors) &&
              other.stream_status.errors.length > 0 && (
                <pre className='bg-background/60 mt-1 max-h-32 overflow-y-auto rounded border p-2 font-mono text-[11px] leading-relaxed wrap-break-word whitespace-pre-wrap'>
                  {other.stream_status.errors.join('\n')}
                </pre>
              )}
          </DetailSection>
        )}

        {/* Param override */}
        {other?.po && Array.isArray(other.po) && other.po.length > 0 && (
          <DetailSection
            icon={<Settings2 className='size-3.5' aria-hidden='true' />}
            iconTone='chart-3'
            label={`${t('Param Override')} (${other.po.length})`}
          >
            {other.po.filter(Boolean).map((line) => {
              const parsed = parseAuditLine(line)
              if (!parsed) return null
              return (
                <div
                  key={`${parsed.action}-${parsed.content}`}
                  className='bg-background/60 flex min-w-0 flex-col gap-1.5 rounded border p-2 sm:flex-row sm:items-start sm:gap-2'
                >
                  <StatusBadge
                    variant='neutral'
                    label={getParamOverrideActionLabel(parsed.action, t)}
                    className='shrink-0 font-medium'
                    copyable={false}
                  />
                  <span className='min-w-0 font-mono text-[11px] leading-relaxed break-all sm:wrap-break-word'>
                    {parsed.content}
                  </span>
                </div>
              )
            })}
          </DetailSection>
        )}

        {/* Content */}
        {details && (
          <div className='space-y-1.5'>
            <Label className='text-xs font-semibold'>{t('Content')}</Label>
            <div className='bg-muted/30 relative min-w-0 overflow-hidden rounded-md border p-2.5'>
              <Button
                variant='ghost'
                size='sm'
                className='absolute top-1.5 right-1.5 h-5 w-5 p-0'
                onClick={() => copyToClipboard(details)}
                title={t('Copy to clipboard')}
                aria-label={t('Copy to clipboard')}
              >
                {copiedText === details ? (
                  <Check className='size-3 text-green-600' />
                ) : (
                  <Copy className='size-3' />
                )}
              </Button>
              <p className='min-w-0 pr-6 text-xs leading-relaxed break-all whitespace-pre-wrap sm:wrap-break-word'>
                {details}
              </p>
            </div>
          </div>
        )}
      </div>
    </Dialog>
  )
}
