import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { getAdminKey } from "@/api/client";
import { useSetupStatus } from "@/api/queries";
import { Loading } from "@/components/common/AsyncState";
import { AppShell } from "@/components/AppShell";
import { Login } from "@/pages/Login";
import { Dashboard } from "@/pages/Dashboard";
import { Lanes } from "@/pages/Lanes";
import { LaneEditor } from "@/pages/LaneEditor";
import { Channels } from "@/pages/Channels";
import { Models } from "@/pages/Models";
import { Logs } from "@/pages/Logs";
import { Keys } from "@/pages/Keys";
import { Playground } from "@/pages/Playground";
import { Settings } from "@/pages/Settings";

/**
 * 路由与鉴权闸门（ui-spec §3/§6）。
 *
 * 深链可直达：每个页面都有真实 URL，刷新不会丢位置（§7 要求 SPA 回退 index.html）。
 * 未初始化时任何路径都强制进设置口令页；已初始化后该页不可再进入（后端 409）。
 */
export function App() {
  const status = useSetupStatus();
  const location = useLocation();
  const authed = Boolean(getAdminKey());

  if (status.isLoading) return <Loading />;

  // setup/status 失败时按"未初始化"处理，避免把用户误导到一个不存在的登录页
  const initialized = status.data?.initialized ?? false;

  if (!initialized) {
    return (
      <Routes>
        <Route path="*" element={<Login initialized={false} />} />
      </Routes>
    );
  }

  if (!authed && !location.pathname.startsWith("/login")) {
    return <Navigate to="/login" replace />;
  }

  return (
    <Routes>
      <Route path="/login" element={<Login initialized />} />
      <Route element={<AppShell />}>
        <Route index element={<Dashboard />} />
        <Route path="lanes" element={<Lanes />} />
        <Route path="lanes/:name" element={<LaneEditor />} />
        <Route path="channels" element={<Channels />} />
        <Route path="models" element={<Models />} />
        <Route path="logs" element={<Logs />} />
        <Route path="keys" element={<Keys />} />
        <Route path="playground" element={<Playground />} />
        <Route path="settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
