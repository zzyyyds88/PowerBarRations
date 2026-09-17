// Package route 实现 PowerBarRations 的请求内选路运行态。
//
// 与 model.ResolveRoute 的分工：ResolveRoute 只回答"这个模型名对应哪些成员、
// 顺序如何"；本包回答"本次请求的每一次尝试应该打哪个成员、失败后要不要重试
// 同一成员、要不要换人、什么时候快抛 503"。
//
// 语义依据：docs/routing-spec-v1.md §2（选择算法）、§3（尝试循环）、§4（错误分类）、
// §5（熔断）、§6（冷却与亲和）。
//
// 两种模式（failover / manual）共用同一套可用性判断
// （冷却 + 熔断，见 runtime.go）与同一套尝试预算，仅"选谁"这一步不同。
package route

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
)

// contextKey 请求内路由态在 gin.Context 中的键。
const contextKey = "pbr_request_route"

// ErrorCodeNoAvailableChannel 模型面无可用成员。body 形态由 WriteNoAvailableChannel 固定，
// 不走 NewAPIError 的通用序列化（见 routing-spec §4.2：下游 fallback 分类依赖该 body）。
const ErrorCodeNoAvailableChannel types.ErrorCode = "no_available_member"

// ErrorKind 错误分类（routing-spec §4.1）。
type ErrorKind string

const (
	KindSoftRateLimit ErrorKind = "soft_rate_limit"
	KindSoftTransient ErrorKind = "soft_transient"
	KindHardAuth      ErrorKind = "hard_auth"
	KindHardQuota     ErrorKind = "hard_quota"
	KindClientError   ErrorKind = "client_error"
	KindBadResponse   ErrorKind = "bad_response"
	KindCanceled      ErrorKind = "canceled"
)

// quotaKeywordMatcher 由外部注入的"欠费/额度"关键词判定（deployment 私有条目）。
// 关键词优先于状态码：欠费类上游常以 400 到达，状态码路径捕不到（design-v1 §7.6）。
var quotaKeywordMatcher func(message string) bool

// SetQuotaKeywordMatcher 注入关键词判定实现（由 middleware 在启动时接到基座的关键词表）。
func SetQuotaKeywordMatcher(matcher func(string) bool) {
	quotaKeywordMatcher = matcher
}

// Classify 按 routing-spec §4.1 把一次尝试的错误归类。
func Classify(err *types.NewAPIError) ErrorKind {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		// 顺序很重要：我方每成员超时是把 context.Canceled 翻译成 DeadlineExceeded
		// 的错误链（两种都 errors.Is 命中）。必须先判 DeadlineExceeded，否则自己的
		// 超时会被当成"客户端断开"——不重试、不换人、不冷却（routing-spec §8）。
		if errors.Is(err, context.DeadlineExceeded) {
			// 超时是我们自己设的 deadline（含等待响应头阶段的翻译），属真实失败。
			return KindSoftTransient
		}
		return KindCanceled
	}
	// 关键词优先于状态码（欠费类以 400 到达）。
	if quotaKeywordMatcher != nil && quotaKeywordMatcher(err.Error()) {
		return KindHardQuota
	}
	if err.GetErrorCode() == ErrorCodeNoAvailableChannel {
		// 成员耗尽：语义上是"服务不可用"而非"请求不合法"，日志里不能记成 client_error。
		return KindSoftTransient
	}
	switch err.GetErrorCode() {
	case types.ErrorCodeBadResponse, types.ErrorCodeBadResponseBody, types.ErrorCodeEmptyResponse,
		types.ErrorCodeReadResponseBodyFailed:
		// 200 但响应不合法/无法解析（routing-spec §4.1 的 bad_response）。
		return KindBadResponse
	case types.ErrorCodeDoRequestFailed:
		// 传输层失败：连接失败、超时、EOF 都归软故障（§4.1 明确把"连接超时/EOF"列为 soft_transient）。
		// 注意：这里的超时来自我们为每次尝试设的 deadline，属"真实失败"，必须计入冷却。
		return KindSoftTransient
	case types.ErrorCodeChannelResponseTimeExceeded:
		return KindSoftTransient
	case types.ErrorCodeChannelNoAvailableKey, types.ErrorCodeChannelInvalidKey:
		return KindHardAuth
	}
	// 注意：基座的 types.NewError 默认把 StatusCode 设成 500，我方内部错误（请求转换失败等）
	// 都是"500 + skipRetry"。因此 skipRetry 必须优先于状态码判定，否则我方校验类错误
	// 会被当成上游软故障，触发换人与冷却——正是 routing-spec §4.1 禁止的"坏请求打冷健康成员"。
	if types.IsSkipRetryError(err) {
		return KindClientError
	}
	switch code := err.StatusCode; {
	case code == http.StatusTooManyRequests:
		return KindSoftRateLimit
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return KindHardAuth
	case code == http.StatusBadRequest || code == http.StatusUnprocessableEntity:
		// routing-spec §4.1：只有 400/422（且未命中欠费关键词）才是"请求本身不合法"
		// 的 client_error。401/403/408/429 已在上方分流。
		return KindClientError
	case code == http.StatusRequestTimeout, code == http.StatusServiceUnavailable, code >= 500:
		return KindSoftTransient
	default:
		// 其余 4xx（404 成员无此模型、405/409 等）与状态码 0/异常：一律按可逃逸的
		// 瞬时失败处理（routing-spec §4.1 只把 400/422 定义为 client_error）。
		// 若把 404 这类"上游确实没有该模型"的失败归成 client_error，请求会不换人、
		// 不冷却，直接撞死在第一个成员上。
		return KindSoftTransient
	}
}

// ShouldSwitchMember 该错误是否应换下一个成员（routing-spec §4.1 最后一列）。
func ShouldSwitchMember(kind ErrorKind) bool {
	switch kind {
	case KindSoftRateLimit, KindSoftTransient, KindHardAuth, KindHardQuota, KindBadResponse:
		return true
	default:
		return false
	}
}

// ShouldRetrySameMember 该错误是否应先在**同一成员**上重试（§4.1 第二列）。
func ShouldRetrySameMember(kind ErrorKind) bool {
	switch kind {
	case KindSoftRateLimit, KindSoftTransient, KindBadResponse:
		return true
	default:
		return false
	}
}

// Attempt 一次尝试的记录（W5 落 request_logs.attempts 用）。
type Attempt struct {
	AttemptNum int       `json:"attempt_num"`
	Member     string    `json:"member"`
	Status     string    `json:"status"`
	DurationMs int64     `json:"duration_ms,omitempty"`
	ErrorKind  ErrorKind `json:"error_kind,omitempty"`
	Msg        string    `json:"msg,omitempty"`
}

// State 一次请求内的选路运行态。请求作用域，随 gin.Context 生命周期结束。
type State struct {
	Route   *model.ResolvedRoute
	Runtime *Runtime

	mu       sync.Mutex
	order    []int // Route.Members 的下标，按尝试顺序
	current  int   // 当前成员在 Route.Members 中的下标；-1 表示尚未选过
	attempts map[int]int
	history  []Attempt
	// cooldownSeconds 车道级冷却时长（写入运行态时用；成员级覆盖在 configFor）。
	cooldownSeconds int
	// attemptStartedAt 本轮尝试的开始时刻，用于 attempts[].duration_ms
	// （routing-spec §9 / api-spec §4.4）。此前该字段声明了却从不赋值，
	// 带 omitempty 导致响应里根本不出现，排障时无法定位"哪个成员慢"。
	attemptStartedAt time.Time
	// claimedProbe 本次请求真正占用的探测槽所属成员键（空 = 未占）。
	//
	// 探测槽是**车道级单槽**，但只有占槽者可以归还它：一个从未探测的并发请求
	// 若也去清 ProbeMember，就会把别人的槽释放掉，"单探测"在并发下失效
	// （routing-spec §2.2/§6）。因此归属必须记在请求态上。
	claimedProbe string
	// skipRecorded 已记过"被跳过"的成员键，避免同一请求多轮重试时
	// 把同一个冷却中的成员重复写进尝试链。
	skipRecorded map[string]bool
	// memberCfg 每个成员生效的六键（车道默认叠加成员级覆盖）。
	memberCfg []model.LaneRelayConfig
}

func laneKeyOf(resolved *model.ResolvedRoute) string {
	if resolved.RouteKey != "" {
		return resolved.RouteKey
	}
	return resolved.Model
}

// budgetFor 该成员在本请求内的尝试预算（成员级覆盖优先）。
func (s *State) budgetFor(idx int) int {
	if budget := s.configFor(idx).MemberMaxAttempts; budget > 0 {
		return budget
	}
	return 1
}

func (s *State) configFor(idx int) model.LaneRelayConfig {
	if idx >= 0 && idx < len(s.memberCfg) {
		return s.memberCfg[idx]
	}
	return s.Route.Config.Normalize()
}

// NewState 基于解析结果建立请求态。运行态取自进程级注册表（按路由键分车道）。
//
// 只有**真实存在且成员非空的显式车道**才登记运行态：未配车道的模型名
// （Source=unconfigured）在请求里出现任意多次都不得永久新增 Runtime，否则
// SnapshotLanes 会随"请求过的模型名数"无界增长，SSE 快照退化成每个模型名
// 一次 DB 查询（routing-spec §1.3）。空链请求不会选中成员，给一个不登记的
// 临时运行态即可，既避免 nil 解引用又不污染注册表。
func NewState(resolved *model.ResolvedRoute) *State {
	cfg := resolved.Config.Normalize()
	routeKey := laneKeyOf(resolved)
	var runtime *Runtime
	if resolved.Source == model.RouteSourceExplicit && len(resolved.Members) > 0 {
		runtime = Default.For(routeKey)
	} else {
		runtime = newRuntime()
		runtime.laneName = routeKey
	}
	s := &State{
		Route:           resolved,
		Runtime:         runtime,
		current:         -1,
		attempts:        map[int]int{},
		cooldownSeconds: cfg.MemberCooldownSeconds,
	}
	order := make([]int, 0, len(resolved.Members))
	if resolved.PinnedMemberId != 0 {
		for i, m := range resolved.Members {
			if m.MemberId == resolved.PinnedMemberId {
				order = append(order, i)
				break
			}
		}
	}
	for i := range resolved.Members {
		if len(order) > 0 && order[0] == i {
			continue
		}
		order = append(order, i)
	}
	s.order = order
	s.memberCfg = make([]model.LaneRelayConfig, len(resolved.Members))
	for i := range resolved.Members {
		s.memberCfg[i] = resolved.EffectiveConfig(&resolved.Members[i])
	}
	// 清理已不在成员链里的残留运行态（routing-spec §1.3）：成员删除后重新
	// 加回同一 (channel_id, upstream_model) 会复用同一个键，不清就会继承
	// 旧的冷却/熔断状态。
	valid := make(map[string]bool, len(resolved.Members))
	for i := range resolved.Members {
		valid[memberKeyOf(&resolved.Members[i])] = true
	}
	// 传入车道版本：只有"比上次处理过的版本更新、且成员集合确实变化"时才清理一次，
	// 避免旧配置的并发请求用旧成员集合删掉新配置刚写入的成员运行态（routing-spec §1.3）。
	s.Runtime.PruneStaleState(valid, resolved.LaneVersion)
	return s
}

// Current 返回当前成员（未选过时为 nil）。
func (s *State) Current() *model.RouteMember {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentMemberLocked()
}

func (s *State) currentMemberLocked() *model.RouteMember {
	if s.current < 0 || s.current >= len(s.Route.Members) {
		return nil
	}
	return &s.Route.Members[s.current]
}

// Next 返回本轮应尝试的成员。lastErr 为 nil 表示这是本请求的首次尝试。
//
// 第二个返回值是本轮开始前应等待的时长（同成员重试间隔，routing-spec §3.1）。
// 返回 ok=false 表示"没有可用成员/不该继续尝试"，调用方应按 §4.2 快抛 503
// （客户端的错则原样返回，见 §4.1 的 client_error / canceled 两行）。
func (s *State) Next(lastErr *types.NewAPIError) (*model.RouteMember, time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current >= 0 && lastErr != nil {
		kind := Classify(lastErr)
		if !ShouldSwitchMember(kind) {
			// client_error / canceled：不换人、不冷却，直接把错误交回客户端。
			// 这仍是一次真实尝试，照记耗时（否则最后一次尝试的 duration_ms 缺失）。
			attemptedKey := memberKeyOf(s.currentMemberLocked())
			s.history = append(s.history,
				failedAttemptWithDuration(len(s.history)+1, memberLabel(s.currentMemberLocked()), kind, lastErr, s.elapsedMs()))
			s.markAttemptedLocked(attemptedKey)
			return nil, 0, false
		}
		member := s.currentMemberLocked()
		key := memberKeyOf(member)
		budgetLeft := s.attempts[s.current] < s.budgetFor(s.current)
		s.history = append(s.history,
			failedAttemptWithDuration(len(s.history)+1, memberLabel(member), kind, lastErr, s.elapsedMs()))
		s.markAttemptedLocked(key)

		memberCfg := s.configFor(s.current)
		if budgetLeft && ShouldRetrySameMember(kind) {
			// 同成员原地重试：按 retry_interval 间隔后再打一次（§3.1）。
			s.attempts[s.current]++
			s.attemptStartedAt = time.Now()
			return member, time.Duration(memberCfg.MemberRetryIntervalSeconds) * time.Second, true
		}
		// 尝试预算耗尽或硬故障：写入冷却与熔断计分后换人。
		s.Runtime.withLock(func() {
			s.Runtime.recordFailure(key, kind, memberCfg.MemberCooldownSeconds, CurrentCircuitSettings())
			s.Runtime.releaseProbe(key)
			if s.claimedProbe == key {
				s.claimedProbe = ""
			}
		})
	}

	member, ok := s.pickLocked()
	if !ok {
		return nil, 0, false
	}
	// 新一轮尝试开始计时（duration_ms = 从选中到出结果）。
	s.attemptStartedAt = time.Now()
	return member, 0, true
}

// elapsedMs 本轮尝试已耗时（毫秒）。未开始计时时返回 0。
func (s *State) elapsedMs() int64 {
	if s.attemptStartedAt.IsZero() {
		return 0
	}
	return time.Since(s.attemptStartedAt).Milliseconds()
}

// markAttemptedLocked 记下"这个成员本请求已经真实尝试过"。
//
// 已在尝试链里留下 failed/success 的成员，不需要再补一条"被跳过"的记录：
// 它为什么没被继续用，前面那条失败记录已经解释清楚了。不记的话，一个成员
// 失败进冷却后，紧接着的下一轮选路又会为它写一条 cooldown，链上出现
// [failed(成员A), cooldown(成员A)] 这种自相重复。调用方持 s.mu。
func (s *State) markAttemptedLocked(key string) {
	if s.skipRecorded == nil {
		s.skipRecorded = map[string]bool{}
	}
	s.skipRecorded[key] = true
}

// recordSkipLocked 记一条"被跳过"的尝试（routing-spec §9 的 cooldown /
// circuit_break / skipped）。调用方持 s.mu **且持 Runtime 锁**（本函数从
// chooseFailoverLocked 的 withLock 块内调用，Runtime 的锁不可重入，
// 此处不得再次 withLock，否则自死锁）。
//
// 同一成员在同一请求内只记一次：多轮重试会反复跳过同一个冷却中的成员，
// 重复写会把尝试链撑成噪声。
func (s *State) recordSkipLocked(key, member string) {
	if s.skipRecorded == nil {
		s.skipRecorded = map[string]bool{}
	}
	if s.skipRecorded[key] {
		return
	}
	s.skipRecorded[key] = true
	status := "skipped"
	if s.Runtime != nil {
		status = s.Runtime.SkipReason(key)
	}
	s.history = append(s.history, Attempt{
		AttemptNum: len(s.history) + 1,
		Member:     member,
		Status:     status,
	})
}

func failedAttemptWithDuration(num int, member string, kind ErrorKind, err *types.NewAPIError, durationMs int64) Attempt {
	return Attempt{
		AttemptNum: num,
		Member:     member,
		Status:     "failed",
		DurationMs: durationMs,
		ErrorKind:  kind,
		Msg:        truncate(err, 512),
	}
}

// pickLocked 按车道模式选出下一个可用成员。调用方持 s.mu；Runtime.mu 由内部按需获取。
func (s *State) pickLocked() (*model.RouteMember, bool) {
	switch s.Route.Mode {
	case model.LaneModeManual:
		return s.pickManualLocked()
	default:
		return s.pickFailoverLocked()
	}
}

// pickFailoverLocked 按 priority 降序遍历，取第一个可用成员（routing-spec §2.2）。
func (s *State) pickFailoverLocked() (*model.RouteMember, bool) {
	chosen := s.chooseFailoverLocked()
	if chosen < 0 {
		return nil, false
	}
	s.attempts[chosen]++
	s.markCurrentLocked(chosen)
	return &s.Route.Members[chosen], true
}

// chooseFailoverLocked 选出本轮成员并处理探测槽。调用方持 s.mu；
// Runtime.mu 由内部获取。返回 -1 表示无可选成员。
func (s *State) chooseFailoverLocked() int {
	chosen := -1
	s.Runtime.withLock(func() {
		// 亲和期内沿用当前成员（§6：不提前切回高优先级成员）。
		if s.Runtime.HasCurrent && s.Runtime.AffinityUntil > nowMs() {
			if idx := s.indexOfKey(s.Runtime.CurrentMember); idx >= 0 {
				key := memberKeyOf(&s.Route.Members[idx])
				if s.attempts[idx] < s.budgetFor(idx) &&
					s.Runtime.availabilityOf(key, CurrentCircuitSettings()) != availSkip {
					// 亲和绕过的高优先级成员必须留痕，否则 attempts 链无法解释
					// "为什么没用 P1"（routing-spec §6/§9）。
					s.recordAffinitySkipsLocked(idx)
					chosen = idx
					return
				}
			}
		}
		for _, idx := range s.order {
			if s.attempts[idx] >= s.budgetFor(idx) {
				continue
			}
			key := memberKeyOf(&s.Route.Members[idx])
			switch s.Runtime.availabilityOf(key, CurrentCircuitSettings()) {
			case availSkip:
				// 记录"为什么没用这个成员"（routing-spec §9）：cooldown / circuit_break。
				s.recordSkipLocked(key, memberLabel(&s.Route.Members[idx]))
				continue
			case availProbeReady:
				if !s.Runtime.takeProbe(key) {
					// 探测槽已被占用：本轮跳过，同样留痕。
					s.recordSkipLocked(key, memberLabel(&s.Route.Members[idx]))
					continue
				}
				s.claimedProbe = key
				s.Runtime.recordProbeStart(key, s.Runtime.circuitOpen(key))
				chosen = idx
				return
			default:
				chosen = idx
				return
			}
		}
	})
	return chosen
}

// recordAffinitySkipsLocked 为亲和期内被绕过、且本轮仍有预算的高优先级成员
// （按 s.order 排在 affinityIdx 之前者）补一条"被跳过"记录。调用方持 s.mu
// **且持 Runtime 锁**（本函数从 chooseFailoverLocked 的 withLock 块内调用）。
func (s *State) recordAffinitySkipsLocked(affinityIdx int) {
	for _, idx := range s.order {
		if idx == affinityIdx {
			return
		}
		if s.attempts[idx] >= s.budgetFor(idx) {
			continue
		}
		s.recordSkipLocked(memberKeyOf(&s.Route.Members[idx]), memberLabel(&s.Route.Members[idx]))
	}
}

// pickManualLocked 只用手工指定的成员；不可用即"无可用"，不静默换人（routing-spec §2.1）。
func (s *State) pickManualLocked() (*model.RouteMember, bool) {
	idx := s.activeMemberIndex()
	if idx < 0 || s.attempts[idx] >= s.budgetFor(idx) {
		return nil, false
	}
	key := memberKeyOf(&s.Route.Members[idx])
	ok := false
	s.Runtime.withLock(func() {
		// §2.1：manual **不参与冷却**（人工指定的成员就是唯一选择，冷却在此
		// 没有可换的替代者，只会把请求变成 503），但仍过熔断。
		// 此前直接用 availabilityOf，把冷却也一并判了，导致指定成员一次失败后
		// 冷却期内所有请求都 503——而它其实是好的。
		//
		// 半开状态必须走单探测槽：否则第一次探测把熔断翻成 half_open 后，
		// 并发请求会绕过探测槽直接选中，形成"半开涌入"（routing-spec §2.1/§5.2）。
		switch {
		case s.Runtime.circuitState(key) == CircuitOpen && nowMs() < s.Runtime.circuitOpenUntil(key):
			return // 熔断打开且退避未到 → 无可用
		case s.Runtime.circuitState(key) == CircuitOpen,
			s.Runtime.circuitState(key) == CircuitHalfOpen:
			// 退避期到或半开在途：只放行唯一探测者，槽被占则本轮无可用。
			if !s.Runtime.takeProbe(key) {
				return
			}
			s.claimedProbe = key
			s.Runtime.recordProbeStart(key, s.Runtime.circuitState(key) == CircuitOpen)
		}
		ok = true
	})
	if !ok {
		return nil, false
	}
	s.attempts[idx]++
	s.markCurrentLocked(idx)
	return &s.Route.Members[idx], true
}

// markCurrentLocked 记录当前成员；发生故障切换时武装亲和（§6：切换后首次成功才启动）。
func (s *State) markCurrentLocked(idx int) {
	key := memberKeyOf(&s.Route.Members[idx])
	s.Runtime.withLock(func() {
		if s.Runtime.HasCurrent && s.Runtime.CurrentMember != key {
			s.Runtime.AffinityArmed = true
		}
		s.Runtime.CurrentMember = key
		s.Runtime.HasCurrent = true
		s.current = idx
	})
}

func (s *State) indexOfKey(key string) int {
	for i := range s.Route.Members {
		if memberKeyOf(&s.Route.Members[i]) == key {
			return i
		}
	}
	return -1
}

// activeMemberIndex 解析 manual 模式的 active_member：先按成员别名，再按 channel/upstream 标签。
func (s *State) activeMemberIndex() int {
	active := strings.TrimSpace(s.Route.ActiveMember)
	if active == "" {
		return -1
	}
	for i := range s.Route.Members {
		if s.Route.Members[i].PublicAlias != "" && s.Route.Members[i].PublicAlias == active {
			return i
		}
	}
	for i := range s.Route.Members {
		if memberLabel(&s.Route.Members[i]) == active {
			return i
		}
	}
	return -1
}

// OnSuccess 上报一次成功：解除冷却、复位熔断、按需启动亲和。
func (s *State) OnSuccess() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current < 0 || s.current >= len(s.Route.Members) {
		return
	}
	key := memberKeyOf(&s.Route.Members[s.current])
	affinitySeconds := s.configFor(s.current).MemberAffinitySeconds
	s.Runtime.withLock(func() {
		s.Runtime.recordSuccess(key, affinitySeconds)
		if s.claimedProbe == key {
			s.claimedProbe = ""
		}
	})
}

// SuccessAttempt 返回最后一次成功尝试的记录，含本轮耗时（routing-spec §9）。
//
// 成功不像失败那样进 history（history 只存失败尝试），所以由调用方在收尾时
// 取用；duration_ms 依赖 Next 开始计时，未经过 Next 时返回 0。
func (s *State) SuccessAttempt(member string) Attempt {
	if s == nil {
		return Attempt{Member: member, Status: "success"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Attempt{
		Member:     member,
		Status:     "success",
		DurationMs: s.elapsedMs(),
	}
}

// ReportMemberUnavailable 记一次"成员自身不可用"（渠道被禁用/删除、循环内
// 取不到 key 或注入上下文失败）。routing-spec §3 第 4.2 步要求这算该成员的一次
// 失败并计入冷却/熔断，否则被禁用渠道的车道会无限重选同一成员。
//
// 成员当前不在 s.current（还没被选中就发现不可用）时，也补一条 failed 尝试，
// 让 attempts 链能解释"为什么这个成员被跳过"。
func (s *State) ReportMemberUnavailable(member *model.RouteMember, reason string) {
	if s == nil || member == nil || s.Runtime == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := memberKeyOf(member)
	s.Runtime.withLock(func() {
		s.history = append(s.history, Attempt{
			AttemptNum: len(s.history) + 1,
			Member:     memberLabel(member),
			Status:     "failed",
			ErrorKind:  KindSoftTransient,
			Msg:        "member unavailable: " + reason,
		})
		s.Runtime.recordFailure(key, KindSoftTransient, s.cooldownSeconds, CurrentCircuitSettings())
	})
	s.markAttemptedLocked(key)
}

// ReleaseProbe 归还本请求占用的探测槽（幂等）。
//
// 调用方必须在这些路径上调用，否则探测槽会被永久占住、该成员此后永远取不到探测机会：
// 候选成员自身不可用被跳过、错误不换人（client_error/canceled）、请求收尾。
//
// **只归还自己占的那个**（s.claimedProbe）：调用方可能是从未探测过的并发请求
// （收尾 defer 对每个 PBR 请求都会走一遍），若无条件清 ProbeMember，就会把别的
// 在途探测者的槽释放掉，使"每车道同时只放行一个探测"在并发下失效
// （routing-spec §2.2/§6）。
func (s *State) ReleaseProbe() {
	if s == nil || s.Runtime == nil {
		return
	}
	s.mu.Lock()
	key := s.claimedProbe
	s.claimedProbe = ""
	s.mu.Unlock()
	if key == "" {
		return
	}
	s.Runtime.withLock(func() {
		s.Runtime.releaseProbe(key)
	})
}

// MaxRetriesForLoop 给转发循环的 retry 上界（循环总共跑 retry+1 轮）。
//
// 取值 = 成员数 × 单成员预算：预算内的真实尝试最多这么多轮，之后再加一轮用于
// "发现已无可用成员"并按 §4.2 快抛 503——否则最后一次失败会把上游的原始错误
// （如 500）直接透给下游，而不是契约要求的 503。
func (s *State) MaxRetriesForLoop() int {
	if s == nil || s.Route == nil {
		return 0
	}
	n := len(s.Route.Members)
	if n == 0 {
		return 0
	}
	total := 0
	for i := 0; i < n; i++ {
		total += s.budgetFor(i)
	}
	return total
}

// History 已发生的尝试记录。
func (s *State) History() []Attempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Attempt(nil), s.history...)
}

// LogAttempts 把尝试链转成落库形态（request_logs.attempts）。
//
// 供"快抛 503"这类在中间件阶段就结束的请求使用：routing-spec §4.2 要求快抛前
// 必须已把本次所有尝试写入日志（含 cooldown / circuit_break 状态）。
func (s *State) LogAttempts() []model.PBRAttempt {
	history := s.History()
	out := make([]model.PBRAttempt, 0, len(history))
	for _, item := range history {
		out = append(out, model.PBRAttempt{
			AttemptNum: item.AttemptNum,
			Member:     item.Member,
			Status:     item.Status,
			DurationMs: item.DurationMs,
			ErrorKind:  string(item.ErrorKind),
			Msg:        item.Msg,
		})
	}
	return out
}

func memberLabel(m *model.RouteMember) string {
	if m == nil {
		return ""
	}
	if m.Channel != "" {
		return m.Channel + "/" + m.UpstreamModel
	}
	return m.UpstreamModel
}

func truncate(err *types.NewAPIError, max int) string {
	if err == nil {
		return ""
	}
	// 脱敏后再截断：上游错误体可能回显 Authorization/key，而 attempts 链会进日志与 API
	msg := err.MaskSensitiveErrorWithStatusCode()
	if len(msg) <= max {
		return msg
	}
	return msg[:max]
}

// ---------- gin.Context 往来 ----------

// Attach 把请求态挂到上下文。
func Attach(c *gin.Context, s *State) {
	if c == nil || s == nil {
		return
	}
	c.Set(contextKey, s)
}

// From 取出请求态；非 PBR 路由（如任务插件走旧链路）返回 nil。
func From(c *gin.Context) *State {
	if c == nil {
		return nil
	}
	if v, ok := c.Get(contextKey); ok {
		if s, ok := v.(*State); ok {
			return s
		}
	}
	return nil
}

// ---------- 503 快抛 ----------

// NoAvailableError 构造"无可用成员"的错误，供转发循环中断使用。
func NoAvailableError(modelName string) *types.NewAPIError {
	return types.NewError(
		errors.New(NoAvailableMessage(modelName)),
		ErrorCodeNoAvailableChannel,
		types.ErrOptionWithStatusCode(http.StatusServiceUnavailable),
		types.ErrOptionWithSkipRetry(),
	)
}

// NoAvailableMessage 是下游 fallback 分类依赖的固定文案（routing-spec §4.2）。
func NoAvailableMessage(modelName string) string {
	return "No available channel for model " + modelName
}

// WriteNoAvailableChannel 按固定 body 形态写出 503 并中止 gin 处理链。
//
// 必须 Abort：本函数可能在中间件阶段调用，若只写响应不中止，后续 relay handler
// 会再写一次响应体（双写）。在 Relay 的 defer 中调用时 Abort 是幂等的无操作。
func WriteNoAvailableChannel(c *gin.Context, modelName string) {
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error": gin.H{"message": NoAvailableMessage(modelName)},
	})
}

// IsNoAvailable 判定错误是否为"无可用成员"。
func IsNoAvailable(err *types.NewAPIError) bool {
	return err != nil && err.GetErrorCode() == ErrorCodeNoAvailableChannel
}
