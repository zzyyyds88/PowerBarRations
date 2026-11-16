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
import { describe, expect, it } from 'vitest'

import { detectProviderPreset, PROVIDER_PRESETS } from '../provider-presets'

/*
预设只负责"填空"（协议 / 地址 / 名称）。服务商硬性请求头由网关在
relay/channel/provider_quirks.go 里按目标 host 自动补齐，前端不再参与，
所以这里只测识别逻辑。
*/
describe('detectProviderPreset', () => {
  it('distinguishes OpenCode Go from Zen on the same host', () => {
    expect(detectProviderPreset('https://opencode.ai/zen/go/v1')?.id).toBe(
      'opencode-go'
    )
    expect(detectProviderPreset('https://opencode.ai/zen')?.id).toBe(
      'opencode-zen'
    )
    // /zen/go 前缀不能被 /zen 抢先命中
    expect(detectProviderPreset('https://opencode.ai/zen/go')?.id).not.toBe(
      'opencode-zen'
    )
  })

  it('matches by host when no path prefix is declared', () => {
    expect(detectProviderPreset('https://api.deepseek.com/v1')?.id).toBe(
      'deepseek'
    )
    expect(detectProviderPreset('https://openrouter.ai/api/v1')?.id).toBe(
      'openrouter'
    )
  })

  it('tolerates a bare host without scheme', () => {
    expect(detectProviderPreset('api.moonshot.cn/v1')?.id).toBe('moonshot')
  })

  it('returns null for unknown or empty input', () => {
    expect(detectProviderPreset('https://llm.example.com/v1')).toBeNull()
    expect(detectProviderPreset('')).toBeNull()
    expect(detectProviderPreset(null)).toBeNull()
  })

  it('every preset carries a protocol, host and base URL', () => {
    for (const preset of PROVIDER_PRESETS) {
      expect(preset.name).toBeTruthy()
      expect(preset.baseUrl.startsWith('https://')).toBe(true)
      expect(preset.hosts.length).toBeGreaterThan(0)
      expect(preset.protocol).toBeTruthy()
    }
  })
})
