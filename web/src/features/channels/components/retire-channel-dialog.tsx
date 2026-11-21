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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Link2, Loader2, ShieldAlert } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { fetchRetireChannelPlan, retireChannel } from '../api'
import { invalidateChannelCaches } from '../lib/channel-actions'

function NameList(props: { title: string; names: string[] }) {
  if (props.names.length === 0) return null
  return (
    <div className='min-w-0'>
      <p className='text-muted-foreground text-[11px] font-medium'>
        {props.title}
      </p>
      <ul className='mt-0.5 flex flex-wrap gap-1'>
        {props.names.map((name) => (
          <li
            key={name}
            className='bg-muted/40 max-w-full truncate rounded px-1.5 py-0.5 font-mono text-[11px]'
            title={name}
          >
            {name}
          </li>
        ))}
      </ul>
    </div>
  )
}

/**
 * 渠道退役确认弹窗（design-v1 §7.9、ui-spec §6.5）。
 *
 * 两步：①打开即调 dry_run 拿逐项预览——引用清单、将被清理/删除的车道、
 * 受影响的成员组（共享链的连带影响在这里明示）、历史改写条数、未过期异步对象；
 * ②输入渠道全名解锁红色确认（GitHub 式二次确认）。确认后调 retire 并逐项 toast。
 */
export function RetireChannelDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelName: string
  onSuccess?: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [confirmName, setConfirmName] = useState('')
  const [retiring, setRetiring] = useState(false)

  const planQuery = useQuery({
    queryKey: ['channel-retire-plan', props.channelName],
    queryFn: () => fetchRetireChannelPlan(props.channelName),
    enabled: props.open && props.channelName !== '',
    staleTime: 0,
    meta: { errorToast: false },
  })
  const plan = planQuery.data

  const canConfirm =
    !!plan && confirmName.trim() === props.channelName && !retiring

  const handleRetire = async () => {
    if (!canConfirm) return
    setRetiring(true)
    try {
      const result = await retireChannel(props.channelName)
      toast.success(
        t(
          'Channel retired: {{cleaned}} lanes cleaned, {{deleted}} lanes removed, {{logs}} history entries renamed.',
          {
            cleaned: result.lanes_cleaned.length,
            deleted: result.lanes_deleted.length,
            logs: result.logs_renamed,
          }
        )
      )
      invalidateChannelCaches(queryClient)
      props.onSuccess?.()
      props.onOpenChange(false)
      setConfirmName('')
    } catch (error) {
      toast.error(
        getServerErrorMessage(error) || t('Failed to retire the channel')
      )
    } finally {
      setRetiring(false)
    }
  }

  return (
    <AlertDialog
      open={props.open}
      onOpenChange={(nextOpen) => {
        props.onOpenChange(nextOpen)
        if (!nextOpen) setConfirmName('')
      }}
    >
      <AlertDialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-lg'>
        <AlertDialogHeader className='text-start'>
          <AlertDialogTitle>
            {t('Retire channel "{{name}}"', { name: props.channelName })}
          </AlertDialogTitle>
          <AlertDialogDescription render={<div />}>
            {t(
              'Retiring stops the channel, removes it from lanes and member groups, deletes it, and marks its history as retired. This cannot be undone.'
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>

        {planQuery.isPending && (
          <p className='text-muted-foreground flex items-center gap-2 text-sm'>
            <Loader2 className='size-4 animate-spin' aria-hidden='true' />
            {t('Loading the retirement plan…')}
          </p>
        )}

        {planQuery.isError && (
          <Alert variant='destructive'>
            <AlertDescription className='flex flex-wrap items-center justify-between gap-2'>
              <span>
                {getServerErrorMessage(planQuery.error) ||
                  t('Could not load the retirement plan.')}
              </span>
              <button
                type='button'
                className='underline underline-offset-2'
                onClick={() => void planQuery.refetch()}
              >
                {t('Retry')}
              </button>
            </AlertDescription>
          </Alert>
        )}

        {plan && (
          <div className='border-border/60 space-y-2.5 rounded-md border p-3 text-xs'>
            <div className='flex items-center gap-1.5'>
              <ShieldAlert
                className='text-warning size-3.5 shrink-0'
                aria-hidden='true'
              />
              <span className='font-medium'>
                {t(
                  'The channel is disabled first, then its references are removed.'
                )}
              </span>
            </div>
            <NameList
              title={t('Lanes cleaned (kept, members removed)')}
              names={plan.lanes_cleaned}
            />
            <NameList
              title={t('Lanes deleted (no members left)')}
              names={plan.lanes_deleted}
            />
            {plan.groups_affected.length > 0 && (
              <div className='min-w-0'>
                <NameList
                  title={t('Member groups to update (groups are kept)')}
                  names={plan.groups_affected}
                />
                <p className='text-muted-foreground mt-1 flex items-start gap-1'>
                  <Link2
                    className='mt-0.5 size-3 shrink-0'
                    aria-hidden='true'
                  />
                  {t(
                    'Removing a channel from a member group affects every lane referencing that group.'
                  )}
                </p>
              </div>
            )}
            {plan.groups_emptied.length > 0 && (
              <NameList
                title={t('Member groups left empty (kept)')}
                names={plan.groups_emptied}
              />
            )}
            <p className='text-muted-foreground'>
              {t(
                '{{logs}} request log entries and {{stats}} stats rows will be renamed to the retired form.',
                { logs: plan.logs_rename, stats: plan.stats_rows_rename }
              )}
            </p>
            {plan.pending_async_objects > 0 && (
              <div className='border-warning/30 bg-warning/10 flex items-start gap-1.5 rounded-md border px-2.5 py-2'>
                <AlertTriangle
                  className='text-warning mt-0.5 size-3.5 shrink-0'
                  aria-hidden='true'
                />
                <span>
                  {t(
                    '{{count}} unexpired async objects (Files/Batches) are pinned to this channel and will wait for expiry cleanup.',
                    { count: plan.pending_async_objects }
                  )}
                </span>
              </div>
            )}
          </div>
        )}

        <div className='space-y-1.5'>
          <Input
            value={confirmName}
            onChange={(event) => setConfirmName(event.target.value)}
            placeholder={props.channelName}
            aria-label={t('Type the channel name to confirm')}
            autoComplete='off'
            spellCheck={false}
          />
          <p className='text-muted-foreground text-[11px]'>
            {t('Type "{{name}}" to enable retirement.', {
              name: props.channelName,
            })}
          </p>
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={retiring}>
            {t('Cancel')}
          </AlertDialogCancel>
          <Button
            variant='destructive'
            onClick={handleRetire}
            disabled={!canConfirm}
          >
            {retiring && (
              <Loader2 className='size-4 animate-spin' aria-hidden='true' />
            )}
            {t('Retire and delete')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
