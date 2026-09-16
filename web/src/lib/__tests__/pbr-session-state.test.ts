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
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"

import { discardPBRSession, getPBRSessionState, pbrLogin } from "../pbr-auth"

// token-spec §2.5.1：口令变更后旧会话立即失效，前端必须能识别 stale 并清理，
// 否则会卡在"登录成功却每个请求都 401"的状态。
function jsonResponse(body: unknown, ok = true) {
  return {
    ok,
    status: ok ? 200 : 401,
    json: async () => body,
  } as unknown as Response
}

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal("fetch", fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("PBR 会话状态", () => {
  test("有效会话返回 authenticated=true 且 stale=false", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ authenticated: true, stale: false })
    )
    expect(await getPBRSessionState()).toEqual({
      authenticated: true,
      stale: false,
    })
  })

  test("带了失效 Cookie 时返回 stale=true（前端据此清态提示重登）", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ authenticated: false, stale: true })
    )
    expect(await getPBRSessionState()).toEqual({
      authenticated: false,
      stale: true,
    })
  })

  test("从未登录时不报 stale", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ authenticated: false, stale: false })
    )
    expect(await getPBRSessionState()).toEqual({
      authenticated: false,
      stale: false,
    })
  })

  test("登录前先清掉残留的失效 Cookie", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ initialized: true }))
    await pbrLogin("some-password")

    const urls = fetchMock.mock.calls.map((call) => call[0])
    expect(urls[0]).toBe("/api/v1/auth/logout")
    expect(urls).toContain("/api/v1/auth/login")
  })

  test("discardPBRSession 调用登出端点", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ logged_out: true }))
    await discardPBRSession()
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/auth/logout",
      expect.objectContaining({ method: "POST" })
    )
  })
})
