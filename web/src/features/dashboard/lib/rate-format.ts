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
// 首页概览指标条两个速率格的展示格式化（ui-spec §6.2A）。
//
// 两个速率的口径不同，展示规则也不同：
// - RPM 是小数（0.79），固定 1 位；
// - TPM 是数量级（199.2K），量大走缩写，量小退成带小数的普通数字。
//
// 语言 tag 必须先过 `toIntlLocale`：i18next 的 `zhCN`/`zhTW` 是非标准 tag，
// 直接喂 `Intl.*` 会抛 `Invalid language tag`（上一轮曾把整页打成 500）。

import { toIntlLocale } from '@/i18n/languages'
import { formatTokenAmount } from '@/lib/format'

/** 每分钟请求数（RPM）：固定 1 位小数；无窗口（<= 0）为 `0.0`。 */
export function formatRequestsPerMinute(
  value: number,
  locale?: string
): string {
  if (!Number.isFinite(value) || value <= 0) return '0.0'
  return new Intl.NumberFormat(toIntlLocale(locale), {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(value)
}

/** 每分钟 token 数（TPM）：>= 1000 走数量级缩写，更小给小数；无窗口为 `0`。 */
export function formatTokensPerMinute(value: number, locale?: string): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  if (value >= 1_000) return formatTokenAmount(value, locale)
  return new Intl.NumberFormat(toIntlLocale(locale), {
    maximumFractionDigits: 2,
  }).format(value)
}
