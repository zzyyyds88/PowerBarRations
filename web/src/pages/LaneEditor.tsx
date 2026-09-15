import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslations } from "use-intl";
import { toast } from "sonner";
import {
  DragDropContext,
  Draggable,
  Droppable,
  type DropResult,
} from "@hello-pangea/dnd";
import { Activity, GripVertical, Plus, RotateCcw, Save, Trash2 } from "lucide-react";
import { deleteLane, saveLane } from "@/api/lanes";
import {
  qk,
  useChannels,
  useInvalidate,
  useLane,
  useLaneHealth,
  useProbeLane,
  useResetCircuits,
} from "@/api/queries";
import type { Lane, LaneConfig, MemberHealth, ProbeResult } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Field, Input, Select, Switch } from "@/components/ui/input";
import { Badge, ErrorBox, Skeleton } from "@/components/common/AsyncState";
import { circuitTone } from "@/components/common/tones";
import { secondsUntil } from "@/lib/utils";

/**
 * 车道编辑器 /lanes/:name（ui-spec §6.4）。
 *
 * 成员列表拖拽排序**即改 priority**：PBR 语义是 priority 数字大者优先，
 * 列表按优先级从高到低展示，所以拖拽后按新顺序重排数组并把 priority 写回为
 * `(total - index) * 10`，随后立即 PUT /lanes/{name} 持久化并失效
 * `qk.lanes` / `qk.lane(name)`（后端回读按 `priority desc, id asc` 排序，
 * 因此重排结果刷新后仍在）。
 *
 * 运行态（熔断 / 冷却倒计时 / 探测中 / 当前成员 / 亲和 / 连续失败 / 滚动成功率）
 * 来自 `useLaneHealth`，SSE 会写同一份 query 缓存。
 */

const MODES = ["failover", "manual", "weighted", "round_robin"] as const;

const MODE_LABEL: Record<string, string> = {
  failover: "modeFailover",
  manual: "modeManual",
  weighted: "modeWeighted",
  round_robin: "modeRoundRobin",
};

const CIRCUIT_LABEL: Record<string, string> = {
  closed: "circuitClosed",
  open: "circuitOpen",
  half_open: "circuitHalfOpen",
};

const CONFIG_KEYS: (keyof LaneConfig)[] = [
  "member_max_attempts",
  "member_retry_interval_seconds",
  "member_non_stream_response_timeout_seconds",
  "member_stream_first_event_timeout_seconds",
  "member_cooldown_seconds",
  "member_affinity_seconds",
];

const DEFAULT_CONFIG: LaneConfig = {
  member_max_attempts: 2,
  member_retry_interval_seconds: 3,
  member_non_stream_response_timeout_seconds: 120,
  member_stream_first_event_timeout_seconds: 30,
  member_cooldown_seconds: 60,
  member_affinity_seconds: 0,
};

/** 编辑器内部成员草稿：overrides 只保留显式覆盖的键。 */
interface MemberDraft {
  uid: string;
  channel: string;
  upstream_model: string;
  public_alias: string;
  priority: number;
  weight: number;
  overrides: Record<string, number>;
}

interface Draft {
  enabled: boolean;
  mode: string;
  active_member: string;
  config: LaneConfig;
  members: MemberDraft[];
}

let uidSeq = 0;
function nextUid(): string {
  uidSeq += 1;
  return `lane-member-${uidSeq}`;
}

/** 成员标签与后端 `memberLabel` 一致：`channel/upstream_model`（空则回落车道名）。 */
function memberLabel(channel: string, upstreamModel: string, laneName: string): string {
  return `${channel}/${upstreamModel || laneName}`;
}

function toDraft(lane: Lane): Draft {
  return {
    enabled: lane.enabled,
    mode: lane.mode,
    active_member: lane.active_member ?? "",
    config: lane.config ?? DEFAULT_CONFIG,
    members: lane.members.map((member) => ({
      uid: nextUid(),
      channel: member.channel,
      upstream_model: member.upstream_model,
      public_alias: member.public_alias ?? "",
      priority: member.priority,
      weight: member.weight,
      overrides: member.overrides ? { ...member.overrides } : {},
    })),
  };
}

export function LaneEditor() {
  const t = useTranslations("lanes");
  const tc = useTranslations("common");
  const params = useParams<{ name: string }>();
  const name = params.name ?? "";
  const navigate = useNavigate();
  const invalidate = useInvalidate();

  const lane = useLane(name);
  const health = useLaneHealth(name);
  const channels = useChannels();
  const probe = useProbeLane();
  const reset = useResetCircuits();

  const [draft, setDraft] = useState<Draft | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  const [probeResults, setProbeResults] = useState<ProbeResult[] | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [, setTick] = useState(0);

  // 冷却 / 亲和倒计时每秒刷新（ui-spec §6.4 验收：熔断成员显示倒计时且刷新后仍在）。
  useEffect(() => {
    const timer = window.setInterval(() => setTick((value) => value + 1), 1000);
    return () => window.clearInterval(timer);
  }, []);

  // 仅在服务端版本（updated_at）变化时重置草稿：30s 轮询每次都会换新的
  // data 对象引用，若无条件重置会把正在编辑/刚拖拽的内容打回去。
  const appliedVersion = useRef<string | number | undefined>(undefined);
  useEffect(() => {
    const data = lane.data;
    if (!data) return;
    const version = data.updated_at || 0;
    if (appliedVersion.current === version) return;
    appliedVersion.current = version;
    setDraft(toDraft(data));
  }, [lane.data]);

  const channelOptions = channels.data?.items ?? [];
  const memberHealth = useMemo(() => {
    const map = new Map<string, MemberHealth>();
    for (const item of health.data?.members ?? []) {
      map.set(item.member, item);
      map.set(memberLabel(item.channel, item.upstream_model, name), item);
    }
    return map;
  }, [health.data, name]);

  const probeMap = useMemo(() => {
    const map = new Map<string, ProbeResult>();
    for (const result of probeResults ?? []) {
      map.set(`${result.channel}/${result.upstream_model}`, result);
    }
    return map;
  }, [probeResults]);

  const payloadOf = (next: Draft) => ({
    name,
    enabled: next.enabled,
    mode: next.mode,
    active_member: next.mode === "manual" ? next.active_member : "",
    config: next.config,
    members: next.members.map((member) => {
      const overrides: Record<string, number> = {};
      for (const key of CONFIG_KEYS) {
        const value = member.overrides[key];
        if (value !== undefined) overrides[key] = value;
      }
      return {
        channel: member.channel,
        upstream_model: member.upstream_model,
        public_alias: member.public_alias,
        priority: member.priority,
        weight: member.weight,
        overrides: Object.keys(overrides).length > 0 ? overrides : undefined,
      };
    }),
  });

  const persist = async (next: Draft, successMessage?: string) => {
    setSaving(true);
    setError(null);
    try {
      const saved = await saveLane(name, payloadOf(next));
      setDraft(toDraft(saved));
      invalidate([qk.lanes, qk.lane(name), qk.laneHealth(name)]);
      if (successMessage) toast.success(successMessage);
      return true;
    } catch (err) {
      setError(err);
      toast.error(err instanceof Error ? err.message : String(err));
      return false;
    } finally {
      setSaving(false);
    }
  };

  const updateMember = (uid: string, patch: Partial<MemberDraft>) => {
    setDraft((current) =>
      current
        ? { ...current, members: current.members.map((m) => (m.uid === uid ? { ...m, ...patch } : m)) }
        : current,
    );
  };

  const addMember = () => {
    setDraft((current) => {
      if (!current) return current;
      const lowest =
        current.members.length > 0 ? Math.min(...current.members.map((m) => m.priority)) : 20;
      return {
        ...current,
        members: [
          ...current.members,
          {
            uid: nextUid(),
            channel: channelOptions[0]?.name ?? "",
            upstream_model: "",
            public_alias: "",
            priority: lowest - 10,
            weight: 1,
            overrides: {},
          },
        ],
      };
    });
  };

  const removeMember = (uid: string) => {
    setDraft((current) =>
      current ? { ...current, members: current.members.filter((m) => m.uid !== uid) } : current,
    );
  };

  /**
   * 拖拽结束 → 立即写回 priority 并持久化。
   * 数组顺序即"越靠前优先级越高"，故 priority = (total - index) * 10，
   * 保持严格递减且与列表顺序一致（后端按 priority desc 回读）。
   */
  const onDragEnd = (result: DropResult) => {
    if (!draft) return;
    const { source, destination } = result;
    if (!destination || destination.index === source.index) return;

    const reordered = Array.from(draft.members);
    const [moved] = reordered.splice(source.index, 1);
    if (!moved) return;
    reordered.splice(destination.index, 0, moved);

    const total = reordered.length;
    const withPriority = reordered.map((member, index) => ({
      ...member,
      priority: (total - index) * 10,
    }));

    const next: Draft = { ...draft, members: withPriority };
    setDraft(next);
    // 拖拽结束立即持久化；失败时回滚到拖拽前的顺序，避免界面与后端 priority 不一致。
    void persist(next, t("prioritySaved")).then((ok) => {
      if (!ok) setDraft(draft);
    });
  };

  const runProbe = async () => {
    setError(null);
    try {
      const data = await probe.mutateAsync(name);
      setProbeResults(data.results);
      invalidate([qk.laneHealth(name)]);
    } catch (err) {
      setError(err);
    }
  };

  const runReset = async () => {
    setError(null);
    try {
      const data = await reset.mutateAsync(name);
      toast.success(t("resetDone", { count: data.reset }));
    } catch (err) {
      setError(err);
    }
  };

  const removeLane = async () => {
    setError(null);
    try {
      await deleteLane(name);
      invalidate([qk.lanes]);
      toast.success(t("deleted", { name }));
      navigate("/lanes", { replace: true });
    } catch (err) {
      setError(err);
    }
  };

  if (lane.isLoading) {
    return (
      <div className="space-y-3">
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <Skeleton rows={5} />
      </div>
    );
  }

  if (lane.error) {
    return (
      <div className="space-y-3">
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <ErrorBox error={lane.error} onRetry={() => void lane.refetch()} />
      </div>
    );
  }

  if (!lane.data || !draft) {
    return (
      <div className="space-y-3">
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <Skeleton rows={3} />
      </div>
    );
  }

  const affinitySeconds = secondsUntil(health.data?.affinity_until);
  const currentMember = health.data?.current_member ?? "";

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-lg font-semibold">{name}</h1>
          <Badge tone="muted">{t(MODE_LABEL[draft.mode] ?? "mode")}</Badge>
          {currentMember ? (
            <Badge tone="info">
              {t("current")}: <span className="ml-1 font-mono">{currentMember}</span>
            </Badge>
          ) : null}
          {affinitySeconds > 0 ? (
            <Badge tone="warn">{t("affinityCountdown", { seconds: affinitySeconds })}</Badge>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={() => navigate("/lanes")}>
            {tc("back")}
          </Button>
          <Button variant="outline" size="sm" disabled={probe.isPending} onClick={() => void runProbe()}>
            <Activity className="h-3.5 w-3.5" />
            {probe.isPending ? t("probing") : t("probe")}
          </Button>
          <Button variant="outline" size="sm" disabled={reset.isPending} onClick={() => void runReset()}>
            <RotateCcw className="h-3.5 w-3.5" />
            {t("resetCircuits")}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setConfirmDelete((value) => !value)}>
            <Trash2 className="h-3.5 w-3.5" />
            {t("deleteLane")}
          </Button>
        </div>
      </div>

      {error ? <ErrorBox error={error} /> : null}

      {confirmDelete ? (
        <div className="rounded-md border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200">
          <p>{t("deleteConfirm", { name })}</p>
          <div className="mt-2 flex gap-2">
            <Button variant="danger" size="sm" onClick={() => void removeLane()}>
              {tc("confirm")}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setConfirmDelete(false)}>
              {tc("cancel")}
            </Button>
          </div>
        </div>
      ) : null}

      {/* 基本设置：启用 / 模式 / manual 指定成员 */}
      <section className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="grid gap-3 md:grid-cols-3">
          <Field label={tc("enabled")}>
            <div className="flex h-8 items-center gap-2">
              <Switch
                checked={draft.enabled}
                onChange={(value) => setDraft({ ...draft, enabled: value })}
                label={tc("enabled")}
              />
              <span className="text-sm text-slate-500 dark:text-slate-400">
                {draft.enabled ? tc("enabled") : tc("disabled")}
              </span>
            </div>
          </Field>
          <Field label={t("mode")}>
            <Select
              value={draft.mode}
              onChange={(event) => setDraft({ ...draft, mode: event.target.value })}
            >
              {MODES.map((item) => (
                <option key={item} value={item}>
                  {t(MODE_LABEL[item])}
                </option>
              ))}
            </Select>
          </Field>
          {draft.mode === "manual" ? (
            <Field label={t("activeMember")}>
              <Select
                value={draft.active_member}
                onChange={(event) => setDraft({ ...draft, active_member: event.target.value })}
              >
                <option value="">{tc("none")}</option>
                {draft.members
                  .filter((member) => member.channel)
                  .map((member) => {
                    const label = memberLabel(member.channel, member.upstream_model, name);
                    return (
                      <option key={member.uid} value={member.public_alias || label}>
                        {member.public_alias || label}
                      </option>
                    );
                  })}
              </Select>
            </Field>
          ) : null}
        </div>

        <div className="mt-4">
          <div className="mb-2 text-xs font-medium text-slate-600 dark:text-slate-300">{t("config")}</div>
          <div className="grid gap-3 md:grid-cols-3">
            {CONFIG_KEYS.map((key) => (
              <Field
                key={key}
                label={t(configLabelKey(key))}
                hint={key === "member_affinity_seconds" ? t("affinityHint") : undefined}
              >
                <Input
                  type="number"
                  value={draft.config[key]}
                  onChange={(event) => {
                    const parsed = Number(event.target.value);
                    setDraft({
                      ...draft,
                      config: { ...draft.config, [key]: Number.isFinite(parsed) ? parsed : 0 },
                    });
                  }}
                />
              </Field>
            ))}
          </div>
        </div>

        <div className="mt-4">
          <Button disabled={saving} onClick={() => void persist(draft, tc("save"))}>
            <Save className="h-4 w-4" />
            {tc("save")}
          </Button>
        </div>
      </section>

      {/* 成员列表：拖拽排序即改 priority */}
      <section className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
          <div>
            <div className="text-sm font-semibold">{t("members")}</div>
            <div className="text-xs text-slate-500 dark:text-slate-400">{t("dragHint")}</div>
          </div>
          <Button variant="outline" size="sm" onClick={addMember}>
            <Plus className="h-3.5 w-3.5" />
            {t("addMember")}
          </Button>
        </div>

        {draft.members.length === 0 ? (
          <p className="rounded-md border border-dashed border-slate-300 p-6 text-center text-sm text-slate-500 dark:border-slate-700 dark:text-slate-400">
            {t("noMembers")}
          </p>
        ) : (
          <DragDropContext onDragEnd={onDragEnd}>
            <Droppable droppableId="lane-members">
              {(provided) => (
                <ul
                  ref={provided.innerRef}
                  {...provided.droppableProps}
                  className="space-y-2"
                >
                  {draft.members.map((member, index) => (
                    <Draggable key={member.uid} draggableId={member.uid} index={index}>
                      {(dragProvided, snapshot) => (
                        <li
                          ref={dragProvided.innerRef}
                          {...dragProvided.draggableProps}
                          className={
                            "rounded-md border border-slate-200 bg-white p-2 dark:border-slate-800 dark:bg-slate-900 " +
                            (snapshot.isDragging ? "shadow-lg" : "")
                          }
                        >
                          <MemberRow
                            laneName={name}
                            member={member}
                            index={index}
                            dragHandleProps={dragProvided.dragHandleProps}
                            channels={channelOptions.map((channel) => channel.name)}
                            health={memberHealth.get(
                              memberLabel(member.channel, member.upstream_model, name),
                            )}
                            probeResult={
                              probeMap.get(
                                memberLabel(member.channel, member.upstream_model, name),
                              ) ?? null
                            }
                            onChange={(patch) => updateMember(member.uid, patch)}
                            onRemove={() => removeMember(member.uid)}
                            canRemove={draft.members.length > 1}
                          />
                        </li>
                      )}
                    </Draggable>
                  ))}
                  {provided.placeholder}
                </ul>
              )}
            </Droppable>
          </DragDropContext>
        )}
      </section>

      {probeResults ? (
        <section className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <div className="mb-2 text-sm font-semibold">{t("probeResults")}</div>
          <ul className="space-y-1 text-xs">
            {probeResults.map((result, index) => (
              <li key={`${result.channel}/${result.upstream_model}/${index}`} className="flex flex-wrap items-center gap-2">
                <Badge tone={result.status === "success" ? "ok" : "err"}>
                  {result.status === "success" ? tc("success") : tc("failed")}
                </Badge>
                <span className="font-mono">
                  {result.channel}/{result.upstream_model}
                </span>
                <span className="text-slate-500 dark:text-slate-400">
                  {t("durationMs", { ms: result.duration_ms })}
                </span>
                {result.error_kind ? (
                  <span className="text-rose-600 dark:text-rose-400">{result.error_kind}</span>
                ) : null}
                {result.msg ? (
                  <span className="text-slate-500 dark:text-slate-400">{result.msg}</span>
                ) : null}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  );
}

function configLabelKey(key: keyof LaneConfig): string {
  switch (key) {
    case "member_max_attempts":
      return "maxAttempts";
    case "member_retry_interval_seconds":
      return "retryInterval";
    case "member_non_stream_response_timeout_seconds":
      return "nonStreamTimeout";
    case "member_stream_first_event_timeout_seconds":
      return "streamFirstEventTimeout";
    case "member_cooldown_seconds":
      return "cooldownSeconds";
    default:
      return "affinitySeconds";
  }
}

/** 单行成员：渠道 / 上游模型 / 别名 / 优先级 / 权重 / 覆盖入口 + 实时状态徽标。 */
function MemberRow({
  laneName,
  member,
  index,
  dragHandleProps,
  channels,
  health,
  probeResult,
  onChange,
  onRemove,
  canRemove,
}: {
  laneName: string;
  member: MemberDraft;
  index: number;
  dragHandleProps: React.HTMLAttributes<HTMLElement> | null | undefined;
  channels: string[];
  health: MemberHealth | undefined;
  probeResult: ProbeResult | null;
  onChange: (patch: Partial<MemberDraft>) => void;
  onRemove: () => void;
  canRemove: boolean;
}) {
  const t = useTranslations("lanes");
  const tc = useTranslations("common");
  const [overridesOpen, setOverridesOpen] = useState(false);

  const cooldown = secondsUntil(health?.cooldown_until);
  const label = memberLabel(member.channel, member.upstream_model, laneName);
  const rate = health?.rolling_success_rate ?? 0;
  // 渠道可能不在前 200 条列表里（或已被删除），保留当前值以免下拉静默改成员。
  const channelChoices =
    member.channel && !channels.includes(member.channel)
      ? [member.channel, ...channels]
      : channels;

  const setOverride = (key: keyof LaneConfig, raw: string) => {
    const next = { ...member.overrides };
    if (raw === "") {
      delete next[key];
    } else {
      const parsed = Number(raw);
      if (Number.isFinite(parsed)) next[key] = parsed;
    }
    onChange({ overrides: next });
  };

  return (
    <div>
      <div className="flex items-start gap-2">
        <span
          {...dragHandleProps}
          className="mt-2 cursor-grab text-slate-400 hover:text-slate-600 dark:hover:text-slate-200"
          aria-label={t("dragHint")}
        >
          <GripVertical className="h-4 w-4" />
        </span>

        <div className="grid min-w-0 flex-1 items-end gap-2 md:grid-cols-[1.2fr_1.2fr_1fr_0.6fr_0.6fr_auto]">
          <Field label={t("channel")}>
            <Select value={member.channel} onChange={(event) => onChange({ channel: event.target.value })}>
              <option value="">{tc("none")}</option>
              {channelChoices.map((channel) => (
                <option key={channel} value={channel}>
                  {channel}
                </option>
              ))}
            </Select>
          </Field>
          <Field label={t("upstreamModel")} hint={t("upstreamModelHint")}>
            <Input
              value={member.upstream_model}
              onChange={(event) => onChange({ upstream_model: event.target.value })}
            />
          </Field>
          <Field label={t("publicAlias")}>
            <Input
              value={member.public_alias}
              onChange={(event) => onChange({ public_alias: event.target.value })}
            />
          </Field>
          <Field label={t("priority")}>
            <Input
              type="number"
              value={member.priority}
              onChange={(event) => onChange({ priority: Number(event.target.value) || 0 })}
            />
          </Field>
          <Field label={t("weight")}>
            <Input
              type="number"
              value={member.weight}
              onChange={(event) => onChange({ weight: Number(event.target.value) || 0 })}
            />
          </Field>
          <Button
            variant="ghost"
            size="icon"
            aria-label={tc("delete")}
            disabled={!canRemove}
            onClick={onRemove}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* 实时状态徽标 + 逐成员运行态 */}
      <div className="mt-2 flex flex-wrap items-center gap-2 pl-6 text-xs">
        {health?.probing ? (
          <Badge tone="info">{t("probing")}</Badge>
        ) : cooldown > 0 ? (
          <Badge tone="warn">{t("cooldownCountdown", { seconds: cooldown })}</Badge>
        ) : (
          <Badge tone={circuitTone(health?.circuit ?? "closed")}>
            {t(CIRCUIT_LABEL[health?.circuit ?? "closed"] ?? "circuitClosed")}
          </Badge>
        )}
        {health?.current ? <Badge tone="ok">{t("current")}</Badge> : null}
        {health && !health.available ? <Badge tone="err">{tc("disabled")}</Badge> : null}
        <span className="text-slate-500 dark:text-slate-400">
          {t("consecutiveFailures")}: {health?.consecutive_failures ?? 0}
        </span>
        <span className="text-slate-500 dark:text-slate-400">
          {t("rollingSuccessRate")}: {(rate * 100).toFixed(0)}%
        </span>
        {probeResult ? (
          <Badge tone={probeResult.status === "success" ? "ok" : "err"}>
            {probeResult.status === "success" ? tc("success") : tc("failed")}
            {` · ${t("durationMs", { ms: probeResult.duration_ms })}`}
            {probeResult.error_kind ? ` · ${probeResult.error_kind}` : ""}
          </Badge>
        ) : null}
        <button
          type="button"
          className="text-slate-500 underline-offset-2 hover:underline dark:text-slate-400"
          onClick={() => setOverridesOpen((value) => !value)}
        >
          {t("overrides")}
          {Object.keys(member.overrides).length > 0
            ? ` (${Object.keys(member.overrides).length})`
            : ""}
        </button>
        <span className="font-mono text-slate-400">{`#${index + 1} ${label}`}</span>
      </div>

      {overridesOpen ? (
        <div className="mt-2 rounded-md border border-slate-200 p-2 dark:border-slate-800">
          <p className="mb-2 text-xs text-slate-500 dark:text-slate-400">{t("overridesHint")}</p>
          <div className="grid gap-2 md:grid-cols-3">
            {CONFIG_KEYS.map((key) => (
              <Field key={key} label={t(configLabelKey(key))}>
                <Input
                  type="number"
                  value={member.overrides[key] ?? ""}
                  placeholder={String(DEFAULT_CONFIG[key])}
                  onChange={(event) => setOverride(key, event.target.value)}
                />
              </Field>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
