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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'

import { SettingsSection } from '../components/settings-section'

/** 与后端 system/options 的熔断字段一一对应（api-spec §6.3）。 */
interface CircuitOptions {
  circuit_failure_threshold: number
  circuit_open_seconds: number
  circuit_max_open_seconds: number
  circuit_rolling_min_samples: number
  circuit_rolling_failure_rate: number
  circuit_half_open_successes: number
  circuit_allowed_fails: Record<string, number>
}

type NumericCircuitKey = Exclude<keyof CircuitOptions, 'circuit_allowed_fails'>

interface FieldSpec {
  key: NumericCircuitKey
  label: string
  description: string
  min: number
  step: number
}

const FIELDS: FieldSpec[] = [
  {
    key: 'circuit_failure_threshold',
    label: 'Failure threshold',
    description:
      'Circuit opens when the accumulated failure score reaches this value. Hard failures score 1.0, 5xx 0.6, 429 0.2.',
    min: 0.1,
    step: 0.1,
  },
  {
    key: 'circuit_open_seconds',
    label: 'Circuit open seconds (0 = member cooldown)',
    description:
      'How long an opened circuit rejects the member before a half-open probe.',
    min: 0,
    step: 1,
  },
  {
    key: 'circuit_max_open_seconds',
    label: 'Max circuit open seconds',
    description: 'Upper bound for the exponential reopen backoff.',
    min: 1,
    step: 1,
  },
  {
    key: 'circuit_rolling_min_samples',
    label: 'Rolling window min samples',
    description:
      'Minimum samples before the rolling failure rate can open the circuit.',
    min: 1,
    step: 1,
  },
  {
    key: 'circuit_rolling_failure_rate',
    label: 'Rolling failure rate (0-1]',
    description:
      'Rolling-window failure rate that opens the circuit (combined with the score threshold, whichever comes first).',
    min: 0.01,
    step: 0.01,
  },
  {
    key: 'circuit_half_open_successes',
    label: 'Half-open successes required',
    description:
      'Consecutive successful probes required before the circuit closes again (default 2).',
    min: 1,
    step: 1,
  },
]

const toDraft = (options: CircuitOptions): Record<string, string> =>
  Object.fromEntries(
    FIELDS.map((field) => [field.key, String(options[field.key])])
  )

/**
 * 按错误类型的"连续失败几次即开断"。与累计失败分、滚动失败率取或，
 * 未列出的类型不单独触发；429 允许更多次，避免瞬时限流直接熔断。
 */
const ALLOWED_FAIL_FIELDS = [
  {
    key: 'hard_auth',
    label: 'Hard auth allowed fails',
    description:
      'Ignored credentials: consecutive failures before the circuit opens.',
  },
  {
    key: 'hard_quota',
    label: 'Hard quota allowed fails',
    description:
      'Exhausted quota or billing: consecutive failures before the circuit opens.',
  },
  {
    key: 'soft_transient',
    label: 'Transient allowed fails',
    description:
      '5xx and connection errors: consecutive failures before the circuit opens.',
  },
  {
    key: 'soft_rate_limit',
    label: 'Rate-limit allowed fails',
    description:
      '429 responses: keep this higher so short bursts do not open the circuit.',
  },
  {
    key: 'bad_response',
    label: 'Bad response allowed fails',
    description:
      'Unparseable upstream bodies: consecutive failures before the circuit opens.',
  },
] as const

const DEFAULT_ALLOWED_FAILS: Record<string, number> = {
  hard_auth: 2,
  hard_quota: 2,
  soft_transient: 4,
  soft_rate_limit: 10,
  bad_response: 3,
}

const toAllowedDraft = (
  options: CircuitOptions | undefined
): Record<string, string> => {
  const source = options?.circuit_allowed_fails ?? DEFAULT_ALLOWED_FAILS
  return Object.fromEntries(
    ALLOWED_FAIL_FIELDS.map((field) => [
      field.key,
      String(source[field.key] ?? DEFAULT_ALLOWED_FAILS[field.key]),
    ])
  )
}

export function ResilienceSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Record<string, string>>({})
  const [allowedDraft, setAllowedDraft] = useState<Record<string, string>>({})

  const optionsQuery = useQuery({
    queryKey: ['pbr-circuit-options'],
    queryFn: async () => {
      const res = await api.get<CircuitOptions>('/api/v1/system/options')
      return res.data
    },
  })

  // 选项到达后回填草稿；用渲染期调整代替 setState-in-effect。
  const [prevOptions, setPrevOptions] = useState(optionsQuery.data)
  if (optionsQuery.data !== prevOptions) {
    setPrevOptions(optionsQuery.data)
    if (optionsQuery.data) {
      setDraft(toDraft(optionsQuery.data))
      setAllowedDraft(toAllowedDraft(optionsQuery.data))
    }
  }

  const save = useMutation({
    mutationFn: async () => {
      const payload: Partial<CircuitOptions> = {}
      for (const field of FIELDS) {
        const value = Number(draft[field.key])
        if (!Number.isFinite(value) || value < field.min) {
          throw new Error(
            t('{{field}} must be at least {{min}}', {
              field: t(field.label),
              min: field.min,
            })
          )
        }
        payload[field.key] = value
      }
      const allowedFails: Record<string, number> = {}
      for (const field of ALLOWED_FAIL_FIELDS) {
        const value = Number(allowedDraft[field.key])
        if (!Number.isFinite(value) || value < 1) {
          throw new Error(
            t('{{field}} must be at least {{min}}', {
              field: t(field.label),
              min: 1,
            })
          )
        }
        allowedFails[field.key] = value
      }
      payload.circuit_allowed_fails = allowedFails
      await api.put('/api/v1/system/options', payload)
    },
    onSuccess: async () => {
      toast.success(t('Resilience settings saved'))
      await queryClient.invalidateQueries({ queryKey: ['pbr-circuit-options'] })
    },
    onError: (error: unknown) => handleServerError(error),
  })

  return (
    <SettingsSection title={t('Resilience & Circuit Breaker')}>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Circuit score is recorded once per failed request, not per upstream retry. A member re-opening within the notification window is suppressed by the webhook hysteresis.'
        )}
      </p>
      <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'>
        {FIELDS.map((field) => (
          <label key={field.key} className='flex flex-col gap-1.5'>
            <span className='text-sm font-medium'>{t(field.label)}</span>
            <Input
              type='number'
              min={field.min}
              step={field.step}
              value={draft[field.key] ?? ''}
              onChange={(event) =>
                setDraft((prev) => ({
                  ...prev,
                  [field.key]: event.target.value,
                }))
              }
            />
            <span className='text-muted-foreground text-xs'>
              {t(field.description)}
            </span>
          </label>
        ))}
      </div>
      <div className='border-border border-t pt-4'>
        <div className='mb-3'>
          <h4 className='text-sm font-medium'>
            {t('Open circuit by consecutive failures')}
          </h4>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Each error kind can open the circuit on its own. These thresholds are OR-ed with the failure score and the rolling failure rate.'
            )}
          </p>
        </div>
        <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'>
          {ALLOWED_FAIL_FIELDS.map((field) => (
            <label key={field.key} className='flex flex-col gap-1.5'>
              <span className='text-sm font-medium'>{t(field.label)}</span>
              <Input
                type='number'
                min={1}
                step={1}
                value={allowedDraft[field.key] ?? ''}
                onChange={(event) =>
                  setAllowedDraft((prev) => ({
                    ...prev,
                    [field.key]: event.target.value,
                  }))
                }
              />
              <span className='text-muted-foreground text-xs'>
                {t(field.description)}
              </span>
            </label>
          ))}
        </div>
      </div>
      <div className='flex justify-end'>
        <Button
          type='button'
          disabled={save.isPending || optionsQuery.isLoading}
          onClick={() => save.mutate()}
        >
          {t('Save resilience settings')}
        </Button>
      </div>
    </SettingsSection>
  )
}
