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
import { api } from '@/lib/api'

// 模型状态（GET /api/model-status，api-spec §6.13）。
//
// 跨渠道按**上游模型**聚合的只读运行表现，是首页概览「Token 排行榜」与
// 「服务状态」两卡的数据源。口径与渠道状态（§6.12）的 models[] 完全一致：
// success_rate 为 0~1 小数，output_tps = Σcompletion_tokens / Σ生成耗时
// （流式扣掉首字等待），avg_ttft_ms 只对流式有样本。
//
// 数据源是**明细日志**：POST /logs/prune 清理后该窗口的样本随之减少（刻意取舍）。
// 成功读**裸对象**（api-spec §2.3）；失败由 axios 拒绝并交由既有
// handleServerError 机制处理，不自造信封。

export interface PBRModelStatusItem {
  /** 上游真名（渠道映射后的实际调用名）。 */
  model: string
  requests: number
  successes: number
  failures: number
  /** 0~1 的小数；requests 为 0 时为 0。 */
  success_rate: number
  /** 明细日志均值（毫秒）；无样本为 0。 */
  avg_total_ms: number
  /** 只对流式有样本；无样本为 0。 */
  avg_ttft_ms: number
  /** 输出速度（tokens/s）；无样本为 0。 */
  output_tps: number
  tokens_in: number
  tokens_out: number
  tokens_total: number
  /** 该窗口按渠道级 prices 折算的上游花费（元）。 */
  estimated_cost: number
}

export interface PBRModelStatusResponse {
  from: number
  to: number
  items: PBRModelStatusItem[]
}

/** 模型状态（`GET /api/model-status?from=&to=`，from/to 为 Unix 秒）。 */
export async function getPBRModelStatus(params: {
  from: number
  to: number
}): Promise<PBRModelStatusResponse> {
  const res = await api.get<PBRModelStatusResponse>('/api/model-status', {
    params: { from: params.from, to: params.to },
  })
  return res.data
}
