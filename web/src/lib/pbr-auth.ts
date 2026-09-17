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
/*
PowerBarRations —— PBR 原生认证适配层（token-spec §2）

背景：控制台前端整体搬迁自 new-api，其认证模型是"用户名+密码 → access token + refresh cookie"，
而 PBR 只有**一个登录口令**：登录后服务端签发 HttpOnly 会话 Cookie，浏览器不再持有管理密钥。

本模块把 PBR 的认证接成 new-api 前端期望的 `AuthBundle` 形状，使其余页面（依赖
auth store 的 user/accessToken/session 字段）无需改动即可工作。

映射：
  - POST /api/v1/auth/login   {password} → 签发会话 Cookie
  - GET  /api/v1/auth/session            → {authenticated: boolean, stale: boolean}
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
  if (!res.ok) throw new Error(`setup status request failed: ${res.status}`)
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
  if (!res.ok) {
    throw new Error(body.error?.message || `setup failed: ${res.status}`)
  }
  return body
}

/**
 * 会话状态（PBR /api/v1/auth/session，token-spec §2.5.1）。
 *
 * `stale=true` 表示浏览器带了会话 Cookie 但服务端已不认它——最典型的是管理口令
 * 变更后签名材料随之变化，旧会话立即失效。此时必须清除本地态并让用户知道
 * "凭据已变更"，否则会卡在"看似已登录、实际每个请求都 401"的状态。
 */
export interface PBRSessionState {
  authenticated: boolean
  stale: boolean
}

/** 查询当前会话状态（PBR /api/v1/auth/session）。 */
export async function getPBRSessionState(): Promise<PBRSessionState> {
  const res = await fetch('/api/v1/auth/session', {
    credentials: 'same-origin',
    cache: 'no-store',
  })
  if (!res.ok) return { authenticated: false, stale: false }
  const body = (await res.json().catch(() => ({}))) as {
    authenticated?: boolean
    stale?: boolean
  }
  return {
    authenticated: Boolean(body.authenticated),
    stale: Boolean(body.stale),
  }
}

/** 查询当前是否持有有效会话（兼容旧调用方）。 */
export async function getPBRSession(): Promise<boolean> {
  return (await getPBRSessionState()).authenticated
}

/**
 * 主动丢弃服务端会话 Cookie。
 *
 * 登录接口已经会用同名 Cookie 覆盖旧值，所以这一步是"双保险"，主要给
 * 检测到 `stale` 时调用：让浏览器立刻丢掉失效 Cookie，避免它继续被携带。
 */
export async function discardPBRSession(): Promise<void> {
  await pbrLogout()
}

/**
 * 口令登录（PBR /api/v1/auth/login）。成功返回合成 bundle，失败抛出可读错误。
 *
 * 登录前先清掉可能残留的失效 Cookie（口令变更后旧会话即失效，见 token-spec §2.5.1）：
 * 服务端登录成功本会用同名 Cookie 覆盖，但对**登录失败**的响应不会下发新 Cookie，
 * 残留旧值会让后续请求继续 401，制造"登录成功却进不去"的错觉。这里显式清一次。
 */
export async function pbrLogin(password: string): Promise<AuthBundle> {
  await discardPBRSession()
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
