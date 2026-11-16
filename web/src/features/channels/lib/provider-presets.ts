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
内置服务商预设（渠道创建时的「快速配置」）。

背景（用户 2026-11-05 反馈）：上游协议只有 4 种，但同一协议下的服务商在
base_url 之外还有各自的硬性要求——最典型的是 OpenCode Zen/Go 必须带
`x-opencode-session`，缺了就是 400 "cannot be routed efficiently"。这类要求
写不进协议抽象，用户只能自己去翻文档、手填 param_override，这就是"加个渠道
怎么这么麻烦"的来源。

预设只做**填空题**，不引入新的厂商类型：选中后仍落到同一个协议 + base_url +
param_override，渠道模型和路由语义完全不变（ui-spec §6.4 的"不按厂商分类"
继续成立）。

服务商**硬性请求头**不在这里：它由网关在组装上游请求时按目标 host 自动补齐
（`relay/channel/provider_quirks.go`），现有渠道和 API 创建的渠道都自动受益，
用户不需要为某个服务商手写 param_override。本模块只负责把可变的、用户可见的
配置（协议 / 地址 / 名称）填好。
*/
import type { ChannelProtocol } from '../types'

export interface ProviderPreset {
  /** 稳定 id（用作选项 key）。 */
  id: string
  /** 展示名（服务商名，不做 i18n——是专有名词）。 */
  name: string
  /** Lobe 图标名；无对应图标时回退首字母。 */
  icon?: string
  protocol: ChannelProtocol
  baseUrl: string
  /** 用于从 base_url 自动识别的 host 列表。 */
  hosts: string[]
  /**
   * 同一 host 下按路径区分的服务（OpenCode Zen / Go 共用 opencode.ai）。
   * 只在匹配到该前缀时才认这个预设。
   */
  pathPrefix?: string
  /** 键前缀提示，仅用于说明，不参与识别。 */
  keyHint?: string
}

/**
 * 预设清单。
 *
 * 只收录「地址固定且已知」的服务商；需要用户自己填私有部署地址的不进来。
 * 只填协议 / 地址 / 名称——服务商硬性请求头由网关自动补齐，见文件头说明。
 */
export const PROVIDER_PRESETS: readonly ProviderPreset[] = [
  {
    id: 'opencode-go',
    name: 'OpenCode Go',
    icon: 'OpenCode',
    protocol: 'openai-chat',
    baseUrl: 'https://opencode.ai/zen/go',
    hosts: ['opencode.ai'],
    pathPrefix: '/zen/go',
    keyHint: 'oc_sk_...',
  },
  {
    id: 'opencode-zen',
    name: 'OpenCode Zen',
    icon: 'OpenCode',
    protocol: 'openai-chat',
    baseUrl: 'https://opencode.ai/zen',
    hosts: ['opencode.ai'],
    pathPrefix: '/zen',
    keyHint: 'oc_sk_...',
  },
  {
    id: 'deepseek',
    name: 'DeepSeek',
    icon: 'DeepSeek',
    protocol: 'openai-chat',
    baseUrl: 'https://api.deepseek.com',
    hosts: ['api.deepseek.com'],
    keyHint: 'sk-...',
  },
  {
    id: 'openrouter',
    name: 'OpenRouter',
    icon: 'OpenRouter',
    protocol: 'openai-chat',
    baseUrl: 'https://openrouter.ai/api',
    hosts: ['openrouter.ai'],
    keyHint: 'sk-or-...',
  },
  {
    id: 'moonshot',
    name: 'Moonshot',
    icon: 'Moonshot',
    protocol: 'openai-chat',
    baseUrl: 'https://api.moonshot.cn',
    hosts: ['api.moonshot.cn'],
    keyHint: 'sk-...',
  },
  {
    id: 'siliconflow',
    name: 'SiliconFlow',
    icon: 'SiliconCloud',
    protocol: 'openai-chat',
    baseUrl: 'https://api.siliconflow.cn',
    hosts: ['api.siliconflow.cn'],
    keyHint: 'sk-...',
  },
  {
    id: 'zhipu',
    name: 'Zhipu GLM',
    icon: 'Zhipu',
    protocol: 'openai-chat',
    baseUrl: 'https://open.bigmodel.cn/api/paas',
    hosts: ['open.bigmodel.cn'],
    keyHint: '...',
  },
  {
    id: 'alibaba',
    name: 'Alibaba DashScope',
    icon: 'Qwen',
    protocol: 'openai-chat',
    baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode',
    hosts: ['dashscope.aliyuncs.com'],
    keyHint: 'sk-...',
  },
  {
    id: 'volcengine',
    name: 'Volcengine Ark',
    icon: 'Volcengine',
    protocol: 'openai-chat',
    baseUrl: 'https://ark.cn-beijing.volces.com/api',
    hosts: ['ark.cn-beijing.volces.com'],
    keyHint: '...',
  },
  {
    id: 'groq',
    name: 'Groq',
    icon: 'Groq',
    protocol: 'openai-chat',
    baseUrl: 'https://api.groq.com/openai',
    hosts: ['api.groq.com'],
    keyHint: 'gsk_...',
  },
  {
    id: 'xai',
    name: 'xAI',
    icon: 'XAI',
    protocol: 'openai-chat',
    baseUrl: 'https://api.x.ai',
    hosts: ['api.x.ai'],
    keyHint: 'xai-...',
  },
  {
    id: 'cerebras',
    name: 'Cerebras',
    icon: 'Cerebras',
    protocol: 'openai-chat',
    baseUrl: 'https://api.cerebras.ai',
    hosts: ['api.cerebras.ai'],
    keyHint: 'csk-...',
  },
  {
    id: 'mistral',
    name: 'Mistral',
    icon: 'Mistral',
    protocol: 'openai-chat',
    baseUrl: 'https://api.mistral.ai',
    hosts: ['api.mistral.ai'],
    keyHint: '...',
  },
  {
    id: 'anthropic',
    name: 'Anthropic',
    icon: 'Claude',
    protocol: 'anthropic',
    baseUrl: 'https://api.anthropic.com',
    hosts: ['api.anthropic.com'],
    keyHint: 'sk-ant-...',
  },
  {
    id: 'gemini',
    name: 'Google Gemini',
    icon: 'Gemini',
    protocol: 'gemini',
    baseUrl: 'https://generativelanguage.googleapis.com',
    hosts: ['generativelanguage.googleapis.com'],
    keyHint: 'AIza...',
  },
]

/** 解析 base_url（容错：无协议、带空格、带完整端点路径）。 */
function parseBaseUrl(raw: string): URL | null {
  const trimmed = raw.trim()
  if (!trimmed) return null
  const withProtocol = /^https?:\/\//i.test(trimmed)
    ? trimmed
    : `https://${trimmed}`
  try {
    return new URL(withProtocol)
  } catch {
    return null
  }
}

/**
 * 从 base_url 识别服务商预设。
 *
 * 先按 host 匹配，再按 `pathPrefix` 区分同一 host 下的服务（OpenCode Zen/Go）。
 * 无前缀约束的预设只按 host 命中；识别不到返回 null，UI 回落为普通渠道。
 */
export function detectProviderPreset(
  baseUrl: string | null | undefined
): ProviderPreset | null {
  const parsed = parseBaseUrl(baseUrl ?? '')
  if (!parsed) return null
  const host = parsed.hostname.toLowerCase()
  const path = parsed.pathname.toLowerCase()

  let hostMatch: ProviderPreset | null = null
  for (const preset of PROVIDER_PRESETS) {
    if (!preset.hosts.some((item) => host === item.toLowerCase())) continue
    if (preset.pathPrefix) {
      if (path.startsWith(preset.pathPrefix.toLowerCase())) return preset
      continue
    }
    if (!hostMatch) hostMatch = preset
  }
  return hostMatch
}

/** 名字去重展示：同名预设（Zen/Go）在 base_url 上区分，选项里保留完整名。 */
export function presetLabel(preset: ProviderPreset): string {
  return preset.name
}
