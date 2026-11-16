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
/*
成员选料树（车道编排器与成员组编辑器共用的左栏，ui-spec §6.3）：
渠道折叠 → 模型列表（带搜索框，按渠道名或模型名过滤）；已加入的 (渠道, 模型)
直接禁用。从 lane-composer 抽出共享，禁止两处各写一份。
*/
import { Loader2, Plus, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Input } from '@/components/ui/input'
import { getLobeIcon } from '@/lib/lobe-icon'
import { resolveModelProvider } from '@/lib/model-provider'
import { cn } from '@/lib/utils'

import type { PBRChannelCatalogEntry } from '../api'

/** 左栏一个可选模型项；已加入时禁用（同一 (渠道, 模型) 不可重复）。 */
function ModelPickerItem(props: {
  model: string
  added: boolean
  onAdd: () => void
}) {
  const provider = resolveModelProvider(props.model)
  const icon = provider?.icon
  return (
    <button
      type='button'
      disabled={props.added}
      onClick={props.onAdd}
      aria-label={props.model}
      title={props.model}
      className={cn(
        'flex w-full items-center gap-2 rounded-md border px-2 py-1.5 text-left text-xs transition-colors',
        props.added ? 'cursor-not-allowed opacity-50' : 'hover:bg-muted'
      )}
    >
      {icon ? (
        <>{getLobeIcon(icon, 16)}</>
      ) : (
        <span className='bg-muted text-muted-foreground flex size-4 items-center justify-center rounded text-[9px] font-medium uppercase'>
          {props.model.trim()[0] ?? '?'}
        </span>
      )}
      <span className='min-w-0 flex-1 truncate font-mono'>{props.model}</span>
      <Plus className='size-3.5 shrink-0' />
    </button>
  )
}

export function MemberPickerAccordion(props: {
  catalog: PBRChannelCatalogEntry[]
  loading?: boolean
  /** 已加入的 (渠道, 模型) 键集合：命中的项禁用（去重键由调用方定义）。 */
  selectedKeys: Set<string>
  /** 与 memberKey 同构的键函数（车道与组共享同一判定）。 */
  keyOf: (channel: string, model: string) => string
  onAdd: (channel: string, model: string) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const keyword = search.trim().toLowerCase()

  const channels = useMemo(() => {
    return props.catalog
      .filter((c) => c.enabled && c.models.length > 0)
      .map((c) => ({
        ...c,
        models: keyword
          ? c.models.filter(
              (m) =>
                m.toLowerCase().includes(keyword) ||
                c.name.toLowerCase().includes(keyword)
            )
          : c.models,
      }))
      .filter((c) => c.models.length > 0)
  }, [props.catalog, keyword])

  if (props.loading) {
    return (
      <p className='text-muted-foreground flex items-center gap-2 p-2 text-xs'>
        <Loader2 className='size-3.5 animate-spin' /> {t('Loading...')}
      </p>
    )
  }
  if (channels.length === 0) {
    return (
      <p className='text-muted-foreground p-2 text-xs'>
        {t('No channels declare models yet.')}
      </p>
    )
  }
  return (
    <>
      <div className='relative shrink-0 px-1 pb-1.5'>
        <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3.5 size-3.5 -translate-y-1/2' />
        <Input
          className='h-7 pl-7 text-xs'
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={t('Search channels or models')}
          aria-label={t('Search channels or models')}
        />
      </div>
      <Accordion multiple className='space-y-1'>
        {channels.map((channel) => {
          // 渠道名本应唯一；历史重名时用「名称 + 模型清单 + 映射键」区分，
          // 不用数组下标（同一渠道的模型/映射不同即视为不同目录条目）。
          const channelKey = `${channel.name}\u0000${channel.models.join(',')}\u0000${Object.keys(channel.model_mapping).join(',')}`
          return (
            <AccordionItem
              key={channelKey}
              value={channelKey}
              className='rounded-md border px-2'
            >
              <AccordionTrigger className='py-2 hover:no-underline'>
                <span className='truncate font-mono text-xs'>
                  {channel.name}
                </span>
                <span className='text-muted-foreground ml-2 shrink-0 text-xs'>
                  {channel.models.length}
                </span>
              </AccordionTrigger>
              <AccordionContent className='space-y-1 pt-1'>
                {channel.models.map((model) => (
                  <ModelPickerItem
                    key={model}
                    model={model}
                    added={props.selectedKeys.has(
                      props.keyOf(channel.name, model)
                    )}
                    onAdd={() => props.onAdd(channel.name, model)}
                  />
                ))}
              </AccordionContent>
            </AccordionItem>
          )
        })}
      </Accordion>
    </>
  )
}
