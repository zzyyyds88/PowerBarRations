import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, LogIn } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { z } from 'zod'

import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { loginFormSchema } from '@/features/auth/constants'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import type { AuthFormProps } from '@/features/auth/types'
import { handleServerError } from '@/lib/handle-server-error'
import { pbrLogin } from '@/lib/pbr-auth'
import { AuthOperationError } from '@/lib/secure-verification'
import { cn } from '@/lib/utils'

/**
 * PBR 登录表单：只有一个登录口令。
 *
 * PBR 无账号体系、无二次验证、无 passkey/第三方登录（/api/status 不返回这些开关），
 * 因此这里只保留口令字段；服务端校验后签发 HttpOnly 会话 Cookie。
 * schema 里保留 username 仅为兼容上游类型，界面不渲染该字段。
 */
export function UserAuthForm({
  className,
  redirectTo,
  ...props
}: AuthFormProps) {
  const { t } = useTranslation()
  const [isLoading, setIsLoading] = useState(false)
  const { handleLoginResult } = useAuthRedirect()
  const loginFailedMessage = t('Login failed')

  const form = useForm<z.infer<typeof loginFormSchema>>({
    resolver: zodResolver(loginFormSchema),
    defaultValues: { username: 'admin', password: '' },
  })

  async function onSubmit(data: z.infer<typeof loginFormSchema>) {
    setIsLoading(true)
    try {
      const bundle = await pbrLogin(data.password)
      form.setValue('password', '')
      if (await handleLoginResult(bundle, redirectTo)) {
        toast.success(t('Welcome back!'))
      }
    } catch (error: unknown) {
      handleServerError(AuthOperationError.from(error, loginFailedMessage))
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className={cn('grid gap-4', className)}
        {...props}
      >
        <FormField
          control={form.control}
          name='password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Password')}</FormLabel>
              <FormControl>
                <PasswordInput
                  autoComplete='current-password'
                  placeholder={t('Enter password')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <Button
          type='submit'
          className='mt-2 w-full justify-center gap-2'
          disabled={isLoading}
        >
          {isLoading ? <Loader2 className='animate-spin' /> : <LogIn />}
          {t('Sign in')}
        </Button>
      </form>
    </Form>
  )
}
