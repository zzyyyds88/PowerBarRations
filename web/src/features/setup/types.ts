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
/** PBR 首启初始化：与上游 new-api 的多步向导不同，PBR 只需设置一个登录口令。 */

export interface SetupStatus {
  /** 是否已完成初始化（对应 PBR GET /api/v1/setup/status 的 initialized）。 */
  status: boolean
  root_init: boolean
  database_type: string
}

export interface SetupFormValues {
  password: string
  confirmPassword: string
}

export interface SetupResponse {
  success: boolean
  message?: string
  data?: SetupStatus
}
