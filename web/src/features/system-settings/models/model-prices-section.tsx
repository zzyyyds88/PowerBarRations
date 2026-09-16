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
import { Plus, RotateCcw, Save, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

// PBR 单价表（design-v1 §16.9#7，ui-spec §6.7"单价"）。
//
// 只用于成本折算展示：人民币/百万 token 的输入、输出、缓存读、缓存写四个单价。
// 存在 options 表的 PBRModelPrices 键，经 PUT /api/option/ 读写。

type ModelPriceRow = {
  id: number
  model: string
  input: string
  output: string
  cache_read: string
  cache_write: string
}

function parseRows(raw: string): ModelPriceRow[] {
  try {
    const parsed = JSON.parse(raw || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed.map((item, index) => ({
      id: index + 1,
      model: String(item?.model ?? ''),
      input: item?.input != null ? String(item.input) : '',
      output: item?.output != null ? String(item.output) : '',
      cache_read: item?.cache_read != null ? String(item.cache_read) : '',
      cache_write: item?.cache_write != null ? String(item.cache_write) : '',
    }))
  } catch {
    return []
  }
}

function toNumber(value: string): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

function serializeRows(rows: ModelPriceRow[]): string {
  const payload = rows
    .filter((row) => row.model.trim() !== '')
    .map((row) => ({
      model: row.model.trim(),
      input: toNumber(row.input),
      output: toNumber(row.output),
      cache_read: toNumber(row.cache_read),
      cache_write: toNumber(row.cache_write),
    }))
  return JSON.stringify(payload)
}

export function ModelPricesSection({
  defaultValue,
}: {
  defaultValue: string
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const initialRows = useMemo(() => parseRows(defaultValue), [defaultValue])
  const [rows, setRows] = useState<ModelPriceRow[]>(initialRows)

  const baseline = useMemo(() => serializeRows(initialRows), [initialRows])
  const dirty = serializeRows(rows) !== baseline

  const updateRow = (index: number, key: keyof ModelPriceRow, value: string) => {
    setRows((prev) =>
      prev.map((row, i) => (i === index ? { ...row, [key]: value } : row))
    )
  }

  const handleSave = () => {
    updateOption.mutate({
      key: 'PBRModelPrices',
      value: serializeRows(rows),
    })
  }

  return (
    <SettingsSection title={t('Model prices')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Cost accounting only (not billing): CNY per 1M tokens. Empty or 0 means that field is not converted.'
        )}
      </p>
      <div className='overflow-x-auto'>
        <table className='w-full min-w-[640px] text-sm'>
          <thead>
            <tr className='text-muted-foreground border-b text-left'>
              <th className='py-2 pr-2 font-medium'>{t('Model')}</th>
              <th className='py-2 pr-2 font-medium'>{t('Input')}</th>
              <th className='py-2 pr-2 font-medium'>{t('Output')}</th>
              <th className='py-2 pr-2 font-medium'>{t('Cache read')}</th>
              <th className='py-2 pr-2 font-medium'>{t('Cache write')}</th>
              <th className='w-10' />
            </tr>
          </thead>
          <tbody>
            {rows.map((row, index) => (
              <tr key={row.id} className='border-b last:border-0'>
                <td className='py-1.5 pr-2'>
                  <Input
                    aria-label={t('Model')}
                    value={row.model}
                    onChange={(event) =>
                      updateRow(index, 'model', event.target.value)
                    }
                  />
                </td>
                {(
                  ['input', 'output', 'cache_read', 'cache_write'] as const
                ).map((field) => (
                  <td key={field} className='py-1.5 pr-2'>
                    <Input
                      aria-label={t(field)}
                      inputMode='decimal'
                      value={row[field]}
                      onChange={(event) =>
                        updateRow(index, field, event.target.value)
                      }
                    />
                  </td>
                ))}
                <td className='py-1.5'>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    aria-label={t('Remove')}
                    onClick={() =>
                      setRows((prev) => prev.filter((_, i) => i !== index))
                    }
                  >
                    <Trash2 className='size-4' />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className='flex items-center gap-2'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() =>
            setRows((prev) => [
              ...prev,
              {
                id: prev.reduce((max, row) => Math.max(max, row.id), 0) + 1,
                model: '',
                input: '',
                output: '',
                cache_read: '',
                cache_write: '',
              },
            ])
          }
        >
          <Plus className='size-4' />
          {t('Add model')}
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={!dirty}
          onClick={() => setRows(initialRows)}
        >
          <RotateCcw className='size-4' />
          {t('Reset')}
        </Button>
        <Button
          type='button'
          size='sm'
          disabled={!dirty || updateOption.isPending}
          onClick={handleSave}
        >
          <Save className='size-4' />
          {t('Save changes')}
        </Button>
      </div>
    </SettingsSection>
  )
}
