import { useNow } from "@/hooks/useNow";
import { useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslations } from "use-intl";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { deleteChannel, updateChannel } from "@/api/channels";
import type { Channel, StatBucket } from "@/api/types";
import {
  qk,
  useCapabilities,
  useChannels,
  useInvalidate,
  useStats,
  useSyncModels,
  useTestChannel,
} from "@/api/queries";
import { Button } from "@/components/ui/button";
import { Field, Input, Select, Switch, Textarea } from "@/components/ui/input";
import { Badge, EmptyState, ErrorBox, Skeleton } from "@/components/common/AsyncState";

/**
 * 渠道 `/channels`（ui-spec §6.5）。
 *
 * 渠道是"模型声明即自动成链"的入口：models 写进来就立刻可路由。
 * 三件必须做对的事：
 * 1. key 只写不读——列表/详情只显示 `key_prefix`，编辑留空即保留原值；
 * 2. 从上游拉取模型必须先 dry-run 看差异，确认后才落库；
 * 3. 删除被显式车道引用的渠道时后端返回 409，必须把引用清单摆出来而不是吞成通用错误。
 */

/** 上游同步的 dry-run 响应（api-spec §5.3）。 */
interface SyncModelsResult {
  dry_run?: boolean;
  valid?: boolean;
  channel: string;
  models?: string[];
  added?: string[];
  removed?: string[];
  diff?: { models?: { add?: string[]; remove?: string[] } };
  // sync-models 的保护性字段：空上游清单或被车道引用的模型会被拦截。
  blocked?: boolean;
  empty_upstream?: boolean;
  referenced_by?: string[];
}

/** 后端聚合未承诺耗时字段；若未来补上，这里做可选读取而不是伪造数字。 */
type StatBucketWithLatency = StatBucket & {
  avg_latency_ms?: number;
  avg_duration_ms?: number;
};

type EditorState = { kind: "create" } | { kind: "edit"; name: string };

type SyncDiff = { channel: string; add: string[]; remove: string[] };

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return String(error);
}

/** 后端 409 message 形如 `channel is referenced by lanes: a, b`，抽出车道清单。 */
function referencedLanes(message: string): string {
  const match = /referenced by lanes:\s*(.+)$/i.exec(message);
  return (match?.[1] ?? message).trim();
}

/** 多值编辑：换行或逗号分隔 → string[]（去空、去重、保序）。 */
function parseModels(text: string): string[] {
  const seen = new Set<string>();
  const models: string[] = [];
  for (const raw of text.split(/[\n,]/)) {
    const model = raw.trim();
    if (!model || seen.has(model)) continue;
    seen.add(model);
    models.push(model);
  }
  return models;
}

function formatOverride(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value !== "object") return "";
  if (Object.keys(value as Record<string, unknown>).length === 0) return "";
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return "";
  }
}

/** 单渠道 Stats：按 `group_by=channel` 取聚合，再过滤出本渠道。 */
function ChannelStats({ name }: { name: string }) {
  const t = useTranslations("channels");
  // 近 7 天窗口。用 useNow 作为"现在"（读 state，避免在 render 里调用
  // Date.now() —— 那属于不纯调用，且会让查询键在每次渲染都变）；每分钟
  // 前移一次锚点，滚动窗口才不会长期停在旧区间。
  const now = useNow(60_000);
  const params = useMemo(() => {
    const to = Math.floor(now / 1000);
    const from = to - 7 * 24 * 3600;
    return new URLSearchParams({
      granularity: "day",
      group_by: "channel",
      from: String(from),
      to: String(to),
    });
  }, [now]);
  const stats = useStats(params);

  if (stats.isLoading) return <Skeleton rows={1} />;
  if (stats.error) return <ErrorBox error={stats.error} onRetry={() => void stats.refetch()} />;

  const buckets = (stats.data?.items ?? []).filter((bucket) => bucket.group === name);
  if (buckets.length === 0) {
    return (
      <p className="rounded-md border border-dashed border-slate-300 p-3 text-xs text-slate-500 dark:border-slate-700 dark:text-slate-400">
        {t("statsEmpty")}
      </p>
    );
  }

  const requests = buckets.reduce((sum, bucket) => sum + bucket.requests, 0);
  const failures = buckets.reduce((sum, bucket) => sum + bucket.failures, 0);
  let latencySum = 0;
  let latencyWeight = 0;
  for (const bucket of buckets) {
    const ext = bucket as StatBucketWithLatency;
    const value = ext.avg_latency_ms ?? ext.avg_duration_ms;
    if (typeof value === "number" && Number.isFinite(value)) {
      latencySum += value * bucket.requests;
      latencyWeight += bucket.requests;
    }
  }
  const latency = latencyWeight > 0 ? Math.round(latencySum / latencyWeight) : null;

  const tiles: { label: string; value: string }[] = [
    { label: t("statsRequests"), value: String(requests) },
    { label: t("statsFailures"), value: String(failures) },
    { label: t("statsAvgLatency"), value: latency === null ? "—" : `${latency}ms` },
  ];

  return (
    <div className="rounded-md border border-slate-200 bg-slate-50 p-2 dark:border-slate-700 dark:bg-slate-950/40">
      <div className="text-xs font-medium text-slate-600 dark:text-slate-300">
        {t("stats")} · {t("statsWindow")}：{t("statsWindow7d")}
      </div>
      <div className="mt-2 grid grid-cols-3 gap-2">
        {tiles.map((tile) => (
          <div key={tile.label} className="rounded border border-slate-200 bg-white p-2 dark:border-slate-700 dark:bg-slate-900">
            <div className="text-xs text-slate-500 dark:text-slate-400">{tile.label}</div>
            <div className="mt-0.5 font-mono text-sm">{tile.value}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

/** 渠道表单（新建 / 编辑共用）。key 留空 = 保留原值。 */
function ChannelForm({
  channel,
  adapters,
  adaptersLoading,
  adaptersError,
  onSaved,
  onCancel,
}: {
  channel: Channel | null;
  adapters: string[];
  adaptersLoading: boolean;
  adaptersError: unknown;
  onSaved: (name: string) => void;
  onCancel: () => void;
}) {
  const t = useTranslations("channels");
  const tc = useTranslations("common");
  const invalidate = useInvalidate();

  const [name, setName] = useState(channel?.name ?? "");
  const [type, setType] = useState(channel?.type ?? "");
  const [baseUrl, setBaseUrl] = useState(channel?.base_url ?? "");
  const [priority, setPriority] = useState(String(channel?.priority ?? 0));
  const [weight, setWeight] = useState(String(channel?.weight ?? 1));
  const [modelsText, setModelsText] = useState((channel?.models ?? []).join("\n"));
  const [key, setKey] = useState("");
  const [overrideText, setOverrideText] = useState(formatOverride(channel?.param_override));
  const [proxy, setProxy] = useState(channel?.proxy ?? "");
  const [enabled, setEnabled] = useState(channel?.enabled ?? true);

  const save = useMutation({
    mutationFn: ({ target, body }: { target: string; body: Record<string, unknown> }) =>
      updateChannel(target, body),
  });

  // 已保存渠道的类型可能不在 /capabilities 列表里（老配置）：保留为可选项，避免静默改写。
  const typeOptions = useMemo(() => {
    const options = [...adapters];
    if (type && !options.includes(type)) options.unshift(type);
    return options;
  }, [adapters, type]);

  const submit = async () => {
    const target = (channel?.name ?? name).trim();
    if (!target) {
      toast.error(t("nameRequired"));
      return;
    }
    if (!type.trim()) {
      toast.error(t("typeRequired"));
      return;
    }
    if (!baseUrl.trim()) {
      toast.error(t("baseUrlRequired"));
      return;
    }
    // param_override 先解析再提交：非法 JSON 绝不能发出去（后端会 400，但本地就该拦住）。
    let parsedOverride: unknown;
    const rawOverride = overrideText.trim();
    if (rawOverride !== "" && rawOverride !== "null") {
      try {
        parsedOverride = JSON.parse(rawOverride);
      } catch {
        toast.error(t("paramOverrideInvalid"));
        return;
      }
    }

    const body: Record<string, unknown> = {
      type: type.trim(),
      base_url: baseUrl.trim(),
      priority: Number.parseInt(priority, 10) || 0,
      weight: Math.max(0, Number.parseInt(weight, 10) || 0),
      models: parseModels(modelsText),
      proxy: proxy.trim(),
      enabled,
    };
    // key 只写：留空即省略，后端保留原值（api-spec §4.1）。
    if (key.trim()) body.key = key.trim();
    if (parsedOverride !== undefined) body.param_override = parsedOverride;

    try {
      await save.mutateAsync({ target, body });
      toast.success(t("saved"));
      await invalidate([qk.channels, qk.channel(target), qk.models]);
      onSaved(target);
    } catch (error) {
      toast.error(t("requestFailed", { message: errorMessage(error) }));
    }
  };

  return (
    <div className="space-y-3 rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900">
      <h2 className="text-sm font-semibold">
        {channel ? t("editTitle", { name: channel.name }) : t("new")}
      </h2>

      {channel ? (
        <div className="flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
          <span>{t("keyPrefix")}:</span>
          <span className="font-mono">{channel.key_prefix || tc("notConfigured")}</span>
        </div>
      ) : null}

      <Field label={t("name")}>
        <Input
          value={name}
          disabled={Boolean(channel)}
          onChange={(event) => setName(event.target.value)}
        />
      </Field>

      <Field label={t("type")}>
        <Select
          value={type}
          disabled={adaptersLoading}
          onChange={(event) => setType(event.target.value)}
        >
          <option value="">{tc("none")}</option>
          {typeOptions.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </Select>
      </Field>
      {adaptersError ? <ErrorBox error={adaptersError} /> : null}

      <Field label={t("baseUrl")}>
        <Input value={baseUrl} onChange={(event) => setBaseUrl(event.target.value)} />
      </Field>

      <div className="grid grid-cols-2 gap-3">
        <Field label={t("priority")} hint={t("priorityHint")}>
          <Input
            inputMode="numeric"
            value={priority}
            onChange={(event) => setPriority(event.target.value)}
          />
        </Field>
        <Field label={t("weight")}>
          <Input
            inputMode="numeric"
            value={weight}
            onChange={(event) => setWeight(event.target.value)}
          />
        </Field>
      </div>

      <Field label={t("models")} hint={t("modelsHint")}>
        <Textarea
          rows={5}
          value={modelsText}
          onChange={(event) => setModelsText(event.target.value)}
        />
      </Field>

      <Field label={t("key")} hint={t("keyHint")}>
        <Input
          type="password"
          autoComplete="off"
          value={key}
          placeholder={channel?.key_prefix ?? ""}
          onChange={(event) => setKey(event.target.value)}
        />
      </Field>

      <Field label={t("paramOverride")} hint={t("paramOverrideHint")}>
        <Textarea
          rows={4}
          value={overrideText}
          onChange={(event) => setOverrideText(event.target.value)}
        />
      </Field>

      <Field label={t("proxy")}>
        <Input value={proxy} onChange={(event) => setProxy(event.target.value)} />
      </Field>

      <div className="flex items-center gap-2">
        <Switch checked={enabled} onChange={setEnabled} label={t("enabled")} />
        <span className="text-sm">{t("enabled")}</span>
      </div>

      <div className="flex items-center gap-2 pt-1">
        <Button disabled={save.isPending} onClick={() => void submit()}>
          {tc("save")}
        </Button>
        <Button variant="outline" onClick={onCancel}>
          {tc("cancel")}
        </Button>
      </div>
    </div>
  );
}

export function Channels() {
  const t = useTranslations("channels");
  const tc = useTranslations("common");
  const channels = useChannels();
  const capabilities = useCapabilities();
  const invalidate = useInvalidate();
  const testChannel = useTestChannel();
  const sync = useSyncModels();

  const [editor, setEditor] = useState<EditorState | null>(null);
  const [statsFor, setStatsFor] = useState<string | null>(null);
  const [diff, setDiff] = useState<SyncDiff | null>(null);
  const [conflict, setConflict] = useState<{ name: string; lanes: string } | null>(null);

  const del = useMutation({
    mutationFn: (name: string) =>
      deleteChannel(name),
  });

  const items = channels.data?.items ?? [];
  const editingChannel =
    editor?.kind === "edit" ? items.find((item) => item.name === editor.name) ?? null : null;

  const runTest = async (channel: Channel) => {
    try {
      const result = await testChannel.mutateAsync({ name: channel.name });
      const values = {
        status: String(result.status_code),
        ms: String(Math.round(result.duration_ms)),
      };
      if (result.status === "success") {
        toast.success(t("testOk", values));
      } else {
        const suffix = result.error_kind ? ` (${result.error_kind})` : "";
        toast.error(t("testFail", values) + suffix);
      }
    } catch (error) {
      toast.error(t("requestFailed", { message: errorMessage(error) }));
    }
  };

  const pullModels = async (name: string) => {
    try {
      const result = (await sync.mutateAsync({ name, dryRun: true })) as SyncModelsResult;
      const add = result.diff?.models?.add ?? result.added ?? [];
      const remove = result.diff?.models?.remove ?? result.removed ?? [];
      if (add.length === 0 && remove.length === 0) {
        setDiff(null);
        toast.info(t("syncNoChange"));
        return;
      }
      // 保护性拦截：空上游清单（防误清空）或被车道成员引用的模型（防摘掉在用成员）。
      if (result.blocked) {
        const reason = result.empty_upstream
          ? t("syncBlockedEmpty")
          : t("syncBlockedReferenced", { refs: (result.referenced_by ?? []).join("; ") });
        toast.warning(reason);
      }
      // 先看差异再确认：不直接落库。
      setDiff({ channel: name, add, remove });
    } catch (error) {
      toast.error(t("requestFailed", { message: errorMessage(error) }));
    }
  };

  const applySync = async () => {
    if (!diff) return;
    try {
      const result = (await sync.mutateAsync({ name: diff.channel, dryRun: false })) as SyncModelsResult;
      toast.success(
        t("syncApplied", {
          added: String((result.added ?? []).length),
          removed: String((result.removed ?? []).length),
        }),
      );
      setDiff(null);
    } catch (error) {
      toast.error(t("requestFailed", { message: errorMessage(error) }));
    }
  };

  const removeChannel = async (channel: Channel) => {
    if (!window.confirm(t("deleteConfirm", { name: channel.name }))) return;
    setConflict(null);
    try {
      await del.mutateAsync(channel.name);
      toast.success(t("deleted"));
      invalidate([qk.channels, qk.channel(channel.name), qk.models]);
      if (editor?.kind === "edit" && editor.name === channel.name) setEditor(null);
    } catch (error) {
      // 409：把后端给出的引用清单展示出来，用户才能自助解阻（ui-spec §6.5 验收）。
      if (error instanceof ApiError && error.status === 409) {
        const lanes = referencedLanes(error.message);
        setConflict({ name: channel.name, lanes });
        toast.error(t("deleteReferenced", { lanes }));
        return;
      }
      toast.error(t("requestFailed", { message: errorMessage(error) }));
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void channels.refetch()}>
            {tc("refresh")}
          </Button>
          <Button size="sm" onClick={() => setEditor({ kind: "create" })}>
            {t("new")}
          </Button>
        </div>
      </div>

      {conflict ? (
        <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="font-medium">{t("deleteReferenced", { lanes: conflict.lanes })}</div>
              <div className="mt-1 font-mono text-xs opacity-80">{conflict.name}</div>
            </div>
            <Button variant="outline" size="sm" onClick={() => setConflict(null)}>
              {tc("close")}
            </Button>
          </div>
        </div>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,28rem)]">
        <div className="space-y-3">
          {channels.isLoading ? <Skeleton rows={4} /> : null}
          {channels.error ? (
            <ErrorBox error={channels.error} onRetry={() => void channels.refetch()} />
          ) : null}

          {!channels.isLoading && !channels.error && items.length === 0 ? (
            <EmptyState
              message={t("emptyGuide")}
              action={<Button onClick={() => setEditor({ kind: "create" })}>{t("new")}</Button>}
            />
          ) : null}

          {items.map((channel) => {
            const testing = testChannel.isPending && testChannel.variables?.name === channel.name;
            const deleting = del.isPending && del.variables === channel.name;
            const syncing = sync.isPending && sync.variables?.name === channel.name;
            return (
              <div
                key={channel.name}
                className="rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{channel.name}</span>
                  <Badge tone="info">{channel.type}</Badge>
                  <Badge tone={channel.enabled ? "ok" : "muted"}>
                    {channel.enabled ? tc("enabled") : tc("disabled")}
                  </Badge>
                  {channel.key_prefix ? (
                    <span className="font-mono text-xs text-slate-500 dark:text-slate-400">
                      {channel.key_prefix}…
                    </span>
                  ) : (
                    <Badge tone="warn">{tc("notConfigured")}</Badge>
                  )}
                </div>

                <div className="mt-1 truncate font-mono text-xs text-slate-500 dark:text-slate-400">
                  {channel.base_url}
                </div>

                <div className="mt-2 flex flex-wrap items-center gap-3 text-xs text-slate-500 dark:text-slate-400">
                  <span>
                    {t("priority")}: {channel.priority}
                  </span>
                  <span>
                    {t("weight")}: {channel.weight}
                  </span>
                  <span>
                    {t("modelsCount")}: {channel.models.length}
                  </span>
                </div>
                {channel.models.length > 0 ? (
                  <div className="mt-1 truncate text-xs text-slate-500 dark:text-slate-400">
                    {channel.models.join(", ")}
                  </div>
                ) : null}

                <div className="mt-3 flex flex-wrap items-center gap-2">
                  <Button variant="outline" size="sm" disabled={testing} onClick={() => void runTest(channel)}>
                    {t("test")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setStatsFor(statsFor === channel.name ? null : channel.name)}
                  >
                    {t("stats")}
                  </Button>
                  <Button variant="outline" size="sm" disabled={syncing} onClick={() => void pullModels(channel.name)}>
                    {t("syncModels")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setEditor({ kind: "edit", name: channel.name })}
                  >
                    {tc("edit")}
                  </Button>
                  <Button variant="danger" size="sm" disabled={deleting} onClick={() => void removeChannel(channel)}>
                    {tc("delete")}
                  </Button>
                </div>

                {conflict && conflict.name === channel.name ? (
                  <div className="mt-2 rounded-md border border-amber-300 bg-amber-50 p-2 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100">
                    {t("deleteReferenced", { lanes: conflict.lanes })}
                  </div>
                ) : null}

                {diff && diff.channel === channel.name ? (
                  <div className="mt-2 rounded-md border border-slate-200 bg-slate-50 p-2 text-xs dark:border-slate-700 dark:bg-slate-950/40">
                    <div className="font-medium">{t("syncDiff")}</div>
                    <div className="mt-1 flex flex-wrap gap-2">
                      <Badge tone="ok">
                        {t("syncAdded")}: {diff.add.length}
                      </Badge>
                      <Badge tone="err">
                        {t("syncRemoved")}: {diff.remove.length}
                      </Badge>
                    </div>
                    {diff.add.length > 0 ? (
                      <div className="mt-1 break-all font-mono">+ {diff.add.join(", ")}</div>
                    ) : null}
                    {diff.remove.length > 0 ? (
                      <div className="mt-1 break-all font-mono">- {diff.remove.join(", ")}</div>
                    ) : null}
                    <div className="mt-2 flex items-center gap-2">
                      <Button size="sm" disabled={syncing} onClick={() => void applySync()}>
                        {t("syncApply")}
                      </Button>
                      <Button variant="outline" size="sm" onClick={() => setDiff(null)}>
                        {tc("cancel")}
                      </Button>
                    </div>
                  </div>
                ) : null}

                {statsFor === channel.name ? (
                  <div className="mt-2">
                    <ChannelStats name={channel.name} />
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>

        <div>
          {editor && (editor.kind === "create" || editingChannel) ? (
            <ChannelForm
              key={editor.kind === "edit" ? `edit:${editor.name}` : "create"}
              channel={editingChannel}
              adapters={capabilities.data?.adapters ?? []}
              adaptersLoading={capabilities.isLoading}
              adaptersError={capabilities.error}
              onSaved={() => setEditor(null)}
              onCancel={() => setEditor(null)}
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}
