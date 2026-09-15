import { api } from "@/api/client";
import type { Lane } from "@/api/types";

/** 车道写操作（ui-spec §1：端点封装只住 api/ 层）。 */
export function saveLane(name: string, payload: unknown): Promise<Lane> {
  return api.put<Lane>(`/lanes/${encodeURIComponent(name)}`, payload);
}

export function deleteLane(name: string): Promise<unknown> {
  return api.del(`/lanes/${encodeURIComponent(name)}`);
}
