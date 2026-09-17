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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { t as i18nT } from 'i18next'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { CopyButton } from '@/components/copy-button'
import { LanguageSwitcher } from '@/components/language-switcher'
import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { useSystemConfig } from '@/hooks/use-system-config'

import { deriveAdminKey, submitSetup } from './api'

/**
 * PBR 首启初始化。
 *
 * 与上游 new-api 的四步向导（数据库/管理员/使用模式/复核）不同，PBR 是单文件
 * SQLite + 单口令网关：这里只要求设置一个登录口令，提交后服务端签发会话，
 * 浏览器直接进入控制台。
 *
 * 提交成功后**不立即跳转**：先展示一次由口令派生的管理密钥
 * Base64(SHA256(口令))（token-spec §2.1，标准 Base64 带填充），供 AI/脚本使用。
 * 该密钥仅在此处展示一次，**不写入 localStorage**；用户可复制或直接跳过。
 *
 * 刻意**不限制口令长度**（token-spec §2）：即便很短也允许，只在过短时给出提示。
 */
const setupSchema = z
  .object({
    password: z.string().min(1, {
      error: () => i18nT('Please set a login password'),
    }),
    confirmPassword: z.string().min(1, {
      error: () => i18nT('Please enter the login password again'),
    }),
  })
  .refine((values) => values.password === values.confirmPassword, {
    error: () => i18nT('The two passwords do not match'),
    path: ['confirmPassword'],
  })

export function SetupWizard() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { systemName } = useSystemConfig({ autoLoad: true })
  const [setupComplete, setSetupComplete] = useState(false)
  const [adminKey, setAdminKey] = useState<string | null>(null)

  const form = useForm<z.infer<typeof setupSchema>>({
    resolver: zodResolver(setupSchema),
    defaultValues: { password: '', confirmPassword: '' },
  })

  const goToConsole = () => {
    void navigate({ href: '/dashboard', replace: true })
  }

  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof setupSchema>) => submitSetup(values),
    onSuccess: async (res, values) => {
      if (!res.success) {
        toast.error(res.message || t('Setup failed'))
        return
      }
      if (res.message) toast.warning(res.message)
      toast.success(t('Setup complete'))
      // 服务端已签发会话 Cookie；派生并展示一次性管理密钥，等待用户复制/跳过。
      const derived = await deriveAdminKey(values.password)
      setAdminKey(derived)
      setSetupComplete(true)
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  // 用 useWatch 而非 form.watch：后者与 React Compiler 不兼容（lint 规则）。
  const password = useWatch({ control: form.control, name: 'password' })

  return (
    <div className='relative flex min-h-svh flex-col items-center justify-center gap-6 p-6'>
      <div className='absolute top-4 right-4'>
        <LanguageSwitcher />
      </div>

      {setupComplete ? (
        <Card className='w-full max-w-md'>
          <CardHeader>
            <CardTitle>{t('Setup complete')}</CardTitle>
            <CardDescription>{t('Your admin key')}</CardDescription>
          </CardHeader>
          <CardContent className='space-y-4'>
            <p className='text-muted-foreground text-xs'>
              {t(
                'This is the admin key for AI clients and scripts. It is shown only once and is never stored in the browser.'
              )}
            </p>

            {adminKey ? (
              <div className='flex items-center gap-2'>
                <Input
                  readOnly
                  value={adminKey}
                  onFocus={(event) => event.target.select()}
                  className='font-mono text-xs'
                  aria-label={t('Admin key')}
                />
                <CopyButton
                  value={adminKey}
                  tooltip={t('Copy admin key')}
                  aria-label={t('Copy admin key')}
                />
              </div>
            ) : (
              <p className='text-warning text-xs'>
                {t(
                  'This browser cannot derive the admin key (Web Crypto is unavailable). Recompute it as described below.'
                )}
              </p>
            )}

            <div className='space-y-1'>
              <p className='text-sm font-medium'>{t('How to recompute')}</p>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'It is Base64(SHA256(login password)), standard Base64 with padding. Recompute it whenever you need it.'
                )}
              </p>
            </div>
          </CardContent>
          <CardFooter>
            <Button className='w-full' onClick={goToConsole}>
              {t('Continue to console')}
            </Button>
          </CardFooter>
        </Card>
      ) : (
        <Card className='w-full max-w-md'>
          <CardHeader>
            <CardTitle>
              {t('Initialize {{name}}', {
                name: systemName || 'PowerBarRations',
              })}
            </CardTitle>
            <CardDescription>
              {t(
                'Set the login password for this gateway. It is the only credential; keep it safe.'
              )}
            </CardDescription>
          </CardHeader>

          <Form {...form}>
            <form
              onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
            >
              <CardContent className='space-y-4'>
                <FormField
                  control={form.control}
                  name='password'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Login password')}</FormLabel>
                      <FormControl>
                        <PasswordInput
                          autoComplete='new-password'
                          placeholder='••••••••'
                          {...field}
                        />
                      </FormControl>
                      {password.length > 0 && password.length < 8 && (
                        <FormDescription className='text-warning'>
                          {t(
                            'This password is very short. Length is allowed, but a longer one is safer.'
                          )}
                        </FormDescription>
                      )}
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='confirmPassword'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Confirm password')}</FormLabel>
                      <FormControl>
                        <PasswordInput
                          autoComplete='new-password'
                          placeholder='••••••••'
                          {...field}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <p className='text-muted-foreground text-xs'>
                  {t(
                    'The admin key used by AI clients is derived from this password and changes when you change it.'
                  )}
                </p>
              </CardContent>

              <CardFooter>
                <Button
                  type='submit'
                  className='w-full'
                  disabled={mutation.isPending}
                >
                  {mutation.isPending && (
                    <Loader2 className='size-4 animate-spin' />
                  )}
                  {t('Initialize')}
                </Button>
              </CardFooter>
            </form>
          </Form>
        </Card>
      )}
    </div>
  )
}
