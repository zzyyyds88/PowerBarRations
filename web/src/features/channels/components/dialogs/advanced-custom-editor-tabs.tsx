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
import {
  ChevronDown,
  ChevronRight,
  CircleDollarSign,
  Code2,
  ListTree,
} from 'lucide-react'
import type { Dispatch, SetStateAction } from 'react'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Combobox } from '@/components/ui/combobox'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import {
  ADVANCED_CUSTOM_BALANCE_LABEL,
  ADVANCED_CUSTOM_BALANCE_PATH,
  ADVANCED_CUSTOM_MODEL_LIST_LABEL,
  ADVANCED_CUSTOM_MODEL_LIST_PATH,
  ADVANCED_CUSTOM_TEMPLATE_OPTIONS,
  getAdvancedCustomIncomingPathLabel,
  getAdvancedCustomManagementRoute,
  getAdvancedCustomTemplateConfig,
  stringifyAdvancedCustomConfig,
  type validateAdvancedCustomConfig,
} from '../../lib/advanced-custom'
import type { AdvancedCustomConfig, AdvancedCustomRoute } from '../../types'
import type {
  AdvancedCustomRouteGroup,
  AdvancedCustomRouteRow,
} from './advanced-custom-editor-model'
import { RouteModeBadges } from './advanced-custom-editor-parts'
import {
  ManagementRouteEditor,
  RouteGroupEditor,
} from './advanced-custom-route-group-editor'

export type AdvancedCustomEditorTab =
  | 'forwarding'
  | 'models'
  | 'balance'
  | 'json'

interface AdvancedCustomEditorTabsProps {
  activeTab: AdvancedCustomEditorTab
  switchTab: (tab: AdvancedCustomEditorTab) => void
  routeRows: AdvancedCustomRouteRow[]
  normalizedConfig: AdvancedCustomConfig
  availableIncomingPathOptions: Array<{ value: string; label: string }>
  addRoute: (incomingPath: string | null) => void
  selectTemplate: (templateKey: string) => void
  setExpandedRouteGroups: Dispatch<SetStateAction<Set<string>>>
  routeGroups: AdvancedCustomRouteGroup[]
  validationError: ReturnType<typeof validateAdvancedCustomConfig>
  canFixCatchAllOrder: boolean
  fixCatchAllOrder: () => void
  expandedRouteGroups: Set<string>
  updateGroupIncomingPath: (
    group: AdvancedCustomRouteGroup,
    incomingPath: string | null
  ) => void
  addRouteForIncomingPath: (incomingPath: string) => void
  moveRouteWithinGroup: (index: number, direction: -1 | 1) => void
  moveRouteToGroupEnd: (index: number) => void
  removeRoute: (index: number) => void
  updateRoute: (index: number, patch: Partial<AdvancedCustomRoute>) => void
  usedIncomingPaths: ReadonlySet<string>
  onManagementRouteChange: (
    path: string,
    route: AdvancedCustomRoute | null
  ) => void
  jsonText: string
  handleJsonChange: (value: string) => void
  templateKey: string
  jsonError: string
}

export function AdvancedCustomEditorTabs(props: AdvancedCustomEditorTabsProps) {
  const { t } = useTranslation()
  const modelListRoute = getAdvancedCustomManagementRoute(
    props.normalizedConfig,
    ADVANCED_CUSTOM_MODEL_LIST_PATH
  )
  const balanceRoute = getAdvancedCustomManagementRoute(
    props.normalizedConfig,
    ADVANCED_CUSTOM_BALANCE_PATH
  )

  return (
    <Tabs
      value={props.activeTab}
      onValueChange={(value) =>
        props.switchTab(value as AdvancedCustomEditorTab)
      }
      className='min-w-0 gap-0'
    >
      <div className='border-b px-4 py-3'>
        <TabsList className='grid h-auto w-full grid-cols-2 gap-1 sm:grid-cols-4'>
          <TabsTrigger value='forwarding'>
            <ListTree className='size-4' aria-hidden='true' />
            {t('Forwarding Routes')}
            <Badge variant='outline'>{props.routeRows.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value='models'>
            <ListTree className='size-4' aria-hidden='true' />
            {t('Model List')}
            <Badge variant='outline'>
              {modelListRoute ? t('Configured') : t('Not configured')}
            </Badge>
          </TabsTrigger>
          <TabsTrigger value='balance'>
            <CircleDollarSign className='size-4' aria-hidden='true' />
            {t('Balance Query')}
            <Badge variant='outline'>
              {balanceRoute ? t('Configured') : t('Not configured')}
            </Badge>
          </TabsTrigger>
          <TabsTrigger value='json'>
            <Code2 className='size-4' aria-hidden='true' />
            {t('Full JSON')}
          </TabsTrigger>
        </TabsList>
      </div>

      <TabsContent value='forwarding' className='flex flex-col gap-4 p-4'>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
          <div>
            <p className='font-medium'>{t('Forwarding Routes')}</p>
            <p className='text-muted-foreground text-xs'>
              {t('Add routes individually or replace them from a template.')}
            </p>
          </div>
          <div className='flex flex-wrap gap-2'>
            <Combobox
              options={props.availableIncomingPathOptions}
              value=''
              onValueChange={(incomingPath) => {
                if (typeof incomingPath === 'string') {
                  props.addRoute(incomingPath)
                }
              }}
              disabled={props.availableIncomingPathOptions.length === 0}
              className='w-full'
              placeholder={t('Add route')}
            />
            <Select
              value={null}
              onValueChange={(value) => {
                if (typeof value === 'string') props.selectTemplate(value)
              }}
            >
              <SelectTrigger size='sm'>
                <SelectValue placeholder={t('Add template')} />
              </SelectTrigger>
              <SelectContent align='end' alignItemWithTrigger={false}>
                <SelectGroup>
                  {ADVANCED_CUSTOM_TEMPLATE_OPTIONS.map((template) => (
                    <SelectItem key={template.value} value={template.value}>
                      {t(template.label)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => props.setExpandedRouteGroups(new Set())}
            >
              {t('Collapse all')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() =>
                props.setExpandedRouteGroups(
                  new Set(props.routeGroups.map((group) => group.incomingPath))
                )
              }
            >
              {t('Expand all')}
            </Button>
          </div>
        </div>

        {props.validationError ? (
          <Alert variant='destructive'>
            <AlertDescription className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
              <span>
                {props.validationError.routeIndex !== undefined
                  ? `${t('Route')} ${props.validationError.routeIndex + 1}: `
                  : ''}
                {t(props.validationError.message)}
              </span>
              {props.canFixCatchAllOrder ? (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={props.fixCatchAllOrder}
                >
                  {t('Fix order')}
                </Button>
              ) : null}
            </AlertDescription>
          </Alert>
        ) : null}

        <div className='flex flex-col gap-3'>
          {props.routeGroups.map((routeGroup) => (
            <Collapsible
              key={routeGroup.incomingPath || 'advanced-custom-empty-path'}
              open={props.expandedRouteGroups.has(routeGroup.incomingPath)}
              onOpenChange={(expanded) =>
                props.setExpandedRouteGroups((current) => {
                  const next = new Set(current)
                  if (expanded) next.add(routeGroup.incomingPath)
                  else next.delete(routeGroup.incomingPath)
                  return next
                })
              }
              className='rounded-md border'
            >
              <CollapsibleTrigger
                render={
                  <button
                    type='button'
                    className='hover:bg-muted/50 focus-visible:ring-ring/50 flex min-h-11 w-full items-center gap-3 rounded-md px-3 py-2 text-left outline-none focus-visible:ring-3'
                  />
                }
              >
                {props.expandedRouteGroups.has(routeGroup.incomingPath) ? (
                  <ChevronDown className='size-4 shrink-0' aria-hidden='true' />
                ) : (
                  <ChevronRight
                    className='size-4 shrink-0'
                    aria-hidden='true'
                  />
                )}
                <span className='min-w-0 flex-1'>
                  <span className='flex min-w-0 flex-wrap items-center gap-2'>
                    <span className='truncate font-medium'>
                      {getAdvancedCustomIncomingPathLabel(
                        routeGroup.incomingPath
                      )}
                    </span>
                    <RouteModeBadges
                      routes={routeGroup.routeRows.map(
                        (routeRow) => routeRow.route
                      )}
                    />
                  </span>
                  <span className='text-muted-foreground block truncate font-mono text-xs'>
                    {routeGroup.incomingPath}
                  </span>
                </span>
                <Badge variant='outline'>
                  {routeGroup.routeRows.length} {t('Routes')}
                </Badge>
              </CollapsibleTrigger>
              <CollapsibleContent className='border-t'>
                <RouteGroupEditor
                  group={routeGroup}
                  usedIncomingPaths={props.usedIncomingPaths}
                  validationError={props.validationError}
                  hideHeader
                  onAddRoute={() =>
                    props.addRouteForIncomingPath(routeGroup.incomingPath)
                  }
                  onIncomingPathChange={(nextIncomingPath) =>
                    props.updateGroupIncomingPath(routeGroup, nextIncomingPath)
                  }
                  onMoveRoute={(index, direction) =>
                    props.moveRouteWithinGroup(index, direction)
                  }
                  onMoveRouteToEnd={props.moveRouteToGroupEnd}
                  onRemoveRoute={props.removeRoute}
                  onRouteChange={props.updateRoute}
                />
              </CollapsibleContent>
            </Collapsible>
          ))}
        </div>
      </TabsContent>

      <TabsContent value='models' className='p-4'>
        <ManagementRouteEditor
          route={modelListRoute}
          path={ADVANCED_CUSTOM_MODEL_LIST_PATH}
          title={ADVANCED_CUSTOM_MODEL_LIST_LABEL}
          description={t(
            'This route is used only by channel management to discover upstream models.'
          )}
          onChange={(route) =>
            props.onManagementRouteChange(
              ADVANCED_CUSTOM_MODEL_LIST_PATH,
              route
            )
          }
        />
      </TabsContent>

      <TabsContent value='balance' className='p-4'>
        <ManagementRouteEditor
          route={balanceRoute}
          path={ADVANCED_CUSTOM_BALANCE_PATH}
          title={ADVANCED_CUSTOM_BALANCE_LABEL}
          description={t(
            'This route is used only by channel management to query the upstream balance.'
          )}
          onChange={(route) =>
            props.onManagementRouteChange(ADVANCED_CUSTOM_BALANCE_PATH, route)
          }
        />
      </TabsContent>

      <TabsContent value='json' className='p-4'>
        <JsonCodeEditor
          value={props.jsonText}
          onChange={props.handleJsonChange}
          placeholder={stringifyAdvancedCustomConfig(
            getAdvancedCustomTemplateConfig(props.templateKey)
          )}
          heightClassName='h-[420px] min-h-[420px] max-h-[420px]'
          aria-invalid={Boolean(props.jsonError)}
          ariaLabel={t('Advanced text editing')}
        />
        <p className='text-muted-foreground mt-2 text-xs'>
          {t('Edit JSON text directly. Format will be validated on save.')}
        </p>
        {props.jsonError ? (
          <p className='text-destructive mt-1 text-xs'>{props.jsonError}</p>
        ) : null}
      </TabsContent>
    </Tabs>
  )
}
