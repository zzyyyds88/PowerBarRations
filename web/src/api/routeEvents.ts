import type { LaneHealth } from "@/api/types";

/**
 * 车道运行态 SSE 的接入层（ui-spec §4）。
 *
 * 放在 api/ 层是刻意的：ui-spec §1 规定"`api/` 只封装对 api-spec 端点的调用；
 * 禁止模块内直连或自造接口"。
 *
 * 认证走 HttpOnly 会话 Cookie（token-spec §2.3），由浏览器自动携带，
 * 因此这里**不再需要 Authorization 头**；用 fetch + ReadableStream 仍是为了
 * 统一错误处理与中断控制。
 */
export function openRouteEvents(signal: AbortSignal): Promise<Response> {
	return fetch("/api/v1/route-events", {
		credentials: "same-origin",
		headers: { Accept: "text/event-stream" },
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
