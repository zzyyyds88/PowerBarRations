import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { openRouteEvents, parseRouteEvent } from "@/api/routeEvents";

/**
 * 车道运行态 SSE（ui-spec §4）。
 *
 * 关键约定：
 * - 用 fetch + ReadableStream 读 SSE（统一错误处理与中断控制）；
 *   认证走 HttpOnly 会话 Cookie，由浏览器自动携带，密钥不进 URL；
 * - **断开必须自动重连**，重连后先取一次快照再接受增量；
 * - 另有 30s 轮询兜底（在 queryClient 的 refetchInterval 里配置）。
 */

const RECONNECT_BASE_MS = 1_000;
const RECONNECT_MAX_MS = 30_000;

export function useRouteEvents(onSnapshotNeeded: () => void) {
  const [connected, setConnected] = useState(false);
  const queryClient = useQueryClient();
  const stopped = useRef(false);
  const snapshotRef = useRef(onSnapshotNeeded);
  snapshotRef.current = onSnapshotNeeded;

  useEffect(() => {
    stopped.current = false;
    let timer: number | undefined;
    let attempt = 0;
    // 每次连接一个 AbortController：卸载/重连时中止上一条流，避免悬挂连接。
    let controller = new AbortController();

    const scheduleReconnect = () => {
      if (stopped.current) return;
      // 指数退避，上限 30s，避免网关重启时把服务端打满
      const delay = Math.min(RECONNECT_BASE_MS * 2 ** attempt, RECONNECT_MAX_MS);
      attempt += 1;
      timer = window.setTimeout(() => void run(), delay);
    };

    const run = async (): Promise<void> => {
      if (stopped.current) return;
      controller = new AbortController();
      try {
        const response = await openRouteEvents(controller.signal);
        if (!response.ok || !response.body) {
          setConnected(false);
          scheduleReconnect();
          return;
        }
        setConnected(true);
        attempt = 0;
        // 重连后先取一次快照再接受增量（ui-spec §4）
        snapshotRef.current();

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const chunks = buffer.split("\n\n");
          buffer = chunks.pop() ?? "";
          for (const chunk of chunks) {
            for (const line of chunk.split("\n")) {
              if (!line.startsWith("data:")) continue;
              const payload = line.slice(5).trim();
              if (!payload) continue;
              // 单条解析失败不应中断流
              for (const lane of parseRouteEvent(payload)) {
                queryClient.setQueryData(["lane-health", lane.lane], lane);
              }
            }
          }
        }
        // 正常断流同样要重连（此前只更新异常路径，UI 会一直显示"已连接"）
        setConnected(false);
        scheduleReconnect();
      } catch {
        setConnected(false);
        scheduleReconnect();
      }
    };

    void run();
    return () => {
      stopped.current = true;
      controller.abort();
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [queryClient]);

  return { connected };
}
