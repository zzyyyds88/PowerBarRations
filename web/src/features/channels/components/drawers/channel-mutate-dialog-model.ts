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
import { cn } from '@/lib/utils'

import { CHANNEL_TYPE_NEW_API } from '../../constants'
import type { ChannelFormValues } from '../../lib'
import type { ChannelConfigurationStatus } from '../../lib/channel-configuration'
import type { Channel } from '../../types'

export type ChannelMutateDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Channel | null
}

export type ModelMappingGuardrail = {
  invalidJson: boolean
  entries: Array<{ source: string; target: string }>
  missingSourceModels: string[]
  exposedTargetModels: string[]
}

// Helper functions
export const createEmptyModelMappingGuardrail = (): ModelMappingGuardrail => ({
  invalidJson: false,
  entries: [],
  missingSourceModels: [],
  exposedTargetModels: [],
})

export const formatModelNames = (models: string[]): string =>
  models.map((model) => `"${model}"`).join(', ')

export const MODEL_MAPPING_PREVIEW_FALLBACK: Array<{
  source: string
  target: string
}> = [{ source: 'client-model', target: 'upstream-model' }]

export const ADVANCED_CUSTOM_ROUTE_TYPE_PREVIEW_LIMIT = 3
export const UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT = 8
// Channel types whose upstream requires an explicit base URL before probing.
export const DISCOVERY_BASE_URL_REQUIRED_TYPES = new Set([
  3,
  8,
  36,
  45,
  CHANNEL_TYPE_NEW_API,
])
export const SENSITIVE_FORM_FIELDS = [
  'type',
  'base_url',
  'key',
  'other',
  'key_mode',
  'param_override',
  'header_override',
  'settings',
  'setting',
  'advanced_custom',
  'is_enterprise_account',
  'vertex_key_type',
  'aws_key_type',
  'azure_responses_version',
  'force_format',
  'thinking_to_content',
  'proxy',
  'http_protocol',
  'http2_connection_shards',
  'pass_through_body_enabled',
  'system_prompt',
  'system_prompt_override',
  'allow_service_tier',
  'disable_store',
  'allow_safety_identifier',
  'allow_include_obfuscation',
  'allow_inference_geo',
  'allow_speed',
  'claude_beta_query',
  'upstream_model_update_check_enabled',
  'upstream_model_update_auto_sync_enabled',
  'upstream_model_update_ignored_models',
] satisfies (keyof ChannelFormValues)[]

export function parseSettingsRecord(
  settings: string | undefined
): Record<string, unknown> {
  if (!settings?.trim()) return {}
  try {
    const parsed = JSON.parse(settings)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
  } catch {
    return {}
  }
  return {}
}

export function formatUnixTime(timestamp: unknown): string {
  const seconds = Number(timestamp)
  if (!Number.isFinite(seconds) || seconds <= 0) return '-'
  return new Date(seconds * 1000).toLocaleString()
}

export function channelConfigurationBlockClassName(
  status: ChannelConfigurationStatus,
  className?: string
) {
  return cn(
    'border-border/60 rounded-lg border p-4 transition-colors',
    status === 'configured' && 'border-primary/35 ring-primary/20 ring-1',
    status === 'error' && 'border-destructive/50 ring-destructive/20 ring-1',
    className
  )
}
