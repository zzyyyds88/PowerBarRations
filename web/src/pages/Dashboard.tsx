import { useNow } from "@/hooks/useNow";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslations } from "use-intl";
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { MemberHealth, StatBucket } from "@/api/types";
import { useLaneHealth, useLanes, useLogs, useStats } from "@/api/queries";
import { Badge, EmptyState, ErrorBox, Skeleton } from "@/components/common/AsyncState";
import { circuitTone } from "@/components/common/tones";
import { Button } from "@/components/ui/button";
import { cn, compactNumber, formatTime, secondsUntil } from "@/lib/utils";

/**
 * Dashboard 概览（ui-spec §6.2）。
 *
 * 数据全部来自 api-spec 端点：`/stats`（总量/曲线/排行）、`/lanes` + `/lanes/{n}/health`
 * （异常车道入口）、`/logs?success=false`（最近失败）。
 * 口径说明：成本是**折算记账**，不参与准入也不扣费（design-v1 §8 / G7），
 * 因此成本卡下必须常驻"仅折算、非计费"标注。
 */

const RANGES = [
  { key: "1h", labelKey: "range1h", seconds: 3_600, granularity: "hour" },
  { key: "24h", labelKey: "range24h", seconds: 86_400, granularity: "hour" },
  { key: "7d", labelKey: "range7d", seconds: 604_800, granularity: "day" },
  { key: "30d", labelKey: "range30d", seconds: 2_592_000, granularity: "day" },
] as const;

type RangeKey = (typeof RANGES)[number]["key"];

/** 概览只探测前若干条车道的运行态，避免车道多时把管理面打满。 */
const MAX_HEALTH_LANES = 20;

/** 折算成本：单价表以人民币、每百万 token 计（design-v1 §16）。 */
function formatCost(value: number): string {
  if (!Number.isFinite(value)) return "¥0.0000";
  return `¥${value.toFixed(4)}`;
}

function isAbnormalMember(member: MemberHealth): boolean {
  return member.circuit !== "closed" || member.cooldown_until > Date.now();
}

function circuitLabelKey(state: MemberHealth["circuit"]): "circuitOpen" | "circuitHalfOpen" | "circuitClosed" {
  if (state === "open") return "circuitOpen";
  if (state === "half_open") return "circuitHalfOpen";
  return "circuitClosed";
}

/** 按分组维度汇总请求数并取 Top N（lane / model 共用）。 */
function topGroups(items: StatBucket[], limit = 5): { name: string; requests: number }[] {
  const byGroup = new Map<string, number>();
  for (const item of items) {
    if (!item.group) continue;
    byGroup.set(item.group, (byGroup.get(item.group) ?? 0) + item.requests);
  }
  return [...byGroup.entries()]
    .map(([name, requests]) => ({ name, requests }))
    .sort((a, b) => b.requests - a.requests)
    .slice(0, limit);
}

function TotalCard({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <div className="text-xs text-slate-500 dark:text-slate-400">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
      {note ? <div className="mt-1 text-xs text-amber-600 dark:text-amber-400">{note}</div> : null}
    </div>
  );
}

function RankCard({
  title,
  items,
  emptyMessage,
}: {
  title: string;
  items: { name: string; requests: number }[];
  emptyMessage: string;
}) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <h2 className="text-sm font-semibold">{title}</h2>
      {items.length === 0 ? (
        <div className="mt-3">
          <EmptyState message={emptyMessage} />
        </div>
      ) : (
        <ol className="mt-3 space-y-2">
          {items.map((item, index) => (
            <li key={item.name} className="flex items-center gap-3 text-sm">
              <span className="w-4 shrink-0 text-right tabular-nums text-slate-400">{index + 1}</span>
              <span className="min-w-0 flex-1 truncate" title={item.name}>
                {item.name}
              </span>
              <span className="shrink-0 font-medium tabular-nums">{compactNumber(item.requests)}</span>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

/** 单条车道运行态探测：仅异常时渲染红色入口，同时把结果上报给父级汇总。 */
function LaneHealthEntry({
  name,
  onReport,
}: {
  name: string;
  onReport: (name: string, abnormal: boolean) => void;
}) {
  const health = useLaneHealth(name);
  const abnormalMembers = useMemo(
    () => (health.data?.members ?? []).filter(isAbnormalMember),
    [health.data],
  );
  const abnormal = abnormalMembers.length > 0;

  useEffect(() => {
    // 查询落定（成功或失败）后上报，避免父级一直停在骨架态
    if (!health.isPending) onReport(name, abnormal);
  }, [health.isPending, abnormal, name, onReport]);

  if (!abnormal) return null;
  return <AbnormalLaneCard name={name} members={abnormalMembers} />;
}

function AbnormalLaneCard({ name, members }: { name: string; members: MemberHealth[] }) {
  // 冷却倒计时需要"现在"，每秒推进一次（读 state，不在 render 里调 Date.now）
  const nowMs = useNow(1000);
  const t = useTranslations("dashboard");
  const tl = useTranslations("lanes");
  const navigate = useNavigate();

  return (
    <div className="rounded-md border border-rose-300 bg-white p-3 dark:border-rose-800 dark:bg-slate-900">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="font-medium">{name}</span>
        <Badge tone="err">
          {tl("abnormalMembers")} · {members.length}
        </Badge>
      </div>
      <ul className="mt-2 space-y-1">
        {members.map((member, index) => (
          <li key={`${member.channel}/${member.upstream_model}#${index}`} className="flex flex-wrap items-center gap-2 text-xs">
            <span className="font-mono">{member.channel}</span>
            <span className="text-slate-500 dark:text-slate-400">{member.upstream_model}</span>
            <Badge tone={circuitTone(member.circuit)}>{tl(circuitLabelKey(member.circuit))}</Badge>
            {member.cooldown_until > nowMs ? (
              <span className="text-amber-600 dark:text-amber-400">
                {tl("cooldownCountdown", { seconds: secondsUntil(member.cooldown_until) })}
              </span>
            ) : null}
          </li>
        ))}
      </ul>
      <div className="mt-2">
        <Button size="sm" variant="outline" onClick={() => navigate(`/lanes/${encodeURIComponent(name)}`)}>
          {t("viewLane")}
        </Button>
      </div>
    </div>
  );
}

/** 异常车道区：有冷却/熔断成员的车道在此醒目列出（ui-spec §6.2 验收）。 */
function AbnormalLanesSection({
  names,
  loading,
  error,
  onRetry,
}: {
  names: string[];
  loading: boolean;
  error: unknown;
  onRetry: () => void;
}) {
  const t = useTranslations("dashboard");
  const [reported, setReported] = useState<Record<string, boolean>>({});

  const report = useCallback((name: string, abnormal: boolean) => {
    setReported((prev) => (prev[name] === abnormal ? prev : { ...prev, [name]: abnormal }));
  }, []);

  const probed = useMemo(() => names.slice(0, MAX_HEALTH_LANES), [names]);
  const abnormalNames = probed.filter((name) => reported[name]);
  const pending = probed.filter((name) => !(name in reported)).length;
  const allChecked = pending === 0;

  if (loading) return <Skeleton rows={2} />;
  if (error) return <ErrorBox error={error} onRetry={onRetry} />;

  return (
    <section
      className={cn(
        "rounded-lg border p-4",
        abnormalNames.length > 0
          ? "border-rose-300 bg-rose-50 dark:border-rose-800 dark:bg-rose-950/30"
          : "border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900",
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold">{t("abnormalLanes")}</h2>
        {abnormalNames.length > 0 ? <Badge tone="err">{abnormalNames.length}</Badge> : null}
      </div>

      {abnormalNames.length === 0 ? (
        <div className="mt-3">
          {allChecked ? <EmptyState message={t("noAbnormalLanes")} /> : <Skeleton rows={2} />}
        </div>
      ) : null}

      <div className="mt-3 grid gap-3 md:grid-cols-2">
        {probed.map((name) => (
          <LaneHealthEntry key={name} name={name} onReport={report} />
        ))}
      </div>

      {names.length > probed.length ? (
        <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
          {t("abnormalScanLimited", { count: probed.length })}
        </p>
      ) : null}
    </section>
  );
}

export function Dashboard() {
  const t = useTranslations("dashboard");
  const tc = useTranslations("common");
  const navigate = useNavigate();

  const [rangeKey, setRangeKey] = useState<RangeKey>("24h");
  const range = RANGES.find((item) => item.key === rangeKey) ?? RANGES[1];

  // 时间窗锚点：每分钟前移，避免长期停留在旧窗口。
  // 用 useNow 而非在 render 里调 Date.now()，也不在 effect 里 setState
  // （两者都会被 React Compiler 判为不纯/级联渲染）。
  const nowMs = useNow(60_000);
  const anchorSec = Math.floor(nowMs / 1000);
  const fromSec = anchorSec - range.seconds;

  const laneStatsParams = useMemo(() => {
    const params = new URLSearchParams({
      granularity: range.granularity,
      from: String(fromSec),
      to: String(anchorSec),
      group_by: "lane",
    });
    return params;
  }, [range.granularity, fromSec, anchorSec]);

  const modelStatsParams = useMemo(() => {
    const params = new URLSearchParams({
      granularity: range.granularity,
      from: String(fromSec),
      to: String(anchorSec),
      group_by: "model",
    });
    return params;
  }, [range.granularity, fromSec, anchorSec]);

  const failureParams = useMemo(() => new URLSearchParams({ success: "false", limit: "10" }), []);

  const laneStats = useStats(laneStatsParams);
  const modelStats = useStats(modelStatsParams);
  const lanes = useLanes();
  const failures = useLogs(failureParams);

  const buckets = useMemo(() => laneStats.data?.items ?? [], [laneStats.data]);

  const totals = useMemo(() => {
    let requests = 0;
    let successes = 0;
    let tokens = 0;
    let cost = 0;
    for (const item of buckets) {
      requests += item.requests;
      successes += item.successes;
      tokens += item.prompt_tokens + item.completion_tokens;
      cost += item.estimated_cost;
    }
    return { requests, successes, tokens, cost };
  }, [buckets]);

  const curve = useMemo(() => {
    const byBucket = new Map<number, number>();
    for (const item of buckets) {
      byBucket.set(item.bucket_ts, (byBucket.get(item.bucket_ts) ?? 0) + item.requests);
    }
    return [...byBucket.entries()]
      .sort((a, b) => a[0] - b[0])
      .map(([ts, requests]) => {
        const at = new Date(ts * 1000);
        const label =
          range.granularity === "day"
            ? `${String(at.getMonth() + 1).padStart(2, "0")}/${String(at.getDate()).padStart(2, "0")}`
            : `${String(at.getHours()).padStart(2, "0")}:00`;
        return { label, requests };
      });
  }, [buckets, range]);

  const laneRank = useMemo(() => topGroups(buckets), [buckets]);
  const modelRank = useMemo(() => topGroups(modelStats.data?.items ?? []), [modelStats.data]);

  const laneNames = useMemo(() => (lanes.data?.items ?? []).map((lane) => lane.name), [lanes.data]);
  const failureItems = failures.data?.items ?? [];
  const successRate = totals.requests > 0 ? (totals.successes / totals.requests) * 100 : 0;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <div className="flex items-center gap-1" role="group" aria-label={t("range")}>
          {RANGES.map((item) => (
            <Button
              key={item.key}
              size="sm"
              variant={item.key === rangeKey ? "default" : "outline"}
              aria-pressed={item.key === rangeKey}
              onClick={() => setRangeKey(item.key)}
            >
              {t(item.labelKey)}
            </Button>
          ))}
        </div>
      </div>

      <AbnormalLanesSection
        names={laneNames}
        loading={lanes.isLoading}
        error={lanes.error}
        onRetry={() => void lanes.refetch()}
      />

      {laneStats.isLoading ? (
        <Skeleton rows={2} />
      ) : laneStats.error ? (
        <ErrorBox error={laneStats.error} onRetry={() => void laneStats.refetch()} />
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <TotalCard label={t("requests")} value={compactNumber(totals.requests)} />
            <TotalCard label={t("successRate")} value={`${successRate.toFixed(1)}%`} />
            <TotalCard label={t("tokens")} value={compactNumber(totals.tokens)} />
            <TotalCard label={t("cost")} value={formatCost(totals.cost)} note={t("costNote")} />
          </div>

          <section className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
            <h2 className="text-sm font-semibold">{t("activity")}</h2>
            {curve.length === 0 ? (
              <div className="mt-3">
                <EmptyState message={tc("empty")} />
              </div>
            ) : (
              <div className="mt-3 h-64 w-full">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={curve} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} />
                    <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={12} />
                    <YAxis tickLine={false} axisLine={false} fontSize={12} allowDecimals={false} width={48} />
                    <Tooltip />
                    <Line
                      type="monotone"
                      dataKey="requests"
                      name={t("requests")}
                      stroke="#0ea5e9"
                      strokeWidth={2}
                      dot={curve.length <= 2 ? { r: 3 } : false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            )}
          </section>
        </>
      )}

      <div className="grid gap-3 md:grid-cols-2">
        <RankCard title={t("rankLanes")} items={laneRank} emptyMessage={tc("empty")} />
        {modelStats.isLoading ? (
          <Skeleton rows={2} />
        ) : modelStats.error ? (
          <ErrorBox error={modelStats.error} onRetry={() => void modelStats.refetch()} />
        ) : (
          <RankCard title={t("rankModels")} items={modelRank} emptyMessage={tc("empty")} />
        )}
      </div>

      <section className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-sm font-semibold">{t("recentFailures")}</h2>
          <Button size="sm" variant="ghost" onClick={() => navigate("/logs?success=false")}>
            {t("goToLogs")}
          </Button>
        </div>
        {failures.isLoading ? (
          <div className="mt-3">
            <Skeleton rows={3} />
          </div>
        ) : failures.error ? (
          <div className="mt-3">
            <ErrorBox error={failures.error} onRetry={() => void failures.refetch()} />
          </div>
        ) : failureItems.length === 0 ? (
          <div className="mt-3">
            <EmptyState message={t("noFailures")} />
          </div>
        ) : (
          <ul className="mt-2 divide-y divide-slate-200 dark:divide-slate-800">
            {failureItems.map((log) => (
              <li key={log.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2 text-sm">
                <span className="shrink-0 text-xs tabular-nums text-slate-500 dark:text-slate-400">
                  {formatTime(Date.parse(log.ts))}
                </span>
                <span className="min-w-0 flex-1 truncate" title={log.request_model}>
                  {log.request_model}
                </span>
                <span
                  className="min-w-0 flex-[2] truncate text-xs text-slate-500 dark:text-slate-400"
                  title={log.error_summary ?? log.error_kind ?? ""}
                >
                  {log.error_summary ?? log.error_kind ?? ""}
                </span>
                <Badge tone="err">{log.error_kind || `HTTP ${log.http_status}`}</Badge>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
