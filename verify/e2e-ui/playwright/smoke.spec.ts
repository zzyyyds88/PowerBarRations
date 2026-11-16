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
import { expect, test } from '@playwright/test'

const password = process.env.PBR_E2E_PASSWORD ?? 'test123'
const seededChannel = process.env.PBR_E2E_CHANNEL ?? 'pw-e2e'

test.skip(
  !process.env.PBR_E2E_BASE_URL,
  'PBR_E2E_BASE_URL is required; run verify/e2e-ui/playwright/run.sh'
)

const NAV_ITEMS = [
  { label: 'Overview', path: '/overview' },
  { label: 'Routing', path: '/routes' },
  { label: 'Channels', path: '/connections/channels' },
  { label: 'Channel status', path: '/connections/health' },
  { label: 'Model catalog', path: '/connections/catalog' },
  { label: 'API Keys', path: '/keys' },
  { label: 'Logs', path: '/logs/requests' },
  { label: 'Settings', path: '/system-settings/site' },
] as const

const LOG_TABS = [
  { label: 'Request logs', path: '/logs/requests' },
  { label: 'Audit logs', path: '/logs/audit' },
  { label: 'System tasks', path: '/logs/tasks' },
] as const

test('login, 7+1 routes, log tabs, command palette and export entries', async ({
  page,
}) => {
  // 登录由 UI 的 /sign-in 表单触发（不写 localStorage、不塞管理密钥）。
  await page.goto('/sign-in')
  await page
    .getByRole('textbox', { name: 'Password', exact: true })
    .fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/overview(?:\/|$)/)

  // 概览的趋势有「调用」和「消耗」两条同轴时间线（ui-spec §6 概览）。
  await page.getByRole('tab', { name: 'Cost trend' }).click()
  await expect(
    page.getByRole('tab', { name: 'Cost trend' })
  ).toHaveAttribute('aria-selected', 'true')

  for (const item of NAV_ITEMS) {
    const link = page.getByRole('link', { name: item.label, exact: true }).first()
    await expect(link).toBeVisible()
    await link.click()
    await expect(page).toHaveURL(new RegExp(`${item.path}(?:/|$)`))
    await expect(page.getByRole('main').first()).toBeVisible()
  }

  // 日志三页签是真实路由（刷新/深链可用），逐个点击并断言 URL。
  await page.goto('/logs/requests')
  for (const tab of LOG_TABS) {
    const link = page
      .getByRole('navigation', { name: 'Logs' })
      .getByRole('link', { name: tab.label, exact: true })
    await link.click()
    await expect(page).toHaveURL(new RegExp(`${tab.path}(?:/|$)`))
  }

  // 命令面板 ⌘K：打开后输入实体名，必须出现实体分组结果（不是空列表）。
  await page.keyboard.press('Control+k')
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  const search = dialog.getByRole('combobox').first()
  await expect(search).toBeVisible()
  await search.fill(seededChannel)
  await expect(dialog.getByText(seededChannel, { exact: false }).first()).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()

  // 导出入口只断言按钮存在且可点，不触发下载（本层只读页面）。
  await page.goto('/connections/channels')
  await expect(
    page.getByRole('button', { name: 'Export CSV' }).first()
  ).toBeVisible()
  await page.goto('/keys')
  await expect(
    page.getByRole('button', { name: 'Export CSV' }).first()
  ).toBeVisible()

  // 渠道行动作降噪（ui-spec §7）：行内只保留编辑/测试，启停收进 ... 菜单。
  await page.goto('/connections/channels')
  const channelRow = page
    .getByRole('row')
    .filter({ hasText: seededChannel })
    .first()
  await expect(channelRow.getByRole('button', { name: 'Edit' })).toBeVisible()
  await expect(
    channelRow.getByRole('button', { name: 'Test Connection' })
  ).toBeVisible()
  await expect(
    channelRow.getByRole('button', { name: 'Disable' })
  ).toHaveCount(0)
  await channelRow.getByRole('button', { name: 'Open menu' }).click()
  await expect(
    page.getByRole('menuitem', { name: 'Disable' }).first()
  ).toBeVisible()
  await page.keyboard.press('Escape')

  // 模型目录的只读价格/健康列存在（没有价格/流量时显示 —，不伪造 0）。
  await page.goto('/connections/catalog')
  await expect(
    page.getByRole('columnheader', { name: 'Price (¥/1M)' })
  ).toBeVisible()
  await expect(
    page.getByRole('columnheader', { name: 'Health' })
  ).toBeVisible()

  // 日志窗口 KPI 与视图切换；卡片选择写入 localStorage 并跨刷新保持。
  await page.goto('/logs/requests')
  await expect(page.getByText('Window cost')).toBeVisible()
  await expect(page.getByText('Window requests')).toBeVisible()
  const logView = page.getByRole('group', { name: 'Log view' })
  await expect(logView).toBeVisible()
  await logView.getByRole('button', { name: 'Cards' }).click()
  await expect(
    logView.getByRole('button', { name: 'Cards' })
  ).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByText('No Logs Found')).toBeVisible()
  await page.reload()
  await expect(
    page.getByRole('group', { name: 'Log view' }).getByRole('button', {
      name: 'Cards',
    })
  ).toHaveAttribute('aria-pressed', 'true')
  await page
    .getByRole('group', { name: 'Log view' })
    .getByRole('button', { name: 'Table' })
    .click()
})