#!/usr/bin/env python3
"""针对真实数据实例的浏览器验收（ADR 0008 新功能真实渲染 + 交互）。

与 verify/e2e-ui/user_journey.py 的区别：那个脚本自包含（自己起空实例、自己造数据）；
本脚本**指向一个已存在的实例**（用现网备份数据迁移出来的副本），只验证
"真实数据下新功能的渲染与交互"，因此不做初始化与造数。

用法：
    python3 verify/e2e-ui/real_data_check.py --base http://127.0.0.1:8194 --password '...'

缺 chromium / websocket-client 时 SKIP（退出码 2），不包装成 PASS。
"""
import argparse
import base64
import hashlib
import json
import os
import shutil
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request

try:
    import websocket
except ImportError:
    websocket = None

REPO = os.environ.get("PBR_REPO") or os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "..")
)
WORK = "/tmp/pbr-realtest-ui"
STAMP = time.strftime("%Y%m%d-%H%M%S")
EVID = os.path.join(REPO, "verify", "e2e-ui", "realdata-%s.log" % STAMP)
SHOTS = os.path.join(REPO, "verify", "e2e-ui", "shots")


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


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
                self.console_errors.append(
                    str(d["params"].get("exceptionDetails", {}).get("text", ""))[:240]
                )
            if d.get("method") == "Runtime.consoleAPICalled" and d["params"].get("type") == "error":
                self.console_errors.append(
                    " ".join(str(a.get("value", "")) for a in d["params"].get("args", []))[:240]
                )
            if d.get("id") == mid:
                return d
        return {}

    def val(self, expr, timeout=30):
        r = self.send(
            "Runtime.evaluate",
            {"expression": expr, "returnByValue": True, "awaitPromise": True},
            timeout=timeout,
        )
        return ((r.get("result") or {}).get("result") or {}).get("value")

    def nav(self, url, wait=3.0):
        self.send("Page.navigate", {"url": url})
        time.sleep(wait)
        for _ in range(24):
            if self.val("document.body && document.body.innerText.length>20", timeout=5):
                break
            time.sleep(0.5)

    def shot(self, name):
        try:
            r = self.send("Page.captureScreenshot", {"format": "png"})
            data = (r.get("result") or {}).get("data")
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


JS_SET_SELECTOR = (
    "(function(){var el=document.querySelector(%s);if(!el) return false;"
    "var p=Object.getPrototypeOf(el);var d=Object.getOwnPropertyDescriptor(p,'value');"
    "if(d&&d.set){d.set.call(el,%s);}else{el.value=%s;}"
    "el.dispatchEvent(new Event('input',{bubbles:true}));"
    "el.dispatchEvent(new Event('change',{bubbles:true}));return true;})()"
)
JS_CLICK_EXACT = (
    "(function(){var want=%s;var bs=[...document.querySelectorAll('button')];"
    "for(var i=0;i<bs.length;i++){var s=(bs[i].textContent||'').trim();"
    "for(var j=0;j<want.length;j++){if(s===want[j]){bs[i].click();return s;}}}"
    "return false;})()"
)
JS_CLICK_CONTAINS = (
    "(function(){var want=%s;var bs=[...document.querySelectorAll('button')];"
    "for(var i=0;i<bs.length;i++){var s=(bs[i].textContent||'').trim();"
    "for(var j=0;j<want.length;j++){if(s.indexOf(want[j])>=0){bs[i].click();return s;}}}"
    "return false;})()"
)
JS_SET_BY_NAME = (
    "(function(){var el=document.querySelector('input[name=%s],textarea[name=%s]');"
    "if(!el) return false;var p=Object.getPrototypeOf(el);"
    "var d=Object.getOwnPropertyDescriptor(p,'value');"
    "if(d&&d.set){d.set.call(el,%s);}else{el.value=%s;}"
    "el.dispatchEvent(new Event('input',{bubbles:true}));"
    "el.dispatchEvent(new Event('change',{bubbles:true}));return true;})()"
)


def js_set_selector(sel, val):
    return JS_SET_SELECTOR % (json.dumps(sel), json.dumps(val), json.dumps(val))


def js_set_by_name(name, val):
    return JS_SET_BY_NAME % (json.dumps(name), json.dumps(name), json.dumps(val), json.dumps(val))


def js_click_exact(texts):
    return JS_CLICK_EXACT % json.dumps(texts, ensure_ascii=False)


def js_click_contains(texts):
    return JS_CLICK_CONTAINS % json.dumps(texts, ensure_ascii=False)


# 打开**指定车道**卡片的成员编排器。
#
# 两个坑（都实测踩到）：
#  1. 卡片按名字排序，"第一个 Edit members" 未必是目标车道——脚本曾改了
#     车道 A 却回读车道 B，得到假失败。
#  2. 不能用 `textContent.indexOf(lane)` 认卡片：车道名可能作为**成员模型名**出现在
#     别的卡片里（某车道的成员恰好与另一条车道同名）。必须按"卡片文本以车道名开头"匹配。
CARD_OPEN_JS = (
    "(function(){var want=%s;"
    "var btns=[...document.querySelectorAll('button')].filter(function(b){"
    "  var t=(b.textContent||'').trim();return t==='Edit members'||t==='编辑成员链';});"
    "for(var i=0;i<btns.length;i++){var n=btns[i].parentElement,card=null;"
    "  while(n&&n!==document.body){"
    "    if(n.matches && n.matches('[class*=Card],[data-slot*=card]')){card=n;break;}"
    "    n=n.parentElement;}"
    "  var scope=card||btns[i].parentElement;"
    "  var txt=(scope.textContent||'').replace(/\\s+/g,'');"
    "  if(txt.indexOf(want)===0){btns[i].click();return 'ok';}}"
    "return 'nocard';})()"
)


# 成员行：从拖拽手柄向上找最低的、含启停开关的祖先（与 user_journey.py 同口径）。
JS_MEMBER_ROWS = (
    "(function(){var rows=[];var seen=new Set();"
    "var handles=document.querySelectorAll('[draggable]');"
    "for(var i=0;i<handles.length;i++){var n=handles[i].parentElement;var row=null;"
    "while(n&&n!==document.body){"
    "if(n.querySelector('[role=switch]')){row=n;break;}n=n.parentElement;}"
    "if(row&&!seen.has(row)){seen.add(row);rows.push(row);}}return rows;})()"
)
# 每个成员行是否显示「解析后的上游」只读文本，以及是否还有上游输入框。
JS_MEMBER_SHAPE = (
    "(function(){var rows=%s;return rows.map(function(d){"
    "var txt=d.textContent||'';"
    "var inputs=[...d.querySelectorAll('input')].map(function(i){return i.getAttribute('aria-label')||'';});"
    "return {text:txt.slice(0,120), inputs:inputs};});})()" % JS_MEMBER_ROWS
)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True, help="已存在的 PBR 实例基址")
    ap.add_argument("--password", required=True, help="登录口令（用于页面登录）")
    args = ap.parse_args()
    base = args.base.rstrip("/")
    pw = args.password

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

    def check(desc, ok, detail=""):
        if ok:
            log("  PASS: " + desc)
            PASS["n"] += 1
        else:
            log("  FAIL: " + desc + (" | " + str(detail)[:300] if detail else ""))
            FAIL["n"] += 1

    log("=== 真实数据实例浏览器验收 @ %s ===" % STAMP)
    log("base = %s" % base)

    admin_key = admin_key_of(pw)
    procs = []
    cdp = None
    try:
        s, health = req(base, "GET", "/api/v1/health")
        check("实例可访问", s == 200, (s, health))
        if s != 200:
            return 1

        # 真实数据前置：必须有车道
        s, models = req(base, "GET", "/api/v1/models", key=admin_key)
        explicit = [m for m in (models.get("items") or []) if m.get("source") == "explicit"]
        check("实例有真实车道数据（前置）", len(explicit) > 0, explicit[:3])
        if not explicit:
            return 1
        # 取成员最多的车道做 UI 验证
        target = max(explicit, key=lambda m: m.get("member_count", 0))
        lane = target["model"]
        log("  验证目标车道: %s（%d 成员）" % (lane, target.get("member_count", 0)))

        cdp_port = free_port()
        proc = subprocess.Popen(
            [chrome, "--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
             "--no-first-run", "--no-default-browser-check", "--lang=zh-CN", "--accept-lang=zh-CN",
             "--window-size=1600,1200",
             "--remote-debugging-port=%d" % cdp_port, "--remote-allow-origins=*",
             "--user-data-dir=" + WORK + "/profile", "about:blank"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
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
            log("FAIL: DevTools 未就绪")
            return 1
        cdp = CDP(ws_url)
        cdp.send("Runtime.enable")
        cdp.send("Page.enable")

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

        # ---- 1) 真实登录 ----
        log("")
        log("=== 1) 用真实口令登录 ===")
        cdp.nav(base + "/sign-in", wait=3)
        check("进入 /sign-in", cdp.val("location.pathname") == "/sign-in", cdp.val("location.pathname"))
        cdp.val(js_set_by_name("password", pw))
        time.sleep(0.4)
        cdp.val(js_click_exact(["Sign in", "登录"]))
        check("登录进入控制台", wait_for("location.pathname.indexOf('/dashboard')===0 || location.pathname==='/'", 25),
              cdp.val("location.pathname"))
        cdp.shot("rd-01-login")

        # ---- 2) 路由页：真实车道卡片 ----
        log("")
        log("=== 2) 路由页显示真实车道卡片 ===")
        cdp.nav(base + "/routes", wait=5)
        body = cdp.val("document.body.innerText") or ""
        check("路由页出现真实车道 %s" % lane, lane in body, body[-300:])
        # 中文渠道名应正常显示（真实数据里有中文渠道）
        check("车道卡片渲染了成员摘要", "members" in body or "成员" in body, body[-300:])
        cdp.shot("rd-02-routes")

        # ---- 3) 编排器：成员行是只读上游名（无输入框）+ 启停开关 ----
        log("")
        log("=== 3) 编排器成员行：只读上游名 + 启停开关（ADR 0008）===")
        # 必须打开**目标车道**的编排器：卡片按名字排序，第一个 Edit members 未必是它
        # （实测踩到：脚本改了车道 A 却回读车道 B → 假失败）。
        # 定位方式：找到含目标车道名的卡片，点它内部的 Edit members。
        opened = cdp.val(CARD_OPEN_JS % json.dumps(lane))
        check("打开目标车道 %s 的编排器" % lane, opened == "ok", opened)
        check("编排器打开", wait_for("!!document.querySelector('#lane-route-key')", 15))
        # 确认打开的是目标车道（路由键输入框的值）
        got_key = cdp.val("(function(){var el=document.querySelector('#lane-route-key');return el?el.value:'';})()")
        check("编排器载入的是目标车道（路由键匹配）", got_key == lane, (got_key, lane))
        time.sleep(1.2)
        shape = cdp.val(JS_MEMBER_SHAPE) or []
        check("成员行数 > 0", len(shape) > 0, len(shape))
        # 每行都应只读展示「解析后的上游」，且**没有**上游真名输入框
        upstream_inputs = [i for m in shape for i in m["inputs"] if "Upstream model" in i or "上游" in i]
        check("成员行不再有上游真名输入框（ADR 0008 移除成员级改名）",
              len(upstream_inputs) == 0, upstream_inputs)
        resolved = cdp.val(
            "[...document.querySelectorAll('*')].filter(function(e){"
            "return e.children.length===0 && /Resolved upstream|解析后的上游/.test(e.textContent||'');}).length"
        )
        check("成员行显示「解析后的上游」只读文本", (resolved or 0) > 0, resolved)
        switches = cdp.val("document.querySelectorAll('[role=switch]').length")
        check("每个成员行有启停开关", (switches or 0) >= len(shape), (switches, len(shape)))
        cdp.shot("rd-03-composer")

        # ---- 4) 真实交互：点击开关停用首成员 → 保存 → 回读 ----
        log("")
        log("=== 4) 真实点击开关停用首成员并保存 ===")
        s, before = req(base, "GET", "/api/v1/routes/" + lane, key=admin_key)
        first_model = (before.get("members") or [{}])[0].get("model")
        first_chan = (before.get("members") or [{}])[0].get("channel")
        first_enabled = (before.get("members") or [{}])[0].get("enabled")
        log("  首成员: %s / %s (enabled=%s)" % (first_chan, first_model, first_enabled))

        # 点**第 0 行**的开关。
        # 不能按"行文本含模型名"匹配：真实数据里同一模型名可在多个渠道出现
        # （某车道的同一模型名在 4 个渠道各出现一次），按文本会点错行（实测踩到）。
        # 行序与后端 priority 降序一致，故第 0 行就是首成员。
        toggled = cdp.val(
            "(function(){var rows=%s;if(rows.length===0)return false;"
            "var sw=rows[0].querySelector('[role=switch]');"
            "if(!sw)return false;sw.click();return true;})()" % JS_MEMBER_ROWS
        )
        check("点中首成员（第 0 行）的启停开关", bool(toggled), first_model)
        time.sleep(0.5)
        cdp.val(js_click_exact(["Save", "保存"]))
        # 轮询等落库：固定 sleep 可能在写完成前回读（读到旧值 → 假失败）。
        m0 = {}
        for _ in range(20):
            time.sleep(0.5)
            s, after = req(base, "GET", "/api/v1/routes/" + lane, key=admin_key)
            m0 = (after.get("members") or [{}])[0]
            if bool(m0.get("enabled")) != bool(first_enabled):
                break
        check("回读首成员 enabled 已翻转且仍在链里、位置不变",
              s == 200 and m0.get("model") == first_model and m0.get("enabled") != first_enabled,
              m0)
        cdp.shot("rd-04-toggled")

        # ---- 5) 真实端到端：停用者不再被命中 ----
        log("")
        log("=== 5) 端到端：被停用成员不再命中 ===")
        s, kd = req(base, "GET", "/api/v1/keys", key=admin_key)
        # 建一个临时客户端密钥用于端到端
        # 唯一名：重复跑脚本时同名密钥会 409（实测踩到，导致"无客户端密钥"假失败）。
        key_name = "realtest-ui-%s" % STAMP
        s, newk = req(base, "POST", "/api/v1/keys", {"name": key_name}, key=admin_key)
        ck = newk.get("key") if isinstance(newk, dict) else None
        if not ck:
            # 退路：直接用已有密钥的明文（不可能，明文只在创建时给一次），故报明原因。
            check("创建端到端用客户端密钥", False, (s, newk))
        if ck:
            r = urllib.request.Request(base + "/v1/chat/completions",
                                       data=json.dumps({"model": lane, "messages": [{"role": "user", "content": "hi"}]}).encode(),
                                       method="POST")
            r.add_header("Content-Type", "application/json")
            r.add_header("Authorization", "Bearer " + ck)
            served = None
            try:
                with urllib.request.urlopen(r, timeout=30) as resp:
                    served = resp.headers.get("X-Served-By")
            except urllib.error.HTTPError as e:
                served = e.headers.get("X-Served-By")
            check("端到端成功且未命中被停用成员",
                  served is not None and ("model=%s" % first_model) not in served, served)
            # 日志留痕 disabled
            time.sleep(1)
            s, logs = req(base, "GET", "/api/v1/logs?limit=5", key=admin_key)
            found = False
            for it in (logs.get("items") or []):
                if it.get("lane") == lane:
                    for a in (it.get("attempts") or []):
                        if a.get("status") == "disabled" and first_model in a.get("member", ""):
                            found = True
            check("attempts 链留痕 disabled（新标签 channel/模型）", found, logs.get("items", [])[:1])
        else:
            check("端到端成功且未命中被停用成员", False, "无客户端密钥")
            check("attempts 链留痕 disabled（新标签 channel/模型）", False, "无客户端密钥")

        # ---- 6) 复原开关（真实数据不留副作用）----
        log("")
        log("=== 6) 复原开关（不给真实数据留副作用）===")
        cdp.nav(base + "/routes", wait=4)
        cdp.val(CARD_OPEN_JS % json.dumps(lane))
        wait_for("!!document.querySelector('#lane-route-key')", 15)
        time.sleep(1.0)
        cdp.val(
            "(function(){var rows=%s;if(rows.length===0)return false;"
            "var sw=rows[0].querySelector('[role=switch]');if(!sw)return false;"
            "sw.click();return true;})()" % JS_MEMBER_ROWS
        )
        time.sleep(0.5)
        cdp.val(js_click_exact(["Save", "保存"]))
        time.sleep(3)
        s, restored = req(base, "GET", "/api/v1/routes/" + lane, key=admin_key)
        r0 = (restored.get("members") or [{}])[0]
        check("首成员已复原为启用", r0.get("enabled") is True, r0)

        # ---- 7) 无 console 报错 ----
        log("")
        log("=== 7) 页面无 console 报错 ===")
        errs = [e for e in cdp.console_errors if e.strip()]
        check("无页面级 console 报错", len(errs) == 0, errs[:3])

        log("")
        log("=== 结果：PASS=%d FAIL=%d ===" % (PASS["n"], FAIL["n"]))
        log("=== evidence: %s ===" % EVID)
        if FAIL["n"] == 0:
            log("PASS: 真实数据实例浏览器验收通过")
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
        logfh.close()


if __name__ == "__main__":
    shutil.rmtree(WORK, ignore_errors=True)
    os.makedirs(WORK, exist_ok=True)
    sys.exit(main())
