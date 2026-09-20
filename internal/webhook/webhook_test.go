package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupDeliveryDB 提供内存库落投递日志；用后恢复全局 DB。
func setupDeliveryDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.WebhookDelivery{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
}

// restoreConfig 恢复配置注入点并清空防风暴表，避免用例间串扰。
func restoreConfig(t *testing.T) {
	t.Cleanup(func() {
		configLoader = func() string { return common.OptionMap[OptionKey] }
		stormMu.Lock()
		stormLast = map[string]int64{}
		stormMu.Unlock()
	})
}

// 验签契约（/doc §5.3）：HMAC-SHA256(secret, "{ts}.{raw_body}") 的 hex。
// 用独立构造的 HMAC 比对，保证实现与手册公式一致。
func TestSignPayloadMatchesHandbookFormula(t *testing.T) {
	secret := "notify-secret-1234"
	body := []byte(`{"type":"pbr","text":"[PBR] model-1/ch-a:model-1 熔断打开：open_seconds=60"}`)
	ts := int64(1789600000)

	got := SignPayload(secret, ts, body)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	assert.Equal(t, expected, got)

	// 换 secret / 换 body / 换 ts 都必须得到不同签名（防重放与防伪造的基本性质）。
	assert.NotEqual(t, got, SignPayload("other-secret", ts, body))
	assert.NotEqual(t, got, SignPayload(secret, ts+1, body))
	assert.NotEqual(t, got, SignPayload(secret, ts, []byte("{}")))
}

// secret 回显掩码（api-spec §5.8）：****+末 4 位；不足 8 字符全掩码；空串为空。
func TestMaskSecret(t *testing.T) {
	assert.Equal(t, "", MaskSecret(""))
	assert.Equal(t, "****", MaskSecret("abc"))
	assert.Equal(t, "****", MaskSecret("1234567"))
	assert.Equal(t, "****5678", MaskSecret("12345678"))
	assert.Equal(t, "****alue", MaskSecret("super-long-secret-value"))
}

// 配置校验：name 非空唯一、url 必须是 http(s)、events 取值在白名单内。
func TestValidateTargets(t *testing.T) {
	valid := []Target{
		{Name: "notify", URL: "http://127.0.0.1:8645/webhooks/xxx", Secret: "s", Enabled: true, Events: []string{}},
		{Name: "ops", URL: "https://ops.example/hooks", Events: []string{"circuit_open", "cooldown"}},
	}
	require.NoError(t, ValidateTargets(valid))

	dup := []Target{{Name: "a", URL: "http://a.example/x"}, {Name: "a", URL: "http://b.example/x"}}
	assert.Error(t, ValidateTargets(dup))

	for _, bad := range []Target{
		{Name: "", URL: "http://a.example/x"},
		{Name: "b", URL: "ftp://a.example/x"},
		{Name: "b", URL: "a.example/x"},
		{Name: "b", URL: ""},
		{Name: "b", URL: "http://a.example/x", Events: []string{"circuit_exploded"}},
	} {
		assert.Error(t, ValidateTargets([]Target{bad}), "target %+v 必须被拒绝", bad)
	}
}

// 防风暴：同一 (target, lane, member, event) 60s 窗口只放行一条；不同 key
// 互不影响；窗口过期后重新放行。
func TestStormWindowMergesDuplicates(t *testing.T) {
	restoreConfig(t)
	now := time.Unix(1789600000, 0)
	key := stormKey("notify", route.Event{Type: "circuit_open", Lane: "model-1", Member: "1:m"})

	assert.True(t, stormAllow(key, now), "窗口内第一条必须放行")
	assert.False(t, stormAllow(key, now.Add(30*time.Second)), "窗口内第二条必须被合并丢弃")
	assert.False(t, stormAllow(key, now.Add(59*time.Second)))
	assert.True(t, stormAllow(key, now.Add(61*time.Second)), "窗口过期后必须重新放行")

	other := stormKey("notify", route.Event{Type: "circuit_open", Lane: "model-1", Member: "2:m"})
	assert.True(t, stormAllow(other, now), "不同 member 的 key 必须独立判定")
}

// 重试与成功：前两次 500、第三次 200 → 最终成功，attempt=3，投递日志一行。
func TestDeliverRetriesThenSucceeds(t *testing.T) {
	restoreConfig(t)
	setupDeliveryDB(t)

	var mu sync.Mutex
	var requests int
	var bodies []string
	var timestamps []string
	var signatures []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests++
		bodies = append(bodies, string(raw))
		timestamps = append(timestamps, r.Header.Get("X-Webhook-Timestamp"))
		signatures = append(signatures, r.Header.Get("X-Webhook-Signature-V2"))
		mu.Unlock()
		if requests <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	previousDelays := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = previousDelays })

	target := Target{Name: "notify", URL: server.URL, Secret: "secret-abcdef", Enabled: true}
	ev := route.Event{Ts: 1789600000000, Type: route.EventCircuitOpen, Lane: "model-1", Member: "1:m", Detail: "open_seconds=60"}

	result := deliver(target, ev, summaryText(ev))
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, 200, result.HTTPStatus)
	assert.Equal(t, 3, result.Attempts)
	assert.Equal(t, 3, requests, "重试必须打到第三次 200 为止")
	assert.Empty(t, result.Error)

	// 同一请求体同一签名：三次请求的 ts/签名/body 完全一致。
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, len(setOf(bodies)), "重试必须复用同一请求体")
	assert.Equal(t, 1, len(setOf(timestamps)), "重试必须复用同一时间戳")
	assert.Equal(t, 1, len(setOf(signatures)), "重试必须复用同一签名")
	ts, err := strconv.ParseInt(timestamps[0], 10, 64)
	require.NoError(t, err)
	assert.Equal(t, SignPayload("secret-abcdef", ts, []byte(bodies[0])), signatures[0], "签名必须与手册公式一致")

	entries, err := model.ListWebhookDeliveries(10, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1, "最终结果只落一行")
	assert.Equal(t, "success", entries[0].Status)
	assert.Equal(t, 200, entries[0].HTTPStatus)
	assert.Equal(t, 3, entries[0].Attempt)
	assert.Equal(t, "notify", entries[0].Target)
	assert.Equal(t, route.EventCircuitOpen, entries[0].EventType)
	assert.Equal(t, "model-1", entries[0].Lane)
	assert.Equal(t, "1:m", entries[0].Member)
}

// 死信：始终 500 → 4 次尝试（首次 + 3 重试）后记 failed。
func TestDeliverDeadLetterAfterRetriesExhausted(t *testing.T) {
	restoreConfig(t)
	setupDeliveryDB(t)

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	previousDelays := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = previousDelays })

	target := Target{Name: "dead", URL: server.URL, Secret: "secret", Enabled: true}
	ev := route.Event{Ts: 1789600000000, Type: route.EventCooldown, Lane: "model-1", Member: "2:m"}

	result := deliver(target, ev, summaryText(ev))
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, 500, result.HTTPStatus)
	assert.Equal(t, maxAttempts, result.Attempts)
	assert.Equal(t, maxAttempts, requests)
	assert.Contains(t, result.Error, "unexpected status 500")

	entries, err := model.ListWebhookDeliveries(10, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "failed", entries[0].Status, "重试耗尽必须记死信")
	assert.Equal(t, maxAttempts, entries[0].Attempt)
}

// dispatch：enabled + 白名单匹配 + 防风暴合并共同决定投递。
// 投递为独立 goroutine，断言前用 WaitPendingDeliveries 等待排空。
func TestDispatchFiltersAndMerges(t *testing.T) {
	restoreConfig(t)

	var mu sync.Mutex
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configLoader = func() string {
		raw, _ := json.Marshal([]Target{
			{Name: "on", URL: server.URL, Enabled: true, Events: []string{"circuit_open"}},
			{Name: "off", URL: server.URL, Enabled: false},
		})
		return string(raw)
	}
	configCache.mu.Lock()
	configCache.raw = ""
	configCache.targets = nil
	configCache.mu.Unlock()

	// 白名单只含 circuit_open：cooldown 事件不投递。
	dispatch(route.Event{Ts: 1, Type: route.EventCooldown, Lane: "l", Member: "1:m"})
	WaitPendingDeliveries()
	assert.Equal(t, 0, requests)

	// 同一 (target, lane, member, event) 60s 内第二条被合并。
	dispatch(route.Event{Ts: 2, Type: route.EventCircuitOpen, Lane: "l", Member: "1:m"})
	WaitPendingDeliveries()
	assert.Equal(t, 1, requests)
	dispatch(route.Event{Ts: 3, Type: route.EventCircuitOpen, Lane: "l", Member: "1:m"})
	WaitPendingDeliveries()
	assert.Equal(t, 1, requests, "防风暴窗口内同类事件必须被丢弃")

	// 不同 member / 不同 lane 是新 key。
	dispatch(route.Event{Ts: 4, Type: route.EventCircuitOpen, Lane: "l", Member: "2:m"})
	WaitPendingDeliveries()
	assert.Equal(t, 2, requests)
	dispatch(route.Event{Ts: 5, Type: route.EventCircuitOpen, Lane: "other", Member: "1:m"})
	WaitPendingDeliveries()
	assert.Equal(t, 3, requests)
}

// 订阅入口：非白名单事件不入队；缓冲满时丢弃并计数（绝不阻塞调用方）。
func TestEnqueueEventFiltersAndDropsWhenFull(t *testing.T) {
	previousQueue := eventQueue
	eventQueue = make(chan route.Event, 2)
	t.Cleanup(func() { eventQueue = previousQueue })
	droppedEvents.Store(0)

	enqueueEvent(route.Event{Type: "affinity_start"}) // 非白名单
	enqueueEvent(route.Event{Type: "skip"})
	select {
	case ev := <-eventQueue:
		t.Fatalf("非白名单事件不应入队：%v", ev)
	default:
	}

	enqueueEvent(route.Event{Type: route.EventCircuitOpen})
	enqueueEvent(route.Event{Type: route.EventCircuitClosed})
	enqueueEvent(route.Event{Type: route.EventCooldown}) // 队列已满 → 丢弃并计数

	assert.EqualValues(t, 1, droppedEvents.Load(), "缓冲满必须丢弃并计数")
	assert.EqualValues(t, 1, DroppedEvents(), "DroppedEvents 出口与内部计数一致")
	assert.Len(t, eventQueue, 2)
}

// text 摘要格式（/doc §5.2）：`[PBR] {lane}/{member} {中文摘要}：{detail}`。
func TestSummaryText(t *testing.T) {
	assert.Equal(t, "[PBR] model-1/ch-a:model-1 熔断打开：open_seconds=60",
		summaryText(route.Event{Type: route.EventCircuitOpen, Lane: "model-1", Member: "ch-a:model-1", Detail: "open_seconds=60"}))
	assert.Equal(t, "[PBR] model-1/ch-a:model-1 进入冷却：cooldown_until=1789600060000",
		summaryText(route.Event{Type: route.EventCooldown, Lane: "model-1", Member: "ch-a:model-1", Detail: "cooldown_until=1789600060000"}))
	assert.Equal(t, "[PBR] ch-a:model-1 熔断恢复", // lane 缺失退化为 member
		summaryText(route.Event{Type: route.EventCircuitClosed, Member: "ch-a:model-1"}))
}

// 配置热更新：configLoader 的原始串变化后，下一次读取即拿到新目标。
func TestCurrentTargetsHotReload(t *testing.T) {
	restoreConfig(t)
	configLoader = func() string { return "" }
	configCache.mu.Lock()
	configCache.raw = "\x00sentinel" // 强制首轮重算
	configCache.mu.Unlock()

	assert.Empty(t, CurrentTargets())

	configLoader = func() string {
		return `[{"name":"notify","url":"http://127.0.0.1:8645/x","secret":"abcd1234","enabled":true,"events":["circuit_open"]}]`
	}
	targets := CurrentTargets()
	require.Len(t, targets, 1)
	assert.Equal(t, "notify", targets[0].Name)
	assert.True(t, targets[0].Enabled)
	assert.Equal(t, []string{"circuit_open"}, targets[0].Events)
}

// 测试投递：同步一次成功，事件体标注"测试事件"。
func TestDeliverTestSendsMarkedPayload(t *testing.T) {
	restoreConfig(t)
	setupDeliveryDB(t)

	var body []byte
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := DeliverTest(Target{Name: "notify", URL: server.URL, Secret: "s", Enabled: true})
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, 1, result.Attempts)
	assert.Equal(t, "application/json", contentType)

	var payload webhookPayload
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Equal(t, "pbr", payload.Type)
	assert.Contains(t, payload.Text, "测试事件")
	assert.Equal(t, route.EventCircuitOpen, payload.Event.Type)

	entries, err := model.ListWebhookDeliveries(10, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1, "测试投递也要记 deliveries")
	assert.Equal(t, "success", entries[0].Status)
	assert.Equal(t, 1, entries[0].Attempt)
}

// 投递隔离（design-v1 §16.10）：一个 target 挂起（首个响应悬挂直到放行）
// 不影响另一个 target 收到事件——慢目标的重试不阻塞快目标的投递。
func TestDispatchIsolatesSlowTarget(t *testing.T) {
	restoreConfig(t)
	setupDeliveryDB(t)

	release := make(chan struct{})
	var mu sync.Mutex
	var fastRequests int
	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fastRequests++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer fastServer.Close()
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // 挂起：慢目标进入重试周期
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()
	defer close(release) // LIFO：先放行慢目标，慢目标 goroutine 结束后 httptest 才能关服

	configLoader = func() string {
		raw, _ := json.Marshal([]Target{
			{Name: "slow", URL: slowServer.URL, Enabled: true},
			{Name: "fast", URL: fastServer.URL, Enabled: true},
		})
		return string(raw)
	}
	configCache.mu.Lock()
	configCache.raw = ""
	configCache.targets = nil
	configCache.mu.Unlock()

	dispatch(route.Event{Ts: 1, Type: route.EventCircuitOpen, Lane: "iso", Member: "1:m"})
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return fastRequests >= 1
	}, 2*time.Second, 10*time.Millisecond, "慢目标挂起时快目标必须仍收到投递")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, fastRequests)
}

// reset 事件（手动复通 circuits/reset）在推送白名单内；摘要为车道级
// （member 为空，退化为 [PBR] {title}：{detail}）。
func TestResetEventWhitelisted(t *testing.T) {
	restoreConfig(t)
	setupDeliveryDB(t)

	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	configLoader = func() string {
		raw, _ := json.Marshal([]Target{{Name: "notify", URL: server.URL, Enabled: true}})
		return string(raw)
	}
	configCache.mu.Lock()
	configCache.raw = ""
	configCache.targets = nil
	configCache.mu.Unlock()

	assert.Equal(t, "[PBR] model-1 熔断与冷却已清空：circuits cleared",
		summaryText(route.Event{Type: route.EventReset, Lane: "model-1", Detail: "circuits cleared"}))
	assert.NoError(t, ValidateTargets([]Target{{Name: "n", URL: server.URL, Events: []string{route.EventReset}}}))

	dispatch(route.Event{Ts: 1, Type: route.EventReset, Lane: "model-1", Member: "", Detail: "circuits cleared"})
	WaitPendingDeliveries()

	var payload webhookPayload
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Equal(t, "reset", payload.Event.Type)
	assert.Equal(t, "model-1", payload.Event.Lane)
	assert.Equal(t, "", payload.Event.Member)
	assert.Equal(t, "[PBR] model-1 熔断与冷却已清空：circuits cleared", payload.Text)

	entries, err := model.ListWebhookDeliveries(10, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "reset", entries[0].EventType)
}

// setOf 去重集合大小。
func setOf(items []string) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		out[item] = true
	}
	return out
}
