/**
 * 路由键分组（PBR 无"用户分组"概念，模型名即路由键）。
 *
 * 上游前端多处用 getGroups() 做分组筛选（来自 /api/group/）。PBR 已在 W7
 * 删除用户分组，故这里返回空列表，保留函数签名以免改动调用方。
 */
export async function getGroups(): Promise<{ success: boolean; data: string[] }> {
  return { success: true, data: [] }
}
