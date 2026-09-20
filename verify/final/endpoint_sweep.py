#!/usr/bin/env python3
"""管理面端点全量实跑（W8 的 A1）。

对 openapi.json 里登记的每个 path×method 发一次真实请求，判定：
- 不得 5xx（未处理异常 / 未实现桩）；
- 不得 501；
- 404 只允许出现在"按设计就不存在的对象"（脚本显式标注）。

用法：endpoint_sweep.py <openapi.json> <base_url> <admin_key>
输出：{"results": [{"method","path","status","note","expected_missing"}]}
"""
import json
import subprocess
import sys

# 需要请求体的写接口：给一份最小合法 body，让校验层能走到
BODIES = {
    ("put", "/channels/{name}"): {"type": "openai", "base_url": "http://127.0.0.1:1", "models": ["sweep-model"]},
    ("put", "/lanes/{name}"): {"enabled": False, "mode": "failover", "members": []},
    ("put", "/lanes/{name}/members"): {"members": []},
    ("put", "/keys/{name}"): {"enabled": True},
    ("post", "/keys"): {"name": "sweep-key"},
    ("post", "/setup"): {},
    ("post", "/auth/login"): {"password": "x"},
    ("post", "/auth/password"): {"current": "x", "new": "y"},
    ("put", "/system/options"): {},
    ("post", "/import"): {"version": "v1"},
}

# 按设计就会 404 的探测路径（对象不存在），不计为失败
EXPECTED_MISSING = {
    ("get", "/channels/{name}"),
    ("get", "/lanes/{name}"),
    ("get", "/lanes/{name}/health"),
    ("get", "/keys/{name}"),
    ("delete", "/channels/{name}"),
    ("delete", "/lanes/{name}"),
    ("delete", "/keys/{name}"),
    ("get", "/logs/{id}"),
    ("get", "/system-tasks/{id}"),
}

SUBSTITUTIONS = {"{name}": "sweep-probe", "{id}": "999999", "{model}": "sweep-model"}

# DELETE 之后对象即不存在，顺序不定，因此 2xx 与 404 都算符合预期
DELETE_OK = True


def fill(path: str) -> str:
    result = path
    for token, value in SUBSTITUTIONS.items():
        result = result.replace(token, value)
    return result


def main() -> int:
    openapi_path, base, key = sys.argv[1], sys.argv[2], sys.argv[3]
    # 第 4 个参数是真实上游地址：探针渠道指向它，sync-models 才能走到成功分支
    upstream = sys.argv[4] if len(sys.argv) > 4 else base
    # 假上游会校验 key，探针渠道必须带对，否则 sync-models 会被 401 拦下
    probe_key = sys.argv[5] if len(sys.argv) > 5 else "sk-sweep"
    paths = json.load(open(openapi_path))["paths"]

    # 渠道探针的 base_url 必须指向真实上游，否则 sync-models 会因为"上游不可达"而 502
    BODIES[("put", "/channels/{name}")] = {
        "type": "openai", "base_url": upstream, "key": probe_key,
        "models": ["sweep-model"], "enabled": True,
    }

    # 先造出探针对象，让「更新已存在对象」类端点走到业务分支而不是 404
    headers = ["-H", f"Authorization: Bearer {key}", "-H", "Content-Type: application/json"]
    subprocess.run(["curl", "-s", "-o", "/dev/null", "-X", "PUT", *headers, "-d",
                    json.dumps({"type": "openai", "base_url": upstream, "key": probe_key,
                                "models": ["sweep-model"], "enabled": True}),
                    base + "/api/v1/channels/sweep-probe"])
    subprocess.run(["curl", "-s", "-o", "/dev/null", "-X", "PUT", *headers, "-d",
                    json.dumps({"enabled": False, "mode": "failover", "members": []}),
                    base + "/api/v1/lanes/sweep-probe"])
    subprocess.run(["curl", "-s", "-o", "/dev/null", "-X", "POST", *headers, "-d",
                    json.dumps({"name": "sweep-probe"}), base + "/api/v1/keys"])

    def status_of(method, url, body=None):
        command = ["curl", "-s", "--max-time", "8", "-o", "/dev/null", "-w", "%{http_code}",
                   "-X", method.upper(), "-H", f"Authorization: Bearer {key}"]
        if body is not None:
            command += ["-H", "Content-Type: application/json", "-d", json.dumps(body)]
        command.append(url)
        raw = subprocess.run(command, capture_output=True, text=True).stdout.strip()
        return int(raw) if raw.isdigit() else 0

    def json_of(method, url, body=None):
        command = ["curl", "-s", "--max-time", "8", "-X", method.upper(),
                   "-H", f"Authorization: Bearer {key}", "-H", "Content-Type: application/json"]
        if body is not None:
            command += ["-d", json.dumps(body)]
        command.append(url)
        raw = subprocess.run(command, capture_output=True, text=True).stdout
        try:
            return json.loads(raw)
        except ValueError:
            return {}

    results = []
    deferred_deletes = []
    for path, methods in paths.items():
        for method, _operation in methods.items():
            method = method.lower()
            if method == "delete":
                # DELETE 会破坏后续探针的前置状态：先记录，稍后为每个端点单独造对象再删。
                deferred_deletes.append(path)
                continue
            if method not in ("get", "post", "put"):
                continue
            url = base + "/api/v1" + fill(path)
            if method == "get" and path == "/logs?success=false":
                continue
            command = [
                "curl", "-s", "--max-time", "8", "-o", "/dev/null", "-w", "%{http_code}",
                "-X", method.upper(),
                "-H", f"Authorization: Bearer {key}",
            ]
            body = BODIES.get((method, path))
            if body is not None:
                command += ["-H", "Content-Type: application/json", "-d", json.dumps(body)]
            command.append(url)
            raw = subprocess.run(command, capture_output=True, text=True).stdout.strip()
            status = int(raw) if raw.isdigit() else 0
            if path == "/route-events":
                # SSE 长连接按设计不返回，curl 超时为 000 —— 视为符合预期
                results.append({"method": method, "path": path,
                                "status": 200 if status == 0 else status,
                                "expected_missing": False, "note": "SSE 长连接（超时即符合预期）"})
                continue
            expected = status == 404 and ((method, path) in EXPECTED_MISSING or method == "delete")
            results.append({
                "method": method,
                "path": path,
                "status": status,
                "expected_missing": expected,
                "note": "删除/不存在对象" if expected and status == 404 else "",
            })

    # —— DELETE 全量实跑 ——
    # 每个 DELETE 端点都先造出目标对象再删，确保走的是"真的删掉"分支而不是"对象不存在"。
    # 探针对象一律用 sweep- 前缀的一次性名字，不碰真实配置。
    status_of("PUT", base + "/api/v1/channels/sweep-delete",
              {"type": "openai", "base_url": upstream, "key": probe_key, "models": ["sweep-model"]})
    status_of("PUT", base + "/api/v1/lanes/sweep-delete",
              {"enabled": False, "mode": "failover", "members": []})
    status_of("POST", base + "/api/v1/keys", {"name": "sweep-delete"})
    # Ollama 删除：需要一条 Ollama 渠道，base_url 指向假上游（/api/delete 由它应答 200）。
    status_of("PUT", base + "/api/v1/channels/sweep-ollama",
              {"type": "ollama", "base_url": upstream, "key": probe_key,
               "models": ["sweep-ollama-model"], "enabled": True})
    # /channels/disabled：造一条已停用渠道，确保删的是真对象。
    status_of("PUT", base + "/api/v1/channels/sweep-disabled",
              {"type": "openai", "base_url": upstream, "key": probe_key,
               "models": ["sweep-disabled-model"], "enabled": False})
    status_of("PUT", base + "/api/v1/model-metadata/sweep-meta",
              {"description": "sweep", "icon": "", "tags": [], "endpoints": [], "status": 1, "name_rule": 0})
    prefill = json_of("POST", base + "/api/v1/prefill-groups",
                      {"name": "sweep-prefill", "type": "model", "items": []})
    prefill_id = prefill.get("id")

    delete_probes = [
        ("/channels/{name}", base + "/api/v1/channels/sweep-delete", None),
        ("/lanes/{name}", base + "/api/v1/lanes/sweep-delete", None),
        ("/keys/{name}", base + "/api/v1/keys/sweep-delete", None),
        ("/channels/{name}/ollama/models",
         base + "/api/v1/channels/sweep-ollama/ollama/models", {"model_name": "sweep-ollama-model"}),
        ("/channels/disabled", base + "/api/v1/channels/disabled", None),
        ("/model-metadata/{model}", base + "/api/v1/model-metadata/sweep-meta", None),
        ("/system/log-files", base + "/api/v1/system/log-files?mode=by_count&value=1", None),
        ("/system/performance/disk-cache", base + "/api/v1/system/performance/disk-cache", None),
    ]
    if isinstance(prefill_id, int):
        delete_probes.append(("/prefill-groups/{id}", base + f"/api/v1/prefill-groups/{prefill_id}", None))

    for path, url, body in delete_probes:
        results.append({"method": "delete", "path": path, "status": status_of("DELETE", url, body),
                        "expected_missing": False, "note": "一次性对象删除"})

    # 覆盖守卫：openapi 里登记了、但没有对应探针的 DELETE 端点必须显式失败，
    # 否则新增 DELETE 端点会像以前一样静默漏跑（status=0 → 不会触发"没有 5xx"）。
    probed = {path for path, _url, _body in delete_probes}
    for path in deferred_deletes:
        if path not in probed:
            results.append({"method": "delete", "path": path, "status": 0,
                            "expected_missing": False, "note": "DELETE 未纳入实跑（覆盖缺口）"})

    print(json.dumps({"results": results, "delete_paths": sorted(deferred_deletes)}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
