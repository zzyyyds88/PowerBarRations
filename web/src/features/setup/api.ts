import { getPBRSetupStatus, submitPBRSetup } from '@/lib/pbr-auth'

import type { SetupFormValues, SetupResponse } from './types'

/**
 * 首启状态。PBR 只有一个 initialized 布尔；这里适配成上游 setup 依赖的
 * `data.status` 形状，使 __root 的"未初始化则跳 /setup"判断继续可用。
 */
export async function getSetupStatus(): Promise<SetupResponse> {
  const { initialized } = await getPBRSetupStatus()
  return {
    success: true,
    data: { status: initialized, root_init: false, database_type: '' },
  }
}

/** 设置首个登录口令（POST /api/v1/setup），成功即签发会话。 */
export async function submitSetup(
  payload: SetupFormValues
): Promise<SetupResponse> {
  const res = await submitPBRSetup(payload.password)
  return {
    success: true,
    message: res.warning ?? '',
    data: { status: true, root_init: false, database_type: '' },
  }
}
