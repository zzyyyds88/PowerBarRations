import { api } from "@/api/client";

/**
 * 首次初始化与登录（token-spec §2）。两者都不带管理密钥：它们正是取得密钥的入口。
 */

/** 首次设口令：口令派生管理密钥，响应里的明文只出现这一次。 */
export function setupAdmin(password: string): Promise<{ admin_key: string; warning?: string }> {
  return api.post<{ admin_key: string; warning?: string }>("/setup", { password });
}

export function login(password: string): Promise<{ token: string }> {
  return api.post<{ token: string }>("/auth/login", { password });
}
