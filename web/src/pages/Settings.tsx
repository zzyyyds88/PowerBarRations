import { useEffect, useMemo, useRef, useState, type ChangeEvent, type ReactNode } from "react";
import { useTranslations } from "use-intl";
import { toast } from "sonner";
import { Plus, Trash2 } from "lucide-react";
import { setSessionFlag } from "@/api/client";
import { logout as logoutRequest } from "@/api/session";
import {
  changeAdminPassword,
  exportConfigBundle,
  pruneRequestLogs,
  updateSystemOptions,
} from "@/api/settings";
import { qk, useImportConfig, useInvalidate, useSystemOptions, useVersion } from "@/api/queries";
import type { ImportDiffList, ImportResult, ModelPrice, SystemOptions } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Field, Input, Select, Switch, Textarea } from "@/components/ui/input";
import { CopyButton } from "@/components/common/CopyButton";
import { EmptyState, ErrorBox, Skeleton } from "@/components/common/AsyncState";
import { useSettingStore, type Locale, type Theme } from "@/stores/setting";

/**
 * 设置（ui-spec §6.9）：账户 / API 密钥 / 外观 / 备份 / 信息 / 单价 / 日志 / 系统 八个分节。
 *
 * 两条容易踩的契约：
 * 1. `PUT /system/options` 是**字段级补丁**（api-spec §6.9）：只发要改的键，未出现的键保持不变。
 *    因此所有 patch 都基于 `useSystemOptions()` 已加载的值构造，绝不凭空拼字段。
 * 2. 保存系统分节时**必须同时带上两个 automatic_* 开关**：后端只对出现过的键落库，
 *    漏掉开关会让"保存关键词"顺手把开关静默重置。
 * 3. 单价分节的 `model_prices` 是**整表替换**（传 `[]` 即清空），因此保存时提交完整数组；
 *    但它仍只是 `PUT /system/options` 的一个键，其余选项原样不动。单价表只用于日志的
 *    成本折算展示，不参与准入、不扣任何额度（design-v1 G7「只看不扣」）。
 */

const ADMIN_KEY_PREFIX_LENGTH = 12;
const CONFIG_FILE_NAME = "pbr-config.json";

/** 车道默认六键（model.DefaultLaneRelayConfig：2/3/120/30/60/0）。 */
const DEFAULT_SIX_KEYS = [
  { label: "maxAttempts", value: 2 },
  { label: "retryInterval", value: 3 },
  { label: "nonStreamTimeout", value: 120 },
  { label: "streamFirstEventTimeout", value: 30 },
  { label: "cooldownSeconds", value: 60 },
  { label: "affinitySeconds", value: 0 },
] as const;

/** 管理密钥派生（token-spec §2.1）：Base64(SHA256(登录口令))，标准 Base64 带填充。 */
async function deriveAdminKey(password: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(password));
  let binary = "";
  for (const byte of new Uint8Array(digest)) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
}

/** 运行时长 → 可读文本（符号单位，与 locale 无关）。 */
function formatUptime(seconds: number | undefined): string {
  if (seconds === undefined || seconds === null) return "—";
  const total = Math.max(0, Math.floor(seconds));
  const days = Math.floor(total / 86_400);
  const hours = Math.floor((total % 86_400) / 3_600);
  const minutes = Math.floor((total % 3_600) / 60);
  const secs = total % 60;
  const parts: string[] = [];
  if (days > 0) parts.push(`${days}d`);
  if (days > 0 || hours > 0) parts.push(`${hours}h`);
  if (days > 0 || hours > 0 || minutes > 0) parts.push(`${minutes}m`);
  parts.push(`${secs}s`);
  return parts.join(" ");
}

function parseKeywords(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

function sameKeywords(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((value, index) => value === b[index]);
}

/** 系统分节草稿：数字以字符串保存，允许用户清空重填。 */
interface SystemDraft {
  autoDisable: boolean;
  autoEnable: boolean;
  failureThreshold: string;
  openSeconds: string;
  maxOpenSeconds: string;
  keywordsText: string;
}

function draftFromOptions(options: SystemOptions): SystemDraft {
  return {
    autoDisable: options.automatic_disable_channel_enabled,
    autoEnable: options.automatic_enable_channel_enabled,
    failureThreshold: String(options.circuit_failure_threshold),
    openSeconds: String(options.circuit_open_seconds),
    maxOpenSeconds: String(options.circuit_max_open_seconds),
    keywordsText: options.automatic_disable_keywords.join("\n"),
  };
}

/** 字段级补丁：两个 automatic_* 开关恒定携带，其余键仅在真的改动时出现。 */
interface SystemPatch {
  automatic_disable_channel_enabled: boolean;
  automatic_enable_channel_enabled: boolean;
  circuit_failure_threshold?: number;
  circuit_open_seconds?: number;
  circuit_max_open_seconds?: number;
  automatic_disable_keywords?: string[];
}

/** 单价行草稿：数字以字符串保存，允许用户清空重填（空 = 该口径不折算）。 */
interface PriceRow {
  id: string;
  model: string;
  input: string;
  output: string;
  cacheRead: string;
  cacheWrite: string;
}

let priceRowSeq = 0;

/** 行标识：删除行会移动索引，用稳定 id 而不是数组下标做 key，免得输入串行。 */
function nextPriceRowId(): string {
  priceRowSeq += 1;
  return `price-row-${priceRowSeq}`;
}

/** 后端回显的 0/缺省一律显示为空串：空串 = 不折算，与后端 omitempty 口径一致。 */
function priceText(value: number | undefined): string {
  return value ? String(value) : "";
}

function priceRows(prices: ModelPrice[]): PriceRow[] {
  return prices.map((price) => ({
    id: nextPriceRowId(),
    model: price.model,
    input: priceText(price.input),
    output: priceText(price.output),
    cacheRead: priceText(price.cache_read),
    cacheWrite: priceText(price.cache_write),
  }));
}

/** 脏检查签名：只比语义值（去首尾空白），"加了行又删掉"能抵消回干净态。 */
function priceSignature(rows: PriceRow[]): string {
  return JSON.stringify(
    rows.map((row) => ({
      model: row.model.trim(),
      input: row.input.trim(),
      output: row.output.trim(),
      cacheRead: row.cacheRead.trim(),
      cacheWrite: row.cacheWrite.trim(),
    })),
  );
}

interface PriceParse {
  value?: number;
  error?: "invalid" | "negative";
}

/** 空串 = 该口径不折算（省略字段）；非法或负数返回 error 交由调用方报错。 */
function parsePriceInput(text: string): PriceParse {
  const trimmed = text.trim();
  if (trimmed === "") return {};
  const value = Number(trimmed);
  if (!Number.isFinite(value)) return { error: "invalid" };
  if (value < 0) return { error: "negative" };
  return { value };
}

/** 单价分节的字段级补丁：整表替换，且只带 model_prices 一个键。 */
interface PricingPatch {
  model_prices: ModelPrice[];
}

/** 单价表列：模型名 + 四个单价 + 删除按钮；窄屏整体横向滚动。 */
const PRICE_GRID =
  "grid min-w-[38rem] grid-cols-[minmax(0,1.6fr)_repeat(4,minmax(0,1fr))_2rem] items-center gap-2";

/** /version 的实际字段比 useVersion 的返回类型更宽：build_time/db_path 由后端返回。 */
interface VersionInfo {
  version: string;
  build_time?: string;
  uptime_seconds?: number;
  db_path?: string;
}

function Section({
  title,
  actions,
  children,
}: {
  title: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-4 py-2.5 dark:border-slate-800">
        <h2 className="text-sm font-medium">{title}</h2>
        {actions}
      </header>
      <div className="space-y-3 p-4">{children}</div>
    </section>
  );
}

function errorText(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

/** 单个资源的 dry-run diff 列表（add/update/unchanged/remove/skipped）。 */
function DiffRow({ label, list }: { label: string; list?: ImportDiffList }) {
  const t = useTranslations("settings");
  if (!list) return null;
  const groups: { label: string; names: string[] }[] = [
    { label: t("diffAdd"), names: list.add ?? [] },
    { label: t("diffUpdate"), names: list.update ?? [] },
    { label: t("diffRemove"), names: list.remove ?? [] },
    { label: t("diffSkipped"), names: list.skipped ?? [] },
    { label: t("diffUnchanged"), names: list.unchanged ?? [] },
  ].filter((group) => group.names.length > 0);
  if (groups.length === 0) return null;
  return (
    <div className="rounded-md border border-slate-200 px-3 py-2 dark:border-slate-800">
      <div className="text-xs font-medium text-slate-600 dark:text-slate-300">{label}</div>
      <ul className="mt-1 space-y-0.5">
        {groups.map((group) => (
          <li key={group.label} className="flex gap-2 text-xs">
            <span className="w-16 shrink-0 text-slate-500 dark:text-slate-400">{group.label}</span>
            <span className="min-w-0 flex-1 break-all font-mono text-slate-700 dark:text-slate-200">
              {group.names.join(", ")}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function Settings() {
  const t = useTranslations("settings");
  const tc = useTranslations("common");
  const tn = useTranslations("nav");
  const tl = useTranslations("lanes");
  const ta = useTranslations("auth");

  const invalidate = useInvalidate();
  const options = useSystemOptions();
  const version = useVersion();
  const importConfig = useImportConfig();

  const theme = useSettingStore((state) => state.theme);
  const locale = useSettingStore((state) => state.locale);
  const setTheme = useSettingStore((state) => state.setTheme);
  const setLocale = useSettingStore((state) => state.setLocale);

  // ---- 账户 ----
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [accountError, setAccountError] = useState<unknown>(null);
  const [accountBusy, setAccountBusy] = useState(false);

  // ---- 管理密钥（供 AI 用）----
  // 浏览器不保存管理密钥（认证走 HttpOnly 会话 Cookie）。需要给 AI 配置时，
  // 由用户在此输入登录口令，本地计算 Base64(SHA256(口令))，口令不离开浏览器。
  const [keyPassword, setKeyPassword] = useState("");
  const [derivedKey, setDerivedKey] = useState("");
  const [keyError, setKeyError] = useState<unknown>(null);
  const [keyBusy, setKeyBusy] = useState(false);

  // ---- 备份 ----
  const [exportBusy, setExportBusy] = useState(false);
  const [importBundle, setImportBundle] = useState<unknown>(null);
  const [importName, setImportName] = useState("");
  const [importDiff, setImportDiff] = useState<ImportResult | null>(null);
  const [importError, setImportError] = useState<unknown>(null);

  // ---- 日志 ----
  const [retention, setRetention] = useState("");
  const [logsError, setLogsError] = useState<unknown>(null);
  const [retentionBusy, setRetentionBusy] = useState(false);
  const [pruneBusy, setPruneBusy] = useState(false);

  // ---- 系统 ----
  const [draft, setDraft] = useState<SystemDraft | null>(null);
  const [systemError, setSystemError] = useState<unknown>(null);
  const [systemBusy, setSystemBusy] = useState(false);
  const seededRef = useRef(false);

  // ---- 单价表 ----
  const [priceDraft, setPriceDraft] = useState<PriceRow[] | null>(null);
  const [priceBaseline, setPriceBaseline] = useState("");
  const [pricingError, setPricingError] = useState<unknown>(null);
  const [pricingBusy, setPricingBusy] = useState(false);
  const priceSeededRef = useRef(false);

  // 只在首次拿到选项时播种草稿：后台 30s 轮询不会覆盖用户未保存的编辑。
  useEffect(() => {
    if (!options.data || seededRef.current) return;
    seededRef.current = true;
    setDraft(draftFromOptions(options.data));
    setRetention(String(options.data.log_retention_days));
  }, [options.data]);

  // 单价表同理：单独一个播种闸门，避免系统草稿的改动状态污染它。
  useEffect(() => {
    if (!options.data || priceSeededRef.current) return;
    priceSeededRef.current = true;
    const rows = priceRows(options.data.model_prices ?? []);
    setPriceDraft(rows);
    setPriceBaseline(priceSignature(rows));
  }, [options.data]);

  /** 只有改动过的行才需要保存：草稿与上次落库/回读的签名不一致时才允许提交。 */
  const pricingDirty = useMemo(
    () => priceDraft !== null && priceSignature(priceDraft) !== priceBaseline,
    [priceDraft, priceBaseline],
  );

  const adminKeyPrefix = useMemo(() => {
    if (!derivedKey) return tc("notConfigured");
    return derivedKey.length > ADMIN_KEY_PREFIX_LENGTH
      ? `${derivedKey.slice(0, ADMIN_KEY_PREFIX_LENGTH)}…`
      : derivedKey;
  }, [derivedKey, tc]);

  const deriveKey = async () => {
    setKeyError(null);
    if (!keyPassword) {
      setKeyError(new Error(ta("passwordRequired")));
      return;
    }
    setKeyBusy(true);
    try {
      setDerivedKey(await deriveAdminKey(keyPassword));
      setKeyPassword("");
    } catch (error) {
      setKeyError(error);
    } finally {
      setKeyBusy(false);
    }
  };

  // 导入 diff 的"真实变更"总数：unchanged 不算变更。
  const importChanges = useMemo(() => {
    const diff = importDiff?.diff;
    if (!diff) return 0;
    let total = 0;
    for (const list of [diff.channels, diff.lanes, diff.keys]) {
      if (!list) continue;
      total += (list.add?.length ?? 0) + (list.update?.length ?? 0) + (list.remove?.length ?? 0);
    }
    total += diff.options?.changed?.length ?? 0;
    return total;
  }, [importDiff]);

  const changePassword = async () => {
    setAccountError(null);
    if (!newPassword) {
      setAccountError(new Error(ta("passwordRequired")));
      return;
    }
    if (newPassword !== confirmPassword) {
      setAccountError(new Error(ta("passwordMismatch")));
      return;
    }
    setAccountBusy(true);
    try {
      await changeAdminPassword(currentPassword, newPassword);
      // 口令变了 ⇒ 管理密钥与旧会话都随之变。服务端已给当前浏览器续签新会话
      // （token-spec §2.3），因此这里只需清空表单与已展示的派生密钥。
      setDerivedKey("");
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      toast.success(t("passwordChanged"));
    } catch (error) {
      setAccountError(error);
    } finally {
      setAccountBusy(false);
    }
  };

  const exportConfig = async () => {
    setExportBusy(true);
    try {
      const bundle = await exportConfigBundle();
      const blob = new Blob([JSON.stringify(bundle, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = CONFIG_FILE_NAME;
      anchor.click();
      URL.revokeObjectURL(url);
      toast.success(t("exported"));
    } catch (error) {
      toast.error(errorText(error, tc("error")));
    } finally {
      setExportBusy(false);
    }
  };

  const pickImportFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setImportError(null);
    setImportDiff(null);
    setImportBundle(null);
    setImportName("");
    try {
      const parsed: unknown = JSON.parse(await file.text());
      setImportName(file.name);
      // 先干跑拿 diff，用户确认后才真正落库（ui-spec §6.9）。
      const result = await importConfig.mutateAsync({ bundle: parsed, dryRun: true });
      setImportBundle(parsed);
      setImportDiff(result);
    } catch (error) {
      setImportError(error);
    }
  };

  const applyImport = async () => {
    if (importBundle === null) return;
    setImportError(null);
    try {
      await importConfig.mutateAsync({ bundle: importBundle, dryRun: false });
      toast.success(t("importApplied"));
      setImportBundle(null);
      setImportDiff(null);
      setImportName("");
    } catch (error) {
      setImportError(error);
    }
  };

  const saveRetention = async () => {
    setLogsError(null);
    const days = Number(retention);
    if (!Number.isFinite(days) || days <= 0 || !Number.isInteger(days)) {
      setLogsError(new Error(t("invalidNumber")));
      return;
    }
    setRetentionBusy(true);
    try {
      // 字段级补丁：只发 log_retention_days，其余选项原样不动。
      const updated = await updateSystemOptions({ log_retention_days: days });
      setRetention(String(updated.log_retention_days));
      invalidate([qk.options]);
      toast.success(t("saved"));
    } catch (error) {
      setLogsError(error);
    } finally {
      setRetentionBusy(false);
    }
  };

  const pruneLogs = async () => {
    setLogsError(null);
    setPruneBusy(true);
    try {
      const preview = await pruneRequestLogs(true);
      const count = preview.would_delete ?? 0;
      toast.message(t("pruneDryRun", { count }));
      if (count <= 0) return;
      if (!window.confirm(t("pruneConfirm"))) return;
      const done = await pruneRequestLogs(false);
      toast.success(t("pruned", { count: done.deleted ?? 0 }));
    } catch (error) {
      setLogsError(error);
    } finally {
      setPruneBusy(false);
    }
  };

  const saveSystem = async () => {
    if (!draft || !options.data) return;
    setSystemError(null);
    const failureThreshold = Number(draft.failureThreshold);
    const openSeconds = Number(draft.openSeconds);
    const maxOpenSeconds = Number(draft.maxOpenSeconds);
    if (!Number.isFinite(failureThreshold) || failureThreshold <= 0) {
      setSystemError(new Error(t("invalidNumber")));
      return;
    }
    if (!Number.isFinite(openSeconds) || openSeconds < 0) {
      setSystemError(new Error(t("invalidNumber")));
      return;
    }
    if (!Number.isFinite(maxOpenSeconds) || maxOpenSeconds <= 0) {
      setSystemError(new Error(t("invalidNumber")));
      return;
    }

    const loaded = options.data;
    const keywords = parseKeywords(draft.keywordsText);
    // 两个开关恒定提交（见文件头注释 2）；其余键只在真的改动时出现。
    const patch: SystemPatch = {
      automatic_disable_channel_enabled: draft.autoDisable,
      automatic_enable_channel_enabled: draft.autoEnable,
    };
    if (failureThreshold !== loaded.circuit_failure_threshold) {
      patch.circuit_failure_threshold = failureThreshold;
    }
    if (openSeconds !== loaded.circuit_open_seconds) {
      patch.circuit_open_seconds = openSeconds;
    }
    if (maxOpenSeconds !== loaded.circuit_max_open_seconds) {
      patch.circuit_max_open_seconds = maxOpenSeconds;
    }
    if (!sameKeywords(keywords, loaded.automatic_disable_keywords)) {
      patch.automatic_disable_keywords = keywords;
    }

    setSystemBusy(true);
    try {
      // 后端回显落库后的完整选项，直接以它回填 = 保存后回读一致。
      const updated = await updateSystemOptions(patch);
      setDraft(draftFromOptions(updated));
      setRetention(String(updated.log_retention_days));
      invalidate([qk.options]);
      toast.success(t("saved"));
    } catch (error) {
      setSystemError(error);
    } finally {
      setSystemBusy(false);
    }
  };

  const info: VersionInfo | undefined = version.data;

  const addPriceRow = () => {
    setPricingError(null);
    setPriceDraft((rows) => [
      ...(rows ?? []),
      { id: nextPriceRowId(), model: "", input: "", output: "", cacheRead: "", cacheWrite: "" },
    ]);
  };

  const updatePriceRow = (id: string, field: Exclude<keyof PriceRow, "id">, value: string) => {
    setPriceDraft((rows) =>
      rows ? rows.map((row) => (row.id === id ? { ...row, [field]: value } : row)) : rows,
    );
  };

  const removePriceRow = (id: string) => {
    setPricingError(null);
    setPriceDraft((rows) => (rows ? rows.filter((row) => row.id !== id) : rows));
  };

  const savePricing = async () => {
    if (!priceDraft) return;
    setPricingError(null);

    // 先在前端做与后端同口径的校验（模型名非空、不重复、价格非负），
    // 免得把一个必然 400 的请求发出去。
    const list: ModelPrice[] = [];
    const seen = new Set<string>();
    for (const row of priceDraft) {
      const model = row.model.trim();
      if (!model) {
        setPricingError(new Error(t("modelRequired")));
        return;
      }
      if (seen.has(model)) {
        setPricingError(new Error(t("duplicateModel", { model })));
        return;
      }
      seen.add(model);

      const parsed = {
        input: parsePriceInput(row.input),
        output: parsePriceInput(row.output),
        cache_read: parsePriceInput(row.cacheRead),
        cache_write: parsePriceInput(row.cacheWrite),
      };
      const bad = Object.values(parsed).find((item) => item.error);
      if (bad) {
        setPricingError(
          new Error(
            bad.error === "negative"
              ? t("negativePrice", { model })
              : t("invalidPrice", { model }),
          ),
        );
        return;
      }

      // 留空与 0 一律不写字段：与后端 omitempty 的回显口径一致（两者都表示不折算）。
      const price: ModelPrice = { model };
      if (parsed.input.value) price.input = parsed.input.value;
      if (parsed.output.value) price.output = parsed.output.value;
      if (parsed.cache_read.value) price.cache_read = parsed.cache_read.value;
      if (parsed.cache_write.value) price.cache_write = parsed.cache_write.value;
      list.push(price);
    }

    // 字段级补丁：只发 model_prices 一个键（值为整表），其余选项不受影响。
    const patch: PricingPatch = { model_prices: list };
    setPricingBusy(true);
    try {
      // 后端回显落库并排序后的完整单价表，直接以它回填 = 保存后回读一致。
      const updated = await updateSystemOptions(patch);
      const rows = priceRows(updated.model_prices ?? list);
      setPriceDraft(rows);
      setPriceBaseline(priceSignature(rows));
      invalidate([qk.options]);
      toast.success(t("saved"));
    } catch (error) {
      setPricingError(error);
    } finally {
      setPricingBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      <h1 className="text-lg font-semibold">{t("title")}</h1>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* 账户 */}
        <Section title={t("account")}>
          <p className="text-xs text-slate-500 dark:text-slate-400">{t("accountHint")}</p>
          <Field label={t("currentPassword")}>
            <Input
              type="password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(event) => setCurrentPassword(event.target.value)}
            />
          </Field>
          <Field label={t("newPassword")} hint={ta("setupHint")}>
            <Input
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
            />
          </Field>
          <Field label={ta("confirmPassword")}>
            <Input
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(event) => setConfirmPassword(event.target.value)}
            />
          </Field>
          {accountError ? <ErrorBox error={accountError} /> : null}
          <Button
            disabled={accountBusy || currentPassword.length === 0 || newPassword.length === 0}
            onClick={() => void changePassword()}
          >
            {t("changePassword")}
          </Button>
        </Section>

        {/* API 密钥（供 AI 用） */}
        <Section title={t("apiKey")}>
          <div className="flex items-center justify-between gap-3 text-sm">
            <span className="text-slate-500 dark:text-slate-400">{t("apiKeyAlgorithm")}</span>
            <code className="font-mono text-xs">Base64(SHA256(login password))</code>
          </div>
          <p className="text-xs text-slate-500 dark:text-slate-400">{t("howToRecompute")}</p>
          <p className="text-xs text-slate-500 dark:text-slate-400">{t("apiKeyLocalHint")}</p>
          <Field label={t("keyPasswordLabel")} hint={t("keyPasswordHint")}>
            <Input
              type="password"
              autoComplete="current-password"
              value={keyPassword}
              onChange={(event) => setKeyPassword(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void deriveKey();
              }}
            />
          </Field>
          {keyError ? <ErrorBox error={keyError} /> : null}
          <Button disabled={keyBusy || keyPassword.length === 0} onClick={() => void deriveKey()}>
            {t("deriveKey")}
          </Button>
          {derivedKey ? (
            <>
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="text-slate-500 dark:text-slate-400">{t("apiKeyPrefix")}</span>
                <code className="break-all font-mono text-xs">{adminKeyPrefix}</code>
              </div>
              <CopyButton value={derivedKey} />
            </>
          ) : null}
          <Button
            variant="outline"
            onClick={() => {
              void logoutRequest().catch(() => undefined).finally(() => {
                setSessionFlag(false);
                window.location.assign("/login");
              });
            }}
          >
            {t("signOut")}
          </Button>
        </Section>

        {/* 外观 */}
        <Section title={t("appearance")}>
          <Field label={t("theme")}>
            <Select value={theme} onChange={(event) => setTheme(event.target.value as Theme)}>
              <option value="light">{t("themeLight")}</option>
              <option value="dark">{t("themeDark")}</option>
              <option value="system">{t("themeSystem")}</option>
            </Select>
          </Field>
          <Field label={t("language")}>
            <Select value={locale} onChange={(event) => setLocale(event.target.value as Locale)}>
              <option value="zh_hans">简体中文</option>
              <option value="zh_hant">繁體中文</option>
              <option value="en">English</option>
            </Select>
          </Field>
        </Section>

        {/* 备份 */}
        <Section title={t("backup")}>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" disabled={exportBusy} onClick={() => void exportConfig()}>
              {t("export")}
            </Button>
            <label className="inline-flex cursor-pointer items-center gap-1.5 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:hover:bg-slate-800">
              {t("importChooseFile")}
              <input
                type="file"
                accept="application/json,.json"
                className="hidden"
                onChange={(event) => void pickImportFile(event)}
              />
            </label>
            {importName ? <span className="font-mono text-xs text-slate-500">{importName}</span> : null}
          </div>

          {importConfig.isPending ? <Skeleton rows={2} /> : null}
          {importError ? <ErrorBox error={importError} /> : null}

          {importDiff ? (
            <div className="space-y-2">
              {!importDiff.valid ? (
                <p className="text-sm text-rose-600 dark:text-rose-400">{t("importInvalid")}</p>
              ) : importChanges === 0 ? (
                <EmptyState message={t("importNoChange")} />
              ) : (
                <>
                  <div className="text-xs font-medium text-slate-600 dark:text-slate-300">
                    {t("importDryRun")}
                  </div>
                  <DiffRow label={tn("channels")} list={importDiff.diff.channels} />
                  <DiffRow label={tn("lanes")} list={importDiff.diff.lanes} />
                  <DiffRow label={tn("keys")} list={importDiff.diff.keys} />
                  {importDiff.diff.options?.changed?.length ? (
                    <DiffRow
                      label={t("diffOptions")}
                      list={{
                        add: [],
                        update: importDiff.diff.options.changed,
                        unchanged: [],
                        remove: [],
                        skipped: [],
                      }}
                    />
                  ) : null}
                </>
              )}

              {importDiff.warnings?.length ? (
                <div className="rounded-md border border-amber-200 bg-amber-50 p-2 text-xs text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
                  <div className="font-medium">{t("importWarnings")}</div>
                  <ul className="mt-1 list-disc pl-4">
                    {importDiff.warnings.map((warning) => (
                      <li key={warning}>{warning}</li>
                    ))}
                  </ul>
                </div>
              ) : null}

              <Button
                disabled={!importDiff.valid || importChanges === 0 || importConfig.isPending}
                onClick={() => void applyImport()}
              >
                {t("importApply")}
              </Button>
            </div>
          ) : null}
        </Section>

        {/* 信息 */}
        <Section title={t("info")}>
          {version.isLoading ? (
            <Skeleton rows={3} />
          ) : version.isError ? (
            <ErrorBox error={version.error} onRetry={() => void version.refetch()} />
          ) : (
            <dl className="space-y-2 text-sm">
              <div className="flex items-center justify-between gap-3">
                <dt className="text-slate-500 dark:text-slate-400">{t("version")}</dt>
                <dd className="font-mono text-xs">{info?.version || tc("notConfigured")}</dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="text-slate-500 dark:text-slate-400">{t("buildTime")}</dt>
                <dd className="font-mono text-xs">{info?.build_time || tc("notConfigured")}</dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="text-slate-500 dark:text-slate-400">{t("uptime")}</dt>
                <dd className="font-mono text-xs">{formatUptime(info?.uptime_seconds)}</dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="text-slate-500 dark:text-slate-400">{t("dbPath")}</dt>
                <dd className="break-all font-mono text-xs">{info?.db_path || tc("notConfigured")}</dd>
              </div>
            </dl>
          )}
        </Section>

        {/* 单价 */}
        <Section
          title={t("pricing")}
          actions={
            priceDraft ? (
              <div className="flex flex-wrap items-center gap-2">
                <Button variant="outline" size="sm" onClick={addPriceRow}>
                  <Plus className="h-3.5 w-3.5" />
                  {t("addModel")}
                </Button>
                <Button
                  size="sm"
                  disabled={pricingBusy || !pricingDirty}
                  onClick={() => void savePricing()}
                >
                  {tc("save")}
                </Button>
              </div>
            ) : undefined
          }
        >
          {options.isLoading ? (
            <Skeleton rows={3} />
          ) : options.isError ? (
            <ErrorBox error={options.error} onRetry={() => void options.refetch()} />
          ) : !priceDraft ? (
            <Skeleton rows={3} />
          ) : (
            <>
              <p className="text-xs text-slate-500 dark:text-slate-400">{t("pricingHint")}</p>
              <p className="text-xs text-slate-500 dark:text-slate-400">{t("pricingSaveHint")}</p>

              {priceDraft.length === 0 ? (
                <EmptyState
                  message={t("pricingEmpty")}
                  action={
                    <Button variant="outline" onClick={addPriceRow}>
                      <Plus className="h-3.5 w-3.5" />
                      {t("addModel")}
                    </Button>
                  }
                />
              ) : (
                <div className="overflow-x-auto">
                  <div
                    className={`${PRICE_GRID} border-b border-slate-200 pb-1.5 text-xs font-medium text-slate-600 dark:border-slate-800 dark:text-slate-300`}
                  >
                    <span>{t("model")}</span>
                    <span>{t("inputPrice")}</span>
                    <span>{t("outputPrice")}</span>
                    <span>{t("cacheReadPrice")}</span>
                    <span>{t("cacheWritePrice")}</span>
                    <span className="sr-only">{t("remove")}</span>
                  </div>
                  <div className="mt-2 space-y-2">
                    {priceDraft.map((row) => (
                      <div key={row.id} className={PRICE_GRID}>
                        <Input
                          value={row.model}
                          placeholder={t("modelPlaceholder")}
                          aria-label={t("model")}
                          onChange={(event) => updatePriceRow(row.id, "model", event.target.value)}
                        />
                        <Input
                          type="number"
                          min={0}
                          step="any"
                          value={row.input}
                          placeholder={t("pricePlaceholder")}
                          aria-label={t("inputPrice")}
                          onChange={(event) => updatePriceRow(row.id, "input", event.target.value)}
                        />
                        <Input
                          type="number"
                          min={0}
                          step="any"
                          value={row.output}
                          placeholder={t("pricePlaceholder")}
                          aria-label={t("outputPrice")}
                          onChange={(event) => updatePriceRow(row.id, "output", event.target.value)}
                        />
                        <Input
                          type="number"
                          min={0}
                          step="any"
                          value={row.cacheRead}
                          placeholder={t("pricePlaceholder")}
                          aria-label={t("cacheReadPrice")}
                          onChange={(event) => updatePriceRow(row.id, "cacheRead", event.target.value)}
                        />
                        <Input
                          type="number"
                          min={0}
                          step="any"
                          value={row.cacheWrite}
                          placeholder={t("pricePlaceholder")}
                          aria-label={t("cacheWritePrice")}
                          onChange={(event) => updatePriceRow(row.id, "cacheWrite", event.target.value)}
                        />
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t("remove")}
                          title={t("remove")}
                          onClick={() => removePriceRow(row.id)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {pricingError ? <ErrorBox error={pricingError} /> : null}
            </>
          )}
        </Section>

        {/* 日志 */}
        <Section title={t("logs")}>
          {options.isLoading ? (
            <Skeleton rows={2} />
          ) : options.isError ? (
            <ErrorBox error={options.error} onRetry={() => void options.refetch()} />
          ) : (
            <>
              <Field label={t("retentionDays")}>
                <Input
                  type="number"
                  min={1}
                  value={retention}
                  onChange={(event) => setRetention(event.target.value)}
                />
              </Field>
              {logsError ? <ErrorBox error={logsError} /> : null}
              <div className="flex flex-wrap items-center gap-2">
                <Button variant="outline" disabled={retentionBusy} onClick={() => void saveRetention()}>
                  {tc("save")}
                </Button>
                <Button variant="danger" disabled={pruneBusy} onClick={() => void pruneLogs()}>
                  {t("pruneNow")}
                </Button>
              </div>
            </>
          )}
        </Section>

        {/* 系统 */}
        <div className="lg:col-span-2">
          <Section
            title={t("system")}
            actions={
              <Button disabled={systemBusy || !draft} onClick={() => void saveSystem()}>
                {tc("save")}
              </Button>
            }
          >
            {options.isLoading ? (
              <Skeleton rows={6} />
            ) : options.isError ? (
              <ErrorBox error={options.error} onRetry={() => void options.refetch()} />
            ) : !draft ? (
              <Skeleton rows={3} />
            ) : (
              <>
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-sm font-medium">{t("autoDisable")}</div>
                  </div>
                  <Switch
                    checked={draft.autoDisable}
                    label={t("autoDisable")}
                    onChange={(value) => setDraft({ ...draft, autoDisable: value })}
                  />
                </div>

                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-sm font-medium">{t("autoEnable")}</div>
                    <p className="mt-0.5 text-xs text-amber-700 dark:text-amber-300">
                      {t("autoEnableHint")}
                    </p>
                  </div>
                  <Switch
                    checked={draft.autoEnable}
                    label={t("autoEnable")}
                    onChange={(value) => setDraft({ ...draft, autoEnable: value })}
                  />
                </div>

                <div className="grid gap-3 sm:grid-cols-3">
                  <Field label={t("failureThreshold")}>
                    <Input
                      type="number"
                      min={1}
                      value={draft.failureThreshold}
                      onChange={(event) => setDraft({ ...draft, failureThreshold: event.target.value })}
                    />
                  </Field>
                  <Field label={t("openSeconds")}>
                    <Input
                      type="number"
                      min={0}
                      value={draft.openSeconds}
                      onChange={(event) => setDraft({ ...draft, openSeconds: event.target.value })}
                    />
                  </Field>
                  <Field label={t("maxOpenSeconds")}>
                    <Input
                      type="number"
                      min={1}
                      value={draft.maxOpenSeconds}
                      onChange={(event) => setDraft({ ...draft, maxOpenSeconds: event.target.value })}
                    />
                  </Field>
                </div>

                <Field label={t("keywords")} hint={t("keywordsHint")}>
                  <Textarea
                    rows={5}
                    value={draft.keywordsText}
                    onChange={(event) => setDraft({ ...draft, keywordsText: event.target.value })}
                  />
                </Field>

                <div>
                  <div className="text-sm font-medium">{t("defaultLaneConfig")}</div>
                  <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
                    {t("defaultLaneConfigHint")}
                  </p>
                  <dl className="mt-2 grid gap-2 sm:grid-cols-3">
                    {DEFAULT_SIX_KEYS.map((item) => (
                      <div
                        key={item.label}
                        className="flex items-center justify-between gap-2 rounded-md border border-slate-200 px-2.5 py-1.5 text-xs dark:border-slate-800"
                      >
                        <dt className="text-slate-500 dark:text-slate-400">{tl(item.label)}</dt>
                        <dd className="font-mono">{item.value}</dd>
                      </div>
                    ))}
                  </dl>
                </div>

                {systemError ? <ErrorBox error={systemError} /> : null}
              </>
            )}
          </Section>
        </div>
      </div>
    </div>
  );
}
