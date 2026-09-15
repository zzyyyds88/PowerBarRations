import { api } from "@/api/client";
import type { Channel } from "@/api/types";

/**
 * 渠道写操作（ui-spec §1：`api/` 是唯一允许封装管理端点的层，页面不得直连）。
 *
 * 只做"路径 + 请求体"到端点的映射，业务语义（校验、失效缓存、toast）留在页面。
 */
export function updateChannel(name: string, body: unknown): Promise<Channel> {
  return api.put<Channel>(`/channels/${encodeURIComponent(name)}`, body);
}

export function deleteChannel(name: string): Promise<{ deleted: boolean; name: string }> {
  return api.del<{ deleted: boolean; name: string }>(`/channels/${encodeURIComponent(name)}`);
}
