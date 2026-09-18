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
import { Copy } from 'lucide-react'
import React, { useState, useCallback, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import useDialogState from '@/hooks/use-dialog'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { handleServerError } from '@/lib/handle-server-error'

import { fetchTokenKey, fetchTokenKeysBatch, rotateApiKey } from '../api'
import { ERROR_MESSAGES } from '../constants'
import type { ApiKey, ApiKeysDialogType } from '../types'

type ApiKeysContextType = {
  open: ApiKeysDialogType | null
  setOpen: (str: ApiKeysDialogType | null) => void
  currentRow: ApiKey | null
  setCurrentRow: React.Dispatch<React.SetStateAction<ApiKey | null>>
  refreshTrigger: number
  triggerRefresh: () => void
  resolvedKey: string
  setResolvedKey: React.Dispatch<React.SetStateAction<string>>
  /**
   * 显式轮换：调用 rotate 接口并返回仅此一次的新明文（会立即作废旧密钥）。
   * `force` 为 true 时忽略缓存，确保每次显式轮换都真正换新。
   */
  rotateKey: (id: number, force?: boolean) => Promise<string | null>
  /** 当前打开轮换确认/明文展示的令牌 id（跨行重挂载保持）。 */
  rotateTarget: number | null
  /** 最近一次轮换返回的明文，只在对应令牌的确认框内展示一次。 */
  rotatedKey: { id: number; key: string } | null
  openRotateConfirm: (id: number) => void
  closeRotateConfirm: () => void
  /**
   * 请求「需要明文密钥」的动作（Copy Key / CC Switch / Chat 等）：
   * 先弹轮换确认，确认并拿到新明文后执行 action；取消则不执行。
   */
  requestRotateAction: (
    id: number,
    action: (realKey: string) => void | Promise<void>
  ) => void
  /** 确认框内的确认：真正轮换并执行待办动作。 */
  confirmRotateAction: () => Promise<void>
  /** 确认框内的取消/完成：清空待办动作。 */
  cancelRotateAction: () => void
  resolveRealKeysBatch: (ids: number[]) => Promise<Record<number, string>>
  resolvedKeys: Record<number, string>
  loadingKeys: Record<number, boolean>
  copiedKeyId: number | null
  markKeyCopied: (id: number) => void
}

const ApiKeysContext = React.createContext<ApiKeysContextType | null>(null)

export function ApiKeysProvider({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const [open, setOpen] = useDialogState<ApiKeysDialogType>(null)
  const [currentRow, setCurrentRow] = useState<ApiKey | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  const [resolvedKey, setResolvedKey] = useState('')

  const [resolvedKeys, setResolvedKeys] = useState<Record<number, string>>({})
  const [loadingKeys, setLoadingKeys] = useState<Record<number, boolean>>({})
  const pendingRequests = useRef<Record<number, Promise<string | null>>>({})

  const [copiedKeyId, setCopiedKeyId] = useState<number | null>(null)
  const copiedTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  // 轮换确认/一次性明文放在 Provider：表格行在 Provider 状态变化时可能重挂载，
  // 局部 state 会丢失，导致刚显示的新明文消失。
  const [rotateTarget, setRotateTarget] = useState<number | null>(null)
  const [rotatedKey, setRotatedKey] = useState<{
    id: number
    key: string
  } | null>(null)

  const openRotateConfirm = useCallback((id: number) => {
    setRotateTarget(id)
    setRotatedKey(null)
  }, [])

  // 待执行动作：仅迁移前没有明文的密钥需要确认轮换，取消即丢弃。
  const [pendingRotateAction, setPendingRotateAction] = useState<
    ((realKey: string) => void | Promise<void>) | null
  >(null)

  const closeRotateConfirm = useCallback(() => {
    setRotateTarget(null)
    setRotatedKey(null)
    setPendingRotateAction(null)
  }, [])

  const requestRotateAction = useCallback(
    (id: number, action: (realKey: string) => void | Promise<void>) => {
      setPendingRotateAction(() => action)
      setRotateTarget(id)
      setRotatedKey(null)
    },
    []
  )
  const cancelRotateAction = useCallback(() => {
    setPendingRotateAction(null)
  }, [])

  useEffect(() => {
    return () => clearTimeout(copiedTimerRef.current)
  }, [])

  const markKeyCopied = useCallback((id: number) => {
    setCopiedKeyId(id)
    clearTimeout(copiedTimerRef.current)
    copiedTimerRef.current = setTimeout(() => setCopiedKeyId(null), 2000)
  }, [])

  const triggerRefresh = useCallback(() => {
    setRefreshTrigger((prev) => prev + 1)
  }, [])

  const resolveKey = useCallback(
    async (id: number, force = false): Promise<string | null> => {
      if (!force && resolvedKeys[id]) return resolvedKeys[id]
      if (id in pendingRequests.current) return pendingRequests.current[id]

      const request = (async () => {
        setLoadingKeys((prev) => ({ ...prev, [id]: true }))
        try {
          const res = await fetchTokenKey(id)
          if (res.success && res.data?.key) {
            const fullKey = res.data.key
            setResolvedKeys((prev) => ({ ...prev, [id]: fullKey }))
            setRotatedKey({ id, key: fullKey })
            return fullKey
          }
          handleServerError(res, t(ERROR_MESSAGES.UNEXPECTED))
          return null
        } catch (error) {
          handleServerError(error, t(ERROR_MESSAGES.UNEXPECTED))
          return null
        } finally {
          delete pendingRequests.current[id]
          setLoadingKeys((prev) => {
            const next = { ...prev }
            delete next[id]
            return next
          })
        }
      })()

      pendingRequests.current[id] = request
      return request
    },
    [resolvedKeys, t]
  )

  const confirmRotateAction = useCallback(async () => {
    const action = pendingRotateAction
    const target = rotateTarget
    if (target === null) return
    const result = await rotateApiKey(target)
    if (!result.success || !result.data?.key) {
      handleServerError(result, t(ERROR_MESSAGES.UNEXPECTED))
      return
    }
    const realKey = result.data.key
    setResolvedKeys((prev) => ({ ...prev, [target]: realKey }))
    setRotatedKey({ id: target, key: realKey })
    if (!realKey) return
    if (action) {
      setPendingRotateAction(null)
      await action(realKey)
    }
  }, [pendingRotateAction, rotateTarget, t])

  const resolveRealKeysBatch = useCallback(
    async (ids: number[]): Promise<Record<number, string>> => {
      const uncachedIds = ids.filter((id) => !resolvedKeys[id])
      if (uncachedIds.length === 0) {
        const result: Record<number, string> = {}
        for (const id of ids) result[id] = resolvedKeys[id]
        return result
      }

      for (const id of uncachedIds) {
        setLoadingKeys((prev) => ({ ...prev, [id]: true }))
      }

      try {
        const res = await fetchTokenKeysBatch(uncachedIds)
        if (res.success && res.data?.keys) {
          const newKeys: Record<number, string> = {}
          for (const [idStr, key] of Object.entries(res.data.keys)) {
            newKeys[Number(idStr)] = key
          }
          setResolvedKeys((prev) => ({ ...prev, ...newKeys }))

          const result: Record<number, string> = { ...newKeys }
          for (const id of ids) {
            if (resolvedKeys[id]) result[id] = resolvedKeys[id]
          }
          return result
        }
        handleServerError(res, t(ERROR_MESSAGES.UNEXPECTED))
        return {}
      } catch (error) {
        handleServerError(error, t(ERROR_MESSAGES.UNEXPECTED))
        return {}
      } finally {
        for (const id of uncachedIds) {
          setLoadingKeys((prev) => {
            const next = { ...prev }
            delete next[id]
            return next
          })
        }
      }
    },
    [resolvedKeys, t]
  )

  return (
    <ApiKeysContext
      value={{
        open,
        setOpen,
        currentRow,
        setCurrentRow,
        refreshTrigger,
        triggerRefresh,
        resolvedKey,
        setResolvedKey,
        rotateKey: resolveKey,
        rotateTarget,
        rotatedKey,
        openRotateConfirm,
        closeRotateConfirm,
        requestRotateAction,
        confirmRotateAction,
        cancelRotateAction,
        resolveRealKeysBatch,
        resolvedKeys,
        loadingKeys,
        copiedKeyId,
        markKeyCopied,
      }}
    >
      {children}
      <RotateKeyConfirmDialog />
    </ApiKeysContext>
  )
}

/** 轮换确认与一次性明文：全局唯一一份，行内「查看」与行菜单动作共用。 */
function RotateKeyConfirmDialog() {
  const { t } = useTranslation()
  const {
    rotateTarget,
    rotatedKey,
    loadingKeys,
    confirmRotateAction,
    cancelRotateAction,
  } = useApiKeys()
  const revealedKey =
    rotateTarget !== null && rotatedKey?.id === rotateTarget
      ? rotatedKey.key
      : null
  const isLoading = rotateTarget !== null && Boolean(loadingKeys[rotateTarget])

  return (
    <ConfirmDialog
      destructive={revealedKey === null}
      open={rotateTarget !== null}
      onOpenChange={(open) => {
        if (!open) cancelRotateAction()
      }}
      title={revealedKey ? t('New key (shown once)') : t('Rotate this key?')}
      desc={t(
        'The current key stops working immediately. The new key is shown only once; copy it now.'
      )}
      confirmText={revealedKey ? t('Done') : t('Rotate')}
      isLoading={isLoading && revealedKey === null}
      handleConfirm={revealedKey ? cancelRotateAction : confirmRotateAction}
    >
      {revealedKey ? (
        <div className='space-y-2'>
          <input
            readOnly
            value={revealedKey}
            autoFocus
            onFocus={(e) => e.target.select()}
            className='bg-muted/50 w-full rounded-md border px-3 py-2 font-mono text-xs outline-none'
            aria-label={t('New key (shown once)')}
          />
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={async () => {
              const ok = await copyToClipboard(revealedKey)
              if (ok) toast.success(t('Copied'))
            }}
          >
            <Copy className='size-3.5' />
            {t('Copy')}
          </Button>
        </div>
      ) : null}
    </ConfirmDialog>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useApiKeys = () => {
  const apiKeysContext = React.useContext(ApiKeysContext)

  if (!apiKeysContext) {
    throw new Error('useApiKeys has to be used within <ApiKeysContext>')
  }

  return apiKeysContext
}
