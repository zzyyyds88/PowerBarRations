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
import { Eye, EyeOff } from 'lucide-react'
import * as React from 'react'

import { cn } from '@/lib/utils'

import { Button } from './ui/button'
import { Input } from './ui/input'

type PasswordInputProps = Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  'type'
> & {
  ref?: React.Ref<HTMLInputElement>
}

// 触屏浏览器（安卓 Chrome 等）会在 type=password 输入框内画原生眼睛，
// 与本组件的切换按钮叠成两只且无法用 CSS 屏蔽。触屏端改用
// type=text + -webkit-text-security: disc 掩码，原生眼睛随之消失；
// 桌面端保持 type=password 以保留密码管理器自动填充。
const isTouchSecureText =
  typeof navigator !== 'undefined' &&
  typeof CSS !== 'undefined' &&
  (navigator.maxTouchPoints ?? 0) > 0 &&
  Boolean(CSS.supports?.('-webkit-text-security', 'disc'))

export function PasswordInput({
  className,
  disabled,
  ref,
  ...props
}: PasswordInputProps) {
  const [showPassword, setShowPassword] = React.useState(false)

  return (
    <div className={cn('relative rounded-md', className)}>
      <Input
        type={isTouchSecureText || showPassword ? 'text' : 'password'}
        style={
          isTouchSecureText && !showPassword
            ? ({ WebkitTextSecurity: 'disc' } as React.CSSProperties)
            : undefined
        }
        autoCapitalize={isTouchSecureText ? 'none' : props.autoCapitalize}
        spellCheck={isTouchSecureText ? false : props.spellCheck}
        ref={ref}
        disabled={disabled}
        {...props}
      />
      <Button
        type='button'
        size='icon'
        variant='ghost'
        disabled={disabled}
        className='text-muted-foreground absolute end-1 top-1/2 h-6 w-6 -translate-y-1/2 rounded-md'
        onClick={() => setShowPassword((prev) => !prev)}
        aria-label='Toggle password visibility'
      >
        {showPassword ? (
          <Eye size={18} aria-hidden='true' />
        ) : (
          <EyeOff size={18} aria-hidden='true' />
        )}
      </Button>
    </div>
  )
}
