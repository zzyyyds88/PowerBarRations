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
import { useMutation } from '@tanstack/react-query'
import { FlaskConical } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { TitledCard } from '@/components/ui/titled-card'

import { previewPrivacy, type PrivacyHit } from '../privacy-api'

function HitList(props: { hits: PrivacyHit[] }) {
  const { t } = useTranslation()
  if (props.hits.length === 0) {
    return (
      <p className='text-muted-foreground text-xs'>
        {t('Nothing found in this text.')}
      </p>
    )
  }
  return (
    <ul className='max-h-52 space-y-1 overflow-auto'>
      {props.hits.map((hit) => (
        <li
          key={`${hit.kind}-${hit.placeholder}`}
          className='flex flex-wrap items-baseline justify-between gap-2 rounded-md border px-2 py-1.5 text-xs'
        >
          <span className='font-mono'>
            {hit.kind} · {hit.by}
          </span>
          <span className='text-muted-foreground font-mono'>
            {hit.placeholder}
          </span>
          <span className='text-muted-foreground'>
            {hit.head ? `${hit.head}…` : ''} {hit.len} {t('characters')} ×
            {hit.n}
            {hit.action ? ` · ${hit.action}` : ''}
          </span>
        </li>
      ))}
    </ul>
  )
}

export function PrivacyPreviewCard() {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const mutation = useMutation({
    mutationFn: () => previewPrivacy(text),
  })

  return (
    <TitledCard
      title={t('Try masking')}
      description={t(
        'Preview what a vendor would receive. Nothing is saved or remembered.'
      )}
      icon={<FlaskConical className='size-4' aria-hidden='true' />}
      contentClassName='space-y-3 p-4'
    >
      <Textarea
        className='min-h-24 font-mono text-xs'
        placeholder={t('Paste a prompt, a config or a log line')}
        value={text}
        onChange={(event) => setText(event.target.value)}
      />
      <div className='flex items-center justify-between gap-2'>
        <span className='text-muted-foreground text-xs'>
          {t('Up to 64 KiB; preview never stores the text.')}
        </span>
        <Button
          type='button'
          size='sm'
          className='h-8'
          disabled={mutation.isPending || text.trim() === ''}
          onClick={() => mutation.mutate()}
        >
          {mutation.isPending ? t('Masking…') : t('Preview')}
        </Button>
      </div>
      {mutation.isError && (
        <p className='text-destructive text-xs'>
          {mutation.error instanceof Error
            ? mutation.error.message
            : t('Could not preview this text')}
        </p>
      )}
      {mutation.data && (
        <div className='space-y-2'>
          <div>
            <div className='text-muted-foreground mb-1 text-xs'>
              {t('What the vendor would receive')}
            </div>
            <pre className='bg-muted/30 max-h-48 overflow-auto rounded-md border p-2 font-mono text-xs whitespace-pre-wrap'>
              {mutation.data.text}
            </pre>
          </div>
          <HitList hits={mutation.data.hits} />
          {mutation.data.truncated && (
            <p className='text-muted-foreground text-xs'>
              {t('The text was truncated at 64 KiB.')}
            </p>
          )}
        </div>
      )}
    </TitledCard>
  )
}
