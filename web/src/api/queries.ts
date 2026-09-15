import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type {
  AuditEntry,
  Capabilities,
  Channel,
  ClientKey,
  ImportResult,
  Lane,
  LaneHealth,
  ListResponse,
  ModelSummary,
  ProbeResult,
  RequestLog,
  RouteDetail,
  StatBucket,
  SystemOptions,
} from "@/api/types";

/** 拼查询串；路径本身保持纯字面量，便于契约检查解析。 */
function withQuery(path: string, query: string): string {
  return query ? `${path}?${query}` : path;
}

/** 查询键集中定义，便于写操作后精准失效。 */
export const qk = {
  setupStatus: ["setup-status"] as const,
  capabilities: ["capabilities"] as const,
  channels: ["channels"] as const,
  channel: (name: string) => ["channel", name] as const,
  lanes: ["lanes"] as const,
  lane: (name: string) => ["lane", name] as const,
  laneHealth: (name: string) => ["lane-health", name] as const,
  models: ["models"] as const,
  route: (model: string) => ["route", model] as const,
  logs: (params: string) => ["logs", params] as const,
  log: (id: number) => ["log", id] as const,
  stats: (params: string) => ["stats", params] as const,
  // stats 的前缀键：各项 stats 查询键形如 ["stats", "<查询串>"]，写操作后要失效
  // 全部 stats 查询必须用这个前缀。若误用 qk.stats("") 得到 ["stats", ""]，
  // partialMatchKey 要求第二项字符串相等，永远匹配不到真实键（写后不刷新）。
  statsRoot: ["stats"] as const,
  keys: ["keys"] as const,
  options: ["system-options"] as const,
  audit: ["audit"] as const,
  version: ["version"] as const,
};

export function useSetupStatus() {
  return useQuery({
    queryKey: qk.setupStatus,
    queryFn: () => api.get<{ initialized: boolean }>("/setup/status"),
    retry: 0,
    refetchInterval: false,
  });
}

export function useCapabilities() {
  return useQuery({
    queryKey: qk.capabilities,
    queryFn: () => api.get<Capabilities>("/capabilities"),
    staleTime: 5 * 60_000,
  });
}

export function useChannels() {
  return useQuery({
    queryKey: qk.channels,
    queryFn: () => api.get<ListResponse<Channel>>("/channels?limit=200"),
  });
}

export function useChannel(name: string) {
  return useQuery({
    queryKey: qk.channel(name),
    queryFn: () => api.get<Channel>(`/channels/${encodeURIComponent(name)}`),
    enabled: Boolean(name),
  });
}

export function useLanes() {
  return useQuery({
    queryKey: qk.lanes,
    queryFn: () => api.get<ListResponse<Lane>>("/lanes?limit=200"),
  });
}

export function useLane(name: string) {
  return useQuery({
    queryKey: qk.lane(name),
    queryFn: () => api.get<Lane>(`/lanes/${encodeURIComponent(name)}`),
    enabled: Boolean(name),
  });
}

export function useLaneHealth(name: string) {
  return useQuery({
    queryKey: qk.laneHealth(name),
    queryFn: () => api.get<LaneHealth>(`/lanes/${encodeURIComponent(name)}/health`),
    enabled: Boolean(name),
    // 运行态变化快：比全局 30s 更密一些，SSE 仍作加速
    refetchInterval: 15_000,
  });
}

export function useModels() {
  return useQuery({
    queryKey: qk.models,
    queryFn: () => api.get<ListResponse<ModelSummary>>("/models"),
  });
}

export function useRoute(model: string) {
  return useQuery({
    queryKey: qk.route(model),
    queryFn: () => api.get<RouteDetail>(`/routes/${encodeURIComponent(model)}`),
    enabled: Boolean(model),
  });
}

export function useLogs(params: URLSearchParams) {
  const qs = params.toString();
  return useQuery({
    queryKey: qk.logs(qs),
    queryFn: () => api.get<ListResponse<RequestLog>>(`/logs?${qs}`),
  });
}

export function useLog(id: number | null) {
  return useQuery({
    queryKey: qk.log(id ?? 0),
    queryFn: () => api.get<RequestLog>(`/logs/${id}`),
    enabled: id !== null && id > 0,
  });
}

export function useStats(params: URLSearchParams) {
  const qs = params.toString();
  return useQuery({
    queryKey: qk.stats(qs),
    queryFn: () =>
      api.get<{ granularity: string; group_by: string; items: StatBucket[] }>(`/stats?${qs}`),
  });
}

export function useKeys() {
  return useQuery({
    queryKey: qk.keys,
    queryFn: () => api.get<ListResponse<ClientKey>>("/keys?limit=200"),
  });
}

export function useSystemOptions() {
  return useQuery({
    queryKey: qk.options,
    queryFn: () => api.get<SystemOptions>("/system/options"),
  });
}

export function useAudit() {
  return useQuery({
    queryKey: qk.audit,
    queryFn: () => api.get<ListResponse<AuditEntry>>("/audit?limit=50"),
  });
}

export function useVersion() {
  return useQuery({
    queryKey: qk.version,
    queryFn: () => api.get<{ version: string; started_at?: string; uptime_seconds?: number }>("/version"),
    staleTime: 60_000,
  });
}

/** 写操作后统一失效相关缓存（TanStack Query 的失效即"写后回读"）。 */
export function useInvalidate() {
  const client = useQueryClient();
  // 调用方传"要失效的 query key 列表"，每个元素就是一条 key：invalidate([qk.channels, qk.models])。
  // 注意：qk.channels 本身已是 ["channels"]；若再包一层得到 [["channels"]]，前缀匹配会打空，
  // 写操作后状态不刷新（首启"我已保存，继续"因此卡在管理密钥页）。这里对误包一层做兼容。
  // 返回 Promise，调用方可 await 以确认回读完成。
  return async (keys: readonly (readonly unknown[])[]) => {
    await Promise.all(
      keys.map((key) => {
        const flat = key.length === 1 && Array.isArray(key[0]) ? (key[0] as readonly unknown[]) : key;
        return client.invalidateQueries({ queryKey: flat });
      }),
    );
  };
}

export function useProbeLane() {
  return useMutation({
    mutationFn: (name: string) =>
      api.post<{ lane: string; probed: number; results: ProbeResult[] }>(
        `/lanes/${encodeURIComponent(name)}/probe`,
      ),
  });
}

export function useResetCircuits() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (name: string) =>
      api.post<{ lane: string; reset: number }>(`/lanes/${encodeURIComponent(name)}/circuits/reset`),
    onSuccess: (_data, name) => invalidate([qk.laneHealth(name), qk.lanes]),
  });
}

export function useTestChannel() {
  return useMutation({
    mutationFn: ({ name, model }: { name: string; model?: string }) =>
      api.post<ProbeResult>(
        withQuery(`/channels/${encodeURIComponent(name)}/test`, model ? `model=${encodeURIComponent(model)}` : ""),
      ),
  });
}

export function useSyncModels() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: ({ name, dryRun }: { name: string; dryRun: boolean }) =>
      api.post<{
        dry_run?: boolean;
        valid?: boolean;
        channel: string;
        models?: string[];
        current?: string[];
        remote?: string[];
        added?: string[];
        removed?: string[];
        diff?: { models?: { add?: string[]; remove?: string[] } };
      }>(withQuery(`/channels/${encodeURIComponent(name)}/sync-models`, dryRun ? "dry_run=true" : "")),
    onSuccess: (_data, vars) => {
      if (!vars.dryRun) void invalidate([qk.channels, qk.models]);
    },
  });
}

export function useImportConfig() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: ({ bundle, dryRun }: { bundle: unknown; dryRun: boolean }) =>
      api.post<ImportResult>(withQuery("/import", dryRun ? "dry_run=true" : ""), bundle),
    onSuccess: (_data, vars) => {
      if (!vars.dryRun) {
        void invalidate([qk.channels, qk.lanes, qk.keys, qk.options, qk.models]);
      }
    },
  });
}
