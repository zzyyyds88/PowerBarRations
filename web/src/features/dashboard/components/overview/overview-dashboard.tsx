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
import { Link } from '@tanstack/react-router'
import {
  Copy,
  RadioTower,
  ShieldCheck,
  TerminalSquare,
  Timer,
  type LucideIcon,
} from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import { Button } from '@/components/ui/button'
import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { fetchTokenKey, getApiKeys } from '@/features/keys/api'
import type { ApiKey } from '@/features/keys/types'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { getUserModels } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { MOTION_TRANSITION } from '@/lib/motion'
import { ROLE } from '@/lib/roles'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  useApiInfo,
  useDashboardContentVisibility,
} from '../../hooks/use-status-data'
import { ApiInfoPanel } from './api-info-panel'
import { PerformanceHealthPanel } from './performance-health-panel'
import { SummaryCards } from './summary-cards'

interface RequestExample {
  endpoint: string
  model: string
  keyName: string
  keyId?: number
  displayKey: string
  ready: boolean
}

interface HeroSignal {
  label: string
  value: string
  icon: LucideIcon
  tone: IconBadgeTone
  /** Present when the signal's backing request failed — lets the user retry. */
  onRetry?: () => void
}

function getCurrentOrigin(): string {
  if (typeof window === 'undefined') return ''
  return window.location.origin
}

function normalizeEndpoint(sourceUrl?: string): string {
  const fallback = `${getCurrentOrigin()}/v1/chat/completions`
  const trimmed = sourceUrl?.trim()
  if (!trimmed) return fallback

  const withoutTrailingSlash = trimmed.replace(/\/+$/, '')
  if (withoutTrailingSlash.endsWith('/v1/chat/completions')) {
    return withoutTrailingSlash
  }
  if (withoutTrailingSlash.endsWith('/v1')) {
    return `${withoutTrailingSlash}/chat/completions`
  }
  return `${withoutTrailingSlash}/v1/chat/completions`
}

function getPreferredKey(keys: ApiKey[]): ApiKey | null {
  return keys.find((item) => item.status === 1) ?? keys[0] ?? null
}

function formatDisplayKey(key?: string): string {
  if (!key) return 'sk-...'
  if (key.length <= 14) return key
  return `${key.slice(0, 7)}...${key.slice(-4)}`
}

function buildCurlCommand(args: {
  endpoint: string
  apiKey: string
  model: string
}): string {
  return [
    `curl ${args.endpoint} \\`,
    '  -H "Content-Type: application/json" \\',
    `  -H "Authorization: Bearer ${args.apiKey}" \\`,
    `  -d '{"model":"${args.model}","messages":[{"role":"user","content":"Say hello in one sentence."}]}'`,
  ].join('\n')
}

function RequestPreview(props: {
  example: RequestExample
  signals: HeroSignal[]
}) {
  const { t } = useTranslation()
  const shouldReduceMotion = useReducedMotion()
  const [isCopying, setIsCopying] = useState(false)
  const { copyToClipboard } = useCopyToClipboard({ notify: false })
  const previewCurl = buildCurlCommand({
    endpoint: props.example.endpoint,
    apiKey: props.example.displayKey,
    model: props.example.model,
  })
  const previewLines = previewCurl.split('\n')
  const handleCopyRequest = async () => {
    if (!props.example.keyId || isCopying) return

    setIsCopying(true)
    try {
      const result = await fetchTokenKey(props.example.keyId)
      const key = result.success && result.data?.key ? result.data.key : ''
      if (!key) {
        handleServerError(result, t('Failed to copy to clipboard'))
        return
      }

      const realCurl = buildCurlCommand({
        endpoint: props.example.endpoint,
        apiKey: `sk-${key}`,
        model: props.example.model,
      })
      const copied = await copyToClipboard(realCurl)
      if (copied) {
        toast.success(t('Copied to clipboard'))
      } else {
        toast.error(t('Failed to copy to clipboard'))
      }
    } catch (error) {
      handleServerError(error, t('Failed to copy to clipboard'))
    } finally {
      setIsCopying(false)
    }
  }

  return (
    <motion.div
      initial={shouldReduceMotion ? false : { opacity: 0, y: 10, scale: 0.98 }}
      animate={shouldReduceMotion ? undefined : { opacity: 1, y: 0, scale: 1 }}
      transition={MOTION_TRANSITION.slow}
      className='bg-background/75 relative overflow-hidden rounded-2xl border p-3 shadow-sm backdrop-blur'
    >
      {!shouldReduceMotion && (
        <motion.div
          className='via-foreground/30 pointer-events-none absolute inset-x-0 top-0 h-px bg-linear-to-r from-transparent to-transparent'
          animate={{ x: ['-100%', '100%'] }}
          transition={{ duration: 3.2, repeat: Infinity, ease: 'easeInOut' }}
          aria-hidden='true'
        />
      )}

      <div className='flex items-center justify-between gap-3 border-b pb-3'>
        <div className='flex min-w-0 items-center gap-2'>
          <IconBadge tone='info'>
            <TerminalSquare />
          </IconBadge>
          <div className='min-w-0'>
            <div className='truncate text-sm font-medium'>
              {t('First API request')}
            </div>
            <div className='text-muted-foreground truncate text-xs'>
              {props.example.ready
                ? props.example.keyName
                : t('Create an API key to unlock the real request')}
            </div>
          </div>
        </div>
        {props.example.ready ? (
          <Button
            variant='outline'
            size='sm'
            className='h-7 gap-1.5 px-2 text-xs'
            disabled={isCopying}
            onClick={handleCopyRequest}
            aria-label={t('Copy ready-to-run curl')}
          >
            <Copy data-icon='inline-start' />
            {isCopying ? t('Loading') : t('Copy')}
          </Button>
        ) : (
          <Button size='sm' variant='outline' render={<Link to='/keys' />}>
            {t('Create API Key')}
          </Button>
        )}
      </div>

      <div className='bg-foreground/[0.035] my-3 rounded-xl p-3 font-mono text-xs'>
        <div className='mb-2 flex items-center gap-1.5'>
          <span className='bg-destructive size-2 rounded-full' />
          <span className='bg-warning size-2 rounded-full' />
          <span className='bg-success size-2 rounded-full' />
        </div>
        <div className='flex flex-col gap-1 overflow-hidden'>
          {previewLines.map((line) => (
            <code
              key={line}
              className='text-muted-foreground truncate'
              title={line}
            >
              {line}
            </code>
          ))}
        </div>
      </div>

      <div className='grid gap-2'>
        {props.signals.map((signal) => {
          const Icon = signal.icon

          return (
            <div
              key={signal.label}
              className='bg-muted/40 flex items-center justify-between gap-3 rounded-xl px-3 py-2'
            >
              <span className='flex min-w-0 items-center gap-2'>
                <IconBadge tone={signal.tone} size='xs'>
                  <Icon />
                </IconBadge>
                <span className='truncate text-xs font-medium'>
                  {signal.label}
                </span>
              </span>
              <span className='flex shrink-0 items-center gap-2'>
                <span className='text-muted-foreground text-xs'>
                  {signal.value}
                </span>
                {signal.onRetry && (
                  <Button
                    variant='outline'
                    size='sm'
                    className='h-6 px-2 text-xs'
                    onClick={signal.onRetry}
                  >
                    {t('Retry')}
                  </Button>
                )}
              </span>
            </div>
          )
        })}
      </div>
    </motion.div>
  )
}

export function OverviewDashboard() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const { items: apiInfoItems } = useApiInfo()
  const { apiInfo: showApiInfoPanel } = useDashboardContentVisibility()

  const isAdmin = Boolean(user?.role && user.role >= ROLE.ADMIN)

  const apiKeysQuery = useQuery({
    queryKey: ['dashboard', 'overview', 'api-keys'],
    queryFn: async () => {
      const result = requireServerSuccess(await getApiKeys({ p: 1, size: 10 }))
      return result.success ? (result.data?.items ?? []) : []
    },
    staleTime: 60 * 1000,
  })

  const modelsQuery = useQuery({
    queryKey: ['dashboard', 'overview', 'user-models'],
    queryFn: async () => {
      const result = requireServerSuccess(await getUserModels())
      return result.success ? (result.data ?? []) : []
    },
    staleTime: 5 * 60 * 1000,
  })

  const { isError: apiKeysError, refetch: refetchApiKeys } = apiKeysQuery
  const { isError: modelsError, refetch: refetchModels } = modelsQuery

  const preferredKey = useMemo(
    () => getPreferredKey(apiKeysQuery.data ?? []),
    [apiKeysQuery.data]
  )

  // A failed request must not degrade into "Needs API key" or a stuck "Loading".
  const authSignalValue = preferredKey ? t('Secured') : t('Needs API key')
  const modelSignalValue = modelsQuery.data?.[0] ?? t('Loading')

  const heroSignals = useMemo<HeroSignal[]>(
    () => [
      {
        label: t('Route active'),
        value: apiInfoItems.length > 0 ? t('Online') : t('Current domain'),
        icon: RadioTower,
        tone: 'info',
      },
      {
        label: t('Auth configured'),
        value: apiKeysError ? t('Failed to load') : authSignalValue,
        icon: ShieldCheck,
        tone: 'success',
        onRetry: apiKeysError ? () => void refetchApiKeys() : undefined,
      },
      {
        label: t('Model selected'),
        value: modelsError ? t('Failed to load') : modelSignalValue,
        icon: Timer,
        tone: 'chart-4',
        onRetry: modelsError ? () => void refetchModels() : undefined,
      },
    ],
    [
      apiInfoItems.length,
      apiKeysError,
      refetchApiKeys,
      authSignalValue,
      modelSignalValue,
      modelsError,
      refetchModels,
      t,
    ]
  )

  const requestExample = useMemo<RequestExample>(() => {
    const endpoint = normalizeEndpoint(apiInfoItems[0]?.url)
    const model = modelsQuery.data?.[0] ?? 'gpt-4o-mini'
    const keyName = preferredKey?.name ?? t('No API key yet')
    const ready = Boolean(preferredKey?.id && model)

    return {
      endpoint,
      model,
      keyName,
      keyId: preferredKey?.id,
      displayKey: preferredKey
        ? formatDisplayKey(`sk-${preferredKey.key}`)
        : 'sk-...',
      ready,
    }
  }, [apiInfoItems, modelsQuery.data, preferredKey, t])

  const showLeftContentPanels = isAdmin || showApiInfoPanel

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Overview')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <SummaryCards />

          <CardStaggerContainer
            className={cn(
              'grid grid-cols-1 gap-4',
              showLeftContentPanels && 'xl:grid-cols-[minmax(0,1fr)_22rem]'
            )}
          >
            {showLeftContentPanels && (
              <div
                className={cn(
                  'grid min-w-0 grid-cols-1 gap-4',
                  isAdmin && showApiInfoPanel && 'lg:grid-cols-2'
                )}
              >
                {isAdmin && (
                  <CardStaggerItem
                    className={showApiInfoPanel ? 'lg:col-span-2' : undefined}
                  >
                    <PerformanceHealthPanel />
                  </CardStaggerItem>
                )}
                {showApiInfoPanel && (
                  <CardStaggerItem>
                    <ApiInfoPanel />
                  </CardStaggerItem>
                )}
              </div>
            )}
            <div className='grid min-w-0 grid-cols-1 content-start gap-4'>
              <CardStaggerItem>
                <RequestPreview
                  example={requestExample}
                  signals={heroSignals}
                />
              </CardStaggerItem>
            </div>
          </CardStaggerContainer>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
