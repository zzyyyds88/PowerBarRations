#!/usr/bin/env python3
"""T4 验收：模型管理页的"成员链（故障切换）"是否真的打通。

用户心智（design-v1 §7.7）：
  渠道管理里把上游与模型填好 → **模型管理**里为该模型定"优先打谁、再打谁"
  → 令牌允许该模型。

本脚本全程自包含（自建独立端口 + 独立 SQLite + 假上游，不碰现网）：
  1. 构建 PBR 与假上游，设登录口令
  2. 建两个都声明 t4-model 的渠道：channel-live（假上游）与 channel-dead（死端口）
  3. 真实无头浏览器登录 → 进入 /models/routing → 断言面板列出模型与两个成员
     → 点"下移"调整顺序 → 点"保存"（走 PUT /api/v1/lanes/t4-model）
  4. 回读 /api/v1/routes/t4-model，断言显式链已落库且顺序为 dead → live
  5. 用客户端密钥真发一次 /v1/chat/completions：断言第一个成员失败后逃逸到 live 成功
  6. 断言日志里 attempts 链含两个成员

输出 JSON 到 stdout；退出码 0=全过，1=有失败，2=环境不支持。
"""
import base64, hashlib, json, os, random, shutil, socket, subprocess, sys, time, urllib.error, urllib.request

try:
    import websocket
except ImportError:
    websocket = None

REPO = os.environ.get("PBR_REPO", "/root/powerbar-rations")
WORK = "/tmp/pbr-t4"
PW = "Pbr-T4-Failover-Passw0rd!2026"


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def run(cmd, **kw):
    return subprocess.run(cmd, shell=isinstance(cmd, str), cwd=kw.pop("cwd", REPO),
                          capture_output=True, text=True, **kw)


def req(base, method, path, body=None, key=None, timeout=20):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(base + path, data=data, method=method)
    if data is not None:
        r.add_header("Content-Type", "application/json")
    if key:
        r.add_header("Authorization", "Bearer " + key)
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            raw = resp.read().decode()
            return resp.status, (json.loads(raw) if raw else {})
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"raw": raw}


class CDP:
    def __init__(self, ws):
        self.ws = websocket.create_connection(ws, timeout=40)
        self.i = 1
        self.errors = []

    def send(self, method, params=None, timeout=40):
        mid = self.i; self.i += 1
        self.ws.send(json.dumps({"id": mid, "method": method, "params": params or {}}))
        end = time.time() + timeout
        while time.time() < end:
            self.ws.settimeout(max(0.1, end - time.time()))
            try:
                raw = self.ws.recv()
            except Exception:
                break
            if not raw:
                continue
            d = json.loads(raw)
            if d.get("method") == "Runtime.exceptionThrown":
                self.errors.append(str(d["params"].get("exceptionDetails", {}).get("text", ""))[:240])
            if d.get("method") == "Runtime.consoleAPICalled" and d["params"].get("type") == "error":
                self.errors.append(" ".join(str(a.get("value", "")) for a in d["params"].get("args", []))[:240])
            if d.get("id") == mid:
                return d
        return {}

    def val(self, expr):
        r = self.send("Runtime.evaluate", {"expression": expr, "returnByValue": True, "awaitPromise": True})
        return ((r.get("result") or {}).get("result") or {}).get("value")

    def close(self):
        try: self.ws.close()
        except Exception: pass


def main():
    checks = []
    def check(name, ok, detail=""):
        checks.append({"name": name, "pass": bool(ok), "detail": str(detail)[:300]})
        print(("  PASS " if ok else "  FAIL ") + name + ((" | " + str(detail)[:200]) if detail and not ok else ""))

    if websocket is None:
        print(json.dumps({"skipped": "websocket-client missing"})); return 2
    chrome = shutil.which("chromium") or shutil.which("chromium-browser") or shutil.which("google-chrome")
    if not chrome:
        print(json.dumps({"skipped": "no chromium"})); return 2

    shutil.rmtree(WORK, ignore_errors=True); os.makedirs(WORK, exist_ok=True)
    port, up_port, dead_port = free_port(), free_port(), free_port()

    print("--- 构建")
    # 先建控制台，再 go build：go:embed 会把当时磁盘上的 web/dist 一起内嵌，
    # 顺序反了就会把仓库占位页封进二进制，UI 步骤必然失败。
    web_dist = os.path.join(REPO, "web", "dist")
    index_html = os.path.join(web_dist, "index.html")
    real_index = os.path.exists(index_html) and "/static/js/" in open(index_html, encoding="utf-8", errors="ignore").read()
    if not real_index:
        print("--- 控制台未构建，先 pnpm build")
        b = run(["pnpm", "build"], cwd=os.path.join(REPO, "web"),
                env={**os.environ, "npm_config_registry": "https://registry.npmmirror.com"})
        if b.returncode:
            print(json.dumps({"error": "web build failed", "stderr": b.stderr[-800:]})); return 1
        real_index = os.path.exists(index_html) and "/static/js/" in open(index_html, encoding="utf-8", errors="ignore").read()
    print("--- 控制台产物:", "已构建" if real_index else "仅占位页（面板 UI 断言将退化为 API 直写）")

    b1 = run(["go", "build", "-o", WORK + "/pbr", "."],
             env={**os.environ, "GOCACHE": REPO + "/tmp/gocache", "GOTMPDIR": REPO + "/tmp"})
    b2 = run(["go", "build", "-o", WORK + "/fakeupstream", "./internal/testutil/fakeupstream/cmd/fakeupstream"],
             env={**os.environ, "GOCACHE": REPO + "/tmp/gocache", "GOTMPDIR": REPO + "/tmp"})
    if b1.returncode or b2.returncode:
        print(json.dumps({"error": "build failed", "stderr": (b1.stderr + b2.stderr)[-800:]})); return 1

    up = subprocess.Popen([WORK + "/fakeupstream", "-addr", f"127.0.0.1:{up_port}",
                           "-log", WORK + "/upstream.log"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    env = {**os.environ, "PORT": str(port), "PBR_BIND": "127.0.0.1", "GIN_MODE": "release",
           "SQLITE_PATH": f"{WORK}/t4.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"}
    pbr = subprocess.Popen([WORK + "/pbr"], env=env, cwd=WORK, stdout=open(WORK + "/pbr.log", "w"), stderr=subprocess.STDOUT)
    base = f"http://127.0.0.1:{port}"
    try:
        ok_health = False
        for _ in range(60):
            try:
                s, _ = req(base, "GET", "/api/v1/health", timeout=3)
                if s == 200: ok_health = True; break
            except Exception:
                pass
            time.sleep(0.5)
        check("服务起得来（/api/v1/health）", ok_health)
        if not ok_health:
            print(json.dumps({"checks": checks})); return 1

        s, b = req(base, "POST", "/api/v1/setup", {"password": PW})
        admin_key = b.get("admin_key") or base64.b64encode(hashlib.sha256(PW.encode()).digest()).decode()
        check("首启设口令返回管理密钥", s in (200, 201) and bool(admin_key), (s, b))

        # 两个渠道都声明 t4-model：live 高优先级（隐式链在前），dead 低优先级
        s1, _ = req(base, "PUT", "/api/v1/channels/channel-live",
                    {"type": "openai", "base_url": f"http://127.0.0.1:{up_port}", "key": "sk-t4",
                     "enabled": True, "priority": 9, "models": ["t4-model"]}, key=admin_key)
        s2, _ = req(base, "PUT", "/api/v1/channels/channel-dead",
                    {"type": "openai", "base_url": f"http://127.0.0.1:{dead_port}", "key": "sk-dead",
                     "enabled": True, "priority": 1, "models": ["t4-model"]}, key=admin_key)
        check("建两个都声明 t4-model 的渠道", s1 in (200, 201) and s2 in (200, 201), (s1, s2))

        s, models = req(base, "GET", "/api/v1/models", key=admin_key)
        items = models.get("items", []) if isinstance(models, dict) else []
        m1 = next((m for m in items if m.get("model") == "t4-model"), None)
        check("GET /models 列出 t4-model 且含 2 个成员", bool(m1) and m1.get("member_count") == 2, (s, m1))

        s, route0 = req(base, "GET", "/api/v1/routes/t4-model", key=admin_key)
        implicit_order = [x.get("channel") for x in route0.get("members", [])]
        check("初始为隐式链且 live 在前", route0.get("source") in ("implicit", None) and implicit_order[:1] == ["channel-live"],
              route0)

        s, keyb = req(base, "POST", "/api/v1/keys", {"name": "t4-client", "enabled": True}, key=admin_key)
        client_key = (keyb or {}).get("key") or (keyb or {}).get("plaintext") or (keyb or {}).get("data", {}).get("key")
        check("建客户端密钥并拿到一次性明文", s in (200, 201) and bool(client_key), (s, keyb))

        # ---- 浏览器：真实操作"模型管理 → Routing & Failover"面板 ----
        cws = None
        if real_index:
            cdp_port = free_port()
            proc = subprocess.Popen([chrome, "--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
                                     "--no-first-run", "--lang=en-US", f"--remote-debugging-port={cdp_port}",
                                     "--remote-allow-origins=*", f"--user-data-dir={WORK}/profile", "about:blank"],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            try:
                ws_url = ""
                for _ in range(80):
                    try:
                        for t in json.loads(urllib.request.urlopen(f"http://127.0.0.1:{cdp_port}/json/list", timeout=5).read()):
                            if t.get("type") == "page":
                                ws_url = t["webSocketDebuggerUrl"]; break
                        if ws_url: break
                    except Exception:
                        pass
                    time.sleep(0.5)
                if ws_url:
                    cdp = CDP(ws_url); cdp.send("Runtime.enable"); cdp.send("Page.enable")
                    cdp.send("Page.navigate", {"url": base + "/"})
                    time.sleep(4)
                    # 同源登录拿会话 Cookie
                    st = cdp.val("fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},"
                                 "credentials:'same-origin',body:JSON.stringify({password:%s})}).then(r=>r.status)" % json.dumps(PW))
                    check("浏览器登录成功", st == 200, st)
                    cdp.send("Page.navigate", {"url": base + "/models/routing"})
                    time.sleep(6)
                    path = cdp.val("location.pathname")
                    check("进入 /models/routing（未被踢回登录）", path == "/models/routing", path)
                    body_text = cdp.val("document.body.innerText.slice(0,800)") or ""
                    print("  [debug] path=", path)
                    print("  [debug] body=", body_text.replace("\n", " | ")[:800])
                    print("  [debug] console_errors=", cdp.errors[:6])
                    listed = cdp.val("document.body.innerText.includes('t4-model')")
                    check("面板列出模型 t4-model", bool(listed))
                    members = cdp.val("document.body.innerText.includes('channel-live') && document.body.innerText.includes('channel-dead')")
                    check("面板列出两个成员", bool(members))
                    # 点第一个"下移"，再点"保存"
                    moved = cdp.val("(function(){var b=document.querySelector('button[aria-label=\"Move down\"]');"
                                    "if(!b) return false; b.click(); return true;})()")
                    time.sleep(1)
                    saved = cdp.val("(function(){var bs=[...document.querySelectorAll('button')].filter(function(x){return x.textContent.trim()==='Save';});"
                                    "if(!bs.length) return false; bs[0].click(); return true;})()")
                    time.sleep(4)
                    check("面板可下移成员并保存", bool(moved) and bool(saved), (moved, saved))
                    cws = cdp
            finally:
                proc.terminate()
                try: proc.wait(timeout=10)
                except Exception: proc.kill()

        # ---- 后端回读：显式链已落库且顺序被翻转 ----
        s, route1 = req(base, "GET", "/api/v1/routes/t4-model", key=admin_key)
        order = [x.get("channel") for x in route1.get("members", [])]
        if real_index:
            check("保存后回读为显式链", route1.get("source") == "explicit", route1.get("source"))
            check("顺序已翻转为 dead → live", order[:2] == ["channel-dead", "channel-live"], order)
        else:
            # 无真实控制台产物时，退化为直接写显式链，仍验证后端语义
            req(base, "PUT", "/api/v1/lanes/t4-model", {"enabled": True, "mode": "failover",
                "config": {"member_max_attempts": 1, "member_retry_interval_seconds": 0,
                           "member_non_stream_response_timeout_seconds": 1,
                           "member_stream_first_event_timeout_seconds": 1,
                           "member_cooldown_seconds": 1, "member_affinity_seconds": 0},
                "members": [{"channel": "channel-dead", "upstream_model": "t4-model", "priority": 2},
                            {"channel": "channel-live", "upstream_model": "t4-model", "priority": 1}]}, key=admin_key)
            s, route1 = req(base, "GET", "/api/v1/routes/t4-model", key=admin_key)
            order = [x.get("channel") for x in route1.get("members", [])]
            check("显式链顺序为 dead → live（API 直写）", order[:2] == ["channel-dead", "channel-live"], order)

        # ---- 真发一次模型面请求：第一个成员死 → 逃逸到 live ----
        model_body = {"model": "t4-model", "messages": [{"role": "user", "content": "ping"}]}
        try:
            r = urllib.request.Request(base + "/v1/chat/completions", data=json.dumps(model_body).encode(), method="POST")
            r.add_header("Content-Type", "application/json")
            r.add_header("Authorization", "Bearer " + str(client_key))
            with urllib.request.urlopen(r, timeout=30) as resp:
                code, out = resp.status, json.loads(resp.read().decode())
        except urllib.error.HTTPError as e:
            code, out = e.code, e.read().decode()[:200]
        check("模型面请求在首成员失败后逃逸成功（200）", code == 200, (code, out))

        # ---- 日志 attempts 链（写入可能异步，短暂重试）----
        blob = ""
        for _ in range(12):
            s, logs = req(base, "GET", "/api/v1/logs?limit=5", key=admin_key)
            blob = json.dumps(logs)
            if "channel-dead" in blob and "channel-live" in blob:
                break
            time.sleep(0.5)
        check("日志记录了两次尝试（含 channel-dead 与 channel-live）",
              "channel-dead" in blob and "channel-live" in blob, blob[:300])

        print(json.dumps({"checks": checks}, ensure_ascii=False, indent=2))
        return 0 if all(c["pass"] for c in checks) else 1
    finally:
        for p in (pbr, up):
            try: p.terminate()
            except Exception: pass
        for p in (pbr, up):
            try: p.wait(timeout=8)
            except Exception: p.kill()


if __name__ == "__main__":
    sys.exit(main())
