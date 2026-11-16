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
import { Eye, EyeOff, Loader2, RefreshCw } from 'lucide-react'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import { FIELD_DESCRIPTIONS } from '../../constants'
import { getKeyPromptForType } from '../../lib'
import { useChannelMutateContext } from './channel-mutate-context'
import { ChannelAuthSection } from './sections'

export function ChannelAuthFields() {
  const {
    awsKeyType,
    channelId,
    currentType,
    form,
    handleRefreshCodexCredential,
    isCodexCredentialRefreshing,
    isEditing,
    isMultiKeyChannel,
    revealKey,
    sensitiveLocked,
    showKey,
    t,
  } = useChannelMutateContext()
  return (
    <ChannelAuthSection>
      <FormField
        control={form.control}
        name='key'
        render={({ field }) => {
          let keyPlaceholder = t(getKeyPromptForType(currentType))
          if (isEditing) {
            keyPlaceholder = t('Leave empty to keep existing key')
          } else if (currentType === 33 && awsKeyType === 'api_key') {
            keyPlaceholder = t('Enter API Key, format: APIKey|Region')
          } else if (currentType === 33) {
            keyPlaceholder = t(
              'Enter key, format: AccessKey|SecretAccessKey|Region'
            )
          }

          const keyDescription = isEditing
            ? t('Enter new key to update, or leave empty to keep current key')
            : t(FIELD_DESCRIPTIONS.KEY)
          return (
            <FormItem>
              <FormLabel required>{t('API Key')}</FormLabel>
              <div className='relative'>
                <FormControl>
                  <Input
                    type={showKey ? 'text' : 'password'}
                    autoComplete='off'
                    className='pr-9 font-mono'
                    placeholder={keyPlaceholder}
                    {...field}
                  />
                </FormControl>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  className='absolute top-1/2 right-1 size-7 -translate-y-1/2'
                  aria-label={showKey ? t('Hide API key') : t('Show API key')}
                  title={showKey ? t('Hide API key') : t('Show API key')}
                  disabled={sensitiveLocked}
                  onClick={revealKey}
                >
                  {showKey ? (
                    <EyeOff className='size-3.5' />
                  ) : (
                    <Eye className='size-3.5' />
                  )}
                </Button>
              </div>
              <FormDescription>
                <span className='flex flex-col gap-2'>
                  <span>{keyDescription}</span>
                  {isEditing && isMultiKeyChannel && (
                    <span className='text-warning'>
                      {t(
                        'This channel currently stores multiple keys; saving a new key replaces them with a single key.'
                      )}
                    </span>
                  )}
                </span>
              </FormDescription>
              <FormMessage />
            </FormItem>
          )
        }}
      />

      {currentType === 57 && (
        <div className='border-border/60 flex flex-col gap-3 border-y py-4'>
          <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
            <div className='text-muted-foreground text-xs'>
              {t('Codex channels use an OAuth JSON credential as the key.')}
            </div>
            <div className='flex flex-wrap items-center gap-2'>
              {isEditing && channelId && (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={handleRefreshCodexCredential}
                  disabled={sensitiveLocked || isCodexCredentialRefreshing}
                >
                  {isCodexCredentialRefreshing ? (
                    <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                  ) : (
                    <RefreshCw className='mr-2 h-4 w-4' />
                  )}
                  {isCodexCredentialRefreshing
                    ? t('Refreshing...')
                    : t('Refresh credential')}
                </Button>
              )}
            </div>
          </div>
          <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
            <AlertDescription>
              {t(
                "Disclaimer: Personal use only. Do not distribute or share any credentials. This channel has prerequisites and requires prior setup; use it only if you understand the flow and risks, and comply with OpenAI's terms and policies. Credentials and configuration are for Codex CLI integration only, and are not intended for any other client, platform, or channel."
              )}
            </AlertDescription>
          </Alert>
        </div>
      )}
    </ChannelAuthSection>
  )
}
