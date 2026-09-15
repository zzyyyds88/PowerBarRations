/*
PowerBarRations —— PBR 原生认证适配层（token-spec §2）

背景：控制台前端整体搬迁自 new-api，其认证模型是"用户名+密码 → access token + refresh cookie"，
而 PBR 只有**一个登录口令**：登录后服务端签发 HttpOnly 会话 Cookie，浏览器不再持有管理密钥。

本模块把 PBR 的认证接成 new-api 前端期望的 `AuthBundle` 形状，使其余页面（依赖
auth store 的 user/accessToken/session 字段）无需改动即可工作。

映射：
  - POST /api/v1/auth/login   {password} → 签发会话 Cookie
  - GET  /api/v1/auth/session            → {authenticated: boolean}
  - POST /api/v1/auth/logout             → 清 Cookie
  - GET  /api/v1/setup/status            → {initialized: boolean}
  - POST /api/v1/setup       {password}  → 首个口令 + 签发会话

由于会话是 HttpOnly Cookie，前端拿不到也不需要 token；这里用一个**本地合成**的
access token（仅用于满足前端类型与请求头）与会话描述。真正的鉴权由 Cookie 承载。
*/

import type { AuthBundle, AuthUser } from '@/stores/auth-store'

/** PBR 是单用户网关：合成一个"管理员"身份，权限为最高。 */
export const PBR_ADMIN_USER: AuthUser = {
  id: 1,
  username: 'admin',
  display_name: '管理员',
  role: 100, // ROLE.SUPER_ADMIN，见 src/lib/roles.ts
}

/** 合成会话描述：有效期取 7 天，与后端 PBR_SESSION_TTL_HOURS 缺省一致。 */
function syntheticSession() {
  const now = Math.floor(Date.now() / 1000)
  return {
    sid: 'pbr-session',
    current: true,
    login_method: 'password',
    ip: '',
    user_agent: typeof navigator === 'undefined' ? '' : navigator.userAgent,
    created_at: now,
    last_active_at: now,
    expires_at: now + 7 * 24 * 60 * 60,
  }
}

/**
 * 合成一个前端可用的 AuthBundle。
 *
 * `access_token` 只是占位：PBR 鉴权走 HttpOnly Cookie，不依赖 Authorization 头。
 * 之所以仍填非空字符串，是因为上游前端多处会以"是否有 accessToken"判断登录态。
 */
export function buildPBRBundle(): AuthBundle {
  return {
    access_token: 'pbr-session-cookie',
    token_type: 'Cookie',
    access_expires_at: Math.floor(Date.now() / 1000) + 7 * 24 * 60 * 60,
    user: PBR_ADMIN_USER,
    session: syntheticSession(),
  }
}

export interface PBRSetupStatus {
  initialized: boolean
}

/** 读取首启状态（PBR /api/v1/setup/status）。 */
export async function getPBRSetupStatus(): Promise<PBRSetupStatus> {
  const res = await fetch('/api/v1/setup/status', {
    credentials: 'same-origin',
    cache: 'no-store',
  })
  if (!res.ok) throw new Error('setup status request failed: ' + res.status)
  return (await res.json()) as PBRSetupStatus
}

/** 首启设置口令（PBR /api/v1/setup），成功即签发会话。 */
export async function submitPBRSetup(password: string): Promise<{ warning?: string }> {
  const res = await fetch('/api/v1/setup', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  const body = (await res.json().catch(() => ({}))) as { warning?: string; error?: { message?: string } }
  if (!res.ok) throw new Error(body.error?.message || 'setup failed: ' + res.status)
  return body
}

/** 查询当前是否持有有效会话（PBR /api/v1/auth/session）。 */
export async function getPBRSession(): Promise<boolean> {
  const res = await fetch('/api/v1/auth/session', {
    credentials: 'same-origin',
    cache: 'no-store',
  })
  if (!res.ok) return false
  const body = (await res.json().catch(() => ({}))) as { authenticated?: boolean }
  return Boolean(body.authenticated)
}

/**
 * 口令登录（PBR /api/v1/auth/login）。成功返回合成 bundle，失败抛出可读错误。
 */
export async function pbrLogin(password: string): Promise<AuthBundle> {
  const res = await fetch('/api/v1/auth/login', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } }
  if (!res.ok) {
    throw new Error(body.error?.message || '登录失败')
  }
  return buildPBRBundle()
}

/** 登出（PBR /api/v1/auth/logout）。 */
export async function pbrLogout(): Promise<void> {
  await fetch('/api/v1/auth/logout', {
    method: 'POST',
    credentials: 'same-origin',
    cache: 'no-store',
  }).catch(() => undefined)
}
