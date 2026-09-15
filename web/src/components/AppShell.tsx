import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useTranslations } from "use-intl";
import {
  Boxes,
  FolderTree,
  Home,
  KeyRound,
  LogOut,
  Radio,
  ScrollText,
  Settings as SettingsIcon,
  Sparkles,
} from "lucide-react";
import { setSessionFlag } from "@/api/client";
import { logout as logoutRequest } from "@/api/session";
import { useRouteEvents } from "@/hooks/useRouteEvents";
import { useInvalidate, qk } from "@/api/queries";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/common/AsyncState";
import { useSettingStore } from "@/stores/setting";

const NAV = [
  { to: "/", key: "dashboard", icon: Home, end: true },
  { to: "/lanes", key: "lanes", icon: FolderTree },
  { to: "/channels", key: "channels", icon: Radio },
  { to: "/models", key: "models", icon: Sparkles },
  { to: "/logs", key: "logs", icon: ScrollText },
  { to: "/keys", key: "keys", icon: KeyRound },
  { to: "/playground", key: "playground", icon: Boxes },
  { to: "/settings", key: "settings", icon: SettingsIcon },
] as const;

/** AppShell：侧边导航 + 内容区（ui-spec §5/§6）。 */
export function AppShell() {
  const t = useTranslations("nav");
  const tc = useTranslations("common");
  const navigate = useNavigate();
  const invalidate = useInvalidate();
  const locale = useSettingStore((state) => state.locale);
  const setLocale = useSettingStore((state) => state.setLocale);

  // SSE 重连后先取一次快照（ui-spec §4）
  const { connected } = useRouteEvents(() => {
    void invalidate([qk.lanes]);
  });

  const logout = () => {
    // 先让服务端清 Cookie，再清本地标记并回登录页（后端已失效则静默继续）。
    void logoutRequest().catch(() => undefined).finally(() => {
      setSessionFlag(false);
      navigate("/login", { replace: true });
    });
  };

  return (
    <div className="flex min-h-screen bg-slate-50 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
      <aside className="flex w-52 shrink-0 flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        <div className="px-4 py-3">
          <div className="text-sm font-semibold">{tc("appName")}</div>
          <div className="mt-1">
            <Badge tone={connected ? "ok" : "warn"}>
              {connected ? t("realtimeOn") : t("realtimeOff")}
            </Badge>
          </div>
        </div>
        <nav className="flex-1 space-y-0.5 px-2">
          {NAV.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={"end" in item ? item.end : false}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2 rounded-md px-2.5 py-1.5 text-sm transition-colors",
                    isActive
                      ? "bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900"
                      : "text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800",
                  )
                }
              >
                <Icon className="h-4 w-4" />
                {t(item.key)}
              </NavLink>
            );
          })}
        </nav>
        <div className="space-y-2 border-t border-slate-200 p-2 dark:border-slate-800">
          <select
            aria-label="language"
            value={locale}
            onChange={(event) => setLocale(event.target.value as typeof locale)}
            className="w-full rounded-md border border-slate-300 bg-white px-2 py-1 text-xs dark:border-slate-700 dark:bg-slate-900"
          >
            <option value="zh_hans">简体中文</option>
            <option value="zh_hant">繁體中文</option>
            <option value="en">English</option>
          </select>
          <button
            type="button"
            onClick={logout}
            className="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-sm text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
          >
            <LogOut className="h-4 w-4" />
            {tc("logout")}
          </button>
        </div>
      </aside>
      <main className="min-w-0 flex-1 p-6">
        <Outlet />
      </main>
    </div>
  );
}
