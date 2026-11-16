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
  CONDITION_MODE_VALUES,
  FROM_REQUIRED_MODES,
  MODE_META,
  OPERATION_MODE_LABEL_MAP,
  OPERATION_MODE_VALUES,
  TO_REQUIRED_MODES,
  VALUE_REQUIRED_MODES,
  type ParamOverrideCondition,
  type ParamOverrideOperation,
} from './param-override-editor-constants'

let localIdSeed = 0
export const nextLocalId = () => `po_${Date.now()}_${localIdSeed++}`

const toValueText = (value: unknown): string => {
  if (value === undefined) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

export const parseLooseValue = (valueText: string): unknown => {
  const raw = String(valueText ?? '').trim()
  if (raw === '') return ''
  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

export const verifyJSON = (text: string): boolean => {
  try {
    JSON.parse(text)
    return true
  } catch {
    return false
  }
}

export const normalizeCondition = (
  condition: Record<string, unknown> = {}
): ParamOverrideCondition => ({
  id: nextLocalId(),
  path: typeof condition.path === 'string' ? condition.path : '',
  mode: CONDITION_MODE_VALUES.has(condition.mode as string)
    ? (condition.mode as string)
    : 'full',
  value_text: toValueText(condition.value),
  invert: condition.invert === true,
  pass_missing_key: condition.pass_missing_key === true,
})

export const createDefaultCondition = (): ParamOverrideCondition =>
  normalizeCondition({})

export const normalizeOperation = (
  operation: Record<string, unknown> = {}
): ParamOverrideOperation => ({
  id: nextLocalId(),
  description:
    typeof operation.description === 'string' ? operation.description : '',
  path: typeof operation.path === 'string' ? operation.path : '',
  mode: OPERATION_MODE_VALUES.has(operation.mode as string)
    ? (operation.mode as string)
    : 'set',
  value_text: toValueText(operation.value),
  keep_origin: operation.keep_origin === true,
  from: typeof operation.from === 'string' ? operation.from : '',
  to: typeof operation.to === 'string' ? operation.to : '',
  logic: String(operation.logic || 'OR').toUpperCase() === 'AND' ? 'AND' : 'OR',
  conditions: Array.isArray(operation.conditions)
    ? (operation.conditions as Record<string, unknown>[]).map(
        normalizeCondition
      )
    : [],
})

export const createDefaultOperation = (): ParamOverrideOperation =>
  normalizeOperation({ mode: 'set' })

export const reorderOperations = (
  ops: ParamOverrideOperation[],
  sourceId: string,
  targetId: string,
  position: 'before' | 'after' = 'before'
): ParamOverrideOperation[] => {
  if (!sourceId || !targetId || sourceId === targetId) return ops
  const srcIdx = ops.findIndex((o) => o.id === sourceId)
  if (srcIdx < 0) return ops
  const next = [...ops]
  const [moved] = next.splice(srcIdx, 1)
  let insertIdx = next.findIndex((o) => o.id === targetId)
  if (insertIdx < 0) return ops
  if (position === 'after') insertIdx += 1
  next.splice(insertIdx, 0, moved)
  return next
}

export const isOperationBlank = (
  operation: ParamOverrideOperation
): boolean => {
  const hasCondition = operation.conditions.some(
    (c) =>
      c.path.trim() ||
      c.value_text.trim() ||
      c.mode !== 'full' ||
      c.invert ||
      c.pass_missing_key
  )
  return (
    operation.mode === 'set' &&
    !operation.path.trim() &&
    !operation.from.trim() &&
    !operation.to.trim() &&
    operation.value_text.trim() === '' &&
    !operation.keep_origin &&
    !hasCondition
  )
}

export const getOperationSummary = (
  operation: ParamOverrideOperation,
  index: number
): string => {
  const mode = operation.mode || 'set'
  const modeLabel = OPERATION_MODE_LABEL_MAP[mode] || mode
  if (mode === 'sync_fields') {
    const from = operation.from.trim()
    const to = operation.to.trim()
    return `${index + 1}. ${modeLabel} · ${from || to || '-'}`
  }
  const path = operation.path.trim()
  const from = operation.from.trim()
  const to = operation.to.trim()
  return `${index + 1}. ${modeLabel} · ${path || from || to || '-'}`
}

export const getModeTagTailwind = (mode: string): string => {
  if (mode.includes('header')) {
    return 'bg-cyan-500/15 text-cyan-700 dark:text-cyan-300 border-cyan-500/20'
  }
  if (mode.includes('replace') || mode.includes('trim')) {
    return 'bg-violet-500/15 text-violet-700 dark:text-violet-300 border-violet-500/20'
  }
  if (mode.includes('copy') || mode.includes('move')) {
    return 'bg-blue-500/15 text-blue-700 dark:text-blue-300 border-blue-500/20'
  }
  if (mode.includes('error') || mode.includes('prune')) {
    return 'bg-red-500/15 text-red-700 dark:text-red-300 border-red-500/20'
  }
  if (mode.includes('sync')) {
    return 'bg-green-500/15 text-green-700 dark:text-green-300 border-green-500/20'
  }
  return 'bg-muted text-muted-foreground'
}

export const getModePathLabel = (mode: string): string => {
  if (mode === 'set_header' || mode === 'delete_header') return 'Header Name'
  if (mode === 'prune_objects') return 'Target Path (optional)'
  return 'Target Field Path'
}

export const getModePathPlaceholder = (mode: string): string => {
  if (mode === 'set_header') return 'Authorization'
  if (mode === 'delete_header') return 'X-Debug-Mode'
  if (mode === 'prune_objects') return 'messages'
  return 'temperature'
}

export const getModeFromLabel = (mode: string): string => {
  if (mode === 'replace') return 'Match Text'
  if (mode === 'regex_replace') return 'Regex Pattern'
  if (mode === 'copy_header' || mode === 'move_header') return 'Source Header'
  return 'Source Field'
}

export const getModeFromPlaceholder = (mode: string): string => {
  if (mode === 'replace') return 'openai/'
  if (mode === 'regex_replace') return '^gpt-'
  if (mode === 'copy_header' || mode === 'move_header') return 'Authorization'
  return 'model'
}

export const getModeToLabel = (mode: string): string => {
  if (mode === 'replace' || mode === 'regex_replace') return 'Replace With'
  if (mode === 'copy_header' || mode === 'move_header') return 'Target Header'
  return 'Target Field'
}

export const getModeToPlaceholder = (mode: string): string => {
  if (mode === 'replace') return '(leave empty to delete)'
  if (mode === 'regex_replace') return 'openai/gpt-'
  if (mode === 'copy_header' || mode === 'move_header') return 'X-Upstream-Auth'
  return 'original_model'
}

export const getModeValueLabel = (mode: string): string => {
  if (mode === 'set_header') {
    return 'Header Value (supports string or JSON mapping)'
  }
  if (mode === 'pass_headers') {
    return 'Pass-through Headers (comma-separated or JSON array)'
  }
  if (
    mode === 'trim_prefix' ||
    mode === 'trim_suffix' ||
    mode === 'ensure_prefix' ||
    mode === 'ensure_suffix'
  ) {
    return 'Prefix/Suffix Text'
  }
  if (mode === 'prune_objects') return 'Prune Rule (string or JSON object)'
  return 'Value (supports JSON or plain text)'
}

export const getModeValuePlaceholder = (mode: string): string => {
  if (mode === 'set_header') return 'Bearer sk-xxx'
  if (mode === 'pass_headers') return 'Authorization, X-Request-Id'
  if (
    mode === 'trim_prefix' ||
    mode === 'trim_suffix' ||
    mode === 'ensure_prefix' ||
    mode === 'ensure_suffix'
  ) {
    return 'openai/'
  }
  if (mode === 'prune_objects') return '{"type":"redacted_thinking"}'
  return '0.7'
}

export const parseSyncTargetSpec = (
  spec: string
): { type: string; key: string } => {
  const raw = String(spec ?? '').trim()
  if (!raw) return { type: 'json', key: '' }
  const idx = raw.indexOf(':')
  if (idx < 0) return { type: 'json', key: raw }
  const prefix = raw.slice(0, idx).trim().toLowerCase()
  const key = raw.slice(idx + 1).trim()
  return prefix === 'header' ? { type: 'header', key } : { type: 'json', key }
}

export const buildSyncTargetSpec = (type: string, key: string): string => {
  const normalizedType = type === 'header' ? 'header' : 'json'
  const normalizedKey = String(key ?? '').trim()
  if (!normalizedKey) return ''
  return `${normalizedType}:${normalizedKey}`
}

// return_error helpers

export type ReturnErrorDraft = {
  message: string
  statusCode: number
  code: string
  type: string
  skipRetry: boolean
  simpleMode: boolean
}

export const parseReturnErrorDraft = (valueText: string): ReturnErrorDraft => {
  const defaults: ReturnErrorDraft = {
    message: '',
    statusCode: 400,
    code: '',
    type: '',
    skipRetry: true,
    simpleMode: true,
  }
  const raw = String(valueText ?? '').trim()
  if (!raw) return defaults
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const statusRaw =
        parsed.status_code !== undefined ? parsed.status_code : parsed.status
      const statusValue = Number(statusRaw)
      return {
        ...defaults,
        message: String(
          (parsed.message as string) || (parsed.msg as string) || ''
        ).trim(),
        statusCode:
          Number.isInteger(statusValue) &&
          statusValue >= 100 &&
          statusValue <= 599
            ? statusValue
            : 400,
        code: String((parsed.code as string) || '').trim(),
        type: String((parsed.type as string) || '').trim(),
        skipRetry: parsed.skip_retry !== false,
        simpleMode: false,
      }
    }
  } catch {
    /* treat as plain text */
  }
  return { ...defaults, message: raw, simpleMode: true }
}

export const buildReturnErrorValueText = (
  draft: Partial<ReturnErrorDraft>
): string => {
  const message = String(draft.message || '').trim()
  if (draft.simpleMode) return message
  const statusCode = Number(draft.statusCode)
  const payload: Record<string, unknown> = {
    message,
    status_code:
      Number.isInteger(statusCode) && statusCode >= 100 && statusCode <= 599
        ? statusCode
        : 400,
  }
  const code = String(draft.code || '').trim()
  const type = String(draft.type || '').trim()
  if (code) payload.code = code
  if (type) payload.type = type
  if (draft.skipRetry === false) payload.skip_retry = false
  return JSON.stringify(payload)
}

// prune_objects helpers

export type PruneRule = {
  id: string
  path: string
  mode: string
  value_text: string
  invert: boolean
  pass_missing_key: boolean
}

export type PruneObjectsDraft = {
  simpleMode: boolean
  typeText: string
  logic: string
  recursive: boolean
  rules: PruneRule[]
}

export const normalizePruneRule = (
  rule: Record<string, unknown> = {}
): PruneRule => ({
  id: nextLocalId(),
  path: typeof rule.path === 'string' ? rule.path : '',
  mode: CONDITION_MODE_VALUES.has(rule.mode as string)
    ? (rule.mode as string)
    : 'full',
  value_text: toValueText(rule.value),
  invert: rule.invert === true,
  pass_missing_key: rule.pass_missing_key === true,
})

export const parsePruneObjectsDraft = (
  valueText: string
): PruneObjectsDraft => {
  const defaults: PruneObjectsDraft = {
    simpleMode: true,
    typeText: '',
    logic: 'AND',
    recursive: true,
    rules: [],
  }
  const raw = String(valueText ?? '').trim()
  if (!raw) return defaults
  try {
    const parsed = JSON.parse(raw)
    if (typeof parsed === 'string') {
      return { ...defaults, typeText: parsed.trim() }
    }
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const rules: PruneRule[] = []
      if (
        parsed.where &&
        typeof parsed.where === 'object' &&
        !Array.isArray(parsed.where)
      ) {
        for (const [path, value] of Object.entries(
          parsed.where as Record<string, unknown>
        )) {
          rules.push(normalizePruneRule({ path, mode: 'full', value }))
        }
      }
      if (Array.isArray(parsed.conditions)) {
        for (const item of parsed.conditions) {
          if (item && typeof item === 'object') {
            rules.push(normalizePruneRule(item))
          }
        }
      } else if (
        parsed.conditions &&
        typeof parsed.conditions === 'object' &&
        !Array.isArray(parsed.conditions)
      ) {
        for (const [path, value] of Object.entries(
          parsed.conditions as Record<string, unknown>
        )) {
          rules.push(normalizePruneRule({ path, mode: 'full', value }))
        }
      }
      const typeText =
        parsed.type === undefined ? '' : String(parsed.type).trim()
      const logic =
        String(parsed.logic || 'AND').toUpperCase() === 'OR' ? 'OR' : 'AND'
      const recursive = parsed.recursive !== false
      const hasAdvancedFields =
        parsed.logic !== undefined ||
        parsed.recursive !== undefined ||
        parsed.where !== undefined ||
        parsed.conditions !== undefined
      return {
        ...defaults,
        simpleMode: !hasAdvancedFields,
        typeText,
        logic,
        recursive,
        rules,
      }
    }
    return { ...defaults, typeText: String(parsed ?? '').trim() }
  } catch {
    return { ...defaults, typeText: raw }
  }
}

export const buildPruneObjectsValueText = (
  draft: PruneObjectsDraft
): string => {
  const typeText = String(draft.typeText || '').trim()
  if (draft.simpleMode) return typeText
  const payload: Record<string, unknown> = {}
  if (typeText) payload.type = typeText
  if (String(draft.logic || 'AND').toUpperCase() === 'OR') payload.logic = 'OR'
  if (draft.recursive === false) payload.recursive = false
  const conditions = (draft.rules || [])
    .filter((rule) => String(rule.path || '').trim())
    .map((rule) => {
      const conditionPayload: Record<string, unknown> = {
        path: String(rule.path || '').trim(),
        mode: CONDITION_MODE_VALUES.has(rule.mode) ? rule.mode : 'full',
      }
      const valueRaw = String(rule.value_text || '').trim()
      if (valueRaw !== '') conditionPayload.value = parseLooseValue(valueRaw)
      if (rule.invert) conditionPayload.invert = true
      if (rule.pass_missing_key) conditionPayload.pass_missing_key = true
      return conditionPayload
    })
  if (conditions.length > 0) payload.conditions = conditions
  if (!payload.type && !payload.conditions) {
    return JSON.stringify({ logic: 'AND' })
  }
  return JSON.stringify(payload)
}

// pass_headers helpers

const parsePassHeaderNames = (rawValue: unknown): string[] => {
  if (Array.isArray(rawValue)) {
    return rawValue.map((i) => String(i ?? '').trim()).filter(Boolean)
  }
  if (rawValue && typeof rawValue === 'object') {
    const obj = rawValue as Record<string, unknown>
    if (Array.isArray(obj.headers)) {
      return obj.headers.map((i) => String(i ?? '').trim()).filter(Boolean)
    }
    if (obj.header !== undefined) {
      const single = String(obj.header ?? '').trim()
      return single ? [single] : []
    }
    return []
  }
  if (typeof rawValue === 'string') {
    return rawValue
      .split(',')
      .map((i) => i.trim())
      .filter(Boolean)
  }
  return []
}

// Condition payload builder
const buildConditionPayload = (
  condition: ParamOverrideCondition
): Record<string, unknown> | null => {
  const path = condition.path.trim()
  if (!path) return null
  const payload: Record<string, unknown> = {
    path,
    mode: condition.mode || 'full',
    value: parseLooseValue(condition.value_text),
  }
  if (condition.invert) payload.invert = true
  if (condition.pass_missing_key) payload.pass_missing_key = true
  return payload
}

// Validation

const validateOperations = (
  operations: ParamOverrideOperation[],
  t: (key: string, options?: Record<string, unknown>) => string
): string => {
  for (let i = 0; i < operations.length; i++) {
    const op = operations[i]
    const mode = op.mode || 'set'
    const meta = MODE_META[mode] || MODE_META.set
    const line = i + 1
    const pathValue = op.path.trim()
    const fromValue = op.from.trim()
    const toValue = op.to.trim()

    if (meta.path && !pathValue) {
      return t('Rule {{line}} is missing target path', { line })
    }
    if (FROM_REQUIRED_MODES.has(mode) && !fromValue) {
      if (!(meta.pathAlias && pathValue)) {
        return t('Rule {{line}} is missing source field', { line })
      }
    }
    if (TO_REQUIRED_MODES.has(mode) && !toValue) {
      if (!(meta.pathAlias && pathValue)) {
        return t('Rule {{line}} is missing target field', { line })
      }
    }
    if (VALUE_REQUIRED_MODES.has(mode) && op.value_text.trim() === '') {
      return t('Rule {{line}} is missing value', { line })
    }

    if (mode === 'return_error') {
      const raw = op.value_text.trim()
      if (!raw) return t('Rule {{line}} is missing value', { line })
      try {
        const parsed = JSON.parse(raw)
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          if (
            !String((parsed as Record<string, unknown>).message || '').trim()
          ) {
            return t('Rule {{line}} return_error requires a message field', {
              line,
            })
          }
        }
      } catch {
        /* plain string is allowed */
      }
    }

    if (mode === 'prune_objects') {
      const raw = op.value_text.trim()
      if (!raw) {
        return t('Rule {{line}} prune_objects is missing conditions', { line })
      }
    }

    if (mode === 'pass_headers') {
      const raw = op.value_text.trim()
      if (!raw) {
        return t('Rule {{line}} pass_headers is missing header names', { line })
      }
      const parsed = parseLooseValue(raw)
      const headers = parsePassHeaderNames(parsed)
      if (headers.length === 0) {
        return t('Rule {{line}} pass_headers format is invalid', { line })
      }
    }
  }
  return ''
}

// Parse initial state

export type EditorState = {
  editMode: 'visual' | 'json'
  visualMode: 'operations' | 'legacy'
  legacyValue: string
  operations: ParamOverrideOperation[]
  jsonText: string
  jsonError: string
}

export const parseInitialState = (rawValue: string): EditorState => {
  const text = typeof rawValue === 'string' ? rawValue : ''
  const trimmed = text.trim()
  if (!trimmed) {
    return {
      editMode: 'visual',
      visualMode: 'operations',
      legacyValue: '',
      operations: [createDefaultOperation()],
      jsonText: '',
      jsonError: '',
    }
  }

  if (!verifyJSON(trimmed)) {
    return {
      editMode: 'json',
      visualMode: 'operations',
      legacyValue: '',
      operations: [createDefaultOperation()],
      jsonText: text,
      jsonError: 'Invalid JSON format',
    }
  }

  const parsed = JSON.parse(trimmed) as Record<string, unknown>
  const pretty = JSON.stringify(parsed, null, 2)

  if (
    parsed &&
    typeof parsed === 'object' &&
    !Array.isArray(parsed) &&
    Array.isArray(parsed.operations)
  ) {
    return {
      editMode: 'visual',
      visualMode: 'operations',
      legacyValue: '',
      operations:
        (parsed.operations as Record<string, unknown>[]).length > 0
          ? (parsed.operations as Record<string, unknown>[]).map(
              normalizeOperation
            )
          : [createDefaultOperation()],
      jsonText: pretty,
      jsonError: '',
    }
  }

  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    return {
      editMode: 'visual',
      visualMode: 'legacy',
      legacyValue: pretty,
      operations: [createDefaultOperation()],
      jsonText: pretty,
      jsonError: '',
    }
  }

  return {
    editMode: 'json',
    visualMode: 'operations',
    legacyValue: '',
    operations: [createDefaultOperation()],
    jsonText: pretty,
    jsonError: '',
  }
}

// Build operations JSON

export const buildOperationsJson = (
  sourceOperations: ParamOverrideOperation[],
  options: { validate: boolean },
  t: (key: string, options?: Record<string, unknown>) => string
): string => {
  const filteredOps = sourceOperations.filter((o) => !isOperationBlank(o))
  if (filteredOps.length === 0) return ''

  if (options.validate) {
    const message = validateOperations(filteredOps, t)
    if (message) throw new Error(message)
  }

  const payloadOps = filteredOps.map((operation) => {
    const mode = operation.mode || 'set'
    const meta = MODE_META[mode] || MODE_META.set
    const descriptionValue = String(operation.description || '').trim()
    const pathValue = operation.path.trim()
    const fromValue = operation.from.trim()
    const toValue = operation.to.trim()
    const payload: Record<string, unknown> = { mode }
    if (descriptionValue) payload.description = descriptionValue
    if (meta.path) payload.path = pathValue
    if (meta.pathOptional && pathValue) payload.path = pathValue
    if (meta.value) payload.value = parseLooseValue(operation.value_text)
    if (meta.keepOrigin && operation.keep_origin) payload.keep_origin = true
    if (meta.from) payload.from = fromValue
    if (!meta.to && operation.to.trim()) payload.to = toValue
    if (meta.to) payload.to = toValue
    if (meta.pathAlias) {
      if (!payload.from && pathValue) payload.from = pathValue
      if (!payload.to && pathValue) payload.to = pathValue
    }
    const conditions = operation.conditions
      .map(buildConditionPayload)
      .filter(Boolean)
    if (conditions.length > 0) {
      payload.conditions = conditions
      payload.logic = operation.logic === 'AND' ? 'AND' : 'OR'
    }
    return payload
  })

  return JSON.stringify({ operations: payloadOps }, null, 2)
}
