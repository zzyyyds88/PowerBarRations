import { getAdminKey } from "@/api/client";
import type { LaneHealth } from "@/api/types";

/**
 * 车道运行态 SSE 的接入层（ui-spec §4）。
 *
 * 放在 api/ 层是刻意的：ui-spec §1 规定"`api/` 只封装对 api-spec 端点的调用；
 * 禁止模块内直连或自造接口"。此前这个 fetch 直接写在 hooks 里，属于绕过 api 层。
 *
 * 注意必须用 fetch + ReadableStream 而不是 EventSource：后者无法带
 * Authorization 头，而管理密钥不能进 URL。
 */
export function openRouteEvents(signal: AbortSignal): Promise<Response> {
  const key = getAdminKey();
  return fetch("/api/v1/route-events", {
    headers: { Authorization: `Bearer ${key}`, Accept: "text/event-stream" },
    signal,
  });
}

/** 解析一条 SSE 数据行里的车道运行态增量。 */
export function parseRouteEvent(payload: string): LaneHealth[] {
  try {
    const parsed = JSON.parse(payload) as { lanes?: LaneHealth[] };
    return parsed.lanes ?? [];
  } catch {
    return [];
  }
}
