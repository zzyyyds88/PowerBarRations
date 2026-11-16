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
// ============================================================================
// System Status
// ============================================================================

/**
 * `/api/status` 响应。PBR 无账号体系，这里只保留控制台真正读取的字段；
 * 其余键通过索引签名透传（后端选项 map 本身是开放集合）。
 */
export interface SystemStatus {
  success?: boolean
  message?: string
  data?: {
    version?: string
    system_name?: string
    logo?: string
    display_in_currency?: boolean
    display_token_stat_enabled?: boolean
    quota_per_unit?: number
    quota_display_type?: string
    usd_exchange_rate?: number
    custom_currency_symbol?: string
    custom_currency_exchange_rate?: number
    user_agreement_enabled?: boolean
    privacy_policy_enabled?: boolean
    [key: string]: unknown
  }
  version?: string
  system_name?: string
  logo?: string
  display_in_currency?: boolean
  display_token_stat_enabled?: boolean
  quota_per_unit?: number
  quota_display_type?: string
  usd_exchange_rate?: number
  custom_currency_symbol?: string
  custom_currency_exchange_rate?: number
  user_agreement_enabled?: boolean
  privacy_policy_enabled?: boolean
  [key: string]: unknown
}

// ============================================================================
// Form Props
// ============================================================================

export interface AuthFormProps extends React.HTMLAttributes<HTMLFormElement> {
  redirectTo?: string
}
