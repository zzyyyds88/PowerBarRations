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
import { mergeProps } from '@base-ui/react/merge-props'
import { useRender } from '@base-ui/react/use-render'
import { cva, type VariantProps } from 'class-variance-authority'

import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'group/badge inline-flex h-5 w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-4xl border px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-all duration-150 ease-out focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 [&>svg]:pointer-events-none [&>svg]:size-3!',
  {
    variants: {
      variant: {
        /* Filled primary pill: same gradient + inner highlight as the
         * filled button, one elevation step down. */
        default:
          'border-transparent bg-primary text-primary-foreground shadow-primary-sm [background-image:var(--gradient-primary)] [a]:hover:shadow-primary [a]:hover:[background-image:var(--gradient-primary-hover)]',
        /* Neutral glass pill: card face, hairline border, inner highlight. */
        secondary:
          'border-border text-secondary-foreground shadow-control [background-image:var(--surface-card)] [a]:hover:brightness-95',
        /* Tonal pills: the shared tonal-tile recipe (`pbr-tile` supplies the
         * same-hue wash, same-hue hairline and inner highlight) driven by the
         * hue slot, plus the derived `--tone-*-text` label. Nothing here is a
         * literal color, so presets repaint the pill. */
        destructive:
          'pbr-tile pbr-tone-destructive border text-[var(--tone-destructive-text)] focus-visible:ring-destructive/20 [a]:hover:brightness-95 dark:focus-visible:ring-destructive/40',
        warning:
          'pbr-tile pbr-tone-warning border text-[var(--tone-warning-text)] focus-visible:ring-warning/20 [a]:hover:brightness-95',
        outline:
          'border-border text-foreground [a]:hover:bg-(--surface-hover) [a]:hover:text-foreground',
        ghost:
          'border-transparent hover:bg-(--surface-hover) hover:text-muted-foreground dark:hover:bg-(--surface-hover)',
        link: 'border-transparent text-primary underline-offset-4 hover:underline',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  }
)

function Badge({
  className,
  variant = 'default',
  render,
  ...props
}: useRender.ComponentProps<'span'> & VariantProps<typeof badgeVariants>) {
  return useRender({
    defaultTagName: 'span',
    props: mergeProps<'span'>(
      {
        className: cn(badgeVariants({ variant }), className),
      },
      props
    ),
    render,
    state: {
      slot: 'badge',
      variant,
    },
  })
}

export { Badge, badgeVariants }
