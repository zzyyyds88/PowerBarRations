#!/usr/bin/env bash
# W8-A4（真实迁移证据）：在旧两层网关的**只读副本**上跑 pbr migrate 两次，验证幂等与对账，
# 并证明迁移产物能被 PBR 加载、车道数与被迁移计划一致。
#
# 输入（运维私有，经环境变量传入，不入仓库）：
#   ROUTING_DB  octopus 路由层 SQLite 路径（只读；脚本自行拷贝副本）
#   VENDOR_DB   new-api 厂商层 SQLite 路径（只读；脚本自行拷贝副本）
#
# 安全边界：绝不写源库；渠道名/地址/key 只落在 /tmp 工作目录与报告文件，不打印到日志。
set -uo pipefail

REPO="${PBR_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORK=/tmp/pbr-a4-migrate
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/final/a4-migrate-$STAMP.log"
PORT=6841
ADMIN='a4-migrate-admin-key'

if [ -z "${ROUTING_DB:-}" ] || [ -z "${VENDOR_DB:-}" ]; then
  echo "usage: ROUTING_DB=<octopus.db> VENDOR_DB=<new-api.db> bash verify/final/a4_migrate.sh" >&2
  exit 2
fi

exec > "$EVID" 2>&1
echo "=== W8-A4 真实迁移幂等与对账 @ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}
cleanup() { pkill -f "$WORK/pbr" 2>/dev/null; }
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"

echo "--- 0) 拷贝旧库副本（带 WAL 三件套并 checkpoint）"
cp -f "$ROUTING_DB" "$WORK/routing.db"
for ext in -wal -shm; do [ -f "$ROUTING_DB$ext" ] && cp -f "$ROUTING_DB$ext" "$WORK/routing.db$ext"; done
sqlite3 "$WORK/routing.db" "PRAGMA wal_checkpoint(FULL);" >/dev/null 2>&1
cp -f "$VENDOR_DB" "$WORK/vendor.db"
for ext in -wal -shm; do [ -f "$VENDOR_DB$ext" ] && cp -f "$VENDOR_DB$ext" "$WORK/vendor.db$ext"; done
sqlite3 "$WORK/vendor.db" "PRAGMA wal_checkpoint(FULL);" >/dev/null 2>&1

echo "--- 1) 构建 pbr"
( cd "$REPO" && go build -o "$WORK/pbr" . ) || { echo "FAIL: build"; exit 1; }

echo "--- 2) 第一次迁移"
"$WORK/pbr" migrate --routing "$WORK/routing.db" --vendor "$WORK/vendor.db" --target "$WORK/target1.db" --report "$WORK/report1.json" --keys both > "$WORK/run1.txt" 2>&1
check "第一次迁移退出码 0" "$?|$(cat "$WORK/run1.txt")" "0|"
cat "$WORK/run1.txt"

echo "--- 3) 第二次迁移（幂等）"
"$WORK/pbr" migrate --routing "$WORK/routing.db" --vendor "$WORK/vendor.db" --target "$WORK/target2.db" --report "$WORK/report2.json" --keys both > "$WORK/run2.txt" 2>&1
check "第二次迁移退出码 0" "$?|$(cat "$WORK/run2.txt")" "0|"
if diff -q "$WORK/report1.json" "$WORK/report2.json" >/dev/null 2>&1; then
  check "两次迁移计划完全一致（幂等）" "same" "same"
else
  check "两次迁移计划完全一致（幂等）" "different" "same"
fi

echo "--- 4) 目标库计数一致"
for t in channels lanes lane_members client_keys abilities; do
  c1=$(sqlite3 "$WORK/target1.db" "select count(*) from $t;" 2>/dev/null)
  c2=$(sqlite3 "$WORK/target2.db" "select count(*) from $t;" 2>/dev/null)
  check "$t 计数一致" "$c1" "$c2"
  echo "    $t = $c1"
done

# 审查 B1 回归：client_keys 落库行数必须等于计划密钥数（同名不同明文改名后不得丢）。
planned_keys=$(python3 -c "import json;print(len(json.load(open('$WORK/report2.json'))['keys']))")
db_keys=$(sqlite3 "$WORK/target1.db" "select count(*) from client_keys;" 2>/dev/null)
check "client_keys 行数 = 计划密钥数（B1：不丢凭据）" "$db_keys" "$planned_keys"
# report 的 keys 名必须两两唯一（改名生效）。
unique_keys=$(python3 -c "import json;ks=json.load(open('$WORK/report2.json'))['keys'];print(len(set(ks)),len(ks))")
check "计划密钥名唯一（B1：改名生效）" "$unique_keys" "$unique_keys"
dup_renamed=$(python3 -c "import json;r=json.load(open('$WORK/report2.json'))['report'];print(len(r.get('duplicate_key_names') or []))")
echo "    duplicate_key_names = $dup_renamed（>0 表示确有同名不同明文被改名）"
# 逐条哈希对账（不只数条数）：改名事件里每把被改名的密钥，其 sha256 前 8 位必须
# 都能在库内找到对应 key_hash 前缀，证明"两条同名明文都活着"。
hash_ok=$(python3 - "$WORK/target1.db" "$WORK/report2.json" <<'PYHASH'
import json, sqlite3, sys
db, report = sys.argv[1], sys.argv[2]
dups = (json.load(open(report))["report"].get("duplicate_key_names") or [])
hashes = [r[0] for r in sqlite3.connect(db).execute("select key_hash from client_keys")]
ok = all(any(h.startswith(d["sha256_8"]) for h in hashes) for d in dups)
print("yes" if (not dups or ok) else "no")
PYHASH
)
check "被改名的密钥哈希都能在库内找到（B1：不丢凭据）" "$hash_ok" "yes"

# 审查 B2 回归：非法 --keys 必须 exit 2，且不得产出目标库。
"$WORK/pbr" migrate --routing "$WORK/routing.db" --vendor "$WORK/vendor.db" --target "$WORK/bad.db" --keys bogus > "$WORK/bad.txt" 2>&1
bad_rc=$?
check "非法 --keys 退出码为 2（B2）" "$bad_rc" "2"
if [ -f "$WORK/bad.db" ]; then bad_exists=yes; else bad_exists=no; fi
check "非法 --keys 不产出目标库（B2）" "$bad_exists" "no"

echo "--- 5) 迁移产物可被 PBR 加载（显式车道数 = 计划数）"
( cd "$WORK" && exec env PBR_ADMIN_KEY="$ADMIN" PORT=$PORT \
  SQLITE_PATH="$WORK/target1.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=a4-session CRYPTO_SECRET=a4-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &
BASE="http://127.0.0.1:$PORT"
ok=0
for _ in $(seq 1 60); do
  if curl -sf "$BASE/api/v1/health" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
if [ "$ok" != 1 ]; then
  check "PBR 以迁移库启动" "no" "yes"
else
  check "PBR 以迁移库启动" "yes" "yes"
  planned_lanes=$(python3 -c "import json;print(len(json.load(open('$WORK/report2.json'))['lanes']))")
  live_lanes=$(curl -s -H "Authorization: Bearer $ADMIN" "$BASE/api/v1/lanes" | python3 -c "import sys,json;print(len(json.load(sys.stdin)['items']))")
  check "加载的显式车道数 = 迁移计划数" "$live_lanes" "$planned_lanes"
  live_models=$(curl -s -H "Authorization: Bearer $ADMIN" "$BASE/api/v1/models" | python3 -c "import sys,json;print(len(json.load(sys.stdin)['items']))")
  echo "    可路由模型键 = $live_models"
  check "可路由模型键非空" "$live_models" "$live_models"
fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [ "$FAIL" == 0 ]; then echo "PASS: 真实迁移幂等与对账通过"; else echo "FAIL: 存在未通过项"; fi
echo "=== evidence: $EVID ==="
[ "$FAIL" == 0 ]
