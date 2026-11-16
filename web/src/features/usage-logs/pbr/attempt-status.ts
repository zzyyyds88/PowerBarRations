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
import type { PBRAttempt } from './pbr-logs-api'

/** attempts 状态 → 展示色（routing-spec §9：区分冷却/熔断/跳过/人工停用）。 */
const ATTEMPT_STATUS_CLASSES: Record<string, string> = {
  success: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300',
  failed: 'bg-destructive/15 text-destructive',
  cooldown: 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
  circuit_break: 'bg-violet-500/15 text-violet-700 dark:text-violet-300',
  skipped: 'bg-muted text-muted-foreground',
  // 人工停用（配置态，非故障）：用与 cooldown（琥珀）/circuit_break（紫）都不同的
  // 中性灰蓝，避免被误读成"上游出故障"（routing-spec §9 要求并列且可区分）。
  disabled: 'bg-slate-500/15 text-slate-600 dark:text-slate-300',
}

export function attemptStatusClass(status: string): string {
  return ATTEMPT_STATUS_CLASSES[status] ?? 'bg-muted text-muted-foreground'
}

/** 是否是需要解释"为什么没用它"的跳过类状态。 */
export function isSkipStatus(status: string): boolean {
  return (
    status === 'cooldown' ||
    status === 'circuit_break' ||
    status === 'skipped' ||
    status === 'disabled'
  )
}

/** 失败请求是否有可展示的尝试链（成功请求也有，只是只有一条 success）。 */
export function hasAttempts(attempts: PBRAttempt[] | undefined): boolean {
  return Array.isArray(attempts) && attempts.length > 0
}

/** 把 attempts 压成一行摘要，便于列表列展示。 */
export function summarizeAttempts(attempts: PBRAttempt[] | undefined): string {
  if (!attempts || !hasAttempts(attempts)) {
    return ''
  }
  return attempts
    .map((attempt) => `${attempt.attempt_num}.${attempt.status}`)
    .join(' → ')
}
