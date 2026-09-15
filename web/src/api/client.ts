import { QueryClient } from "@tanstack/react-query";

/**
 * 管理 API 客户端与查询缓存（ui-spec §1：TanStack Query）。
 *
 * 认证模型见 docs/token-spec-v1.md §2：无账号，只有一个登录口令；
 * 管理密钥由口令派生，存在浏览器本地作 Bearer。
 */

const ADMIN_KEY_STORAGE = "pbr.adminKey";

export function getAdminKey(): string {
  return localStorage.getItem(ADMIN_KEY_STORAGE) ?? "";
}

export function setAdminKey(key: string): void {
  if (key) {
    localStorage.setItem(ADMIN_KEY_STORAGE, key);
  } else {
    localStorage.removeItem(ADMIN_KEY_STORAGE);
  }
}

/** 管理 API 的统一错误包络（api-spec §3）。 */
export class ApiError extends Error {
  code: string;
  hint: string;
  status: number;

  constructor(status: number, code: string, message: string, hint: string) {
    super(message);
    this.status = status;
    this.code = code;
    this.hint = hint;
  }
}

/** 401 时清空本地密钥并回登录页（ui-spec §3）。 */
function handleUnauthorized(): void {
  setAdminKey("");
  if (!window.location.pathname.startsWith("/login") && !window.location.pathname.startsWith("/setup")) {
    window.location.assign("/login");
  }
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Content-Type", "application/json");
  const key = getAdminKey();
  if (key) {
    headers.set("Authorization", `Bearer ${key}`);
  }
  // 管理 API 一律禁用浏览器缓存：否则写后回读可能命中启发式缓存，UI 看起来"没生效"。
  const response = await fetch(`/api/v1${path}`, { cache: "no-store", ...init, headers });
  const text = await response.text();
  if (!response.ok) {
    let code = "http_" + response.status;
    let message = text || response.statusText;
    let hint = "";
    try {
      const parsed = JSON.parse(text) as { error?: { code?: string; message?: string; hint?: string } };
      if (parsed.error) {
        code = parsed.error.code ?? code;
        message = parsed.error.message ?? message;
        hint = parsed.error.hint ?? "";
      }
    } catch {
      // 非 JSON 错误体，保留原文
    }
    if (response.status === 401) {
      handleUnauthorized();
    }
    throw new ApiError(response.status, code, message, hint);
  }
  if (!text) {
    return undefined as T;
  }
  return JSON.parse(text) as T;
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: "POST", body: body === undefined ? undefined : JSON.stringify(body) }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: "PUT", body: body === undefined ? undefined : JSON.stringify(body) }),
  del: <T>(path: string) => request<T>(path, { method: "DELETE" }),
};

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 30s 轮询兜底（ui-spec §4）：SSE 仅作加速，不作为唯一数据源。
      refetchInterval: 30_000,
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 5_000,
    },
  },
});
