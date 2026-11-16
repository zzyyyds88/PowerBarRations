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
/**
 * Billing expression parsing utilities (read-only display support).
 *
 * Parses the dynamic billing expression format so that usage-log breakdown
 * rows can be rendered from the backend expressions. Only the parsing side
 * is kept here; expression authoring lives with the backend.
 *
 * Display adapters intentionally accept fewer shapes than the shared
 * simulator. Existing ordered-tier and request-rule contracts stay intact;
 * executable custom expressions do not imply fixed unit prices.
 */

import {
  readTokenTierChain,
  readTimeTokenPricing,
  type TokenTier,
} from './billing-expression/display'
import { compileBillingExpression } from './billing-expression/parser'
import {
  splitExpressionAtTopLevel,
  unwrapExpressionParens,
} from './billing-expression/structure'

// ---------------------------------------------------------------------------
// Variable registry
// ---------------------------------------------------------------------------

export type BillingVar = {
  key: string
  field: string | null
  tierField: string | null
  label: string
  shortLabel: string
  side: 'input' | 'output' | 'condition'
  isBase?: boolean
  isConditionOnly?: boolean
  group?: string
}

export const BILLING_VARS: BillingVar[] = [
  {
    key: 'p',
    field: 'inputPrice',
    tierField: 'input_unit_cost',
    label: 'Input price',
    shortLabel: 'Input',
    side: 'input',
    isBase: true,
  },
  {
    key: 'c',
    field: 'outputPrice',
    tierField: 'output_unit_cost',
    label: 'Completion price',
    shortLabel: 'Output',
    side: 'output',
    isBase: true,
  },
  {
    key: 'len',
    field: null,
    tierField: null,
    label: 'Input length',
    shortLabel: 'Length',
    side: 'condition',
    isConditionOnly: true,
  },
  {
    key: 'cr',
    field: 'cacheReadPrice',
    tierField: 'cache_read_unit_cost',
    label: 'Cache read price',
    shortLabel: 'Cache Read',
    side: 'input',
    group: 'cache',
  },
  {
    key: 'cc',
    field: 'cacheCreatePrice',
    tierField: 'cache_create_unit_cost',
    label: 'Cache create price',
    shortLabel: 'Cache Write',
    side: 'input',
    group: 'cache',
  },
  {
    key: 'img_cr',
    field: 'imageCachePrice',
    tierField: 'image_cache_unit_cost',
    label: 'Image cache input price',
    shortLabel: 'Image Cache',
    side: 'input',
    group: 'cache',
  },
  {
    key: 'cc1h',
    field: 'cacheCreate1hPrice',
    tierField: 'cache_create_1h_unit_cost',
    label: 'Cache create (1h) price',
    shortLabel: 'Cache Write (1h)',
    side: 'input',
    group: 'cache',
  },
  {
    key: 'img',
    field: 'imagePrice',
    tierField: 'image_unit_cost',
    label: 'Image input price',
    shortLabel: 'Image In',
    side: 'input',
    group: 'media',
  },
  {
    key: 'img_o',
    field: 'imageOutputPrice',
    tierField: 'image_output_unit_cost',
    label: 'Image output price',
    shortLabel: 'Image Out',
    side: 'output',
    group: 'media',
  },
  {
    key: 'ai',
    field: 'audioInputPrice',
    tierField: 'audio_input_unit_cost',
    label: 'Audio input price',
    shortLabel: 'Audio In',
    side: 'input',
    group: 'media',
  },
  {
    key: 'ao',
    field: 'audioOutputPrice',
    tierField: 'audio_output_unit_cost',
    label: 'Audio output price',
    shortLabel: 'Audio Out',
    side: 'output',
    group: 'media',
  },
]

/** Vars that have real price fields (excludes condition-only vars like `len`) */
export const BILLING_PRICING_VARS: BillingVar[] = BILLING_VARS.filter(
  (v) => !v.isConditionOnly
)

const BILLING_VAR_KEY_TO_FIELD = Object.fromEntries(
  BILLING_PRICING_VARS.map((v) => [v.key, v.field as string])
) as Record<string, string>

// ---------------------------------------------------------------------------
// Request rule constants
// ---------------------------------------------------------------------------

const MATCH_EQ = 'eq'
const MATCH_CONTAINS = 'contains'
const MATCH_GT = 'gt'
const MATCH_GTE = 'gte'
const MATCH_LT = 'lt'
const MATCH_LTE = 'lte'
const MATCH_EXISTS = 'exists'
const MATCH_RANGE = 'range'

const TIME_FUNCS = ['hour', 'minute', 'weekday', 'month', 'day'] as const
type TimeFunc = (typeof TIME_FUNCS)[number]

const NUMERIC_LITERAL_REGEX = /^-?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$/

type ParamHeaderCondition = {
  source: 'param' | 'header'
  path: string
  mode: string
  value: string
}

type TimeCondition = {
  source: 'time'
  timeFunc: TimeFunc
  timezone: string
  mode: string
  value: string
  rangeStart: string
  rangeEnd: string
}

type RequestCondition = TimeCondition | ParamHeaderCondition

type RequestRuleGroup = {
  conditions: RequestCondition[]
  multiplier: string
  conditionText?: string
  matched?: boolean
}

export type RequestRuleTrace = {
  cond: string
  multiplier: number
  matched: boolean
}

export type TierCondition = {
  var: 'p' | 'c' | 'len'
  op: '<' | '<=' | '>' | '>='
  value: number
}

export type ParsedTier = {
  billingUnit?: 'token' | 'request'
  fixedPrice?: number
  conditionText?: string
  label: string
  conditions: TierCondition[]
  [field: string]: unknown
}

// ---------------------------------------------------------------------------
// Tier parser
// ---------------------------------------------------------------------------

function mapTokenTier(
  tier: TokenTier & { conditionText?: string }
): ParsedTier {
  return {
    label: tier.label,
    ...(tier.imageCount ? { imageCount: true } : {}),
    conditions: tier.conditions,
    ...(tier.billingUnit === 'request'
      ? { billingUnit: tier.billingUnit, fixedPrice: tier.fixedPrice }
      : {}),
    ...(tier.conditionText ? { conditionText: tier.conditionText } : {}),
    ...Object.fromEntries(
      Object.entries(tier.prices).map(([key, price]) => [
        BILLING_VAR_KEY_TO_FIELD[key],
        price,
      ])
    ),
  }
}

export function parseTiersFromExpr(exprStr: string): ParsedTier[] {
  if (!exprStr) return []
  const compiled = compileBillingExpression(exprStr)
  if (compiled.status !== 'ready') return []
  const canonical = readTokenTierChain(compiled.ast)
  if (canonical) return canonical.map(mapTokenTier)
  return readTimeTokenPricing(exprStr)?.tiers.map(mapTokenTier) ?? []
}

export function normalizeTierLabel(label: string | undefined): string {
  if (!label) return ''
  return label
    .replaceAll(/<[=＝]?|≤|＜[=＝]?/g, '<')
    .replaceAll(/>[=＝]?|≥|＞[=＝]?/g, '>')
    .replaceAll(/\s+/g, '')
    .toLowerCase()
}

// ---------------------------------------------------------------------------
// Request rule parser
// ---------------------------------------------------------------------------

function splitTopLevelMultiply(expr: string): string[] {
  return splitExpressionAtTopLevel(expr, '*')
}

function splitTopLevelAnd(expr: string): string[] {
  return splitExpressionAtTopLevel(expr, '&&')
}

function parseExprLiteral(raw: string): string | null {
  const text = raw.trim()
  if (text === 'true' || text === 'false') return text
  if (NUMERIC_LITERAL_REGEX.test(text)) return text
  try {
    return JSON.parse(text) as string
  } catch {
    return null
  }
}

// Time function value domains. Values outside these ranges are invalid for
// the corresponding time function (e.g. hour() is 0-23) and would otherwise
// produce always-true conditions like hour >= -1 || hour < -5.
const TIME_FUNC_RANGES: Record<TimeFunc, [number, number]> = {
  hour: [0, 23],
  minute: [0, 59],
  weekday: [0, 6],
  month: [1, 12],
  day: [1, 31],
}

function isTimeValueInRange(timeFunc: TimeFunc, text: string): boolean {
  if (!NUMERIC_LITERAL_REGEX.test(text)) return false
  const value = Number(text)
  if (!Number.isInteger(value)) return false
  const [min, max] = TIME_FUNC_RANGES[timeFunc]
  return value >= min && value <= max
}

function tryParseTimeCondition(expr: string): RequestCondition | null {
  let m = expr.match(
    /^(hour|minute|weekday|month|day)\("([^"]+)"\) >= ([\d.eE+-]+) (?:&&|\|\|) \1\("\2"\) < ([\d.eE+-]+)$/
  )
  if (!m) {
    m = expr.match(
      /^\((hour|minute|weekday|month|day)\("([^"]+)"\) >= ([\d.eE+-]+) (?:&&|\|\|) \1\("\2"\) < ([\d.eE+-]+)\)$/
    )
  }
  if (m) {
    // Reject invalid bounds at parse time: a leniently parsed rule would be
    // silently dropped when the expression is re-evaluated.
    if (
      !isTimeValueInRange(m[1] as TimeFunc, m[3]) ||
      !isTimeValueInRange(m[1] as TimeFunc, m[4])
    ) {
      return null
    }
    return {
      source: 'time',
      timeFunc: m[1] as TimeFunc,
      timezone: m[2],
      mode: MATCH_RANGE,
      value: '',
      rangeStart: m[3],
      rangeEnd: m[4],
    }
  }
  m = expr.match(
    /^(hour|minute|weekday|month|day)\("([^"]+)"\) (==|>=|<) ([\d.eE+-]+)$/
  )
  if (m) {
    if (!isTimeValueInRange(m[1] as TimeFunc, m[4])) return null
    const opMap: Record<string, string> = {
      '==': MATCH_EQ,
      '>=': MATCH_GTE,
      '<': MATCH_LT,
    }
    return {
      source: 'time',
      timeFunc: m[1] as TimeFunc,
      timezone: m[2],
      mode: opMap[m[3]] || MATCH_EQ,
      value: m[4],
      rangeStart: '',
      rangeEnd: '',
    }
  }
  return null
}

function tryParseRequestCondition(expr: string): RequestCondition | null {
  const tc = tryParseTimeCondition(expr)
  if (tc) return tc

  let m = expr.match(/^header\("([^"]+)"\) != ""$/)
  if (m) return { source: 'header', path: m[1], mode: MATCH_EXISTS, value: '' }

  m = expr.match(/^param\("([^"]+)"\) != nil$/)
  if (m) return { source: 'param', path: m[1], mode: MATCH_EXISTS, value: '' }

  m = expr.match(/^has\(header\("([^"]+)"\), ((?:"(?:[^"\\]|\\.)*"))\)$/)
  if (m) {
    return {
      source: 'header',
      path: m[1],
      mode: MATCH_CONTAINS,
      value: JSON.parse(m[2]) as string,
    }
  }

  m = expr.match(
    /^param\("([^"]+)"\) != nil && has\(param\("([^"]+)"\), ((?:"(?:[^"\\]|\\.)*"))\)$/
  )
  if (m && m[1] === m[2]) {
    return {
      source: 'param',
      path: m[1],
      mode: MATCH_CONTAINS,
      value: JSON.parse(m[3]) as string,
    }
  }

  m = expr.match(
    /^param\("([^"]+)"\) != nil && param\("([^"]+)"\) (>|>=|<|<=) ([\d.eE+-]+)$/
  )
  if (m && m[1] === m[2]) {
    const opMap: Record<string, string> = {
      '>': MATCH_GT,
      '>=': MATCH_GTE,
      '<': MATCH_LT,
      '<=': MATCH_LTE,
    }
    return { source: 'param', path: m[1], mode: opMap[m[3]], value: m[4] }
  }

  m = expr.match(/^(param|header)\("([^"]+)"\) == (.+)$/)
  if (m) {
    const parsedValue = parseExprLiteral(m[3])
    if (parsedValue === null) return null
    return {
      source: m[1] as 'param' | 'header',
      path: m[2],
      mode: MATCH_EQ,
      value: String(parsedValue),
    }
  }

  return null
}

function tryParseTimeRangePair(
  lower: string,
  upper: string
): RequestCondition | null {
  const a = tryParseTimeCondition(lower)
  const b = tryParseTimeCondition(upper)
  if (!a || !b || a.source !== 'time' || b.source !== 'time') return null
  const ta = a as TimeCondition
  const tb = b as TimeCondition
  if (ta.timeFunc !== tb.timeFunc || ta.timezone !== tb.timezone) return null
  if (ta.mode !== MATCH_GTE || tb.mode !== MATCH_LT) return null
  return {
    source: 'time',
    timeFunc: ta.timeFunc,
    timezone: ta.timezone,
    mode: MATCH_RANGE,
    value: '',
    rangeStart: ta.value,
    rangeEnd: tb.value,
  }
}

function tryParseRequestConditions(
  conditionStr: string
): RequestCondition[] | null {
  // A single time range like hour(tz) >= 9 && hour(tz) < 12 must stay one
  // MATCH_RANGE condition instead of being split into two scalar conditions.
  const wholeTimeCond = tryParseTimeCondition(conditionStr.trim())
  if (wholeTimeCond) return [wholeTimeCond]

  const andParts = splitTopLevelAnd(conditionStr)
  const conditions: RequestCondition[] = []
  for (let i = 0; i < andParts.length; i += 1) {
    const part = andParts[i].trim()
    // Adjacent matching time bounds (fn >= X && fn < Y) form one range; merge
    // them so a single MATCH_RANGE row is kept even when other conditions
    // follow in the same group.
    const next = i + 1 < andParts.length ? andParts[i + 1].trim() : ''
    const merged = next ? tryParseTimeRangePair(part, next) : null
    if (merged) {
      conditions.push(merged)
      i += 1
      continue
    }
    const condition = tryParseRequestCondition(part)
    if (!condition) return null
    conditions.push(condition)
  }
  return conditions.length > 0 ? conditions : null
}

function tryParseRuleGroupFactor(part: string): RequestRuleGroup | null {
  const m = part.match(/^\((.+) \? ([\d.eE+-]+) : 1\)$/s)
  if (!m) return null

  const conditions = tryParseRequestConditions(m[1])
  if (!conditions) return null
  return { conditions, multiplier: m[2] }
}

function tryParseRequestRuleExpr(expr: string): RequestRuleGroup[] | null {
  const trimmed = (expr || '').trim()
  if (!trimmed) return []

  const parts = splitTopLevelMultiply(trimmed)
  const groups: RequestRuleGroup[] = []
  for (const part of parts) {
    const group = tryParseRuleGroupFactor(part)
    if (!group) return null
    groups.push(group)
  }
  return groups
}

// ---------------------------------------------------------------------------
// Split billing expr and request rules
// ---------------------------------------------------------------------------

function unwrapOuterParens(expr: string): string {
  return unwrapExpressionParens(expr)
}

export function splitBillingExprAndRequestRules(expr: string): {
  billingExpr: string
  requestRuleExpr: string
} {
  const trimmed = (expr || '').trim()
  if (!trimmed) return { billingExpr: '', requestRuleExpr: '' }

  const parts = splitTopLevelMultiply(trimmed)
  if (parts.length <= 1) return { billingExpr: trimmed, requestRuleExpr: '' }

  const ruleParts: string[] = []
  const baseParts: string[] = []

  parts.forEach((part) => {
    const parsed = tryParseRequestRuleExpr(part)
    const compiled = compileBillingExpression(part)
    const traced =
      compiled.status === 'ready' &&
      compiled.requestRules.some((rule) => rule.node === compiled.ast)
    if ((parsed && parsed.length > 0) || traced) {
      ruleParts.push(part)
    } else {
      baseParts.push(part)
    }
  })

  const quantityParts = baseParts.filter(
    (part) => unwrapOuterParens(part) === 'image_count'
  )
  if (ruleParts.length === 0 || baseParts.length - quantityParts.length !== 1) {
    return { billingExpr: trimmed, requestRuleExpr: '' }
  }

  return {
    billingExpr: baseParts.map(unwrapOuterParens).join(' * '),
    requestRuleExpr: ruleParts.join(' * '),
  }
}
