/** 资源类型（与 api-spec-v1 §4 对齐）。 */

export interface Channel {
  name: string;
  type: string;
  base_url: string;
  priority: number;
  weight: number;
  models: string[];
  param_override?: unknown;
  enabled: boolean;
  proxy: string;
  key_set: boolean;
  key_prefix: string;
  created_at: string;
  updated_at: string;
}

export interface LaneMember {
  channel: string;
  upstream_model: string;
  public_alias?: string;
  priority: number;
  weight: number;
  overrides?: Record<string, number>;
}

export interface LaneConfig {
  member_max_attempts: number;
  member_retry_interval_seconds: number;
  member_non_stream_response_timeout_seconds: number;
  member_stream_first_event_timeout_seconds: number;
  member_cooldown_seconds: number;
  member_affinity_seconds: number;
}

export interface Lane {
  name: string;
  enabled: boolean;
  mode: string;
  active_member?: string;
  config: LaneConfig;
  members: LaneMember[];
  created_at: string;
  updated_at: string;
}

export interface ModelSummary {
  model: string;
  source: "implicit" | "explicit";
  member_count: number;
}

export interface RouteDetail {
  model: string;
  source: string;
  mode: string;
  config?: LaneConfig;
  members: { channel: string; upstream_model: string; priority: number; weight: number; public_alias?: string }[];
}

export interface MemberHealth {
  member: string;
  channel: string;
  upstream_model: string;
  circuit: "closed" | "open" | "half_open";
  consecutive_failures: number;
  failure_score: number;
  rolling_success_rate: number;
  cooldown_until: number;
  circuit_open_until: number;
  last_error_kind?: string;
  current: boolean;
  probing: boolean;
  available: boolean;
}

export interface LaneHealth {
  lane: string;
  source: string;
  mode: string;
  current_member: string;
  probe_member: string;
  affinity_until: number;
  members: MemberHealth[];
  events: { ts: number; type: string; member: string; detail?: string }[];
}

export interface ProbeResult {
  channel: string;
  upstream_model: string;
  status: "success" | "failed";
  duration_ms: number;
  error_kind?: string;
  msg?: string;
  ok: boolean;
  status_code: number;
}

export interface Attempt {
  attempt_num: number;
  member: string;
  status: string;
  /** routing-spec §9：每条尝试都带耗时，0 也要出现（非 omitempty）。 */
  duration_ms: number;
  error_kind?: string;
  msg?: string;
}

/** POST /logs/prune 的响应（api-spec §5.6）：dry_run 只报将删除的条数。 */
export interface PruneResult {
  dry_run?: boolean;
  would_delete?: number;
  deleted?: number;
  retention_days?: number;
  before?: string;
}

export interface RequestLog {
  id: number;
  ts: string;
  lane: string;
  request_model: string;
  route_source: string;
  channel: string;
  upstream_model: string;
  key_name: string;
  inbound_format: string;
  success: boolean;
  http_status: number;
  error_kind?: string;
  error_summary?: string;
  prompt_tokens: number;
  completion_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  reasoning_tokens: number;
  ttft_ms: number;
  total_ms: number;
  is_stream: boolean;
  attempts: Attempt[];
  total_attempts: number;
  estimated_cost: number;
}

export interface StatBucket {
  bucket_ts: number;
  group: string;
  requests: number;
  successes: number;
  failures: number;
  prompt_tokens: number;
  completion_tokens: number;
  estimated_cost: number;
}

export interface ClientKey {
  name: string;
  enabled: boolean;
  lane_policy: { mode: string; allow_lanes: string[]; deny_lanes: string[] };
  ip_allowlist: string[];
  rate_limit_rpm: number;
  max_concurrency: number;
  expires_at: string | null;
  notes: string;
  key_prefix: string;
  last_used_at: string | null;
  created_at?: string;
  updated_at?: string;
}

/**
 * 单价表条目（api-spec §6.9 / design-v1 §16.9#7）：人民币 / 百万 token。
 *
 * 四个价格字段都可省略或为 0（= 该口径不折算）；模型不在表里就完全不折算
 * （`estimated_cost = 0`）。这张表**只用于日志的成本折算展示**：不参与准入、
 * 不扣任何额度（G7「只看不扣」）。计价键是请求模型名。
 */
export interface ModelPrice {
  model: string;
  input?: number;
  output?: number;
  cache_read?: number;
  cache_write?: number;
}

export interface SystemOptions {
  circuit_failure_threshold: number;
  circuit_open_seconds: number;
  circuit_max_open_seconds: number;
  log_retention_days: number;
  probe_concurrency: number;
  automatic_enable_channel_enabled: boolean;
  automatic_disable_channel_enabled: boolean;
  automatic_disable_keywords: string[];
  /** 单价表：整表替换语义（PUT 传完整数组，传 `[]` 即清空）。 */
  model_prices: ModelPrice[];
}

export interface AuditEntry {
  id: number;
  ts: string;
  actor: string;
  action: string;
  resource: string;
  name: string;
  before_digest: string;
  after_digest: string;
  dry_run: boolean;
}

export interface ListResponse<T> {
  items: T[];
  next_cursor: string | null;
}

export interface Capabilities {
  api_version: string;
  lane_modes: string[];
  adapters: string[];
  inbound_formats: string[];
  circuit: { states: string[]; settings: { FailureThreshold: number; OpenSeconds: number; MaxOpenSeconds: number } };
}

export interface ImportDiffList {
  add: string[];
  update: string[];
  unchanged: string[];
  remove: string[];
  skipped: string[];
}

export interface ImportResult {
  dry_run: boolean;
  valid: boolean;
  diff: {
    channels?: ImportDiffList;
    lanes?: ImportDiffList;
    keys?: ImportDiffList;
    options?: { changed: string[] };
  };
  warnings?: string[];
}
