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
import { t as i18nT } from 'i18next'
import { z } from 'zod'

// ============================================================================
// Form Schemas
// ============================================================================

// PBR：无账号体系，只有登录口令（token-spec §2）。
// username 保留在 schema 里（表单默认填固定占位、界面隐藏）以维持上游 RHF 类型契约。
// zod 校验信息通过惰性函数在**校验时**取当前语言，避免模块加载时锁定文案。
export const loginFormSchema = z.object({
  username: z.string().min(1, {
    error: () => i18nT('Please enter your username or email'),
  }),
  password: z.string().min(1, {
    error: () => i18nT('Please enter your login password'),
  }),
})
