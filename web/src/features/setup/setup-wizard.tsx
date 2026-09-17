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
import { Loader2 } from 'lucide-react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

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
import { useSystemConfig } from '@/hooks/use-system-config'

import { submitSetup } from './api'

/**
 * PBR 首启初始化。
 *
 * 与上游 new-api 的四步向导（数据库/管理员/使用模式/复核）不同，PBR 是单文件
 * SQLite + 单口令网关：这里只要求设置一个登录口令，提交后服务端签发会话，
 * 浏览器直接进入控制台。
 *
 * 刻意**不限制口令长度**（token-spec §2）：即便很短也允许，只在过短时给出提示。
 */
const setupSchema = z
  .object({
    password: z.string().min(1, '请设置登录口令'),
    confirmPassword: z.string().min(1, '请再次输入登录口令'),
  })
  .refine((values) => values.password === values.confirmPassword, {
    message: '两次输入的口令不一致',
    path: ['confirmPassword'],
  })

export function SetupWizard() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { systemName } = useSystemConfig({ autoLoad: true })

  const form = useForm<z.infer<typeof setupSchema>>({
    resolver: zodResolver(setupSchema),
    defaultValues: { password: '', confirmPassword: '' },
  })

  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof setupSchema>) => submitSetup(values),
    onSuccess: async (res) => {
      if (!res.success) {
        toast.error(res.message || t('Setup failed'))
        return
      }
      if (res.message) toast.warning(res.message)
      toast.success(t('Setup complete'))
      // 服务端已签发会话 Cookie，直接进入控制台。
      await navigate({ href: '/dashboard', replace: true })
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

      <Card className='w-full max-w-md'>
        <CardHeader>
          <CardTitle>
            {t('Initialize {{name}}', { name: systemName || 'PowerBarRations' })}
          </CardTitle>
          <CardDescription>
            {t(
              'Set the login password for this gateway. It is the only credential; keep it safe.'
            )}
          </CardDescription>
        </CardHeader>

        <Form {...form}>
          <form onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
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
              <Button type='submit' className='w-full' disabled={mutation.isPending}>
                {mutation.isPending && <Loader2 className='size-4 animate-spin' />}
                {t('Initialize')}
              </Button>
            </CardFooter>
          </form>
        </Form>
      </Card>
    </div>
  )
}
