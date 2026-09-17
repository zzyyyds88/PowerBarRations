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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import { normalizeModelName } from '../../lib'
import { UpstreamModelSelection } from '../upstream-model-selection'

// 探测选择弹窗（ui-spec §6.4 / §6.9）：上游探测成功后居中弹出，候选以勾选列表
// 呈现（含当前已选、上游未再返回的草稿名，便于同处增删），点「应用」才回填渠道
// 模型清单，取消/关闭不写入。打开时以当前已选模型为初始勾选；existingModels 供
// 列表把候选项标注为"新增/已存在"，并驱动「仅加入新增」。
type ConfigureModelsDialogProps = {
  open: boolean
  /** Upstream candidates returned by discovery. */
  candidates: string[]
  /** Models currently in the channel list; checked on open. */
  selectedModels: string[]
  /** Saved models used to classify candidates as new vs existing. */
  existingModels: string[]
  redirectModels?: string[]
  redirectSourceModels?: string[]
  onOpenChange: (open: boolean) => void
  onApply: (models: string[]) => void
}

export function ConfigureModelsDialog(props: ConfigureModelsDialogProps) {
  const { t } = useTranslation()
  // Mounted for each opening, so the initial selection and unchecked
  // candidates persist for the whole dialog session.
  const [initialSelected] = useState(() =>
    [...new Set(props.selectedModels.map(normalizeModelName).filter(Boolean))]
  )
  const [selected, setSelected] = useState(initialSelected)

  const candidates = useMemo(
    () => [...new Set(props.candidates.map(normalizeModelName).filter(Boolean))],
    [props.candidates]
  )
  // The picker lists every upstream candidate plus the models already selected
  // (including draft-only custom names the upstream no longer returns), so the
  // user can both add and remove from the same checklist.
  const selectionModels = useMemo(
    () => [...new Set([...candidates, ...initialSelected])],
    [candidates, initialSelected]
  )
  const existingSet = useMemo(
    () =>
      new Set(props.existingModels.map(normalizeModelName).filter(Boolean)),
    [props.existingModels]
  )
  const newCandidates = useMemo(
    () => candidates.filter((model) => !existingSet.has(model)),
    [candidates, existingSet]
  )

  const addAll = () => {
    setSelected([...new Set([...selected, ...candidates])])
  }
  const addNewOnly = () => {
    setSelected([...new Set([...selected, ...newCandidates])])
  }

  return (
    <Dialog
      size='xl'
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Select upstream models')}
      description={t(
        'Found {{count}} upstream models · {{added}} new · {{existing}} existing',
        {
          count: candidates.length,
          added: newCandidates.length,
          existing: candidates.length - newCandidates.length,
        }
      )}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={() => {
              props.onApply(selected)
              props.onOpenChange(false)
            }}
          >
            {t('Apply')}
          </Button>
        </>
      }
    >
      <div className='space-y-3'>
        <div className='flex flex-wrap items-center justify-end gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={candidates.length === 0}
            onClick={addAll}
          >
            {t('Add all')}
          </Button>
          {newCandidates.length > 0 && (
            <Button
              type='button'
              variant='secondary'
              size='sm'
              onClick={addNewOnly}
            >
              {t('Add new only')}
            </Button>
          )}
        </div>
        {/* A single flat checklist: counts are summarized in the header and the
            batch actions above, so the old New/Existing/Removed tabs are not
            needed here. */}
        <UpstreamModelSelection
          key='discovery'
          models={selectionModels}
          selected={selected}
          existingModels={props.existingModels}
          redirectModels={props.redirectModels}
          redirectSourceModels={props.redirectSourceModels}
          onChange={setSelected}
          showChanges={false}
        />
      </div>
    </Dialog>
  )
}
