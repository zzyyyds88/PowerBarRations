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
