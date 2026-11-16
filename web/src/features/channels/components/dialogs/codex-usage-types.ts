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
export type CodexRateLimitWindow = {
  used_percent?: number
  reset_at?: number
  reset_after_seconds?: number
  limit_window_seconds?: number
}

export type CodexRateLimit = {
  plan_type?: string
  allowed?: boolean
  limit_reached?: boolean
  primary_window?: CodexRateLimitWindow
  secondary_window?: CodexRateLimitWindow
}

export type CodexAdditionalRateLimit = {
  limit_name?: string
  metered_feature?: string
  rate_limit?: CodexRateLimit
  primary_window?: CodexRateLimitWindow
  secondary_window?: CodexRateLimitWindow
  plan_type?: string
}

export type CodexResetCredit = {
  id?: string
  reset_type?: string
  status?: string
  granted_at?: string | null
  expires_at?: string | null
  redeem_started_at?: string | null
  redeemed_at?: string | null
  profile_image_url?: string
  profile_user_id?: string
  title?: string
  description?: string
}

export type CodexResetCreditsPayload = {
  credits?: CodexResetCredit[]
  available_count?: number
  total_earned_count?: number
}

export type CodexUsagePayload = {
  plan_type?: string
  user_id?: string
  email?: string
  rate_limit?: CodexRateLimit
  additional_rate_limits?: CodexAdditionalRateLimit[]
  rate_limit_reset_credits?: {
    available_count?: number
  }
  credits?: {
    overage_limit_reached?: boolean
  }
  spend_control?: {
    reached?: boolean
  }
}

export type RateLimitSource = {
  plan_type?: string
  rate_limit?: CodexRateLimit
}

/**
 * Codex 用量/重置额度响应（api-spec §5.3.1）：成功 `{upstream_status,body}`，
 * 上游非 2xx 时后端回 502 `upstream_error`（由 axios 拒绝承载）。
 */
export type CodexUsageDialogData = {
  upstream_status?: number
  body?: unknown
}

export type CodexUsageDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelName?: string
  channelId?: number
  channelDisplayName?: string
  channelDisplayId?: string
  response: CodexUsageDialogData | null
  onRefresh?: () => void | Promise<void>
  isRefreshing?: boolean
}
