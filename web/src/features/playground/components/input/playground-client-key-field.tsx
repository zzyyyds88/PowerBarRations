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
import { Link } from '@tanstack/react-router'
import { KeyRoundIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { ClientKeyOption } from '../../types'

interface PlaygroundClientKeyFieldProps {
  value: string
  options: ClientKeyOption[]
  disabled?: boolean
  isLoading?: boolean
  onChange: (value: string) => void
}

/**
 * 试打台的客户端密钥输入。
 *
 * 模型面只认客户端密钥（token-spec §1），管理面会话对 `/v1/*` 无效，
 * 因此这里必须由使用者提供 `pbr-...`。密钥只存本机 sessionStorage。
 */
export function PlaygroundClientKeyField(props: PlaygroundClientKeyFieldProps) {
  const { t } = useTranslation()
  const selected = props.options.find((option) => option.value === props.value)

  return (
    <div className='flex flex-col gap-1.5 px-1 md:flex-row md:items-center md:gap-2'>
      <Label
        className='text-muted-foreground flex shrink-0 items-center gap-1 text-xs'
        htmlFor='playground-client-key'
      >
        <KeyRoundIcon aria-hidden='true' size={12} />
        {t('Client Key')}
      </Label>
      <Select
        value={selected ? String(selected.id) : ''}
        onValueChange={(id) => {
          const option = props.options.find((item) => String(item.id) === id)
          if (option) props.onChange(option.value)
        }}
        disabled={
          props.disabled || props.isLoading || props.options.length === 0
        }
      >
        <SelectTrigger
          id='playground-client-key'
          aria-label={t('Client Key')}
          className='h-8 w-full max-w-sm text-xs'
        >
          <SelectValue
            placeholder={
              props.isLoading ? t('Loading...') : t('Select a client key')
            }
          >
            {selected ? (
              <span className='flex min-w-0 items-center gap-2'>
                <span className='truncate'>{selected.label}</span>
                <span className='text-muted-foreground shrink-0 font-mono text-[11px]'>
                  {selected.prefix}…
                </span>
              </span>
            ) : null}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          {props.options.map((option) => (
            <SelectItem key={option.id} value={String(option.id)}>
              <span className='flex min-w-0 items-center gap-2'>
                <span className='truncate'>{option.label}</span>
                <span className='text-muted-foreground font-mono text-[11px]'>
                  {option.prefix}…
                </span>
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {props.options.length === 0 && !props.isLoading ? (
        <p className='text-muted-foreground text-xs'>
          {t(
            'No API keys available. Create your first API key to get started.'
          )}{' '}
          <Link className='text-primary hover:underline' to='/keys'>
            {t('API Keys')}
          </Link>
        </p>
      ) : null}
    </div>
  )
}
