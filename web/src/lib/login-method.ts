/**
 * 登录方式展示名。原位于 features/security（多用户/账号安全，已随 W7 删除），
 * 但保留页（请求日志/审计）需要它，故移到这里。
 */
import type { TFunction } from 'i18next'

export function loginMethodLabel(method: string, t: TFunction): string {
  const normalized = method.trim().toLowerCase()
  switch (normalized) {
    case 'password':
      return t('Password')
    case '2fa':
      return t('Two-factor Authentication')
    case 'passkey':
      return t('Passkey')
    case 'wechat':
      return t('WeChat')
    case 'telegram':
      return t('Telegram')
    case 'oauth':
      return t('OAuth')
    case 'unknown':
    case '':
      return t('Unknown')
    default:
      break
  }
  if (!normalized.startsWith('oauth:')) return method
  const provider = normalized.slice('oauth:'.length)
  const providerNames: Record<string, string> = {
    discord: 'Discord',
    github: 'GitHub',
    linuxdo: 'LinuxDO',
    oidc: 'OIDC',
  }
  return `${t('OAuth')} · ${providerNames[provider] || provider}`
}
