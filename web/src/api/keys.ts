import { api } from "@/api/client";
import type { ClientKey } from "@/api/types";

/** 客户端密钥写操作（ui-spec §1：端点封装只住 api/ 层）。 */

/** 创建：明文只在响应里出现一次（token-spec §3.1）。 */
export function createKey(body: unknown): Promise<ClientKey & { key?: string }> {
  return api.post<ClientKey & { key?: string }>("/keys", body);
}

export function updateKey(name: string, body: unknown): Promise<ClientKey> {
  return api.put<ClientKey>(`/keys/${encodeURIComponent(name)}`, body);
}

/** 轮换：旧明文立即失效，新明文同样只回显一次。 */
export function rotateKey(name: string): Promise<ClientKey & { key?: string }> {
  return api.post<ClientKey & { key?: string }>(`/keys/${encodeURIComponent(name)}/rotate`);
}

export function deleteKey(name: string): Promise<unknown> {
  return api.del(`/keys/${encodeURIComponent(name)}`);
}
