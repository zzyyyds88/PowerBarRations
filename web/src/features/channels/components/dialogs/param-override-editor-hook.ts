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
import { useCallback, useMemo, useState, type DragEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  TEMPLATE_PRESET_CONFIG,
  type ParamOverrideCondition,
  type ParamOverrideEditorDialogProps,
  type ParamOverrideOperation,
} from './param-override-editor-constants'
import {
  buildOperationsJson,
  buildPruneObjectsValueText,
  buildReturnErrorValueText,
  createDefaultCondition,
  createDefaultOperation,
  isOperationBlank,
  normalizeOperation,
  normalizePruneRule,
  parseInitialState,
  parseLooseValue,
  parsePruneObjectsDraft,
  parseReturnErrorDraft,
  reorderOperations,
  verifyJSON,
  type PruneObjectsDraft,
  type PruneRule,
  type ReturnErrorDraft,
} from './param-override-editor-model'

export function useParamOverrideEditor(props: ParamOverrideEditorDialogProps) {
  const { t } = useTranslation()

  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [visualMode, setVisualMode] = useState<'operations' | 'legacy'>(
    'operations'
  )
  const [legacyValue, setLegacyValue] = useState('')
  const [operations, setOperations] = useState<ParamOverrideOperation[]>([
    createDefaultOperation(),
  ])
  const [jsonText, setJsonText] = useState('')
  const [jsonError, setJsonError] = useState('')
  const [operationSearch, setOperationSearch] = useState('')
  const [selectedOperationId, setSelectedOperationId] = useState('')
  const [expandedConditions, setExpandedConditions] = useState<
    Record<string, boolean>
  >({})
  const [draggedOperationId, setDraggedOperationId] = useState('')
  const [dragOverOperationId, setDragOverOperationId] = useState('')
  const [dragOverPosition, setDragOverPosition] = useState<'before' | 'after'>(
    'before'
  )
  const [templatePresetKey, setTemplatePresetKey] =
    useState('operations_default')

  // 弹窗打开或外部 value 变化时重置草稿；用渲染期调整代替 setState-in-effect。
  const initKey = props.open ? String(props.value ?? '') : null
  const [prevInitKey, setPrevInitKey] = useState<string | null>(null)
  if (initKey !== prevInitKey) {
    setPrevInitKey(initKey)
    if (initKey !== null) {
      const state = parseInitialState(props.value)
      setEditMode(state.editMode)
      setVisualMode(state.visualMode)
      setLegacyValue(state.legacyValue)
      setOperations(state.operations)
      setJsonText(state.jsonText)
      setJsonError(state.jsonError)
      setOperationSearch('')
      setSelectedOperationId(state.operations[0]?.id || '')
      setExpandedConditions({})
      setDraggedOperationId('')
      setDragOverOperationId('')
      setDragOverPosition('before')
      setTemplatePresetKey(
        state.visualMode === 'legacy' ? 'legacy_default' : 'operations_default'
      )
    }
  }

  // 保持 selectedOperationId 合法（operations 变化时）。
  const [prevOperations, setPrevOperations] = useState(operations)
  if (operations !== prevOperations) {
    setPrevOperations(operations)
    if (operations.length === 0) {
      setSelectedOperationId('')
    } else if (!operations.some((o) => o.id === selectedOperationId)) {
      setSelectedOperationId(operations[0].id)
    }
  }

  const templatePresetOptions = useMemo(
    () =>
      Object.entries(TEMPLATE_PRESET_CONFIG).map(([value, config]) => ({
        value,
        label: config.label,
      })),
    []
  )

  const operationCount = useMemo(
    () => operations.filter((o) => !isOperationBlank(o)).length,
    [operations]
  )

  const filteredOperations = useMemo(() => {
    const keyword = operationSearch.trim().toLowerCase()
    if (!keyword) return operations
    return operations.filter((op) => {
      const searchableText = [
        op.description,
        op.mode,
        op.path,
        op.from,
        op.to,
        op.value_text,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
      return searchableText.includes(keyword)
    })
  }, [operationSearch, operations])

  const selectedOperation = useMemo(
    () => operations.find((o) => o.id === selectedOperationId),
    [operations, selectedOperationId]
  )

  const selectedOperationIndex = useMemo(
    () => operations.findIndex((o) => o.id === selectedOperationId),
    [operations, selectedOperationId]
  )

  const returnErrorDraft = useMemo(() => {
    if (!selectedOperation || selectedOperation.mode !== 'return_error') {
      return null
    }
    return parseReturnErrorDraft(selectedOperation.value_text)
  }, [selectedOperation])

  const pruneObjectsDraft = useMemo(() => {
    if (!selectedOperation || selectedOperation.mode !== 'prune_objects') {
      return null
    }
    return parsePruneObjectsDraft(selectedOperation.value_text)
  }, [selectedOperation])

  const topOperationModes = useMemo(() => {
    const counts: Record<string, number> = {}
    for (const op of operations) {
      const mode = op.mode || 'set'
      counts[mode] = (counts[mode] || 0) + 1
    }
    return Object.entries(counts)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 4)
  }, [operations])

  const updateOperation = useCallback(
    (operationId: string, patch: Partial<ParamOverrideOperation>) => {
      setOperations((prev) =>
        prev.map((o) => (o.id === operationId ? { ...o, ...patch } : o))
      )
    },
    []
  )

  const addOperation = useCallback(() => {
    const created = createDefaultOperation()
    setOperations((prev) => [...prev, created])
    setSelectedOperationId(created.id)
  }, [])

  const duplicateOperation = useCallback((operationId: string) => {
    let insertedId = ''
    setOperations((prev) => {
      const idx = prev.findIndex((o) => o.id === operationId)
      if (idx < 0) return prev
      const source = prev[idx]
      const cloned = normalizeOperation({
        description: source.description,
        path: source.path,
        mode: source.mode,
        value: parseLooseValue(source.value_text),
        keep_origin: source.keep_origin,
        from: source.from,
        to: source.to,
        logic: source.logic,
        conditions: source.conditions.map((c) => ({
          path: c.path,
          mode: c.mode,
          value: parseLooseValue(c.value_text),
          invert: c.invert,
          pass_missing_key: c.pass_missing_key,
        })),
      })
      insertedId = cloned.id
      const next = [...prev]
      next.splice(idx + 1, 0, cloned)
      return next
    })
    if (insertedId) setSelectedOperationId(insertedId)
  }, [])

  const removeOperation = useCallback((operationId: string) => {
    setOperations((prev) => {
      if (prev.length <= 1) return [createDefaultOperation()]
      return prev.filter((o) => o.id !== operationId)
    })
  }, [])

  // Conditions
  const addCondition = useCallback((operationId: string) => {
    const created = createDefaultCondition()
    setOperations((prev) =>
      prev.map((op) =>
        op.id === operationId
          ? { ...op, conditions: [...op.conditions, created] }
          : op
      )
    )
    setExpandedConditions((prev) => ({ ...prev, [created.id]: true }))
  }, [])

  const updateCondition = useCallback(
    (
      operationId: string,
      conditionId: string,
      patch: Partial<ParamOverrideCondition>
    ) => {
      setOperations((prev) =>
        prev.map((op) =>
          op.id === operationId
            ? {
                ...op,
                conditions: op.conditions.map((c) =>
                  c.id === conditionId ? { ...c, ...patch } : c
                ),
              }
            : op
        )
      )
    },
    []
  )

  const removeCondition = useCallback(
    (operationId: string, conditionId: string) => {
      setOperations((prev) =>
        prev.map((op) =>
          op.id === operationId
            ? {
                ...op,
                conditions: op.conditions.filter((c) => c.id !== conditionId),
              }
            : op
        )
      )
    },
    []
  )

  // return_error draft
  const updateReturnErrorDraft = useCallback(
    (operationId: string, draftPatch: Partial<ReturnErrorDraft>) => {
      setOperations((prev) =>
        prev.map((op) => {
          if (op.id !== operationId) return op
          const draft = parseReturnErrorDraft(op.value_text)
          const nextDraft = { ...draft, ...draftPatch }
          return {
            ...op,
            value_text: buildReturnErrorValueText(nextDraft),
          }
        })
      )
    },
    []
  )

  // prune_objects draft
  const updatePruneObjectsDraft = useCallback(
    (
      operationId: string,
      updater:
        | Partial<PruneObjectsDraft>
        | ((draft: PruneObjectsDraft) => PruneObjectsDraft)
    ) => {
      setOperations((prev) =>
        prev.map((op) => {
          if (op.id !== operationId) return op
          const draft = parsePruneObjectsDraft(op.value_text)
          const nextDraft =
            typeof updater === 'function'
              ? updater(draft)
              : { ...draft, ...updater }
          return {
            ...op,
            value_text: buildPruneObjectsValueText(nextDraft),
          }
        })
      )
    },
    []
  )

  const addPruneRule = useCallback(
    (operationId: string) => {
      updatePruneObjectsDraft(operationId, (draft) => ({
        ...draft,
        simpleMode: false,
        rules: [...draft.rules, normalizePruneRule({})],
      }))
    },
    [updatePruneObjectsDraft]
  )

  const updatePruneRule = useCallback(
    (operationId: string, ruleId: string, patch: Partial<PruneRule>) => {
      updatePruneObjectsDraft(operationId, (draft) => ({
        ...draft,
        rules: draft.rules.map((r) =>
          r.id === ruleId ? { ...r, ...patch } : r
        ),
      }))
    },
    [updatePruneObjectsDraft]
  )

  const removePruneRule = useCallback(
    (operationId: string, ruleId: string) => {
      updatePruneObjectsDraft(operationId, (draft) => ({
        ...draft,
        rules: draft.rules.filter((r) => r.id !== ruleId),
      }))
    },
    [updatePruneObjectsDraft]
  )

  // Drag and drop
  const resetDragState = useCallback(() => {
    setDraggedOperationId('')
    setDragOverOperationId('')
    setDragOverPosition('before')
  }, [])

  const handleDragStart = useCallback(
    (event: DragEvent, operationId: string) => {
      setDraggedOperationId(operationId)
      setSelectedOperationId(operationId)
      event.dataTransfer.effectAllowed = 'move'
      event.dataTransfer.setData('text/plain', operationId)
    },
    []
  )

  const handleDragOver = useCallback(
    (event: DragEvent, operationId: string) => {
      event.preventDefault()
      if (!draggedOperationId || draggedOperationId === operationId) return
      const rect = event.currentTarget.getBoundingClientRect()
      const position: 'before' | 'after' =
        event.clientY - rect.top > rect.height / 2 ? 'after' : 'before'
      setDragOverOperationId(operationId)
      setDragOverPosition(position)
      event.dataTransfer.dropEffect = 'move'
    },
    [draggedOperationId]
  )

  const handleDrop = useCallback(
    (event: DragEvent, operationId: string) => {
      event.preventDefault()
      const sourceId =
        draggedOperationId || event.dataTransfer.getData('text/plain')
      const position =
        dragOverOperationId === operationId ? dragOverPosition : 'before'
      if (sourceId && operationId && sourceId !== operationId) {
        setOperations((prev) =>
          reorderOperations(prev, sourceId, operationId, position)
        )
        setSelectedOperationId(sourceId)
      }
      resetDragState()
    },
    [draggedOperationId, dragOverOperationId, dragOverPosition, resetDragState]
  )

  // Mode switching & templates
  const buildVisualJson = useCallback((): string => {
    if (visualMode === 'legacy') {
      const trimmed = legacyValue.trim()
      if (!trimmed) return ''
      if (!verifyJSON(trimmed)) {
        throw new Error(t('Parameter override must be valid JSON format'))
      }
      const parsed = JSON.parse(trimmed) as unknown
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error(t('Legacy format must be a JSON object'))
      }
      return JSON.stringify(parsed, null, 2)
    }
    return buildOperationsJson(operations, { validate: true }, t)
  }, [legacyValue, operations, t, visualMode])

  const switchToJsonMode = useCallback(() => {
    if (editMode === 'json') return
    try {
      setJsonText(buildVisualJson())
      setJsonError('')
    } catch (error) {
      toast.error((error as Error).message)
      if (visualMode === 'legacy') {
        setJsonText(legacyValue)
      } else {
        setJsonText(buildOperationsJson(operations, { validate: false }, t))
      }
      setJsonError(
        (error as Error).message || t('Parameter configuration error')
      )
    }
    setEditMode('json')
  }, [buildVisualJson, editMode, legacyValue, operations, t, visualMode])

  const switchToVisualMode = useCallback(() => {
    if (editMode === 'visual') return
    const trimmed = jsonText.trim()
    if (!trimmed) {
      const fallback = createDefaultOperation()
      setVisualMode('operations')
      setOperations([fallback])
      setSelectedOperationId(fallback.id)
      setLegacyValue('')
      setJsonError('')
      setEditMode('visual')
      return
    }
    if (!verifyJSON(trimmed)) {
      toast.error(t('Parameter override must be valid JSON format'))
      return
    }
    const parsed = JSON.parse(trimmed) as Record<string, unknown>
    if (
      parsed &&
      typeof parsed === 'object' &&
      !Array.isArray(parsed) &&
      Array.isArray(parsed.operations)
    ) {
      const nextOps =
        (parsed.operations as Record<string, unknown>[]).length > 0
          ? (parsed.operations as Record<string, unknown>[]).map(
              normalizeOperation
            )
          : [createDefaultOperation()]
      setVisualMode('operations')
      setOperations(nextOps)
      setSelectedOperationId(nextOps[0]?.id || '')
      setLegacyValue('')
      setJsonError('')
      setEditMode('visual')
      setTemplatePresetKey('operations_default')
      return
    }
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const fallback = createDefaultOperation()
      setVisualMode('legacy')
      setLegacyValue(JSON.stringify(parsed, null, 2))
      setOperations([fallback])
      setSelectedOperationId(fallback.id)
      setJsonError('')
      setEditMode('visual')
      setTemplatePresetKey('legacy_default')
      return
    }
    toast.error(t('Parameter override must be a valid JSON object'))
  }, [editMode, jsonText, t])

  const fillTemplate = useCallback(
    (mode: 'fill' | 'append') => {
      const preset =
        TEMPLATE_PRESET_CONFIG[templatePresetKey] ||
        TEMPLATE_PRESET_CONFIG.operations_default
      const payload = preset.payload as Record<string, unknown>

      if (preset.kind === 'legacy') {
        if (mode === 'append' && visualMode === 'legacy') {
          const trimmed = legacyValue.trim()
          let parsedCurrent: Record<string, unknown> = {}
          if (trimmed) {
            if (!verifyJSON(trimmed)) {
              toast.error(t('Current legacy JSON is invalid, cannot append'))
              return
            }
            parsedCurrent = JSON.parse(trimmed) as Record<string, unknown>
          }
          const merged = { ...payload, ...parsedCurrent }
          const text = JSON.stringify(merged, null, 2)
          setVisualMode('legacy')
          setLegacyValue(text)
          setOperations([createDefaultOperation()])
          setJsonText(text)
          setJsonError('')
          setEditMode('visual')
        } else {
          const text = JSON.stringify(payload, null, 2)
          setVisualMode('legacy')
          setLegacyValue(text)
          setOperations([createDefaultOperation()])
          setJsonText(text)
          setJsonError('')
          setEditMode('visual')
        }
        return
      }

      const operationsPayload = ((payload as Record<string, unknown>)
        .operations || []) as Record<string, unknown>[]

      if (mode === 'append') {
        const appended = operationsPayload.map(normalizeOperation)
        const existing =
          visualMode === 'operations'
            ? operations.filter((o) => !isOperationBlank(o))
            : []
        const nextOps = [...existing, ...appended]
        setVisualMode('operations')
        setOperations(nextOps.length > 0 ? nextOps : appended)
        setSelectedOperationId(nextOps[0]?.id || appended[0]?.id || '')
        setLegacyValue('')
        setJsonError('')
        setEditMode('visual')
        setJsonText('')
      } else {
        const nextOps = operationsPayload.map(normalizeOperation)
        const finalOps =
          nextOps.length > 0 ? nextOps : [createDefaultOperation()]
        setVisualMode('operations')
        setOperations(finalOps)
        setSelectedOperationId(finalOps[0]?.id || '')
        setJsonText(JSON.stringify({ operations: operationsPayload }, null, 2))
        setJsonError('')
        setEditMode('visual')
      }
    },
    [legacyValue, operations, templatePresetKey, visualMode, t]
  )

  const resetEditorState = useCallback(() => {
    const fallback = createDefaultOperation()
    setVisualMode('operations')
    setLegacyValue('')
    setOperations([fallback])
    setSelectedOperationId(fallback.id)
    setJsonText('')
    setJsonError('')
    setTemplatePresetKey('operations_default')
    setEditMode('visual')
  }, [])

  // JSON mode
  const handleJsonChange = useCallback(
    (nextValue: string) => {
      setJsonText(nextValue)
      const trimmed = nextValue.trim()
      if (!trimmed) {
        setJsonError('')
        return
      }
      setJsonError(verifyJSON(trimmed) ? '' : t('JSON format error'))
    },
    [t]
  )

  const visualValidationError = useMemo(() => {
    if (editMode !== 'visual') return ''
    try {
      buildVisualJson()
      return ''
    } catch (error) {
      return (error as Error)?.message || t('Parameter configuration error')
    }
  }, [buildVisualJson, editMode, t])

  // Save
  const handleSave = useCallback(() => {
    try {
      let result = ''
      if (editMode === 'json') {
        const trimmed = jsonText.trim()
        if (trimmed) {
          if (!verifyJSON(trimmed)) {
            throw new Error(t('Parameter override must be valid JSON format'))
          }
          result = JSON.stringify(JSON.parse(trimmed), null, 2)
        }
      } else {
        result = buildVisualJson()
      }
      props.onSave(result)
      props.onOpenChange(false)
    } catch (error) {
      toast.error((error as Error).message)
    }
  }, [buildVisualJson, editMode, jsonText, props, t])

  // Expand/collapse all conditions
  const expandAllConditions = useCallback(() => {
    if (!selectedOperation) return
    const map: Record<string, boolean> = {}
    for (const c of selectedOperation.conditions) map[c.id] = true
    setExpandedConditions((prev) => ({ ...prev, ...map }))
  }, [selectedOperation])

  const collapseAllConditions = useCallback(() => {
    if (!selectedOperation) return
    const map: Record<string, boolean> = {}
    for (const c of selectedOperation.conditions) map[c.id] = false
    setExpandedConditions((prev) => ({ ...prev, ...map }))
  }, [selectedOperation])

  return {
    editMode,
    visualMode,
    legacyValue,
    jsonText,
    jsonError,
    setLegacyValue,
    operations,
    operationCount,
    filteredOperations,
    selectedOperation,
    selectedOperationIndex,
    selectedOperationId,
    setSelectedOperationId,
    operationSearch,
    setOperationSearch,
    expandedConditions,
    setExpandedConditions,
    draggedOperationId,
    dragOverOperationId,
    dragOverPosition,
    templatePresetKey,
    setTemplatePresetKey,
    templatePresetOptions,
    returnErrorDraft,
    pruneObjectsDraft,
    topOperationModes,
    updateOperation,
    addOperation,
    duplicateOperation,
    removeOperation,
    addCondition,
    updateCondition,
    removeCondition,
    updateReturnErrorDraft,
    updatePruneObjectsDraft,
    addPruneRule,
    updatePruneRule,
    removePruneRule,
    resetDragState,
    handleDragStart,
    handleDragOver,
    handleDrop,
    switchToJsonMode,
    switchToVisualMode,
    fillTemplate,
    resetEditorState,
    handleJsonChange,
    visualValidationError,
    handleSave,
    expandAllConditions,
    collapseAllConditions,
  }
}

export type ParamOverrideEditorModel = ReturnType<typeof useParamOverrideEditor>
