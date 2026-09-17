package route

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"pbr/model"
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
	EventCircuitOpen     = "circuit_open"
	EventCircuitHalfOpen = "circuit_half_open"
	EventCircuitClosed   = "circuit_closed"
	EventCooldown        = "cooldown"
	EventSkip            = "skip"
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
)

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
}

// 熔断参数的 system/options 键（design-v1 §7.5：阈值与时长经 system/options 配置）。
const (
	OptionCircuitFailureThreshold = "PBRCircuitFailureThreshold"
	OptionCircuitOpenSeconds      = "PBRCircuitOpenSeconds"
	OptionCircuitMaxOpenSeconds   = "PBRCircuitMaxOpenSeconds"
	// OptionLogRetentionDays 明细日志保留天数（design-v1 §16.5，默认 30）。
	// 不引入 cron：仅作配置，实际清理由外部调 POST /logs/prune 触发。
	OptionLogRetentionDays = "PBRLogRetentionDays"
	// OptionProbeConcurrency 探活并发上限（design-v1 §16.6，默认 4）：
	// 避免一次性对上游造成突发压力。
	OptionProbeConcurrency = "PBRProbeConcurrency"
)

// DefaultLogRetentionDays 明细日志默认保留天数（design-v1 §16.5）。
const DefaultLogRetentionDays = 30

// DefaultProbeConcurrency 探活默认并发上限（design-v1 §16.6）。
const DefaultProbeConcurrency = 4

// DefaultCircuitSettings 默认值。阈值取 2：两次硬故障、或约三次 5xx 即打开，
// 对单用户网关足够敏感，又不会被偶发限流误触发（429 权重 0.2，需 10 次）。
func DefaultCircuitSettings() CircuitSettings {
	return CircuitSettings{FailureThreshold: 2, OpenSeconds: 0, MaxOpenSeconds: 1800}
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
	LastTransitionAt    int64     `json:"last_transition_at"`
	// RecentOutcomes 定长环形缓冲的最近结果（true=成功），用于计算
	// rolling_success_rate（routing-spec §7）。只保留最近 rollingWindowSize 次。
	RecentOutcomes []bool `json:"-"`
}

// rollingWindowSize 滚动成功率的窗口大小（定长环形缓冲，避免无界增长）。
const rollingWindowSize = 20

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

// Event 运行态事件（带时间戳，作为验收与排障证据）。
// Lane 为事件所属车道的路由键；Lane 为空表示历史事件或测试注入
// （design-v1 §16.10：webhook 事件的 lane 字段来自这里）。
type Event struct {
	Ts     int64  `json:"ts"`
	Type   string `json:"type"`
	Lane   string `json:"lane,omitempty"`
	Member string `json:"member"`
	Detail string `json:"detail,omitempty"`
}

// Runtime 一条车道（一个路由键）的运行态，全部请求共享。
type Runtime struct {
	mu sync.Mutex

	// laneName 本车道键（由 Registry.For 建立时写入），事件透传给订阅方。
	laneName string

	CurrentMember string
	HasCurrent    bool

	ProbeMember string
	HasProbe    bool

	AffinityUntil int64
	AffinityArmed bool

	Cooldowns map[string]int64
	Circuits  map[string]*Circuit
	Events    []Event
}

func newRuntime() *Runtime {
	return &Runtime{Cooldowns: map[string]int64{}, Circuits: map[string]*Circuit{}, Events: []Event{}}
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
func (r *Runtime) circuitOpen(key string) bool {
	circuit := r.Circuits[key]
	return circuit != nil && circuit.State == CircuitOpen
}

// circuitState 该成员当前熔断态；无条目返回 closed。
func (r *Runtime) circuitState(key string) string {
	circuit := r.Circuits[key]
	if circuit == nil {
		return CircuitClosed
	}
	return circuit.State
}

// circuitOpenUntil 熔断退避截止时刻（ms）；未打开时返回 0。
func (r *Runtime) circuitOpenUntil(key string) int64 {
	circuit := r.Circuits[key]
	if circuit == nil || circuit.State != CircuitOpen {
		return 0
	}
	return circuit.OpenUntil
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

// nowMs 可被测试替换。
var nowMs = func() int64 { return time.Now().UnixMilli() }

func memberKeyOf(m *model.RouteMember) string {
	if m == nil {
		return ""
	}
	return strconv.Itoa(m.ChannelId) + ":" + m.UpstreamModel
}

// weightOf 按错误分类给出熔断计分权重。
func weightOf(kind ErrorKind) float64 {
	switch kind {
	case KindHardAuth, KindHardQuota, KindBadResponse:
		return weightHard
	case KindSoftTransient:
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

func (r *Runtime) availabilityOf(key string, memberCooldownSeconds int, settings CircuitSettings) availability {
	now := nowMs()
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
	until := r.Cooldowns[key]
	if until == 0 {
		return availOK
	}
	if now < until {
		return availSkip
	}
	return availProbeReady
}

// SkipReason 成员被跳过时的原因，用于 attempts[].status（routing-spec §9）：
// `circuit_break`（熔断打开）、`cooldown`（冷却未到期）、`skipped`（其他不可选，
// 例如探测槽已被占用）。文档要求据此解释"为什么没用优先级最高的那个成员"。
func (r *Runtime) SkipReason(key string) string {
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

// takeProbe 占用单探测槽；已被占用则返回 false（§6：每车道同时只允许一个探测请求，
// 对同一成员也不例外——否则"到期的高优先级成员"会被并发请求同时涌入）。
func (r *Runtime) takeProbe(key string) bool {
	if r.HasProbe {
		return false
	}
	r.HasProbe = true
	r.ProbeMember = key
	return true
}

func (r *Runtime) releaseProbe(key string) {
	if r.HasProbe && r.ProbeMember == key {
		r.HasProbe = false
		r.ProbeMember = ""
	}
}

// recordFailure 记一次失败：更新熔断计分，必要时打开熔断；尝试预算耗尽则进入冷却。
func (r *Runtime) recordFailure(key string, kind ErrorKind, memberCooldownSeconds int, settings CircuitSettings) {
	now := nowMs()
	// routing-spec §4.1：hard_auth / hard_quota 冷却取较长档。
	memberCooldownSeconds = cooldownSecondsFor(kind, memberCooldownSeconds)
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
	circuit.Score += weightOf(kind)
	circuit.ConsecutiveFailures++
	circuit.recordOutcome(false)

	if memberCooldownSeconds > 0 {
		until := now + int64(memberCooldownSeconds)*1000
		r.Cooldowns[key] = until
		r.appendEvent(EventCooldown, key, "cooldown_until="+strconv.FormatInt(until, 10))
	}

	if circuit.State != CircuitOpen && circuit.Score >= settings.FailureThreshold {
		r.openCircuit(key, circuit, now, memberCooldownSeconds, settings)
	}
}

// openCircuit 打开熔断，并按打开次数做指数退避（open_seconds × 2^k，设上限）。
func (r *Runtime) openCircuit(key string, circuit *Circuit, now int64, memberCooldownSeconds int, settings CircuitSettings) {
	base := settings.OpenSeconds
	if base <= 0 {
		base = memberCooldownSeconds
	}
	if base <= 0 {
		base = 60
	}
	duration := base
	for i := 0; i < circuit.OpenCount; i++ {
		duration *= openBackoffK
		if duration >= settings.MaxOpenSeconds {
			duration = settings.MaxOpenSeconds
			break
		}
	}
	if duration > settings.MaxOpenSeconds {
		duration = settings.MaxOpenSeconds
	}
	circuit.State = CircuitOpen
	circuit.OpenCount++
	circuit.OpenedAt = now
	circuit.OpenUntil = now + int64(duration)*1000
	circuit.LastTransitionAt = now
	r.appendEvent(EventCircuitOpen, key, "open_seconds="+strconv.Itoa(duration))
}

// recordProbeStart 记一次半开探测开始（时间戳证据）。
func (r *Runtime) recordProbeStart(key string, wasOpen bool) {
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
func (r *Runtime) recordSuccess(key string, affinitySeconds int) {
	now := nowMs()
	delete(r.Cooldowns, key)
	circuit := r.Circuits[key]
	if circuit == nil {
		// 全成功的成员此前没有熔断器条目；建一个，使滚动成功率也有样本
		// （routing-spec §7 要求每个成员都暴露 rolling_success_rate）。
		circuit = &Circuit{State: CircuitClosed}
		r.Circuits[key] = circuit
	}
	was := circuit.State
	circuit.State = CircuitClosed
	circuit.Score = 0
	circuit.ConsecutiveFailures = 0
	circuit.OpenUntil = 0
	circuit.recordOutcome(true)
	if was != CircuitClosed {
		circuit.OpenCount = 0
		circuit.LastTransitionAt = now
		r.appendEvent(EventCircuitClosed, key, "recovered")
	}
	r.releaseProbe(key)
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
var eventSubscriber struct {
	mu sync.RWMutex
	fn func(Event)
}

// SetEventSubscriber 注册运行态事件订阅者；传 nil 注销。
// 同一进程只应有一个订阅者（webhook 投递器），后注册者覆盖先注册者。
func SetEventSubscriber(fn func(Event)) {
	eventSubscriber.mu.Lock()
	defer eventSubscriber.mu.Unlock()
	eventSubscriber.fn = fn
}

func (r *Runtime) appendEvent(eventType, member, detail string) {
	event := Event{Ts: nowMs(), Type: eventType, Lane: r.laneName, Member: member, Detail: detail}
	r.Events = append(r.Events, event)
	if len(r.Events) > 200 {
		r.Events = r.Events[len(r.Events)-200:]
	}
	// 旁路转发给订阅者（非阻塞；订阅者自行缓冲，见 eventSubscriber 注释）。
	eventSubscriber.mu.RLock()
	fn := eventSubscriber.fn
	eventSubscriber.mu.RUnlock()
	if fn != nil {
		fn(event)
	}
}

// Reset 清空该车道的全部熔断与冷却（POST /lanes/{name}/circuits/reset）。
// Reset 清空该车道的全部运行态（冷却/熔断/探测槽/亲和），返回被清除的
// 熔断器条目数（api-spec §6.6 的响应体 `{"reset": <int>}`）。
func (r *Runtime) Reset() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	cleared := len(r.Circuits)
	r.Cooldowns = map[string]int64{}
	r.Circuits = map[string]*Circuit{}
	r.HasProbe = false
	r.ProbeMember = ""
	r.AffinityArmed = false
	r.AffinityUntil = 0
	r.appendEvent("reset", "", "circuits cleared")
	return cleared
}

// PruneStaleState 清理已不在成员链里的残留状态（routing-spec §1.3：
// "成员被删除或渠道被删除时，清理其残留状态"，参照线上 groupRouteLocked）。
//
// 为什么必须清：运行态键是 `channelId:upstreamModel`。成员删除后若重新加回
// 同一渠道+模型（编辑车道时 UpsertLane 先全删再重建成员），键完全相同，
// 新成员会**直接继承**旧成员的冷却与熔断计分——表现为"刚加回来的成员
// 立刻被判为熔断/冷却中"。探测槽与 CurrentMember 同理需要收敛。
//
// 注意只在"解析出的成员集合非空"时清理：模型暂时没有可用渠道（例如渠道被
// 禁用/删除）时解析结果本就为空，此时清空全部状态会把仍有意义的冷却记录
// 一并抹掉，等渠道恢复后失去退避保护。
func (r *Runtime) PruneStaleState(valid map[string]bool) {
	if len(valid) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.Cooldowns {
		if !valid[key] {
			delete(r.Cooldowns, key)
		}
	}
	for key := range r.Circuits {
		if !valid[key] {
			delete(r.Circuits, key)
		}
	}
	if r.HasProbe && !valid[r.ProbeMember] {
		r.HasProbe = false
		r.ProbeMember = ""
	}
	if r.HasCurrent && !valid[r.CurrentMember] {
		r.HasCurrent = false
		r.CurrentMember = ""
		r.AffinityUntil = 0
		r.AffinityArmed = false
	}
}

// MemberHealth 单个成员的运行态快照。
type MemberHealth struct {
	Member              string    `json:"member"`
	Channel             string    `json:"channel"`
	UpstreamModel       string    `json:"upstream_model"`
	Circuit             string    `json:"circuit"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	FailureScore        float64   `json:"failure_score"`
	RollingSuccessRate  float64   `json:"rolling_success_rate"`
	CooldownUntil       int64     `json:"cooldown_until"`
	CircuitOpenUntil    int64     `json:"circuit_open_until"`
	LastErrorKind       ErrorKind `json:"last_error_kind,omitempty"`
	Current             bool      `json:"current"`
	Probing             bool      `json:"probing"`
	Available           bool      `json:"available"`
}

// HealthSnapshot 车道运行态快照（GET /lanes/{name}/health）。
type HealthSnapshot struct {
	Lane          string         `json:"lane"`
	Source        string         `json:"source"`
	Mode          string         `json:"mode"`
	CurrentMember string         `json:"current_member"`
	ProbeMember   string         `json:"probe_member"`
	AffinityUntil int64          `json:"affinity_until"`
	Members       []MemberHealth `json:"members"`
	Events        []Event        `json:"events"`
}

// Health 生成快照；不复位的只读操作。
func (r *Runtime) Health(resolved *model.ResolvedRoute, settings CircuitSettings) HealthSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	cooldownSeconds := resolved.Config.Normalize().MemberCooldownSeconds
	snapshot := HealthSnapshot{
		Lane:          resolved.Model,
		Source:        resolved.Source,
		Mode:          resolved.Mode,
		CurrentMember: r.currentLabel(resolved),
		ProbeMember:   r.probeLabel(resolved),
		AffinityUntil: r.AffinityUntil,
		Events:        append([]Event(nil), r.Events...),
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
			Probing:       r.HasProbe && r.ProbeMember == key,
		}
		if circuit := r.Circuits[key]; circuit != nil {
			item.Circuit = circuit.State
			item.ConsecutiveFailures = circuit.ConsecutiveFailures
			item.FailureScore = circuit.Score
			item.RollingSuccessRate = circuit.RollingSuccessRate()
			item.CircuitOpenUntil = circuit.OpenUntil
			item.LastErrorKind = circuit.LastErrorKind
		}
		item.CooldownUntil = r.Cooldowns[key]
		item.Available = r.availabilityOf(key, cooldownSeconds, settings) != availSkip
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
	return r.CurrentMember
}

func (r *Runtime) probeLabel(resolved *model.ResolvedRoute) string {
	if !r.HasProbe {
		return ""
	}
	for i := range resolved.Members {
		if memberKeyOf(&resolved.Members[i]) == r.ProbeMember {
			return memberLabel(&resolved.Members[i])
		}
	}
	return r.ProbeMember
}
