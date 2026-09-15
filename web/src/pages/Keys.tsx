import { useNow } from "@/hooks/useNow";
import { useMemo, useState } from "react";
import { useTranslations } from "use-intl";
import { toast } from "sonner";
import { AlertTriangle, KeyRound, Plus, RotateCw, Trash2 } from "lucide-react";
import { createKey, deleteKey, rotateKey, updateKey } from "@/api/keys";
import { qk, useInvalidate, useKeys, useModels, useStats } from "@/api/queries";
import type { ClientKey } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Field, Input, Select, Textarea } from "@/components/ui/input";
import { Badge, EmptyState, ErrorBox, Skeleton } from "@/components/common/AsyncState";
import { CopyButton } from "@/components/common/CopyButton";
import { cn, compactNumber, formatTime } from "@/lib/utils";

/**
 * 令牌面板（ui-spec §6.8）。
 *
 * 三件事是本页的验收核心：
 * 1. 明文只在创建/轮换响应里出现一次——用醒目区块 + 复制按钮展示，关闭后不再持有；
 * 2. 权限编辑必须能在"允许全部车道"与"仅允许指定车道"之间切换（mode 不再是硬编码 all），
 *    两种模式都可叠加拒绝列表；
 * 3. 每把令牌的用量卡（请求数/token/折算成本/最后使用）取自 /stats?group_by=key。
 */

type LanePolicyMode = "all" | "allow";

interface KeyDraft {
  name: string;
  mode: LanePolicyMode;
  allowLanes: string[];
  denyLanes: string[];
  ipAllowlist: string;
  rateLimitRpm: string;
  maxConcurrency: string;
  expiresAt: string;
  notes: string;
}

interface KeyUsage {
  requests: number;
  tokens: number;
  cost: number;
}

/** 用量统计窗口：近 30 天（粒度 day，group_by=key）。 */
const STATS_WINDOW_DAYS = 30;

function emptyDraft(): KeyDraft {
  return {
    name: "",
    mode: "all",
    allowLanes: [],
    denyLanes: [],
    ipAllowlist: "",
    rateLimitRpm: "",
    maxConcurrency: "",
    expiresAt: "",
    notes: "",
  };
}

/** datetime-local 需要 `YYYY-MM-DDTHH:mm` 形式的本地时间。 */
function isoToLocalInput(iso: string | null): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`;
}

function draftFromKey(key: ClientKey): KeyDraft {
  return {
    name: key.name,
    mode: key.lane_policy.mode === "allow" ? "allow" : "all",
    allowLanes: key.lane_policy.allow_lanes ?? [],
    denyLanes: key.lane_policy.deny_lanes ?? [],
    ipAllowlist: (key.ip_allowlist ?? []).join("\n"),
    rateLimitRpm: key.rate_limit_rpm > 0 ? String(key.rate_limit_rpm) : "",
    maxConcurrency: key.max_concurrency > 0 ? String(key.max_concurrency) : "",
    expiresAt: isoToLocalInput(key.expires_at),
    notes: key.notes ?? "",
  };
}

/** 非数字/负数统一收敛成 0（后端 0 = 不限制）。 */
function toInt(raw: string): number {
  const parsed = Number.parseInt(raw, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

/** 表单 → POST/PUT /keys 请求体（后端是字段级补丁，但全量发送同样安全）。 */
function toBody(draft: KeyDraft): Record<string, unknown> {
  return {
    name: draft.name.trim(),
    lane_policy: {
      mode: draft.mode,
      allow_lanes: draft.mode === "allow" ? draft.allowLanes : [],
      deny_lanes: draft.denyLanes,
    },
    ip_allowlist: draft.ipAllowlist
      .split("\n")
      .map((entry) => entry.trim())
      .filter(Boolean),
    rate_limit_rpm: toInt(draft.rateLimitRpm),
    max_concurrency: toInt(draft.maxConcurrency),
    // 空串 = 清除过期时间（后端接受 ""/"null"）
    expires_at: draft.expiresAt ? new Date(draft.expiresAt).toISOString() : "",
    notes: draft.notes,
  };
}

function StatCell({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-slate-200 bg-slate-50 p-2 dark:border-slate-800 dark:bg-slate-950/50">
      <div className="text-xs text-slate-500 dark:text-slate-400">{label}</div>
      <div className="mt-0.5 font-mono text-sm">{value}</div>
    </div>
  );
}

/**
 * 权限多选：候选 = 全部**路由键**（/models 返回的模型名），点击即切换。
 *
 * 关键修正：判定对象是路由键（token-spec §3.2），不是"显式车道"。隐式路由
 * （渠道声明 models 即自动成链）根本没有 lane 记录，只列 useLanes() 会让
 * 没建车道时一个都选不了。此外必须支持手填：模型可能尚未创建，
 * 但令牌要先准备好权限。
 */
function LanePicker({
  candidates,
  selected,
  onToggle,
  emptyLabel,
  addPlaceholder,
  addLabel,
}: {
  candidates: string[];
  selected: string[];
  onToggle: (name: string) => void;
  emptyLabel: string;
  addPlaceholder: string;
  addLabel: string;
}) {
  const [typing, setTyping] = useState("");
  // 已选但不在候选里的（手填的）也要显示出来，否则用户看不到自己加过什么。
  const extra = selected.filter((name) => !candidates.includes(name));
  const all = [...candidates, ...extra];
  const submitTyping = () => {
    const value = typing.trim();
    if (!value) return;
    if (!all.includes(value)) onToggle(value);
    setTyping("");
  };
  return (
    <div className="space-y-1.5">
      {all.length === 0 ? (
        <p className="text-xs text-slate-500 dark:text-slate-400">{emptyLabel}</p>
      ) : (
        <div className="flex flex-wrap gap-1.5 rounded-md border border-slate-200 p-2 dark:border-slate-700">
          {all.map((name) => {
            const on = selected.includes(name);
            return (
              <button
                key={name}
                type="button"
                aria-pressed={on}
                onClick={() => onToggle(name)}
                className={cn(
                  "rounded px-2 py-0.5 text-xs transition-colors",
                  on
                    ? "bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900"
                    : "bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:hover:bg-slate-700",
                )}
              >
                {name}
              </button>
            );
          })}
        </div>
      )}
      <div className="flex gap-2">
        <Input
          value={typing}
          placeholder={addPlaceholder}
          onChange={(event) => setTyping(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              submitTyping();
            }
          }}
        />
        <Button variant="outline" onClick={submitTyping} disabled={typing.trim() === ""}>
          {addLabel}
        </Button>
      </div>
    </div>
  );
}

/** 权限编辑表单：mode 可在 all/allow 间切换，两种模式都能编辑拒绝列表。 */
function KeyForm({
  draft,
  onChange,
  routeKeys,
  busy,
  submitLabel,
  onSubmit,
  onCancel,
  nameLocked,
}: {
  draft: KeyDraft;
  onChange: (draft: KeyDraft) => void;
  routeKeys: string[];
  busy: boolean;
  submitLabel: string;
  onSubmit: () => void;
  onCancel: () => void;
  nameLocked?: boolean;
}) {
  const t = useTranslations("keys");
  const tc = useTranslations("common");

  const toggleLane = (field: "allowLanes" | "denyLanes", name: string) => {
    const list = draft[field];
    onChange({
      ...draft,
      [field]: list.includes(name) ? list.filter((entry) => entry !== name) : [...list, name],
    });
  };

  const pickerField = (
    label: string,
    hint: string,
    field: "allowLanes" | "denyLanes",
  ) => (
    <div>
      <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-300">{label}</span>
      <LanePicker
        candidates={routeKeys}
        selected={draft[field]}
        onToggle={(name) => toggleLane(field, name)}
        emptyLabel={t("noLanes")}
        addPlaceholder={t("addLanePlaceholder")}
        addLabel={tc("add")}
      />
      <span className="mt-1 block text-xs text-slate-500 dark:text-slate-400">{hint}</span>
    </div>
  );

  return (
    <div className="space-y-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <div className="grid gap-3 md:grid-cols-2">
        <Field label={t("name")}>
          <Input
            value={draft.name}
            disabled={nameLocked}
            onChange={(event) => onChange({ ...draft, name: event.target.value })}
          />
        </Field>
        <Field label={t("lanePolicy")}>
          <Select
            value={draft.mode}
            onChange={(event) =>
              onChange({ ...draft, mode: event.target.value === "allow" ? "allow" : "all" })
            }
          >
            <option value="all">{t("allowAll")}</option>
            <option value="allow">{t("allowOnly")}</option>
          </Select>
        </Field>
      </div>

      {draft.mode === "allow" ? pickerField(t("allowLanes"), t("allowLanesHint"), "allowLanes") : null}
      {pickerField(t("denyLanes"), t("denyLanesHint"), "denyLanes")}

      <div className="grid gap-3 md:grid-cols-3">
        <Field label={t("rateLimitRpm")} hint={t("rateLimitHint")}>
          <Input
            inputMode="numeric"
            value={draft.rateLimitRpm}
            onChange={(event) => onChange({ ...draft, rateLimitRpm: event.target.value })}
          />
        </Field>
        <Field label={t("maxConcurrency")} hint={t("concurrencyHint")}>
          <Input
            inputMode="numeric"
            value={draft.maxConcurrency}
            onChange={(event) => onChange({ ...draft, maxConcurrency: event.target.value })}
          />
        </Field>
        <Field label={t("expiresAt")} hint={t("expiresAtHint")}>
          <Input
            type="datetime-local"
            value={draft.expiresAt}
            onChange={(event) => onChange({ ...draft, expiresAt: event.target.value })}
          />
        </Field>
      </div>

      <Field label={t("ipAllowlist")} hint={t("ipAllowlistHint")}>
        <Textarea
          rows={3}
          value={draft.ipAllowlist}
          onChange={(event) => onChange({ ...draft, ipAllowlist: event.target.value })}
        />
      </Field>

      <Field label={t("notes")}>
        <Input value={draft.notes} onChange={(event) => onChange({ ...draft, notes: event.target.value })} />
      </Field>

      <div className="flex items-center gap-2">
        <Button disabled={busy || draft.name.trim() === ""} onClick={onSubmit}>
          {submitLabel}
        </Button>
        <Button variant="ghost" onClick={onCancel}>
          {tc("cancel")}
        </Button>
      </div>
    </div>
  );
}

export function Keys() {
  const t = useTranslations("keys");
  const tc = useTranslations("common");
  const keysQuery = useKeys();
  // 权限判定的对象是**路由键（模型名）**，不是显式车道：隐式路由没有 lane 记录。
  const modelsQuery = useModels();
  const invalidate = useInvalidate();

  // 用量卡数据源：/stats?group_by=key（StatBucket.group 即令牌名）。
  // 用 useNow 作为"现在"（读 state，避免 render 里调 Date.now）
  const now = useNow(60_000);
  const statsParams = useMemo(() => {
    const to = Math.floor(now / 1000);
    const from = to - STATS_WINDOW_DAYS * 24 * 60 * 60;
    const params = new URLSearchParams();
    params.set("granularity", "day");
    params.set("from", String(from));
    params.set("to", String(to));
    params.set("group_by", "key");
    return params;
  }, [now]);
  const statsQuery = useStats(statsParams);

  const [draft, setDraft] = useState<KeyDraft>(emptyDraft);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState<KeyDraft>(emptyDraft);
  const [issued, setIssued] = useState<{ name: string; key: string } | null>(null);
  const [pending, setPending] = useState<{ kind: "rotate" | "delete"; name: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const routeKeys = (modelsQuery.data?.items ?? []).map((item) => item.model);

  const usage = useMemo(() => {
    const map = new Map<string, KeyUsage>();
    for (const bucket of statsQuery.data?.items ?? []) {
      const current = map.get(bucket.group) ?? { requests: 0, tokens: 0, cost: 0 };
      current.requests += bucket.requests;
      current.tokens += bucket.prompt_tokens + bucket.completion_tokens;
      current.cost += bucket.estimated_cost;
      map.set(bucket.group, current);
    }
    return map;
  }, [statsQuery.data]);

  const fail = (error: unknown) => {
    toast.error(error instanceof Error ? error.message : String(error));
  };

  const create = async () => {
    setBusy(true);
    try {
      const created = await createKey(toBody(draft));
      setIssued({ name: created.name, key: created.key ?? "" });
      setDraft(emptyDraft());
      setFormOpen(false);
      invalidate([qk.keys, qk.statsRoot]);
      toast.success(t("createDone", { name: created.name }));
    } catch (error) {
      fail(error);
    } finally {
      setBusy(false);
    }
  };

  const saveEdit = async (name: string) => {
    setBusy(true);
    try {
      await updateKey(name, toBody(editDraft));
      setEditing(null);
      invalidate([qk.keys]);
      toast.success(t("updateDone", { name }));
    } catch (error) {
      fail(error);
    } finally {
      setBusy(false);
    }
  };

  /** 启用/停用：PUT /keys/{name} 只发 {enabled}（字段级补丁，不碰其他字段）。 */
  const toggleEnabled = async (key: ClientKey) => {
    setBusy(true);
    try {
      await updateKey(key.name, { enabled: !key.enabled });
      invalidate([qk.keys]);
      toast.success(t(key.enabled ? "disableDone" : "enableDone", { name: key.name }));
    } catch (error) {
      fail(error);
    } finally {
      setBusy(false);
    }
  };

  const rotate = async (name: string) => {
    setBusy(true);
    try {
      const rotated = await rotateKey(name);
      setIssued({ name, key: rotated.key ?? "" });
      setPending(null);
      invalidate([qk.keys]);
      toast.success(t("rotateDone", { name }));
    } catch (error) {
      fail(error);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (name: string) => {
    setBusy(true);
    try {
      await deleteKey(name);
      setPending(null);
      if (editing === name) setEditing(null);
      invalidate([qk.keys, qk.statsRoot]);
      toast.success(t("deleteDone", { name }));
    } catch (error) {
      fail(error);
    } finally {
      setBusy(false);
    }
  };

  const openCreate = () => {
    setDraft(emptyDraft());
    setFormOpen(true);
  };

  const items = keysQuery.data?.items ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void keysQuery.refetch()}>
            {tc("refresh")}
          </Button>
          <Button size="sm" onClick={openCreate}>
            <Plus className="h-3.5 w-3.5" />
            {t("new")}
          </Button>
        </div>
      </div>

      {issued ? (
        <div className="rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-900 dark:bg-amber-950/40">
          <div className="flex items-center gap-2 font-medium text-amber-900 dark:text-amber-200">
            <AlertTriangle className="h-4 w-4" />
            {t("plainOnce")}
          </div>
          <div className="mt-2 break-all rounded-md border border-amber-200 bg-white p-3 font-mono text-xs dark:border-amber-900 dark:bg-slate-950">
            {issued.key}
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <CopyButton value={issued.key} />
            <span className="text-xs text-amber-800 dark:text-amber-300">{t("plainOnceHint")}</span>
            <Button variant="ghost" size="sm" onClick={() => setIssued(null)}>
              {tc("close")}
            </Button>
          </div>
        </div>
      ) : null}

      {formOpen ? (
        <KeyForm
          draft={draft}
          onChange={setDraft}
          routeKeys={routeKeys}
          busy={busy}
          submitLabel={t("new")}
          onSubmit={() => void create()}
          onCancel={() => setFormOpen(false)}
        />
      ) : null}

      {keysQuery.isLoading ? (
        <Skeleton rows={4} />
      ) : keysQuery.isError ? (
        <ErrorBox error={keysQuery.error} onRetry={() => void keysQuery.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState
          message={t("emptyGuide")}
          action={
            <Button onClick={openCreate}>
              <Plus className="h-3.5 w-3.5" />
              {t("new")}
            </Button>
          }
        />
      ) : (
        <div className="space-y-3">
          {items.map((key) => {
            const stat = usage.get(key.name);
            const policy = key.lane_policy;
            const allowNames = policy.allow_lanes ?? [];
            const denyNames = policy.deny_lanes ?? [];
            const isEditing = editing === key.name;
            return (
              <div
                key={key.name}
                className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900"
              >
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <KeyRound className="h-4 w-4 text-slate-400" />
                      <span className="font-medium">{key.name}</span>
                      <Badge tone={key.enabled ? "ok" : "muted"}>
                        {key.enabled ? tc("enabled") : tc("disabled")}
                      </Badge>
                    </div>
                    <div className="mt-1 font-mono text-xs text-slate-500 dark:text-slate-400">
                      {key.key_prefix}…
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Button variant="outline" size="sm" disabled={busy} onClick={() => void toggleEnabled(key)}>
                      {key.enabled ? t("disable") : t("enable")}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        if (isEditing) {
                          setEditing(null);
                        } else {
                          setEditDraft(draftFromKey(key));
                          setEditing(key.name);
                        }
                      }}
                    >
                      {isEditing ? tc("cancel") : tc("edit")}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setPending({ kind: "rotate", name: key.name })}
                    >
                      <RotateCw className="h-3.5 w-3.5" />
                      {t("rotate")}
                    </Button>
                    <Button
                      variant="danger"
                      size="sm"
                      onClick={() => setPending({ kind: "delete", name: key.name })}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                      {t("delete")}
                    </Button>
                  </div>
                </div>

                {/* 当前策略必须一眼可见：要么"允许全部车道"，要么列出具体车道集合。 */}
                <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
                  <span className="text-slate-500 dark:text-slate-400">{t("lanePolicy")}:</span>
                  <Badge tone={policy.mode === "allow" ? "info" : "ok"}>
                    {policy.mode === "allow" ? t("allowOnly") : t("allowAll")}
                  </Badge>
                  {policy.mode === "allow" ? (
                    allowNames.length > 0 ? (
                      <span className="font-mono text-slate-600 dark:text-slate-300">
                        {allowNames.join(", ")}
                      </span>
                    ) : (
                      <span className="text-slate-500 dark:text-slate-400">{tc("none")}</span>
                    )
                  ) : null}
                  {denyNames.length > 0 ? (
                    <span className="text-rose-600 dark:text-rose-400">
                      {t("denyLanes")}: {denyNames.join(", ")}
                    </span>
                  ) : null}
                </div>

                <div className="mt-3 text-xs font-medium text-slate-500 dark:text-slate-400">
                  {t("usage")} · {t("usage30d")}
                </div>
                <div className="mt-1 grid grid-cols-2 gap-2 md:grid-cols-4">
                  <StatCell label={t("usageRequests")} value={compactNumber(stat?.requests ?? 0)} />
                  <StatCell label={t("usageTokens")} value={compactNumber(stat?.tokens ?? 0)} />
                  <StatCell label={t("usageCost")} value={(stat?.cost ?? 0).toFixed(4)} />
                  <StatCell
                    label={t("lastUsed")}
                    value={key.last_used_at ? formatTime(Date.parse(key.last_used_at)) : tc("never")}
                  />
                </div>

                <div className="mt-2 flex flex-wrap gap-3 text-xs text-slate-500 dark:text-slate-400">
                  <span>
                    {t("rateLimitRpm")}: {key.rate_limit_rpm > 0 ? key.rate_limit_rpm : tc("none")}
                  </span>
                  <span>
                    {t("maxConcurrency")}: {key.max_concurrency > 0 ? key.max_concurrency : tc("none")}
                  </span>
                  <span>
                    {t("expiresAt")}:{" "}
                    {key.expires_at ? formatTime(Date.parse(key.expires_at)) : tc("never")}
                  </span>
                  {key.notes ? (
                    <span>
                      {t("notes")}: {key.notes}
                    </span>
                  ) : null}
                </div>

                {pending && pending.name === key.name ? (
                  <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-900 dark:bg-amber-950/40">
                    <div className="flex items-start gap-2">
                      <AlertTriangle className="mt-0.5 h-4 w-4 text-amber-600" />
                      <div className="flex-1">
                        <div>
                          {pending.kind === "rotate"
                            ? t("rotateConfirm", { name: pending.name })
                            : t("deleteConfirm", { name: pending.name })}
                        </div>
                        <div className="mt-2 flex gap-2">
                          <Button
                            size="sm"
                            variant={pending.kind === "delete" ? "danger" : "default"}
                            disabled={busy}
                            onClick={() => {
                              if (pending.kind === "rotate") {
                                void rotate(pending.name);
                              } else {
                                void remove(pending.name);
                              }
                            }}
                          >
                            {tc("confirm")}
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => setPending(null)}>
                            {tc("cancel")}
                          </Button>
                        </div>
                      </div>
                    </div>
                  </div>
                ) : null}

                {isEditing ? (
                  <div className="mt-3">
                    <KeyForm
                      draft={editDraft}
                      onChange={setEditDraft}
                      routeKeys={routeKeys}
                      busy={busy}
                      nameLocked
                      submitLabel={tc("save")}
                      onSubmit={() => void saveEdit(key.name)}
                      onCancel={() => setEditing(null)}
                    />
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
