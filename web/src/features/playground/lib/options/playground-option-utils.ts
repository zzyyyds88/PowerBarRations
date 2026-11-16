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
import type { ModelOption } from '../../types'

/**
 * 试打台只接受 chat 补全；embedding / rerank / 语音 / 图像模型即使有车道，
 * 发到 `/v1/chat/completions` 也会被上游拒绝。PBR 目前没有模型能力字段，
 * 因此用名字前缀做保守过滤（宁可少列一个，也不让用户点进去必然失败）。
 */
const NON_CHAT_MODEL_PATTERN =
  /(^|[/:_-])(bge|embed|embedding|rerank|whisper|tts|speech|clip|stable-diffusion|sdxl|flux|dall-e|dalle)([/:_-]|$)/i

export function isChatCapableModel(name: string): boolean {
  return !NON_CHAT_MODEL_PATTERN.test(name)
}

export function getModelFallback(
  models: ModelOption[],
  currentModel: string
): string | null {
  const hasCurrentModel = models.some((model) => model.value === currentModel)

  if (hasCurrentModel || models.length === 0) {
    return null
  }

  return models[0].value
}

export function shouldClearModelWhenUnavailable(
  models: ModelOption[],
  currentModel: string
): boolean {
  if (currentModel === '') {
    return false
  }

  return !models.some((model) => model.value === currentModel)
}

export function getOptionLoadErrorMessage(
  error: unknown,
  fallbackMessage: string
): string {
  return error instanceof Error ? error.message : fallbackMessage
}
