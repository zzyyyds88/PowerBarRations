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
import { getPBRSetupStatus, submitPBRSetup } from '@/lib/pbr-auth'
import { sha256Bytes } from '@/lib/sha256'

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

/**
 * 计算 Base64(SHA256(口令))，与后端 token-spec §2.1 的派生规则一致：
 * 标准 Base64 带填充（32 字节摘要）。优先 crypto.subtle；非安全上下文
 * （HTTP 非 localhost）下 subtle 被禁用，用纯 JS SHA-256 兜底，两条路径
 * 结果必须一致。连兜底都失败才返回 null（理论上不会发生）。
 */
export async function deriveAdminKey(password: string): Promise<string | null> {
  const bytes = new TextEncoder().encode(password)
  let digest: Uint8Array | null = null
  const subtle = globalThis.crypto?.subtle
  if (subtle) {
    try {
      digest = new Uint8Array(await subtle.digest('SHA-256', bytes))
    } catch {
      digest = null
    }
  }
  if (!digest) {
    try {
      digest = sha256Bytes(bytes)
    } catch {
      return null
    }
  }
  let binary = ''
  for (const byte of digest) binary += String.fromCharCode(byte)
  return btoa(binary)
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
