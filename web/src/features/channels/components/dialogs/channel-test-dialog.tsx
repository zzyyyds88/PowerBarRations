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
import type { RowSelectionState } from '@tanstack/react-table'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'

import { updateChannel } from '../../api'
import { channelsQueryKeys, handleTestChannel } from '../../lib'
import type { GetChannelsResponse, SearchChannelsResponse } from '../../types'
import { useChannels } from '../channels-provider'
import { FailureDetailsSheet } from './channel-test-results'
import { ChannelTestTable } from './channel-test-table'
import {
  BATCH_TEST_CONCURRENCY,
  BATCH_TEST_DELAY_MS,
  createChannelTestCachePatch,
  endpointTypeOptions,
  getLatestChannelTestCachePatch,
  sleep,
  STREAM_INCOMPATIBLE_ENDPOINTS,
  type BatchProgress,
  type ChannelTestCachePatch,
  type ChannelTestDialogContentProps,
  type ChannelTestDialogProps,
  type FailureDetailsState,
  type ModelRow,
  type TestResult,
} from './channel-test-types'

type ChannelListCache = GetChannelsResponse | SearchChannelsResponse

export function ChannelTestDialog({
  open,
  onOpenChange,
}: ChannelTestDialogProps) {
  const { currentRow } = useChannels()

  if (!currentRow) {
    return null
  }

  return (
    <ChannelTestDialogContent
      key={currentRow.id}
      open={open}
      onOpenChange={onOpenChange}
      currentRow={currentRow}
    />
  )
}

function ChannelTestDialogContent({
  open,
  onOpenChange,
  currentRow,
}: ChannelTestDialogContentProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const currentChannelId = currentRow.id
  const batchStopRequestedRef = useRef(false)
  const batchProgressToastIdRef = useRef<ReturnType<
    typeof toast.loading
  > | null>(null)
  const [endpointType, setEndpointType] = useState('auto')
  const [isStreamTest, setIsStreamTest] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const [testResults, setTestResults] = useState<Record<string, TestResult>>({})
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})
  const [testingModels, setTestingModels] = useState<Set<string>>(
    () => new Set()
  )
  const [isBatchTesting, setIsBatchTesting] = useState(false)
  const [isBatchStopRequested, setIsBatchStopRequested] = useState(false)
  const [batchProgress, setBatchProgress] = useState<BatchProgress | null>(null)
  const [removedModels, setRemovedModels] = useState<Set<string>>(
    () => new Set()
  )
  const [isDeleteFailedDialogOpen, setIsDeleteFailedDialogOpen] =
    useState(false)
  const [isDeletingFailed, setIsDeletingFailed] = useState(false)
  const [failureDetails, setFailureDetails] =
    useState<FailureDetailsState | null>(null)
  const [pagination, setPagination] = useState({
    pageIndex: 0,
    pageSize: 30,
  })
  const endpointSelectItems = useMemo(
    () =>
      endpointTypeOptions.map((option) => ({
        value: option.value,
        label: t(option.label),
      })),
    [t]
  )

  const dismissBatchProgressToast = useCallback(() => {
    if (batchProgressToastIdRef.current === null) return

    toast.dismiss(batchProgressToastIdRef.current)
    batchProgressToastIdRef.current = null
  }, [])

  useEffect(() => {
    if (!batchProgress) {
      dismissBatchProgressToast()
      return
    }

    const title = isBatchStopRequested
      ? t('Stopping batch test...')
      : t('Batch testing models...')
    const completedText = t('{{completed}}/{{total}} completed', {
      completed: batchProgress.completed,
      total: batchProgress.total,
    })
    const resultText = t('{{success}} succeeded, {{failed}} failed', {
      success: batchProgress.success,
      failed: batchProgress.failed,
    })

    batchProgressToastIdRef.current = toast.loading(title, {
      id: batchProgressToastIdRef.current ?? undefined,
      description: `${completedText} · ${resultText}`,
    })
  }, [batchProgress, dismissBatchProgressToast, isBatchStopRequested, t])

  useEffect(() => dismissBatchProgressToast, [dismissBatchProgressToast])

  const resetState = useCallback(() => {
    batchStopRequestedRef.current = true
    setEndpointType('auto')
    setIsStreamTest(false)
    setSearchTerm('')
    setTestResults({})
    setRowSelection({})
    setTestingModels(() => new Set())
    setIsBatchTesting(false)
    setIsBatchStopRequested(false)
    setBatchProgress(null)
    setRemovedModels(() => new Set())
    setIsDeleteFailedDialogOpen(false)
    setIsDeletingFailed(false)
    setFailureDetails(null)
    setPagination({ pageIndex: 0, pageSize: 30 })
  }, [])

  const streamDisabled = STREAM_INCOMPATIBLE_ENDPOINTS.has(endpointType)
  const effectiveStreamTest = !streamDisabled && isStreamTest

  const handleEndpointTypeChange = useCallback((value: string | null) => {
    if (value === null) return

    setEndpointType(value)
    if (STREAM_INCOMPATIBLE_ENDPOINTS.has(value)) {
      setIsStreamTest(false)
    }
  }, [])

  const handleSearchTermChange = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      setSearchTerm(event.target.value)
      setPagination((prev) =>
        prev.pageIndex === 0 ? prev : { ...prev, pageIndex: 0 }
      )
    },
    []
  )

  const modelsValue = currentRow.models
  const defaultTestModel = currentRow.test_model?.trim()

  const baseModels = useMemo(() => {
    if (!modelsValue) return []
    return modelsValue
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
  }, [modelsValue])

  const models = useMemo(
    () => baseModels.filter((model) => !removedModels.has(model)),
    [baseModels, removedModels]
  )

  const successModels = useMemo(
    () => models.filter((model) => testResults[model]?.status === 'success'),
    [models, testResults]
  )

  const failedModels = useMemo(
    () => models.filter((model) => testResults[model]?.status === 'error'),
    [models, testResults]
  )

  const filteredModels = useMemo(() => {
    if (!searchTerm) return models
    const keyword = searchTerm.toLowerCase()
    return models.filter((model) => model.toLowerCase().includes(keyword))
  }, [models, searchTerm])

  const tableData = useMemo<ModelRow[]>(
    () => filteredModels.map((model) => ({ model })),
    [filteredModels]
  )

  const markModelTesting = useCallback((key: string, isTesting: boolean) => {
    setTestingModels((prev) => {
      const next = new Set(prev)
      if (isTesting) {
        next.add(key)
      } else {
        next.delete(key)
      }
      return next
    })
  }, [])

  const updateTestResult = useCallback((key: string, result: TestResult) => {
    setFailureDetails((current) => (current?.model === key ? null : current))
    setTestResults((prev) => ({
      ...prev,
      [key]: result,
    }))
  }, [])

  const updateChannelTestCache = useCallback(
    (patch?: ChannelTestCachePatch) => {
      if (!patch) return

      queryClient.setQueriesData<ChannelListCache>(
        { queryKey: channelsQueryKeys.lists() },
        (oldData) => {
          if (!oldData?.items.length) return oldData

          let changed = false
          const nextItems = oldData.items.map((channel) => {
            if (channel.id !== currentChannelId) return channel

            changed = true
            return {
              ...channel,
              response_time: patch.responseTime,
              test_time: patch.testTime,
            }
          })

          if (!changed) return oldData

          return {
            ...oldData,
            items: nextItems,
          }
        }
      )
    },
    [currentChannelId, queryClient]
  )

  const refreshChannelLists = useCallback(
    (patch?: ChannelTestCachePatch) => {
      updateChannelTestCache(patch)
      void queryClient
        .invalidateQueries({ queryKey: channelsQueryKeys.lists() })
        .then(() => updateChannelTestCache(patch))
        .catch(() => undefined)
    },
    [queryClient, updateChannelTestCache]
  )

  const testSingleModel = useCallback(
    async (
      model: string,
      silent = false,
      refreshList = true
    ): Promise<TestResult | undefined> => {
      if (!currentRow) return

      markModelTesting(model, true)
      updateTestResult(model, { status: 'testing' })
      let finalResult: TestResult | undefined

      try {
        await handleTestChannel(
          currentRow.id,
          {
            channelName: currentRow.name,
            testModel: model,
            endpointType: endpointType === 'auto' ? undefined : endpointType,
            stream: effectiveStreamTest || undefined,
            silent,
          },
          (success, responseTime, error, errorCode) => {
            const completedAt = Date.now()
            finalResult = {
              status: success ? 'success' : 'error',
              responseTime,
              completedAt,
              error,
              errorCode,
            }
            updateTestResult(model, finalResult)
          }
        )
      } catch (error: unknown) {
        finalResult = {
          status: 'error',
          completedAt: Date.now(),
          error: error instanceof Error ? error.message : t('Test failed'),
        }
        updateTestResult(model, finalResult)
      } finally {
        markModelTesting(model, false)
        if (refreshList) {
          refreshChannelLists(
            createChannelTestCachePatch(
              finalResult?.responseTime,
              finalResult?.completedAt
            )
          )
        }
      }
      return finalResult
    },
    [
      currentRow,
      endpointType,
      effectiveStreamTest,
      markModelTesting,
      refreshChannelLists,
      t,
      updateTestResult,
    ]
  )

  const handleStopBatchTest = useCallback(() => {
    if (!isBatchTesting || isBatchStopRequested) return

    batchStopRequestedRef.current = true
    setIsBatchStopRequested(true)
  }, [isBatchStopRequested, isBatchTesting])

  const handleBatchTest = useCallback(
    async (modelsToTest: string[]) => {
      const uniqueModels = [
        ...new Set(modelsToTest.map((model) => model.trim()).filter(Boolean)),
      ]
      if (!uniqueModels.length) return

      batchStopRequestedRef.current = false
      setIsBatchTesting(true)
      setIsBatchStopRequested(false)
      setBatchProgress({
        total: uniqueModels.length,
        completed: 0,
        success: 0,
        failed: 0,
      })

      let resultPatch: ChannelTestCachePatch | undefined
      const results: TestResult[] = []
      let completedCount = 0
      let successCount = 0
      let failedCount = 0

      try {
        const createFallbackResult = (error?: unknown): TestResult => ({
          status: 'error',
          completedAt: Date.now(),
          error: error instanceof Error ? error.message : t('Test failed'),
        })

        const recordBatchResult = (result: TestResult) => {
          results.push(result)
          completedCount += 1
          if (result.status === 'success') {
            successCount += 1
          }
          failedCount = completedCount - successCount

          setBatchProgress({
            total: uniqueModels.length,
            completed: completedCount,
            success: successCount,
            failed: failedCount,
          })
        }

        for (
          let startIndex = 0;
          startIndex < uniqueModels.length;
          startIndex += BATCH_TEST_CONCURRENCY
        ) {
          if (batchStopRequestedRef.current) {
            break
          }

          const batch = uniqueModels.slice(
            startIndex,
            startIndex + BATCH_TEST_CONCURRENCY
          )
          const batchPromises = batch.map(async (modelName) => {
            try {
              const result = await testSingleModel(modelName, true, false)
              const finalResult = result ?? createFallbackResult()
              if (!result) {
                updateTestResult(modelName, finalResult)
              }
              recordBatchResult(finalResult)
              return finalResult
            } catch (error: unknown) {
              const fallbackResult = createFallbackResult(error)
              updateTestResult(modelName, fallbackResult)
              recordBatchResult(fallbackResult)
              return fallbackResult
            }
          })

          await Promise.allSettled(batchPromises)

          if (
            batchStopRequestedRef.current ||
            startIndex + BATCH_TEST_CONCURRENCY >= uniqueModels.length
          ) {
            break
          }

          await sleep(BATCH_TEST_DELAY_MS)
        }

        resultPatch = getLatestChannelTestCachePatch(results)
        const stopped =
          batchStopRequestedRef.current && completedCount < uniqueModels.length

        dismissBatchProgressToast()
        if (stopped) {
          toast.info(
            t(
              'Batch test stopped: {{completed}}/{{total}} completed, {{success}} succeeded, {{failed}} failed',
              {
                completed: completedCount,
                total: uniqueModels.length,
                success: successCount,
                failed: failedCount,
              }
            )
          )
        } else if (failedCount > 0) {
          toast.error(
            t(
              'Batch test completed: {{success}} succeeded, {{failed}} failed',
              {
                success: successCount,
                failed: failedCount,
              }
            )
          )
        } else {
          toast.success(
            t('Batch test completed: {{count}} succeeded', {
              count: successCount,
            })
          )
        }
      } finally {
        batchStopRequestedRef.current = false
        setIsBatchTesting(false)
        setIsBatchStopRequested(false)
        setBatchProgress(null)
        setRowSelection({})
        refreshChannelLists(resultPatch)
      }
    },
    [
      dismissBatchProgressToast,
      refreshChannelLists,
      t,
      testSingleModel,
      updateTestResult,
    ]
  )

  const handleSelectSuccessfulModels = useCallback(() => {
    setRowSelection(() => {
      const next: RowSelectionState = {}
      for (const model of successModels) {
        next[model] = true
      }
      return next
    })
  }, [successModels])

  const handleDeleteFailedModels = useCallback(async () => {
    const failed = models.filter(
      (model) => testResults[model]?.status === 'error'
    )
    if (!failed.length) {
      setIsDeleteFailedDialogOpen(false)
      return
    }

    const failedSet = new Set(failed)
    const remaining = models.filter((model) => !failedSet.has(model))

    setIsDeletingFailed(true)
    try {
      const response = await updateChannel(currentRow.id, {
        models: remaining.join(','),
      })
      if (response.success) {
        setRemovedModels((prev) => {
          const next = new Set(prev)
          for (const model of failed) next.add(model)
          return next
        })
        setTestResults((prev) => {
          const next = { ...prev }
          for (const model of failed) delete next[model]
          return next
        })
        setRowSelection((prev) => {
          const next = { ...prev }
          for (const model of failed) delete next[model]
          return next
        })
        toast.success(
          t('Deleted {{count}} failed models', { count: failed.length })
        )
        refreshChannelLists()
        setIsDeleteFailedDialogOpen(false)
      } else {
        handleServerError(response, t('Failed to delete failed models'))
      }
    } catch (error: unknown) {
      handleServerError(error, t('Failed to delete failed models'))
    } finally {
      setIsDeletingFailed(false)
    }
  }, [currentRow.id, models, refreshChannelLists, t, testResults])

  const handleClose = useCallback(() => {
    resetState()
    onOpenChange(false)
  }, [onOpenChange, resetState])

  const handleDialogOpenChange = useCallback(
    (nextOpen: boolean) => {
      if (!nextOpen) {
        handleClose()
      }
    },
    [handleClose]
  )

  const isAnyTesting = testingModels.size > 0 || isBatchTesting
  const isFilteringModels = searchTerm.trim().length > 0
  const testAllButtonLabel = isFilteringModels
    ? t('Test {{count}} matching models', { count: filteredModels.length })
    : t('Test all {{count}} models', { count: filteredModels.length })

  return (
    <>
      <Dialog
        size='lg'
        open={open}
        onOpenChange={handleDialogOpenChange}
        title={
          <span className='inline-flex max-w-full min-w-0 items-center gap-1.5'>
            <span className='shrink-0'>{t('Test Channel Connection')}:</span>
            <span className='min-w-0 truncate'>{currentRow.name}</span>
          </span>
        }
        contentClassName='max-h-[90vh] overflow-hidden sm:max-w-4xl'
        bodyClassName='space-y-4'
        footer={
          <Button variant='outline' onClick={handleClose}>
            {t('Close')}
          </Button>
        }
      >
        <div className='max-h-[78vh] space-y-4 overflow-y-auto py-4 pr-1'>
          <div className='grid gap-4 md:grid-cols-2'>
            <div className='grid gap-2'>
              <Label htmlFor='endpoint-type'>{t('Endpoint Type')}</Label>
              <Combobox
                options={endpointSelectItems}
                value={endpointType}
                onValueChange={handleEndpointTypeChange}
                id='endpoint-type'
                className='w-full min-w-0'
                placeholder={t('Auto detect (default)')}
              />
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Override the endpoint used for testing. Leave empty to auto detect.'
                )}
              </p>
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='stream-toggle'>{t('Stream Mode')}</Label>
              <div className='flex items-center gap-2'>
                <Switch
                  id='stream-toggle'
                  checked={effectiveStreamTest}
                  onCheckedChange={setIsStreamTest}
                  disabled={streamDisabled}
                />
                <span className='text-sm'>
                  {effectiveStreamTest ? t('Enabled') : t('Disabled')}
                </span>
              </div>
              <p className='text-muted-foreground text-xs'>
                {t('Enable streaming mode for the test request.')}
              </p>
            </div>
          </div>

          <ChannelTestTable
            models={models}
            filteredModels={filteredModels}
            successModels={successModels}
            failedModels={failedModels}
            tableData={tableData}
            testResults={testResults}
            testingModels={testingModels}
            isBatchTesting={isBatchTesting}
            isBatchStopRequested={isBatchStopRequested}
            isAnyTesting={isAnyTesting}
            searchTerm={searchTerm}
            defaultTestModel={defaultTestModel ?? ''}
            testAllButtonLabel={testAllButtonLabel}
            rowSelection={rowSelection}
            setRowSelection={setRowSelection}
            pagination={pagination}
            setPagination={setPagination}
            onSearchChange={handleSearchTermChange}
            onBatchTest={handleBatchTest}
            onStopBatchTest={handleStopBatchTest}
            onSelectSuccessful={handleSelectSuccessfulModels}
            onDeleteFailedPrompt={() => setIsDeleteFailedDialogOpen(true)}
            onTestSingle={testSingleModel}
            onOpenDetails={setFailureDetails}
          />
        </div>
      </Dialog>
      <ConfirmDialog
        open={isDeleteFailedDialogOpen}
        onOpenChange={setIsDeleteFailedDialogOpen}
        title={t('Delete failed models')}
        desc={t(
          'This removes {{count}} failed models from this channel. This action cannot be undone.',
          { count: failedModels.length }
        )}
        destructive
        isLoading={isDeletingFailed}
        confirmText={t('Delete')}
        handleConfirm={handleDeleteFailedModels}
      />
      <FailureDetailsSheet
        details={failureDetails}
        onOpenChange={(sheetOpen) => {
          if (!sheetOpen) {
            setFailureDetails(null)
          }
        }}
      />
    </>
  )
}
