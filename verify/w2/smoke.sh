#!/usr/bin/env bash
# W2 验收：容错（冷却 / 熔断三态 + 半开 + 指数退避 / 亲和 / 四模式 / 错误分类 / 超时）。
#
# 验收门（docs/goal-prompt.md §四 W2）：
#   1) 指向必然 500 的假端点 → 熔断打开、错误快抛（不再打上游）；
#   2) 修好 → 半开窗口内自动复通（留时间戳证据）；
#   3) probe 逐成员；
#   4) 四模式各跑通一条。
# 附加（design-v1 §7.5/§7.6、routing-spec §4.1）：429 不误伤、client_error 不冷却不换人、
# 欠费 400 由关键词捕获。
#
# 全程本地：独立端口、独立 SQLite、内置假上游；不触碰任何现网容器与凭据。
set -uo pipefail

REPO=/root/powerbar-rations
WORK=/tmp/pbr-w2
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/w2/run-$STAMP.log"
PORT=6793
UPSTREAM_PORT=6804
GOOD_KEY='sk-w2-good'
PBR_PW='Pbr-W2-Verify-Passw0rd!2026'
LEGACY_PW='Pbr-W2-Legacy-Passw0rd!2026'

exec > "$EVID" 2>&1
echo "=== W2 容错验收 @ $STAMP ==="

PASS=0
FAIL=0
check() { # check <描述> <实际> <期望子串>
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then
    echo "  PASS: $desc"; PASS=$((PASS + 1))
  else
    echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL + 1))
  fi
}
check_not() {
  local desc="$1" actual="$2" unwanted="$3"
  if [[ "$actual" != *"$unwanted"* ]]; then
    echo "  PASS: $desc"; PASS=$((PASS + 1))
  else
    echo "  FAIL: $desc（不应包含 $unwanted）"; echo "        实际: $actual"; FAIL=$((FAIL + 1))
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
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) \
  || { echo "FAIL: build"; exit 1; }

echo "--- 1) 启动内置假上游 :$UPSTREAM_PORT（要求 key=$GOOD_KEY）"
"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.stdout" 2>&1 &

echo "--- 2) 启动 pbr :$PORT"
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=w2-session CRYPTO_SECRET=w2-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

echo "--- 3) 等待就绪"
ok=0
for i in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:$PORT/api/v1/health" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
[[ $ok == 1 ]] || { echo "FAIL: 服务未就绪"; tail -40 "$WORK/pbr.log"; exit 1; }
echo "ready after ${i}s"

BASE="http://127.0.0.1:$PORT"
H='-H Content-Type:application/json'
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{"d":d}))' "$1"; }
jq1() { python3 -c "import sys,json;print(eval(sys.argv[1],{'d':json.load(sys.stdin),'json':json}))" "$1"; }
# assert_json <描述> <json 文本> <python 布尔表达式，d=解析结果>
assert_json() {
  local desc="$1" payload="$2" expr="$3"
  if echo "$payload" | python3 -c "import sys,json;d=json.load(sys.stdin);sys.exit(0 if ($expr) else 1)" 2>/dev/null; then
    echo "  PASS: $desc"; PASS=$((PASS + 1))
  else
    echo "  FAIL: $desc"; echo "        表达式: $expr"; echo "        实际  : $payload"; FAIL=$((FAIL + 1))
  fi
}

echo
echo "--- 4) 首启设口令并取得管理密钥"
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
[[ -n "$ADMIN_KEY" ]] || { echo "FAIL: 未取得管理密钥"; exit 1; }
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
echo "admin_key: 已取得（${#ADMIN_KEY} 字节，不打印）"

echo "--- 5) 收紧熔断参数（经 /api/v1/system/options）：阈值 1、打开 2s、退避上限 10s"
curl -s "${A[@]}" -X PUT -d '{"circuit_failure_threshold":1,"circuit_open_seconds":2,"circuit_max_open_seconds":10}' \
  "$BASE/api/v1/system/options" | head -c 400; echo
OPTS=$(curl -s "${A[@]}" "$BASE/api/v1/system/options")
check "选项已落库并可回读" "$OPTS" '"circuit_failure_threshold":1'

echo "--- 6) 建两个渠道（同一假上游，"'"$GOOD_KEY"'"，PBR 渠道没有 priority）"
for ch in channel-a channel-b; do
  curl -s "${A[@]}" -X PUT -d '{
    "type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'",
    "models":["mode-failover"],"enabled":true
  }' "$BASE/api/v1/channels/$ch" > /dev/null
done
echo "  渠道：$(curl -s "${A[@]}" "$BASE/api/v1/channels" | jq1 "','.join(c['name'] for c in d['items'])")"

echo "--- 7) PBR 客户端密钥（W7 后基座用户/令牌链路已删除）"
CLIENT_KEY=$(curl -s "${A[@]}" -X POST -d '{"name":"w2-key"}' "$BASE/api/v1/keys" | jget 'd["key"]')
[[ -n "$CLIENT_KEY" ]] || { echo "FAIL: 未取得客户端密钥"; exit 1; }
echo "  客户端密钥已取得"

chat() { # chat <model>
  curl -s -D "$WORK/headers.txt" -o "$WORK/body.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
    -H "Authorization: Bearer $CLIENT_KEY" -H 'Content-Type: application/json' \
    -d '{"model":"'"$1"'","messages":[{"role":"user","content":"ping"}]}'
}
chat_stream() { # chat_stream <model> —— 流式：走 member_stream_first_event_timeout
  curl -s -D "$WORK/headers.txt" -o "$WORK/body.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
    -H "Authorization: Bearer $CLIENT_KEY" -H 'Content-Type: application/json' \
    -d '{"model":"'"$1"'","stream":true,"messages":[{"role":"user","content":"ping"}]}'
}
served_by() { grep -i '^x-served-by:' "$WORK/headers.txt" | tr -d '\r' | sed 's/^[Xx]-[Ss]erved-[Bb]y: *//'; }
control() { curl -s $H -X POST -d "$1" "http://127.0.0.1:$UPSTREAM_PORT/__control" > /dev/null; }
upstream_hits() { grep -ac '"path"' "$WORK/upstream.log" 2>/dev/null || echo 0; }
health() { curl -s "${A[@]}" "$BASE/api/v1/lanes/$1/health"; }

echo
echo "--- 8) 建四条单成员车道（各自独立模型名，互不干扰）"
# solo-model：熔断开/半开复通用；rate-model：429 不误伤；其余用于四模式。
curl -s "${A[@]}" -X PUT -d '{
  "enabled":true,"mode":"failover",
  "config":{"member_max_attempts":1,"member_retry_interval_seconds":0,
            "member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":30,
            "member_cooldown_seconds":1,"member_affinity_seconds":0},
  "members":[{"channel":"channel-a","upstream_model":"solo-model","priority":1}]
}' "$BASE/api/v1/lanes/solo-model" > /dev/null
curl -s "${A[@]}" -X PUT -d '{
  "enabled":true,"mode":"failover",
  "config":{"member_max_attempts":1,"member_retry_interval_seconds":0,
            "member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":30,
            "member_cooldown_seconds":1,"member_affinity_seconds":0},
  "members":[{"channel":"channel-a","upstream_model":"rate-model","priority":1}]
}' "$BASE/api/v1/lanes/rate-model" > /dev/null
for m in bad-model quota-model; do
  curl -s "${A[@]}" -X PUT -d '{
    "enabled":true,"mode":"failover",
    "config":{"member_max_attempts":1,"member_retry_interval_seconds":0,
              "member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":30,
              "member_cooldown_seconds":1,"member_affinity_seconds":0},
    "members":[{"channel":"channel-a","upstream_model":"'"$m"'","priority":1}]
  }' "$BASE/api/v1/lanes/$m" > /dev/null
done
echo "  lanes: $(curl -s "${A[@]}" "$BASE/api/v1/lanes" | jq1 "','.join(l['name'] for l in d['items'])")"

echo
echo "=== 验收门 1：必然 500 → 熔断打开、错误快抛 ==="
control '{"model":"solo-model","status":500}'
echo "  注入 500；请求 1"
CODE=$(chat solo-model); echo "    HTTP $CODE $(head -c 120 "$WORK/body.json")"
check "第 1 次失败后无可用成员（503）" "$CODE" "503"
sleep 1.2
echo "  请求 2（冷却到期后探测）"
CODE=$(chat solo-model); echo "    HTTP $CODE $(head -c 120 "$WORK/body.json")"
check "第 2 次失败后仍 503" "$CODE" "503"
H_SOLO=$(health solo-model); echo "  health: $(echo "$H_SOLO" | jq1 "json.dumps({m['member']:{'circuit':m['circuit'],'score':m['failure_score'],'cooldown_until':m['cooldown_until'],'open_until':m['circuit_open_until']} for m in d['members']},ensure_ascii=False)")"
check "熔断已打开" "$H_SOLO" '"circuit":"open"'
check "记录到最后错误分类" "$H_SOLO" '"last_error_kind":"soft_transient"'

BEFORE=$(upstream_hits)
echo "  熔断打开期间连打 3 发，上游计数 before=$BEFORE"
for i in 1 2 3; do chat solo-model > /dev/null; done
AFTER=$(upstream_hits)
echo "  上游计数 after=$AFTER"
check "熔断打开期间不再打上游（错误快抛）" "$AFTER" "$BEFORE"

echo
echo "=== 验收门 2：修好 → 半开窗口内自动复通（时间戳证据） ==="
control '{"model":"solo-model","status":200}'
echo "  已修好；等待打开窗口（2s）结束进入半开"
sleep 2.2
H_OPEN=$(health solo-model)
OPEN_TS=$(echo "$H_OPEN" | jq1 "[e['ts'] for e in d['events'] if e['type']=='circuit_open'][0]")
echo "  circuit_open 事件时间戳: $OPEN_TS"
CODE=$(chat solo-model); echo "  HTTP $CODE $(head -c 160 "$WORK/body.json")"
check "半开探测成功（恢复服务）" "$CODE" "200"
check "响应 model 回填请求名" "$(cat "$WORK/body.json")" '"model":"solo-model"'
H_CLOSED=$(health solo-model)
echo "  health events: $(echo "$H_CLOSED" | jq1 "','.join(e['type']+'@'+str(e['ts']) for e in d['events'][-4:])")"
check "熔断已复通" "$H_CLOSED" '"circuit":"closed"'
check "有 circuit_half_open 时间戳" "$H_CLOSED" '"type":"circuit_half_open"'
check "有 circuit_closed 时间戳" "$H_CLOSED" '"type":"circuit_closed"'
HALF_TS=$(echo "$H_CLOSED" | jq1 "[e['ts'] for e in d['events'] if e['type']=='circuit_half_open'][0]")
CLOSED_TS=$(echo "$H_CLOSED" | jq1 "[e['ts'] for e in d['events'] if e['type']=='circuit_closed'][0]")
echo "  时间戳序列：open=$OPEN_TS half_open=$HALF_TS closed=$CLOSED_TS"
python3 - "$OPEN_TS" "$HALF_TS" "$CLOSED_TS" <<'PY' && { echo "  PASS: 打开 → 半开 → 复通的时间戳严格递增"; PASS=$((PASS+1)); } || { echo "  FAIL: 时间戳顺序不对"; FAIL=$((FAIL+1)); }
import sys
o, h, c = (int(x) for x in sys.argv[1:4])
sys.exit(0 if o <= h <= c and o > 0 else 1)
PY

echo
echo "=== 验收门 3：probe 逐成员 ==="
curl -s "${A[@]}" -X PUT -d '{
  "enabled":true,"mode":"failover",
  "members":[
    {"channel":"channel-a","upstream_model":"mode-failover","priority":20},
    {"channel":"channel-b","upstream_model":"mode-failover","priority":10}
  ]
}' "$BASE/api/v1/lanes/mode-failover" > /dev/null
PROBE=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/mode-failover/probe")
echo "  $PROBE"
check "probe 覆盖 2 个成员" "$PROBE" '"probed":2'
# 按 api-spec §6.4 的语义断言（status/duration_ms），不依赖字段顺序
check "成员 1 探活成功" "$(echo "$PROBE" | python3 -c 'import sys,json;r=json.load(sys.stdin)["results"][0];print(r["status"] if r.get("status")=="success" else "wrong:"+str(r.get("status")))')" "success"
check "成员 2 探活成功" "$(echo "$PROBE" | python3 -c 'import sys,json;r=json.load(sys.stdin)["results"][1];print(r["status"])')" "success"
CH_TEST=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/channels/channel-a/test?model=mode-failover")
echo "  channel test: $CH_TEST"
check "单渠道探活成功" "$CH_TEST" '"ok":true'

echo
echo "=== 验收门 4：两种现存模式各跑通一条；已删模式必须 422 invalid_mode ==="
curl -s "${A[@]}" -X PUT -d '{
  "enabled":true,"mode":"manual","active_member":"channel-b/mode-failover",
  "members":[
    {"channel":"channel-a","upstream_model":"mode-failover","priority":20},
    {"channel":"channel-b","upstream_model":"mode-failover","priority":10}
  ]
}' "$BASE/api/v1/lanes/mode-manual" > /dev/null
# failover 车道用上游模型名 mode-failover，但请求名也叫 mode-failover，已在上一步建好。

CODE=$(chat mode-failover); echo "  failover : HTTP $CODE served=$(served_by)"
check "failover 走 P1" "$(served_by)" 'channel=1:channel-a'

CODE=$(chat mode-manual); echo "  manual   : HTTP $CODE served=$(served_by)"
check "manual 只用 active_member" "$CODE" "200"
check "manual 命中指定成员 channel-b" "$(served_by)" 'channel=2:channel-b'

# routing-spec §2.3：weighted / round_robin 已删除，传入返回 422 invalid_mode
# （成员 weight 也会被忽略）。
echo "  -- 已删模式拒绝 --"
for mode in weighted round_robin; do
  BODY=$(curl -s -o /tmp/pbr-w2-mode.json -w '%{http_code}' "${A[@]}" -X PUT -d '{
    "enabled":true,"mode":"'"$mode"'",
    "members":[{"channel":"channel-a","upstream_model":"mode-x","priority":1}]
  }' "$BASE/api/v1/lanes/mode-rejected-$mode")
  check "$mode 被拒绝为 422" "$BODY" "422"
  check "$mode 错误码为 invalid_mode" "$(cat /tmp/pbr-w2-mode.json)" 'invalid_mode'
done

echo
echo "=== 验收门 5：上游挂起（不发响应头）→ 按真实超时换人，不得当成客户端取消 ==="
# 回归：等待响应头阶段的超时曾被误判为 canceled（不换人、不冷却），
# 必须翻译成 deadline → soft_transient → 换到下一成员（routing-spec §8）。
curl -s "${A[@]}" -X PUT -d '{
  "enabled":true,"mode":"failover",
  "config":{"member_max_attempts":1,"member_retry_interval_seconds":0,
            "member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":1,
            "member_cooldown_seconds":5,"member_affinity_seconds":0},
  "members":[
    {"channel":"channel-a","upstream_model":"hang-a","priority":20},
    {"channel":"channel-b","upstream_model":"hang-b","priority":10}
  ]}' "$BASE/api/v1/lanes/hang-model" > /dev/null
control '{"model":"hang-a","delay_ms":4000}'   # channel-a 的上游模型：4s 内不发响应头
control '{"model":"hang-b","status":200}'      # channel-b 正常
HITS_BEFORE=$(upstream_hits)
CODE=$(chat_stream hang-model); SERVED=$(served_by)
HITS_AFTER=$(upstream_hits)
echo "  HTTP $CODE served=$SERVED upstream hits ${HITS_BEFORE}→${HITS_AFTER}"
# 注意：流式请求在打上游之前就已由网关写出 200 + SSE 头，所以 HTTP 码不能区分成败；
# 判据是"最终服务者变成 channel-b"与"响应体不是错误包"。
check "挂起成员被跳过，由下一成员服务" "$SERVED" 'channel=2:channel-b'
check_not "响应体不是错误包" "$(cat "$WORK/body.json")" '"error"'
H_HANG=$(health hang-model)
echo "  hang health: $(echo "$H_HANG" | jq1 "json.dumps({m['member']:{'kind':m.get('last_error_kind'),'cooldown_until':m['cooldown_until'],'circuit':m['circuit']} for m in d['members']},ensure_ascii=False)")"
check "超时归类为 soft_transient（不是 canceled）" "$H_HANG" '"soft_transient"'
check_not "超时不得被标成 canceled" "$H_HANG" '"last_error_kind":"canceled"'
assert_json "挂起成员进入冷却/熔断（未被静默放过）" "$H_HANG" "any(m['member'].startswith('channel-a') and (m['cooldown_until']>0 or m['circuit']!='closed') for m in d['members'])"
control '{"model":"hang-a","status":200,"delay_ms":0}'

echo
echo "=== 附加：错误分类（429 不误伤 / client_error 不冷却 / 欠费关键词） ==="
control '{"model":"rate-model","status":429}'
for i in 1 2 3; do chat rate-model > /dev/null; sleep 1.2; done
H_RATE=$(health rate-model)
echo "  rate health: $(echo "$H_RATE" | jq1 "json.dumps({m['member']:{'circuit':m['circuit'],'score':m['failure_score'],'kind':m.get('last_error_kind')} for m in d['members']},ensure_ascii=False)")"
check_not "429 不打开熔断" "$H_RATE" '"circuit":"open"'
check "429 归类为 soft_rate_limit" "$H_RATE" '"soft_rate_limit"'

control '{"model":"bad-model","status":400,"body":"{\"error\":{\"message\":\"bad request\"}}"}'
CODE=$(chat bad-model); echo "  client_error: HTTP $CODE $(head -c 120 "$WORK/body.json")"
check "client_error 原样返回 400（不是 503）" "$CODE" "400"
H_BAD=$(health bad-model)
echo "  client_error health: $H_BAD"
assert_json "client_error 不触发冷却" "$H_BAD" "all(m['cooldown_until']==0 for m in d['members'])"
assert_json "client_error 不记熔断计分" "$H_BAD" "all(m['failure_score']==0 for m in d['members'])"

control '{"model":"quota-model","status":400,"body":"{\"error\":{\"message\":\"Your credit balance is too low\"}}"}'
CODE=$(chat quota-model); echo "  quota: HTTP $CODE $(head -c 160 "$WORK/body.json")"
check "欠费 400 被关键词捕获（换人后无可用）" "$CODE" "503"
H_QUOTA=$(health quota-model)
echo "  quota health: $(echo "$H_QUOTA" | jq1 "json.dumps([{m['member']:{'kind':m.get('last_error_kind'),'cooldown':m['cooldown_until']}} for m in d['members']],ensure_ascii=False)")"
check "欠费归类为 hard_quota" "$H_QUOTA" '"hard_quota"'

echo
echo "--- 取消熔断与冷却（circuits/reset）"
RESET=$(curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/solo-model/circuits/reset")
echo "  $RESET"
check "reset 返回清除计数" "$RESET" '"reset":'

echo
echo "--- 私有数据自查：源码中不得出现本次测试密钥"
LEAK=$(grep -rn "sk-w2-good" "$REPO" --exclude-dir=.git --exclude-dir=verify 2>/dev/null | head -5)
if [[ -z "$LEAK" ]]; then echo "  PASS: 源码无测试密钥残留"; PASS=$((PASS + 1)); else echo "  FAIL: $LEAK"; FAIL=$((FAIL + 1)); fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then
  echo "PASS: W2 验收门全部通过"
else
  echo "FAIL: 存在未通过项"
  echo "--- pbr.log 末尾 ---"; tail -50 "$WORK/pbr.log"
  exit 2
fi
echo "=== evidence: $EVID ==="
