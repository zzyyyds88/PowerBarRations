#!/usr/bin/env python3
"""完整使用流程走查（无头 Chromium + CDP），真实操作 UI，不是只看渲染。

流程：
  1. 首启设口令（故意用很短的密码，验证"不限制位数"）→ 拿到一次性管理密钥 → 进入控制台
  2. 逐页导航（Dashboard/Lanes/Channels/Models/Logs/Keys/Playground/Settings）：渲染 + 无 console 报错
  3. 渠道页真实建一个渠道（填表 → 保存 → 列表出现）
  4. 密钥页真实建一个客户端密钥（拿到一次性明文）
  5. Playground 页真实选模型 + 填内容 + 发送 → 断言假上游回复出现
  6. 日志页断言出现该请求
  7. 设置页改口令 → 清本地凭据 → 用新口令重新登录
逐页截图落 out_dir。

用法：console_flow.py <base_url> <repo_root> <out_dir> <setup_password>
输出：JSON 到 stdout；退出码 0=全过，1=有失败，2=环境不支持。
"""

import base64
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
except ImportError:
    websocket = None

NOISE = re.compile(
    r"dbus|libva|vaapi|gpu|GL |EGL|Fontconfig|shared_memory|Failed to connect to the bus|"
    r"sandbox|Vulkan|angle|DevTools|Autofill|net::ERR_|favicon|"
    r"Failed to load resource|404 \(Not Found\)|Download the React DevTools",
    re.I,
)


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class CDP:
    def __init__(self, ws_url: str):
        self.ws = websocket.create_connection(ws_url, timeout=40)
        self.next_id = 1
        self.console_errors: list[str] = []

    def send(self, method: str, params: dict | None = None, timeout: float = 30) -> dict:
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
            if data.get("method") == "Runtime.consoleAPICalled" and data["params"].get("type") == "error":
                parts = [str(a.get("value", a.get("description", ""))) for a in data["params"].get("args", [])]
                text = " ".join(p for p in parts if p)
                if not NOISE.search(text):
                    self.console_errors.append(text[:300])
            elif data.get("method") == "Runtime.exceptionThrown":
                detail = data["params"].get("exceptionDetails", {})
                text = str(detail.get("text", "")) + " " + str((detail.get("exception") or {}).get("description", ""))
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
        return self.send("Runtime.evaluate", {"expression": expression, "returnByValue": True, "awaitPromise": True})

    def value(self, expression: str):
        res = self.evaluate(expression)
        return ((res.get("result") or {}).get("result") or {}).get("value")

    def screenshot(self, path: str):
        shot = self.send("Page.captureScreenshot", {"format": "png"})
        data = ((shot.get("result") or {}).get("data")) or ""
        if data:
            with open(path, "wb") as f:
                f.write(base64.b64decode(data))

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


def load_messages(repo: str, locale: str) -> dict:
    path = os.path.join(repo, "web/src/locales", locale + ".json")
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def pick(messages: dict, dotted: str) -> list[str]:
    node = messages
    for part in dotted.split("."):
        if not isinstance(node, dict):
            return []
        node = node.get(part)
    if isinstance(node, str):
        return [node]
    return []


JS_SET = """
(function(){
  var cands = __CANDS__;
  var value = __VALUE__;
  var labels = Array.prototype.slice.call(document.querySelectorAll('label'));
  for (var i = 0; i < labels.length; i++) {
    var lab = labels[i];
    var span = lab.querySelector('span');
    var text = (span ? span.textContent : lab.textContent) || '';
    var hit = cands.some(function(c){ return c && text.indexOf(c) >= 0; });
    if (!hit) continue;
    var el = lab.querySelector('input, select, textarea');
    if (!el) continue;
    var proto = el.tagName === 'SELECT' ? HTMLSelectElement.prototype : (el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype);
    var setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
    setter.call(el, value);
    el.dispatchEvent(new Event('input', {bubbles:true}));
    el.dispatchEvent(new Event('change', {bubbles:true}));
    return true;
  }
  return false;
})()
"""

JS_SET_BY_PLACEHOLDER = """
(function(){
  var cands = __CANDS__;
  var value = __VALUE__;
  var els = Array.prototype.slice.call(document.querySelectorAll('input, textarea'));
  for (var i = 0; i < els.length; i++) {
    var el = els[i];
    var ph = el.getAttribute('placeholder') || '';
    if (cands.some(function(c){ return c && ph.indexOf(c) >= 0; })) {
      var proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      var setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
      setter.call(el, value);
      el.dispatchEvent(new Event('input', {bubbles:true}));
      el.dispatchEvent(new Event('change', {bubbles:true}));
      return true;
    }
  }
  return false;
})()
"""

JS_SET_BY_AUTOCOMPLETE = """
(function(){
  var ac = __AC__;
  var value = __VALUE__;
  var els = Array.prototype.slice.call(document.querySelectorAll('input'));
  var hit = false;
  var setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
  for (var i = 0; i < els.length; i++) {
    if ((els[i].getAttribute('autocomplete') || '') === ac) {
      setter.call(els[i], value);
      els[i].dispatchEvent(new Event('input', {bubbles:true}));
      els[i].dispatchEvent(new Event('change', {bubbles:true}));
      hit = true;
    }
  }
  return hit;
})()
"""

JS_CLICK = """
(function(){
  var cands = __CANDS__;
  var btns = Array.prototype.slice.call(document.querySelectorAll('button'));
  for (var i = 0; i < btns.length; i++) {
    var text = (btns[i].textContent || '').trim();
    if (cands.some(function(c){ return c && text.indexOf(c) >= 0; })) { btns[i].click(); return text; }
  }
  return '';
})()
"""

JS_CLICK_LAST = """
(function(){
  var cands = __CANDS__;
  var btns = Array.prototype.slice.call(document.querySelectorAll('button'));
  var picked = null;
  for (var i = 0; i < btns.length; i++) {
    var text = (btns[i].textContent || '').trim();
    if (cands.some(function(c){ return c && text.indexOf(c) >= 0; })) { picked = btns[i]; }
  }
  if (picked) { picked.click(); return picked.textContent.trim(); }
  return '';
})()
"""


def js_set(candidates: list[str], value: str) -> str:
    return JS_SET.replace("__CANDS__", json.dumps(candidates)).replace("__VALUE__", json.dumps(value))


def js_set_placeholder(candidates: list[str], value: str) -> str:
    return JS_SET_BY_PLACEHOLDER.replace("__CANDS__", json.dumps(candidates)).replace("__VALUE__", json.dumps(value))


JS_VALUE_IN_LABEL = """
(function(){
  var cands = __CANDS__;
  var labels = Array.prototype.slice.call(document.querySelectorAll('label'));
  for (var i = 0; i < labels.length; i++) {
    var s = labels[i].textContent || '';
    if (cands.some(function(c){ return c && s.indexOf(c) >= 0; })) {
      var el = labels[i].querySelector('input, select, textarea');
      if (el) return el.value;
    }
  }
  return null;
})()
"""

JS_TYPE_AUTOCOMPLETE = """
(function(){
  var ac = __AC__;
  var value = __VALUE__;
  var els = Array.prototype.slice.call(document.querySelectorAll('input'));
  var hit = 0;
  for (var i = 0; i < els.length; i++) {
    if ((els[i].getAttribute('autocomplete') || '') === ac) {
      els[i].focus();
      els[i].select();
      document.execCommand('insertText', false, value);
      hit++;
    }
  }
  return hit;
})()
"""

JS_CLICK_WITH_SIBLING = """
(function(){
  var cands = __CANDS__;
  var sib = __SIB__;
  var btns = Array.prototype.slice.call(document.querySelectorAll('button'));
  for (var i = 0; i < btns.length; i++) {
    var t = (btns[i].textContent || '').trim();
    if (!cands.some(function(c){ return c && t.indexOf(c) >= 0; })) continue;
    var parent = btns[i].parentElement;
    var siblings = parent ? Array.prototype.slice.call(parent.querySelectorAll('button')) : [];
    var has = siblings.some(function(b){ var x = (b.textContent||'').trim(); return sib.some(function(c){ return c && x.indexOf(c) >= 0; }); });
    if (has && !btns[i].disabled) { btns[i].click(); return t; }
  }
  return '';
})()
"""


def js_type_autocomplete(autocomplete: str, value: str) -> str:
    return JS_TYPE_AUTOCOMPLETE.replace("__AC__", json.dumps(autocomplete)).replace("__VALUE__", json.dumps(value))


def js_value_in_label(candidates: list[str]) -> str:
    return JS_VALUE_IN_LABEL.replace("__CANDS__", json.dumps(candidates))


def js_click_with_sibling(candidates: list[str], sibling: list[str]) -> str:
    return JS_CLICK_WITH_SIBLING.replace("__CANDS__", json.dumps(candidates)).replace("__SIB__", json.dumps(sibling))


def js_set_autocomplete(autocomplete: str, value: str) -> str:
    return JS_SET_BY_AUTOCOMPLETE.replace("__AC__", json.dumps(autocomplete)).replace("__VALUE__", json.dumps(value))


def js_click(candidates: list[str]) -> str:
    return JS_CLICK.replace("__CANDS__", json.dumps(candidates))


def js_click_last(candidates: list[str]) -> str:
    return JS_CLICK_LAST.replace("__CANDS__", json.dumps(candidates))


def main() -> int:
    if len(sys.argv) < 5:
        print("usage: console_flow.py <base_url> <repo_root> <out_dir> <setup_password>")
        return 2
    base, repo, out_dir, password = sys.argv[1:5]
    if websocket is None:
        print(json.dumps({"skipped": "python websocket-client 未安装"}))
        return 2
    chrome = shutil.which("chromium") or shutil.which("chromium-browser") or shutil.which("google-chrome")
    if not chrome:
        print(json.dumps({"skipped": "未找到 chromium"}))
        return 2

    zh = load_messages(repo, "zh_hans")
    en = load_messages(repo, "en")

    def T(dotted: str) -> list[str]:
        out = []
        out.extend(pick(zh, dotted))
        out.extend(pick(en, dotted))
        return out

    os.makedirs(out_dir, exist_ok=True)
    steps = []

    def record(name: str, ok: bool, detail: str = ""):
        steps.append({"step": name, "ok": bool(ok), "detail": str(detail)[:400]})
        print(("  PASS: " if ok else "  FAIL: ") + name + ((" | " + str(detail)[:200]) if detail and not ok else ""))

    port = free_port()
    profile = os.path.join(out_dir, "chrome-profile")
    proc = subprocess.Popen(
        [
            chrome, "--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
            "--no-first-run", "--no-default-browser-check", "--ignore-certificate-errors",
            "--allow-insecure-localhost", "--lang=zh-CN", "--accept-lang=zh-CN,zh;q=0.9",
            f"--remote-debugging-port={port}", "--remote-allow-origins=*",
            f"--user-data-dir={profile}", "about:blank",
        ],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    try:
        ws_url = ""
        for _ in range(80):
            try:
                targets = json.loads(urllib.request.urlopen(f"http://127.0.0.1:{port}/json/list", timeout=5).read())
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

        def wait_for(expr: str, timeout: float = 20) -> bool:
            deadline = time.time() + timeout
            while time.time() < deadline:
                try:
                    if cdp.value(expr):
                        return True
                except Exception:
                    pass
                time.sleep(0.4)
            return False

        # 1) 首启设口令（故意用 3 位短口令，验证不限制位数）
        cdp.send("Page.navigate", {"url": base + "/"})
        time.sleep(3)
        cdp.screenshot(os.path.join(out_dir, "01-setup.png"))
        record("打开首启设置页", wait_for("document.body && document.body.innerText.length > 0"), cdp.value("document.body.innerText.slice(0,120)"))

        cdp.evaluate(js_set_placeholder(T("auth.password"), password))
        cdp.evaluate(js_set_placeholder(T("auth.confirmPassword"), password))
        clicked = cdp.value(js_click(T("auth.submit")))
        record("提交短口令（不限制位数）", bool(clicked), "clicked=" + str(clicked))
        got_key = wait_for("!!localStorage.getItem('pbr.adminKey')", 25)
        admin_key = cdp.value("localStorage.getItem('pbr.adminKey')") if got_key else ""
        record("取得管理密钥并落本地", bool(admin_key), "len=" + str(len(admin_key or "")))
        cdp.screenshot(os.path.join(out_dir, "02-issued-key.png"))
        cdp.evaluate(js_click(T("auth.savedIt")))
        record("确认已保存进入控制台", wait_for("location.pathname === '/' && document.body.innerText.length > 200", 20), cdp.value("location.pathname"))

        # 2) 逐页导航
        pages = [("/", "03-dashboard"), ("/lanes", "04-lanes"), ("/channels", "05-channels"),
                 ("/models", "06-models"), ("/logs", "07-logs"), ("/keys", "08-keys"),
                 ("/playground", "09-playground"), ("/settings", "10-settings")]
        for path, shot in pages:
            cdp.console_errors = []
            cdp.send("Page.navigate", {"url": base + path})
            time.sleep(3)
            cdp.screenshot(os.path.join(out_dir, shot + ".png"))
            html_len = cdp.value("document.documentElement.outerHTML.length") or 0
            errs = list(cdp.console_errors)
            record("页面 " + path + " 渲染且无 console 报错", html_len > 2000 and not errs, "bytes=%s errors=%s" % (html_len, errs[:2]))

        # 3) 渠道页真实建渠道
        cdp.send("Page.navigate", {"url": base + "/channels"})
        time.sleep(2.5)
        cdp.evaluate(js_click(T("channels.new")))
        editor_ready = wait_for("!!Array.from(document.querySelectorAll('label')).find(function(l){var s=l.textContent||'';return s.indexOf('名称')>=0||s.indexOf('Name')>=0;})", 10)
        record("渠道编辑器已打开", editor_ready)
        fields = {
            "name": cdp.value(js_set(T("channels.name"), "flow-channel")),
            "type": cdp.value(js_set(T("channels.type"), "openai")),
            "baseUrl": cdp.value(js_set(T("channels.baseUrl"), "http://127.0.0.1:5711")),
            "priority": cdp.value(js_set(T("channels.priority"), "10")),
            "models": cdp.value(js_set(T("channels.models"), "flow-model")),
            "key": cdp.value(js_set(T("channels.key"), "sk-dev-good")),
        }
        record("渠道表单字段已填写", all(fields.values()), json.dumps(fields, ensure_ascii=False))
        time.sleep(0.5)
        name_after = cdp.value(js_value_in_label(T("channels.name")))
        record("渠道名称在 React 状态中保留", name_after == "flow-channel", "value=%r" % (name_after,))
        cdp.screenshot(os.path.join(out_dir, "11-channel-form.png"))
        cdp.evaluate(js_click_with_sibling(T("common.save"), T("common.cancel")))
        created = wait_for("document.body.innerText.indexOf('flow-channel') >= 0", 20)
        record("渠道页建渠道并出现在列表", created, cdp.value("document.body.innerText.slice(0,200)"))
        cdp.screenshot(os.path.join(out_dir, "12-channel-created.png"))

        # 4) 密钥页建客户端密钥，抓一次性明文
        cdp.send("Page.navigate", {"url": base + "/keys"})
        time.sleep(2.5)
        cdp.evaluate(js_click(T("keys.new")))
        keys_ready = wait_for("!!Array.from(document.querySelectorAll('label')).find(function(l){var s=l.textContent||'';return s.indexOf('名称')>=0||s.indexOf('Name')>=0;})", 10)
        record("密钥编辑器已打开", keys_ready)
        name_set = cdp.value(js_set(T("keys.name"), "flow-key"))
        time.sleep(0.8)
        name_after = cdp.value(js_value_in_label(T("keys.name")))
        record("密钥名称已填写", bool(name_set) and name_after == "flow-key", "set=%s value=%r" % (name_set, name_after))
        cdp.evaluate(js_click_with_sibling(T("keys.new"), T("common.cancel")))
        got_plain = wait_for("(document.body.innerText.match(/pbr-[A-Za-z0-9]{20,}/) || [null])[0] !== null", 12)
        if not got_plain:
            cdp.evaluate(js_click_with_sibling(T("keys.new"), T("common.cancel")))
            got_plain = wait_for("(document.body.innerText.match(/pbr-[A-Za-z0-9]{20,}/) || [null])[0] !== null", 15)
        plain = cdp.value("(document.body.innerText.match(/pbr-[A-Za-z0-9]{20,}/) || [''])[0]") if got_plain else ""
        record("密钥页建客户端密钥并拿到明文", bool(plain), "len=" + str(len(plain or "")))
        cdp.screenshot(os.path.join(out_dir, "13-key-issued.png"))

        # 5) Playground 真发一条
        cdp.send("Page.navigate", {"url": base + "/playground"})
        time.sleep(2.5)
        cdp.evaluate(js_set_placeholder(["pbr-"], plain))
        cdp.evaluate(js_set(T("playground.lane"), "flow-model"))
        cdp.evaluate(js_set_placeholder(T("playground.content"), "你好"))
        time.sleep(0.5)
        cdp.evaluate(js_click(T("playground.send")))
        # 流式时假上游回的是 "pong|<prompt>"，非流式是 "pong from fake upstream"；两者都含 pong。
        replied = wait_for("document.body.innerText.indexOf('pong') >= 0", 30)
        record("Playground 真实发送并收到上游回复", replied, cdp.value("document.body.innerText.slice(0,300)"))
        cdp.screenshot(os.path.join(out_dir, "14-playground-reply.png"))

        # 6) 日志页出现该请求
        cdp.send("Page.navigate", {"url": base + "/logs"})
        time.sleep(3)
        has_log = cdp.value("document.body.innerText.indexOf('flow-model') >= 0")
        record("日志页出现该请求", bool(has_log), cdp.value("document.body.innerText.slice(0,200)"))

        # 7) 设置页改口令 → 用新口令重登
        cdp.send("Page.navigate", {"url": base + "/settings"})
        time.sleep(3)
        new_password = password + "-new"
        cur_set = cdp.value(js_type_autocomplete("current-password", password))
        new_set = cdp.value(js_type_autocomplete("new-password", new_password))
        time.sleep(0.8)
        cur_val = cdp.value("(function(){var e=document.querySelector('input[autocomplete=current-password]');return e?e.value:null;})()")
        new_vals = cdp.value("Array.prototype.map.call(document.querySelectorAll('input[autocomplete=new-password]'),function(e){return e.value;}).join('|')")
        record("改口令表单已填写", bool(cur_set and new_set), "current=%s new=%s persisted=%r/%r" % (cur_set, new_set, cur_val, new_vals))
        time.sleep(0.5)
        clicked_pw = cdp.value(js_click(T("settings.changePassword") or T("common.save")))
        record("点击修改口令", bool(clicked_pw), "clicked=%r" % (clicked_pw,))
        time.sleep(4)
        cdp.screenshot(os.path.join(out_dir, "15-password-changed.png"))

        cdp.evaluate("localStorage.removeItem('pbr.adminKey'); 'ok'")
        cdp.send("Page.navigate", {"url": base + "/login"})
        time.sleep(3)
        cdp.evaluate(js_set_placeholder(T("auth.password"), new_password))
        cdp.evaluate(js_click(T("auth.signIn")))
        relogin = wait_for("location.pathname === '/' && document.body.innerText.length > 200", 20)
        record("新口令重新登录成功", relogin, cdp.value("location.pathname"))
        cdp.screenshot(os.path.join(out_dir, "16-relogin.png"))

        cdp.close()
        passed = sum(1 for s in steps if s["ok"])
        failed = len(steps) - passed
        print(json.dumps({"steps": steps, "passed": passed, "failed": failed}, ensure_ascii=False, indent=2))
        return 0 if failed == 0 else 1
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except Exception:
            proc.kill()


if __name__ == "__main__":
    sys.exit(main())
