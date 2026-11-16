#!/usr/bin/env python3
"""最小无头浏览器验证：new-api 控制台 + PBR 认证是否打通。

用法：pbr_console_login_probe.py <base_url> <password>
输出 JSON：{loaded, login_redirected, page, errors, screenshots}
退出码 0=通过，1=失败，2=环境不支持。
"""
import base64, json, os, shutil, socket, subprocess, sys, time, urllib.request

try:
    import websocket
except ImportError:
    websocket = None


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class CDP:
    def __init__(self, ws):
        self.ws = websocket.create_connection(ws, timeout=40)
        self.i = 1
        self.errors = []

    def send(self, method, params=None, timeout=30):
        mid = self.i; self.i += 1
        self.ws.send(json.dumps({"id": mid, "method": method, "params": params or {}}))
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.ws.settimeout(max(0.1, deadline - time.time()))
            try:
                raw = self.ws.recv()
            except Exception:
                break
            if not raw:
                continue
            d = json.loads(raw)
            if d.get("method") in ("Runtime.exceptionThrown",):
                detail = d["params"].get("exceptionDetails", {})
                self.errors.append(str(detail.get("text", ""))[:200])
            elif d.get("method") == "Runtime.consoleAPICalled" and d["params"].get("type") == "error":
                self.errors.append(" ".join(str(a.get("value", "")) for a in d["params"].get("args", []))[:200])
            if d.get("id") == mid:
                return d
        return {}

    def value(self, expr):
        r = self.send("Runtime.evaluate", {"expression": expr, "returnByValue": True, "awaitPromise": True})
        return ((r.get("result") or {}).get("result") or {}).get("value")

    def close(self):
        try: self.ws.close()
        except Exception: pass


def main():
    if len(sys.argv) < 3:
        print("usage: probe.py <base_url> <password>"); return 2
    base, password = sys.argv[1], sys.argv[2]
    if websocket is None:
        print(json.dumps({"skipped": "no websocket-client"})); return 2
    chrome = shutil.which("chromium") or shutil.which("chromium-browser") or shutil.which("google-chrome")
    if not chrome:
        print(json.dumps({"skipped": "no chromium"})); return 2
    out = "/tmp/pbr-console-probe"; shutil.rmtree(out, ignore_errors=True); os.makedirs(out, exist_ok=True)
    port = free_port()
    proc = subprocess.Popen([chrome, "--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
                             "--ignore-certificate-errors", "--no-first-run", "--lang=zh-CN",
                             f"--remote-debugging-port={port}", "--remote-allow-origins=*",
                             f"--user-data-dir={out}/profile", "about:blank"],
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        ws_url = ""
        for _ in range(80):
            try:
                for t in json.loads(urllib.request.urlopen(f"http://127.0.0.1:{port}/json/list", timeout=5).read()):
                    if t.get("type") == "page":
                        ws_url = t["webSocketDebuggerUrl"]; break
                if ws_url: break
            except Exception:
                pass
            time.sleep(0.5)
        if not ws_url:
            print(json.dumps({"error": "devtools not ready"})); return 1
        cdp = CDP(ws_url)
        cdp.send("Runtime.enable"); cdp.send("Page.enable")

        # 1) 打开根路径：应加载 SPA（title 含项目名）
        cdp.send("Page.navigate", {"url": base + "/"})
        time.sleep(5)
        title = cdp.value("document.title")
        root_html = cdp.value("document.getElementById('root') ? document.getElementById('root').innerHTML.length : -1")
        loaded = bool(title) and isinstance(root_html, int) and root_html > 0

        # 2) 直接访问受保护页，应被重定向到登录页
        cdp.send("Page.navigate", {"url": base + "/channels"})
        time.sleep(4)
        path_after = cdp.value("location.pathname")
        redirected = path_after == "/sign-in"

        # 3) 在登录页用口令登录（直接调用同源接口并写入前端所需状态后跳转）
        login_status = cdp.value(
            "(function(){return fetch('/api/v1/auth/login',{method:'POST',"
            "headers:{'Content-Type':'application/json'},credentials:'same-origin',"
            "body:JSON.stringify({password:%s})}).then(function(r){return r.status;});})()" % json.dumps(password))
        time.sleep(1)
        authed = cdp.value(
            "fetch('/api/v1/auth/session',{credentials:'same-origin'}).then(function(r){return r.json();})"
            ".then(function(j){return j.authenticated === true;})")

        # 4) 刷新后应能进入受保护页
        cdp.send("Page.navigate", {"url": base + "/channels"})
        time.sleep(5)
        final_path = cdp.value("location.pathname")
        protected_ok = final_path == "/channels"

        shot = cdp.send("Page.captureScreenshot", {"format": "png"})
        data = ((shot.get("result") or {}).get("data")) or ""
        if data:
            open(os.path.join(out, "final.png"), "wb").write(base64.b64decode(data))

        result = {
            "title": title, "spa_loaded": loaded,
            "redirect_to_signin": redirected, "redirect_path": path_after,
            "login_status": login_status, "session_authenticated": authed,
            "protected_after_login": protected_ok, "final_path": final_path,
            "errors": cdp.errors[:5],
        }
        cdp.close()
        print(json.dumps(result, ensure_ascii=False, indent=2))
        ok = loaded and redirected and login_status == 200 and authed and protected_ok
        return 0 if ok else 1
    finally:
        proc.terminate()
        try: proc.wait(timeout=10)
        except Exception: proc.kill()


if __name__ == "__main__":
    sys.exit(main())
