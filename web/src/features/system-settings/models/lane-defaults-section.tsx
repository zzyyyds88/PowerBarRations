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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { safeNumberFieldProps } from '../utils/numeric-field'

/** 默认六键（api-spec §4.2 / §5.1 的 lane_defaults）。 */
export interface LaneDefaults {
  member_max_attempts: number
  member_retry_interval_seconds: number
  member_non_stream_response_timeout_seconds: number
  member_stream_first_event_timeout_seconds: number
  member_cooldown_seconds: number
  member_affinity_seconds: number
}

export const LANE_DEFAULTS_QUERY_KEY = ['pbr-system-options'] as const

/** 读取默认六键（GET /api/v1/system/options）。 */
export async function fetchLaneDefaults(): Promise<LaneDefaults> {
  const res = await api.get<{ lane_defaults?: LaneDefaults }>(
    '/api/v1/system/options'
  )
  return (
    res.data.lane_defaults ?? {
      member_max_attempts: 2,
      member_retry_interval_seconds: 3,
      member_non_stream_response_timeout_seconds: 120,
      member_stream_first_event_timeout_seconds: 30,
      member_cooldown_seconds: 60,
      member_affinity_seconds: 0,
    }
  )
}

// 四个"必须为正"的预算/时长 + 两个允许为 0 的间隔（与后端 validateLaneDefaults 一致）。
const positiveInt = (message: string) => z.coerce.number().int().min(1, message)
const nonNegativeInt = (message: string) => z.coerce.number().int().min(0, message)

const createSchema = (
  t: (key: string, options?: Record<string, unknown>) => string
) =>
  z.object({
    member_max_attempts: positiveInt(t('Must be a positive integer')),
    member_retry_interval_seconds: nonNegativeInt(
      t('Must be a non-negative integer')
    ),
    member_non_stream_response_timeout_seconds: positiveInt(
      t('Must be a positive integer')
    ),
    member_stream_first_event_timeout_seconds: positiveInt(
      t('Must be a positive integer')
    ),
    member_cooldown_seconds: positiveInt(t('Must be a positive integer')),
    member_affinity_seconds: nonNegativeInt(
      t('Must be a non-negative integer')
    ),
  })

type LaneDefaultsFormValues = z.output<ReturnType<typeof createSchema>>
type LaneDefaultsFormInput = z.input<ReturnType<typeof createSchema>>

const FIELD_KEYS: Array<keyof LaneDefaults> = [
  'member_max_attempts',
  'member_retry_interval_seconds',
  'member_non_stream_response_timeout_seconds',
  'member_stream_first_event_timeout_seconds',
  'member_cooldown_seconds',
  'member_affinity_seconds',
]

const FIELD_LABELS: Record<keyof LaneDefaults, string> = {
  member_max_attempts: 'Member max attempts',
  member_retry_interval_seconds: 'Member retry interval (seconds)',
  member_non_stream_response_timeout_seconds:
    'Non-stream response timeout (seconds)',
  member_stream_first_event_timeout_seconds:
    'Stream first event timeout (seconds)',
  member_cooldown_seconds: 'Member cooldown (seconds)',
  member_affinity_seconds: 'Member affinity (seconds)',
}

export function LaneDefaultsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const schema = createSchema(t)

  const defaultsQuery = useQuery({
    queryKey: LANE_DEFAULTS_QUERY_KEY,
    queryFn: fetchLaneDefaults,
    staleTime: 30_000,
  })

  const form = useForm<
    LaneDefaultsFormInput,
    unknown,
    LaneDefaultsFormValues
  >({
    resolver: zodResolver(schema),
    defaultValues: defaultsQuery.data,
    values: defaultsQuery.data,
  })

  const saveMutation = useMutation({
    mutationFn: async (values: LaneDefaultsFormValues) => {
      const res = await api.put('/api/v1/system/options', {
        lane_defaults: values,
      })
      return res.data
    },
    onSuccess: async () => {
      toast.success(t('Saved'))
      await queryClient.invalidateQueries({
        queryKey: LANE_DEFAULTS_QUERY_KEY,
      })
    },
    onError: (error) => handleServerError(error, t('Failed to save')),
  })

  const onSubmit = form.handleSubmit((values) => saveMutation.mutate(values))

  return (
    <SettingsSection title={t('Lane Defaults')}>
      <Form {...form}>
        <SettingsForm onSubmit={onSubmit}>
          <SettingsPageFormActions
            isSaving={saveMutation.isPending}
            onSave={onSubmit}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Applies to newly created or seeded lanes, and to lanes that do not configure these keys themselves. Existing explicit lane config still wins.'
            )}
          </p>
          <div className='grid min-w-0 gap-6 lg:grid-cols-2 xl:grid-cols-3'>
            {FIELD_KEYS.map((key) => (
              <FormField
                control={form.control}
                key={key}
                name={key}
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t(FIELD_LABELS[key])}</FormLabel>
                    <FormControl>
                      <Input min='0' type='number' {...safeNumberFieldProps(field)} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'See the routing spec for the timeout arithmetic before lowering attempts.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
