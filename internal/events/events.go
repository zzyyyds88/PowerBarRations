// Package events 定义 PBR 进程内事件通知的共享合同。
//
// 事件生产者（例如 internal/route 与 model）只依赖本包，不依赖 webhook
// 或其他上层适配器。订阅者回调必须自行完成非阻塞处理（通常是写入有界
// channel）；Publish 不持有订阅锁调用回调，因此回调可以安全地注销订阅。
package events

import (
	"sort"
	"sync"
)

const (
	// 路由运行态事件。
	EventCircuitOpen     = "circuit_open"
	EventCircuitHalfOpen = "circuit_half_open"
	EventCircuitClosed   = "circuit_closed"
	EventCooldown        = "cooldown"
	EventReset           = "reset"
	EventSkip            = "skip"

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
	EventCircuitOpen:     {},
	EventCircuitHalfOpen: {},
	EventCircuitClosed:   {},
	EventCooldown:        {},
	EventReset:           {},
	EventSkip:            {},
	EventChannelDisabled: {},
	EventChannelEnabled:  {},
	EventChannelDeleted:  {},
	EventUpstreamBurst:   {},
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
