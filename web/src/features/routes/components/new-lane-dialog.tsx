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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import {
  getPBRRoute,
  listPBRModels,
  pbrModelsQueryKey,
  savePBRFailover,
} from '../api'

/**
 * 「新建车道」：手动创建一条路由键（可自定义、无需任何渠道声明），
 * 并从渠道声明里勾选成员。保存即 PUT /api/v1/lanes/{name} 固化。
 */
export function NewLaneDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [picked, setPicked] = useState<string[]>([])

  const modelsQuery = useQuery({
    queryKey: pbrModelsQueryKey,
    queryFn: listPBRModels,
    enabled: props.open,
  })
  // 候选成员来自「渠道声明」的建议链：用同名路由键查一次，拿到候选渠道。
  const candidatesQuery = useQuery({
    queryKey: ['pbr-route', name, 'new-lane-candidates'],
    queryFn: () => getPBRRoute(name),
    enabled: props.open && name.trim().length > 0,
    retry: false,
  })
  const candidates =
    candidatesQuery.data?.candidates ?? candidatesQuery.data?.members ?? []

  const save = useMutation({
    mutationFn: async () => {
      const trimmed = name.trim()
      const members = picked.map((channel, index) => {
        const found = candidates.find((c) => c.channel === channel)
        return {
          channel,
          upstream_model: found?.upstream_model ?? '',
          priority: picked.length - index,
        }
      })
      await savePBRFailover(trimmed, members)
    },
    onSuccess: async () => {
      toast.success(t('Lane saved'))
      await queryClient.invalidateQueries({ queryKey: pbrModelsQueryKey })
      setName('')
      setPicked([])
      props.onOpenChange(false)
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const trimmed = name.trim()
  const canSave = trimmed.length > 0 && picked.length > 0 && !save.isPending
  const knownModels = new Set((modelsQuery.data ?? []).map((m) => m.model))

  // 提前判定，避免嵌套三元（AGENTS §3.2）。
  let memberPicker = null
  if (trimmed.length === 0) {
    memberPicker = (
      <p className='text-muted-foreground text-xs'>
        {t('Enter a route key first.')}
      </p>
    )
  } else if (candidatesQuery.isLoading) {
    memberPicker = (
      <p className='text-muted-foreground flex items-center gap-2 text-xs'>
        <Loader2 className='size-3.5 animate-spin' /> {t('Loading...')}
      </p>
    )
  } else if (candidates.length === 0) {
    memberPicker = (
      <p className='text-muted-foreground text-xs'>
        {t('No channel declares this key. Add the model to a channel first.')}
      </p>
    )
  } else {
    memberPicker = (
      <div className='space-y-1.5'>
        {candidates.map((candidate) => (
          <label
            key={candidate.channel}
            className='flex cursor-pointer items-center gap-2 text-sm'
          >
            <Checkbox
              checked={picked.includes(candidate.channel)}
              onCheckedChange={(checked) =>
                setPicked((prev) =>
                  checked
                    ? [...prev, candidate.channel]
                    : prev.filter((c) => c !== candidate.channel)
                )
              }
              aria-label={candidate.channel}
            />
            <span className='font-mono'>{candidate.channel}</span>
            <span className='text-muted-foreground text-xs'>
              {candidate.upstream_model}
            </span>
          </label>
        ))}
      </div>
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('New lane')}
      description={t(
        'Create a route key by hand, then pick the member channels. Saving solidifies the lane.'
      )}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={!canSave}
            onClick={() => save.mutate()}
          >
            {save.isPending && <Loader2 className='size-4 animate-spin' />}
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='space-y-1.5'>
          <Label htmlFor='new-lane-name'>{t('Route key')}</Label>
          <Input
            id='new-lane-name'
            value={name}
            placeholder={t('e.g. my-pooled-model')}
            onChange={(event) => {
              setName(event.target.value)
              setPicked([])
            }}
          />
          {trimmed.length > 0 && !knownModels.has(trimmed) && (
            <p className='text-muted-foreground text-xs'>
              {t(
                'No channel declares this key yet — you can still create the lane and add members from channels that declare it.'
              )}
            </p>
          )}
        </div>

        <div className='space-y-1.5'>
          <Label>{t('Member channels')}</Label>
          {memberPicker}
        </div>
      </div>
    </Dialog>
  )
}
