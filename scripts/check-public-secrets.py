#!/usr/bin/env python3
"""公开推送前的密钥形状自查（本地脚本，不依赖 CI）。

背景：本仓库已按 AGPL-3.0 公开发布，GitHub 的 secret scanning 只对公开仓库
开启，且**按形状匹配**——脱敏引擎的测试样本若写成完整字面量，会把公开仓库
的 Security 页挂满告警，被访问者误读成真实泄露（2026-11-15 实测：一条
`mongodb+srv://…@cluster0.ab1cd.mongodb.net` 的测试夹具触发了
"MongoDB Atlas Database URI with credentials"）。

同一批 `sk-proj-…` 假值实测**不会**触发（OpenAI 检测器有额外结构要求），
所以本脚本只覆盖"纯形状即可命中"的那几类。发现命中时优先把字面量拆成
`"前缀" + "其余"` 的运行时拼接，测试覆盖的字符串保持不变。

用法：
    python3 scripts/check-public-secrets.py            # 扫当前工作区
    python3 scripts/check-public-secrets.py --staged   # 只扫已 stage 的内容

退出码：0 = 干净，1 = 有命中（阻断）。
"""
from __future__ import annotations

import re
import subprocess
import sys

# 只放实测"按形状即可命中"的规则（2026-11-15 用临时公开仓库逐条验证过）。
# 已验证**不**触发、因此刻意排除的：
#   - AWS 文档示例键 AKIAIOSFODNN7EXAMPLE（GitHub 白名单）
#   - sk-proj-… / ghp_… 假值（OpenAI、GitHub 检测器有额外结构/校验位要求）
#   - postgres://user:pass@db.internal、https://deploy:pw@git.acme-corp.cn
#     （非云厂商主机，检测器不报）
PATTERNS: dict[str, str] = {
    "MongoDB Atlas URI": r"mongodb(\+srv)?://[^/\s:@]+:[^/\s:@]+@[a-z0-9.-]*mongodb\.net",
    "完整 PEM 私钥": (
        r"-----BEGIN ((RSA|EC|OPENSSH|PGP|DSA) )?PRIVATE KEY-----"
        r"[\r\n]+[A-Za-z0-9+/=\r\n]{100,}"
    ),
    "Slack Token": r"\bxox[abprs]-[A-Za-z0-9-]{10,}\b",
    "Google API Key": r"\bAIza[0-9A-Za-z_-]{35}\b",
    "Stripe Live Key": r"\b[rs]k_live_[A-Za-z0-9]{20,}\b",
    "SendGrid Key": r"\bSG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}\b",
    "Telegram Bot Token": r"\b[0-9]{8,10}:AA[A-Za-z0-9_-]{33}\b",
}

SKIP_PREFIXES = ("web/dist/", "web/src/i18n/", "reference/")
SKIP_FILES = {"go.sum", "web/pnpm-lock.yaml"}


def tracked_files(staged: bool) -> list[str]:
    cmd = (
        ["git", "diff", "--cached", "--name-only", "--diff-filter=ACMR"]
        if staged
        else ["git", "ls-files"]
    )
    out = subprocess.run(cmd, capture_output=True, text=True).stdout.split("\n")
    return [
        f
        for f in out
        if f
        and f not in SKIP_FILES
        and not f.startswith(SKIP_PREFIXES)
        and is_text_path(f)
    ]


BINARY_SUFFIXES = (
    ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf", ".zip",
    ".gz", ".tgz", ".woff", ".woff2", ".ttf", ".otf", ".eot", ".wasm",
    ".onnx", ".mp4", ".mov", ".webm", ".mp3",
)


def is_text_path(path: str) -> bool:
    return not path.lower().endswith(BINARY_SUFFIXES)


def read(path: str, staged: bool) -> str:
    """取文件内容；二进制与无法解码的内容一律跳过（本脚本只关心文本形状）。"""
    if staged:
        r = subprocess.run(
            ["git", "show", f":{path}"], capture_output=True
        )
        if r.returncode != 0:
            return ""
        # 不做 text=True 解码：二进制文件会让整个 hook 崩掉。
        if b"\x00" in r.stdout[:8192]:
            return ""
        return r.stdout.decode("utf-8", errors="ignore")
    try:
        with open(path, encoding="utf-8", errors="ignore") as fh:
            return fh.read()
    except OSError:
        return ""


def main() -> int:
    staged = "--staged" in sys.argv
    hits: list[tuple[str, str, int, str]] = []
    for path in tracked_files(staged):
        text = read(path, staged)
        if not text:
            continue
        for name, pattern in PATTERNS.items():
            for m in re.finditer(pattern, text):
                line = text[: m.start()].count("\n") + 1
                hits.append((name, path, line, m.group(0)[:72]))

    if not hits:
        print(f"check-public-secrets: clean ({len(tracked_files(staged))} files)")
        return 0

    print("check-public-secrets: 发现可能被 GitHub secret scanning 标记的形状\n")
    for name, path, line, value in hits:
        print(f"  [{name}] {path}:{line}")
        print(f"      {value!r}")
    print(
        "\n处理方式：把字面量拆成 \"前缀\" + \"其余\" 的运行时拼接（测试覆盖不变），"
        "\n或换成 example.internal / acme-corp.cn 之类的占位主机。"
        "\n只有确认是真实凭据时才轮换它。"
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
