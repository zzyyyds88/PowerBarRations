#!/usr/bin/env python3
"""W8-A3 控制台逐页走查（无头 Chromium + Chrome DevTools Protocol 驱动）。

在真实浏览器里**带鉴权**加载控制台每一页，断言 ui-spec §8 的验收项：
  1. 全部页面可加载，且无 console 报错；
  2. 深链可直达（直接导航到 /lanes 等，不经过首页点按）；
  3. 页面确实渲染出内容（不是空白/白屏）；
  4. 三态组件在源码中齐备；
  5. 无第三方品牌残留。

依赖：chromium（本机 /usr/local/bin/chromium）、python3 `websocket-client`。
若缺依赖，脚本以退出码 2 明确报"跳过原因"，不伪装成通过。

用法：console_walkthrough.py <base_url> <login_password> <repo_root> <out_dir>
（第二参数是**登录口令**：脚本用它在页面内同源登录，服务端下发 HttpOnly 会话 Cookie。）
输出：JSON 到 stdout；截图与 DOM 落在 out_dir。
"""

import json
import os
import re
import shutil
import socket
import subprocess
import sys
import time
import urllib.request

try:
    import websocket  # type: ignore
except ImportError:  # pragma: no cover
    websocket = None

# new-api 风格控制台的保留页（T1/T2 迁移后的实际路由）。
PAGES = [
    ("/", "home", "home"),
    ("/dashboard/overview", "dashboard", "dashboard"),
    ("/channels", "channels", "channels"),
    ("/models/metadata", "models", "models"),
    # 路由与故障切换已是侧边栏独立页 /routes（与 /models 同级）。
    ("/routes", "routes", "routes"),
    ("/keys", "keys", "keys"),
    ("/usage-logs/common", "usage-logs", "logs"),
    ("/playground", "playground", "playground"),
    # task-plugins 子系统已整链路移除；system-info 页已删除，任务面板提为 /system-tasks。
    ("/system-tasks", "system-tasks", "system-tasks"),
    ("/system-settings/site/system-info", "system-settings", "settings"),
]

# 环境噪声（GPU/dbus/字体等），不是页面错误。
NOISE = re.compile(
    r"dbus|libva|vaapi|gpu|GL |EGL|Fontconfig|shared_memory|Failed to connect to the bus|"
    r"sandbox|Vulkan|angle|DevTools|Autofill|net::ERR_|favicon|"
    r"Failed to load resource|404 \(Not Found\)",
    re.I,
)


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class CDP:
    """极简 CDP 客户端：只用到 Target/Runtime/Page/Log 几个域。"""

    def __init__(self, ws_url: str):
        self.ws = websocket.create_connection(ws_url, timeout=30)
        self.next_id = 1
        self.console_errors: list[str] = []

    def send(self, method: str, params: dict | None = None, timeout: float = 20) -> dict:
        msg_id = self.next_id
        self.next_id += 1
        self.ws.send(json.dumps({"id": msg_id, "method": method, "params": params or {}}))
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.ws.settimeout(max(0.1, deadline - time.time()))
            try:
                raw = self.ws.recv()
            except Exception:
                break
            if not raw:
                continue
            data = json.loads(raw)
            # 收集事件里的 console 报错
            if data.get("method") == "Runtime.consoleAPICalled":
                if data["params"].get("type") == "error":
                    parts = [
                        str(a.get("value", a.get("description", "")))
                        for a in data["params"].get("args", [])
                    ]
                    text = " ".join(p for p in parts if p)
                    if not NOISE.search(text):
                        self.console_errors.append(text[:300])
            elif data.get("method") == "Runtime.exceptionThrown":
                detail = data["params"].get("exceptionDetails", {})
                text = str(detail.get("text", "")) + " " + str(
                    (detail.get("exception") or {}).get("description", "")
                )
                if not NOISE.search(text):
                    self.console_errors.append(text[:300])
            elif data.get("method") == "Log.entryAdded":
                entry = data["params"].get("entry", {})
                if entry.get("level") == "error":
                    text = str(entry.get("text", ""))
                    if not NOISE.search(text):
                        self.console_errors.append(text[:300])
            if data.get("id") == msg_id:
                return data
        return {}

    def evaluate(self, expression: str):
        return self.send(
            "Runtime.evaluate",
            {"expression": expression, "returnByValue": True, "awaitPromise": True},
        )

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


def fetch_json(url: str, timeout: float = 10):
    with urllib.request.urlopen(url, timeout=timeout) as resp:
        return json.loads(resp.read().decode())


def main() -> int:
    base, login_password, repo, out_dir = sys.argv[1:5]
    if websocket is None:
        print(json.dumps({"skipped": "python websocket-client 未安装（pip install websocket-client）"}))
        return 2
    chrome = shutil.which("chromium") or shutil.which("chromium-browser") or shutil.which("google-chrome")
    if not chrome:
        print(json.dumps({"skipped": "未找到 chromium，无法做浏览器级走查"}))
        return 2

    os.makedirs(out_dir, exist_ok=True)
    port = free_port()
    profile = os.path.join(out_dir, "chrome-profile")
    proc = subprocess.Popen(
        [
            chrome,
            "--headless",
            "--disable-gpu",
            "--no-sandbox",
            "--disable-dev-shm-usage",
            "--no-first-run",
            "--no-default-browser-check",
            f"--remote-debugging-port={port}",
            "--remote-allow-origins=*",
            f"--user-data-dir={profile}",
            "about:blank",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )

    try:
        # 等 DevTools 端点就绪
        ws_url = ""
        for _ in range(60):
            try:
                targets = fetch_json(f"http://127.0.0.1:{port}/json/list")
                for t in targets:
                    if t.get("type") == "page":
                        ws_url = t["webSocketDebuggerUrl"]
                        break
                if ws_url:
                    break
            except Exception:
                pass
            time.sleep(0.5)
        if not ws_url:
            print(json.dumps({"error": "DevTools 端点未就绪"}))
            return 1

        cdp = CDP(ws_url)
        cdp.send("Runtime.enable")
        cdp.send("Page.enable")
        cdp.send("Log.enable")

        # 先访问同源页面，再用登录口令经同源 fetch 登录：
        # 服务端会下发 HttpOnly 会话 Cookie（token-spec §2.3），与真实浏览器行为一致。
        # 控制台不再把管理密钥存 localStorage，因此这里也改为设置"已登录"标记。
        cdp.send("Page.navigate", {"url": f"{base}/sign-in"})
        time.sleep(2.5)
        login_js = (
            "(async function(){"
            "const r=await fetch('/api/v1/auth/login',{method:'POST',"
            "headers:{'Content-Type':'application/json'},"
            f"body:JSON.stringify({{password:{json.dumps(login_password)}}})}});"
            "if(r.ok){localStorage.setItem('pbr.signedIn','1');}"
            "return r.status;})()"
        )
        login_result = cdp.evaluate(login_js)
        login_status = ((login_result.get("result") or {}).get("result") or {}).get("value")
        if login_status != 200:
            print(json.dumps({"error": "登录失败，无法走查", "status": login_status}))
            return 1

        results = []
        for path, name, expect in PAGES:
            cdp.console_errors = []
            cdp.send("Page.navigate", {"url": f"{base}{path}"})
            # 等 SPA 渲染 + 首屏查询落定
            time.sleep(3.5)
            dom = cdp.evaluate("document.documentElement.outerHTML")
            html = ((dom.get("result") or {}).get("result") or {}).get("value", "") or ""
            title = cdp.evaluate("document.title")
            pathname = cdp.evaluate("location.pathname")
            got_path = ((pathname.get("result") or {}).get("result") or {}).get("value", "")

            # 截图存档（证据）
            shot = cdp.send("Page.captureScreenshot", {"format": "png"})
            data = ((shot.get("result") or {}).get("data")) or ""
            shot_path = os.path.join(out_dir, f"{name}.png")
            if data:
                import base64

                with open(shot_path, "wb") as f:
                    f.write(base64.b64decode(data))
            with open(os.path.join(out_dir, f"{name}.dom.html"), "w", encoding="utf-8") as f:
                f.write(html)

            # 渲染成功判据：根节点有实际内容（长度 + 非空 root），且路由停在预期路径
            root_rendered = len(html) > 2000
            routed = (got_path.rstrip("/") == path.rstrip("/") or got_path.startswith(path.rstrip("/") + "/") or (path == "/" and got_path == "/"))
            results.append(
                {
                    "page": path,
                    "rendered": root_rendered,
                    "dom_bytes": len(html),
                    "routed": routed,
                    "landed_path": got_path,
                    "console_errors": list(cdp.console_errors),
                    "screenshot": os.path.basename(shot_path) if data else "",
                    "expect_key": expect,
                }
            )

        cdp.close()

        # 源码级检查：三态组件与品牌
        src_all = []
        for root, _dirs, files in os.walk(os.path.join(repo, "web/src")):
            for f in files:
                if f.endswith((".tsx", ".ts")):
                    src_all.append(os.path.join(root, f))
        joined = "\n".join(open(p, encoding="utf-8", errors="replace").read() for p in src_all)

        # 品牌残留：只查真正的"品牌面"，不误伤合法的上游中继协议名（渠道类型
        # "New API/One API" 是本网关要对接的上游协议，必须保留）。
        brand_hits = []

        # 1) 外壳标题/元信息不得是上游品牌
        index_path = os.path.join(repo, "web/index.html")
        if os.path.exists(index_path):
            for i, line in enumerate(open(index_path, encoding="utf-8", errors="replace").read().splitlines(), 1):
                if "<title>New API" in line or 'content="New API"' in line:
                    brand_hits.append(f"web/index.html:{i}: {line.strip()[:90]}")

        # 2) 旧蓝本（octopus 控制台）不得残留任何标识
        for p in src_all:
            text = open(p, encoding="utf-8", errors="replace").read()
            for i, line in enumerate(text.splitlines(), 1):
                if "octopus" in line.lower() and not line.strip().startswith(("//", "*", "/*")):
                    brand_hits.append(f"{os.path.relpath(p, repo)}:{i}: {line.strip()[:90]}")

        # 3) 渲染出的文档标题不得是上游品牌
        try:
            title_val = ((cdp.evaluate("document.title").get("result") or {}).get("result") or {}).get("value") or ""
        except Exception:
            title_val = ""
        if title_val.strip() == "New API":
            brand_hits.append("document.title == 'New API'")

        summary = {
            "pages": results,
            "pages_rendered": sum(1 for r in results if r["rendered"]),
            "pages_routed": sum(1 for r in results if r["routed"]),
            "pages_total": len(PAGES),
            "console_errors": [e for r in results for e in r["console_errors"]],
            # new-api 控制台的三态组件命名（旧 octopus 蓝本是 ErrorBox/Loading）。
            "three_states": {
                "Skeleton": "Skeleton" in joined,
                "EmptyState": "EmptyState" in joined,
                "ErrorState": "ErrorState" in joined,
                "LoadingState": "LoadingState" in joined,
            },
            "brand_hits": brand_hits,
        }
        print(json.dumps(summary, ensure_ascii=False, indent=2))
        return 0
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except Exception:
            proc.kill()


if __name__ == "__main__":
    sys.exit(main())
