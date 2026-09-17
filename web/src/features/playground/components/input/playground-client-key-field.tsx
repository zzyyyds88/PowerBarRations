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
import { KeyRoundIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface PlaygroundClientKeyFieldProps {
  value: string
  disabled?: boolean
  onChange: (value: string) => void
}

/**
 * 试打台的客户端密钥输入。
 *
 * 模型面只认客户端密钥（token-spec §1），管理面会话对 `/v1/*` 无效，
 * 因此这里必须由使用者提供 `pbr-...`。密钥只存本机 localStorage。
 */
export function PlaygroundClientKeyField(props: PlaygroundClientKeyFieldProps) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-1.5 px-1 md:flex-row md:items-center md:gap-2'>
      <Label
        className='text-muted-foreground flex shrink-0 items-center gap-1 text-xs'
        htmlFor='playground-client-key'
      >
        <KeyRoundIcon aria-hidden='true' size={12} />
        {t('Client Key')}
      </Label>
      <Input
        autoComplete='off'
        className='h-8 max-w-sm text-xs'
        disabled={props.disabled}
        id='playground-client-key'
        onChange={(event) => props.onChange(event.target.value)}
        placeholder={t('pbr-...')}
        spellCheck={false}
        type='password'
        value={props.value}
      />
      <p className='text-muted-foreground text-xs'>
        {t(
          'The playground calls /v1/chat/completions with this client key. Create or rotate it on the Keys page.'
        )}
      </p>
    </div>
  )
}
