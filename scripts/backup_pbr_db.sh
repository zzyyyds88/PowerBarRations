#!/usr/bin/env bash
# PowerBarRations SQLite 在线备份。
#
# 用法：
#   scripts/backup_pbr_db.sh [--db /path/pbr.db] [--out /path/backup.db]
#
# 默认源库取 $SQLITE_PATH（去掉 ?参数 后的文件路径），否则 ./pbr.db；
# 默认输出到 ./backups/pbr-<UTC时间>.db。使用 Python sqlite3 在线 backup API，
# 备份期间服务可以继续读写；备份完成后对副本做 quick_check，失败即删除副本
# 并以非零退出。脚本只读源库，不输出 DSN/凭据。
set -euo pipefail
umask 077

db="${SQLITE_PATH:-}"
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --db) db="${2:-}"; shift 2 ;;
    --out) out="${2:-}"; shift 2 ;;
    -h|--help)
      sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

# DSN → 文件路径；SQLITE_PATH 允许带 ?_pragma=...。
db="${db%%\?*}"
db="${db#file:}"
if [ -z "$db" ]; then
  db="./pbr.db"
fi
if [ ! -f "$db" ]; then
  echo "database not found: $db" >&2
  exit 1
fi

if [ -z "$out" ]; then
  mkdir -p ./backups
  out="./backups/pbr-$(date -u +%Y%m%dT%H%M%SZ).db"
fi
if [ -e "$out" ]; then
  echo "refusing to overwrite existing file: $out" >&2
  exit 1
fi
mkdir -p "$(dirname "$out")"

python3 -I - "$db" "$out" <<'PY'
import os
import sqlite3
import sys

src, dst = sys.argv[1], sys.argv[2]

# 只读连接 + 在线 backup API：对运行中的 WAL 库也能取得一致快照。
source = sqlite3.connect(f"file:{src}?mode=ro", uri=True)
try:
    target = sqlite3.connect(dst)
    try:
        source.backup(target)
        result = target.execute("PRAGMA quick_check").fetchone()
        if not result or result[0] != "ok":
            raise SystemExit("quick_check failed: %r" % (result,))
        target.commit()
    finally:
        target.close()
finally:
    source.close()

# 副本只允许当前用户读写。
os.chmod(dst, 0o600)
PY

echo "backup ok: $out"
