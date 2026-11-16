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
import { GripVertical, Plus, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'

import {
  LEGACY_TEMPLATE,
  OPERATION_MODE_LABEL_MAP,
  OPERATION_TEMPLATE,
} from './param-override-editor-constants'
import type { ParamOverrideEditorModel } from './param-override-editor-hook'
import {
  getModeTagTailwind,
  getOperationSummary,
} from './param-override-editor-model'
import { RuleEditor } from './param-override-rule-editor'

export function ParamOverrideEditorView(props: {
  editor: ParamOverrideEditorModel
}) {
  const { t } = useTranslation()
  const editor = props.editor

  return (
    <>
      {/* Toolbar */}
      <div className='bg-muted/30 border-b px-4 py-3'>
        <div className='flex flex-wrap items-center gap-2'>
          <span className='text-muted-foreground text-xs font-medium'>
            {t('Mode')}
          </span>
          <Button
            type='button'
            variant={editor.editMode === 'visual' ? 'default' : 'outline'}
            size='sm'
            onClick={editor.switchToVisualMode}
          >
            {t('Visual')}
          </Button>
          <Button
            type='button'
            variant={editor.editMode === 'json' ? 'default' : 'outline'}
            size='sm'
            onClick={editor.switchToJsonMode}
          >
            {t('JSON Text')}
          </Button>

          <div className='bg-border mx-1 h-5 w-px' />

          <span className='text-muted-foreground text-xs font-medium'>
            {t('Template')}
          </span>
          <Combobox
            options={editor.templatePresetOptions.map((o) => ({
              value: o.value,
              label: t(o.label),
            }))}
            value={editor.templatePresetKey}
            onValueChange={(v) =>
              editor.setTemplatePresetKey(v || 'operations_default')
            }
            className='h-8 w-[220px]'
          />
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => editor.fillTemplate('fill')}
          >
            {t('Fill Template')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() => editor.fillTemplate('append')}
          >
            {t('Append Template')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={editor.resetEditorState}
          >
            {t('Reset')}
          </Button>
        </div>
      </div>
      {/* Content */}
      <div className='min-h-0 flex-1 overflow-hidden'>
        {editor.editMode === 'visual' &&
          (editor.visualMode === 'legacy' ? (
            <div className='p-4'>
              <p className='text-muted-foreground mb-2 text-sm'>
                {t('Legacy Format (JSON Object)')}
              </p>
              <JsonCodeEditor
                value={editor.legacyValue}
                onChange={editor.setLegacyValue}
                placeholder={JSON.stringify(LEGACY_TEMPLATE, null, 2)}
                heightClassName='h-72 min-h-72 max-h-72'
                ariaLabel={t('Legacy Format (JSON Object)')}
              />
              <p className='text-muted-foreground mt-2 text-xs'>
                {t(
                  'Edit JSON object directly. Suitable for simple parameter overrides.'
                )}
              </p>
            </div>
          ) : (
            <div className='flex h-full'>
              {/* Left sidebar */}
              <div className='flex w-[280px] flex-shrink-0 flex-col border-r'>
                <div className='flex items-center justify-between border-b px-3 py-2'>
                  <div className='flex items-center gap-2'>
                    <span className='text-sm font-medium'>{t('Rules')}</span>
                    <Badge variant='secondary'>
                      {editor.operationCount}/{editor.operations.length}
                    </Badge>
                  </div>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={editor.addOperation}
                  >
                    <Plus className='h-4 w-4' />
                  </Button>
                </div>

                {editor.topOperationModes.length > 0 && (
                  <div className='flex flex-wrap gap-1 border-b px-3 py-2'>
                    {editor.topOperationModes.map(([mode, count]) => (
                      <span
                        key={`mode_stat_${mode}`}
                        className={cn(
                          'inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium',
                          getModeTagTailwind(mode)
                        )}
                      >
                        {t(OPERATION_MODE_LABEL_MAP[mode] || mode)} · {count}
                      </span>
                    ))}
                  </div>
                )}

                <div className='px-3 py-2'>
                  <div className='relative'>
                    <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-3.5 w-3.5' />
                    <Input
                      value={editor.operationSearch}
                      onChange={(e) =>
                        editor.setOperationSearch(e.target.value)
                      }
                      placeholder={t('Search rules...')}
                      className='h-8 pl-8 text-xs'
                    />
                  </div>
                </div>

                <ScrollArea className='flex-1'>
                  <div className='flex flex-col gap-1 px-3 pb-3'>
                    {editor.filteredOperations.length === 0 ? (
                      <p className='text-muted-foreground py-4 text-center text-xs'>
                        {t('No matching rules')}
                      </p>
                    ) : (
                      editor.filteredOperations.map((operation) => {
                        const index = editor.operations.findIndex(
                          (o) => o.id === operation.id
                        )
                        const isActive =
                          operation.id === editor.selectedOperationId
                        const isDragging =
                          operation.id === editor.draggedOperationId
                        const isDropTarget =
                          operation.id === editor.dragOverOperationId &&
                          editor.draggedOperationId !== '' &&
                          editor.draggedOperationId !== operation.id
                        return (
                          <div
                            key={operation.id}
                            role='button'
                            tabIndex={0}
                            draggable={editor.operations.length > 1}
                            onClick={() =>
                              editor.setSelectedOperationId(operation.id)
                            }
                            onDragStart={(e) =>
                              editor.handleDragStart(e, operation.id)
                            }
                            onDragOver={(e) =>
                              editor.handleDragOver(e, operation.id)
                            }
                            onDrop={(e) => editor.handleDrop(e, operation.id)}
                            onDragEnd={editor.resetDragState}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter' || e.key === ' ') {
                                e.preventDefault()
                                editor.setSelectedOperationId(operation.id)
                              }
                            }}
                            className={cn(
                              'cursor-pointer rounded-lg border p-2.5 transition-colors',
                              isActive
                                ? 'border-primary bg-primary/5'
                                : 'hover:bg-muted/50',
                              isDragging && 'opacity-50',
                              isDropTarget &&
                                editor.dragOverPosition === 'before' &&
                                'border-t-primary border-t-2',
                              isDropTarget &&
                                editor.dragOverPosition === 'after' &&
                                'border-b-primary border-b-2'
                            )}
                          >
                            <div className='flex items-start gap-2'>
                              <GripVertical
                                className={cn(
                                  'text-muted-foreground mt-0.5 h-3.5 w-3.5 flex-shrink-0',
                                  editor.operations.length > 1
                                    ? 'cursor-grab'
                                    : 'cursor-default'
                                )}
                              />
                              <div className='min-w-0 flex-1'>
                                <div className='flex items-center justify-between gap-1'>
                                  <span className='text-xs font-semibold'>
                                    #{index + 1}
                                  </span>
                                  <Badge
                                    variant='outline'
                                    className='text-[10px]'
                                  >
                                    {operation.conditions.length}
                                  </Badge>
                                </div>
                                <p className='text-muted-foreground mt-0.5 line-clamp-1 text-[11px]'>
                                  {getOperationSummary(operation, index)}
                                </p>
                                {operation.description.trim() && (
                                  <p className='text-muted-foreground mt-0.5 line-clamp-2 text-[10px]'>
                                    {operation.description}
                                  </p>
                                )}
                                <span
                                  className={cn(
                                    'mt-1 inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium',
                                    getModeTagTailwind(operation.mode || 'set')
                                  )}
                                >
                                  {t(
                                    OPERATION_MODE_LABEL_MAP[
                                      operation.mode || 'set'
                                    ] ||
                                      operation.mode ||
                                      'set'
                                  )}
                                </span>
                              </div>
                            </div>
                          </div>
                        )
                      })
                    )}
                  </div>
                </ScrollArea>
              </div>

              {/* Right panel - Rule editor */}
              <div className='flex min-w-0 flex-1 flex-col overflow-y-auto'>
                {editor.selectedOperation ? (
                  <RuleEditor
                    operation={editor.selectedOperation}
                    operationIndex={editor.selectedOperationIndex}
                    operations={editor.operations}
                    returnErrorDraft={editor.returnErrorDraft}
                    pruneObjectsDraft={editor.pruneObjectsDraft}
                    expandedConditions={editor.expandedConditions}
                    setExpandedConditions={editor.setExpandedConditions}
                    updateOperation={editor.updateOperation}
                    duplicateOperation={editor.duplicateOperation}
                    removeOperation={editor.removeOperation}
                    addCondition={editor.addCondition}
                    updateCondition={editor.updateCondition}
                    removeCondition={editor.removeCondition}
                    updateReturnErrorDraft={editor.updateReturnErrorDraft}
                    updatePruneObjectsDraft={editor.updatePruneObjectsDraft}
                    addPruneRule={editor.addPruneRule}
                    updatePruneRule={editor.updatePruneRule}
                    removePruneRule={editor.removePruneRule}
                    expandAllConditions={editor.expandAllConditions}
                    collapseAllConditions={editor.collapseAllConditions}
                  />
                ) : (
                  <div className='flex flex-1 items-center justify-center'>
                    <p className='text-muted-foreground text-sm'>
                      {t('Select a rule to edit.')}
                    </p>
                  </div>
                )}

                {editor.visualValidationError && (
                  <div className='border-t px-4 py-2'>
                    <p className='text-destructive text-xs'>
                      {editor.visualValidationError}
                    </p>
                  </div>
                )}
              </div>
            </div>
          ))}
        {/* JSON mode */}
        {editor.editMode !== 'visual' && (
          <div className='p-4'>
            <div className='mb-2 flex items-center gap-2'>
              <span className='text-muted-foreground text-xs'>
                {t('Advanced text editing')}
              </span>
            </div>
            <JsonCodeEditor
              value={editor.jsonText}
              onChange={editor.handleJsonChange}
              placeholder={JSON.stringify(OPERATION_TEMPLATE, null, 2)}
              heightClassName='h-[420px] min-h-[420px] max-h-[420px]'
              aria-invalid={Boolean(editor.jsonError)}
              ariaLabel={t('Advanced text editing')}
            />
            <p className='text-muted-foreground mt-2 text-xs'>
              {t('Edit JSON text directly. Format will be validated on save.')}
            </p>
            {editor.jsonError && (
              <p className='text-destructive mt-1 text-xs'>
                {editor.jsonError}
              </p>
            )}
          </div>
        )}
      </div>
    </>
  )
}
