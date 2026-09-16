// Package webhook 实现运行态事件的出站 webhook 推送
// （design-v1 §16.10，api-spec §5.8，/doc 手册 §5）。
//
// 职责边界：PBR 只定义推送契约并投递；接收方的验签、路由、呈现由消费方
// 自行实现，本包不含任何针对特定接收端的集成。
//
// 事件流：internal/route 的 appendEvent（运行态事件唯一出口）→ 进程级订阅
// 钩子 → 带缓冲 channel（非阻塞，缓冲满丢弃并计数）→ worker 读配置
// （system/options 键 PBRWebhookTargets，进程内缓存、配置 PUT 即热更新）
// → 对每个 enabled 且命中 events 白名单的 target 投递。
//
// 投递语义：8s 超时；非 2xx/超时按 5s/30s/120s 退避重试 3 次（同一请求体
// 同一签名）；耗尽记 webhook_deliveries 死信；同一 (target, lane, member,
// event) 60s 窗口只发一条（防风暴）。
package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pbr/common"
	"pbr/internal/route"
	"pbr/model"
)

// OptionKey 是 webhook 目标配置的 system/options 键，值为 Target 数组的 JSON
// （design-v1 §16.10）。写入只经管理面 PUT /api/webhooks（校验后落库）。
const OptionKey = "PBRWebhookTargets"

// Target 一个投递目标。
type Target struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Secret  string `json:"secret"`
	Enabled bool   `json:"enabled"`
	// Events 事件类型白名单；空 = 全部（api-spec §5.8）。
	Events []string `json:"events"`
}

// allowedEventTypes 事件类型白名单取值域（api-spec §5.8）。
var allowedEventTypes = map[string]bool{
	route.EventCircuitOpen:     true,
	route.EventCircuitHalfOpen: true,
	route.EventCircuitClosed:   true,
	route.EventCooldown:        true,
}

// ValidateTargets 校验目标列表：name 非空且唯一、url 必须是 http(s)、
// events 取值必须在白名单内。返回供 400 validation_failed 使用的人读信息。
func ValidateTargets(targets []Target) error {
	seen := map[string]bool{}
	for i := range targets {
		t := &targets[i]
		t.Name = strings.TrimSpace(t.Name)
		t.URL = strings.TrimSpace(t.URL)
		if t.Name == "" {
			return fmt.Errorf("targets[%d].name is required", i)
		}
		if seen[t.Name] {
			return fmt.Errorf("targets[%d].name %q is duplicated", i, t.Name)
		}
		seen[t.Name] = true
		parsed, err := url.Parse(t.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("targets[%d].url %q must be a valid http(s) URL", i, t.URL)
		}
		for j, ev := range t.Events {
			t.Events[j] = strings.TrimSpace(ev)
			if !allowedEventTypes[t.Events[j]] {
				return fmt.Errorf("targets[%d].events[%d] %q is not a supported event type", i, j, ev)
			}
		}
	}
	return nil
}

// MaskSecret secret 回显掩码（api-spec §5.8）：`****` + 末 4 位；不足 8 字符
// 全掩码；空串原样返回空串。
func MaskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) < 8 {
		return "****"
	}
	return "****" + secret[len(secret)-4:]
}

// configLoader 抽出来供测试注入；生产实现读 common.OptionMap。
var configLoader = func() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[OptionKey]
}

// configCache 缓存解析结果：appendEvent 高频触发时不能每次都做 JSON 解析。
// 以原始字符串为键，配置 PUT 后（UpdateOption 同步内存）下一次读取即生效。
var configCache struct {
	mu      sync.Mutex
	raw     string
	targets []Target
}

// CurrentTargets 读取当前生效的目标配置（带缓存；调用方只读不改）。
func CurrentTargets() []Target {
	raw := configLoader()
	configCache.mu.Lock()
	defer configCache.mu.Unlock()
	if raw != configCache.raw {
		configCache.targets = parseTargets(raw)
		configCache.raw = raw
	}
	return configCache.targets
}

func parseTargets(raw string) []Target {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var targets []Target
	if err := json.Unmarshal([]byte(raw), &targets); err != nil {
		common.SysError("[pbr] webhook: invalid " + OptionKey + " config, ignoring: " + err.Error())
		return nil
	}
	return targets
}

// --- 请求体与签名（api-spec §5.8 的请求体与验签契约） ---

// webhookEvent 事件对象；ts 为毫秒时间戳，member 形如 channelId:upstreamModel。
type webhookEvent struct {
	Ts     int64  `json:"ts"`
	Type   string `json:"type"`
	Lane   string `json:"lane,omitempty"`
	Member string `json:"member"`
	Detail string `json:"detail,omitempty"`
}

// webhookPayload 外层请求体；type 固定 "pbr"，text 为人类可读摘要。
type webhookPayload struct {
	Type  string       `json:"type"`
	Text  string       `json:"text"`
	Event webhookEvent `json:"event"`
}

// eventTitles 事件类型的中文摘要（/doc §5.2 文案）。
var eventTitles = map[string]string{
	route.EventCircuitOpen:     "熔断打开",
	route.EventCircuitHalfOpen: "半开探测开始",
	route.EventCircuitClosed:   "熔断恢复",
	route.EventCooldown:        "进入冷却",
}

// summaryText 生成 text 摘要：`[PBR] {lane}/{member} {中文摘要}：{detail}`；
// lane 缺失时退化为 `[PBR] {member} ...`（/doc §5.2 示例格式）。
func summaryText(ev route.Event) string {
	title := eventTitles[ev.Type]
	if title == "" {
		title = ev.Type
	}
	subject := ev.Member
	if ev.Lane != "" {
		subject = ev.Lane + "/" + ev.Member
	}
	switch {
	case subject == "":
		return fmt.Sprintf("[PBR] %s：%s", title, ev.Detail)
	case ev.Detail == "":
		return fmt.Sprintf("[PBR] %s %s", subject, title)
	default:
		return fmt.Sprintf("[PBR] %s %s：%s", subject, title, ev.Detail)
	}
}

// SignPayload 计算 `X-Webhook-Signature-V2`：hex(HMAC-SHA256(secret, "{ts}.{raw_body}"))。
func SignPayload(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// --- 投递器 ---

const (
	// deliveryTimeout 单次投递超时（api-spec §5.4）。
	deliveryTimeout = 8 * time.Second
	// maxAttempts 首次 + 3 次重试。
	maxAttempts = 1 + 3
	// eventQueueSize 事件缓冲；满了丢弃并计数（design-v1 §16.10 非阻塞）。
	eventQueueSize = 256
	// stormWindow 同一 (target, lane, member, event) 的合并窗口。
	stormWindow = 60 * time.Second
	// maxStormEntries 防风暴表的容量上限，超过即做一次过期清扫。
	maxStormEntries = 4096
	// maxErrorLen 落库错误信息的截断长度。
	maxErrorLen = 500
)

// retryDelays 重试退避（5s/30s/120s）；包变量供测试注入短退避。
var retryDelays = []time.Duration{5 * time.Second, 30 * time.Second, 120 * time.Second}

// nowFunc 时钟；供测试注入。
var nowFunc = time.Now

var httpClient = &http.Client{Timeout: deliveryTimeout}

// Result 一次投递的最终结论（test 端点回传给调用方）。
type Result struct {
	Status     string `json:"status"` // success | failed
	HTTPStatus int    `json:"http_status"`
	Error      string `json:"error"`
	Attempts   int    `json:"attempts"`
}

// eventQueue 运行态事件的缓冲队列；Start 时创建。
var eventQueue chan route.Event

// droppedEvents 缓冲满被丢弃的事件计数（排障观测）。
var droppedEvents atomic.Int64

var (
	startOnce sync.Once
)

// Start 注册事件订阅钩子并启动投递 worker；幂等。
// 在 model.InitDB / InitOptionMap 之后调用（worker 读写 options 与投递日志）。
func Start() {
	startOnce.Do(func() {
		eventQueue = make(chan route.Event, eventQueueSize)
		route.SetEventSubscriber(enqueueEvent)
		go workerLoop()
	})
}

// enqueueEvent 订阅钩子：只收白名单内的运行态事件，非阻塞入队，
// 缓冲满丢弃并计数（绝不阻塞请求路径）。
func enqueueEvent(ev route.Event) {
	if !allowedEventTypes[ev.Type] {
		return
	}
	queue := eventQueue
	if queue == nil {
		return
	}
	select {
	case queue <- ev:
	default:
		droppedEvents.Add(1)
	}
}

func workerLoop() {
	for ev := range eventQueue {
		runSafely(func() { dispatch(ev) })
	}
}

// runSafely worker 单事件隔离 panic：一个坏事件不能杀死投递循环。
func runSafely(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("[pbr] webhook: dispatch panic recovered: %v", r))
		}
	}()
	fn()
}

// dispatch 把事件投递给所有 enabled 且命中白名单的 target。
func dispatch(ev route.Event) {
	for _, target := range CurrentTargets() {
		if !target.Enabled || !targetMatches(target, ev) {
			continue
		}
		if !stormAllow(stormKey(target.Name, ev), nowFunc()) {
			continue
		}
		deliver(target, ev, summaryText(ev))
	}
}

func targetMatches(t Target, ev route.Event) bool {
	if len(t.Events) == 0 {
		return true // 空白名单 = 全部
	}
	for _, evType := range t.Events {
		if evType == ev.Type {
			return true
		}
	}
	return false
}

// --- 防风暴（60s 窗口合并） ---

var (
	stormMu   sync.Mutex
	stormLast = map[string]int64{} // key -> 上次放行的 UnixNano
)

func stormKey(target string, ev route.Event) string {
	return target + "\x1f" + ev.Lane + "\x1f" + ev.Member + "\x1f" + ev.Type
}

// stormAllow 同一 key 在 60s 窗口内只放行一条；窗口内后续同类事件直接丢弃
// （design-v1 §16.10：v1 不做合并计数文本）。
func stormAllow(key string, now time.Time) bool {
	cutoff := now.Add(-stormWindow).UnixNano()
	stormMu.Lock()
	defer stormMu.Unlock()
	if len(stormLast) >= maxStormEntries {
		for k, ts := range stormLast {
			if ts <= cutoff {
				delete(stormLast, k)
			}
		}
	}
	if last, ok := stormLast[key]; ok && last > cutoff {
		return false
	}
	stormLast[key] = now.UnixNano()
	return true
}

// --- 单目标投递（含重试与死信） ---

// deliver 向 target 投递事件：请求体 marshaled 一次，时间戳与签名只算一次
// （重试复用同一请求体同一签名，api-spec §5.4）。每次投递的最终结果（含
// 死信）落 webhook_deliveries。
func deliver(target Target, ev route.Event, text string) Result {
	payload := webhookPayload{
		Type: "pbr",
		Text: text,
		Event: webhookEvent{
			Ts:     ev.Ts,
			Type:   ev.Type,
			Lane:   ev.Lane,
			Member: ev.Member,
			Detail: ev.Detail,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil { // 结构固定，理论不可达；防御性兜底
		result := Result{Status: "failed", Error: "marshal payload: " + err.Error(), Attempts: 0}
		return result
	}
	ts := nowFunc().Unix()
	signature := SignPayload(target.Secret, ts, body)

	var lastStatus int
	var lastErr string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(retryDelays[attempt-2])
		}
		status, err := postWebhook(target, body, ts, signature)
		if err == nil && status >= 200 && status < 300 {
			result := Result{Status: "success", HTTPStatus: status, Attempts: attempt}
			recordDelivery(ev, target, result)
			return result
		}
		if err != nil {
			lastErr = err.Error()
		} else {
			lastErr = fmt.Sprintf("unexpected status %d", status)
		}
		lastStatus = status
	}
	result := Result{Status: "failed", HTTPStatus: lastStatus, Error: clampError(lastErr), Attempts: maxAttempts}
	recordDelivery(ev, target, result)
	return result
}

// postWebhook 发一次投递请求；传输层失败返回 (0, err)，其余返回状态码
// （非 2xx 不是 error，由调用方按状态码进入重试）。
func postWebhook(target Target, body []byte, ts int64, signature string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Webhook-Signature-V2", signature)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

func clampError(msg string) string {
	if len(msg) > maxErrorLen {
		return msg[:maxErrorLen]
	}
	return msg
}

// recordDelivery 投递结论落库（含重试耗尽的死信与测试投递）。
func recordDelivery(ev route.Event, target Target, result Result) {
	entry := &model.WebhookDelivery{
		Ts:         nowFunc().Unix(),
		Target:     target.Name,
		EventType:  ev.Type,
		Lane:       ev.Lane,
		Member:     ev.Member,
		Status:     result.Status,
		HTTPStatus: result.HTTPStatus,
		Error:      result.Error,
		Attempt:    result.Attempts,
	}
	if err := model.InsertWebhookDelivery(entry); err != nil {
		common.SysError("[pbr] webhook: failed to record delivery: " + err.Error())
	}
}

// --- 测试投递（POST /api/webhooks/test 的实现） ---

// DeliverTest 向 target 同步发送一条测试事件（event.type = circuit_open，
// text 标注"测试事件"），同步等待最终结果（含重试）。测试投递是显式人工
// 动作，不走防风暴窗口，但同样落投递日志。
func DeliverTest(target Target) Result {
	ev := route.Event{
		Ts:     nowFunc().UnixMilli(),
		Type:   route.EventCircuitOpen,
		Detail: "manual_test",
	}
	text := fmt.Sprintf("[PBR] 测试事件：target=%s 配置验证", target.Name)
	return deliver(target, ev, text)
}
