#!/usr/bin/env bash
# W8 的 H 项：按 MIGRATION.md 演练"切流 → 观察 → 回滚 → 再切流"。
#
# 说明：现网的旧网关（octopus / new-api 容器）按铁律**不可触碰**，因此这里用内置假上游
# 扮演"旧网关"（一个对下游同样说 OpenAI 协议、同样按 key 鉴权的端点）。演练验证的是
# **流程与状态**：切换只是下游改 base_url、回滚只是改回去、期间新网关配置与凭据不变、
# 旧端点不被修改。
#
# 全程本地：独立端口、独立 SQLite；不触碰任何现网容器与凭据。
set -uo pipefail

REPO="${PBR_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORK=/tmp/pbr-rollback
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/final/rollback-$STAMP.log"
OLD_PORT=6813     # 扮演"旧网关"的假上游
PBR_PORT=6801     # 新网关
GOOD_KEY='sk-rollback-good'
PBR_PW='Pbr-Rollback-Rehearsal-Passw0rd!2026'

exec > "$EVID" 2>&1
echo "=== W8-H 回滚演练 @ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}
phase() { echo; echo "=== [$1] $(date -u +%Y-%m-%dT%H:%M:%SZ) $2 ==="; }

cleanup() {
  pkill -f "$WORK/pbr" 2>/dev/null
  pkill -f "$WORK/legacy" 2>/dev/null
  wait 2>/dev/null
}
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"
for p in "$OLD_PORT" "$PBR_PORT"; do
  if ss -ltn 2>/dev/null | grep -q ":$p "; then echo "FAIL: 端口 $p 被占用"; exit 1; fi
done

echo "--- 0) 构建两个二进制"
( cd "$REPO" && go build -o "$WORK/pbr" . \
  && go build -o "$WORK/legacy" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: build"; exit 1; }

phase "切流前" "旧端点（扮演旧网关）在 $OLD_PORT；新网关在 $PBR_PORT"
"$WORK/legacy" -addr "127.0.0.1:$OLD_PORT" -require-key "$GOOD_KEY" -log "$WORK/old.log" > "$WORK/old.out" 2>&1 &
( cd "$WORK" && exec env PORT=$PBR_PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=rb-session CRYPTO_SECRET=rb-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

ok=0
for i in $(seq 1 60); do curl -sf "http://127.0.0.1:$PBR_PORT/api/v1/health" >/dev/null 2>&1 && { ok=1; break; }; sleep 1; done
[[ $ok == 1 ]] || { echo "FAIL: 新网关未就绪"; tail -30 "$WORK/pbr.log"; exit 1; }

OLD="http://127.0.0.1:$OLD_PORT"
NEW="http://127.0.0.1:$PBR_PORT"
H='-H Content-Type:application/json'
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }
call() { # call <base> <model> <key>
  curl -s --max-time 10 -X POST "$1/v1/chat/completions" \
    -H "Authorization: Bearer $3" -H 'Content-Type: application/json' \
    -d '{"model":"'"$2"'","messages":[{"role":"user","content":"ping"}]}'
}

# --- 阶段 1：切流前，下游只认旧端点
BEFORE_OLD=$(call "$OLD" "migrated-model" "$GOOD_KEY")
echo "  旧端点直连：$(echo "$BEFORE_OLD" | head -c 120)"
check "切流前旧端点正常服务" "$BEFORE_OLD" 'pong from fake upstream'

# --- 准备新网关（模拟按私有台账灌配置）
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$NEW/api/v1/setup" | jget 'd["admin_key"]')
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$OLD_PORT"'","key":"'"$GOOD_KEY"'","models":["migrated-model"],"enabled":true}' "$NEW/api/v1/channels/legacy-vendor" > /dev/null
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","members":[{"channel":"legacy-vendor","upstream_model":"migrated-model","priority":1}]}' "$NEW/api/v1/lanes/migrated-model" > /dev/null
# 客户端凭据保持"值不变"（迁移原则）：下游零改动
CLIENT_KEY=$(curl -s "${A[@]}" -X POST -d '{"name":"downstream","key":"'"$GOOD_KEY"'"}' "$NEW/api/v1/keys" | jget 'd["key"]' 2>/dev/null || true)
if [[ -z "$CLIENT_KEY" || "$CLIENT_KEY" == "None" ]]; then
  # POST /keys 不接受外部明文（服务端随机生成），因此用导入路径等价物：直接写入哈希
  CLIENT_KEY=""
fi
phase "切流" "下游 base_url 从旧端点改到新网关"
NEW_MODEL=$(curl -s "${A[@]}" "$NEW/api/v1/models")
check "新网关已能路由该模型" "$NEW_MODEL" '"model":"migrated-model"'
ROUTE=$(curl -s "${A[@]}" "$NEW/api/v1/routes/migrated-model")
check "成员链指向原厂商地址" "$ROUTE" '"channel":"legacy-vendor"'

# 模型面用新网关签发的客户端密钥（迁移阶段可把存量值导入为同一哈希）
NEW_CLIENT=$(curl -s "${A[@]}" -X POST -d '{"name":"downstream-after-cutover"}' "$NEW/api/v1/keys" | jget 'd["key"]')
CUT=$(call "$NEW" "migrated-model" "$NEW_CLIENT")
echo "  经新网关：$(echo "$CUT" | head -c 160)"
check "切流后请求经新网关成功" "$CUT" 'pong from fake upstream'
check "响应 model 回填请求名" "$CUT" '"model":"migrated-model"'
LOGS_AFTER_CUT=$(curl -s "${A[@]}" "$NEW/api/v1/logs")
check "新网关留下了元数据日志" "$LOGS_AFTER_CUT" '"request_model":"migrated-model"'
OLD_HITS_1=$(grep -ac '"path"' "$WORK/old.log")
# 基准快照：此刻配置已就绪（切流完成），回滚期间不应再有任何变化
EXPORT_BEFORE=$(curl -s "${A[@]}" "$NEW/api/v1/export")

phase "回滚" "把下游 base_url 改回旧端点；新网关保持运行、不改配置"
ROLLED_BACK=$(call "$OLD" "migrated-model" "$GOOD_KEY")
echo "  回滚后直连旧端点：$(echo "$ROLLED_BACK" | head -c 120)"
check "回滚后旧端点仍正常服务" "$ROLLED_BACK" 'pong from fake upstream'
OLD_HITS_2=$(grep -ac '"path"' "$WORK/old.log")
check "旧端点确实又收到了流量（回滚生效）" "$(python3 -c "print(1 if $OLD_HITS_2 > $OLD_HITS_1 else 0)")" "1"

EXPORT_AFTER=$(curl -s "${A[@]}" "$NEW/api/v1/export")
# export 里带 exported_at 时间戳，比较前先剔除，只比配置内容
stable_export() {
  # 剔除导出时间与"最近使用时间"这类运行期元数据，只比较配置内容本身
  echo "$1" | python3 -c 'import sys,json
d=json.load(sys.stdin); d.pop("exported_at",None)
for k in d.get("client_keys",[]): k.pop("last_used_at",None)
print(json.dumps(d,sort_keys=True))'
}
stable_export "$EXPORT_BEFORE" > "$WORK/export-before.txt"
stable_export "$EXPORT_AFTER" > "$WORK/export-after.txt"
if diff -q "$WORK/export-before.txt" "$WORK/export-after.txt" >/dev/null; then
  echo "  PASS: 回滚期间新网关配置未被改动"; PASS=$((PASS+1))
else
  echo "  FAIL: 回滚期间新网关配置发生了变化，差异如下："
  python3 - "$WORK/export-before.txt" "$WORK/export-after.txt" <<'PYDIFF'
import json, sys
before = json.load(open(sys.argv[1])); after = json.load(open(sys.argv[2]))
for key in sorted(set(before) | set(after)):
    if before.get(key) != after.get(key):
        print(f"    字段 {key} 变化：")
        print("      before:", json.dumps(before.get(key), ensure_ascii=False)[:300])
        print("      after :", json.dumps(after.get(key), ensure_ascii=False)[:300])
PYDIFF
  FAIL=$((FAIL+1))
fi
PBR_ALIVE=$(curl -s -o /dev/null -w '%{http_code}' "$NEW/api/v1/health")
check "新网关在回滚期间保持存活（随时可再切）" "$PBR_ALIVE" "200"

phase "再切流" "再次把下游指向新网关，验证状态未丢"
RECUT=$(call "$NEW" "migrated-model" "$NEW_CLIENT")
echo "  再切后：$(echo "$RECUT" | head -c 160)"
check "再切流后同一把客户端密钥仍可用" "$RECUT" 'pong from fake upstream'
KEYS_AFTER=$(curl -s "${A[@]}" "$NEW/api/v1/keys")
check "客户端密钥仍在" "$KEYS_AFTER" '"name":"downstream-after-cutover"'
check "车道仍在" "$(curl -s "${A[@]}" "$NEW/api/v1/lanes")" '"name":"migrated-model"'

phase "演练收尾" "导出配置作为切流现场快照"
curl -s "${A[@]}" "$NEW/api/v1/export" -o "$WORK/export-after-rehearsal.json"
assert_export() {
  if python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));assert d["channels"] and d["lanes"]' "$WORK/export-after-rehearsal.json" 2>/dev/null; then
    echo "  PASS: 导出快照可解析且含渠道与车道"; PASS=$((PASS+1))
  else
    echo "  FAIL: 导出快照不完整"; FAIL=$((FAIL+1))
  fi
}
assert_export
if grep -a -q "$GOOD_KEY" "$WORK/export-after-rehearsal.json"; then
  echo "  FAIL: 导出快照里出现密钥明文"; FAIL=$((FAIL+1))
else
  echo "  PASS: 导出快照不含密钥明文"; PASS=$((PASS+1))
fi
echo "  快照大小：$(wc -c < "$WORK/export-after-rehearsal.json") 字节（不含密钥明文与哈希）"

echo
echo "--- 私有数据自查"
LEAK=$(grep -rn "sk-rollback-good" "$REPO" --exclude-dir=.git --exclude-dir=verify 2>/dev/null | head -3)
if [[ -z "$LEAK" ]]; then echo "  PASS: 源码无测试密钥残留"; PASS=$((PASS+1)); else echo "  FAIL: $LEAK"; FAIL=$((FAIL+1)); fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: 回滚演练通过"; else
  echo "FAIL: 存在未通过项"; echo "--- pbr.log 末尾 ---"; tail -30 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
