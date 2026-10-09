#!/usr/bin/env bash
# W8 的 C 项：故障注入矩阵逐项验证（分类 / 冷却 / 换人 / 快抛 / 已提交后不转移 /
# 成员级拒绝逃逸 / 流内错误帧）。
#
# 覆盖：上游超时、流中途断流、坏响应、空响应、凭据失效、欠费关键词、连接失败、
# 普通 400 成员级拒绝（换成员、不冷却、全员拒绝原样返回）、流内 error 帧（提交前换成员）。
# 全程本地：独立端口、独立 SQLite、内置假上游；不触碰任何现网容器与凭据。
set -uo pipefail

REPO="${PBR_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORK=/tmp/pbr-fault
STAMP=$(date +%Y%m%d-%H%M%S)
EVID="$REPO/verify/final/fault-$STAMP.log"
PORT=6800
UPSTREAM_PORT=6811
DEAD_PORT=6812
GOOD_KEY='sk-fault-good'
PBR_PW='test123'

exec > "$EVID" 2>&1
echo "=== W8-C 故障注入矩阵 @ $STAMP ==="

PASS=0; FAIL=0
check() {
  local desc="$1" actual="$2" want="$3"
  if [[ "$actual" == *"$want"* ]]; then echo "  PASS: $desc"; PASS=$((PASS+1));
  else echo "  FAIL: $desc"; echo "        期望包含: $want"; echo "        实际     : $actual"; FAIL=$((FAIL+1)); fi
}
# assert_json <描述> <json 文本> <python 表达式>（实参必须按此顺序）
assert_json() {
  local desc="$1" payload="$2" expr="$3"
  if echo "$payload" | python3 -c "import sys,json;d=json.load(sys.stdin);sys.exit(0 if ($expr) else 1)" 2>/dev/null; then
    echo "  PASS: $desc"; PASS=$((PASS+1))
  else
    echo "  FAIL: $desc"; echo "        表达式: $expr"; echo "        实际  : $(echo "$payload" | head -c 400)"; FAIL=$((FAIL+1))
  fi
}

cleanup() {
  pkill -f "$WORK/pbr" 2>/dev/null
  pkill -f "$WORK/fakeupstream" 2>/dev/null
  wait 2>/dev/null
}
trap cleanup EXIT

rm -rf "$WORK"; mkdir -p "$WORK"
for p in "$PORT" "$UPSTREAM_PORT" "$DEAD_PORT"; do
  if ss -ltn 2>/dev/null | grep -q ":$p "; then echo "FAIL: 端口 $p 被占用"; exit 1; fi
done

echo "--- 0) 构建"
( cd "$REPO" && go build -o "$WORK/pbr" . \
  && go build -o "$WORK/fakeupstream" ./internal/testutil/fakeupstream/cmd/fakeupstream ) || { echo "FAIL: build"; exit 1; }

"$WORK/fakeupstream" -addr "127.0.0.1:$UPSTREAM_PORT" -require-key "$GOOD_KEY" -log "$WORK/upstream.log" \
  > "$WORK/upstream.out" 2>&1 &
( cd "$WORK" && exec env PORT=$PORT \
  SQLITE_PATH="$WORK/pbr.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate" \
  SESSION_SECRET=fault-session CRYPTO_SECRET=fault-crypto GIN_MODE=release \
  "$WORK/pbr" > "$WORK/pbr.log" 2>&1 ) &

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
control() { curl -s $H -X POST -d "$1" "http://127.0.0.1:$UPSTREAM_PORT/__control" > /dev/null; }

echo "--- 1) 初始化与配置（两条车道：双成员用于换人，单成员用于熔断/快抛）"
ADMIN_KEY=$(curl -s $H -d '{"password":"'"$PBR_PW"'"}' "$BASE/api/v1/setup" | jget 'd["admin_key"]')
A=(-H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json')
# PBR 渠道没有 priority：两个渠道只是"都声明同一模型"，顺序由车道成员决定。
for ch in channel-a channel-b; do
  curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["fi-model"],"enabled":true}' "$BASE/api/v1/channels/$ch" > /dev/null
done
# 死渠道：指向一个没人监听的端口，用于验证"连接失败"分类
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$DEAD_PORT"'","key":"sk-dead","models":["fi-model"],"enabled":true}' "$BASE/api/v1/channels/channel-dead" > /dev/null
# 双成员车道：非流式超时 1s、冷却 1s
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":1,"member_stream_first_event_timeout_seconds":1,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"channel-a","model":"fi-model","priority":20},{"channel":"channel-b","model":"fi-model","priority":10}]}' "$BASE/api/v1/lanes/fi-model" > /dev/null
# 单成员车道（含死渠道兜底），用于"全挂快抛"
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":1,"member_stream_first_event_timeout_seconds":1,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"channel-a","model":"solo-model","priority":1}]}' "$BASE/api/v1/lanes/solo-model" > /dev/null
# 收紧熔断阈值，使"打开 → 半开 → 复通"能在验收时长内跑完
curl -s "${A[@]}" -X PUT -d '{"circuit_failure_threshold":1,"circuit_open_seconds":2,"circuit_max_open_seconds":10}' "$BASE/api/v1/system/options" > /dev/null
CLIENT=$(curl -s "${A[@]}" -X POST -d '{"name":"fault-client"}' "$BASE/api/v1/keys" | jget 'd["key"]')
reset_lane() { curl -s "${A[@]}" -X POST "$BASE/api/v1/lanes/$1/circuits/reset" > /dev/null; }
health() { curl -s "${A[@]}" "$BASE/api/v1/lanes/$1/health"; }
chat() { curl -s -o "$WORK/body.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $CLIENT" -H 'Content-Type: application/json' -d "$1"; }
echo "  配置完成"

echo
echo "=== C1 上游超时（延迟 3s > 成员超时 1s）==="
reset_lane fi-model
control '{"model":"fi-model","status":200,"delay_ms":3000}'
CODE=$(chat '{"model":"fi-model","messages":[{"role":"user","content":"hi"}]}')
echo "  HTTP $CODE"
H1=$(health fi-model)
echo "  health: $(echo "$H1" | python3 -c 'import sys,json;d=json.load(sys.stdin);print([(m["channel"],m["last_error_kind"],m["cooldown_until"] is not None) for m in d["members"]])')"
check "超时后无可用成员（503 快抛）" "$CODE" "503"
assert_json "超时归类为 soft_transient 且进入冷却" "$H1" "any(m['last_error_kind']=='soft_transient' and m['cooldown_until'] is not None for m in d['members'])"
control '{"model":"fi-model","status":200}'

echo
echo "=== C2 凭据失效（401）==="
reset_lane fi-model
control '{"model":"fi-model","status":401,"body":"{\"error\":{\"message\":\"invalid api key\"}}"}'
CODE=$(chat '{"model":"fi-model","messages":[{"role":"user","content":"hi"}]}')
H2=$(health fi-model)
echo "  HTTP $CODE；health: $(echo "$H2" | python3 -c 'import sys,json;d=json.load(sys.stdin);print([(m["channel"],m["last_error_kind"]) for m in d["members"]])')"
check "P1 凭据失效后换人（503 说明两条都失效）" "$CODE" "503"
assert_json "401 归类为 hard_auth" "$H2" "any(m['last_error_kind']=='hard_auth' for m in d['members'])"
control '{"model":"fi-model","status":200}'

echo
echo "=== C3 坏响应（200 但非 JSON）==="
reset_lane fi-model
control '{"model":"fi-model","mode":"bad_json"}'
CODE=$(chat '{"model":"fi-model","messages":[{"role":"user","content":"hi"}]}')
H3=$(health fi-model)
echo "  HTTP $CODE；health: $(echo "$H3" | python3 -c 'import sys,json;d=json.load(sys.stdin);print([(m["channel"],m["last_error_kind"]) for m in d["members"]])')"
check "坏响应不会当成成功" "$CODE" "503"
assert_json "坏响应归类为 bad_response" "$H3" "any(m['last_error_kind']=='bad_response' for m in d['members'])"

echo
echo "=== C4 空响应（200 且 body 为空）==="
reset_lane fi-model
control '{"model":"fi-model","mode":"empty"}'
CODE=$(chat '{"model":"fi-model","messages":[{"role":"user","content":"hi"}]}')
H4=$(health fi-model)
echo "  HTTP $CODE；health: $(echo "$H4" | python3 -c 'import sys,json;d=json.load(sys.stdin);print([(m["channel"],m["last_error_kind"]) for m in d["members"]])')"
check "空响应不会当成成功" "$CODE" "503"
control '{"model":"fi-model","mode":""}'

echo
echo "=== C5 欠费关键词（HTTP 400 + 关键词）==="
reset_lane fi-model
control '{"model":"fi-model","status":400,"body":"{\"error\":{\"message\":\"Your credit balance is too low\"}}"}'
CODE=$(chat '{"model":"fi-model","messages":[{"role":"user","content":"hi"}]}')
H5=$(health fi-model)
echo "  HTTP $CODE；health: $(echo "$H5" | python3 -c 'import sys,json;d=json.load(sys.stdin);print([(m["channel"],m["last_error_kind"]) for m in d["members"]])')"
check "欠费 400 会换人（两成员都欠费 → 503）" "$CODE" "503"
assert_json "归类为 hard_quota" "$H5" "any(m['last_error_kind']=='hard_quota' for m in d['members'])"
control '{"model":"fi-model","status":200,"body":""}'

echo
echo "=== C6 连接失败（渠道指向无人监听的端口）==="
reset_lane fi-model
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":1,"member_stream_first_event_timeout_seconds":1,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"channel-dead","model":"dead-model","priority":30},{"channel":"channel-b","model":"fi-model","priority":10}]}' "$BASE/api/v1/lanes/dead-model" > /dev/null
control '{"model":"dead-model","status":200}'
CODE=$(chat '{"model":"dead-model","messages":[{"role":"user","content":"hi"}]}')
H6=$(health dead-model)
echo "  HTTP $CODE；health: $(echo "$H6" | python3 -c 'import sys,json;d=json.load(sys.stdin);print([(m["channel"],m["last_error_kind"]) for m in d["members"]])')"
check "死渠道失败后由健康成员兜底（200）" "$CODE" "200"
assert_json "连接失败归类为软故障并冷却死渠道" "$H6" "any(m['channel']=='channel-dead' and m['last_error_kind'] in ('soft_transient','bad_response') and m['cooldown_until'] is not None for m in d['members'])"

echo
echo "=== C7 流中途断流：已提交后不再故障转移（routing-spec §4.3）==="
reset_lane fi-model
control '{"model":"fi-model","mode":"disconnect_stream"}'
BEFORE=$(grep -ac '"path"' "$WORK/upstream.log")
STREAM_OUT="$WORK/stream.out"
curl -s -N --max-time 20 -o "$STREAM_OUT" -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT" -H 'Content-Type: application/json' \
  -d '{"model":"fi-model","messages":[{"role":"user","content":"hi"}],"stream":true}' || true
AFTER=$(grep -ac '"path"' "$WORK/upstream.log")
CHUNKS=$(grep -ac 'data:' "$STREAM_OUT" 2>/dev/null || echo 0)
echo "  客户端收到分片数：$CHUNKS；上游命中 $BEFORE → $AFTER"
check "客户端确实收到了部分流（≥2 帧）" "$(python3 -c "print(1 if $CHUNKS>=2 else 0)")" "1"
check "断流后没有向第二个成员重新发起（不重复投递）" "$AFTER" "$((BEFORE + 1))"
DONE_COUNT=$(grep -c 'DONE' "$STREAM_OUT" 2>/dev/null || true)
note_observed_done() { echo "  NOTE: 断流响应中 [DONE] 出现次数 = ${DONE_COUNT:-0}（上游未发结束事件就断开）"; }
note_observed_done
control '{"model":"fi-model","mode":""}'

echo
echo "=== C8 单成员全挂：快抛 503 且不轮询 ==="
reset_lane solo-model
control '{"model":"solo-model","status":500}'
BEFORE=$(grep -ac '"path"' "$WORK/upstream.log")
START=$(date +%s%N)
CODE=$(chat '{"model":"solo-model","messages":[{"role":"user","content":"hi"}]}')
MS=$(( ($(date +%s%N) - START) / 1000000 ))
AFTER=$(grep -ac '"path"' "$WORK/upstream.log")
echo "  HTTP $CODE，耗时 ${MS}ms，上游命中 $BEFORE → $AFTER"
check "全挂 503" "$CODE" "503"
check "固定 body" "$(cat "$WORK/body.json")" 'No available channel for model solo-model'
check "上游只被打了一次（未轮询）" "$AFTER" "$((BEFORE + 1))"

# C8-2：冷却未到期时再打一次——这次在**中间件阶段**就快抛（一个可选成员都没有），
# 仍必须留下"为什么没用它"的 cooldown 记录（routing-spec §4.2：快抛前必须已把
# 本次所有尝试写入日志，含 cooldown/circuit_break）。缺了这条，最需要证据的快抛路径
# 反而查不出原因。
CODE2=$(chat '{"model":"solo-model","messages":[{"role":"user","content":"hi"}]}')
FAST_LOGS=$(curl -s "${A[@]}" "$BASE/api/v1/logs?model=solo-model&limit=2")
echo "  冷却期内再次请求 → HTTP $CODE2"
check "冷却期内再次请求仍是 503" "$CODE2" "503"
assert_json "中间件快抛也带上本次选路记录（cooldown）" "$FAST_LOGS" \
  "len(d['items'])>0 and d['items'][0]['total_attempts']==1 and d['items'][0]['attempts'][0]['status']=='cooldown' and 'channel-a' in d['items'][0]['attempts'][0]['member']"
check "快抛未再打上游" "$(grep -ac '"path"' "$WORK/upstream.log")" "$AFTER"
control '{"model":"solo-model","status":200}'

echo
echo "=== C9 熔断开 → 半开 → 复通（单成员，时间戳证据）==="
reset_lane solo-model
control '{"model":"solo-model","status":500}'
# 每轮之间要等过冷却（1s），否则后续请求会被冷却挡掉、失败分数不累积
for i in 1 2 3; do chat '{"model":"solo-model","messages":[{"role":"user","content":"hi"}]}' > /dev/null; sleep 1.3; done
H9=$(health solo-model)
CLOSED_BEFORE=$(echo "$H9" | python3 -c 'import sys,json;d=json.load(sys.stdin);print([m["circuit"] for m in d["members"]][0])')
control '{"model":"solo-model","status":200}'
sleep 3
CODE=$(chat '{"model":"solo-model","messages":[{"role":"user","content":"hi"}]}')
H9B=$(health solo-model)
echo "  复通前后：$CLOSED_BEFORE → $(echo "$H9B" | python3 -c 'import sys,json;d=json.load(sys.stdin);print([m["circuit"] for m in d["members"]][0])')"
check "复通后恢复正常服务" "$CODE" "200"
assert_json "熔断已回到 closed" "$H9B" "all(m['circuit']=='closed' for m in d['members'])"
assert_json "有 circuit_open 事件时间戳" "$H9B" "any(e['type']=='circuit_open' and e['ts']>0 for e in d['events'])"

echo
echo "=== C10 上游 400（非关键词）：成员级拒绝换成员、不冷却（routing-spec §4.1/§4.2）==="
# 400/422 默认是"上游对该成员的拒绝"（内容审查、参数不支持等）：必须换成员消化；
# 成员本身无过错——不写冷却、不计熔断分；全部成员都拒绝时把上游错误原样返回客户端。
reset_lane fi-model
control '{"model":"fi-model","status":400,"body":"{\"error\":{\"message\":\"reasoning_effort must be low, high, xhigh, max or an integer within [1, 100]\",\"type\":\"invalid_request_error\"}}"}'
CODE=$(chat '{"model":"fi-model","messages":[{"role":"user","content":"hi"}]}')
H10=$(health fi-model)
echo "  HTTP $CODE；health: $(echo "$H10" | python3 -c 'import sys,json;d=json.load(sys.stdin);print([(m["channel"],m["last_error_kind"],m["cooldown_until"] is not None) for m in d["members"]])')"
check "全员拒绝时返回上游 400 报文（§4.2 例外，不合成 503）" "$CODE" "400"
check "错误体保留上游报文" "$(cat "$WORK/body.json")" "reasoning_effort must be"
assert_json "拒绝不写冷却、不计熔断分（circuit 全 closed、无冷却），但留痕 upstream_reject" "$H10" \
  "all(m['cooldown_until'] is None and m['circuit']=='closed' for m in d['members']) and any(m['last_error_kind']=='upstream_reject' for m in d['members'])"
control '{"model":"fi-model","status":200,"body":""}'

echo
echo "=== C11 流内 error 帧（提交前）：不转发、换成员（routing-spec §4.4）==="
# 内容审查类上游：200 + text/event-stream，但流内吐 error 帧。提交前必须识别、
# 不转发给客户端，并按成员级拒绝换下一成员；第二个成员（渠道映射到正常模型）服务成功。
curl -s "${A[@]}" -X PUT -d '{"type":"openai","base_url":"http://127.0.0.1:'"$UPSTREAM_PORT"'","key":"'"$GOOD_KEY"'","models":["fi-stream"],"model_mapping":{"fi-stream":"fi-stream-ok"},"enabled":true}' "$BASE/api/v1/channels/channel-map" > /dev/null
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":2,"member_stream_first_event_timeout_seconds":2,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"channel-a","model":"fi-stream","priority":20},{"channel":"channel-map","model":"fi-stream","priority":10}]}' "$BASE/api/v1/lanes/fi-stream" > /dev/null
control '{"model":"fi-stream","mode":"stream_error_frame"}'
STREAM_OUT="$WORK/stream-reject.out"
CODE=$(curl -s -N --max-time 20 -o "$STREAM_OUT" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT" -H 'Content-Type: application/json' \
  -d '{"model":"fi-stream","messages":[{"role":"user","content":"hi"}],"stream":true}')
H11=$(health fi-stream)
echo "  HTTP $CODE；客户端输出: $(head -c 200 "$STREAM_OUT" | tr '\n' ' ')"
check "换到下一成员后成功（200）" "$CODE" "200"
check "客户端拿到的是正常内容（不是 error 帧）" "$(cat "$STREAM_OUT")" "pong"
check "客户端输出不得包含上游拒绝报文" "$(python3 -c "print('LEAK' if 'inappropriate' in open('$STREAM_OUT').read() else 'CLEAN')")" "CLEAN"
assert_json "流内拒绝归类 upstream_reject 且不写冷却" "$H11" \
  "any(m['last_error_kind']=='upstream_reject' and m['cooldown_until'] is None for m in d['members'])"
# 单成员车道（只有会吐 error 帧的成员）：客户端拿到 400 + 上游报文。
control '{"model":"fi-stream-ok","mode":"stream_error_frame"}'
control '{"model":"fi-stream","mode":""}'
curl -s "${A[@]}" -X PUT -d '{"enabled":true,"mode":"failover","config":{"member_max_attempts":1,"member_retry_interval_seconds":0,"member_non_stream_response_timeout_seconds":2,"member_stream_first_event_timeout_seconds":2,"member_cooldown_seconds":1,"member_affinity_seconds":0},"members":[{"channel":"channel-map","model":"fi-stream","priority":10}]}' "$BASE/api/v1/lanes/fi-stream-solo" > /dev/null
CODE=$(curl -s -o "$WORK/body.json" -w '%{http_code}' -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $CLIENT" -H 'Content-Type: application/json' \
  -d '{"model":"fi-stream-solo","messages":[{"role":"user","content":"hi"}],"stream":true}')
echo "  单成员（自身吐 error 帧）→ HTTP $CODE"
check "流内拒绝无成员可换时返回 400（§4.2 例外）" "$CODE" "400"
check "错误体带上游报文" "$(cat "$WORK/body.json")" "inappropriate"
control '{"model":"fi-stream-ok","mode":""}'

echo
echo "--- 私有数据自查"
LEAK=$(grep -rn "sk-fault-good" "$REPO" --exclude-dir=.git --exclude-dir=verify 2>/dev/null | head -3)
if [[ -z "$LEAK" ]]; then echo "  PASS: 源码无测试密钥残留"; PASS=$((PASS+1)); else echo "  FAIL: $LEAK"; FAIL=$((FAIL+1)); fi

echo
echo "=== 结果：PASS=$PASS FAIL=$FAIL ==="
if [[ $FAIL -eq 0 ]]; then echo "PASS: 故障注入矩阵全部通过"; else
  echo "FAIL: 存在未通过项"; echo "--- pbr.log 末尾 ---"; tail -40 "$WORK/pbr.log"; exit 2
fi
echo "=== evidence: $EVID ==="
