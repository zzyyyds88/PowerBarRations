// Package events 定义 PBR 进程内事件通知的共享合同。
//
// 事件生产者（例如 internal/route 与 model）只依赖本包，不依赖 webhook
// 或其他上层适配器。订阅者回调必须自行完成非阻塞处理（通常是写入有界
// channel）；Publish 不持有订阅锁调用回调，因此回调可以安全地注销订阅。
package events

import (
	"log"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/zzyyyds88/PowerBarRations/internal/metrics"
)

// asyncQueueSize 是运行态事件异步分发的有界队列。事件是通知而不是业务数据，
// 队列满时丢弃并计数，绝不阻塞选路锁或请求路径。
const asyncQueueSize = 1024

const (
	// 路由运行态事件。
	EventCircuitOpen     = "circuit_open"
	EventCircuitHalfOpen = "circuit_half_open"
	EventCircuitClosed   = "circuit_closed"
	EventCooldown        = "cooldown"
	EventReset           = "reset"
	EventSkip            = "skip"
	// EventMemberPaced 限流减速开始（routing-spec §4.1.1）：429 只放慢该成员，
	// 不是成员故障；减速到期自动消失，不发恢复事件（与 EventCooldown 互斥）。
	EventMemberPaced = "member_paced"
	// EventMemberRecovered 成员恢复：冷却/失败留痕之后第一次成功服务（与 EventCooldown 配对，
	// routing-spec §7）。同一次失败也可能同时开了熔断：熔断由 EventCircuitClosed 收口，
	// 冷却仍由本条收口，两类"开始"各自有配对的"结束"。
	EventMemberRecovered = "member_recovered"
	// EventLaneUnavailable 车道不可用：某次请求因"全部成员不可选"按 503 收尾时发出一次
	// （同一停摆期不重复），与 EventLaneRecovered 配对。
	EventLaneUnavailable = "lane_unavailable"
	// EventLaneRecovered 车道恢复：不可用期后第一次成功服务。
	EventLaneRecovered = "lane_recovered"
	// EventMemberThrottled 出站准入未就绪（队列满/排队超时），routing-spec §3.2。
	EventMemberThrottled = "member_throttled"
	// EventAdaptiveLimitChanged AIMD 自适应上限变化，routing-spec §3.2。
	EventAdaptiveLimitChanged = "adaptive_limit_changed"

	// 渠道与上游健康事件。
	EventChannelDisabled = "channel_disabled"
	EventChannelEnabled  = "channel_enabled"
	EventChannelDeleted  = "channel_deleted"
	EventUpstreamBurst   = "upstream_burst"
)

// Event 是进程内事件以及 webhook 请求体中 event 对象的共享形状。
// Lane 对渠道级事件可以为空；Member 对车道级事件可以为空。
type Event struct {
	Ts     int64  `json:"ts"`
	Type   string `json:"type"`
	Lane   string `json:"lane,omitempty"`
	Member string `json:"member"`
	Detail string `json:"detail,omitempty"`
}

// supportedTypes 是管理面 webhook events 白名单的唯一来源。
// 切勿直接暴露可变 map；调用方应使用 IsSupportedType 或
// SupportedEventTypes。
var supportedTypes = map[string]struct{}{
	EventCircuitOpen:          {},
	EventCircuitHalfOpen:      {},
	EventCircuitClosed:        {},
	EventCooldown:             {},
	EventReset:                {},
	EventSkip:                 {},
	EventMemberPaced:          {},
	EventMemberRecovered:      {},
	EventLaneUnavailable:      {},
	EventLaneRecovered:        {},
	EventMemberThrottled:      {},
	EventAdaptiveLimitChanged: {},
	EventChannelDisabled:      {},
	EventChannelEnabled:       {},
	EventChannelDeleted:       {},
	EventUpstreamBurst:        {},
}

// SupportedEventTypes 返回 webhook events 白名单支持的事件类型。
// 返回新切片，调用方修改结果不会影响全局合同。
func SupportedEventTypes() []string {
	result := make([]string, 0, len(supportedTypes))
	for eventType := range supportedTypes {
		result = append(result, eventType)
	}
	sort.Strings(result)
	return result
}

// IsSupportedType 判断事件类型是否属于可配置的 webhook 事件域。
func IsSupportedType(eventType string) bool {
	_, ok := supportedTypes[eventType]
	return ok
}

// Subscriber 接收已发布事件。回调必须非阻塞；需要异步处理时应先写入
// 自己的有界队列，队列满时按消费方策略丢弃或计数。
type Subscriber func(Event)

var subscriber struct {
	mu sync.RWMutex
	fn Subscriber
}

// SetSubscriber 设置进程级事件订阅者；传 nil 注销。PBR 当前只允许一个
// 订阅者（Webhook 投递器），后一次设置会覆盖前一次设置。
func SetSubscriber(fn Subscriber) {
	subscriber.mu.Lock()
	subscriber.fn = fn
	subscriber.mu.Unlock()
}

// Publish 发布一条事件。读取订阅者后立即释放锁，再执行回调，以支持
// 回调中注销或替换订阅者；事件生产者不会因订阅者管理锁长时间等待。
func Publish(event Event) {
	subscriber.mu.RLock()
	fn := subscriber.fn
	subscriber.mu.RUnlock()
	if fn != nil {
		fn(event)
	}
}

var (
	asyncOnce    sync.Once
	asyncQueue   = make(chan Event, asyncQueueSize)
	asyncDropped atomic.Uint64
	asyncPanics  atomic.Uint64
)

// PublishAsync 把事件放入进程级有界队列后立即返回，由单个后台 goroutine
// 顺序调用订阅者。选路运行态在持锁时调用它：锁内只做一次非阻塞入队，
// 不会因为 webhook/未来订阅者变慢而阻塞同车道的其他请求。
func PublishAsync(event Event) {
	asyncOnce.Do(func() { go asyncDispatchLoop() })
	select {
	case asyncQueue <- event:
	default:
		asyncDropped.Add(1)
		metrics.ObserveRouteEventDrop()
	}
}

// AsyncDropped 返回异步事件因队列满被丢弃的累计数量（测试/诊断用）。
func AsyncDropped() uint64 { return asyncDropped.Load() }

// AsyncPanics 返回订阅者回调 panic 被兜住的次数；panic 不应杀掉分发循环。
func AsyncPanics() uint64 { return asyncPanics.Load() }

func asyncDispatchLoop() {
	for event := range asyncQueue {
		func() {
			defer func() {
				if r := recover(); r != nil {
					asyncPanics.Add(1)
					log.Printf("pbr events: subscriber panic recovered: %v", r)
				}
			}()
			Publish(event)
		}()
	}
}
