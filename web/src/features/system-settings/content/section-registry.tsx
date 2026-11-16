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
import type { ContentSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { ChatSettingsSection } from './chat-settings-section'

// 「API 地址」分节随 API 信息面板一并删除（ADR 0007 / ui-spec §6.2）。
const CONTENT_SECTIONS = [
  {
    id: 'chat',
    titleKey: 'Chat Presets',
    build: (settings: ContentSettings) => (
      <ChatSettingsSection defaultValue={settings.Chats} />
    ),
  },
] as const

export type ContentSectionId = (typeof CONTENT_SECTIONS)[number]['id']

const contentRegistry = createSectionRegistry<
  ContentSectionId,
  ContentSettings
>({
  sections: CONTENT_SECTIONS,
  defaultSection: 'chat',
  basePath: '/system-settings/content',
  urlStyle: 'path',
})

export const CONTENT_SECTION_IDS = contentRegistry.sectionIds
export const CONTENT_DEFAULT_SECTION = contentRegistry.defaultSection
export const getContentSectionNavItems = contentRegistry.getSectionNavItems
export const getContentSectionContent = contentRegistry.getSectionContent
export const getContentSectionMeta = contentRegistry.getSectionMeta
