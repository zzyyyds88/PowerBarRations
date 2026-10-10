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
import { cva, type VariantProps } from 'class-variance-authority'
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

const iconBadgeVariants = cva(
  'flex shrink-0 items-center justify-center [&>svg]:shrink-0',
  {
    variants: {
      tone: {
        neutral: 'pbr-tile pbr-tone-neutral border text-muted-foreground',
        primary: 'pbr-tile pbr-tone-primary border text-primary',
        success: 'pbr-tile pbr-tone-success border text-success',
        warning: 'pbr-tile pbr-tone-warning border text-warning',
        info: 'pbr-tile pbr-tone-info border text-info',
        destructive: 'pbr-tile pbr-tone-destructive border text-destructive',
        'chart-1': 'pbr-tile pbr-tone-chart-1 border text-chart-1',
        'chart-2': 'pbr-tile pbr-tone-chart-2 border text-chart-2',
        'chart-3': 'pbr-tile pbr-tone-chart-3 border text-chart-3',
        'chart-4': 'pbr-tile pbr-tone-chart-4 border text-chart-4',
        'chart-5': 'pbr-tile pbr-tone-chart-5 border text-chart-5',
      },
      size: {
        xs: 'size-5 rounded-md [&>svg]:size-3',
        sm: 'size-7 rounded-md [&>svg]:size-3.5',
        md: 'size-8 rounded-lg [&>svg]:size-4',
        title: 'size-8 rounded-lg sm:size-9 [&>svg]:size-4',
        lg: 'size-10 rounded-xl [&>svg]:size-5',
        stat: 'size-5 rounded-md sm:size-7 [&>svg]:size-3 sm:[&>svg]:size-3.5',
      },
    },
    defaultVariants: {
      tone: 'neutral',
      size: 'md',
    },
  }
)

export type IconBadgeTone = NonNullable<
  VariantProps<typeof iconBadgeVariants>['tone']
>

export type IconBadgeSize = NonNullable<
  VariantProps<typeof iconBadgeVariants>['size']
>

interface IconBadgeProps {
  children?: ReactNode
  tone?: IconBadgeTone
  size?: IconBadgeSize
  className?: string
  decorative?: boolean
}

export function IconBadge(props: IconBadgeProps) {
  return (
    <span
      className={cn(
        iconBadgeVariants({ tone: props.tone, size: props.size }),
        props.className
      )}
      aria-hidden={props.decorative ?? true}
    >
      {props.children}
    </span>
  )
}
