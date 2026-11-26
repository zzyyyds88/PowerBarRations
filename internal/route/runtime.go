package route

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/events"
	"github.com/zzyyyds88/PowerBarRations/model"
)

// 本文件实现成员级的运行态：冷却、熔断三态（含半开与指数退避）、亲和、单探测槽。
//
// 语义依据：docs/routing-spec-v1.md §5（熔断）、§6（冷却与亲和）、§2（选择算法）。
// 运行态是**进程内**的，重启清空（routing-spec §1.3）——README 必须写明。
//
// 冷却与熔断共同决定"该成员当前是否可选"（§5.3）：
//
//	可选 = 不在冷却 && 熔断 != open
//
// 因此本文件只暴露一个可用性判断供四种模式共用，不允许多套逻辑并行。

const (
	CircuitClosed   = "closed"
	CircuitOpen     = "open"
	CircuitHalfOpen = "half_open"

	// 运行态事件的类型，供控制台与排障展示（带时间戳）。
	EventCircuitOpen     = events.EventCircuitOpen
	EventCircuitHalfOpen = events.EventCircuitHalfOpen
	EventCircuitClosed   = events.EventCircuitClosed
	EventCooldown        = events.EventCooldown
	EventReset           = events.EventReset
	EventSkip            = events.EventSkip
	EventUpstreamBurst   = events.EventUpstreamBurst
	// EventAdaptiveLimitChanged AIMD 上限变化（routing-spec §3.2）。
	EventAdaptiveLimitChanged = events.EventAdaptiveLimitChanged
	EventMemberPaced          = events.EventMemberPaced
	EventMemberRecovered      = events.EventMemberRecovered
	EventLaneUnavailable      = events.EventLaneUnavailable
	EventLaneRecovered        = events.EventLaneRecovered
)

// 故障权重：硬故障计数权重高、软故障（429）权重低，
// 使持续限流不会像硬故障那样快速打开熔断（design-v1 §7.5）。
const (
	weightHard   = 1.0
	weightSoft   = 0.6
	weightLimit  = 0.2
	weightOther  = 0.5
	openBackoffK = 2 // 重复打开时的退避倍数：open_seconds × 2^k

	// hardFailureCooldownMultiplier 硬鉴权/欠费类失败的冷却倍率：
	// routing-spec §4.1 要求 hard_auth「冷却取较长档」，因为凭据失效不会在
	// 秒级自愈，按软故障时长回避只会让请求反复撞墙。
	hardFailureCooldownMultiplier = 2

	// 连续 upstream_reject 的短期回避（routing-spec §4.1）：上游持续拒绝
	// （通常是渠道配置/必需的会话头不对）时，避免每个请求都先白打它一次。
	rejectStreakThreshold = 5
	rejectCooldownSeconds = 300

	// maxMemberCooldownSeconds 单次冷却上限（1 小时）。Retry-After 异常大或
	// 配置失误时兜底，避免成员被长期踢出选路。
	maxMemberCooldownSeconds = 3600

	// softTransientMinCooldownSeconds 软瞬时故障分级冷却的下限：抖动时只歇这么久，
	// 别把一个"刚打了个喷嚏"的成员当成"病了"。见 recordFailure 里分级那段。
	softTransientMinCooldownSeconds = 5

	// rateLimitPaceSeconds 上游 429 未给 Retry-After 时的默认减速时长（routing-spec §4.1.1）。
	rateLimitPaceSeconds = 3
	// rateLimitMaxPaceSeconds 减速时长上限；Retry-After 超过它时不等待，直接透传 429。
	rateLimitMaxPaceSeconds = 60
)

// jitterSeconds 给冷却叠加 0–20% 的上偏抖动，用 key+now 做确定性散列。
//
// 只上偏：上游 Retry-After 是"至少等这么久"，下偏会提前再撞限流。
// 不引入 math/rand 全局源，避免与并发请求争锁。
func jitterSeconds(base int, key MemberKey, now int64) int {
	if base <= 1 {
		return base
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key.String()))
	_, _ = h.Write([]byte(strconv.FormatInt(now, 10)))
	delta := int(h.Sum32() % uint32(base/5+1))
	return base + delta
}

// cooldownSecondsFor 按错误分类决定冷却时长：硬鉴权/欠费取 base 的较长档。
func cooldownSecondsFor(kind ErrorKind, base int) int {
	if base <= 0 {
		return base
	}
	switch kind {
	case KindHardAuth, KindHardQuota:
		return base * hardFailureCooldownMultiplier
	default:
		return base
	}
}

// CircuitSettings 熔断参数（经 system/options 配置，见 route.ConfigureCircuit）。
type CircuitSettings struct {
	// FailureThreshold 累计失败分数达到该值即打开。
	FailureThreshold float64
	// OpenSeconds 打开后的回避时长；<=0 表示取该成员的冷却时长。
	OpenSeconds int
	// MaxOpenSeconds 指数退避的时长上限。
	MaxOpenSeconds int
	// RollingMinSamples 滚动窗口触发开断所需的最小样本数。
	RollingMinSamples int
	// RollingFailureRate 滚动窗口失败率达到该值即视为持续失败（0 < rate <= 1）。
	RollingFailureRate float64
	// HalfOpenSuccesses 半开探测需要连续成功几次才恢复 closed（默认 2）。
	// 单次成功就恢复会让抖动上游在"恢复→再熔断"之间反复横跳。
	HalfOpenSuccesses int
	// AllowedFails 按错误类型的"连续失败达到几次即打开熔断"。
	// 与累计分阈值、滚动失败率取或；未配置的类型不单独触发。
	AllowedFails map[ErrorKind]int
	// RejectStreakThreshold 连续"配置可疑" upstream_reject 达到该次数时写短期冷却。
	RejectStreakThreshold int
	// RejectCooldownSeconds 连续拒绝冷却时长（秒）。
	RejectCooldownSeconds int
}

// 熔断参数的 system/options 键（design-v1 §7.5：阈值与时长经 system/options 配置）。
const (
	OptionCircuitFailureThreshold = "PBRCircuitFailureThreshold"
	OptionCircuitOpenSeconds      = "PBRCircuitOpenSeconds"
	OptionCircuitMaxOpenSeconds   = "PBRCircuitMaxOpenSeconds"
	// OptionCircuitRollingMinSamples / OptionCircuitRollingFailureRate 滚动窗口失败率证据
	// （design-v1 §7.5：阈值经 system/options 可配）。
	OptionCircuitRollingMinSamples  = "PBRCircuitRollingMinSamples"
	OptionCircuitRollingFailureRate = "PBRCircuitRollingFailureRate"
	OptionCircuitHalfOpenSuccesses  = "PBRCircuitHalfOpenSuccesses"
	OptionCircuitAllowedFails       = "PBRCircuitAllowedFails"
	// OptionRejectStreakThreshold / OptionRejectCooldownSeconds 连续配置可疑拒绝的
	// 阈值与冷却时长（routing-spec §4.1.3）。
	OptionRejectStreakThreshold = "PBRRejectStreakThreshold"
	OptionRejectCooldownSeconds = "PBRRejectCooldownSeconds"
	// OptionLogRetentionDays 明细日志保留天数（design-v1 §16.5，默认 30）。
	// 不引入 cron：仅作配置，实际清理由外部调 POST /logs/prune 触发。
	OptionLogRetentionDays = "PBRLogRetentionDays"
	// OptionProbeConcurrency 探活并发上限（design-v1 §16.6，默认 4）：
	// 避免一次性对上游造成突发压力。
	OptionProbeConcurrency           = "PBRProbeConcurrency"
	OptionUpstreamBurstThreshold     = "PBRUpstreamBurstThreshold"
	OptionUpstreamBurstWindowSeconds = "PBRUpstreamBurstWindowSeconds"
)

// DefaultLogRetentionDays 明细日志默认保留天数（design-v1 §16.5）。
const DefaultLogRetentionDays = 30

// CurrentLogRetentionDays 当前生效的明细保留天数；非法/缺失回落到默认值。
// 读取 common.OptionMap 而不是 system/options 的 HTTP 层，供后台清理任务复用。
func CurrentLogRetentionDays() int {
	common.OptionMapRWMutex.RLock()
	raw, ok := common.OptionMap[OptionLogRetentionDays]
	common.OptionMapRWMutex.RUnlock()
	if !ok {
		return DefaultLogRetentionDays
	}
	days, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || days <= 0 {
		return DefaultLogRetentionDays
	}
	return days
}

// DefaultProbeConcurrency 探活默认并发上限（design-v1 §16.6）。
const DefaultProbeConcurrency = 4

// MaxProbeConcurrency 探活并发硬上限：探活是对上游的突发流量，配置再大也不允许
// 无界并发（design-v1 §16.6）。超界值在读取时钳制，写入侧返回 400。
const MaxProbeConcurrency = 16

// 熔断时长/样本的硬上界：指数退避是 open_seconds × 2^k，若不设上界，
// 配置值接近 int64 上限时乘法会在比较前溢出成负数（反而得到极短冷却）。
// 7 天对单节点网关已是"永久"语义；rolling 样本上限 20 避免窗口无限增长。
const (
	MaxCircuitOpenSeconds       = 7 * 24 * 3600
	MaxCircuitRollingMinSamples = 20
)

// DefaultCircuitSettings 默认值。累计阈值取 5：单次请求无论内部重试几次只记一次
// 失败分（硬故障 1.0 / 软故障 0.6），因此两次硬故障不再立即熔断；429 权重 0.2，
// 单靠累计分需要 25 次。另有滚动窗口失败率证据（默认 rollingFailureRateThreshold，
// 可经 PBRCircuitRollingFailureRate/PBRCircuitRollingMinSamples 调整），两条取或。
func DefaultCircuitSettings() CircuitSettings {
	return CircuitSettings{
		FailureThreshold:      5,
		OpenSeconds:           0,
		MaxOpenSeconds:        1800,
		RollingMinSamples:     rollingMinSamples,
		RollingFailureRate:    rollingFailureRateThreshold,
		HalfOpenSuccesses:     2,
		AllowedFails:          defaultAllowedFails(),
		RejectStreakThreshold: rejectStreakThreshold,
		RejectCooldownSeconds: rejectCooldownSeconds,
	}
}

// defaultAllowedFails 按错误类型的连续失败容忍次数。
// hard_auth/hard_quota 不应反复撞墙；429 是软限流，给更大容忍度。
func defaultAllowedFails() map[ErrorKind]int {
	return map[ErrorKind]int{
		KindHardAuth:      2,
		KindHardQuota:     2,
		KindSoftTransient: 4,
		KindTimeout:       4,
		KindSoftRateLimit: 10,
		KindBadResponse:   3,
	}
}

// IsAllowedFailKind 判断错误类型是否可作为 allowed_fails 的配置键。
func IsAllowedFailKind(kind string) bool {
	switch ErrorKind(kind) {
	case KindHardAuth, KindHardQuota, KindSoftTransient, KindTimeout, KindSoftRateLimit, KindBadResponse:
		return true
	default:
		return false
	}
}

// ParseAllowedFails 解析 allowed_fails 选项（JSON 对象，键为错误类型）。
// 空串返回默认值；未知键或非正数返回错误，避免静默忽略配置失误。
func ParseAllowedFails(raw string) (map[ErrorKind]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultAllowedFails(), nil
	}
	var parsed map[string]int
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	out := make(map[ErrorKind]int, len(parsed))
	for kind, value := range parsed {
		if !IsAllowedFailKind(kind) {
			return nil, fmt.Errorf("unknown allowed_fails kind %q", kind)
		}
		if value <= 0 {
			return nil, fmt.Errorf("allowed_fails[%s] must be > 0", kind)
		}
		out[ErrorKind(kind)] = value
	}
	return out, nil
}

// EncodeAllowedFails 把按错误类型的容忍次数编码为选项值。
func EncodeAllowedFails(values map[ErrorKind]int) string {
	if len(values) == 0 {
		return ""
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}

// Circuit 成员熔断器状态。
type Circuit struct {
	State               string    `json:"state"`
	Score               float64   `json:"score"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	OpenCount           int       `json:"open_count"`
	OpenUntil           int64     `json:"open_until"`
	OpenedAt            int64     `json:"opened_at"`
	LastErrorKind       ErrorKind `json:"last_error_kind,omitempty"`
	// LastErrorAt 最近一次失败的时间（Unix ms，0 = 从未失败）。
	// 与 LastErrorKind 一起是"历史留痕"，不代表当前有故障（api-spec §6.5）。
	LastErrorAt      int64 `json:"last_error_at,omitempty"`
	LastTransitionAt int64 `json:"last_transition_at"`
	// RecentOutcomes 定长环形缓冲的最近结果（true=成功），用于计算
	// rolling_success_rate（routing-spec §7）。只保留最近 rollingWindowSize 次。
	RecentOutcomes []bool `json:"-"`
	// HalfOpenSuccesses 当前半开周期内已连续成功的次数（用于多探针恢复）。
	HalfOpenSuccesses int `json:"-"`
	// RejectStreak 连续"配置可疑" upstream_reject 次数（routing-spec §4.1.3）。
	// 任何非 upstream_reject 的结果（成功、其他失败分类、429 减速）以及请求级
	// 拒绝都会清零；只有无法识别的拒绝才累加。
	RejectStreak int `json:"-"`
	// FailsByKind 各错误类型的连续失败次数（严格连续：记录其他类型时清零）。
	FailsByKind map[ErrorKind]int `json:"-"`
}

// rollingWindowSize 滚动成功率的窗口大小（定长环形缓冲，避免无界增长）。
const rollingWindowSize = 20

// 滚动窗口失败率触发开断的保守规则（design-v1 §7.5：除累计失败计分外，
// "滚动窗口失败率"也是触发证据之一；文档未给具体阈值，故取"样本足够 +
// 失败率很高"双条件，既能让持续不可用开断，又不被个位数抖动误触发）：
//
//   - 窗口内至少 rollingMinSamples 个样本，避免一两次失败就凭比率开断；
//   - 失败率 >= rollingFailureRateThreshold（0.8），即最近 8~10 个结果里最多
//     1~2 个成功，属持续不可用而非偶发抖动。
//
// 与累计 Score 规则取"或"（recordFailure 中判定）。min=8 保证个位数以内的
// 短抖动（含 4 次低权重 429 的既有用例）不会仅凭窗口失败率开断。
const (
	rollingMinSamples           = 8
	rollingFailureRateThreshold = 0.8
)

// recordOutcome 记一次结果进滚动窗口。
func (c *Circuit) recordOutcome(success bool) {
	c.RecentOutcomes = append(c.RecentOutcomes, success)
	if len(c.RecentOutcomes) > rollingWindowSize {
		c.RecentOutcomes = c.RecentOutcomes[len(c.RecentOutcomes)-rollingWindowSize:]
	}
}

// RollingSuccessRate 最近窗口内的成功率；无样本时返回 0
// （routing-spec §7 要求快照含该字段；api-spec §6.5 示例为 0.0/1.0）。
func (c *Circuit) RollingSuccessRate() float64 {
	if c == nil || len(c.RecentOutcomes) == 0 {
		return 0
	}
	ok := 0
	for _, success := range c.RecentOutcomes {
		if success {
			ok++
		}
	}
	return float64(ok) / float64(len(c.RecentOutcomes))
}

// RollingSamples 滚动窗口样本数（0 = 冷启动，成功率无意义）。
func (c *Circuit) RollingSamples() int {
	if c == nil {
		return 0
	}
	return len(c.RecentOutcomes)
}

// RollingFailureRate 最近窗口内的失败率；无样本时返回 0。
func (c *Circuit) RollingFailureRate() float64 {
	if c == nil || len(c.RecentOutcomes) == 0 {
		return 0
	}
	return 1 - c.RollingSuccessRate()
}

// rollingFailureTriggersOpen 判断滚动窗口是否已给出"持续失败"的开断证据。
// 调用方持 Runtime 锁（recordFailure 内）。
func (c *Circuit) rollingFailureTriggersOpen(settings CircuitSettings) bool {
	if c == nil || len(c.RecentOutcomes) < settings.RollingMinSamples {
		return false
	}
	return c.RollingFailureRate() >= settings.RollingFailureRate
}

// Event 运行态事件（带时间戳，作为验收与排障证据）。
// Lane 为事件所属车道的路由键；Lane 为空表示历史事件或测试注入
// （design-v1 §16.10：webhook 事件的 lane 字段来自这里）。
type Event = events.Event

// Runtime 一条车道（一个路由键）的运行态，全部请求共享。
type Runtime struct {
	mu sync.Mutex

	// laneName 本车道键（由 Registry.For 建立时写入），事件透传给订阅方。
	laneName string

	CurrentMember MemberKey
	HasCurrent    bool

	// probe 是唯一有效的恢复探测租约；过期只在合法写操作中回收。
	probe      *ProbeLease
	generation uint64

	AffinityUntil int64
	AffinityArmed bool

	Cooldowns map[MemberKey]int64
	// PacedUntil 限流减速截止（Unix ms，routing-spec §4.1.1）：429 的"发太快"信号。
	// 与 Cooldowns 分开：减速不改变成员可选性，只影响本轮是否立即发送。
	PacedUntil map[MemberKey]int64
	Circuits   map[MemberKey]*Circuit
	Events     []Event

	// laneDown 车道级停摆期标记：一次请求因"全部成员不可选"按 503 收尾时置位并
	// 发出 lane_unavailable（同一停摆期只发一次），此后第一次成功服务发出
	// lane_recovered 并复位（routing-spec §7 的配对事件）。
	laneDown      bool
	laneDownSince int64

	// prunedInit/prunedSig/prunedVersion 记录最近一次成员集合变更的处理结果，
	// 供 PruneStaleState 判定"是否需要清理"并拒绝过期配置的清理（routing-spec §1.3）。
	prunedInit    bool
	prunedSig     string
	prunedVersion int64
	// routeSignature 记录最近一次应用的完整车道配置签名。优先级/成员顺序
	// 变化时，必须立即解除亲和，否则运行态会继续粘住旧的低优先级成员。
	routeSignature string
}

func newRuntime() *Runtime {
	return &Runtime{
		Cooldowns:  map[MemberKey]int64{},
		PacedUntil: map[MemberKey]int64{},
		Circuits:   map[MemberKey]*Circuit{},
		Events:     []Event{},
	}
}

// Registry 进程内运行态注册表，按车道键（路由键）索引。
type Registry struct {
	mu    sync.RWMutex
	lanes map[string]*Runtime
}

// Default 是进程级注册表。
var Default = &Registry{lanes: map[string]*Runtime{}}

// withLock 在持有运行态锁的情况下执行 fn。所有 Runtime 的字段级操作都假定
// 调用方已持锁，锁顺序固定为 State.mu → Runtime.mu，不得反向获取。
func (r *Runtime) withLock(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

// circuitOpen 该成员当前是否处于 open 状态。
func (r *Runtime) circuitOpen(key MemberKey) bool {
	circuit := r.Circuits[key]
	return circuit != nil && circuit.State == CircuitOpen
}

// circuitState 该成员当前熔断态；无条目返回 closed。
func (r *Runtime) circuitState(key MemberKey) string {
	circuit := r.Circuits[key]
	if circuit == nil {
		return CircuitClosed
	}
	return circuit.State
}

// circuitOpenUntil 熔断退避截止时刻（ms）；未打开时返回 0。
func (r *Runtime) circuitOpenUntil(key MemberKey) int64 {
	circuit := r.Circuits[key]
	if circuit == nil || circuit.State != CircuitOpen {
		return 0
	}
	return circuit.OpenUntil
}

// cooldownUntil 该成员冷却截止时间（Unix ms）；无冷却或已到期返回 0。
// 过期条目不再对外暴露（health 显示过去的时刻会与 available=true 自相矛盾）。
func (r *Runtime) cooldownUntil(key MemberKey) int64 {
	until := r.Cooldowns[key]
	if until != 0 && until <= nowMs() {
		return 0
	}
	return until
}

// paceUntil 该成员限流减速截止时间（Unix ms）；无减速或已到期返回 0。
func (r *Runtime) paceUntil(key MemberKey) int64 {
	until := r.PacedUntil[key]
	if until != 0 && until <= nowMs() {
		return 0
	}
	return until
}

// paceRemaining 该成员限流减速的剩余时长；无减速或已到期返回 0。
// 同为成员原地重试的等待下限（routing-spec §3.1）：上游说了 Retry-After 5s，
// 内部重试间隔 3s，就必须等满 5s，而不是按 3s 继续打。
func (r *Runtime) paceRemaining(key MemberKey) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	until := r.paceUntil(key)
	if until == 0 {
		return 0
	}
	// 必须用 nowMs()（可被测试替换的时钟），不能用 time.Until：后者读真实
	// 墙钟，与假时钟/注入时钟不在同一时间轴上。
	remainMs := until - nowMs()
	if remainMs <= 0 {
		return 0
	}
	return time.Duration(remainMs) * time.Millisecond
}

// settingsProvider 由外部注入熔断参数读取（接到 system/options）。
var settingsProvider func() CircuitSettings

// SetCircuitSettingsProvider 注入熔断参数来源；未注入时用默认值。
func SetCircuitSettingsProvider(provider func() CircuitSettings) {
	settingsProvider = provider
}

// CurrentCircuitSettings 返回当前生效的熔断参数（供健康快照与 API 使用）。
func CurrentCircuitSettings() CircuitSettings {
	if settingsProvider == nil {
		return DefaultCircuitSettings()
	}
	settings := settingsProvider()
	if settings.FailureThreshold <= 0 {
		settings = DefaultCircuitSettings()
	}
	if settings.MaxOpenSeconds <= 0 {
		settings.MaxOpenSeconds = DefaultCircuitSettings().MaxOpenSeconds
	}
	if settings.MaxOpenSeconds > MaxCircuitOpenSeconds {
		settings.MaxOpenSeconds = MaxCircuitOpenSeconds
	}
	if settings.OpenSeconds > MaxCircuitOpenSeconds {
		settings.OpenSeconds = MaxCircuitOpenSeconds
	}
	if settings.RollingMinSamples <= 0 {
		settings.RollingMinSamples = rollingMinSamples
	}
	if settings.RollingMinSamples > MaxCircuitRollingMinSamples {
		settings.RollingMinSamples = MaxCircuitRollingMinSamples
	}
	if settings.RollingFailureRate <= 0 || settings.RollingFailureRate > 1 {
		settings.RollingFailureRate = rollingFailureRateThreshold
	}
	if settings.HalfOpenSuccesses <= 0 {
		settings.HalfOpenSuccesses = DefaultCircuitSettings().HalfOpenSuccesses
	}
	if len(settings.AllowedFails) == 0 {
		settings.AllowedFails = defaultAllowedFails()
	}
	if settings.RejectStreakThreshold <= 0 {
		settings.RejectStreakThreshold = rejectStreakThreshold
	}
	if settings.RejectCooldownSeconds <= 0 {
		settings.RejectCooldownSeconds = rejectCooldownSeconds
	}
	return settings
}

// For 取（必要时创建）某车道的运行态。
func (r *Registry) For(lane string) *Runtime {
	r.mu.RLock()
	rt, ok := r.lanes[lane]
	r.mu.RUnlock()
	if ok {
		return rt
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if rt, ok = r.lanes[lane]; ok {
		return rt
	}
	rt = newRuntime()
	rt.laneName = lane
	r.lanes[lane] = rt
	return rt
}

// Generation 只读返回车道当前代次；未登记返回 0。
func (r *Registry) Generation(lane string) uint64 {
	runtime := r.Get(lane)
	if runtime == nil {
		return 0
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.generation
}

// Get 只查询已登记的运行态；未登记时返回 nil（**不创建**）。
//
// 未配车道的模型名不得经注册表永久堆积：否则任何已鉴权请求用一个任意模型名
// 都会新增 Runtime，SnapshotLanes 随"请求过的模型名数"线性增长，SSE 快照
// （buildRouteStateSnapshot 按 SnapshotLanes 逐车道 ResolveRoute）退化为
// O(请求过的模型名数) 次 DB 查询（routing-spec §1.3）。
func (r *Registry) Get(lane string) *Runtime {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lanes[lane]
}

// Remove 删除某车道的运行态（车道被删除时清理其冷却/熔断/探测槽/亲和残留，
// routing-spec §1.3）。幂等：键不存在时无操作。
func (r *Registry) Remove(lane string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.lanes, lane)
}

// SnapshotLanes 返回全部有运行态的车道键（供重启后清空语义与调试）。
func (r *Registry) SnapshotLanes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.lanes))
	for name := range r.lanes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// MaintainLane 在配置写入口收敛成员迁移与残留状态；请求/Health 只读路径不得调用。
func (reg *Registry) MaintainLane(name string) error {
	resolved, err := model.ResolveRouteForDisplay(name)
	if err != nil {
		return err
	}
	if resolved == nil {
		return nil
	}
	key := resolved.RouteKey
	if key == "" {
		key = resolved.Model
	}
	runtime := reg.Get(key)
	if runtime == nil {
		// 已删除或从未登记的车道不需要为“维护”新建 Runtime。
		return nil
	}
	valid := make(map[MemberKey]bool, len(resolved.Members))
	for i := range resolved.Members {
		valid[MemberKeyOf(&resolved.Members[i])] = true
	}
	runtime.PruneStaleState(valid, resolved.LaneVersion)
	return nil
}

// MaintainAll 在配置写入口批量收敛全部已登记车道。
func (reg *Registry) MaintainAll() error {
	for _, name := range reg.SnapshotLanes() {
		if err := reg.MaintainLane(name); err != nil {
			return err
		}
	}
	return nil
}

// MemberRuntimeState 是一次只读快照里的成员运行态。
//
// 供 /metrics 这类"抓取时不查库"的只读消费者使用；不是对外管理 API 的稳定形状。
type MemberRuntimeState struct {
	Lane          string
	MemberKey     MemberKey
	Circuit       string
	CooldownUntil int64
}

// SnapshotMemberStates 返回全部车道的成员运行态快照（只读、不查库）。
//
// 只读取进程内 Registry/Runtime 的既有状态，不触发选路、不访问数据库；
// 同一成员同时有熔断与冷却条目时只返回一条。
func (r *Registry) SnapshotMemberStates() []MemberRuntimeState {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]MemberRuntimeState, 0, len(r.lanes))
	for lane, runtime := range r.lanes {
		if runtime == nil {
			continue
		}
		runtime.mu.Lock()
		for member, circuit := range runtime.Circuits {
			state := CircuitClosed
			if circuit != nil && circuit.State != "" {
				state = circuit.State
			}
			out = append(out, MemberRuntimeState{
				Lane:          lane,
				MemberKey:     member,
				Circuit:       state,
				CooldownUntil: runtime.Cooldowns[member],
			})
		}
		for member, until := range runtime.Cooldowns {
			if _, ok := runtime.Circuits[member]; ok {
				continue
			}
			out = append(out, MemberRuntimeState{
				Lane:          lane,
				MemberKey:     member,
				Circuit:       CircuitClosed,
				CooldownUntil: until,
			})
		}
		runtime.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lane != out[j].Lane {
			return out[i].Lane < out[j].Lane
		}
		return out[i].MemberKey.String() < out[j].MemberKey.String()
	})
	return out
}

// nowMs 可被测试替换。
var nowMs = func() int64 { return time.Now().UnixMilli() }

// weightOf 按错误分类给出熔断计分权重。
func weightOf(kind ErrorKind) float64 {
	switch kind {
	case KindHardAuth, KindHardQuota, KindBadResponse:
		return weightHard
	case KindSoftTransient, KindTimeout:
		return weightSoft
	case KindSoftRateLimit:
		return weightLimit
	default:
		return weightOther
	}
}

// availabilityOf 该成员当前是否可选。冷却与熔断合并为同一个判断（§5.3）。
type availability int

const (
	availOK         availability = iota // 正常可用
	availProbeReady                     // 冷却/熔断到期，仅放行一个探测请求
	availSkip                           // 冷却中或熔断打开，跳过
)

func (r *Runtime) availabilityOf(key MemberKey, settings CircuitSettings) availability {
	now := nowMs()
	// §5.3：先判冷却——冷却未到期时**不得**占用探测槽（half_open/open 到期也一样）。
	until := r.Cooldowns[key]
	if until != 0 && now < until {
		return availSkip
	}
	if circuit := r.Circuits[key]; circuit != nil {
		switch circuit.State {
		case CircuitOpen:
			if now < circuit.OpenUntil {
				return availSkip
			}
			// 退避期到：半开，放一个探测请求。
			return availProbeReady
		case CircuitHalfOpen:
			// 半开且探测在途：只能由单探测槽持有者使用。若这里返回 availOK，
			// 并发请求会把"半开的成员"当健康成员直接选中，绕过单探测槽一拥而上
			// （routing-spec §2.1/§5.2：半开只放行一个探测请求）。
			// 返回 availProbeReady 后由 takeProbe 判定：槽被占则跳过。
			return availProbeReady
		}
	}
	if until != 0 {
		// 冷却已到期（探测槽语义由 takeProbe 决定）。
		return availProbeReady
	}
	return availOK
}

// SkipReason 成员被跳过时的原因，用于 attempts[].status（routing-spec §9）：
// `circuit_break`（熔断打开）、`cooldown`（冷却未到期）、`skipped`（其他不可选，
// 例如探测槽已被占用）。文档要求据此解释"为什么没用优先级最高的那个成员"。
func (r *Runtime) SkipReason(key MemberKey) string {
	now := nowMs()
	if circuit := r.Circuits[key]; circuit != nil {
		switch circuit.State {
		case CircuitOpen:
			if now < circuit.OpenUntil {
				return "circuit_break"
			}
			// 退避已到但探测槽被别人占着 → 本轮只是被跳过
			return "skipped"
		case CircuitHalfOpen:
			// 半开探测在途，槽被占 → 本轮跳过。
			return "skipped"
		}
	}
	if until := r.Cooldowns[key]; until != 0 {
		if now < until {
			return "cooldown"
		}
		return "skipped" // 冷却已到期但探测槽被占用
	}
	return "skipped"
}

// recordFailure 记一次失败：更新熔断计分，必要时打开熔断；尝试预算耗尽则进入冷却。
//
// applyCooldown=false 表示"该车道不参与冷却"（routing-spec §2.1：manual 模式只过
// 熔断、不写冷却——冷却对唯一指定成员没有可换的替代者，只会把请求变成 503，
// 而且残留冷却会在切回 failover 后继续误伤）。熔断计分与亲和收敛照常。
func (r *Runtime) recordFailure(key MemberKey, kind ErrorKind, memberCooldownSeconds int, retryAfterSeconds int, applyCooldown bool, settings CircuitSettings) {
	now := nowMs()

	// 429 = 减速，不是故障（routing-spec §4.1.1 / ADR 0013）：
	// 只写 PacedUntil + member_paced，不写 Cooldowns、不计 Score/FailsByKind/
	// 滚动窗口、不开熔断。与 manual 无关——减速是"放慢"，不是"回避"。
	if kind == KindSoftRateLimit {
		r.applyPaceLocked(key, retryAfterSeconds, now)
		return
	}

	// §6：亲和期内**当前成员**失败 → 立即结束亲和（照搬线上语义）。
	// 否则该成员会在剩余亲和窗口内继续被粘滞，形成"失败→仍粘着→再失败"。
	// 独立探测失败不影响当前路由，故只在 key 就是当前成员时清除。
	if r.HasCurrent && r.CurrentMember == key {
		if r.AffinityUntil != 0 {
			r.appendEvent("affinity_end", key, "ended_by_failure")
		}
		r.AffinityUntil = 0
		r.AffinityArmed = true
	}
	circuit := r.Circuits[key]
	if circuit == nil {
		circuit = &Circuit{State: CircuitClosed}
		r.Circuits[key] = circuit
	}
	circuit.LastErrorKind = kind
	circuit.LastErrorAt = now
	// 连续拒绝只统计"连续 upstream_reject"（routing-spec §4.1）：任何其他失败
	// 都说明该成员不是一直拒绝，序列到此中断。否则 timeout 夹在两次拒绝之间
	// 仍会被累计成"连续拒绝"，把请求级拒绝误升级成配置结论。
	circuit.RejectStreak = 0
	circuit.Score += weightOf(kind)
	circuit.ConsecutiveFailures++
	if circuit.FailsByKind == nil {
		circuit.FailsByKind = map[ErrorKind]int{}
	}
	// 严格“连续”：记录某一类失败时清空其他类的连续计数，避免 timeout 与
	// soft_transient 交替发生时两种计数都累加、比“连续 N 次同类型”更早开断。
	for k := range circuit.FailsByKind {
		if k != kind {
			delete(circuit.FailsByKind, k)
		}
	}
	circuit.FailsByKind[kind]++
	circuit.recordOutcome(false)

	// 冷却时长（单处算术，顺序不可换）：
	// 1) 先按分类取**本地 base**（hard_auth/hard_quota ×2）；
	// 2) soft_transient/timeout 再按连续次数分级（n=1 /8、n=2 /4、n=3 /2、n≥4 满档，下限 5s）；
	// 3) **最后**与 Retry-After 取较大值，再封顶 + 抖动。
	// 先取 Retry-After 再做除法会把上游要求的 90s 缩成 11s（ADR 0013）。
	local := cooldownSecondsFor(kind, memberCooldownSeconds)
	if kind == KindSoftTransient || kind == KindTimeout {
		div := 1
		switch circuit.FailsByKind[kind] {
		case 0, 1:
			div = 8
		case 2:
			div = 4
		case 3:
			div = 2
		}
		scaled := local / div
		// 下限：软故障至少歇 softTransientMinCooldownSeconds；但**绝不把配置本身
		// 就比它短的成员级冷却反过来加长**（否则配置里的 3s 会被"修"成 5s）。
		lower := softTransientMinCooldownSeconds
		if local < lower {
			lower = local
		}
		if scaled < lower {
			scaled = lower
		}
		local = scaled
	}
	if retryAfterSeconds > local {
		local = retryAfterSeconds
	}
	if local > maxMemberCooldownSeconds {
		local = maxMemberCooldownSeconds
	}
	if local > 0 {
		local = jitterSeconds(local, key, now)
		if local > maxMemberCooldownSeconds {
			local = maxMemberCooldownSeconds
		}
	}

	if applyCooldown && local > 0 {
		until := now + int64(local)*1000
		// 冷却单调：并发请求里后到的短冷却不得覆盖先前的长冷却（routing-spec §6）。
		if current := r.Cooldowns[key]; until > current {
			r.Cooldowns[key] = until
			r.appendEvent(EventCooldown, key, "cooldown_until="+strconv.FormatInt(until, 10))
		}
	}

	// 开断证据取"或"：半开探测失败立即回 open（§5.2）；否则按 allowed_fails、
	// 累计失败分阈值、滚动窗口失败率三条证据取或。
	if circuit.State == CircuitHalfOpen {
		circuit.HalfOpenSuccesses = 0
		r.openCircuit(key, circuit, now, local, settings)
		return
	}
	allowedReached := false
	if allowed := settings.AllowedFails[kind]; allowed > 0 && circuit.FailsByKind[kind] >= allowed {
		allowedReached = true
	}
	if circuit.State != CircuitOpen &&
		(allowedReached || circuit.Score >= settings.FailureThreshold || circuit.rollingFailureTriggersOpen(settings)) {
		r.openCircuit(key, circuit, now, local, settings)
	}
}

// applyPaceLocked 记录一次限流减速（429）：只写 PacedUntil 与历史留痕，
// 不写冷却、不计熔断分、不计滚动窗口。调用方持 Runtime 锁。
func (r *Runtime) applyPaceLocked(key MemberKey, retryAfterSeconds int, now int64) {
	if r.HasCurrent && r.CurrentMember == key {
		if r.AffinityUntil != 0 {
			r.appendEvent("affinity_end", key, "ended_by_pace")
		}
		r.AffinityUntil = 0
		r.AffinityArmed = true
	}
	pace := rateLimitPaceSeconds
	if retryAfterSeconds > pace {
		pace = retryAfterSeconds
	}
	if pace > rateLimitMaxPaceSeconds {
		pace = rateLimitMaxPaceSeconds
	}
	until := now + int64(pace)*1000
	// 减速单调：并发 429 里后到的短的 Retry-After 不得覆盖先前的长减速。
	if current := r.PacedUntil[key]; until > current {
		r.PacedUntil[key] = until
		r.appendEvent(EventMemberPaced, key,
			"until="+strconv.FormatInt(until, 10)+" retry_after="+strconv.Itoa(retryAfterSeconds))
	}

	circuit := r.Circuits[key]
	if circuit == nil {
		circuit = &Circuit{State: CircuitClosed}
		r.Circuits[key] = circuit
	}
	circuit.LastErrorKind = KindSoftRateLimit
	circuit.LastErrorAt = now
	// 429 是上游要求放慢，不属于 upstream_reject；同样中断"连续拒绝"序列。
	circuit.RejectStreak = 0
}

// recordReject 记一次"成员级拒绝"（routing-spec §4.1）：只更新 last_error_kind
// 供排障，**不计熔断分**——拒绝是上游对这份请求的判断，不是成员的故障
// （下一个请求它可能完全正常）。但**连续拒绝**说明该成员大概率配置有误
// （例如上游要求一个网关没有转发的头）：达到阈值后写一段短期冷却并留痕，
// 避免每个请求都先白打它一次。任何非 upstream_reject 的结果（成功、其他失败
// 分类、429 减速）都会中断连续语义并由对应记账路径清零。
// 亲和若粘在被拒绝的当前成员上，同样立即结束：本次请求已经离开它，
// 继续粘着只会把后续选路拉回同一个拒绝者。
func (r *Runtime) recordReject(key MemberKey, kind ErrorKind, applyCooldown bool, requestScoped bool, settings CircuitSettings) {
	if r.HasCurrent && r.CurrentMember == key {
		if r.AffinityUntil != 0 {
			r.appendEvent("affinity_end", key, "ended_by_reject")
		}
		r.AffinityUntil = 0
		r.AffinityArmed = true
	}
	circuit := r.Circuits[key]
	if circuit == nil {
		circuit = &Circuit{State: CircuitClosed}
		r.Circuits[key] = circuit
	}
	circuit.LastErrorKind = kind
	circuit.LastErrorAt = nowMs()
	if requestScoped {
		// 内容审查/参数不支持等只针对本次请求：换成员、不冷却，也不累计
		// “配置可疑”的连续拒绝（routing-spec §4.1.3）。它同样中断既有连续
		// 序列——否则请求级拒绝夹在两次配置可疑拒绝之间仍会被算成“连续”。
		circuit.RejectStreak = 0
		return
	}
	circuit.RejectStreak++
	threshold := settings.RejectStreakThreshold
	if threshold <= 0 {
		threshold = rejectStreakThreshold
	}
	cooldownSeconds := settings.RejectCooldownSeconds
	if cooldownSeconds <= 0 {
		cooldownSeconds = rejectCooldownSeconds
	}
	// 连续拒绝达到阈值：清零计数（无论是否写冷却——manual 车道不回避，但计数
	// 必须按"连续"语义收敛，不能单调增长）。写冷却时走统一写法（抖动 + 封顶）。
	if circuit.RejectStreak >= threshold {
		circuit.RejectStreak = 0
		if applyCooldown {
			now := nowMs()
			secs := jitterSeconds(cooldownSeconds, key, now)
			if secs > maxMemberCooldownSeconds {
				secs = maxMemberCooldownSeconds
			}
			until := now + int64(secs)*1000
			// 冷却单调：并发请求里后到的短冷却不得覆盖先前的长冷却。
			if current := r.Cooldowns[key]; until > current {
				r.Cooldowns[key] = until
				r.appendEvent(EventCooldown, key,
					"cooldown_until="+strconv.FormatInt(until, 10)+" reason=consecutive_rejects")
			}
		}
	}
}

// noteLaneUnavailable 登记"车道不可用"停摆期：置位并发出一次 lane_unavailable，
// 同一停摆期内重复调用不重复发事件（routing-spec §7）。调用方持 Runtime 锁。
func (r *Runtime) noteLaneUnavailable() {
	if r.laneDown {
		return
	}
	r.laneDown = true
	r.laneDownSince = nowMs()
	r.appendEvent(EventLaneUnavailable, MemberKey{}, "no_available_member")
}

// openCircuit 打开熔断，并按打开次数做指数退避（open_seconds × 2^k，设上限）。
func (r *Runtime) openCircuit(key MemberKey, circuit *Circuit, now int64, memberCooldownSeconds int, settings CircuitSettings) {
	base := settings.OpenSeconds
	if base <= 0 {
		base = memberCooldownSeconds
	}
	if base <= 0 {
		base = 60
	}
	duration := base
	maxOpen := settings.MaxOpenSeconds
	if maxOpen <= 0 || maxOpen > MaxCircuitOpenSeconds {
		maxOpen = MaxCircuitOpenSeconds
	}
	for i := 0; i < circuit.OpenCount && duration < maxOpen; i++ {
		duration *= openBackoffK
	}
	if duration > maxOpen {
		duration = maxOpen
	}
	circuit.State = CircuitOpen
	circuit.OpenCount++
	circuit.OpenedAt = now
	circuit.OpenUntil = now + int64(duration)*1000
	circuit.LastTransitionAt = now
	circuit.HalfOpenSuccesses = 0
	r.appendEvent(EventCircuitOpen, key, "open_seconds="+strconv.Itoa(duration))
}

// recordProbeStart 记一次半开探测开始（时间戳证据）。
func (r *Runtime) recordProbeStart(key MemberKey, wasOpen bool) {
	if !wasOpen {
		return
	}
	now := nowMs()
	if circuit := r.Circuits[key]; circuit != nil {
		circuit.State = CircuitHalfOpen
		circuit.LastTransitionAt = now
	}
	r.appendEvent(EventCircuitHalfOpen, key, "probe_started")
}

// recordSuccess 记一次成功：解除冷却、复位熔断；若此前发生过故障切换则启动亲和。
// 同时收口两类配对事件（routing-spec §7）：成员级（hadCooldown 且熔断未打开过 →
// member_recovered）与车道级（停摆期后的第一次成功 → lane_recovered）。
func (r *Runtime) recordSuccess(key MemberKey, affinitySeconds int, settings CircuitSettings, fromProbe bool) {
	now := nowMs()
	circuit := r.Circuits[key]
	if circuit == nil {
		// 全成功的成员此前没有熔断器条目；建一个，使滚动成功率也有样本
		// （routing-spec §7 要求每个成员都暴露 rolling_success_rate）。
		circuit = &Circuit{State: CircuitClosed}
		r.Circuits[key] = circuit
	}
	was := circuit.State
	// 在飞成功不能绕过探测槽关闭 open/half_open：只有携带 ProbeLease 的探测成功
	// 才推进熔断恢复；普通请求只记滚动样本并解除减速（routing-spec §5.4）。
	if !fromProbe && (was == CircuitOpen || was == CircuitHalfOpen) {
		circuit.recordOutcome(true)
		delete(r.PacedUntil, key)
		return
	}
	_, hadCooldown := r.Cooldowns[key]
	delete(r.Cooldowns, key)
	if hadCooldown {
		// 冷却与熔断可能由同一次失败同时开始（先写冷却，再开断）。冷却由第一次
		// 成功收口，熔断由 circuit_closed 收口；两者各自配对（routing-spec §7）。
		r.appendEvent(EventMemberRecovered, key, "from=cooldown")
	}
	// 成功即解除限流减速（routing-spec §4.1.1）。
	delete(r.PacedUntil, key)
	circuit.Score = 0
	circuit.ConsecutiveFailures = 0
	circuit.RejectStreak = 0
	circuit.FailsByKind = nil
	circuit.OpenUntil = 0
	circuit.recordOutcome(true)
	required := settings.HalfOpenSuccesses
	if required <= 0 {
		required = 1
	}
	if was == CircuitHalfOpen && circuit.HalfOpenSuccesses+1 < required {
		// 半开仍需更多连续成功：保持 half_open，放行后续探测请求。
		circuit.HalfOpenSuccesses++
		circuit.State = CircuitHalfOpen
		circuit.LastTransitionAt = now
	} else {
		circuit.State = CircuitClosed
		if was != CircuitClosed {
			circuit.OpenCount = 0
			circuit.HalfOpenSuccesses = 0
			circuit.LastTransitionAt = now
			// 恢复后不能让恢复前的失败样本继续参与滚动失败率，否则下一次任意
			// 失败就可能立即再次 open（routing-spec §5.4）。
			circuit.RecentOutcomes = nil
			r.appendEvent(EventCircuitClosed, key, "recovered")
		}
	}
	if r.laneDown {
		r.laneDown = false
		detail := "down_ms=" + strconv.FormatInt(now-r.laneDownSince, 10)
		r.laneDownSince = 0
		r.appendEvent(EventLaneRecovered, MemberKey{}, detail)
	}
	if r.AffinityArmed && affinitySeconds > 0 {
		r.AffinityUntil = now + int64(affinitySeconds)*1000
		r.appendEvent("affinity_start", key, "affinity_seconds="+strconv.Itoa(affinitySeconds))
	}
	r.AffinityArmed = false
}

// eventSubscriber 进程级事件订阅钩子（design-v1 §16.10：webhook 推送经此
// 旁路挂接，internal/route 不反向依赖上层包）。订阅者必须自行缓冲且**绝不
// 阻塞**——appendEvent 持有 Runtime 锁且在请求路径上，订阅者只允许做
// 非阻塞动作（如投入带缓冲的 channel，缓冲满则丢弃并计数）。
// SetEventSubscriber 注册运行态事件订阅者；传 nil 注销。
// 同一进程只应有一个订阅者（webhook 投递器），后注册者覆盖先注册者。
func SetEventSubscriber(fn func(Event)) {
	events.SetSubscriber(fn)
}

func (r *Runtime) appendEvent(eventType string, member MemberKey, detail string) {
	event := Event{Ts: nowMs(), Type: eventType, Lane: r.laneName, Member: member.EventMember(), Detail: detail}
	r.Events = append(r.Events, event)
	if len(r.Events) > 200 {
		r.Events = r.Events[len(r.Events)-200:]
	}
	// 旁路转发给订阅者：锁内只做非阻塞入队，webhook/未来订阅者变慢不会
	// 阻塞选路锁（events.PublishAsync 的队列满即丢弃并计数）。
	events.PublishAsync(event)
}

// Reset 清空该车道的全部运行态（冷却/熔断/探测槽/亲和），返回被清除的
// 熔断器条目数（api-spec §6.6 的响应体 `{"reset": <int>}`）。
// ResetResult 一次运行态重置清除的明细（api-spec §6.6 的 `cleared`）。
type ResetResult struct {
	Circuits               int    `json:"circuits"`
	Cooldowns              int    `json:"cooldowns"`
	Paces                  int    `json:"paces"`
	Probe                  int    `json:"probe"`
	Affinity               int    `json:"affinity"`
	LaneDown               bool   `json:"lane_down"`
	PreviousGeneration     uint64 `json:"previous_generation"`
	Generation             uint64 `json:"generation"`
	InvalidatedProbeLeases int    `json:"invalidated_probe_leases"`
}

// Reset 清空该车道的全部运行态（冷却/减速/熔断/探测槽/亲和/停摆标记），
// 返回清除明细。`reset` 兼容字段 = Circuits。
func (r *Runtime) Reset() ResetResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := ResetResult{
		PreviousGeneration: r.generation,
		Generation:         r.generation + 1,
		Circuits:           len(r.Circuits),
		Cooldowns:          len(r.Cooldowns),
		Paces:              len(r.PacedUntil),
		LaneDown:           r.laneDown,
	}
	if r.probe != nil && r.probe.expiresAt > nowMs() {
		result.Probe = 1
		result.InvalidatedProbeLeases = 1
	}
	if r.AffinityUntil != 0 || r.AffinityArmed {
		result.Affinity = 1
	}
	r.Cooldowns = map[MemberKey]int64{}
	r.PacedUntil = map[MemberKey]int64{}
	r.Circuits = map[MemberKey]*Circuit{}
	r.probe = nil
	r.generation++
	r.AffinityArmed = false
	r.AffinityUntil = 0
	// 停摆标记与当前成员一并复位，避免 Reset 后残留 lane_unavailable 状态。
	r.laneDown = false
	r.laneDownSince = 0
	r.HasCurrent = false
	r.CurrentMember = MemberKey{}
	r.appendEvent(EventReset, MemberKey{}, "circuits cleared")
	return result
}

// Health 返回某车道的健康快照；运行态尚未建立时用一份临时空运行态计算
// （成员全部按"无冷却/无熔断"处理），不写入注册表。
func (reg *Registry) Health(key string, resolved *model.ResolvedRoute, settings CircuitSettings) HealthSnapshot {
	if runtime := reg.Get(key); runtime != nil {
		return runtime.Health(resolved, settings)
	}
	empty := newRuntime()
	empty.laneName = key
	return empty.Health(resolved, settings)
}

// MemberPenaltyClearResult 探活成功后跨车道的惩罚态清理结果（routing-spec §5.4）。
type MemberPenaltyClearResult struct {
	// Lanes 实际被清理的车道名（去重、升序）。
	Lanes []string
	// Members 成功清除惩罚态的成员条目数（本来无惩罚态则不计）。
	Members int
	// ProbeBusyLanes 该成员正在占用探测槽、按单探测槽不变量跳过清理的车道。
	ProbeBusyLanes []string
}

// GenerationSnapshot 返回当前所有车道代次，供跨车道探活在发起请求前建立
// 只读基线。探活结束只能清理代次未变化的车道，不能污染 Reset 后的新状态。
func (reg *Registry) GenerationSnapshot() map[string]uint64 {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	out := make(map[string]uint64, len(reg.lanes))
	for lane, runtime := range reg.lanes {
		if runtime == nil {
			continue
		}
		runtime.mu.Lock()
		out[lane] = runtime.generation
		runtime.mu.Unlock()
	}
	return out
}

// ClearMemberPenaltyAtGenerations 仅在车道代次仍等于探活前快照时清理。
func (reg *Registry) ClearMemberPenaltyAtGenerations(key MemberKey, generations map[string]uint64) MemberPenaltyClearResult {
	return reg.clearPenaltyWithGenerations(func(candidate MemberKey) bool { return candidate == key }, generations)
}

// ClearChannelModelPenaltyAtGenerations 仅在车道代次仍等于探活前快照时清理
// 渠道 × 上游模型作用域下全部成员条目的惩罚态。
func (reg *Registry) ClearChannelModelPenaltyAtGenerations(channelID int, upstreamModel string, generations map[string]uint64) MemberPenaltyClearResult {
	return reg.clearPenaltyWithGenerations(func(candidate MemberKey) bool {
		return candidate.ChannelID == channelID && candidate.UpstreamModel == upstreamModel
	}, generations)
}

func (reg *Registry) clearPenaltyWithGenerations(matches func(MemberKey) bool, generations map[string]uint64) MemberPenaltyClearResult {
	result := MemberPenaltyClearResult{}
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	for laneName, runtime := range reg.lanes {
		if runtime == nil {
			continue
		}
		expected, ok := generations[laneName]
		if !ok {
			continue
		}
		runtime.mu.Lock()
		current := runtime.generation
		runtime.mu.Unlock()
		if current != expected {
			continue
		}
		cleared, probeBusy := runtime.clearMemberPenalty(matches, expected)
		if cleared > 0 {
			result.Lanes = append(result.Lanes, laneName)
			result.Members += cleared
		}
		if probeBusy {
			result.ProbeBusyLanes = append(result.ProbeBusyLanes, laneName)
		}
	}
	sort.Strings(result.Lanes)
	sort.Strings(result.ProbeBusyLanes)
	return result
}

// ClearMemberPenalty 精确清除某成员在所有车道的惩罚态，负数成员 ID 同样精确匹配。
func (reg *Registry) ClearMemberPenalty(key MemberKey) MemberPenaltyClearResult {
	return reg.clearPenalty(func(candidate MemberKey) bool { return candidate == key })
}

// ClearMemberPenaltyAtGeneration 仅在车道代次仍等于探活读取代次时清理。
func (reg *Registry) ClearMemberPenaltyAtGeneration(key MemberKey, generation uint64) MemberPenaltyClearResult {
	return reg.clearPenalty(func(candidate MemberKey) bool { return candidate == key }, generation)
}

// ClearChannelModelPenalty 清除渠道 × 上游模型作用域下全部成员条目的惩罚态。
func (reg *Registry) ClearChannelModelPenalty(channelID int, upstreamModel string) MemberPenaltyClearResult {
	return reg.clearPenalty(func(candidate MemberKey) bool {
		return candidate.ChannelID == channelID && candidate.UpstreamModel == upstreamModel
	})
}

func (reg *Registry) clearPenalty(matches func(MemberKey) bool, generations ...uint64) MemberPenaltyClearResult {
	result := MemberPenaltyClearResult{}
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	for laneName, runtime := range reg.lanes {
		if runtime == nil {
			continue
		}
		if len(generations) > 0 {
			runtime.mu.Lock()
			current := runtime.generation
			runtime.mu.Unlock()
			if current != generations[0] {
				continue
			}
		}
		cleared, probeBusy := runtime.clearMemberPenalty(matches, generations...)
		if cleared > 0 {
			result.Lanes = append(result.Lanes, laneName)
			result.Members += cleared
		}
		if probeBusy {
			result.ProbeBusyLanes = append(result.ProbeBusyLanes, laneName)
		}
	}
	sort.Strings(result.Lanes)
	sort.Strings(result.ProbeBusyLanes)
	return result
}

func (r *Runtime) clearMemberPenalty(matches func(MemberKey) bool, generations ...uint64) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(generations) > 0 && r.generation != generations[0] {
		return 0, false
	}
	now := nowMs()
	r.expireProbeLocked(now)
	if len(generations) > 0 && r.generation != generations[0] {
		return 0, false
	}
	keys := map[MemberKey]struct{}{}
	for key := range r.Cooldowns {
		if matches(key) {
			keys[key] = struct{}{}
		}
	}
	for key := range r.PacedUntil {
		if matches(key) {
			keys[key] = struct{}{}
		}
	}
	for key, circuit := range r.Circuits {
		if matches(key) && circuit != nil && (circuit.State != CircuitClosed || circuit.Score > 0 || circuit.ConsecutiveFailures > 0 || circuit.RejectStreak > 0) {
			keys[key] = struct{}{}
		}
	}
	cleared, probeBusy := 0, false
	for key := range keys {
		if r.probe != nil && r.probe.key == key {
			probeBusy = true
			continue
		}
		_, hadCooldown := r.Cooldowns[key]
		_, hadPace := r.PacedUntil[key]
		delete(r.Cooldowns, key)
		delete(r.PacedUntil, key)
		circuit := r.Circuits[key]
		if circuit != nil {
			wasOpen := circuit.State != CircuitClosed
			hadFailure := wasOpen || circuit.LastErrorKind != "" || circuit.Score > 0 || circuit.ConsecutiveFailures > 0 || circuit.RejectStreak > 0
			circuit.State = CircuitClosed
			circuit.Score = 0
			circuit.ConsecutiveFailures = 0
			circuit.FailsByKind = nil
			circuit.RecentOutcomes = nil
			circuit.OpenUntil = 0
			circuit.HalfOpenSuccesses = 0
			circuit.OpenCount = 0
			circuit.RejectStreak = 0
			circuit.LastTransitionAt = now
			if wasOpen {
				r.appendEvent(EventCircuitClosed, key, "recovered_by_manual_probe")
			}
			if hadCooldown || hadPace || hadFailure {
				r.appendEvent(EventMemberRecovered, key, "from=manual_probe")
			}
		} else if hadCooldown || hadPace {
			r.appendEvent(EventMemberRecovered, key, "from=manual_probe")
		}
		cleared++
	}
	return cleared, probeBusy
}

// PruneStaleState 清理已不在成员链里的残留状态（routing-spec §1.3：
// "成员被删除或渠道被删除时，清理其残留状态"，参照线上 groupRouteLocked）。
//
// 为什么必须清：运行态键是 `channelId:upstreamModel`。成员删除后若重新加回
// 同一渠道+模型（编辑车道时 UpsertLane 先全删再重建成员），键完全相同，
// 新成员会**直接继承**旧成员的冷却与熔断计分——表现为"刚加回来的成员
// 立刻被判为熔断/冷却中"。探测槽与 CurrentMember 同理需要收敛。
//
// 只在"解析出的成员集合非空"时清理：模型暂时没有可用渠道（例如渠道被
// 禁用/删除）时解析结果本就为空，此时清空全部状态会把仍有意义的冷却记录
// 一并抹掉，等渠道恢复后失去退避保护。
//
// 并发与热更新安全：NewState 每次请求都会用本请求解析到的成员集合调用本函数。
// 若请求 A（旧配置，成员 {ch1}）与 B（新配置，成员 {ch1,ch2}）并发，B 先写入
// ch2 的冷却/熔断、A 再用 {ch1} 清理，就会误删 B 刚写入的状态。因此增加
// laneVersion（Lane.UpdatedAt）判定：版本比上次处理过的更旧的请求一律不动
// 运行态（等价于忽略过期配置的解析结果）；只有"版本不旧、且成员集合签名确实
// 变化"时才真正清理一次。laneVersion==0 表示来源无法提供版本（测试夹具/历史
// 数据），退回按签名变化清理。
func (r *Runtime) PruneStaleState(valid map[MemberKey]bool, laneVersion int64) {
	if len(valid) == 0 {
		return
	}
	sig := memberSetSignature(valid)

	r.mu.Lock()
	defer r.mu.Unlock()

	// 键迁移与"清理过期状态"分开：同版本/相同签名的并发编辑（两次 PUT 落在
	// 同一秒）仍可能换了 memberId，健康快照必须看到迁移后的状态。只有不比
	// 已知版本更旧的解析结果才允许迁移（避免旧快照把新状态搬回去）。
	if laneVersion == 0 || r.prunedVersion == 0 || laneVersion >= r.prunedVersion {
		r.migrateStaleMemberKeysLocked(valid)
	}

	if !r.prunedInit {
		// 首次观测：只登记，未清理（此时没有可比对的旧状态）。
		r.prunedInit = true
		r.prunedSig = sig
		r.prunedVersion = laneVersion
		return
	}
	// 过期/同版本的并发请求：不得用它的成员集合裁剪新配置刚写入的状态。
	// 同版本也拒绝——成员组编辑推进的是组的 version，旧快照请求与它可能同为
	// 某个中间版本；宁留残留（不在选路集合里，不影响选路），不删并发新状态。
	if laneVersion != 0 && r.prunedVersion != 0 && laneVersion <= r.prunedVersion {
		return
	}
	if sig == r.prunedSig {
		// 成员集合未变：无需清理；版本只允许前进，绝不回退。
		if laneVersion > r.prunedVersion {
			r.prunedVersion = laneVersion
		}
		return
	}
	// 成员集合真正变化（且非过期解析）：清理一次并登记新集合。
	r.pruneStaleLocked(valid)
	r.prunedSig = sig
	if laneVersion > r.prunedVersion {
		r.prunedVersion = laneVersion
	}
}

// memberSetSignature 成员集合的稳定签名（排序后拼接），用于判定"集合是否变化"。
func memberSetSignature(valid map[MemberKey]bool) string {
	keys := make([]MemberKey, 0, len(valid))
	for key := range valid {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = key.String()
	}
	return strings.Join(parts, "\x00")
}

// pruneStaleLocked 真正执行残留清理。调用方持 r.mu。
func (r *Runtime) pruneStaleLocked(valid map[MemberKey]bool) {
	r.migrateStaleMemberKeysLocked(valid)
	for key := range r.Cooldowns {
		if !valid[key] {
			delete(r.Cooldowns, key)
		}
	}
	for key := range r.PacedUntil {
		if !valid[key] {
			delete(r.PacedUntil, key)
		}
	}
	for key := range r.Circuits {
		if !valid[key] {
			delete(r.Circuits, key)
		}
	}
	if r.HasCurrent && !valid[r.CurrentMember] {
		r.HasCurrent = false
		r.CurrentMember = MemberKey{}
		r.AffinityUntil = 0
		r.AffinityArmed = false
	}
}

// migrateStaleMemberKeysLocked 把"车道编辑导致 memberId 变化"的旧运行态迁移到
// 新键上。
//
// 写入是整链替换（先删光再重插），memberId 必然变化；运行态键含 memberId 时，
// 一次"打开编辑再保存"就会让成员的冷却/熔断凭空消失（runbook 3d 的
// "关→开往返运行态逐字段不变"）。这里在清理旧键前做一次**唯一映射**迁移：
// 仅当同一 (channelId, upstreamModel) 在旧键与新键中都只有一条时迁移；
// 重复成员（同名同模型多条）无法判定归属，宁可不猜（各自独立运行态优先）。
func (r *Runtime) migrateStaleMemberKeysLocked(valid map[MemberKey]bool) {
	if len(valid) == 0 {
		return
	}
	validByPrefix := map[string][]MemberKey{}
	for key := range valid {
		prefix := key.EventMember()
		validByPrefix[prefix] = append(validByPrefix[prefix], key)
	}
	staleByPrefix := map[string][]MemberKey{}
	addStale := func(key MemberKey) {
		if valid[key] {
			return
		}
		prefix := key.EventMember()
		staleByPrefix[prefix] = append(staleByPrefix[prefix], key)
	}
	for key := range r.Cooldowns {
		addStale(key)
	}
	for key := range r.PacedUntil {
		addStale(key)
	}
	for key := range r.Circuits {
		addStale(key)
	}
	for prefix, staleKeys := range staleByPrefix {
		targets := validByPrefix[prefix]
		if len(targets) != 1 {
			continue
		}
		target := targets[0]
		for _, staleKey := range staleKeys {
			if staleKey == target {
				continue
			}
			if until, ok := r.Cooldowns[staleKey]; ok && r.Cooldowns[target] == 0 {
				r.Cooldowns[target] = until
			}
			if until, ok := r.PacedUntil[staleKey]; ok && r.PacedUntil[target] == 0 {
				r.PacedUntil[target] = until
			}
			if circuit, ok := r.Circuits[staleKey]; ok && r.Circuits[target] == nil {
				r.Circuits[target] = circuit
			}
			if r.HasCurrent && r.CurrentMember == staleKey {
				r.CurrentMember = target
			}
			// 迁移成功后删除旧键：否则更高版本的下一次迁移会把旧键里的状态
			// 再次复制回来，已清理的惩罚态被“复活”（幽灵冷却）。
			delete(r.Cooldowns, staleKey)
			delete(r.PacedUntil, staleKey)
			delete(r.Circuits, staleKey)
		}
	}
}

// MemberHealth 单个成员的运行态快照。
//
// 时间字段按 api-spec §2.6 输出 RFC3339 UTC 字符串；无冷却/无退避时为 null
// （api-spec §6.5 示例 `"cooldown_until": null`）。
type MemberHealth struct {
	Member              string  `json:"member"`
	Channel             string  `json:"channel"`
	UpstreamModel       string  `json:"upstream_model"`
	Circuit             string  `json:"circuit"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	FailureScore        float64 `json:"failure_score"`
	RollingSuccessRate  float64 `json:"rolling_success_rate"`
	// RollingSamples 滚动窗口样本数：区分"冷启动无样本"与"最近全失败"（api-spec §6.5）。
	RollingSamples   int                       `json:"rolling_samples"`
	CooldownUntil    *string                   `json:"cooldown_until"`
	PacedUntil       *string                   `json:"paced_until"`
	CircuitOpenUntil *string                   `json:"circuit_open_until"`
	LastErrorKind    ErrorKind                 `json:"last_error_kind,omitempty"`
	LastErrorAt      *string                   `json:"last_error_at"`
	ProbeSince       *string                   `json:"probe_since"`
	ProbeLeaseUntil  *string                   `json:"probe_lease_until"`
	ConfigError      *model.ChannelConfigError `json:"config_error"`
	// OverrideError 成员级覆盖配置错误（无错误为 null）；非 null 时该成员不可选。
	OverrideError *model.LaneConfigError `json:"override_error"`
	// FailsByKind 按错误类型的连续失败计数（用于解释 circuit_allowed_fails）。
	FailsByKind   map[ErrorKind]int `json:"fails_by_kind"`
	Inflight      int               `json:"inflight"`
	Queued        int               `json:"queued"`
	RPMUsed       int               `json:"rpm_used"`
	AdaptiveLimit *int              `json:"adaptive_limit"`
	// TTFTBaselineMs 自适应模式学到的 TTFT 基线（无样本为 0）。
	TTFTBaselineMs float64 `json:"ttft_baseline_ms,omitempty"`
	// RejectStreak 连续"配置可疑"的 upstream_reject 次数（routing-spec §4.1.3）。
	// 请求级 400 不计数；只有后端判定为配置可疑的连续拒绝才累加。
	RejectStreak int  `json:"reject_streak"`
	Current      bool `json:"current"`
	Probing      bool `json:"probing"`
	Available    bool `json:"available"`
	// Enabled 该成员是否参与选路（配置态人工停用，恒回；api-spec §6.5）。
	// 与 Available 是两件事：Available=false 不蕴含任何故障结论，只有
	// Enabled=false 才说明"是运维关的"。关闭者的 Circuit/CooldownUntil/
	// ConsecutiveFailures 等运行态字段仍照常如实给出，不归零也不省略。
	Enabled bool `json:"enabled"`
}

// AffinityState 车道当前亲和（api-spec §6.5：affinity 为对象，无亲和时 null）。
type AffinityState struct {
	Channel       string  `json:"channel"`
	UpstreamModel string  `json:"upstream_model"`
	Until         *string `json:"until"`
}

// HealthSnapshot 车道运行态快照（GET /lanes/{name}/health，api-spec §6.5）。
type HealthSnapshot struct {
	Lane          string         `json:"lane"`
	Source        string         `json:"source"`
	Mode          string         `json:"mode"`
	CurrentMember string         `json:"current_member"`
	ProbeMember   string         `json:"probe_member"`
	Affinity      *AffinityState `json:"affinity"`
	Members       []MemberHealth `json:"members"`
	Events        []Event        `json:"events"`
}

// msToRFC3339 Unix 毫秒 → RFC3339 UTC 字符串（api-spec §2.6）；0（无截止时间）
// 返回 nil，JSON 输出 null（§6.5 的 "cooldown_until": null）。
func msToRFC3339(ms int64) *string {
	if ms <= 0 {
		return nil
	}
	out := time.UnixMilli(ms).UTC().Format(time.RFC3339)
	return &out
}

// circuitBlocksManual 熔断是否正在阻止 manual 成员（open 且退避未到）。
// manual 不参与冷却（routing-spec §2.1），其可用性只由熔断决定。
func (r *Runtime) circuitBlocksManual(key MemberKey) bool {
	circuit := r.Circuits[key]
	return circuit != nil && circuit.State == CircuitOpen && nowMs() < circuit.OpenUntil
}

// Health 生成快照；不复位的只读操作。
func (r *Runtime) Health(resolved *model.ResolvedRoute, settings CircuitSettings) HealthSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	// §2.1：manual 不参与冷却/亲和——快照按 mode 过滤残留冷却，展示与实际选路一致。
	manual := resolved.Mode == model.LaneModeManual
	manualActiveIndex := -1
	if manual {
		manualActiveIndex = model.ActiveMemberIndex(resolved.ActiveMember, resolved.Members)
	}
	snapshot := HealthSnapshot{
		Lane:          resolved.Model,
		Source:        resolved.Source,
		Mode:          resolved.Mode,
		CurrentMember: r.currentLabel(resolved),
		ProbeMember:   r.probeLabel(resolved),
		Events:        append([]Event(nil), r.Events...),
	}
	if !manual && r.AffinityUntil > nowMs() {
		affinity := &AffinityState{Until: msToRFC3339(r.AffinityUntil)}
		for i := range resolved.Members {
			if memberKeyOf(&resolved.Members[i]) == r.CurrentMember {
				affinity.Channel = resolved.Members[i].Channel
				affinity.UpstreamModel = resolved.Members[i].UpstreamModel
				break
			}
		}
		if affinity.Channel == "" {
			// 当前成员已不在链里（并发编辑竞态）：直接使用类型化键的上游名。
			affinity.UpstreamModel = r.CurrentMember.UpstreamModel
		}
		snapshot.Affinity = affinity
	}
	for i := range resolved.Members {
		member := &resolved.Members[i]
		key := memberKeyOf(member)
		item := MemberHealth{
			Member:        memberLabel(member),
			Channel:       member.Channel,
			UpstreamModel: member.UpstreamModel,
			Circuit:       CircuitClosed,
			Current:       r.HasCurrent && r.CurrentMember == key,
			Probing:       r.probe != nil && r.probe.expiresAt > nowMs() && r.probe.key == key,
			Enabled:       !member.Disabled,
		}
		if r.probe != nil && r.probe.expiresAt > nowMs() && r.probe.key == key {
			item.ProbeSince = msToRFC3339(r.probe.acquiredAt)
			item.ProbeLeaseUntil = msToRFC3339(r.probe.expiresAt)
		}
		item.ConfigError = member.ConfigError
		item.OverrideError = member.OverrideError
		item.FailsByKind = map[ErrorKind]int{}
		if circuit := r.Circuits[key]; circuit != nil {
			item.Circuit = circuit.State
			item.ConsecutiveFailures = circuit.ConsecutiveFailures
			item.FailureScore = circuit.Score
			item.RollingSuccessRate = circuit.RollingSuccessRate()
			item.RollingSamples = circuit.RollingSamples()
			item.CircuitOpenUntil = msToRFC3339(r.circuitOpenUntil(key))
			item.LastErrorKind = circuit.LastErrorKind
			item.LastErrorAt = msToRFC3339(circuit.LastErrorAt)
			item.RejectStreak = circuit.RejectStreak
			for kind, count := range circuit.FailsByKind {
				item.FailsByKind[kind] = count
			}
		}
		item.PacedUntil = msToRFC3339(r.paceUntil(key))
		// 出站准入运行态（routing-spec §3.2）：按 (channelId, 成员所选模型) 读。
		cfg := RateLimitConfig{Mode: AdmissionModeDisabled}
		if member.RateLimit != nil {
			cfg = RateLimitConfig{Mode: AdmissionModeFixed,
				MaxConcurrency: member.RateLimit.MaxConcurrency, MaxRPM: member.RateLimit.MaxRPM,
				QueueLimit: member.RateLimit.QueueLimit, QueueWaitSeconds: member.RateLimit.QueueWaitSeconds}
			if member.RateLimit.MaxConcurrency < 0 {
				cfg.Mode = AdmissionModeAdaptive
			}
		}
		stats := DefaultAdmission.Stats(AdmissionKeyOf(member), cfg)
		item.Inflight = stats.Inflight
		item.Queued = stats.Queued
		item.RPMUsed = stats.RPMUsed
		if stats.Adaptive {
			limit := int(stats.AdaptiveLimit)
			item.AdaptiveLimit = &limit
			item.TTFTBaselineMs = stats.TTFTBaselineMs
		}
		// 人工停用直接令 Available=false，但上面与下面的运行态字段仍照实计算——
		// 排障时要能同时看到"我把它关了"和"关之前它是什么状态"（routing-spec §7）。
		if manual {
			// manual 只有被点名的那个成员可用；点名缺失/悬空时整条车道没有可用成员。
			probeBlocks := r.probe != nil && r.probe.expiresAt > nowMs() && r.probe.key == key &&
				(r.circuitBlocksManual(key) || r.circuitState(key) == CircuitHalfOpen)
			item.Available = i == manualActiveIndex &&
				!member.Disabled &&
				member.ChannelEnabled && member.ConfigError == nil && member.OverrideError == nil &&
				!r.circuitBlocksManual(key) && !probeBlocks
		} else {
			avail := r.availabilityOf(key, settings)
			item.CooldownUntil = msToRFC3339(r.cooldownUntil(key))
			// 精确口径（routing-spec §7）：未人工停用 + 渠道启用 + 无配置错误 +
			// 冷却/熔断不拦 + 探测槽未被他处占用（减速不影响可选性）。
			item.Available = !member.Disabled &&
				member.ChannelEnabled && member.ConfigError == nil && member.OverrideError == nil &&
				avail != availSkip &&
				!(avail == availProbeReady && r.probe != nil && r.probe.expiresAt > nowMs())
		}
		snapshot.Members = append(snapshot.Members, item)
	}
	return snapshot
}

func (r *Runtime) currentLabel(resolved *model.ResolvedRoute) string {
	if !r.HasCurrent {
		return ""
	}
	for i := range resolved.Members {
		if memberKeyOf(&resolved.Members[i]) == r.CurrentMember {
			return memberLabel(&resolved.Members[i])
		}
	}
	return r.CurrentMember.String()
}

func (r *Runtime) probeLabel(resolved *model.ResolvedRoute) string {
	if r.probe == nil || r.probe.expiresAt <= nowMs() {
		return ""
	}
	for i := range resolved.Members {
		if memberKeyOf(&resolved.Members[i]) == r.probe.key {
			return memberLabel(&resolved.Members[i])
		}
	}
	return r.probe.key.String()
}
