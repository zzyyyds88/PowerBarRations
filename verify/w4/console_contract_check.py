#!/usr/bin/env python3
"""控制台 ↔ 契约一致性检查。

ui-spec §1 规定：`api/` 只封装对 api-spec 端点的调用；缺端点先补契约，
**禁止模块内直连或自造接口**。据此本检查做三件事：

  1. 从各 feature 的 api 层（`features/<module>/api.ts`、`*-api.ts`）抽出管理 API
     路径字面量，逐条与 `/api/v1/openapi.json` 登记的路径比对——"控制台调了但
     稳定契约没写、且不属于 §9 控制台内部面"会落在 missing 里；
  2. 扫描 `web/src/` 其余文件，出现 `api.get/post/put/del` 调用或 `/api/v1`
     字面量即为**绕过 api 层**，落在 bypass 里（§1 明确禁止）；
  3. 报告契约里登记、但控制台尚未使用的端点（unmatched_documented，仅作提示）。

为什么只看 api 层：页面里还有前端路由（`/login`、`/settings` 等），它们不是 API
端点。api-spec §9 另有一批"控制台内部接口"（/api/channel/**、/api/console/**、
/api/system-task/** 等），它们用同一管理密钥鉴权但不进 stable OpenAPI，因此属于
这些前缀的路径不算 missing（不是"契约没写"，而是刻意登记在 §9 之下）。

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


# api-spec §9：控制台内部接口前缀（用同一管理密钥，但不属于稳定契约，故不进
# openapi.json）。命中这些前缀的调用不算 "missing"。
CONSOLE_INTERNAL_PREFIXES = (
    "/api/channel",
    "/api/console",
    "/api/system-task",
    "/api/user",
    "/api/log",
    "/api/option",
    "/api/prefill_group",
    "/api/performance",
    "/api/perf-metrics",
    "/api/status",
    "/api/about",
    "/api/home_page_content",
    "/api/user-agreement",
    "/api/privacy-policy",
)


def is_console_internal(path: str) -> bool:
    return any(
        path == prefix or path.startswith(prefix + "/")
        for prefix in CONSOLE_INTERNAL_PREFIXES
    )


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
    # 先把模板占位 ${...} 换成 {param}，再截掉查询串。顺序很重要：
    # 若先按 "?" 截断，/lanes/seed${dryRun ? '?dry_run=true' : ''} 会被从三元里的
    # "?" 处切断，残留 "${dryRun "，进而被误匹配到 /lanes/{name}。
    raw = substitute_template_placeholders(raw)
    raw = raw.split("?")[0]
    return raw


def substitute_template_placeholders(raw: str) -> str:
    """把每个 ${...}（含嵌套花括号）替换为 {param}。"""
    out: list[str] = []
    i, n = 0, len(raw)
    while i < n:
        if raw.startswith("${", i):
            depth, j = 1, i + 2
            while j < n and depth > 0:
                if raw[j] == "{":
                    depth += 1
                elif raw[j] == "}":
                    depth -= 1
                j += 1
            out.append("{param}")
            i = j
            continue
        out.append(raw[i])
        i += 1
    return "".join(out)


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

    # ui-spec §1：允许封装管理端点的是 **各 feature 自己的 api 层**，
    # 即 features/<module>/api.ts（以及少量 *-api.ts 辅助文件）。
    # 早期检查器错写成 web/src/api/（该目录并不存在），导致所有真实调用
    # 都被判成"绕过 api 层"，bypass 永远非空。
    def is_api_layer(path: pathlib.Path) -> bool:
        if "node_modules" in path.parts:
            return False
        return path.name == "api.ts" or path.name.endswith("-api.ts")

    def is_test_file(path: pathlib.Path) -> bool:
        # 测试与夹具不是产品代码，不参与"绕过 api 层"扫描。
        return "__tests__" in path.parts or ".test." in path.name

    # 认证基础设施：PBR 会话适配层（ui-spec §3）直接与 auth 端点交互是设计如此，
    # 不属于"页面绕过 feature api 层"。
    AUTH_INFRA_FILES = {"lib/auth-session.ts", "lib/http-client.ts"}

    for source in sorted(src.rglob("*.ts*")):
        text = strip_comments(source.read_text())
        in_api_layer = is_api_layer(source)
        label = str(source.relative_to(src))

        if not in_api_layer:
            if is_test_file(source) or label in AUTH_INFRA_FILES:
                continue
            # 页面/组件里的直连：两种形态都算绕过 api 层（ui-spec §1）。
            for literal in extract_call_paths(text):
                bypass.append(f"{label}: api -> {literal}")
            for literal in ANY_LITERAL.findall(text):
                if literal.startswith("/api/v1"):
                    bypass.append(f"{label}: {literal}")
            continue

        # api 层里"真正发给管理 API 的路径"有两种写法：
        #   1) api.get/post/put/del(<path>) —— 常规封装；
        #   2) 直接以管理前缀开头的字面量（如 SSE / route-events 的 fetch）。
        # 其余路径字面量（如 401 后跳的 "/login" 前端路由）不是接口，必须排除。
        candidates = set(extract_call_paths(text))
        candidates |= {
            lit for lit in ANY_LITERAL.findall(text)
            if lit.startswith("/api/v1/") or lit.startswith("/api/")
        }

        for literal in candidates:
            # api-spec §1：/api 是规范前缀，/api/v1 是兼容别名；openapi.json 以
            # /api 为 servers、paths 不含前缀，因此两者都要剥掉再比对。
            if literal.startswith("/api/v1/"):
                literal = literal[len("/api/v1"):]
            elif literal.startswith("/api/"):
                literal = literal[len("/api"):]
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
            elif is_console_internal("/api" + candidate):
                # §9 控制台内部面：刻意不在 stable OpenAPI，不算契约缺失。
                continue
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
