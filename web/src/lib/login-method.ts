/**
 * 登录方式展示名。PBR 无账号体系，只有登录口令（token-spec §2），
 * 因此这里只处理口令与未知两种值，其余历史值原样透传。
 */
import type { TFunction } from 'i18next'

export function loginMethodLabel(method: string, t: TFunction): string {
  const normalized = method.trim().toLowerCase()
  switch (normalized) {
    case 'password':
      return t('Password')
    case 'unknown':
    case '':
      return t('Unknown')
    default:
      return method
  }
}
