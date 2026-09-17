#!/usr/bin/env bash
# W8 的 D 项（有界版本）：连续并发流量下的正确性、日志完整性与资源增长。
#
# 设计的目标口径是「≥2 小时或 ≥5 万请求」；本脚本在可接受时长内跑一个有界样本
# （默认 3000 请求 / 并发 30），测的是**同一条链路**的这些性质：
#   - 每发请求都恰好一行元数据日志（不重不漏）
#   - 并发下无跨请求串号（假上游回显请求标记，响应内容必须与请求一一对应）
#   - 进程 RSS 与 SQLite 体积增长可控
# 未覆盖：≥2 小时长时间稳定性、聚合表在超大数据量下的正确性。
#
# 全程本地：独立端口、独立 SQLite、内置假上游；不触碰任何现网容器与凭据。
set -uo pipefail

REPO=/root/powerbar-rations
WORK=/tmp/pbr-longrun
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/final/longrun-$STAMP.log"
PORT=6802
UPSTREAM_PORT=6814
GOOD_KEY='sk-longrun-good'
PBR_PW='Pbr-Longrun-Passw0rd!2026'
TOTAL=${TOTAL:-3000}
CONCURRENCY=${CONCURRENCY:-30}

exec > "$EVID" 2>&1
echo "=== W8-D 有界长稳与并发（${TOTAL} 请求 / 并发 ${CONCURRENCY}）@ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}
assert_num() { # assert_num <描述> <实际> <比较式，用 python 求值，变量为 v>
  local desc="$1" value="$2" expr="$3"
  if python3 -c "v=$value; import sys; sys.exit(0 if ($expr) else 1)" 2>/dev/null; then
    echo "  PASS: $desc（实测 $value）"; PASS=$((PASS+1))
  else
    echo "  FAIL: $desc（实测 $value，要求 $expr）"; FAIL=$((FAIL+1))
  fi
}

cleanup() {
  pkill -f "$WORK/pbr" 2>/dev/null
  pkill -f "$WORK/fakeupstream" 2>/dev/null
  wait 2>/dev/null
}
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"
for p in "$PORT" "$UPSTREAM_PORT"; do
  if ss -ltn 2>/dev/null | grep -q ":$p "; then echo "FAIL: 端口 $p 被占用"; exit 1; fi
done

echo "--- 0) 构建"
( cd "$REPO" && go build -o "$WORK/pbr" . \
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: build"; exit 1; }

"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.out" 2>&1 &
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=long-session CRYPTO_SECRET=long-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

ok=0
for i in $(seq 1 60); do curl -sf "http://127.0.0.1:$PORT/api/v1/health" >/dev/null 2>&1 && { ok=1; break; }; sleep 1; done
[[ $ok == 1 ]] || { echo "FAIL: 服务未就绪"; tail -30 "$WORK/pbr.log"; exit 1; }
PBR_PID=$(pgrep -f "$WORK/pbr" | head -1)
echo "  就绪（pid=$PBR_PID，第 ${i}s）"

BASE="http://127.0.0.1:$PORT"
H='-H Content-Type:application/json'
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }

echo "--- 1) 配置"
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
for m in lr-a lr-b lr-c; do
  curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["'"$m"'"],"enabled":true}' "$BASE/api/v1/channels/ch-$m" > /dev/null
  curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":30,"member_stream_first_event_timeout_seconds":30,"member_cooldown_seconds":5,"member_affinity_seconds":0},"members":[{"channel":"ch-'"$m"'","upstream_model":"'"$m"'","priority":1}]}' "$BASE/api/v1/lanes/$m" > /dev/null
done
CLIENT=$(curl -s "${A[@]}" -X POST -d '{"name":"longrun-client"}' "$BASE/api/v1/keys" | jget 'd["key"]')

echo "--- 2) 基线资源占用"
RSS_BEFORE=$(awk '/VmRSS/{print $2}' /proc/$PBR_PID/status)
DB_BEFORE=$(stat -c %s "$WORK/pbr.db")
echo "  RSS=${RSS_BEFORE}kB  DB=${DB_BEFORE}B"

echo "--- 3) 打 ${TOTAL} 发混合流量（并发 ${CONCURRENCY}，含成功/失败/不同模型）"
cat > "$WORK/load.py" <<'PYLOAD'
import json, os, random, sys, time, urllib.request
from concurrent.futures import ThreadPoolExecutor

base = sys.argv[1]
key = sys.argv[2]
total = int(sys.argv[3])
concurrency = int(sys.argv[4])
out = sys.argv[5]

models = ["lr-a", "lr-b", "lr-c"]

def one(i):
    model = models[i % len(models)]
    marker = f"req-{i}-{'x' * (i % 7)}"
    if i % 20 == 19:
        model = "lr-ghost"          # 无渠道声明 → 503
    payload = json.dumps({"model": model, "messages": [{"role": "user", "content": marker}]}).encode()
    request = urllib.request.Request(base + "/v1/chat/completions", data=payload, method="POST",
                                     headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            body = response.read().decode()
            status = response.status
    except urllib.error.HTTPError as err:
        body, status = err.read().decode(), err.code
    except Exception as err:  # noqa: BLE001
        body, status = str(err), 0
    return {"i": i, "model": model, "marker": marker, "status": status, "body": body}

start = time.time()
with ThreadPoolExecutor(max_workers=concurrency) as pool:
    results = list(pool.map(one, range(total)))
elapsed = time.time() - start
json.dump({"elapsed_s": elapsed, "results": results}, open(out, "w"))
print(f"  完成 {total} 发，用时 {elapsed:.1f}s（{total/elapsed:.0f} req/s）")
PYLOAD
python3 "$WORK/load.py" "$BASE" "$CLIENT" "$TOTAL" "$CONCURRENCY" "$WORK/load.json"

echo "--- 4) 正确性：状态码分布与跨请求串号"
python3 - "$WORK/load.json" <<'PYCHECK'
import json, sys
data = json.load(open(sys.argv[1]))
results = data["results"]
ok = [r for r in results if r["status"] == 200]
ghost = [r for r in results if r["model"] == "lr-ghost"]
mismatch = []
for r in ok:
    if r"pong from fake upstream|" + r["marker"] not in r["body"]:
        mismatch.append(r["i"])
print(json.dumps({
    "total": len(results),
    "ok": len(ok),
    "ghost": len(ghost),
    "ghost_503": sum(1 for r in ghost if r["status"] == 503),
    "mismatch": len(mismatch),
    "status_counts": {str(s): sum(1 for r in results if r["status"] == s) for s in sorted({r["status"] for r in results})},
}, ensure_ascii=False))
PYCHECK
REPORT=$(python3 - "$WORK/load.json" <<'PYCHECK2'
import json, sys
data = json.load(open(sys.argv[1]))
results = data["results"]
ok = [r for r in results if r["status"] == 200]
ghost = [r for r in results if r["model"] == "lr-ghost"]
mismatch = [r["i"] for r in ok if ("pong from fake upstream|" + r["marker"]) not in r["body"]]
print(json.dumps({"ok": len(ok), "ghost": len(ghost), "ghost_503": sum(1 for r in ghost if r["status"] == 503),
                  "mismatch": len(mismatch), "other": sum(1 for r in results if r["status"] not in (200, 503))}))
PYCHECK2
)
echo "  统计：$REPORT"
assert_num "成功请求数符合预期（总数 - 无渠道请求）" "$(echo "$REPORT" | jget 'd["ok"]')" "v == $TOTAL - $(echo "$REPORT" | jget 'd["ghost"]')"
assert_num "无渠道声明的一律 503" "$(echo "$REPORT" | jget 'd["ghost_503"]')" "v == $(echo "$REPORT" | jget 'd["ghost"]')"
assert_num "无跨请求串号（响应内容与请求标记一一对应）" "$(echo "$REPORT" | jget 'd["mismatch"]')" "v == 0"
assert_num "没有意外状态码" "$(echo "$REPORT" | jget 'd["other"]')" "v == 0"

echo "--- 5) 日志完整性：行数 = 请求数"
sleep 3   # 等在途请求的收尾写入完成
ROWS=$(python3 - "$WORK/pbr.db" <<'PYROWS'
import sqlite3, sys
conn = sqlite3.connect(sys.argv[1])
print(conn.execute("SELECT COUNT(*) FROM pbr_request_logs").fetchone()[0])
PYROWS
)
echo "  日志行数：$ROWS（请求数 $TOTAL）"
check "日志行数与请求数一致" "$ROWS" "$TOTAL"
ATTEMPTS=$(python3 - "$WORK/pbr.db" <<'PYAT'
import sqlite3, sys
conn = sqlite3.connect(sys.argv[1])
# 无渠道声明的 503 按设计就是"没有任何尝试"，因此只检查确实选过成员的日志
print(conn.execute("""SELECT COUNT(*) FROM pbr_request_logs
                      WHERE http_status <> 503 AND (attempts IS NULL OR attempts='' OR attempts='[]')""").fetchone()[0])
PYAT
)
check "有成员可尝试的日志都有 attempts 链" "$ATTEMPTS" "0"

echo "--- 6) 资源占用（静默后采样，区分高水位与泄漏）"
sample() {
  echo "    [+${1}s] RSS=$(awk '/VmRSS/{print $2}' /proc/$PBR_PID/status)kB 线程=$(awk '/Threads/{print $2}' /proc/$PBR_PID/status) 句柄=$(ls /proc/$PBR_PID/fd 2>/dev/null | wc -l)"
}
echo "  基线 RSS=${RSS_BEFORE}kB"
sleep 15; sample 15
sleep 45; sample 60
RSS_AFTER=$(awk '/VmRSS/{print $2}' /proc/$PBR_PID/status)
THREADS=$(awk '/Threads/{print $2}' /proc/$PBR_PID/status)
FD=$(ls /proc/$PBR_PID/fd 2>/dev/null | wc -l)
DB_AFTER=$(stat -c %s "$WORK/pbr.db")
GROWTH_KB=$(( RSS_AFTER - RSS_BEFORE ))
echo "  最终：RSS ${RSS_BEFORE}kB → ${RSS_AFTER}kB（+${GROWTH_KB}kB）；DB ${DB_BEFORE}B → ${DB_AFTER}B"
assert_num "RSS 增长受控（<128MB）" "$GROWTH_KB" "v < 131072"
DB_PER_ROW=$(python3 -c "print(($DB_AFTER - $DB_BEFORE) / max($ROWS,1))")
assert_num "单行日志平均占用 <2KB" "$(printf '%.1f' "$DB_PER_ROW")" "v < 2048"

echo "--- 7) goroutine 与句柄未失控（进程仍可服务）"
ALIVE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT" -H 'Content-Type: application/json' \
  -d '{"model":"lr-a","messages":[{"role":"user","content":"after-load"}]}')
check "压测后仍能正常服务" "$ALIVE" "200"
echo "  句柄数=$FD 线程数=$THREADS（GOMAXPROCS=$(nproc)）"

echo "--- 8) 聚合统计与明细一致"
# 审查 B4：第 7 步末发的 after-load 请求与第 8 步计数之间存在竞态——晚到的日志写入
# 会让「明细」与「聚合」两次读数差 1，导致历史 README 的 PASS=12 不可稳定复现。
# 修法：先等明细/小时聚合两个读数连续稳定（在途写入全部落库），再对三种读数做
# 带重试的一致性比对，任何一次三者相等即判定通过。
python3 - "$WORK/pbr.db" <<'PYSTABLE'
import sqlite3, sys, time
conn = sqlite3.connect(sys.argv[1])
def read():
    rows = conn.execute("SELECT COUNT(*) FROM pbr_request_logs").fetchone()[0]
    hourly = conn.execute("SELECT COALESCE(SUM(requests),0) FROM pbr_stats_hourly WHERE group_kind='lane'").fetchone()[0]
    return rows, hourly
last = None
stable = 0
deadline = time.time() + 30
while time.time() < deadline:
    cur = read()
    if cur == last:
        stable += 1
        if stable >= 3:
            print("  明细/小时聚合已稳定：rows=%d hourly=%d" % cur)
            break
    else:
        stable = 0
        last = cur
    time.sleep(0.5)
PYSTABLE
ROWS_NOW=""; STATS_TOTAL=""; HOURLY=""; MATCHED=no
for attempt in 1 2 3 4 5 6; do
  ROWS_NOW=$(python3 - "$WORK/pbr.db" <<'PYROWS2'
import sqlite3, sys
conn = sqlite3.connect(sys.argv[1])
print(conn.execute("SELECT COUNT(*) FROM pbr_request_logs").fetchone()[0])
PYROWS2
)
  STATS=$(curl -s "${A[@]}" "$BASE/api/v1/stats?granularity=hour&group_by=lane")
  STATS_TOTAL=$(echo "$STATS" | jget 'sum(i["requests"] for i in d["items"])')
  HOURLY=$(python3 - "$WORK/pbr.db" <<'PYHOURLY'
import sqlite3, sys
conn = sqlite3.connect(sys.argv[1])
print(conn.execute("SELECT COALESCE(SUM(requests),0) FROM pbr_stats_hourly WHERE group_kind='lane'").fetchone()[0])
PYHOURLY
)
  echo "  [第 ${attempt} 次] stats 合计=$STATS_TOTAL 小时聚合=$HOURLY 日志行数=$ROWS_NOW"
  if [[ "$STATS_TOTAL" == "$ROWS_NOW" && "$HOURLY" == "$ROWS_NOW" ]]; then MATCHED=yes; break; fi
  sleep 2
done
check "聚合请求数与日志行数一致" "$STATS_TOTAL" "$ROWS_NOW"
check "小时聚合表与明细一致" "$HOURLY" "$ROWS_NOW"
if [[ "$MATCHED" == yes ]]; then PASS=$((PASS+1)); echo "  PASS: 三种读数在一次快照内一致（无时序竞态）";
else echo "  FAIL: 重试 6 次仍有读数差异（明细/聚合不一致）"; FAIL=$((FAIL+1)); fi

echo
echo "--- 私有数据自查"
LEAK=$(grep -rn "sk-longrun-good" "$REPO" --exclude-dir=.git --exclude-dir=verify 2>/dev/null | head -3)
if [[ -z "$LEAK" ]]; then echo "  PASS: 源码无测试密钥残留"; PASS=$((PASS+1)); else echo "  FAIL: $LEAK"; FAIL=$((FAIL+1)); fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: 有界长稳与并发验收通过"; else
  echo "FAIL: 存在未通过项"; tail -30 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
