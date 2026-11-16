// Package metrics 是 PBR 的 Prometheus 指标导出实现（docs/design-v1.md §8、
// docs/api-spec-v1.md §5.1）。
//
// 两条硬约束：
//   - 抓取只读进程内计数器与调用方推入的 gauge 快照，**不查数据库**；
//   - 标签只允许低基数维度（model / lane / channel / status / error_kind），
//     不得把请求 ID、客户端密钥、上游地址等放进标签。
//
// 路由注册与鉴权（GET /metrics 复用管理密钥 Bearer）由 G5 接线；本包只提供
// http.Handler 与两个更新入口。
package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// 标签缺省值。空标签虽然合法，但无法区分"没有该维度"和"维度为空"，
// 统一归一为 unknown，避免产生一批名字为空的指标序列。
const labelUnknown = "unknown"

// 令牌计数分项（pbr_tokens_total 的 kind 标签取值）。
const (
	tokenKindPrompt     = "prompt"
	tokenKindCompletion = "completion"
	tokenKindCacheRead  = "cache_read"
	tokenKindCacheWrite = "cache_write"
	tokenKindReasoning  = "reasoning"
)

// 熔断状态（pbr_circuit_state 的取值口径：closed=0、half_open=1、open=2）。
const (
	circuitStateClosed   = "closed"
	circuitStateHalfOpen = "half_open"
	circuitStateOpen     = "open"
)

var (
	// 请求耗时覆盖从秒级到十分钟级的完整分布；LLM 长响应远超默认 10s 上限。
	requestDurationBuckets = []float64{
		0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300, 600,
	}
	// 首 token 延迟通常远小于总耗时，用更密的低区间。
	ttftBuckets = []float64{
		0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60,
	}
)

// RequestObservation 是一次模型面请求结束时的观测快照。
// 所有字段都由调用方算好，ObserveRequest 不做推导、不查库；
// 一次模型面请求只能调用一次（requests_total 以请求为单位计数）。
type RequestObservation struct {
	Model   string
	Lane    string
	Channel string
	// Status 是最终返回给客户端的 HTTP 状态码；<=0 时按 unknown 记录。
	Status int
	// Duration 是请求总耗时；<=0 表示未观测，不计入直方图。
	Duration time.Duration
	// TTFT 是首 token 延迟；<=0 表示非流式或未观测，不计入直方图。
	TTFT time.Duration

	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  int
	CacheWriteTokens int
	ReasoningTokens  int

	// Cost 是本次请求的折算成本（0 表示渠道未配价，不计入 cost_total）。
	Cost float64
	// ErrorKind 是上游失败分类（routing-spec §4.1，如 soft_transient）。
	// 仅在上游侧失败时填写；成功或客户端错误留空。
	ErrorKind string
}

// metricsSet 是一组独立注册的指标集合。
//
// 生产使用包级默认实例；测试用 newMetrics 构造隔离实例，避免用例之间
// 互相污染计数。
type metricsSet struct {
	registry *prometheus.Registry

	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	ttft            *prometheus.HistogramVec
	tokensTotal     *prometheus.CounterVec
	costTotal       *prometheus.CounterVec
	upstreamErrors  *prometheus.CounterVec
	circuitState    *prometheus.GaugeVec
	channelUp       *prometheus.GaugeVec
}

// defaultMetrics 是进程级默认指标集合；所有包级入口都转发到它。
var defaultMetrics = newMetrics()

// newMetrics 构造一套带独立 registry 的指标集合（含 Go runtime 与进程指标）。
func newMetrics() *metricsSet {
	registry := prometheus.NewRegistry()
	m := &metricsSet{
		registry: registry,
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pbr",
			Name:      "requests_total",
			Help:      "模型面请求总数（按最终结果计）。",
		}, []string{"model", "lane", "channel", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "pbr",
			Name:      "request_duration_seconds",
			Help:      "模型面请求总耗时（秒）。",
			Buckets:   requestDurationBuckets,
		}, []string{"model"}),
		ttft: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "pbr",
			Name:      "ttft_seconds",
			Help:      "流式请求首 token 延迟（秒）。",
			Buckets:   ttftBuckets,
		}, []string{"model"}),
		tokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pbr",
			Name:      "tokens_total",
			Help:      "令牌用量（按 model 与 kind 分项）。",
		}, []string{"model", "kind"}),
		costTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pbr",
			Name:      "cost_total",
			Help:      "按渠道单价折算的估算成本。",
		}, []string{"model", "channel"}),
		upstreamErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pbr",
			Name:      "upstream_errors_total",
			Help:      "上游失败次数（按错误分类）。",
		}, []string{"kind"}),
		circuitState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "pbr",
			Name:      "circuit_state",
			Help:      "渠道熔断状态：0=closed，1=half_open，2=open。",
		}, []string{"channel"}),
		channelUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "pbr",
			Name:      "channel_up",
			Help:      "渠道当前是否可用：1=up，0=down。",
		}, []string{"channel"}),
	}

	// 注册的 collector 全部是编译期常量构造，重复注册只可能是改错指标名，
	// 属于编程错误而非可恢复运行故障，因此沿用 client_golang 的 MustRegister。
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requestsTotal,
		m.requestDuration,
		m.ttft,
		m.tokensTotal,
		m.costTotal,
		m.upstreamErrors,
		m.circuitState,
		m.channelUp,
	)
	return m
}

// Handler 返回默认指标集合的 Prometheus 文本格式处理器。
func Handler() http.Handler {
	return defaultMetrics.handler()
}

// handler 返回该指标集合的 Prometheus 文本格式处理器。
func (m *metricsSet) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.HTTPErrorOnError,
	})
}

// ObserveRequest 用一次请求的终态更新默认指标集合。
func ObserveRequest(obs RequestObservation) {
	defaultMetrics.observeRequest(obs)
}

// observeRequest 记录一次模型面请求的终态。
//
// 计数口径：
//   - requests_total 每个请求递增一次；
//   - duration / ttft 仅在大于 0 时记入直方图（非流式请求没有 TTFT）；
//   - tokens 仅记录大于 0 的分项，避免为每个请求生成全部五个 kind 的零值序列；
//   - cost 仅在大于 0 时累加（渠道未配价不产生序列）；
//   - error_kind 非空时计入 upstream_errors_total。
func (m *metricsSet) observeRequest(obs RequestObservation) {
	model := labelValue(obs.Model)
	lane := labelValue(obs.Lane)
	channel := labelValue(obs.Channel)

	m.requestsTotal.WithLabelValues(model, lane, channel, statusValue(obs.Status)).Inc()

	if obs.Duration > 0 {
		m.requestDuration.WithLabelValues(model).Observe(obs.Duration.Seconds())
	}
	if obs.TTFT > 0 {
		m.ttft.WithLabelValues(model).Observe(obs.TTFT.Seconds())
	}

	m.observeTokens(model, tokenKindPrompt, obs.PromptTokens)
	m.observeTokens(model, tokenKindCompletion, obs.CompletionTokens)
	m.observeTokens(model, tokenKindCacheRead, obs.CacheReadTokens)
	m.observeTokens(model, tokenKindCacheWrite, obs.CacheWriteTokens)
	m.observeTokens(model, tokenKindReasoning, obs.ReasoningTokens)

	if obs.Cost > 0 {
		m.costTotal.WithLabelValues(model, channel).Add(obs.Cost)
	}

	if kind := strings.TrimSpace(obs.ErrorKind); kind != "" {
		m.upstreamErrors.WithLabelValues(kind).Inc()
	}
}

// observeTokens 只累加正向分项；token 数不可能为负，<=0 视为未提供。
func (m *metricsSet) observeTokens(model, kind string, count int) {
	if count > 0 {
		m.tokensTotal.WithLabelValues(model, kind).Add(float64(count))
	}
}

// SetChannelState 用默认指标集合更新渠道的熔断与可用状态快照。
func SetChannelState(channel, circuitState string, up bool) {
	defaultMetrics.setChannelState(channel, circuitState, up)
}

// setChannelState 记录路由运行态推入的渠道快照（抓取时不再回溯运行态或查库）。
//
// circuitState 只接受 closed / half_open / open；未知取值不更新熔断 gauge
// （宁可不写，也不伪造一个状态），但 channel_up 仍会同步。channel 为空时
// 整条快照忽略，避免产生无身份的空标签序列。
func (m *metricsSet) setChannelState(channel, circuitState string, up bool) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return
	}
	if value, ok := circuitStateValue(circuitState); ok {
		m.circuitState.WithLabelValues(channel).Set(value)
	}
	m.channelUp.WithLabelValues(channel).Set(boolValue(up))
}

// circuitStateValue 把熔断状态名映射为 gauge 数值。
func circuitStateValue(state string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case circuitStateClosed:
		return 0, true
	case circuitStateHalfOpen:
		return 1, true
	case circuitStateOpen:
		return 2, true
	default:
		return 0, false
	}
}

// boolValue 把布尔状态映射为 0/1。
func boolValue(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

// labelValue 归一化标签值：空值统一为 unknown，保持序列可读且稳定。
func labelValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return labelUnknown
	}
	return v
}

// statusValue 把 HTTP 状态码转成标签值；非正数按 unknown 记录。
func statusValue(status int) string {
	if status <= 0 {
		return labelUnknown
	}
	return strconv.Itoa(status)
}
