#!/usr/bin/env bash
# W8-A4（可复核替代证据）：迁移/导入路径的**幂等性**。
#
# 背景：design-v1 §11 明确规定"迁移不在当前阶段实施"，其输入是运维私有台账
# （渠道清单/车道成员/凭据注入点位/单价表，均不入仓库），因此无法在本仓库里
# 对"旧库副本跑两次"做端到端演练——缺的是私有输入，不是实现能力。
#
# 但 §11.2 对迁移提出的**通用规则**是可以在本仓库验证的：
#   - 幂等：以 name 为键 upsert，可重复跑；
#   - 对账报告：输出旧对象 → 新对象计数与未归属项。
# 本脚本验证这两条落在 `POST /api/v1/import` 上确实成立，作为 A4 的替代证据：
#   1. 导出当前配置 → 原样导入两次；
#   2. 第二次的 diff 必须全部落在 unchanged（幂等）；
#   3. 改动其中一项再导入，diff 必须精确指出变化项（对账正确）。
#
# 全程本地：独立端口、独立 SQLite；不触碰任何现网容器与凭据。
set -uo pipefail

REPO=/root/powerbar-rations
WORK=/tmp/pbr-a4
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/final/a4-$STAMP.log"
PORT=6831
GOOD_KEY='sk-a4-good'
PBR_PW='Pbr-A4-Idempotent-Passw0rd!2026'

exec > "$EVID" 2>&1
echo "=== W8-A4 导入幂等与对账（迁移通用规则）@ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}

cleanup() { pkill -f "$WORK/pbr" 2>/dev/null; }
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"
( cd "$REPO" && go build -o "$WORK/pbr" . ) || { echo "FAIL: go build"; exit 1; }
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=a4-session CRYPTO_SECRET=a4-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

BASE="http://127.0.0.1:$PORT"
for _ in $(seq 1 60); do curl -sf "$BASE/api/v1/health" >/dev/null 2>&1 && break; sleep 1; done

H='-H Content-Type:application/json'
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')

echo "--- 0) 造一份可迁移的配置（渠道 + 车道 + 密钥）"
curl -s "${A[@]}" -X PUT "$BASE/api/v1/channels/a4-ch1" \
  -d '{"type":"openai","base_url":"http://127.0.0.1:1","key":"'"$GOOD_KEY"'","models":["a4-m1","a4-m2"],"enabled":true}' >/dev/null
curl -s "${A[@]}" -X PUT "$BASE/api/v1/channels/a4-ch2" \
  -d '{"type":"openai","base_url":"http://127.0.0.1:1","key":"'"$GOOD_KEY"'","models":["a4-m1"],"enabled":true}' >/dev/null
curl -s "${A[@]}" -X PUT "$BASE/api/v1/lanes/a4-m1" \
  -d '{"enabled":true,"mode":"failover","members":[{"channel":"a4-ch1","upstream_model":"a4-m1","priority":20},{"channel":"a4-ch2","upstream_model":"a4-m1","priority":10}]}' >/dev/null
curl -s "${A[@]}" -X POST "$BASE/api/v1/keys" -d '{"name":"a4-key"}' >/dev/null
check "初始配置就绪" "$(curl -s "${A[@]}" "$BASE/api/v1/models" | jget 'len(d["items"])')" "2"

echo "--- 1) 导出当前配置"
curl -s "${A[@]}" "$BASE/api/v1/export" -o "$WORK/bundle.json"
check "导出成功" "$(python3 -c 'import json;d=json.load(open("'"$WORK"'/bundle.json"));print(len(d.get("channels",[])),len(d.get("lanes",[])))')" "2 1"

echo "--- 2) 原样导入第一次"
R1=$(curl -s "${A[@]}" -X POST --data-binary @"$WORK/bundle.json" "$BASE/api/v1/import")
echo "  $(echo "$R1" | head -c 400)"
check "第一次导入合法" "$(echo "$R1" | jget 'd["valid"]')" "True"

echo "--- 3) 原样导入第二次（关键：必须幂等 → diff 全部 unchanged）"
R2=$(curl -s "${A[@]}" -X POST --data-binary @"$WORK/bundle.json" "$BASE/api/v1/import?dry_run=true")
echo "  $(echo "$R2" | head -c 600)"
check "第二次 channels 无新增" "$(echo "$R2" | jget 'len(d["diff"]["channels"]["add"])')" "0"
check "第二次 channels 无变更" "$(echo "$R2" | jget 'len(d["diff"]["channels"]["update"])')" "0"
check "第二次 channels 全部 unchanged" "$(echo "$R2" | jget 'len(d["diff"]["channels"]["unchanged"])')" "2"
check "第二次 lanes 无新增" "$(echo "$R2" | jget 'len(d["diff"]["lanes"]["add"])')" "0"
check "第二次 lanes 无变更" "$(echo "$R2" | jget 'len(d["diff"]["lanes"]["update"])')" "0"
check "第二次 lanes 全部 unchanged" "$(echo "$R2" | jget 'len(d["diff"]["lanes"]["unchanged"])')" "1"
check "第二次 keys 无变更" "$(echo "$R2" | jget 'len(d["diff"]["keys"]["update"])')" "0"
check "第二次 options 无变更" "$(echo "$R2" | jget 'len(d["diff"]["options"]["changed"])')" "0"

echo "--- 4) 对账正确性：改动一项后，diff 必须精确指出变化项"
python3 - "$WORK/bundle.json" "$WORK/bundle2.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
for ch in d["channels"]:
    if ch["name"] == "a4-ch1":
        # 只改这一项。渠道已无 priority/weight，这里用 models 作为探针。
        ch["models"] = ["a4-m1", "a4-m2", "a4-m3"]
json.dump(d, open(sys.argv[2], "w"))
PY
R3=$(curl -s "${A[@]}" -X POST --data-binary @"$WORK/bundle2.json" "$BASE/api/v1/import?dry_run=true")
echo "  $(echo "$R3" | head -c 500)"
check "只报 a4-ch1 变更" "$(echo "$R3" | jget 'd["diff"]["channels"]["update"]')" "a4-ch1"
check "a4-ch2 仍为 unchanged" "$(echo "$R3" | jget '"a4-ch2" in d["diff"]["channels"]["unchanged"]')" "True"
check "车道未受影响" "$(echo "$R3" | jget 'len(d["diff"]["lanes"]["update"])')" "0"

echo "--- 5) 真跑一次改动导入，然后回读验证落库一致"
curl -s "${A[@]}" -X POST --data-binary @"$WORK/bundle2.json" "$BASE/api/v1/import" >/dev/null
check "a4-ch1 模型清单变更已落库" "$(curl -s "${A[@]}" "$BASE/api/v1/channels/a4-ch1" | jget '"a4-m3" in d["models"]')" "True"
R4=$(curl -s "${A[@]}" -X POST --data-binary @"$WORK/bundle2.json" "$BASE/api/v1/import?dry_run=true")
check "再次导入回到幂等（无 update）" "$(echo "$R4" | jget 'len(d["diff"]["channels"]["update"])')" "0"

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
[[ $FAIL == 0 ]] || exit 1
