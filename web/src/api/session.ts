import { api } from "@/api/client";

/**
 * 首次初始化、登录与登出（token-spec §2）。
 *
 * 登录成功后服务端签发 HttpOnly 会话 Cookie（浏览器自动携带）；响应里的
 * `admin_key` 仅供 AI/脚本取用，前端不保存它。
 */

/** 首次设口令：签发会话并返回派生管理密钥（只出现这一次，供 AI 配置）。 */
export function setupAdmin(password: string): Promise<{ admin_key: string; warning?: string }> {
	return api.post<{ admin_key: string; warning?: string }>("/setup", { password });
}

/** 登录：签发会话 Cookie，并返回派生管理密钥。 */
export function login(password: string): Promise<{ token: string; admin_key?: string }> {
	return api.post<{ token: string; admin_key?: string }>("/auth/login", { password });
}

/** 登出：清除会话 Cookie。 */
export function logout(): Promise<{ logged_out: boolean }> {
	return api.post<{ logged_out: boolean }>("/auth/logout");
}
