#!/usr/bin/env python3
"""控制台 ↔ 契约一致性检查。

ui-spec §1 规定：`api/` 只封装对 api-spec 端点的调用；缺端点先补契约，
**禁止模块内直连或自造接口**。据此本检查做三件事：

  1. 从 `web/src/api/`（唯一允许封装管理端点的层）抽出所有管理 API 路径字面量，
     逐条与 `/api/v1/openapi.json` 登记的路径比对——"控制台调了但契约没写"
     会落在 missing 里；
  2. 扫描 `web/src/` 其余文件，出现 `api.get/post/put/del` 调用或 `/api/v1`
     字面量即为**绕过 api 层**，落在 bypass 里（§1 明确禁止）；
  3. 报告契约里登记、但控制台尚未使用的端点（unmatched_documented，仅作提示）。

为什么只看 api/：页面里还有 React Router 的前端路由（`/login`、`/settings` 等），
它们不是 API 端点。此前检查器扫描整个 web/src，迁移到 react-router 后会把前端
路由误报成"契约缺失"。

解析为什么不能只靠一条正则：真实写法有三种形态，都必须抽到路径字面量——
`api.get<T>("/x")`、`api.get<ListResponse<Channel>>("/x")`（嵌套泛型）、
`api.post<T>(withQuery(`/x`, qs))`（查询串拼在工具函数里）。因此先定位被调方，
跳过括号前的平衡泛型，再取第一个字符串字面量。

用法：console_contract_check.py <仓库根> <openapi.json 路径>
输出：{"checked", "missing", "bypass", "unmatched_documented"}
"""
import json
import pathlib
import re
import sys


COMMENT = re.compile(r"/\*.*?\*/|//[^\n]*", re.S)
CALLEE = re.compile(r"\bapi\.(?:get|post|put|del)\b")
LITERAL = re.compile(r'[`"]([^`"]+)[`"]')
ANY_LITERAL = re.compile(r'[`"](/[A-Za-z0-9/_${}()\-.]*)[`"]')
WITH_QUERY = re.compile(r"\s*(?:withQuery\(\s*)?")


def strip_comments(text: str) -> str:
    """去注释：注释里出现的 "/api/v1" 之类说明文字不是接口调用。"""
    return COMMENT.sub("", text)


def extract_call_paths(text: str) -> list[str]:
    """抽出 `api.<method>[<泛型>]([withQuery(]`<字面量>` 里的路径字面量。"""
    out: list[str] = []
    for match in CALLEE.finditer(text):
        i, n = match.end(), len(text)
        while i < n and text[i].isspace():
            i += 1
        if i < n and text[i] == "<":
            # 跳过可能嵌套的泛型参数表
            depth = 0
            while i < n:
                if text[i] == "<":
                    depth += 1
                elif text[i] == ">":
                    depth -= 1
                    if depth == 0:
                        i += 1
                        break
                i += 1
            while i < n and text[i].isspace():
                i += 1
        if i >= n or text[i] != "(":
            continue
        i = WITH_QUERY.match(text, i + 1).end()
        literal = LITERAL.match(text, i)
        if literal:
            out.append(literal.group(1))
    return out


def normalize(raw: str) -> str:
    raw = raw.split("?")[0]
    raw = re.sub(r"\$\{[^}]*\}", "{param}", raw)
    return raw


def segments(path: str) -> list[str]:
    return [seg for seg in path.strip("/").split("/") if seg]


def matches(candidate: str, documented: str) -> bool:
    left, right = segments(candidate), segments(documented)
    if len(left) != len(right):
        return False
    return all(r.startswith("{") or l == r for l, r in zip(left, right))


def main() -> int:
    repo, openapi_path = sys.argv[1], sys.argv[2]
    documented = list(json.load(open(openapi_path))["paths"].keys())

    checked: set[str] = set()
    missing: list[str] = []
    bypass: list[str] = []
    used_documented: set[str] = set()

    src = pathlib.Path(repo, "web/src")
    api_dir = src / "api"

    for source in sorted(src.rglob("*.ts*")):
        text = strip_comments(source.read_text())
        in_api_layer = api_dir in source.parents
        label = str(source.relative_to(src))

        if not in_api_layer:
            # 页面/组件里的直连：两种形态都算绕过 api 层（ui-spec §1）。
            for literal in extract_call_paths(text):
                bypass.append(f"{label}: api -> {literal}")
            for literal in ANY_LITERAL.findall(text):
                if literal.startswith("/api/v1"):
                    bypass.append(f"{label}: {literal}")
            continue

        # api/ 层里"真正发给管理 API 的路径"有两种写法：
        #   1) api.get/post/put/del(<path>) —— 常规封装；
        #   2) 直接以 /api/v1 开头的字面量（如 SSE / route-events 的 fetch）。
        # 其余路径字面量（如 401 后跳的 "/login" 前端路由）不是接口，必须排除。
        candidates = set(extract_call_paths(text))
        candidates |= {lit for lit in ANY_LITERAL.findall(text) if lit.startswith("/api/v1")}

        for literal in candidates:
            if literal.startswith("/api/v1"):
                literal = literal[len("/api/v1"):] or "/"
            candidate = normalize(literal)
            if candidate in ("", "/"):
                continue
            # 纯动态前缀（如 client.ts 里的 `/api/v1${path}`）不含路径知识，跳过
            if all(seg.startswith("{") for seg in segments(candidate)):
                continue
            checked.add(candidate)
            matched = [doc for doc in documented if matches(candidate, doc)]
            if matched:
                used_documented.update(matched)
            else:
                missing.append(f"{label}: {literal}")

    unmatched = sorted(set(documented) - used_documented)
    print(
        json.dumps(
            {
                "checked": sorted(checked),
                "missing": missing,
                "bypass": bypass,
                "unmatched_documented": unmatched,
            },
            ensure_ascii=False,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
