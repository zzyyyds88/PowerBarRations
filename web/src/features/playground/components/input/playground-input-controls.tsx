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
import { SendIcon, SquareIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { PromptInputButton } from '@/components/ai-elements/prompt-input'

import { getInputControlState } from '../../lib'
import type { ModelOption } from '../../types'

type PlaygroundInputControlsProps = {
  disabled?: boolean
  isGenerating?: boolean
  models: ModelOption[]
  onStop?: () => void
  text: string
  tools: ReactNode
}

export function PlaygroundInputControls({
  disabled,
  isGenerating,
  models,
  onStop,
  text,
  tools,
}: PlaygroundInputControlsProps) {
  const { t } = useTranslation()
  const { canSubmit, shouldShowStop } = getInputControlState({
    disabled,
    hasStopHandler: Boolean(onStop),
    isGenerating,
    models,
    text,
  })

  const renderSubmitButton = () =>
    shouldShowStop ? (
      <PromptInputButton
        className='border-destructive/25 bg-destructive/10 text-destructive hover:bg-destructive/15 font-medium'
        onClick={onStop}
        variant='secondary'
      >
        <SquareIcon className='fill-current' size={16} />
        <span className='hidden sm:inline'>{t('Stop')}</span>
        <span className='sr-only sm:hidden'>{t('Stop')}</span>
      </PromptInputButton>
    ) : (
      <PromptInputButton
        className='bg-primary text-primary-foreground hover:bg-primary/90 disabled:bg-muted disabled:text-muted-foreground h-8 px-3 font-medium shadow-sm'
        disabled={!canSubmit}
        type='submit'
        variant='default'
      >
        <SendIcon size={16} />
        <span className='hidden sm:inline'>{t('Send')}</span>
        <span className='sr-only sm:hidden'>{t('Send')}</span>
      </PromptInputButton>
    )

  return (
    <div className='flex w-full items-center justify-between gap-2'>
      <div className='flex min-w-0 items-center gap-2'>{tools}</div>
      <div className='flex items-center gap-2'>{renderSubmitButton()}</div>
    </div>
  )
}
