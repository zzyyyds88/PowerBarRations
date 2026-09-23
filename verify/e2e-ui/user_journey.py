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
# 拖拽重排需要至少 3 个成员才能区分"拖到首位/末位"；成员唯一键是
# (渠道, 上游真名)，所以同一渠道的多个已声明模型即可组成 3 成员车道（ADR 0006）。
# 命名有两条硬约束：
#   ① **不得与 `ui-model` 互为子串**（`X-Served-By` 与页面文案都做包含匹配，
#      `ui-model` 是 `ui-model-2` 的前缀会让"命中谁"的断言假绿）；
#   ② **字母序必须排在 `ui-model` 之后**：试打台在无显式选择时回落到模型列表
#      首项（getModelFallback），而只有 `ui-model` 有车道——排到前面会让试打台
#      默认选中一个没车道的模型，请求 503（实测踩到）。
EXTRA_MODELS = ["ui-zeta-1", "ui-zeta-2"]
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


def http_served_by(base, model, client_key, timeout=30):
    """发一次模型面请求并返回 X-Served-By 响应头（失败/503 时为 None）。

    design-v1 §4.1：X-Served-By 只出现在成功响应上，形态
    `channel=<id>:<name>, model=<upstream>`。这是"这次实际命中谁"的权威证据，
    比 attempts 链更直接（attempts 要等日志落库）。
    """
    if not client_key:
        return None
    data = json.dumps({"model": model, "messages": [{"role": "user", "content": "hi"}]}).encode()
    r = urllib.request.Request(base + "/v1/chat/completions", data=data, method="POST")
    r.add_header("Content-Type", "application/json")
    r.add_header("Authorization", "Bearer " + client_key)
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            return resp.headers.get("X-Served-By")
    except urllib.error.HTTPError as e:
        return e.headers.get("X-Served-By")
    except Exception:
        return None


class CDP:
    def __init__(self, ws_url):
        self.ws = websocket.create_connection(ws_url, timeout=40)
        self.i = 1
        self.console_errors = []
        # 读响应时顺带收到的 CDP 事件先存这里：`Input.dragIntercepted` 会在
        # 拖拽过程中到达，若在读别的响应时被丢弃，拖拽链路就永远等不到它。
        self.events = []
        self.last_drag_debug = ""

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
            if d.get("method"):
                self.events.append(d)
            if d.get("id") == mid:
                return d
        return {}

    def take_event(self, method, timeout=6.0):
        """取出并消费一条指定 CDP 事件；超时返回 None。"""
        end = time.time() + timeout
        while True:
            for idx, ev in enumerate(self.events):
                if ev.get("method") == method:
                    return self.events.pop(idx)
            if time.time() >= end:
                return None
            self.ws.settimeout(max(0.1, end - time.time()))
            try:
                raw = self.ws.recv()
            except Exception:
                continue
            if not raw:
                continue
            d = json.loads(raw)
            if d.get("method"):
                self.events.append(d)

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

    def drag(self, src, dst, drop_after=False):
        """真实鼠标拖拽（HTML5 DnD）——不是合成 drop 事件。

        headless Chromium 下原生 DnD 需要 `Input.setInterceptDrags` 接管：
        按下并移动后浏览器发出 `Input.dragIntercepted`，再由调用方用
        `Input.dispatchDragEvent` 把 drag 数据投递到目标点，这样页面收到的是
        **真实的 dragstart/dragover/drop** 事件链，React 的 onDragStart/Over/Drop
        与 dataTransfer 都按真实路径工作。先在本机最小 HTML 上验证过该链路
        （A,B,C → C,A,B）。

        `drop_after=True` 时落点在目标行下半（插入到其后），否则上半（插入到其前）。
        """
        # 页面侧事件计数：用于区分"浏览器没发起 dragstart"与"CDP 没投递
        # dragIntercepted"——两者都表现为拖拽无效果，但修法完全不同。
        self.val(
            "(function(){window.__pbrDrag={start:0,over:0,drop:0};"
            "['dragstart','dragover','drop'].forEach(function(t){"
            "document.addEventListener(t,function(){window.__pbrDrag[t==='dragstart'?'start':(t==='dragover'?'over':'drop')]++;},true);});"
            "return 1;})()"
        )
        self.send("Input.setInterceptDrags", {"enabled": True})
        try:
            self.send("Input.dispatchMouseEvent", {
                "type": "mousePressed", "x": src["x"], "y": src["y"],
                "button": "left", "buttons": 1, "clickCount": 1})
            # 多步移动：浏览器需要若干 mouseMoved 才认定进入拖拽。
            # 每次移动后都检查 dragIntercepted——它可能在任意一步到达，
            # 且可能已被 send() 收进事件队列。
            data = None
            for frac in (0.35, 0.6, 0.85, 1.0):
                self.send("Input.dispatchMouseEvent", {
                    "type": "mouseMoved", "x": src["x"], "y": src["y"] + frac * 24,
                    "button": "left", "buttons": 1})
                ev = self.take_event("Input.dragIntercepted", timeout=1.5)
                if ev is not None:
                    data = ev["params"]["data"]
                    break
            if data is None:
                ev = self.take_event("Input.dragIntercepted", timeout=4)
                if ev is not None:
                    data = ev["params"]["data"]
            if data is None:
                self.last_drag_debug = "no dragIntercepted; page counters=%s" % (
                    self.val("JSON.stringify(window.__pbrDrag)"),)
                return False
            # 落点：上半 = before（插入到该行之前），下半 = after。
            offset = dst["h"] * 0.35 if drop_after else -dst["h"] * 0.35
            y = dst["y"] + offset
            for evt in ("dragEnter", "dragOver"):
                self.send("Input.dispatchDragEvent",
                          {"type": evt, "x": dst["x"], "y": y, "data": data})
                time.sleep(0.2)
            self.send("Input.dispatchDragEvent",
                      {"type": "drop", "x": dst["x"], "y": y, "data": data})
            time.sleep(0.4)
            self.last_drag_debug = "page counters=%s" % (
                self.val("JSON.stringify(window.__pbrDrag)"),)
            return True
        finally:
            self.send("Input.setInterceptDrags", {"enabled": False})

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

# 编排器成员行的坐标（按行序返回）。定位完全靠**结构**，不靠 Tailwind 类名
# （类名是样式实现细节，改一次样式就失效）也不靠文案匹配
# （模型名可能互为子串，包含匹配会串行）。
#
# 锚点：每个成员行有且只有一个拖拽手柄 `[draggable]`（成员行的固有结构，不随
# 字段增删变化——上游真名输入框已被 ADR 0008 移除，不能再用它当锚点）。
# 从手柄向上找**最低的、同时含启停开关的祖先** —— 那就是行；再上一层是右栏面板
# （它含多个开关，故不会被选中）。
JS_MEMBER_ROWS = (
    "(function(){var rows=[];var seen=new Set();"
    "var handles=document.querySelectorAll('[draggable]');"
    "for(var i=0;i<handles.length;i++){var n=handles[i].parentElement;var row=null;"
    "while(n&&n!==document.body){"
    "if(n.querySelector('[role=switch]')){row=n;break;}"
    "n=n.parentElement;}"
    "if(row&&!seen.has(row)){seen.add(row);rows.push(row);}}"
    "return rows;})()"
)
JS_MEMBER_ROW_RECTS = (
    "(function(){return %s.map(function(d){var r=d.getBoundingClientRect();"
    "return {x:r.left+r.width/2,y:r.top+r.height/2,h:r.height};});})()" % JS_MEMBER_ROWS
)
# 拖拽**必须从手柄按下**：`draggable` 只挂在手柄上（整行 draggable 会让行内输入框
# 的文本选择失效），所以拖拽源坐标取手柄中心，落点坐标才取整行。
JS_MEMBER_HANDLE_RECTS = (
    "(function(){return %s.map(function(d){"
    "var s=d.querySelector('[draggable]');"
    "if(!s)return null;var r=s.getBoundingClientRect();"
    "return {x:r.left+r.width/2,y:r.top+r.height/2,h:r.height};});})()" % JS_MEMBER_ROWS
)
# 成员行的草稿顺序：按行序返回"该行文本里出现的第一个候选模型名"。
#
# 不读输入框值（上游真名输入框已随 ADR 0008 移除），也不依赖 i18n 文案——
# 直接把已知的候选模型名集合传给页面，在每行文本里找命中项。模型名互为子串时
# 由调用方保证候选集内无前缀关系（见 EXTRA_MODELS 的命名约束）。
JS_MEMBER_ROW_MODELS = (
    "(function(){var want=%s;var rows=%s;"
    "return rows.map(function(d){var t=d.textContent||'';"
    "for(var i=0;i<want.length;i++){if(t.indexOf(want[i])>=0)return want[i];}"
    "return '';});})()"
)
# 点击某成员行的启停开关：按"行文本含该模型名"定位行，再点行内开关。
# 不依赖开关 aria-label 的具体措辞（那随 i18n 与实现变化）。
JS_CLICK_MEMBER_SWITCH_BY_MODEL = (
    "(function(){var want=%s;var rows=%s;"
    "for(var i=0;i<rows.length;i++){var t=rows[i].textContent||'';"
    "if(t.indexOf(want)>=0){var sw=rows[i].querySelector('[role=switch]');"
    "if(sw){sw.click();return true;}}}"
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


def js_member_row_rects():
    return JS_MEMBER_ROW_RECTS


def js_member_handle_rects():
    return JS_MEMBER_HANDLE_RECTS


def js_member_row_models(candidates):
    return JS_MEMBER_ROW_MODELS % (json.dumps(list(candidates)), JS_MEMBER_ROWS)


def js_click_member_switch_by_model(model):
    return JS_CLICK_MEMBER_SWITCH_BY_MODEL % (json.dumps(model), JS_MEMBER_ROWS)



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
        # `--window-size` 必须显式给足高度：编排器弹窗是固定档位（xl = 86vh/720px），
        # headless 默认视口只有 600px 高，成员列表会被裁到页脚之下——手柄落在可视区外，
        # elementFromPoint 命中的是页脚，浏览器根本不发 dragstart（实测踩到）。
        cdp_port = free_port()
        proc = subprocess.Popen([chrome, "--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
                                 "--no-first-run", "--no-default-browser-check", "--lang=en-US", "--accept-lang=en-US",
                                 "--window-size=1600,1200",
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
        # 连加 3 个模型：成员唯一键是 (渠道, 上游真名)，同一渠道的多个模型即可
        # 组成 3 成员车道，供后面的拖拽重排路径使用（ADR 0006）。
        for m in [MODEL] + EXTRA_MODELS:
            cdp.val("(function(){var ins=[...document.querySelectorAll('[role=dialog] input,[role=dialog] textarea')];"
                    "var el=ins.filter(function(x){return /Model name|模型名/i.test(x.placeholder||'');})[0];"
                    "if(!el)return false;var p=Object.getPrototypeOf(el);var d=Object.getOwnPropertyDescriptor(p,'value');"
                    "if(d&&d.set)d.set.call(el,%s);else el.value=%s;"
                    "el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));return true;})()" % (json.dumps(m), json.dumps(m)))
            time.sleep(0.4)
            cdp.val(js_click_dialog(["Add", "添加"]))
            time.sleep(0.5)
        time.sleep(0.6)
        cdp.shot("03-channel-form")
        cdp.val(js_click_dialog(["Create Channel", "创建渠道"]))
        time.sleep(3)
        s, ch = req(base, "GET", "/api/v1/channels/" + CHANNEL, key=admin_key)
        check("回读渠道存在且 models 含 ui-model", s == 200 and MODEL in (ch.get("models") or []), (s, ch))
        check("回读渠道声明了 3 个模型（拖拽用例的成员来源）",
              s == 200 and all(m in (ch.get("models") or []) for m in [MODEL] + EXTRA_MODELS),
              ch.get("models"))
        cdp.shot("04-channel-created")

        # ---- 用户动作 4：建车道（路由页只列真实车道，ADR 0007）----
        log("")
        log("=== 用户动作 4：在路由页新建车道并保存 ===")
        cdp.nav(base + "/routes", wait=4)
        check("进入 /routes", cdp.val("location.pathname") == "/routes", cdp.val("location.pathname"))
        # ui-model 已被渠道声明但未配车道：按 ADR 0007 不出现「未配车道」卡片。
        routes_body = cdp.val("document.body.innerText") or ""
        check("路由页不出现渠道声明但未配车道的模型（ADR 0007）",
              MODEL not in routes_body,
              routes_body[-300:])
        # 新建入口是页头「New lane」；路由键手工输入。
        cdp.val(js_click_exact(["New lane", "新建车道", "Create lane"]))
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
        # 保存后该车道出现在路由页。
        cdp.nav(base + "/routes", wait=4)
        check("路由页出现新车道",
              wait_for("document.body.innerText.indexOf(%s)>=0" % json.dumps(MODEL), 15),
              cdp.val("document.body.innerText.slice(-300)"))
        # 删除车道后卡片立即消失（ADR 0007：删车道 = 卡片消失，不再退回未配车道占位）。
        cdp.val(js_click_exact(["Delete lane", "删除车道"]))
        time.sleep(1.2)
        cdp.val(js_click_exact(["Remove lane", "删除车道"]))
        time.sleep(3)
        cdp.nav(base + "/routes", wait=4)
        routes_after = cdp.val("document.body.innerText") or ""
        check("删除车道后卡片从路由页消失（ADR 0007）",
              MODEL not in routes_after,
              routes_after[-300:])
        # 重新建回车道，供后续试打台/日志动作使用。
        cdp.val(js_click_exact(["New lane", "新建车道"]))
        wait_for("!!document.querySelector('#lane-route-key')", 15)
        cdp.val(js_set_selector("#lane-route-key", MODEL))
        time.sleep(0.4)
        cdp.val(js_click_contains([CHANNEL]))
        time.sleep(0.5)
        cdp.val(js_click_aria(MODEL))
        time.sleep(0.5)
        cdp.val(js_click_exact(["Save", "保存"]))
        time.sleep(3)
        s, route = req(base, "GET", "/api/v1/routes/" + MODEL, key=admin_key)
        check("车道已重建可路由", s == 200 and route.get("routable") is True, (s, route))

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

        # ---- 用户动作 7b：拖拽重排成员顺序（test-spec §4 行 4d，真实鼠标拖拽）----
        log("")
        log("=== 用户动作 7b：路由页拖拽重排成员并保存（真实鼠标拖拽）===")
        # 先把车道扩成 3 个成员：成员唯一键是 (渠道, 上游真名)，同一渠道的 3 个模型
        # 即可组成 3 成员车道，才能区分"拖到首位"与"拖到末位"。
        cdp.nav(base + "/routes", wait=4)
        cdp.val(js_click_exact(["Edit members", "编辑成员链"]))
        check("编排器打开", wait_for("!!document.querySelector('#lane-route-key')", 15))
        cdp.val(js_click_contains([CHANNEL]))
        time.sleep(0.6)
        for m in EXTRA_MODELS:
            cdp.val(js_click_aria(m))
            time.sleep(0.5)
        cdp.shot("11-composer-three-members")

        def member_rows():
            return cdp.val(js_member_row_rects()) or []

        def member_handles():
            return cdp.val(js_member_handle_rects()) or []

        # 候选模型名集合：用于在成员行文本里认出"这一行是哪个成员"。
        # 名称之间不互为前缀（见 EXTRA_MODELS 的命名约束），故顺序无关。
        CANDIDATES = [MODEL] + EXTRA_MODELS

        def draft_order():
            return cdp.val(js_member_row_models(CANDIDATES)) or []

        rows_before = member_rows()
        handles_before = member_handles()
        check("编排器里有 3 个成员行（拖拽前置条件）", len(rows_before) == 3, rows_before)
        check("每个成员行都有拖拽手柄（真实拖拽的按下点）",
              len(handles_before) == 3 and all(h for h in handles_before), handles_before)
        # 拖拽前先确认"按下点确实落在可拖拽元素上"：坐标算错或被遮挡时，
        # 浏览器不会发起 dragstart，页面侧表现为计数器全 0（实测踩过）。
        if handles_before:
            h0 = handles_before[0]
            hit = cdp.val(
                "(function(){var e=document.elementFromPoint(%s,%s);"
                "if(!e)return 'NONE';var d=e.closest('[draggable]');"
                "return (d?'draggable:':'NOT-draggable:')+e.tagName+'.'+(e.className||'').toString().slice(0,60);})()"
                % (h0["x"], h0["y"])
            )
            check("拖拽按下点落在可拖拽手柄上（未被遮挡）",
                  isinstance(hit, str) and hit.startswith("draggable:"), hit)
        if len(rows_before) == 3 and len(handles_before) == 3:
            # (a) 拖到末位：从第 1 行手柄按下，落到第 3 行下半（after）。
            dragged = cdp.drag(handles_before[0], rows_before[2], drop_after=True)
            check("拖拽事件链建立（dragIntercepted 后投递 drop）", dragged, cdp.last_drag_debug)
            time.sleep(0.6)
            cdp.shot("12-drag-to-last")
            order_mid = draft_order()
            check("拖到末位后草稿顺序变了（第 1 行移到末位）",
                  order_mid and order_mid[-1] == MODEL and order_mid[0] != MODEL,
                  order_mid)
            # (b) 拖回首位：从当前末行手柄按下，落到当前首行上半（before）。
            rows_mid = member_rows()
            handles_mid = member_handles()
            if len(rows_mid) == 3 and len(handles_mid) == 3:
                cdp.drag(handles_mid[2], rows_mid[0], drop_after=False)
                time.sleep(0.6)
            order_back = draft_order()
            check("拖回首位后草稿顺序复原", order_back and order_back[0] == MODEL, order_back)
            cdp.shot("13-drag-back-to-first")
            # 上移/下移按钮必须仍然存在且有效（拖拽不是唯一途径，test-spec §4 行 4d）。
            up_btns = cdp.val("[...document.querySelectorAll('button[aria-label]')]"
                              ".filter(function(b){return b.getAttribute('aria-label')==='Move up';}).length")
            check("上移/下移按钮仍然存在（拖拽不是唯一重排途径）", (up_btns or 0) == 3, up_btns)
            cdp.val(js_click_exact(["Save", "保存"]))
            time.sleep(3)
            s, route = req(base, "GET", "/api/v1/routes/" + MODEL, key=admin_key)
            got_order = [m.get("model") for m in (route.get("members") or [])]
            got_prio = [m.get("priority") for m in (route.get("members") or [])]
            check("保存后回读成员顺序与草稿一致（拖拽结果已固化）",
                  s == 200 and got_order and got_order[0] == MODEL, got_order)
            check("回读 priority 与新下标一一对应（降序，数字大者优先）",
                  got_prio == sorted(got_prio, reverse=True) and len(got_prio) == 3, got_prio)
        else:
            check("每个成员行都有拖拽手柄（真实拖拽的按下点）", False, "成员行数不足 3")
            check("拖到末位后草稿顺序变了（第 1 行移到末位）", False, "成员行数不足 3")
            check("拖回首位后草稿顺序复原", False, "成员行数不足 3")
            check("上移/下移按钮仍然存在（拖拽不是唯一重排途径）", False, "成员行数不足 3")
            check("保存后回读成员顺序与草稿一致（拖拽结果已固化）", False, "成员行数不足 3")
            check("回读 priority 与新下标一一对应（降序，数字大者优先）", False, "成员行数不足 3")

        # ---- 用户动作 7c：人工停用成员并验证端到端不再命中（test-spec §4 行 4e）----
        log("")
        log("=== 用户动作 7c：点击开关停用成员 → 保存 → 端到端不再命中 → 打开恢复 ===")
        s, route = req(base, "GET", "/api/v1/routes/" + MODEL, key=admin_key)
        # 成员身份 = (渠道, 所选模型) → 用 model 定位行与断言成员；
        # upstream_model 是派生真名（转发目标）→ 用 X-Served-By 断言"命中了谁"（ADR 0008）。
        first_model = (route.get("members") or [{}])[0].get("model")
        first_upstream = (route.get("members") or [{}])[0].get("upstream_model")
        check("停用前该成员确实参与选路（X-Served-By 命中它）",
              first_upstream and (", model=%s" % first_upstream) in (http_served_by(base, MODEL, client_key) or ""),
              (first_upstream, http_served_by(base, MODEL, client_key)))
        cdp.nav(base + "/routes", wait=4)
        cdp.val(js_click_exact(["Edit members", "编辑成员链"]))
        check("编排器再次打开", wait_for("!!document.querySelector('#lane-route-key')", 15))
        time.sleep(0.8)
        toggled = cdp.val(js_click_member_switch_by_model(first_model))
        check("点中头名成员的开关（按成员行文本定位）", bool(toggled), first_model)
        time.sleep(0.6)
        cdp.shot("14-member-toggled-off")
        # 关闭态必须可见地降透明度 + 短标记（ui-spec §6.3：不得只靠开关本身）。
        composer_text = cdp.val("document.body.innerText") or ""
        check("关闭态出现「人工关闭」可见标记", "Manually disabled" in composer_text, composer_text[-300:])
        cdp.val(js_click_exact(["Save", "保存"]))
        time.sleep(3)
        s, route = req(base, "GET", "/api/v1/routes/" + MODEL, key=admin_key)
        members_after = route.get("members") or []
        disabled_members = [m for m in members_after if m.get("enabled") is False]
        check("回读该成员 enabled=false 且仍在成员链里、位置不变",
              s == 200 and len(disabled_members) == 1
              and disabled_members[0].get("model") == first_model
              and members_after[0].get("model") == first_model,
              members_after)
        check("停用不改变成员数（开关不是删除别名）",
              len(members_after) == 3, [m.get("model") for m in members_after])
        # 端到端：头名成员被停用 → 不再命中它（X-Served-By 换人）。
        # X-Served-By 只在成功响应上（design-v1 §4.1），形态
        # `channel=<id>:<name>, model=<upstream>`；成员同属一个渠道，所以判据是
        # "命中的上游不再是它"，而不是渠道名。
        served_off = http_served_by(base, MODEL, client_key)
        check("停用后端到端仍成功（逃逸到次成员）", served_off is not None, served_off)
        check("停用后不再命中被关闭成员（X-Served-By 换人）",
              served_off is not None and (", model=%s" % first_upstream) not in served_off,
              served_off)
        # 重新打开 → 端到端必须恢复命中（test-spec §4 行 4e：必须实测恢复）。
        cdp.val(js_click_exact(["Edit members", "编辑成员链"]))
        wait_for("!!document.querySelector('#lane-route-key')", 15)
        time.sleep(0.8)
        cdp.val(js_click_member_switch_by_model(first_model))
        time.sleep(0.5)
        cdp.val(js_click_exact(["Save", "保存"]))
        time.sleep(3)
        s, route = req(base, "GET", "/api/v1/routes/" + MODEL, key=admin_key)
        check("重新打开后 enabled=true（读回写往返不丢）",
              s == 200 and all(m.get("enabled") is not False for m in (route.get("members") or [])),
              route.get("members"))
        served_on = http_served_by(base, MODEL, client_key)
        check("重新打开后端到端恢复命中被恢复的成员",
              served_on is not None and (", model=%s" % first_upstream) in served_on,
              served_on)
        # 卡片摘要与运行态：被停用成员标灰 + 「人工关闭」徽章（与 Cooldown/Circuit 可区分）。
        # 注意：上一步保存后弹窗会自动关闭，这里必须重新打开编辑器再切开关，
        # 否则点击落在已卸载的 DOM 上（实测：静默 no-op，卡片断言假失败）。
        cdp.val(js_click_exact(["Edit members", "编辑成员链"]))
        check("编排器第三次打开（卡片断言的前置）",
              wait_for("!!document.querySelector('#lane-route-key')", 15))
        time.sleep(0.8)
        cdp.val(js_click_member_switch_by_model(first_model))
        time.sleep(0.4)
        cdp.val(js_click_exact(["Save", "保存"]))
        time.sleep(3)
        cdp.nav(base + "/routes", wait=5)
        card_body = cdp.val("document.body.innerText") or ""
        check("卡片摘要标出被人工停用的成员", "Manually disabled" in card_body, card_body[-300:])
        cdp.shot("15-card-disabled-member")
        # 复原（供后续日志/设置动作使用）。
        cdp.val(js_click_exact(["Edit members", "编辑成员链"]))
        wait_for("!!document.querySelector('#lane-route-key')", 15)
        time.sleep(0.8)
        cdp.val(js_click_member_switch_by_model(first_model))
        time.sleep(0.5)
        cdp.val(js_click_exact(["Save", "保存"]))
        time.sleep(3)

        # ---- 用户动作 7d：改渠道映射即时生效（ADR 0008 的核心修复）----
        log("")
        log("=== 用户动作 7d：渠道模型映射改动立即作用于已建车道 ===")
        # ADR 0008 之前：加入成员时把渠道映射"物化"写进成员字段，之后改映射对该成员
        # 完全失效。现在成员只存所选模型，真名由映射推导，所以**不重存车道**也应立即变化。
        s, before_route = req(base, "GET", "/api/v1/routes/" + MODEL, key=admin_key)
        before_upstream = (before_route.get("members") or [{}])[0].get("upstream_model")
        # 给渠道加一条映射：所选模型 → 一个刻意不同的真名（fake 上游接受任意模型名）。
        s, ch_detail = req(base, "GET", "/api/v1/channels/" + CHANNEL, key=admin_key)
        original_mapping = ch_detail.get("model_mapping") or {}
        new_mapping = dict(original_mapping)
        new_mapping[first_model] = "ui-renamed/" + first_model
        put_body = {
            "type": ch_detail.get("type"),
            "base_url": ch_detail.get("base_url"),
            "models": ch_detail.get("models"),
            "enabled": True,
            "model_mapping": new_mapping,
        }
        # 渠道更新需要 key；用与建渠道时相同的 up_key（脚本内变量）。
        put_body["key"] = up_key
        s, _ = req(base, "PUT", "/api/v1/channels/" + CHANNEL, put_body, key=admin_key)
        check("渠道映射更新成功", s == 200, s)
        # **不重存车道**，直接回读：真名必须已跟随映射。
        s, after_route = req(base, "GET", "/api/v1/routes/" + MODEL, key=admin_key)
        after_upstream = (after_route.get("members") or [{}])[0].get("upstream_model")
        after_model = (after_route.get("members") or [{}])[0].get("model")
        check("成员存的仍是所选模型（未被真名污染）", after_model == first_model, after_model)
        check("改渠道映射后真名立即生效，无需重存车道",
              after_upstream == "ui-renamed/" + first_model and after_upstream != before_upstream,
              (before_upstream, after_upstream))
        # 端到端也应按新真名转发（X-Served-By 的 model= 段来自真名）。
        served_mapped = http_served_by(base, MODEL, client_key)
        check("端到端按新真名转发",
              served_mapped is not None and ("model=ui-renamed/" + first_model) in served_mapped,
              served_mapped)
        # 复原映射，避免影响后续动作。
        put_body["model_mapping"] = original_mapping
        req(base, "PUT", "/api/v1/channels/" + CHANNEL, put_body, key=admin_key)

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
