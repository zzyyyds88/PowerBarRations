import { api } from "@/api/client";
import type { PruneResult, SystemOptions } from "@/api/types";

/**
 * 账户、系统选项与备份（ui-spec §6.9）的端点封装。
 * ui-spec §1：页面不得直连端点，因此这些调用住在这里而不是 Settings 页内。
 */

// 字段名必须与后端契约一致（api-spec §5.1：{"current","new"}）。
// 曾误用 {"current","next"}，后端取不到 new → 400，设置页改口令必然失败。
export function changeAdminPassword(current: string, nextPassword: string): Promise<{ updated: boolean }> {
  return api.post<{ updated: boolean }>("/auth/password", { current, new: nextPassword });
}

/** 导出配置：不含密钥明文与哈希（api-spec §5.7）。 */
export function exportConfigBundle(): Promise<unknown> {
  return api.get<unknown>("/export");
}

export function updateSystemOptions(patch: unknown): Promise<SystemOptions> {
  return api.put<SystemOptions>("/system/options", patch);
}

export function pruneRequestLogs(dryRun: boolean): Promise<PruneResult> {
  return api.post<PruneResult>(`/logs/prune?dry_run=${dryRun ? "true" : "false"}`);
}
