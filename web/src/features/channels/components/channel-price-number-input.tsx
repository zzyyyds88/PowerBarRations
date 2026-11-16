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
import { useState } from 'react'

import { Input } from '@/components/ui/input'

type PriceNumberInputProps = {
  label: string
  value: number | undefined
  disabled?: boolean
  onChange: (value: number | undefined) => void
}

function formatDraft(value: number | undefined): string {
  return value === undefined ? '' : String(value)
}

function parseDraft(raw: string): number | undefined {
  const trimmed = raw.trim()
  if (trimmed === '') return undefined
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined
}

/**
 * 价格数字输入（行级兜底价与 band 价共用）。
 *
 * 用草稿态保留原始键入：受控输入若在键入过程中立即归一化，键入 `0.` 会被
 * `Number("0.")` 归一成 0 并写回输入框，小数点永远打不出来（实测 bug）。
 * 聚焦期间显示草稿，失焦后回到上层归一化值；留空表示 undefined（band 价
 * = 继承行级兜底价）。
 */
export function PriceNumberInput(props: PriceNumberInputProps) {
  const [draft, setDraft] = useState<string | null>(null)

  return (
    <Input
      aria-label={props.label}
      inputMode='decimal'
      value={draft ?? formatDraft(props.value)}
      disabled={props.disabled}
      onChange={(event) => {
        setDraft(event.target.value)
        props.onChange(parseDraft(event.target.value))
      }}
      onBlur={() => setDraft(null)}
    />
  )
}
