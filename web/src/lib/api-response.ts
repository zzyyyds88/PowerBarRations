/** 管理面统一响应包络（api-spec §3）。原在 features/profile/types，已随多用户面删除。 */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}
