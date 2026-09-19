#!/usr/bin/env python3
"""L3 真实用户操作测试（docs/test-spec-v1.md §4）。

像人一样在真实无头浏览器里操作控制台：首启设口令 → 登录 → 建渠道（表单）→
建车道/编辑成员链（UI）→ 建客户端密钥（表单）→ 试打台对话 → 查看日志 → 改系统设置。
每一步都由 UI 触发，再由后端回读佐证（写后回读）。

全程自包含：独立端口 + 独立 SQLite + 内置假上游 + 无头 Chromium（CDP）。
缺 chromium / websocket-client 时 SKIP（退出码 2），不包装成 PASS。

语言：Chromium 以 --lang=en-US 启动，并在首帧前把 i18next 的语言固定为 en，
使按钮文案确定是英文；所有点击仍同时匹配中英文，避免语言抖动导致漏点。
"""
import base64
import hashlib
import json
import os
import random
import shutil
import socket
import string
import subprocess
import sys
import time
import urllib.error
import urllib.request

try:
    import websocket
except ImportError:
    websocket = None

REPO = os.environ.get("PBR_REPO") or os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
WORK = "/tmp/pbr-ui-journey"
STAMP = time.strftime("%Y%m%d-%H%M%S")
EVID = os.path.join(REPO, "verify", "e2e-ui", "run-%s.log" % STAMP)
SHOTS = os.path.join(REPO, "verify", "e2e-ui", "shots")
PW = "Pbr-Ui-Journey-Passw0rd!2026"
MODEL = "ui-model"
CHANNEL = "ui-channel"
KEYNAME = "ui-client"
SITE_NAME = "PBR-UI-Journey"


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def run(cmd, cwd=None, env=None):
    return subprocess.run(cmd, cwd=cwd, env=env, capture_output=True, text=True)


def req(base, method, path, body=None, key=None, timeout=30):
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


def admin_key_of(password):
    return base64.b64encode(hashlib.sha256(password.encode()).digest()).decode()


class CDP:
    def __init__(self, ws_url):
        self.ws = websocket.create_connection(ws_url, timeout=40)
        self.i = 1
        self.console_errors = []

    def send(self, method, params=None, timeout=30):
        mid = self.i
        self.i += 1
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
                self.console_errors.append(str(d["params"].get("exceptionDetails", {}).get("text", ""))[:240])
            if d.get("method") == "Runtime.consoleAPICalled" and d["params"].get("type") == "error":
                self.console_errors.append(" ".join(str(a.get("value", "")) for a in d["params"].get("args", []))[:240])
            if d.get("id") == mid:
                return d
        return {}

    def val(self, expr, timeout=30):
        r = self.send("Runtime.evaluate", {"expression": expr, "returnByValue": True, "awaitPromise": True}, timeout=timeout)
        return ((r.get("result") or {}).get("result") or {}).get("value")

    def nav(self, url, wait=3.0):
        self.send("Page.navigate", {"url": url})
        time.sleep(wait)
        # SPA 首帧可能还没渲染：等到 body 有内容，避免选择器查空。
        for _ in range(20):
            if self.val("document.body && document.body.innerText.length>20", timeout=5):
                break
            time.sleep(0.5)

    def shot(self, name):
        try:
            r = self.send("Page.captureScreenshot", {"format": "png"})
            data = ((r.get("result") or {}).get("data"))
            if data:
                os.makedirs(SHOTS, exist_ok=True)
                with open(os.path.join(SHOTS, name + ".png"), "wb") as fh:
                    fh.write(base64.b64decode(data))
        except Exception:
            pass

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


# 点击/写入用 JS：全部走原生 setter + input/change 事件，React 受控组件才认。
JS_SET_BY_NAME = (
    "(function(){var el=document.querySelector('input[name=%s],textarea[name=%s]');"
    "if(!el) return false;var p=Object.getPrototypeOf(el);"
    "var d=Object.getOwnPropertyDescriptor(p,'value');"
    "if(d&&d.set){d.set.call(el,%s);}else{el.value=%s;}"
    "el.dispatchEvent(new Event('input',{bubbles:true}));"
    "el.dispatchEvent(new Event('change',{bubbles:true}));return true;})()"
)

JS_SET_SELECTOR = (
    "(function(){var el=document.querySelector(%s);if(!el) return false;"
    "var p=Object.getPrototypeOf(el);var d=Object.getOwnPropertyDescriptor(p,'value');"
    "if(d&&d.set){d.set.call(el,%s);}else{el.value=%s;}"
    "el.dispatchEvent(new Event('input',{bubbles:true}));"
    "el.dispatchEvent(new Event('change',{bubbles:true}));return true;})()"
)

# 在 [role=dialog] 范围内点击按钮（用于渠道弹窗底部提交，避免命中页面同名的列表按钮）。
JS_CLICK_DIALOG = (
    "(function(){var want=%s;var bs=[...document.querySelectorAll('[role=dialog] button')];"
    "for(var i=0;i<bs.length;i++){var s=(bs[i].textContent||'').trim();"
    "for(var j=0;j<want.length;j++){if(s===want[j]){bs[i].click();return s;}}}"
    "return false;})()"
)

# 精确匹配按钮文案（避免 "Save" 命中 "Save changes"/"Save Changes"）。
JS_CLICK_EXACT = (
    "(function(){var want=%s;var bs=[...document.querySelectorAll('button')];"
    "for(var i=0;i<bs.length;i++){var s=(bs[i].textContent||'').trim();"
    "for(var j=0;j<want.length;j++){if(s===want[j]){bs[i].click();return s;}}}"
    "return false;})()"
)

# 包含匹配（用于 "Create ChannelCreate" 这类含图标/响应式双文案的按钮）。
JS_CLICK_CONTAINS = (
    "(function(){var want=%s;var bs=[...document.querySelectorAll('button')];"
    "for(var i=0;i<bs.length;i++){var s=(bs[i].textContent||'').trim();"
    "for(var j=0;j<want.length;j++){if(s.indexOf(want[j])>=0){bs[i].click();return s;}}}"
    "return false;})()"
)

# 按 aria-label 精确点击（编排器里的模型项按钮有首字符兜底图标，textContent 不可靠）。
JS_CLICK_ARIA = (
    "(function(){var name=%s;var bs=[...document.querySelectorAll('button[aria-label]')];"
    "for(var i=0;i<bs.length;i++){if(bs[i].getAttribute('aria-label')===name){bs[i].click();return true;}}"
    "return false;})()"
)

# 点选成员候选按钮：New lane / 成员链面板里以渠道名为按钮文案。
JS_CLICK_CHANNEL = (
    "(function(){var name=%s;var bs=[...document.querySelectorAll('button')];"
    "for(var i=0;i<bs.length;i++){if((bs[i].textContent||'').trim()===name){bs[i].click();return true;}}"
    "return false;})()"
)


def js_set_by_name(name, value):
    return JS_SET_BY_NAME % (json.dumps(name), json.dumps(name), json.dumps(value), json.dumps(value))


def js_set_selector(selector, value):
    return JS_SET_SELECTOR % (json.dumps(selector), json.dumps(value), json.dumps(value))


def js_click_exact(texts):
    return JS_CLICK_EXACT % json.dumps(texts, ensure_ascii=False)


def js_click_contains(texts):
    return JS_CLICK_CONTAINS % json.dumps(texts, ensure_ascii=False)


def js_click_dialog(texts):
    return JS_CLICK_DIALOG % json.dumps(texts, ensure_ascii=False)


def js_click_channel(name):
    return JS_CLICK_CHANNEL % json.dumps(name)


def js_click_aria(name):
    return JS_CLICK_ARIA % json.dumps(name)


def main():
    if websocket is None:
        print(json.dumps({"skipped": "python websocket-client 未安装"}))
        return 2
    chrome = shutil.which("chromium") or shutil.which("chromium-browser") or shutil.which("google-chrome")
    if not chrome:
        print(json.dumps({"skipped": "未找到 chromium"}))
        return 2

    os.makedirs(os.path.dirname(EVID), exist_ok=True)
    logfh = open(EVID, "w")

    def log(msg=""):
        print(msg)
        logfh.write(msg + "\n")
        logfh.flush()

    PASS = {"n": 0}
    FAIL = {"n": 0}

    def note(msg):
        # 非计分备注：记录已确认但不阻断 L3 的产品缺口/事实，不掩盖为 PASS。
        log("  NOTE: " + msg)

    def check(desc, ok, detail=""):
        if ok:
            log("  PASS: " + desc)
            PASS["n"] += 1
        else:
            log("  FAIL: " + desc + (" | " + str(detail)[:300] if detail else ""))
            FAIL["n"] += 1

    log("=== L3 真实用户操作测试 @ %s ===" % STAMP)

    procs = []
    cdp = None
    try:
        log("--- 0) 构建（控制台 + 网关 + 假上游）")
        b = run(["pnpm", "build"], cwd=os.path.join(REPO, "web"))
        if b.returncode:
            log("FAIL: 控制台构建\n" + b.stderr[-800:])
            return 1
        # 构建产物校验：index.html 必须引用真实入口（占位页不含 /static/）。
        try:
            with open(os.path.join(REPO, "web", "dist", "index.html"), encoding="utf-8") as fh:
                if "/static/" not in fh.read():
                    log("FAIL: web/dist/index.html 不是真实控制台入口（缺 /static/）")
                    return 1
        except OSError as exc:
            log("FAIL: 无法读取 web/dist/index.html: %s" % exc)
            return 1
        b1 = run(["go", "build", "-o", WORK + "/pbr", "."], cwd=REPO)
        b2 = run(["go", "build", "-o", WORK + "/fakeupstream", "./internal/testutil/fakeupstream/cmd/fakeupstream"], cwd=REPO)
        if b1.returncode or b2.returncode:
            log("FAIL: go build\n" + (b1.stderr + b2.stderr)[-800:])
            return 1
        # 构建会把 web/dist/index.html 改写：跑完恢复（tracked 产物）
        run(["git", "checkout", "--", "web/dist/index.html"], cwd=REPO)

        up_port, port = free_port(), free_port()
        up_key = "sk-ui-" + "".join(random.choice(string.ascii_lowercase) for _ in range(10))
        up = subprocess.Popen([WORK + "/fakeupstream", "-addr", "127.0.0.1:%d" % up_port,
                               "-require-key", up_key, "-log", WORK + "/upstream.log"],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        procs.append(up)
        env = {**os.environ, "PORT": str(port), "PBR_BIND": "127.0.0.1", "GIN_MODE": "release",
               "SESSION_SECRET": "ui-session", "CRYPTO_SECRET": "ui-crypto",
               "SQLITE_PATH": WORK + "/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate"}
        pbr = subprocess.Popen([WORK + "/pbr"], cwd=WORK, env=env,
                               stdout=open(WORK + "/pbr.log", "w"), stderr=subprocess.STDOUT)
        procs.append(pbr)
        base = "http://127.0.0.1:%d" % port
        up_base = "http://127.0.0.1:%d" % up_port

        log("--- 1) 等待就绪")
        ok = False
        for _ in range(60):
            try:
                s, _b = req(base, "GET", "/api/v1/health", timeout=3)
                if s == 200:
                    ok = True
                    break
            except Exception:
                pass
            time.sleep(0.5)
        check("服务起得来", ok)
        if not ok:
            log("FAIL: 未就绪"); return 1

        # 启动 chromium：--lang=en-US 让文案确定是英文（点击仍兼容中文）。
        cdp_port = free_port()
        proc = subprocess.Popen([chrome, "--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
                                 "--no-first-run", "--no-default-browser-check", "--lang=en-US", "--accept-lang=en-US",
                                 "--remote-debugging-port=%d" % cdp_port, "--remote-allow-origins=*",
                                 "--user-data-dir=" + WORK + "/profile", "about:blank"],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        procs.append(proc)
        ws_url = ""
        for _ in range(80):
            try:
                for t in json.loads(urllib.request.urlopen("http://127.0.0.1:%d/json/list" % cdp_port, timeout=5).read()):
                    if t.get("type") == "page":
                        ws_url = t["webSocketDebuggerUrl"]
                        break
                if ws_url:
                    break
            except Exception:
                pass
            time.sleep(0.5)
        if not ws_url:
            log("FAIL: DevTools 未就绪"); return 1
        cdp = CDP(ws_url)
        cdp.send("Runtime.enable"); cdp.send("Page.enable")

        def wait_for(expr, timeout=20):
            end = time.time() + timeout
            while time.time() < end:
                try:
                    if cdp.val(expr, timeout=5):
                        return True
                except Exception:
                    pass
                time.sleep(0.4)
            return False

        # 固定界面语言为英文：i18next 先读 localStorage['i18nextLng']。
        # 必须在任何页面渲染出可点按钮之前设置，避免中文文案漏点。
        cdp.nav(base + "/setup", wait=3)
        cdp.val("localStorage.setItem('i18nextLng','en')")

        # ---- 用户动作 1：首启设口令 ----
        log("")
        log("=== 用户动作 1：首启设口令（/setup 表单）===")
        # 根路径是公开首页（不自动跳转）；首启真实入口是 /setup。
        cdp.nav(base + "/", wait=3)
        check("公开首页可访问（未初始化）", "PowerBarRations" in (cdp.val("document.body.innerText") or ""), cdp.val("location.pathname"))
        cdp.nav(base + "/setup", wait=3)
        at_setup = wait_for("location.pathname.indexOf('/setup')===0", 20)
        check("首启进入 /setup", at_setup, cdp.val("location.pathname"))
        cdp.val(js_set_by_name("password", PW))
        cdp.val(js_set_by_name("confirmPassword", PW))
        time.sleep(0.4)
        clicked = cdp.val(js_click_exact(["Initialize", "初始化"]))
        check("设口令提交按钮可点（Initialize/初始化）", bool(clicked), clicked)
        # 设口令成功后控制台先展示"一次性管理密钥"面板，点"Continue to console"才进控制台。
        check("设口令成功（出现一次性管理密钥面板）",
              wait_for("document.body.innerText.indexOf('Setup complete')>=0 || document.body.innerText.indexOf('初始化完成')>=0", 25),
              cdp.val("document.body.innerText.slice(0,120)"))
        cdp.val(js_click_exact(["Continue to console", "进入控制台"]))
        check("进入控制台", wait_for("location.pathname.indexOf('/dashboard')===0", 25), cdp.val("location.pathname"))
        cdp.shot("01-setup")
        no_ls = cdp.val("!JSON.stringify(Object.keys(localStorage)).match(/admin_key|管理密钥|token/i)")
        check("localStorage 无管理密钥", no_ls, cdp.val("JSON.stringify(Object.keys(localStorage))"))

        admin_key = admin_key_of(PW)

        # ---- 用户动作 2：登出/登录 ----
        log("")
        log("=== 用户动作 2：登出并用口令登录（/sign-in 表单）===")
        cdp.val("fetch('/api/v1/auth/logout',{method:'POST',credentials:'same-origin'}).then(function(){return 1;})")
        time.sleep(1)
        cdp.nav(base + "/sign-in", wait=3)
        check("登出后停在 /sign-in", cdp.val("location.pathname") == "/sign-in", cdp.val("location.pathname"))
        cdp.val(js_set_by_name("password", "wrong-password"))
        cdp.val(js_click_exact(["Sign in", "登录"]))
        time.sleep(2)
        still = cdp.val("location.pathname") == "/sign-in"
        check("错误口令被拒（仍在 /sign-in）", still, cdp.val("location.pathname"))
        cdp.val(js_set_by_name("password", PW))
        cdp.val(js_click_exact(["Sign in", "登录"]))
        check("正确口令登录进入控制台", wait_for("location.pathname.indexOf('/dashboard')===0", 25), cdp.val("location.pathname"))
        cdp.shot("02-signin")

        # ---- 用户动作 3：新建渠道（表单）----
        log("")
        log("=== 用户动作 3：新建渠道（渠道管理 → Create Channel 表单）===")
        cdp.nav(base + "/channels", wait=4)
        check("进入 /channels", cdp.val("location.pathname") == "/channels", cdp.val("location.pathname"))
        cdp.val(js_click_contains(["Create Channel", "创建渠道"]))
        check("渠道弹窗打开", wait_for("!!document.querySelector('[role=dialog] input[name=\"name\"]')", 15))
        cdp.val(js_set_by_name("name", CHANNEL))
        cdp.val(js_set_by_name("base_url", up_base))
        cdp.val(js_set_by_name("key", up_key))
        # 模型：Models 区的文本框（placeholder 形如 "Model name (...)"）填名后点 Add。
        cdp.val("(function(){var ins=[...document.querySelectorAll('[role=dialog] input,[role=dialog] textarea')];"
                "var el=ins.filter(function(x){return /Model name|模型名/i.test(x.placeholder||'');})[0];"
                "if(!el)return false;var p=Object.getPrototypeOf(el);var d=Object.getOwnPropertyDescriptor(p,'value');"
                "if(d&&d.set)d.set.call(el,%s);else el.value=%s;"
                "el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));return true;})()" % (json.dumps(MODEL), json.dumps(MODEL)))
        time.sleep(0.5)
        cdp.val(js_click_dialog(["Add", "添加"]))
        time.sleep(0.6)
        cdp.shot("03-channel-form")
        cdp.val(js_click_dialog(["Create Channel", "创建渠道"]))
        time.sleep(3)
        s, ch = req(base, "GET", "/api/v1/channels/" + CHANNEL, key=admin_key)
        check("回读渠道存在且 models 含 ui-model", s == 200 and MODEL in (ch.get("models") or []), (s, ch))
        cdp.shot("04-channel-created")

        # ---- 用户动作 4：建车道（路由页卡片 + 两栏编排器，ADR 0006）----
        log("")
        log("=== 用户动作 4：在路由页新建车道并保存 ===")
        cdp.nav(base + "/routes", wait=4)
        check("进入 /routes", cdp.val("location.pathname") == "/routes", cdp.val("location.pathname"))
        # ui-model 已被渠道声明但未配车道 → 路由页出现「未配车道」卡片。
        check("路由页出现未配车道的卡片",
              wait_for("document.body.innerText.indexOf(%s)>=0" % json.dumps(MODEL), 15),
              cdp.val("document.body.innerText.slice(-300)"))
        # 卡片操作是「新建车道」；也可用页头「New lane」。
        cdp.val(js_click_exact(["Create lane", "新建车道", "New lane"]))
        check("两栏编排器打开（左栏出现渠道折叠项）",
              wait_for("(function(){var bs=[...document.querySelectorAll('button')];return bs.some(function(b){return (b.textContent||'').trim().indexOf(%s)>=0;});})()" % json.dumps(CHANNEL), 15),
              cdp.val("document.body.innerText.slice(-400)"))
        # 路由键（新建时可编辑）；未配车道时成员列表为空，必须手工加入成员。
        cdp.val(js_set_selector("#lane-route-key", MODEL))
        time.sleep(0.4)
        # 展开渠道（折叠项文案是"渠道名+模型数"，用包含匹配），点模型项加入右栏。
        picked = cdp.val(js_click_contains([CHANNEL]))
        check("展开渠道折叠项", bool(picked), picked)
        time.sleep(0.5)
        picked_model = cdp.val(js_click_aria(MODEL))
        check("点选模型加入成员", bool(picked_model), picked_model)
        time.sleep(0.6)
        cdp.shot("05-lane-dialog")
        cdp.val(js_click_exact(["Save", "保存"]))
        time.sleep(3)
        s, route = req(base, "GET", "/api/v1/routes/" + MODEL, key=admin_key)
        check("回读车道已固化且可路由", s == 200 and route.get("routable") is True, (s, route))

        # ---- 用户动作 5：新建客户端密钥（表单）----
        log("")
        log("=== 用户动作 5：令牌页新建客户端密钥（表单）===")
        cdp.nav(base + "/keys", wait=4)
        cdp.val(js_click_contains(["Create API Key", "创建 API 密钥"]))
        check("密钥抽屉打开", wait_for("!!document.querySelector('input[name=\"name\"]')", 15))
        cdp.val(js_set_by_name("name", KEYNAME))
        time.sleep(0.4)
        cdp.shot("06-key-form")
        cdp.val(js_click_exact(["Save changes", "Save Changes", "保存更改"]))
        time.sleep(3)
        s, keys = req(base, "GET", "/api/v1/keys", key=admin_key)
        names = [k.get("name") for k in (keys.get("items") or [])]
        check("回读密钥列表含 ui-client", s == 200 and KEYNAME in names, (s, names))
        s, kd = req(base, "GET", "/api/v1/keys/" + KEYNAME, key=admin_key)
        client_key = kd.get("key") if isinstance(kd, dict) else None
        check("回读密钥明文可用（token-spec §3.3）", bool(client_key), kd)
        cdp.shot("07-key-created")

        # ---- 用户动作 6：试打台对话 ----
        log("")
        log("=== 用户动作 6：试打台发一次对话 ===")
        cdp.nav(base + "/playground", wait=4)
        check("进入 /playground", cdp.val("location.pathname").startswith("/playground"), cdp.val("location.pathname"))
        if client_key:
            # 在页面里填客户端密钥（#playground-client-key）与消息，点 Send。
            cdp.val(js_set_selector("#playground-client-key", client_key))
            time.sleep(0.4)
            cdp.val(js_set_selector("textarea[name=message]", "你好"))
            time.sleep(0.4)
            cdp.val(js_click_contains(["Send", "发送"]))
            time.sleep(6)
            body = cdp.val("document.body.innerText") or ""
            check("试打台页面出现回复（pong）", "pong" in body.lower(), body[-300:])
            # ui-spec §6.8 / test-spec §4 行 6：试打台必须展示实际命中的上游 X-Served-By。
            # 展示文案是 i18n 的 "Served by: {{servedBy}}"（zh 为"实际服务：…"）。
            check("试打台展示 X-Served-By（实际命中的上游）",
                  ("Served by" in body) or ("实际服务" in body), body[-300:])
        else:
            check("试打台页面出现回复（pong）", False, "无客户端密钥")
        cdp.shot("08-playground")

        # ---- 用户动作 7：查看请求日志 ----
        log("")
        log("=== 用户动作 7：请求日志页出现该请求 ===")
        cdp.nav(base + "/usage-logs/common", wait=5)
        body = cdp.val("document.body.innerText") or ""
        check("日志页可见 ui-model", MODEL in body, body[-300:])
        cdp.shot("09-logs")

        # ---- 用户动作 8：改系统设置并回读 ----
        log("")
        log("=== 用户动作 8：系统设置页保存并回读 ===")
        cdp.nav(base + "/system-settings/site/system-info", wait=4)
        check("进入系统设置页", cdp.val("location.pathname").startswith("/system-settings"), cdp.val("location.pathname"))
        # 站点名经 PUT /api/option/ 落库，回读走 GET /api/system/options/all（基座完整选项）。
        set_ok = cdp.val(js_set_by_name("SystemName", SITE_NAME))
        check("填入新的系统名称", bool(set_ok), set_ok)
        time.sleep(0.4)
        cdp.val(js_click_exact(["Save Changes", "Save changes", "保存更改"]))
        time.sleep(3)
        s, allopts = req(base, "GET", "/api/v1/system/options/all", key=admin_key)
        items = {i.get("key"): i.get("value") for i in (allopts.get("items") or [])} if isinstance(allopts, dict) else {}
        check("保存后回读 SystemName 一致", s == 200 and items.get("SystemName") == SITE_NAME, (s, items.get("SystemName")))
        cdp.shot("10-settings")

        log("")
        log("=== 结果：PASS=%d FAIL=%d ===" % (PASS["n"], FAIL["n"]))
        log("=== evidence: %s ===" % EVID)
        if FAIL["n"] == 0:
            log("PASS: L3 真实用户操作测试通过")
        else:
            log("FAIL: 存在未通过项")
        return 0 if FAIL["n"] == 0 else 1
    finally:
        if cdp:
            cdp.close()
        for p in reversed(procs):
            try:
                p.terminate()
            except Exception:
                pass
        for p in reversed(procs):
            try:
                p.wait(timeout=10)
            except Exception:
                try:
                    p.kill()
                except Exception:
                    pass
        run(["git", "checkout", "--", "web/dist/index.html"], cwd=REPO)
        logfh.close()


if __name__ == "__main__":
    # 每次运行用全新的工作目录（含全新 SQLite 与浏览器 profile），保证"未初始化"起点可复现。
    shutil.rmtree(WORK, ignore_errors=True)
    os.makedirs(WORK, exist_ok=True)
    sys.exit(main())
