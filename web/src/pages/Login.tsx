import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslations } from "use-intl";
import { toast } from "sonner";
import { setSessionFlag } from "@/api/client";
import { login, setupAdmin } from "@/api/session";
import { useInvalidate, qk } from "@/api/queries";
import { ErrorBox } from "@/components/common/AsyncState";
import { CopyButton } from "@/components/common/CopyButton";
import { cn } from "@/lib/utils";

function strengthOf(password: string): "weak" | "fair" | "strong" {
  if (password.length < 8) return "weak";
  const classes = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter((re) => re.test(password)).length;
  if (password.length >= 24 && classes >= 3) return "strong";
  return "fair";
}

function BrandMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className} aria-hidden="true">
      <path d="M12 2L2 7L12 12L22 7L12 2Z" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M2 17L12 22L22 17" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M2 12L12 17L22 12" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function LockIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className} aria-hidden="true">
      <rect x="3" y="11" width="18" height="11" rx="2" stroke="currentColor" strokeWidth="1.8" />
      <path d="M7 11V7a5 5 0 0 1 10 0v4" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}

function EyeIcon({ open, className }: { open: boolean; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className} aria-hidden="true">
      <path d="M1 12s4-7 11-7 11 7 11 7-4 7-11 7S1 12 1 12Z" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <circle cx="12" cy="12" r="3" stroke="currentColor" strokeWidth="1.8" />
      {!open ? <path d="M3 3l18 18" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" /> : null}
    </svg>
  );
}

function ArrowIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className} aria-hidden="true">
      <path d="M5 12h14M12 5l7 7-7 7" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

/** 输入框外壳：左侧图标 + 右侧可选动作，聚焦时高亮边框与阴影。 */
function PasswordInput({
  value,
  onChange,
  onEnter,
  placeholder,
  show,
  onToggle,
  autoFocus,
}: {
  value: string;
  onChange: (value: string) => void;
  onEnter?: () => void;
  placeholder: string;
  show: boolean;
  onToggle: () => void;
  autoFocus?: boolean;
}) {
  return (
    <div className="group relative">
      <LockIcon className="pointer-events-none absolute left-3.5 top-1/2 h-5 w-5 -translate-y-1/2 text-slate-400 transition-colors group-focus-within:text-indigo-500" />
      <input
        type={show ? "text" : "password"}
        value={value}
        autoFocus={autoFocus}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && onEnter) onEnter();
        }}
        className="w-full rounded-xl border border-slate-200 bg-white/80 py-3 pl-11 pr-11 text-sm text-slate-900 shadow-sm outline-none transition-all placeholder:text-slate-400 focus:border-indigo-500 focus:ring-4 focus:ring-indigo-500/15 dark:border-slate-700 dark:bg-slate-950/60 dark:text-slate-100"
      />
      <button
        type="button"
        tabIndex={-1}
        onClick={onToggle}
        aria-label={show ? "hide password" : "show password"}
        className="absolute right-3 top-1/2 -translate-y-1/2 rounded-md p-1 text-slate-400 transition-colors hover:text-indigo-500"
      >
        <EyeIcon open={show} className="h-5 w-5" />
      </button>
    </div>
  );
}

function Decoration({ appName, tagline }: { appName: string; tagline: string }) {
  return (
    <div className="relative hidden overflow-hidden bg-gradient-to-br from-indigo-600 via-violet-600 to-fuchsia-600 p-10 text-white lg:flex lg:flex-col lg:justify-between pbr-gradient-flow">
      <div className="pointer-events-none absolute -left-16 -top-16 h-64 w-64 rounded-full bg-white/20 blur-2xl pbr-float" />
      <div className="pointer-events-none absolute -bottom-20 -right-10 h-72 w-72 rounded-full bg-fuchsia-300/30 blur-2xl pbr-float-slow" />
      <div className="pointer-events-none absolute right-16 top-24 h-24 w-24 rounded-2xl bg-white/10 rotate-12 pbr-float" />
      <div className="relative flex items-center gap-2 text-lg font-semibold">
        <BrandMark className="h-7 w-7" />
        <span>{appName}</span>
      </div>
      <div className="relative max-w-sm">
        <h2 className="text-3xl font-bold leading-snug">{appName}</h2>
        <p className="mt-3 text-sm text-white/80">{tagline}</p>
        <div className="mt-8 grid grid-cols-3 gap-3 text-center text-xs text-white/80">
          <div className="rounded-xl border border-white/20 bg-white/10 p-3 backdrop-blur">
            <div className="text-lg font-semibold text-white">1</div>
            路由层
          </div>
          <div className="rounded-xl border border-white/20 bg-white/10 p-3 backdrop-blur">
            <div className="text-lg font-semibold text-white">SQLite</div>
            存储
          </div>
          <div className="rounded-xl border border-white/20 bg-white/10 p-3 backdrop-blur">
            <div className="text-lg font-semibold text-white">OpenAPI</div>
            管理面
          </div>
        </div>
      </div>
      <div className="relative text-xs text-white/60">PowerBarRations</div>
    </div>
  );
}

/**
 * 设置口令 / 登录（ui-spec §6.1）。
 *
 * 首启：口令（不设长度硬限制，仅做强度提示）+ 确认 → 提交后**弹出一次性管理密钥**，
 * 附复制按钮与"如何重算"说明，用户确认保存后才进入 Dashboard。
 * 视觉：渐变分栏 + 玻璃卡片；右侧装饰区在窄屏隐藏。
 */
export function Login({ initialized }: { initialized: boolean }) {
  const t = useTranslations("auth");
  const tc = useTranslations("common");
  const navigate = useNavigate();
  const invalidate = useInvalidate();

  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [issuedKey, setIssuedKey] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);

  const strength = strengthOf(password);
  const strengthLevel = strength === "weak" ? 1 : strength === "fair" ? 2 : 3;
  const secure = typeof window !== "undefined" && window.location.protocol === "https:";

  const submit = async () => {
    setError(null);
    if (!initialized) {
      if (!password) {
        setError(new Error(t("passwordRequired")));
        return;
      }
      if (password !== confirm) {
        setError(new Error(t("passwordMismatch")));
        return;
      }
    }
    setBusy(true);
    try {
      if (!initialized) {
        const result = await setupAdmin(password);
        // 服务端已签发会话 Cookie；这里展示一次管理密钥供 AI 配置使用。
        setSessionFlag(true);
        setIssuedKey(result.admin_key);
        if (result.warning) toast.warning(result.warning);
      } else {
        await login(password);
        // 浏览器靠 HttpOnly Cookie 鉴权，本地只记"已登录"标记。
        setSessionFlag(true);
        await invalidate([qk.setupStatus]);
        navigate("/", { replace: true });
      }
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  if (issuedKey) {
    return (
      <IssuedAdminKey
        issuedKey={issuedKey}
        onDone={() => {
          void invalidate([qk.setupStatus]);
          navigate("/", { replace: true });
        }}
      />
    );
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-slate-950 px-4 py-10">
      <div className="pointer-events-none absolute inset-0 bg-gradient-to-br from-indigo-700 via-violet-700 to-fuchsia-700 opacity-95 pbr-gradient-flow" />
      <div className="pointer-events-none absolute inset-0 opacity-30 [background-image:radial-gradient(circle_at_20%_20%,rgba(255,255,255,0.35),transparent_45%),radial-gradient(circle_at_80%_0%,rgba(255,255,255,0.2),transparent_40%)]" />

      <div className="pbr-fade-up relative z-10 grid w-full max-w-5xl overflow-hidden rounded-3xl border border-white/20 bg-white/95 shadow-2xl backdrop-blur-xl lg:grid-cols-2 dark:bg-slate-900/90">
        <div className="p-8 sm:p-10">
          <div className="flex items-center gap-2 text-indigo-600 dark:text-indigo-400">
            <BrandMark className="h-7 w-7" />
            <span className="text-base font-semibold text-slate-900 dark:text-slate-100">{tc("appName")}</span>
            {secure ? (
              <span className="ml-auto inline-flex items-center gap-1 rounded-full bg-emerald-100 px-2 py-0.5 text-[10px] font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                <LockIcon className="h-3 w-3" /> HTTPS
              </span>
            ) : null}
          </div>

          <div className="mt-6">
            <h1 className="text-2xl font-bold text-slate-900 dark:text-slate-50">
              {initialized ? t("loginTitle") : t("setupTitle")}
            </h1>
            <p className="mt-1.5 text-sm text-slate-500 dark:text-slate-400">
              {initialized ? t("loginHint") : t("setupHint")}
            </p>
          </div>

          <div className="mt-6 space-y-4">
            <div>
              <label className="mb-1.5 block text-xs font-medium text-slate-600 dark:text-slate-300">{t("password")}</label>
              <PasswordInput
                value={password}
                onChange={setPassword}
                onEnter={initialized ? () => void submit() : undefined}
                placeholder={initialized ? t("loginHint") : t("password")}
                show={showPassword}
                onToggle={() => setShowPassword((v) => !v)}
                autoFocus
              />
            </div>

            {!initialized ? (
              <div>
                <label className="mb-1.5 block text-xs font-medium text-slate-600 dark:text-slate-300">{t("confirmPassword")}</label>
                <PasswordInput
                  value={confirm}
                  onChange={setConfirm}
                  onEnter={() => void submit()}
                  placeholder={t("confirmPassword")}
                  show={showConfirm}
                  onToggle={() => setShowConfirm((v) => !v)}
                />
                <div className="mt-2 flex items-center gap-2">
                  <div className="flex h-1.5 flex-1 gap-1">
                    {[1, 2, 3].map((level) => (
                      <span
                        key={level}
                        className={cn(
                          "h-full flex-1 rounded-full transition-colors",
                          strengthLevel >= level
                            ? strength === "weak"
                              ? "bg-rose-500"
                              : strength === "fair"
                                ? "bg-amber-500"
                                : "bg-emerald-500"
                            : "bg-slate-200 dark:bg-slate-700",
                        )}
                      />
                    ))}
                  </div>
                  <span className="text-xs text-slate-500 dark:text-slate-400">
                    {t("strength")}：
                    {strength === "weak" ? t("strengthWeak") : strength === "fair" ? t("strengthFair") : t("strengthStrong")}
                  </span>
                </div>
              </div>
            ) : null}

            {error ? <ErrorBox error={error} /> : null}

            <button
              type="button"
              disabled={busy || password.length === 0}
              onClick={() => void submit()}
              className="group flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-indigo-600 to-violet-600 px-4 py-3 text-sm font-semibold text-white shadow-lg shadow-indigo-500/25 transition-all hover:from-indigo-500 hover:to-violet-500 hover:shadow-indigo-500/40 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {busy ? <span className="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white" /> : null}
              {initialized ? t("signIn") : t("submit")}
              <ArrowIcon className="h-4 w-4 transition-transform group-hover:translate-x-0.5" />
            </button>
          </div>
        </div>

        <Decoration appName={tc("appName")} tagline={t("brandTagline")} />
      </div>
    </div>
  );
}

/** 一次性管理密钥展示（ui-spec §3 第 2 步）：确认保存后才进入应用。 */
export function IssuedAdminKey({
  issuedKey,
  onDone,
}: {
  issuedKey: string;
  onDone: () => void;
}) {
  const t = useTranslations("auth");
  const tc = useTranslations("common");
  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-slate-950 px-4 py-10">
      <div className="pointer-events-none absolute inset-0 bg-gradient-to-br from-indigo-700 via-violet-700 to-fuchsia-700 opacity-95 pbr-gradient-flow" />
      <div className="pbr-fade-up relative z-10 w-full max-w-lg rounded-3xl border border-white/20 bg-white/95 p-8 shadow-2xl backdrop-blur-xl dark:bg-slate-900/90">
        <div className="flex items-center gap-2 text-indigo-600 dark:text-indigo-400">
          <BrandMark className="h-6 w-6" />
          <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">{tc("appName")}</span>
        </div>
        <h1 className="mt-5 text-xl font-bold text-slate-900 dark:text-slate-50">{t("adminKeyTitle")}</h1>
        <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">{t("adminKeyHint")}</p>
        <div className="mt-4 break-all rounded-xl border border-slate-200 bg-slate-50 p-3 font-mono text-xs text-slate-800 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100">
          {issuedKey}
        </div>
        <div className="mt-4 flex items-center gap-3">
          <CopyButton value={issuedKey} />
          <span className="text-xs text-slate-500 dark:text-slate-400">{t("howToRecompute")}</span>
        </div>
        <button
          type="button"
          onClick={onDone}
          className="mt-6 flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-indigo-600 to-violet-600 px-4 py-3 text-sm font-semibold text-white shadow-lg shadow-indigo-500/25 transition-all hover:from-indigo-500 hover:to-violet-500"
        >
          {t("savedIt")}
          <ArrowIcon className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}
