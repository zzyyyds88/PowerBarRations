import { useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useTranslations } from "use-intl";
import { useVirtualizer } from "@tanstack/react-virtual";
import { X } from "lucide-react";
import type { RequestLog } from "@/api/types";
import { useChannels, useLanes, useLog, useLogs } from "@/api/queries";
import { Badge, EmptyState, ErrorBox, Loading, Skeleton } from "@/components/common/AsyncState";
import { attemptTone } from "@/components/common/tones";
import { Button } from "@/components/ui/button";
import { Field, Input, Select } from "@/components/ui/input";
import { formatTime } from "@/lib/utils";

/** 列表请求的固定页大小；更长的历史靠时间范围收窄。 */
const PAGE_LIMIT = 100;

/** 后端接受 RFC3339；`datetime-local` 给的是本地时间，转成带时区的 ISO 串。 */
function toRFC3339(value: string): string {
  if (!value) return "";
  const ms = Date.parse(value);
  if (Number.isNaN(ms)) return "";
  return new Date(ms).toISOString();
}

/** RFC3339 → `datetime-local` 的值（本地时间，去掉秒与毫秒）。 */
function toLocalInput(value: string): string {
  if (!value) return "";
  const ms = Date.parse(value);
  if (Number.isNaN(ms)) return "";
  const date = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(
    date.getMinutes(),
  )}`;
}

const FILTER_KEYS = ["lane", "channel", "key", "success", "since", "until"] as const;

/**
 * 请求日志（ui-spec §6.7）。
 *
 * - 筛选齐全：车道 / 渠道 / 令牌 / 成功与否 / 时间范围；**筛选状态进 URL**，
 *   刷新或分享链接都能复现同一组条件。
 * - 列表用 `@tanstack/react-virtual` 虚拟滚动：容器固定高度 + 绝对定位行，
 *   只渲染可视窗口内的记录。
 * - 详情用逐尝试时间线展示 `attempts`，`cooldown`/`circuit_break`/`skipped`
 *   三种"没真的打上游"的状态必须与 `failed` 在颜色上可区分。
 */
export function Logs() {
  const t = useTranslations("logs");
  const tc = useTranslations("common");
  const [searchParams, setSearchParams] = useSearchParams();

  const lanes = useLanes();
  const channels = useChannels();

  // 输入框是草稿态，点"查询"才写回 URL（避免每敲一个字符就打一次后端）。
  const [laneDraft, setLaneDraft] = useState(searchParams.get("lane") ?? "");
  const [channelDraft, setChannelDraft] = useState(searchParams.get("channel") ?? "");
  const [keyDraft, setKeyDraft] = useState(searchParams.get("key") ?? "");
  const [successDraft, setSuccessDraft] = useState(searchParams.get("success") ?? "");
  const [sinceDraft, setSinceDraft] = useState(toLocalInput(searchParams.get("since") ?? ""));
  const [untilDraft, setUntilDraft] = useState(toLocalInput(searchParams.get("until") ?? ""));

  // 草稿态只在"已应用的筛选"变化时跟随（浏览器前进/后退、分享链接）；
  // 点开某行详情只改 `id`，不应把用户还没提交的输入冲掉。
  const appliedSignature = FILTER_KEYS.map((name) => searchParams.get(name) ?? "").join("\u0000");
  // 用 React 官方的"渲染期根据前值重置 state"模式，而不是在 effect 里 setState
  // （后者会被 React Compiler 判为级联渲染）。草稿态只在"已应用的筛选"变化时
  // 跟随（浏览器前进/后退、分享链接）；点开某行详情只改 `id`，不会冲掉未提交输入。
  const [syncedSignature, setSyncedSignature] = useState(appliedSignature);
  if (syncedSignature !== appliedSignature) {
    const [lane, channel, key, success, since, until] = appliedSignature.split("\u0000");
    setSyncedSignature(appliedSignature);
    setLaneDraft(lane);
    setChannelDraft(channel);
    setKeyDraft(key);
    setSuccessDraft(success);
    setSinceDraft(toLocalInput(since));
    setUntilDraft(toLocalInput(until));
  }

  const queryParams = useMemo(() => {
    const params = new URLSearchParams();
    for (const name of FILTER_KEYS) {
      const value = searchParams.get(name);
      if (value) params.set(name, value);
    }
    params.set("limit", String(PAGE_LIMIT));
    return params;
  }, [searchParams]);

  const logs = useLogs(queryParams);
  const items = useMemo(() => logs.data?.items ?? [], [logs.data]);

  const openIdRaw = searchParams.get("id");
  const openId = openIdRaw && /^\d+$/.test(openIdRaw) ? Number(openIdRaw) : null;
  const detail = useLog(openId);

  const apply = () => {
    const next = new URLSearchParams();
    if (laneDraft.trim()) next.set("lane", laneDraft.trim());
    if (channelDraft.trim()) next.set("channel", channelDraft.trim());
    if (keyDraft.trim()) next.set("key", keyDraft.trim());
    if (successDraft) next.set("success", successDraft);
    const since = toRFC3339(sinceDraft);
    if (since) next.set("since", since);
    const until = toRFC3339(untilDraft);
    if (until) next.set("until", until);
    setSearchParams(next);
  };

  const reset = () => {
    setLaneDraft("");
    setChannelDraft("");
    setKeyDraft("");
    setSuccessDraft("");
    setSinceDraft("");
    setUntilDraft("");
    setSearchParams(new URLSearchParams());
  };

  const openDetail = (id: number) => {
    const next = new URLSearchParams(searchParams);
    next.set("id", String(id));
    setSearchParams(next);
  };

  const closeDetail = () => {
    const next = new URLSearchParams(searchParams);
    next.delete("id");
    setSearchParams(next);
  };

  const hasFilters = FILTER_KEYS.some((name) => searchParams.get(name));

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <div className="flex items-center gap-2">
          <span className="text-xs text-slate-500">{t("virtualizedHint")}</span>
          <Button variant="outline" size="sm" onClick={() => void logs.refetch()}>
            {tc("refresh")}
          </Button>
        </div>
      </div>

      <section className="rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          <Field label={t("filterLane")}>
            <Input
              list="logs-lane-options"
              value={laneDraft}
              placeholder={t("anyOption")}
              onChange={(event) => setLaneDraft(event.target.value)}
            />
            <datalist id="logs-lane-options">
              {(lanes.data?.items ?? []).map((lane) => (
                <option key={lane.name} value={lane.name} />
              ))}
            </datalist>
          </Field>

          <Field label={t("filterChannel")}>
            <Input
              list="logs-channel-options"
              value={channelDraft}
              placeholder={t("anyOption")}
              onChange={(event) => setChannelDraft(event.target.value)}
            />
            <datalist id="logs-channel-options">
              {(channels.data?.items ?? []).map((channel) => (
                <option key={channel.name} value={channel.name} />
              ))}
            </datalist>
          </Field>

          <Field label={t("filterKey")}>
            <Input
              value={keyDraft}
              placeholder={t("anyOption")}
              onChange={(event) => setKeyDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") apply();
              }}
            />
          </Field>

          <Field label={t("filterSuccess")}>
            <Select value={successDraft} onChange={(event) => setSuccessDraft(event.target.value)}>
              <option value="">{t("all")}</option>
              <option value="true">{t("onlySuccess")}</option>
              <option value="false">{t("onlyFailed")}</option>
            </Select>
          </Field>

          <Field label={t("filterSince")} hint={t("timeHint")}>
            <Input
              type="datetime-local"
              value={sinceDraft}
              onChange={(event) => setSinceDraft(event.target.value)}
            />
          </Field>

          <Field label={t("filterUntil")}>
            <Input
              type="datetime-local"
              value={untilDraft}
              onChange={(event) => setUntilDraft(event.target.value)}
            />
          </Field>
        </div>

        <div className="mt-3 flex items-center gap-2">
          <Button size="sm" onClick={apply}>
            {tc("search")}
          </Button>
          <Button variant="outline" size="sm" onClick={reset} disabled={!hasFilters}>
            {t("resetFilters")}
          </Button>
          {logs.data ? (
            <span className="text-xs text-slate-500">{t("resultCount", { count: items.length })}</span>
          ) : null}
        </div>
      </section>

      <LogTable
        items={items}
        loading={logs.isLoading}
        error={logs.error}
        errorRetry={() => void logs.refetch()}
        hasFilters={hasFilters}
        onOpen={openDetail}
        onClose={closeDetail}
        openId={openId}
      />

      {openId !== null ? (
        <div className="fixed inset-y-0 right-0 z-40 w-[min(620px,94vw)] overflow-auto border-l border-slate-200 bg-white shadow-xl dark:border-slate-800 dark:bg-slate-900">
          <div className="sticky top-0 flex items-center justify-between border-b border-slate-200 bg-white px-4 py-3 dark:border-slate-800 dark:bg-slate-900">
            <h2 className="text-sm font-semibold">
              {t("detail")} #{openId}
            </h2>
            <Button variant="ghost" size="icon" aria-label={tc("close")} onClick={closeDetail}>
              <X className="h-4 w-4" />
            </Button>
          </div>
          <div className="p-4">
            {detail.isLoading ? (
              <Loading />
            ) : detail.isError ? (
              <ErrorBox error={detail.error} onRetry={() => void detail.refetch()} />
            ) : detail.data ? (
              <LogDetail log={detail.data} />
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}

/** 虚拟滚动列表：容器固定高度 + overflow-auto，行绝对定位（ui-spec §6.7）。 */
function LogTable({
  items,
  loading,
  error,
  errorRetry,
  hasFilters,
  onOpen,
  onClose,
  openId,
}: {
  items: RequestLog[];
  loading: boolean;
  error: unknown;
  errorRetry: () => void;
  hasFilters: boolean;
  onOpen: (id: number) => void;
  onClose: () => void;
  openId: number | null;
}) {
  const t = useTranslations("logs");
  const tc = useTranslations("common");
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 40,
    overscan: 12,
  });

  if (loading) return <Skeleton rows={8} />;
  if (error) return <ErrorBox error={error} onRetry={errorRetry} />;
  if (items.length === 0) {
    return <EmptyState message={hasFilters ? t("emptyGuide") : t("emptyGuideNoFilter")} />;
  }

  const gridCols =
    "grid grid-cols-[170px_minmax(140px,1.4fr)_110px_120px_110px_90px_64px_90px_90px_96px] items-center gap-2";

  return (
    <section className="overflow-hidden rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
      {/* 单一横向滚动容器包住表头与列表，保证左右滚动时列对齐 */}
      <div className="overflow-x-auto">
        <div className="min-w-[1180px]">
          <div
            className={`${gridCols} border-b border-slate-200 px-3 py-2 text-xs font-medium text-slate-500 dark:border-slate-800`}
          >
            <span>{t("time")}</span>
            <span>{t("model")}</span>
            <span>{t("filterLane")}</span>
            <span>{t("channel")}</span>
            <span>{t("keyName")}</span>
            <span>{t("status")}</span>
            <span>{t("httpStatus")}</span>
            <span>{t("duration")}</span>
            <span>{t("tokens")}</span>
            <span>{t("cost")}</span>
          </div>

          <div ref={scrollRef} className="h-[60vh] overflow-y-auto">
            <div className="relative" style={{ height: `${virtualizer.getTotalSize()}px` }}>
              {virtualizer.getVirtualItems().map((row) => {
                const log = items[row.index];
                if (!log) return null;
                const active = log.id === openId;
                return (
                  <div
                    key={log.id}
                    data-index={row.index}
                    ref={virtualizer.measureElement}
                    className="absolute left-0 top-0 w-full"
                    style={{ transform: `translateY(${row.start}px)` }}
                  >
                    <button
                      type="button"
                      onClick={() => (active ? onClose() : onOpen(log.id))}
                      className={`${gridCols} w-full px-3 py-2 text-left text-xs transition-colors ${
                        active
                          ? "bg-slate-100 dark:bg-slate-800"
                          : "hover:bg-slate-50 dark:hover:bg-slate-800/60"
                      }`}
                    >
                      <span className="truncate text-slate-600 dark:text-slate-300">
                        {formatTime(Date.parse(log.ts))}
                      </span>
                      <span className="truncate font-mono" title={log.request_model}>
                        {log.request_model}
                      </span>
                      <span className="truncate">{log.lane}</span>
                      <span className="truncate" title={`${log.channel} → ${log.upstream_model}`}>
                        {log.channel}
                      </span>
                      <span className="truncate">{log.key_name}</span>
                      <span>
                        <Badge tone={log.success ? "ok" : "err"}>
                          {log.success ? tc("success") : tc("failed")}
                        </Badge>
                      </span>
                      <span
                        className={`font-mono ${
                          log.http_status >= 400 ? "text-rose-600 dark:text-rose-400" : ""
                        }`}
                      >
                        {log.http_status}
                      </span>
                      <span className="font-mono">{log.total_ms}ms</span>
                      <span className="font-mono">{log.prompt_tokens + log.completion_tokens}</span>
                      <span className="font-mono" title={t("costNote")}>
                        {log.estimated_cost.toFixed(4)}
                      </span>
                    </button>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

/** 逐尝试时间线：一次含逃逸的请求必须能读成 failed → success。 */
function LogDetail({ log }: { log: RequestLog }) {
  const t = useTranslations("logs");
  const tc = useTranslations("common");

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={log.success ? "ok" : "err"}>{log.success ? tc("success") : tc("failed")}</Badge>
        <span className="font-mono text-sm">{log.request_model}</span>
        <Badge tone="muted">HTTP {log.http_status}</Badge>
        <Badge tone="muted">{log.total_ms}ms</Badge>
        <Badge tone="muted">{t("attemptsCount", { count: log.total_attempts })}</Badge>
      </div>

      <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
        <Meta label={t("time")} value={formatTime(Date.parse(log.ts))} />
        <Meta label={t("filterLane")} value={log.lane} />
        <Meta label={t("channel")} value={log.channel} />
        <Meta label={t("upstreamModel")} value={log.upstream_model} />
        <Meta label={t("keyName")} value={log.key_name} />
        <Meta label={t("routeSource")} value={log.route_source} />
        <Meta label={t("inboundFormat")} value={log.inbound_format} />
        <Meta label={t("stream")} value={log.is_stream ? tc("yes") : tc("no")} />
        <Meta label={t("ttft")} value={log.ttft_ms ? `${log.ttft_ms}ms` : "—"} />
        <Meta label={t("promptTokens")} value={String(log.prompt_tokens)} />
        <Meta label={t("completionTokens")} value={String(log.completion_tokens)} />
        <Meta label={t("cacheTokens")} value={`${log.cache_read_tokens} / ${log.cache_write_tokens}`} />
        <Meta label={t("reasoningTokens")} value={String(log.reasoning_tokens)} />
        <Meta label={t("cost")} value={`${log.estimated_cost.toFixed(6)}（${t("costNote")}）`} />
      </dl>

      {log.error_kind || log.error_summary ? (
        <div className="rounded-md border border-rose-200 bg-rose-50 p-3 text-xs text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200">
          <div className="font-medium">{t("errorSummary")}</div>
          {log.error_kind ? <div className="mt-0.5 font-mono">{log.error_kind}</div> : null}
          {log.error_summary ? <div className="mt-1 break-words">{log.error_summary}</div> : null}
        </div>
      ) : null}

      <div>
        <h3 className="mb-2 text-sm font-medium">{t("attemptTimeline")}</h3>
        {log.attempts.length === 0 ? (
          <EmptyState message={t("noAttempts")} />
        ) : (
          <ol className="space-y-2">
            {log.attempts.map((attempt, index) => (
              <li
                key={`${attempt.attempt_num}-${index}`}
                className="rounded-md border border-slate-200 p-2.5 dark:border-slate-800"
              >
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-slate-200 text-[10px] font-medium dark:bg-slate-700">
                    {attempt.attempt_num}
                  </span>
                  <span className="font-mono">{attempt.member}</span>
                  <Badge tone={attemptTone(attempt.status)}>
                    {STATUS_KEYS[attempt.status] ? t(STATUS_KEYS[attempt.status]) : attempt.status}
                  </Badge>
                  <span className="ml-auto font-mono text-slate-500">
                    {attempt.duration_ms === undefined ? "—" : `${attempt.duration_ms}ms`}
                  </span>
                </div>
                {attempt.error_kind ? (
                  <div className="mt-1.5 text-xs">
                    <span className="text-slate-500">{t("attemptErrorKind")}：</span>
                    <span className="font-mono">{attempt.error_kind}</span>
                  </div>
                ) : null}
                {attempt.msg ? (
                  <div className="mt-1 break-words text-xs text-slate-600 dark:text-slate-300">{attempt.msg}</div>
                ) : null}
              </li>
            ))}
          </ol>
        )}
      </div>
    </div>
  );
}

const STATUS_KEYS: Record<string, string> = {
  success: "statusSuccess",
  failed: "statusFailed",
  cooldown: "statusCooldown",
  circuit_break: "statusCircuitBreak",
  skipped: "statusSkipped",
};

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-slate-500">{label}</dt>
      <dd className="truncate font-mono" title={value}>
        {value}
      </dd>
    </div>
  );
}
