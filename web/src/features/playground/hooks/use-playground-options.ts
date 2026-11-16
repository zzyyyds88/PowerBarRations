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
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { getApiKeys } from '@/features/keys/api'
import { API_KEY_STATUS } from '@/features/keys/constants'
import { handleServerError } from '@/lib/handle-server-error'

import { getUserModels } from '../api'
import {
  getModelFallback,
  getOptionLoadErrorMessage,
  shouldClearModelWhenUnavailable,
} from '../lib'
import type { ClientKeyOption, ModelOption, PlaygroundConfig } from '../types'

type UsePlaygroundOptionsParams = {
  currentClientKey: string
  currentModel: string
  setModels: (models: ModelOption[]) => void
  updateConfig: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
}

export function usePlaygroundOptions({
  currentClientKey,
  currentModel,
  setModels,
  updateConfig,
}: UsePlaygroundOptionsParams) {
  const { t } = useTranslation()

  const {
    data: modelsData,
    error: modelsError,
    isError: isModelsError,
    isLoading: isLoadingModels,
  } = useQuery({
    queryKey: ['playground-models'],
    queryFn: async () => getUserModels(),
  })

  const {
    data: clientKeysData,
    error: clientKeysError,
    isError: isClientKeysError,
    isLoading: isLoadingClientKeys,
  } = useQuery({
    queryKey: ['playground-client-keys'],
    queryFn: async () => {
      const result = await getApiKeys({ p: 1, size: 100 })
      if (!result.success) {
        throw new Error(result.message || t('Failed to load API keys'))
      }

      return (result.data?.items ?? [])
        .filter(
          (item) =>
            item.status === API_KEY_STATUS.ENABLED &&
            Boolean(item.key_plain?.trim())
        )
        .map<ClientKeyOption>((item) => {
          const value = item.key_plain?.trim() ?? ''
          return {
            id: item.id,
            label: item.name || value.slice(0, 10),
            value,
            prefix: value.slice(0, 10),
          }
        })
    },
  })

  useEffect(() => {
    if (!isModelsError) return

    handleServerError(
      modelsError,
      getOptionLoadErrorMessage(
        modelsError,
        t('Failed to load playground models')
      )
    )
  }, [isModelsError, modelsError, t])

  useEffect(() => {
    if (!modelsData) return

    setModels(modelsData)
    const fallback = getModelFallback(modelsData, currentModel)

    if (fallback) {
      updateConfig('model', fallback)
      return
    }

    if (shouldClearModelWhenUnavailable(modelsData, currentModel)) {
      updateConfig('model', '')
    }
  }, [modelsData, currentModel, setModels, updateConfig])

  useEffect(() => {
    if (!isClientKeysError) return

    handleServerError(
      clientKeysError,
      getOptionLoadErrorMessage(clientKeysError, t('Failed to load API keys'))
    )
  }, [clientKeysError, isClientKeysError, t])

  useEffect(() => {
    if (!clientKeysData || !currentClientKey) return
    if (clientKeysData.some((item) => item.value === currentClientKey)) return
    updateConfig('clientKey', '')
  }, [clientKeysData, currentClientKey, updateConfig])

  return {
    isLoadingModels,
    clientKeys: clientKeysData ?? [],
    isLoadingClientKeys,
  }
}
