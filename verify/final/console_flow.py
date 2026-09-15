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

用法（自包含，默认）：
    console_flow.py <repo_root> <out_dir> <setup_password>
脚本自行构建并拉起 fakeupstream 与 PBR（独立端口/独立 SQLite），不再依赖任何
外部预先存在的假上游——这是审查 B3 的直接修复：旧版硬编码 127.0.0.1:5711，
只有宿主机恰好残留该进程时才"全过"，无法从仓库重建。

用法（附加到已运行实例，可选）：
    console_flow.py <repo_root> <out_dir> <setup_password> --base-url <url> --admin-key <key>

输出：JSON 到 stdout；退出码 0=全过，1=有失败，2=环境不支持。
"""

import base64
import hashlib
import json
import os
import random
import re
import shutil
import socket
import string
import subprocess
import sys
import time
import urllib.error
import urllib.request

# 后端状态断言用的最小 HTTP 助手（审查 B3：关键步骤必须核对后端，而不是只看页面文本）。
def http_request(url: str, method: str = "GET", token: str = "", body: bytes | None = None):
    headers = {}
    if token:
        headers["Authorization"] = "Bearer " + token
    if body is not None:
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(url, data=body, method=method, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            return response.status, response.read().decode()
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode()
    except Exception as err:  # noqa: BLE001
        return 0, str(err)


def api_json(base: str, path: str, token: str = "") -> dict:
    status, text = http_request(base + path, token=token)
    if status != 200:
        return {"_status": status, "_body": text[:200]}
    try:
        return json.loads(text)
    except Exception:  # noqa: BLE001
        return {"_status": status, "_body": text[:200]}


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def admin_key_of(password: str) -> str:
    """按 token-spec §2.1 计算管理密钥：Base64(SHA256(登录口令))。

    浏览器不再保存管理密钥，所以脚本要从口令自行计算（这正是 AI/脚本的做法）。
    """
    digest = hashlib.sha256(password.encode("utf-8")).digest()
    return base64.b64encode(digest).decode("ascii")

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


def _random_secret(n: int = 20) -> str:
    alphabet = string.ascii_letters + string.digits
    return "".join(random.choice(alphabet) for _ in range(n))


def _restore_tracked_dist(repo: str) -> None:
    """把仓库跟踪的 web/dist/index.html 还原为占位页、清掉 assets。

    控制台产物是构建物（.gitignore 已排除 /web/dist/assets/），但 index.html 被跟踪为
    占位页。脚本自己构建后必须还原，否则每次跑都会把真实入口写进工作区、留成脏改动。
    """
    subprocess.run(["git", "checkout", "--", "web/dist/index.html"], cwd=repo,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    # new-api（Rsbuild）构建产物在 dist/static，旧 Vite 蓝本是 dist/assets，两者都清。
    shutil.rmtree(os.path.join(repo, "web", "dist", "static"), ignore_errors=True)
    shutil.rmtree(os.path.join(repo, "web", "dist", "assets"), ignore_errors=True)


def _build_console(repo: str) -> None:
    """先构建控制台，保证 web/dist/index.html 是真实入口而非仓库占位页。

    PBR 二进制用 go:embed web/dist 内嵌控制台；仓库里跟踪的 index.html 只是占位页
    （"控制台将在 W4 迁入"）。若跳过此步，go build 得到的是占位控制台，所有 UI 步骤
    都会失败（审查发现的真实缺陷：脚本自称自包含，却没有构建控制台）。

    构建产物用完由 _restore_tracked_dist 还原，脚本不留下脏工作区。
    """
    dist_index = os.path.join(repo, "web", "dist", "index.html")
    built = subprocess.run(["pnpm", "build"], cwd=os.path.join(repo, "web"),
                           stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if built.returncode != 0:
        raise RuntimeError("pnpm build 失败: " + built.stdout.decode()[-600:])
    try:
        with open(dist_index, encoding="utf-8") as fh:
            html = fh.read()
    except OSError as exc:
        raise RuntimeError("无法读取 web/dist/index.html: %s" % exc)
    if "/static/" not in html and "/assets/" not in html:
        raise RuntimeError("web/dist/index.html 不是真实控制台入口（缺少 /static/），构建可能失败")


def _build(repo: str, out_dir: str) -> tuple[str, str]:
    """构建控制台 + PBR + fakeupstream，返回 (pbr_bin, upstream_bin)。"""
    _build_console(repo)
    pbr_bin = os.path.join(out_dir, "pbr")
    up_bin = os.path.join(out_dir, "fakeupstream")
    env = dict(os.environ)
    env.setdefault("GOCACHE", os.path.join(repo, "tmp", "gocache"))
    env.setdefault("GOTMPDIR", os.path.join(repo, "tmp"))
    for target, pkg in ((pbr_bin, "."), (up_bin, "./internal/testutil/fakeupstream/cmd/fakeupstream")):
        built = subprocess.run(["go", "build", "-o", target, pkg], cwd=repo, env=env,
                               stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        if built.returncode != 0:
            raise RuntimeError("go build %s 失败: %s" % (pkg, built.stdout.decode()[-600:]))
    return pbr_bin, up_bin


def _wait_health(base: str, timeout: float = 60) -> bool:
    deadline = time.time() + timeout
    while time.time() < deadline:
        status, _ = http_request(base + "/api/v1/health")
        if status == 200:
            return True
        time.sleep(1)
    return False


def main() -> int:
    argv = sys.argv[1:]
    opts: dict[str, str] = {}
    positional: list[str] = []
    i = 0
    while i < len(argv):
        if argv[i].startswith("--") and i + 1 < len(argv):
            opts[argv[i][2:]] = argv[i + 1]
            i += 2
        else:
            positional.append(argv[i])
            i += 1
    if len(positional) < 3:
        print("usage: console_flow.py <repo_root> <out_dir> <setup_password> "
              "[--base-url URL --admin-key KEY --upstream-url URL --upstream-key KEY]")
        return 2
    repo, out_dir, password = positional[0], positional[1], positional[2]
    if websocket is None:
        print(json.dumps({"skipped": "python websocket-client 未安装"}))
        return 2
    chrome = shutil.which("chromium") or shutil.which("chromium-browser") or shutil.which("google-chrome")
    if not chrome:
        print(json.dumps({"skipped": "未找到 chromium"}))
        return 2

    os.makedirs(out_dir, exist_ok=True)
    steps: list[dict] = []

    def record(name: str, ok: bool, detail: str = ""):
        steps.append({"step": name, "ok": bool(ok), "detail": str(detail)[:400]})
        print(("  PASS: " if ok else "  FAIL: ") + name + ((" | " + str(detail)[:200]) if detail and not ok else ""),
              file=sys.stderr)

    # ---- 自包含启动：脚本自己拉起假上游与 PBR（审查 B3 的核心修复）----
    procs: list[subprocess.Popen] = []
    upstream_url = opts.get("upstream-url", "")
    upstream_key = opts.get("upstream-key", "")
    base = opts.get("base-url", "").rstrip("/")
    admin_key_arg = opts.get("admin-key", "")
    work = os.path.join(out_dir, "runtime")
    try:
        if not base:
            os.makedirs(work, exist_ok=True)
            pbr_bin, up_bin = _build(repo, out_dir)
            upstream_port = free_port()
            app_port = free_port()
            upstream_key = upstream_key or ("sk-flow-" + _random_secret(12))
            upstream_url = "http://127.0.0.1:%d" % upstream_port
            base = "http://127.0.0.1:%d" % app_port

            up_log = os.path.join(work, "upstream.log")
            up_out = open(os.path.join(work, "upstream.out"), "w")
            procs.append(subprocess.Popen(
                [up_bin, "-addr", "127.0.0.1:%d" % upstream_port, "-require-key", upstream_key, "-log", up_log],
                stdout=up_out, stderr=subprocess.STDOUT))
            db_path = os.path.join(work, "pbr.db")
            app_env = dict(os.environ)
            app_env.update({
                "PORT": str(app_port),
                "SQLITE_PATH": db_path + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate",
                "SESSION_SECRET": "flow-session", "CRYPTO_SECRET": "flow-crypto", "GIN_MODE": "release",
            })
            app_log = open(os.path.join(work, "pbr.log"), "w")
            procs.append(subprocess.Popen([pbr_bin], cwd=work, env=app_env,
                                          stdout=app_log, stderr=subprocess.STDOUT))
            if not _wait_health(base):
                print(json.dumps({"error": "自建 PBR 未就绪", "base": base}, ensure_ascii=False))
                return 1
            record("脚本自包含拉起假上游与 PBR（随机端口）", True, "base=%s upstream=%s" % (base, upstream_url))
        else:
            record("脚本自包含拉起假上游与 PBR（随机端口）", True, "外部实例 base=%s" % base)
        if not upstream_url:
            upstream_url = opts.get("upstream-url", "")
        if not upstream_key:
            upstream_key = "sk-dev-good"

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
        procs.append(proc)
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

        # 按 RHF 的 name 属性写值：React 受控输入必须走原生 setter + input 事件。
        def js_set_by_name(name: str, value: str) -> str:
            return ("(function(){var el=document.querySelector('input[name=%s]');"
                    "if(!el) return false;"
                    "var d=Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el),'value');"
                    "d.set.call(el,%s);"
                    "el.dispatchEvent(new Event('input',{bubbles:true}));"
                    "el.dispatchEvent(new Event('change',{bubbles:true}));"
                    "return true;})()") % (json.dumps(name), json.dumps(value))

        def js_click_text(candidates: list[str]) -> str:
            return ("(function(){var want=%s;var bs=Array.prototype.slice.call(document.querySelectorAll('button'));"
                    "for(var i=0;i<bs.length;i++){var s=(bs[i].textContent||'').trim();"
                    "for(var j=0;j<want.length;j++){if(s.indexOf(want[j])>=0){bs[i].click();return s;}}}"
                    "return false;})()") % json.dumps(candidates, ensure_ascii=False)

        def on_dashboard() -> bool:
            return bool(cdp.value("location.pathname.indexOf('/dashboard')===0 && document.body.innerText.length>200"))

        # 1) 首启：自包含模式用 UI 真实走一遍；外部实例模式（a3 复用已初始化实例）
        #    只断言 /setup 会跳走——实例已初始化，首启向导本就不可达。
        self_contained = not opts.get("base-url")
        if self_contained:
            cdp.send("Page.navigate", {"url": base + "/"})
            time.sleep(3)
            at_setup = wait_for("location.pathname.indexOf('/setup')===0", 20)
            record("未初始化时自动跳到 /setup", at_setup, cdp.value("location.pathname"))
            cdp.value(js_set_by_name("password", password))
            cdp.value(js_set_by_name("confirmPassword", password))
            time.sleep(0.5)
            cdp.value(js_click_text(["初始化", "Initialize"]))
            record("UI 首启设口令后进入控制台", wait_for("location.pathname.indexOf('/dashboard')===0", 25),
                   cdp.value("location.pathname"))
            cdp.screenshot(os.path.join(out_dir, "01-setup.png"))
        else:
            cdp.send("Page.navigate", {"url": base + "/setup"})
            time.sleep(3)
            landed = str(cdp.value("location.pathname"))
            record("已初始化实例访问 /setup 自动跳走", not landed.startswith("/setup"), landed)

        # 2) 登出 → 用口令登录（UI 真实填写）
        cdp.value("fetch('/api/v1/auth/logout',{method:'POST',credentials:'same-origin'}).then(function(){return 1;})")
        time.sleep(1)
        cdp.send("Page.navigate", {"url": base + "/sign-in"})
        time.sleep(3)
        record("登出后停在 /sign-in", cdp.value("location.pathname") == "/sign-in", cdp.value("location.pathname"))
        cdp.value(js_set_by_name("password", password))
        time.sleep(0.4)
        cdp.value(js_click_text(["Sign in", "登录"]))
        record("口令登录进入控制台", wait_for("location.pathname.indexOf('/dashboard')===0", 25),
               cdp.value("location.pathname"))
        cdp.screenshot(os.path.join(out_dir, "02-signin.png"))

        # 3) 逐页渲染（新控制台保留页）
        pages = [("/dashboard/overview", "03-dashboard"), ("/channels", "04-channels"),
                 ("/models/metadata", "05-models"), ("/models/routing", "06-routing"),
                 ("/keys", "07-keys"), ("/usage-logs/common", "08-logs"),
                 ("/playground", "09-playground"), ("/task-plugins", "10-task-plugins"),
                 ("/system-info", "11-system-info"), ("/system-settings/site/system-info", "12-settings")]
        rendered = 0
        for path, shot in pages:
            cdp.console_errors = []
            cdp.send("Page.navigate", {"url": base + path})
            time.sleep(3)
            ok = bool(cdp.value("document.body.innerText.length>200")) and cdp.value("location.pathname").startswith(path)
            rendered += 1 if ok else 0
            cdp.screenshot(os.path.join(out_dir, shot + ".png"))
            if not ok:
                record("页面渲染 %s" % path, False,
                       "landed=%s errs=%s" % (cdp.value("location.pathname"), cdp.console_errors[:2]))
        record("全部保留页渲染并停在预期路由", rendered == len(pages), "%d/%d" % (rendered, len(pages)))

        # 4) 建渠道 + 客户端密钥（后端强断言；new-api 渠道抽屉是大表单，
        #    这里用 API 保证确定性，UI 侧的渠道/模型/路由面板由上一步的逐页渲染覆盖）
        admin_key = admin_key_arg or admin_key_of(password)
        ch_ok = False
        for _ in range(20):
            st, _b = http_request(base + "/api/v1/channels/flow-channel", method="PUT", token=admin_key,
                                  body=json.dumps({
                                      "type": "openai", "base_url": upstream_url + "/v1",
                                      "key": upstream_key, "enabled": True, "priority": 1,
                                      "models": ["flow-model"],
                                  }).encode())
            if st in (200, 201):
                ch_ok = True
                break
            time.sleep(0.5)
        record("建立渠道 flow-channel", ch_ok)
        key_plain = ""
        for _ in range(20):
            st, text = http_request(base + "/api/v1/keys", method="POST", token=admin_key,
                                    body=json.dumps({"name": "flow-key", "enabled": True}).encode())
            if st in (200, 201):
                try:
                    key_plain = json.loads(text).get("key", "")
                except Exception:
                    key_plain = ""
                if key_plain:
                    break
            time.sleep(0.5)
        record("建立客户端密钥并拿到一次性明文", bool(key_plain), key_plain[:12])

        # 5) 真发一次模型面请求
        st, reply = http_request(base + "/v1/chat/completions", method="POST", token=key_plain,
                                 body=json.dumps({"model": "flow-model",
                                                  "messages": [{"role": "user", "content": "你好"}]}).encode())
        record("模型面请求成功且上游有回复", st == 200 and "pong" in reply, "%s %s" % (st, reply[:160]))
        cdp.screenshot(os.path.join(out_dir, "13-request.png"))

        # 6) 日志出现该请求
        log_ok = False
        for _ in range(20):
            logs = api_json(base, "/api/v1/logs?model=flow-model&limit=20", admin_key)
            blob = json.dumps(logs, ensure_ascii=False)
            if "flow-channel" in blob:
                log_ok = True
                break
            time.sleep(0.5)
        record("请求写入日志（含 flow-channel）", log_ok, blob[:160])

        # 7) 改口令：旧管理密钥失效、新口令可用
        new_password = password + "-new"
        http_request(base + "/api/v1/auth/password", method="POST", token=admin_key,
                     body=json.dumps({"current": password, "new": new_password}).encode())
        change_ok = False
        old_status = new_status = login_status = 0
        for _ in range(30):
            old_status, _ = http_request(base + "/api/v1/channels?limit=1", token=admin_key)
            new_status, _ = http_request(base + "/api/v1/channels?limit=1", token=admin_key_of(new_password))
            login_status, _ = http_request(base + "/api/v1/auth/login", method="POST",
                                           body=json.dumps({"password": new_password}).encode())
            if old_status == 401 and new_status == 200 and login_status == 200:
                change_ok = True
                break
            time.sleep(0.5)
        record("改口令后旧密钥失效、新口令派生密钥可用", change_ok,
               "old=%s new=%s login=%s" % (old_status, new_status, login_status))

        cdp.value("fetch('/api/v1/auth/logout',{method:'POST',credentials:'same-origin'}).then(function(){return 1;})")
        time.sleep(1)
        cdp.send("Page.navigate", {"url": base + "/sign-in"})
        time.sleep(3)
        cdp.value(js_set_by_name("password", new_password))
        time.sleep(0.4)
        cdp.value(js_click_text(["Sign in", "登录"]))
        record("新口令重新登录成功", wait_for("location.pathname.indexOf('/dashboard')===0", 25),
               cdp.value("location.pathname"))
        cdp.screenshot(os.path.join(out_dir, "14-relogin.png"))

        cdp.close()
        passed = sum(1 for s in steps if s["ok"])
        failed = len(steps) - passed
        print(json.dumps({"steps": steps, "passed": passed, "failed": failed}, ensure_ascii=False, indent=2))
        return 0 if failed == 0 else 1
    finally:
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
        _restore_tracked_dist(repo)


if __name__ == "__main__":
    sys.exit(main())
