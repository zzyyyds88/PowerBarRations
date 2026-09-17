#!/usr/bin/env python3
"""控制台网络审计：真实浏览器逐页加载，记录所有 >=400 的 /api 响应。

与 console_walkthrough.py 的区别：**不**忽略 404/加载失败，专门抓
"页面调用了不存在的后端路由"这类静默失败。退出码 0=无失败，1=有失败，2=环境跳过。
用法：console_network_audit.py <base_url> <login_password> <out_dir> [page ...]
"""
import base64
import json
import os
import shutil
import socket
import subprocess
import sys
import time
import urllib.request

try:
    import websocket  # type: ignore
except ImportError:
    websocket = None

PAGES = [
    "/",
    "/dashboard/overview",
    "/channels",
    "/models/metadata",
    "/routes",
    "/keys",
    "/usage-logs/common",
    "/usage-logs/audit",
    "/playground",
    "/system-tasks",
    "/system-settings/site/system-info",
    "/system-settings/models/global",
    "/system-settings/operations/performance",
    "/system-settings/content/dashboard",
]


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class CDP:
    def __init__(self, ws_url):
        self.ws = websocket.create_connection(ws_url, timeout=30)
        self.next_id = 1
        self.api_failures = []
        self.pending = {}

    def send(self, method, params=None, timeout=20):
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
            self._on_event(data)
            if data.get("id") == msg_id:
                return data
        return {}

    def _on_event(self, data):
        method = data.get("method")
        if method == "Network.responseReceived":
            resp = data["params"].get("response", {})
            url = resp.get("url", "")
            status = int(resp.get("status", 0))
            if "/api/" in url and (status >= 400 or status == 0):
                self.api_failures.append(
                    {"method": self.pending.get(data["params"].get("requestId"), "GET"), "status": status, "url": url}
                )
        elif method == "Network.requestWillBeSent":
            req = data["params"].get("request", {})
            self.pending[data["params"].get("requestId")] = req.get("method", "GET")
        elif method == "Network.loadingFailed":
            if not data["params"].get("canceled"):
                rid = data["params"].get("requestId")
                if rid in self.pending:
                    self.api_failures.append(
                        {"method": self.pending[rid], "status": "failed", "url": "requestId=" + str(rid), "error": data["params"].get("errorText")}
                    )

    def evaluate(self, expression):
        return self.send("Runtime.evaluate", {"expression": expression, "returnByValue": True, "awaitPromise": True})

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


def fetch_json(url):
    with urllib.request.urlopen(url, timeout=10) as resp:
        return json.loads(resp.read().decode())


def main() -> int:
    base, login_password, out_dir = sys.argv[1:4]
    pages = sys.argv[4:] or PAGES
    if websocket is None:
        print(json.dumps({"skipped": "python websocket-client 未安装"}))
        return 2
    chrome = shutil.which("chromium") or shutil.which("chromium-browser") or shutil.which("google-chrome")
    if not chrome:
        print(json.dumps({"skipped": "未找到 chromium"}))
        return 2

    os.makedirs(out_dir, exist_ok=True)
    port = free_port()
    profile = os.path.join(out_dir, "chrome-profile")
    proc = subprocess.Popen(
        [chrome, "--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
         "--no-first-run", "--no-default-browser-check", "--ignore-certificate-errors",
         f"--remote-debugging-port={port}", "--remote-allow-origins=*",
         f"--user-data-dir={profile}", "about:blank"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    try:
        ws_url = ""
        for _ in range(60):
            try:
                for t in fetch_json(f"http://127.0.0.1:{port}/json/list"):
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
        cdp.send("Network.enable")

        cdp.send("Page.navigate", {"url": f"{base}/sign-in"})
        time.sleep(2.5)
        login_js = (
            "(async function(){const r=await fetch('/api/v1/auth/login',{method:'POST',"
            "headers:{'Content-Type':'application/json'},"
            f"body:JSON.stringify({{password:{json.dumps(login_password)}}})}});"
            "if(r.ok){localStorage.setItem('pbr.signedIn','1');}return r.status;})()"
        )
        login_status = ((cdp.evaluate(login_js).get("result") or {}).get("result") or {}).get("value")
        if login_status != 200:
            print(json.dumps({"error": "登录失败", "status": login_status}))
            return 1

        per_page = {}
        for path in pages:
            cdp.api_failures = []
            cdp.send("Page.navigate", {"url": f"{base}{path}"})
            time.sleep(4.0)
            per_page[path] = list(cdp.api_failures)
        cdp.close()

        failures = [{"page": p, **f} for p, fs in per_page.items() for f in fs]
        # 去重（同页同 URL）
        dedup = {}
        for f in failures:
            dedup[(f["page"], f["method"], f["url"].split("?")[0], f["status"])] = f
        failures = list(dedup.values())
        print(json.dumps({"failures": failures, "count": len(failures), "pages": len(pages)}, ensure_ascii=False, indent=2))
        return 1 if failures else 0
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except Exception:
            proc.kill()


if __name__ == "__main__":
    sys.exit(main())
