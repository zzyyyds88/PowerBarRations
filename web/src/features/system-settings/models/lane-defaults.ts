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
import { api } from '@/lib/api'

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
