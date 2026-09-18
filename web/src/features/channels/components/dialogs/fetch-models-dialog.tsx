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
import { useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { fetchUpstreamModels, updateChannel } from '../../api'
import { channelsQueryKeys, normalizeModelName } from '../../lib'
import { UpstreamModelSelection } from '../upstream-model-selection'

function normalizeModelNameList(models: readonly string[]): string[] {
  return [
    ...new Set(
      models.map((model) => normalizeModelName(model)).filter(Boolean)
    ),
  ]
}

type FetchModelsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /**
   * 自定义拉取器。传入后由调用方决定从哪拉（例如未保存渠道用草稿连接信息
   * POST /api/channels/batch/fetch-models）；不传则按 `channelId` 拉已保存渠道。
   */
  customFetcher?: () => Promise<string[]>
  /**
   * 传入即进入**表单回填模式**：Save Models 只把勾选结果交给调用方（回填渠道
   * 表单），不写库。不传则要求 `channelId`，Save 直接 PUT /api/channel/ 回写。
   */
  onModelsSelected?: (models: string[]) => void
  /** 已保存渠道的数字 id；表单回填模式下仅用于展示。 */
  channelId?: number | null
  /** 渠道名，仅用于弹窗描述。 */
  channelName?: string | null
  /** 当前渠道模型清单，驱动「新增/已存在/已移除」分类。 */
  existingModels?: string[]
  /** 模型重定向的目标模型（上游未返回也保留）。 */
  redirectModels?: string[]
  /** 模型重定向的来源别名（不参与已移除分类）。 */
  redirectSourceModels?: string[]
}

// 「获取模型列表」弹窗（New API 形态，ui-spec §6.4）：打开即自动拉取上游清单，
// 勾选后 Save Models 回填表单（表单模式）或直接回写渠道（独立模式）。
//
// 与 ConfigureModelsDialog 的分工：后者用于抽屉里「探测上游模型」按钮的草稿
// 探测结果；本弹窗是 models 字段旁的显式入口，自带拉取动作。
export function FetchModelsDialog(props: FetchModelsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [isFetching, setIsFetching] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [fetchedModels, setFetchedModels] = useState<string[]>([])
  const [candidateModels, setCandidateModels] = useState<string[]>([])
  const [selectedModels, setSelectedModels] = useState<string[]>([])

  const existingModels = useMemo(
    () => normalizeModelNameList(props.existingModels ?? []),
    [props.existingModels]
  )
  const canFetch = Boolean(props.customFetcher) || props.channelId != null

  const fetchedModelSet = new Set(normalizeModelNameList(fetchedModels))
  const redirectSourceSet = new Set(
    normalizeModelNameList(props.redirectSourceModels ?? [])
  )
  const hasUnlistedModels = normalizeModelNameList([
    ...candidateModels,
    ...selectedModels,
  ]).some(
    (model) => !fetchedModelSet.has(model) && !redirectSourceSet.has(model)
  )

  const handleFetchModels = async () => {
    if (!props.customFetcher && props.channelId == null) return
    setIsFetching(true)
    setError(null)
    try {
      // 失败由 axios 以真实状态码拒绝（api-spec §3），成功即裸模型名数组。
      const list = props.customFetcher
        ? await props.customFetcher()
        : await fetchUpstreamModels(props.channelId as number)
      const normalized = normalizeModelNameList(list)
      setFetchedModels(normalized)
      setCandidateModels(existingModels)
      setSelectedModels(existingModels)
      toast.success(t('Fetched {{count}} models', { count: normalized.length }))
    } catch (fetchError) {
      setError(fetchError)
      setFetchedModels([])
      setCandidateModels([])
      setSelectedModels(existingModels)
      handleServerError(fetchError, t('Failed to fetch models'))
    } finally {
      setIsFetching(false)
    }
  }

  // 打开即拉取。依赖只取 open + 渠道身份：customFetcher 由调用方 memo 化，
  // 避免因内联函数每次渲染换新而反复拉取。
  useEffect(() => {
    if (!props.open) return
    if (!props.customFetcher && props.channelId == null) return
    void handleFetchModels()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.open, props.customFetcher, props.channelId])

  const handleClose = () => {
    setFetchedModels([])
    setCandidateModels([])
    setSelectedModels([])
    setError(null)
    props.onOpenChange(false)
  }

  const handleSave = async () => {
    // 表单回填模式：把勾选结果交给调用方，由渠道表单统一提交。
    if (props.onModelsSelected) {
      props.onModelsSelected(selectedModels)
      toast.success(t('Models filled to form'))
      handleClose()
      return
    }

    // 独立模式：直接回写已保存渠道（成功体是裸渠道对象）。
    if (props.channelId == null) return
    setIsSaving(true)
    try {
      await updateChannel(props.channelId, { models: selectedModels.join(',') })
      toast.success(t('Models updated successfully'))
      queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
      handleClose()
    } catch (saveError) {
      handleServerError(saveError, t('Failed to update models'))
    } finally {
      setIsSaving(false)
    }
  }

  const showFooterActions =
    canFetch &&
    !isFetching &&
    error == null &&
    (fetchedModels.length > 0 || hasUnlistedModels)

  let dialogDescription: ReactNode = t('Fetch available models from upstream')
  if (props.channelName) {
    dialogDescription = (
      <>
        {t('Channel:')} <strong>{props.channelName}</strong>
      </>
    )
  }

  let dialogBody: ReactNode
  if (!canFetch) {
    dialogBody = (
      <div className='text-muted-foreground py-8 text-center'>
        {t('No channel selected')}
      </div>
    )
  } else if (isFetching) {
    dialogBody = (
      <div className='flex items-center justify-center py-12'>
        <Loader2
          className='text-muted-foreground h-8 w-8 animate-spin'
          aria-hidden='true'
        />
      </div>
    )
  } else if (error != null) {
    dialogBody = (
      <ErrorState
        className='min-h-0 p-3'
        title={t('Failed to fetch models')}
        description={getServerErrorMessage(error, t('Failed to fetch models'))}
        onRetry={() => {
          void handleFetchModels()
        }}
      />
    )
  } else if (fetchedModels.length === 0 && !hasUnlistedModels) {
    dialogBody = (
      <div className='text-muted-foreground py-8 text-center'>
        <p>{t('No models fetched yet.')}</p>
        <Button
          className='mt-4'
          onClick={handleFetchModels}
          disabled={isFetching}
        >
          {t('Fetch Models')}
        </Button>
      </div>
    )
  } else {
    dialogBody = (
      <UpstreamModelSelection
        models={fetchedModels}
        selected={selectedModels}
        onChange={setSelectedModels}
        existingModels={existingModels}
        redirectModels={props.redirectModels}
        redirectSourceModels={props.redirectSourceModels}
      />
    )
  }

  return (
    <Dialog
      size='xl'
      open={props.open}
      onOpenChange={handleClose}
      title={t('Fetch Models')}
      description={dialogDescription}
      bodyClassName='space-y-4'
      footer={
        showFooterActions ? (
          <>
            <Button variant='outline' onClick={handleClose} disabled={isSaving}>
              {t('Cancel')}
            </Button>
            <Button onClick={handleSave} disabled={isSaving}>
              {isSaving && (
                <Loader2
                  className='mr-2 h-4 w-4 animate-spin'
                  aria-hidden='true'
                />
              )}
              {isSaving ? t('Saving...') : t('Save Models')}
            </Button>
          </>
        ) : null
      }
    >
      {dialogBody}
    </Dialog>
  )
}
