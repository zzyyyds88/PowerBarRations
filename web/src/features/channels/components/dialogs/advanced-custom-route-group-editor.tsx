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
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'

import {
  ADVANCED_CUSTOM_AUTH_MODE_OPTIONS,
  ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS,
  ADVANCED_CUSTOM_MODEL_LIST_LABEL,
  ADVANCED_CUSTOM_MODEL_LIST_PATH,
  buildAdvancedCustomAuth,
  createAdvancedCustomManagementRoute,
  getAdvancedCustomAuthMode,
  type AdvancedCustomAuthMode,
  type validateAdvancedCustomConfig,
} from '../../lib/advanced-custom'
import type { AdvancedCustomRoute } from '../../types'
import {
  getOptionLabel,
  isCatchAllRoute,
  routeEditorGridClassName,
  upstreamPathDescriptionKey,
  type AdvancedCustomRouteGroup,
} from './advanced-custom-editor-model'
import {
  FieldBlock,
  ModelRuleHelpPopover,
} from './advanced-custom-editor-parts'
import { RouteEditor } from './advanced-custom-route-editor'

export function ManagementRouteEditor({
  route,
  path,
  title,
  description,
  onChange,
}: {
  route: AdvancedCustomRoute | undefined
  path: string
  title: string
  description: string
  onChange: (route: AdvancedCustomRoute | null) => void
}) {
  const { t } = useTranslation()
  const authMode = route ? getAdvancedCustomAuthMode(route) : 'default'

  if (!route) {
    return (
      <div className='flex min-h-52 flex-col items-center justify-center gap-4 rounded-lg border border-dashed p-6 text-center'>
        <div>
          <p className='font-medium'>{title}</p>
          <p className='text-muted-foreground mt-1 max-w-lg text-sm'>
            {description}
          </p>
          <p className='text-muted-foreground mt-2 font-mono text-xs'>{path}</p>
        </div>
        <Button
          type='button'
          onClick={() => onChange(createAdvancedCustomManagementRoute(path))}
        >
          <Plus data-icon='inline-start' />
          {t('Add management route')}
        </Button>
      </div>
    )
  }

  const updateAuth = (
    field: Exclude<keyof NonNullable<AdvancedCustomRoute['auth']>, 'type'>,
    value: string
  ) => {
    if (!route.auth || route.auth.type === 'none') return
    onChange({
      ...route,
      auth: {
        type: route.auth.type,
        name: route.auth.name || '',
        value: route.auth.value || '',
        [field]: value,
      },
    })
  }

  return (
    <div className='flex flex-col gap-4 rounded-lg border p-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div>
          <p className='font-medium'>{title}</p>
          <p className='text-muted-foreground mt-1 text-sm'>{description}</p>
          <p className='text-muted-foreground mt-1 font-mono text-xs'>{path}</p>
        </div>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={() => onChange(null)}
        >
          <Trash2 data-icon='inline-start' />
          {t('Delete')}
        </Button>
      </div>
      <div className='grid gap-4 md:grid-cols-2'>
        <FieldBlock label={t('Upstream path')}>
          <Input
            value={route.upstream_path || ''}
            onChange={(event) =>
              onChange({ ...route, upstream_path: event.target.value })
            }
            placeholder={
              path === ADVANCED_CUSTOM_MODEL_LIST_PATH
                ? '/v1/models'
                : '/v1/chat/completions'
            }
          />
        </FieldBlock>
        <FieldBlock label={t('Auth')}>
          <Select
            value={authMode}
            onValueChange={(value) =>
              onChange({
                ...route,
                auth: buildAdvancedCustomAuth(
                  value as AdvancedCustomAuthMode,
                  route.auth
                ),
              })
            }
          >
            <SelectTrigger>
              <SelectValue className='min-w-0 truncate'>
                {t(getOptionLabel(ADVANCED_CUSTOM_AUTH_MODE_OPTIONS, authMode))}
              </SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {ADVANCED_CUSTOM_AUTH_MODE_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {t(option.label)}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </FieldBlock>
        {authMode === 'header' || authMode === 'query' ? (
          <>
            <FieldBlock label={t('Auth name')}>
              <Input
                value={route.auth?.name || ''}
                onChange={(event) => updateAuth('name', event.target.value)}
                placeholder={
                  authMode === 'header' ? 'Authorization' : 'api_key'
                }
              />
            </FieldBlock>
            <FieldBlock label={t('Auth value')}>
              <Input
                value={route.auth?.value || ''}
                onChange={(event) => updateAuth('value', event.target.value)}
                placeholder={
                  authMode === 'header' ? 'Bearer {api_key}' : '{api_key}'
                }
              />
            </FieldBlock>
          </>
        ) : null}
      </div>
      <p className='text-muted-foreground text-xs'>
        {t(upstreamPathDescriptionKey)}
      </p>
    </div>
  )
}

export function RouteGroupEditor({
  group,
  usedIncomingPaths,
  validationError,
  hideHeader = false,
  onAddRoute,
  onIncomingPathChange,
  onMoveRoute,
  onMoveRouteToEnd,
  onRemoveRoute,
  onRouteChange,
}: {
  group: AdvancedCustomRouteGroup
  usedIncomingPaths: ReadonlySet<string>
  validationError: ReturnType<typeof validateAdvancedCustomConfig>
  hideHeader?: boolean
  onAddRoute: () => void
  onIncomingPathChange: (incomingPath: string | null) => void
  onMoveRoute: (index: number, direction: -1 | 1) => void
  onMoveRouteToEnd: (index: number) => void
  onRemoveRoute: (index: number) => void
  onRouteChange: (index: number, patch: Partial<AdvancedCustomRoute>) => void
}) {
  const { t } = useTranslation()
  const incomingPath = group.incomingPath || '/v1/chat/completions'
  const isModelListGroup = incomingPath === ADVANCED_CUSTOM_MODEL_LIST_PATH
  const catchAllRoute = group.routeRows.find((routeRow) =>
    isCatchAllRoute(routeRow.route)
  )
  const catchAllRoutePosition = catchAllRoute
    ? group.routeRows.findIndex(
        (routeRow) => routeRow.index === catchAllRoute.index
      )
    : -1
  const hasCatchAll = catchAllRoute !== undefined
  const catchAllIsLast =
    !hasCatchAll || catchAllRoutePosition === group.routeRows.length - 1
  const groupHasError =
    validationError?.routeIndex !== undefined &&
    group.routeRows.some(
      (routeRow) => routeRow.index === validationError.routeIndex
    )

  return (
    <section
      className={cn(
        'border-border overflow-hidden rounded-md border',
        groupHasError && 'border-destructive/60'
      )}
    >
      {!hideHeader ? (
        <div className='bg-muted/20 flex flex-col gap-3 p-3 lg:flex-row lg:items-center lg:justify-between'>
          <div className='flex min-w-0 flex-1 flex-col gap-2'>
            <div className='flex flex-wrap items-center gap-2'>
              <span className='text-sm font-medium'>{t('Route group')}</span>
              <Badge variant='secondary'>
                {group.routeRows.length} {t('Routes')}
              </Badge>
              {isModelListGroup ? (
                <Badge variant='outline'>
                  {ADVANCED_CUSTOM_MODEL_LIST_LABEL}
                </Badge>
              ) : (
                <Badge variant={hasCatchAll ? 'outline' : 'secondary'}>
                  {hasCatchAll ? t('Fallback route') : t('Model-scoped only')}
                </Badge>
              )}
              {!isModelListGroup && !catchAllIsLast ? (
                <Badge variant='destructive'>
                  {t('Fallback must be last')}
                </Badge>
              ) : null}
            </div>
            <Combobox
              options={ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS.map((option) => ({
                ...option,
                description: option.value,
                disabled:
                  (option.value !== incomingPath &&
                    usedIncomingPaths.has(option.value)) ||
                  (option.value === ADVANCED_CUSTOM_MODEL_LIST_PATH &&
                    group.routeRows.length > 1),
              }))}
              value={incomingPath}
              onValueChange={onIncomingPathChange}
              className='h-9 max-w-full lg:max-w-[420px]'
            />
          </div>
        </div>
      ) : null}

      <div className={cn('px-3 py-2', !hideHeader && 'border-t')}>
        <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
          <p className='text-muted-foreground text-xs leading-relaxed'>
            {isModelListGroup
              ? t(
                  'This route discovers upstream OpenAI models and cannot be split or matched by client model rules.'
                )
              : t(
                  'Routes with the same incoming path are split by client model rules. Unmatched requests use the final fallback.'
                )}
          </p>
          {!isModelListGroup ? (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={onAddRoute}
            >
              <Plus data-icon='inline-start' aria-hidden='true' />
              {t('Add split')}
            </Button>
          ) : null}
        </div>
        {groupHasError && validationError ? (
          <p className='text-destructive mt-1 text-xs'>
            {validationError.routeIndex !== undefined
              ? `${t('Route')} ${validationError.routeIndex + 1}: `
              : ''}
            {t(validationError.message)}
          </p>
        ) : null}
      </div>

      <div
        className={cn(
          'text-muted-foreground hidden items-center gap-2 border-t bg-muted/10 px-3 py-2 text-xs font-medium lg:grid',
          routeEditorGridClassName
        )}
      >
        <span>{t('Route')}</span>
        <span className='inline-flex items-center gap-1'>
          {t('Client model')}
          <ModelRuleHelpPopover />
        </span>
        <span>{t('Upstream path')}</span>
        <span>{t('Converter')}</span>
        <span>{t('Auth')}</span>
        <span className='text-right'>{t('Actions')}</span>
      </div>

      <div className='divide-y'>
        {group.routeRows.map((routeRow, position) => {
          const canMoveUp = position > 0
          const canMoveDown = position < group.routeRows.length - 1
          const catchAllOutOfOrder =
            isCatchAllRoute(routeRow.route) && canMoveDown
          const routeErrorMessage =
            validationError?.routeIndex === routeRow.index
              ? validationError.message
              : undefined

          return (
            <RouteEditor
              key={routeRow.routeKey}
              route={routeRow.route}
              index={routeRow.index}
              errorMessage={routeErrorMessage}
              canMoveUp={canMoveUp}
              canMoveDown={canMoveDown}
              catchAllOutOfOrder={catchAllOutOfOrder}
              onChange={(patch) => onRouteChange(routeRow.index, patch)}
              onMoveDown={() => onMoveRoute(routeRow.index, 1)}
              onMoveUp={() => onMoveRoute(routeRow.index, -1)}
              onMoveCatchAllToEnd={() => onMoveRouteToEnd(routeRow.index)}
              onRemove={() => onRemoveRoute(routeRow.index)}
            />
          )
        })}
      </div>
    </section>
  )
}
