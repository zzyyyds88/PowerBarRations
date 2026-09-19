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
import { api } from '@/lib/api'

import { API_ENDPOINTS } from './constants'
import { servedByFromResponse } from './lib/streaming/served-by'
import type {
  ChatCompletionRequest,
  ChatCompletionResponse,
  ChatCompletionResult,
  ModelOption,
} from './types'

/**
 * Send chat completion request (non-streaming)
 *
 * 用 `fetch` 而不是管理面的 axios 实例：模型面凭据是客户端密钥，
 * 而 axios 实例的请求拦截器会强制带上管理面 access token 与会话 Cookie。
 *
 * 同时把响应头 X-Served-By 一并返回（ui-spec §6.8：试打台展示实际命中的上游）。
 */
export async function sendChatCompletion(
  payload: ChatCompletionRequest,
  clientKey: string,
  signal?: AbortSignal
): Promise<ChatCompletionResult> {
  const response = await fetch(API_ENDPOINTS.CHAT_COMPLETIONS, {
    method: 'POST',
    credentials: 'omit',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${clientKey}`,
    },
    body: JSON.stringify(payload),
    signal,
  })
  if (!response.ok) {
    throw new Error(await buildModelFaceErrorMessage(response))
  }
  const body = (await response.json()) as ChatCompletionResponse
  return { response: body, servedBy: servedByFromResponse(response) }
}

/** 把模型面的失败响应压成一句可展示的话（优先服务端 message，其次状态码）。 */
async function buildModelFaceErrorMessage(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as {
      error?: { message?: string; code?: string }
    }
    const message = body.error?.message?.trim()
    if (message) {
      return `${response.status}: ${message}`
    }
    const code = body.error?.code?.trim()
    if (code) {
      return `${response.status}: ${code}`
    }
  } catch {
    // 响应不是 JSON（例如网关层错误页）：退回到状态码
  }
  return `HTTP ${response.status}`
}

/**
 * Get user available models
 */
export async function getUserModels(): Promise<ModelOption[]> {
  // PBR 无"用户模型"概念：可用模型即全部路由键（api-spec §5.7）。
  const res = await api.get('/api/models')
  const body = res.data as { items?: Array<{ model?: string }> }
  const items = body.items ?? []
  return items
    .map((item) => item.model)
    .filter(
      (model): model is string => typeof model === 'string' && model.length > 0
    )
    .map((model) => ({ label: model, value: model }))
}
