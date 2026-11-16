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
import { ADVANCED_CUSTOM_CONVERTER_OPTIONS } from '../../lib/advanced-custom'
import type { AdvancedCustomConverter, AdvancedCustomRoute } from '../../types'

export const longSelectContentClass = 'w-[360px] max-w-[calc(100vw-2rem)]'
export const longSelectItemClass =
  'items-start py-2 [&_[data-slot=select-item-text]]:min-w-0 [&_[data-slot=select-item-text]]:shrink [&_[data-slot=select-item-text]]:whitespace-normal'
export const routeEditorGridClassName =
  'lg:grid-cols-[minmax(9rem,0.9fr)_minmax(0,1fr)_minmax(0,1.25fr)_minmax(0,1.1fr)_minmax(0,0.85fr)_7rem]'
export const upstreamPathDescriptionKey =
  'Use a path to append it to the channel Base URL, or enter a full URL to override the Base URL for this route.'
export const catchAllOrderErrorMessage =
  'Catch-all route must be last for the same incoming path'

export type AdvancedCustomRouteRow = {
  route: AdvancedCustomRoute
  routeKey: string
  index: number
}

export type AdvancedCustomRouteGroup = {
  incomingPath: string
  routeRows: AdvancedCustomRouteRow[]
}

export function getOptionLabel(
  options: ReadonlyArray<{ value: string; label: string }>,
  value: string
) {
  return options.find((option) => option.value === value)?.label || value
}

export function getRouteIncomingPath(route: AdvancedCustomRoute): string {
  return (route.incoming_path || '').trim()
}

export function isCatchAllRoute(route: AdvancedCustomRoute): boolean {
  return !route.models || route.models.length === 0
}

function getRouteConverterLabel(route: AdvancedCustomRoute): string {
  const converter = route.converter || 'none'
  return (
    ADVANCED_CUSTOM_CONVERTER_OPTIONS.find(
      (option) => option.value === converter
    )?.triggerLabel || converter
  )
}

export function getRouteConverters(
  routes: AdvancedCustomRoute[]
): Array<{ converter: AdvancedCustomConverter; label: string }> {
  const converters = new Map<
    AdvancedCustomConverter,
    { converter: AdvancedCustomConverter; label: string }
  >()
  for (const route of routes) {
    const converter = route.converter || 'none'
    if (!converters.has(converter)) {
      converters.set(converter, {
        converter,
        label: getRouteConverterLabel(route),
      })
    }
  }
  return [...converters.values()]
}

export function buildRouteGroups(
  routeRows: AdvancedCustomRouteRow[]
): AdvancedCustomRouteGroup[] {
  const groups: AdvancedCustomRouteGroup[] = []
  const groupByPath = new Map<string, AdvancedCustomRouteGroup>()

  for (const routeRow of routeRows) {
    const incomingPath = getRouteIncomingPath(routeRow.route)
    let group = groupByPath.get(incomingPath)
    if (!group) {
      group = { incomingPath, routeRows: [] }
      groupByPath.set(incomingPath, group)
      groups.push(group)
    }
    group.routeRows.push(routeRow)
  }

  return groups
}
