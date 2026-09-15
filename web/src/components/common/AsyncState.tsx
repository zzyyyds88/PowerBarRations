import type { ReactNode } from "react";
import { useTranslations } from "use-intl";
import { ApiError } from "@/api/client";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

/** 加载态：骨架（ui-spec §6 统一要求）。 */
export function Skeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="space-y-2" aria-busy="true">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="h-9 animate-pulse rounded-md bg-slate-200/70 dark:bg-slate-800" />
      ))}
    </div>
  );
}

export function Loading({ label }: { label?: string }) {
  const t = useTranslations("common");
  return (
    <div className="flex items-center gap-2 py-8 text-sm text-slate-500">
      <span className="h-3 w-3 animate-spin rounded-full border-2 border-slate-400 border-t-transparent" />
      {label ?? t("loading")}
    </div>
  );
}

/** 错误态：必须展示稳定 error.code 与 message（ui-spec §6 / §8.5）。 */
export function ErrorBox({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const t = useTranslations("common");
  let code = "";
  let message = String(error);
  let hint = "";
  if (error instanceof ApiError) {
    code = error.code;
    message = error.message;
    hint = error.hint;
  } else if (error instanceof Error) {
    message = error.message;
  }
  return (
    <div className="rounded-md border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="font-medium">{t("error")}</div>
          {code ? <div className="mt-0.5 font-mono text-xs opacity-80">{code}</div> : null}
          <div className="mt-1">{message}</div>
          {hint ? <div className="mt-1 text-xs opacity-80">{hint}</div> : null}
        </div>
        {onRetry ? (
          <Button variant="outline" size="sm" onClick={onRetry}>
            {t("retry")}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

/** 空态：引导 + 主操作（ui-spec §6 统一要求）。 */
export function EmptyState({
  message,
  action,
}: {
  message: string;
  action?: ReactNode;
}) {
  return (
    <div className="rounded-md border border-dashed border-slate-300 p-8 text-center dark:border-slate-700">
      <p className="text-sm text-slate-500 dark:text-slate-400">{message}</p>
      {action ? <div className="mt-3">{action}</div> : null}
    </div>
  );
}

/** 徽标：状态色与车道运行态一一对应（ui-spec §6.4/§6.7）。 */
export function Badge({
  tone = "muted",
  children,
  className,
}: {
  tone?: "ok" | "warn" | "err" | "muted" | "info";
  children: ReactNode;
  className?: string;
}) {
  const tones: Record<string, string> = {
    ok: "bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200",
    warn: "bg-amber-100 text-amber-900 dark:bg-amber-900/40 dark:text-amber-200",
    err: "bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-200",
    info: "bg-sky-100 text-sky-800 dark:bg-sky-900/40 dark:text-sky-200",
    muted: "bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300",
  };
  return (
    <span
      className={cn("inline-flex items-center rounded px-2 py-0.5 text-xs font-medium", tones[tone], className)}
    >
      {children}
    </span>
  );
}
