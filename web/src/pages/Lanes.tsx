import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslations } from "use-intl";
import { toast } from "sonner";
import { Activity, Plus, Trash2 } from "lucide-react";
import { deleteLane, saveLane } from "@/api/lanes";
import {
  qk,
  useChannels,
  useInvalidate,
  useLaneHealth,
  useLanes,
  useProbeLane,
} from "@/api/queries";
import type { Lane, LaneConfig, ProbeResult } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Field, Input, Select } from "@/components/ui/input";
import { Badge, EmptyState, ErrorBox, Skeleton } from "@/components/common/AsyncState";

/**
 * 车道列表 /lanes（ui-spec §6.3）。
 *
 * 卡片网格：名称 / 模式 / 成员数 / 当前成员 / 异常成员数 + 新建 / 编辑 / 删除 / 探活。
 * 新建表单含名称 + 模式四选一 + 六键 + 成员（含成员级 priority/weight）。
 * 运行态（当前成员、异常成员）来自 `useLaneHealth`，SSE 会写同一份 query 缓存。
 */

const MODES = ["failover", "manual", "weighted", "round_robin"] as const;

const MODE_LABEL: Record<string, string> = {
  failover: "modeFailover",
  manual: "modeManual",
  weighted: "modeWeighted",
  round_robin: "modeRoundRobin",
};

/** 与 model.DefaultLaneRelayConfig 对齐（design-v1 §7.3）。 */
const DEFAULT_CONFIG: LaneConfig = {
  member_max_attempts: 2,
  member_retry_interval_seconds: 3,
  member_non_stream_response_timeout_seconds: 120,
  member_stream_first_event_timeout_seconds: 30,
  member_cooldown_seconds: 60,
  member_affinity_seconds: 0,
};

interface MemberDraft {
  uid: string;
  channel: string;
  upstream_model: string;
  public_alias: string;
  priority: number;
  weight: number;
}

let uidSeq = 0;
function nextUid(): string {
  uidSeq += 1;
  return `member-${uidSeq}`;
}

function newMember(priority: number): MemberDraft {
  return { uid: nextUid(), channel: "", upstream_model: "", public_alias: "", priority, weight: 1 };
}

export function Lanes() {
  const t = useTranslations("lanes");
  const lanes = useLanes();
  const [creating, setCreating] = useState(false);

  const items = lanes.data?.items ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <Button onClick={() => setCreating((value) => !value)}>
          <Plus className="h-4 w-4" />
          {t("new")}
        </Button>
      </div>
      <p className="text-xs text-slate-500 dark:text-slate-400">{t("listHint")}</p>

      {creating ? <CreateLaneForm onClose={() => setCreating(false)} /> : null}

      {lanes.isLoading ? (
        <Skeleton rows={4} />
      ) : lanes.error ? (
        <ErrorBox error={lanes.error} onRetry={() => void lanes.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState
          message={t("emptyGuide")}
          action={<Button onClick={() => setCreating(true)}>{t("new")}</Button>}
        />
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {items.map((lane) => (
            <LaneCard key={lane.name} lane={lane} />
          ))}
        </div>
      )}
    </div>
  );
}

/** 单张车道卡片：运行态徽标 + 操作（编辑 / 删除 / 探活）。 */
function LaneCard({ lane }: { lane: Lane }) {
  const t = useTranslations("lanes");
  const tc = useTranslations("common");
  const navigate = useNavigate();
  const invalidate = useInvalidate();
  const health = useLaneHealth(lane.name);
  const probe = useProbeLane();
  const [results, setResults] = useState<ProbeResult[] | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const abnormal = (health.data?.members ?? []).filter(
    (member) => member.circuit !== "closed" || !member.available,
  ).length;

  const runProbe = async () => {
    try {
      const data = await probe.mutateAsync(lane.name);
      setResults(data.results);
      invalidate([qk.laneHealth(lane.name)]);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    }
  };

  const remove = async () => {
    setDeleting(true);
    try {
      await deleteLane(lane.name);
      toast.success(t("deleted", { name: lane.name }));
      invalidate([qk.lanes]);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    } finally {
      setDeleting(false);
      setConfirming(false);
    }
  };

  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate font-medium">{lane.name}</div>
          <div className="mt-1 flex flex-wrap items-center gap-1">
            <Badge tone="muted">{t(MODE_LABEL[lane.mode] ?? "mode")}</Badge>
            <Badge tone={lane.enabled ? "ok" : "muted"}>
              {lane.enabled ? tc("enabled") : tc("disabled")}
            </Badge>
          </div>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => navigate(`/lanes/${encodeURIComponent(lane.name)}`)}
        >
          {tc("edit")}
        </Button>
      </div>

      <dl className="mt-3 grid grid-cols-3 gap-2 text-xs">
        <div>
          <dt className="text-slate-500 dark:text-slate-400">{t("memberCount")}</dt>
          <dd className="font-medium">{lane.members.length}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-slate-500 dark:text-slate-400">{t("currentMember")}</dt>
          <dd className="truncate font-mono">{health.data?.current_member || "—"}</dd>
        </div>
        <div>
          <dt className="text-slate-500 dark:text-slate-400">{t("abnormalMembers")}</dt>
          <dd className={abnormal > 0 ? "font-medium text-rose-600 dark:text-rose-400" : "font-medium"}>
            {abnormal}
          </dd>
        </div>
      </dl>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button variant="outline" size="sm" disabled={probe.isPending} onClick={() => void runProbe()}>
          <Activity className="h-3.5 w-3.5" />
          {probe.isPending ? t("probing") : t("probe")}
        </Button>
        <Button variant="ghost" size="sm" onClick={() => setConfirming((value) => !value)}>
          <Trash2 className="h-3.5 w-3.5" />
          {tc("delete")}
        </Button>
      </div>

      {confirming ? (
        <div className="mt-3 rounded-md border border-rose-200 bg-rose-50 p-2 text-xs text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200">
          <p>{t("deleteConfirm", { name: lane.name })}</p>
          <div className="mt-2 flex gap-2">
            <Button variant="danger" size="sm" disabled={deleting} onClick={() => void remove()}>
              {tc("confirm")}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setConfirming(false)}>
              {tc("cancel")}
            </Button>
          </div>
        </div>
      ) : null}

      {results ? (
        <ul className="mt-3 space-y-1 text-xs">
          {results.map((result, index) => (
            <li
              key={`${result.channel}/${result.upstream_model}/${index}`}
              className="flex flex-wrap items-center gap-2"
            >
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
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

/** 新建车道：名称 + 模式四选一 + 六键 + 成员（ui-spec §6.3）。 */
function CreateLaneForm({ onClose }: { onClose: () => void }) {
  const t = useTranslations("lanes");
  const tc = useTranslations("common");
  const invalidate = useInvalidate();
  const channels = useChannels();

  const [name, setName] = useState("");
  const [mode, setMode] = useState<string>("failover");
  const [activeMember, setActiveMember] = useState("");
  const [config, setConfig] = useState<LaneConfig>(DEFAULT_CONFIG);
  const [members, setMembers] = useState<MemberDraft[]>([newMember(10)]);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const channelOptions = channels.data?.items ?? [];

  const updateMember = (uid: string, patch: Partial<MemberDraft>) => {
    setMembers((current) => current.map((member) => (member.uid === uid ? { ...member, ...patch } : member)));
  };

  const addMember = () => {
    setMembers((current) => {
      const lowest = current.length > 0 ? Math.min(...current.map((member) => member.priority)) : 20;
      return [...current, newMember(lowest - 10)];
    });
  };

  const removeMember = (uid: string) => {
    setMembers((current) => current.filter((member) => member.uid !== uid));
  };

  const submit = async () => {
    setError(null);
    const trimmed = name.trim();
    if (!trimmed) {
      setError(new Error(t("nameRequired")));
      return;
    }
    if (members.length === 0) {
      setError(new Error(t("memberRequired")));
      return;
    }
    if (members.some((member) => !member.channel)) {
      setError(new Error(t("memberChannelRequired")));
      return;
    }
    setBusy(true);
    try {
      // PUT /lanes/{name} 是 upsert：成员数组顺序即写库顺序。
      await saveLane(trimmed, {
        name: trimmed,
        enabled: true,
        mode,
        active_member: mode === "manual" ? activeMember : "",
        config,
        members: members.map((member) => ({
          channel: member.channel,
          upstream_model: member.upstream_model,
          public_alias: member.public_alias,
          priority: member.priority,
          weight: member.weight,
        })),
      });
      toast.success(t("created", { name: trimmed }));
      invalidate([qk.lanes, qk.lane(trimmed)]);
      onClose();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <div className="text-sm font-semibold">{t("new")}</div>

      <div className="grid gap-3 md:grid-cols-3">
        <Field label={t("name")}>
          <Input value={name} autoFocus onChange={(event) => setName(event.target.value)} />
        </Field>
        <Field label={t("mode")}>
          <Select value={mode} onChange={(event) => setMode(event.target.value)}>
            {MODES.map((item) => (
              <option key={item} value={item}>
                {t(MODE_LABEL[item])}
              </option>
            ))}
          </Select>
        </Field>
        {mode === "manual" ? (
          <Field label={t("activeMember")}>
            <Select value={activeMember} onChange={(event) => setActiveMember(event.target.value)}>
              <option value="">{tc("none")}</option>
              {members
                .filter((member) => member.channel)
                .map((member) => {
                  const label = `${member.channel}/${member.upstream_model || name}`;
                  return (
                    <option key={member.uid} value={label}>
                      {label}
                    </option>
                  );
                })}
            </Select>
          </Field>
        ) : null}
      </div>

      <div>
        <div className="mb-2 text-xs font-medium text-slate-600 dark:text-slate-300">{t("config")}</div>
        <ConfigFields value={config} onChange={setConfig} />
      </div>

      <div>
        <div className="mb-2 flex items-center justify-between">
          <span className="text-xs font-medium text-slate-600 dark:text-slate-300">{t("members")}</span>
          <Button variant="outline" size="sm" onClick={addMember}>
            <Plus className="h-3.5 w-3.5" />
            {t("addMember")}
          </Button>
        </div>
        <div className="space-y-2">
          {members.map((member) => (
            <div
              key={member.uid}
              className="grid items-end gap-2 rounded-md border border-slate-200 p-2 md:grid-cols-[1.2fr_1.2fr_1fr_0.6fr_0.6fr_auto] dark:border-slate-800"
            >
              <Field label={t("channel")}>
                <Select
                  value={member.channel}
                  onChange={(event) => updateMember(member.uid, { channel: event.target.value })}
                >
                  <option value="">{tc("none")}</option>
                  {channelOptions.map((channel) => (
                    <option key={channel.name} value={channel.name}>
                      {channel.name}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label={t("upstreamModel")} hint={t("upstreamModelHint")}>
                <Input
                  value={member.upstream_model}
                  onChange={(event) => updateMember(member.uid, { upstream_model: event.target.value })}
                />
              </Field>
              <Field label={t("publicAlias")}>
                <Input
                  value={member.public_alias}
                  onChange={(event) => updateMember(member.uid, { public_alias: event.target.value })}
                />
              </Field>
              <Field label={t("priority")}>
                <Input
                  type="number"
                  value={member.priority}
                  onChange={(event) =>
                    updateMember(member.uid, { priority: Number(event.target.value) || 0 })
                  }
                />
              </Field>
              <Field label={t("weight")}>
                <Input
                  type="number"
                  value={member.weight}
                  onChange={(event) => updateMember(member.uid, { weight: Number(event.target.value) || 0 })}
                />
              </Field>
              <Button
                variant="ghost"
                size="icon"
                aria-label={tc("delete")}
                disabled={members.length <= 1}
                onClick={() => removeMember(member.uid)}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          ))}
        </div>
      </div>

      {error ? <ErrorBox error={error} /> : null}

      <div className="flex gap-2">
        <Button disabled={busy} onClick={() => void submit()}>
          {tc("create")}
        </Button>
        <Button variant="ghost" onClick={onClose}>
          {tc("cancel")}
        </Button>
      </div>
    </div>
  );
}

/** 车道六键表单（两页共用同一组键，这里内联一份）。 */
function ConfigFields({
  value,
  onChange,
}: {
  value: LaneConfig;
  onChange: (next: LaneConfig) => void;
}) {
  const t = useTranslations("lanes");
  const items: { key: keyof LaneConfig; label: string }[] = [
    { key: "member_max_attempts", label: t("maxAttempts") },
    { key: "member_retry_interval_seconds", label: t("retryInterval") },
    { key: "member_non_stream_response_timeout_seconds", label: t("nonStreamTimeout") },
    { key: "member_stream_first_event_timeout_seconds", label: t("streamFirstEventTimeout") },
    { key: "member_cooldown_seconds", label: t("cooldownSeconds") },
    { key: "member_affinity_seconds", label: t("affinitySeconds") },
  ];

  return (
    <div className="grid gap-3 md:grid-cols-3">
      {items.map((item) => (
        <Field
          key={item.key}
          label={item.label}
          hint={item.key === "member_affinity_seconds" ? t("affinityHint") : undefined}
        >
          <Input
            type="number"
            value={value[item.key]}
            onChange={(event) => {
              const parsed = Number(event.target.value);
              onChange({ ...value, [item.key]: Number.isFinite(parsed) ? parsed : 0 } as LaneConfig);
            }}
          />
        </Field>
      ))}
    </div>
  );
}
