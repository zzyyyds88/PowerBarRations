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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/events"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
)

var (
	upstreamBurstMu             sync.Mutex
	upstreamBurstDetector       = events.NewUpstreamBurstDetector(events.UpstreamBurstConfig{})
	upstreamBurstConfigProvider func() events.UpstreamBurstConfig
)

// SetUpstreamBurstDetector replaces the process-local 5xx detector. Tests and
// configuration reloads use this to provide an isolated detector state.
func SetUpstreamBurstDetector(detector *events.UpstreamBurstDetector) {
	upstreamBurstMu.Lock()
	defer upstreamBurstMu.Unlock()
	if detector == nil {
		detector = events.NewUpstreamBurstDetector(events.UpstreamBurstConfig{})
	}
	upstreamBurstDetector = detector
}

// SetUpstreamBurstConfigProvider supplies the current system/options values.
func SetUpstreamBurstConfigProvider(provider func() events.UpstreamBurstConfig) {
	upstreamBurstMu.Lock()
	upstreamBurstConfigProvider = provider
	upstreamBurstMu.Unlock()
}

func observeUpstreamBurst(resolved *model.ResolvedRoute, member *model.RouteMember, statusCode int) {
	if resolved == nil || member == nil || statusCode < http.StatusInternalServerError || statusCode > 599 {
		return
	}
	upstreamBurstMu.Lock()
	if upstreamBurstConfigProvider != nil {
		configured := upstreamBurstConfigProvider().NormalizedConfig()
		if configured != upstreamBurstDetector.Config() {
			upstreamBurstDetector = events.NewUpstreamBurstDetector(configured)
		}
	}
	result := upstreamBurstDetector.Observe(strconv.Itoa(member.ChannelId), statusCode)
	upstreamBurstMu.Unlock()
	if !result.Triggered {
		return
	}
	events.PublishAsync(events.Event{
		Ts:     time.Now().UnixMilli(),
		Type:   events.EventUpstreamBurst,
		Lane:   laneKeyOf(resolved),
		Member: memberLabel(member),
		Detail: fmt.Sprintf("channel_id=%d count=%d threshold=%d window_seconds=%d status_code=%d", member.ChannelId, result.Count, result.Threshold, int(result.Window/time.Second), statusCode),
	})
}

// contextKey 请求内路由态在 gin.Context 中的键。
const contextKey = "pbr_request_route"

// ErrorCodeNoAvailableChannel 模型面无可用成员。body 形态由 WriteNoAvailableChannel 固定，
// 不走 NewAPIError 的通用序列化（见 routing-spec §4.2：下游 fallback 分类依赖该 body）。
const ErrorCodeNoAvailableChannel types.ErrorCode = "no_available_member"

// EventPanicPick 恐慌模式启用：整条车道只剩「冷却中」的成员时，压着冷却强行上一个，
// 而不是对外返回 503（参 Envoy panic threshold）。事件体只用于排障留痕。
const EventPanicPick = "panic_pick"

// ErrorKind 错误分类（routing-spec §4.1）。
type ErrorKind string

const (
	KindSoftRateLimit ErrorKind = "soft_rate_limit"
	KindSoftTransient ErrorKind = "soft_transient"
	// KindTimeout 我方成员级 deadline 到期（routing-spec §4.1，2026-11-21 拆出自
	// soft_transient）：流式首帧/非流式响应/响应头等待超时——请求已送达上游但
	// 窗口内未产出首个有效事件。已送达但无响应的上游同成员重试大概率重复失败
	// （magpie replayable 实践），故不再原地重试、立即换成员；冷却/熔断与
	// soft_transient 完全同档（§4.5 分级递减、allowed_fails 默认 4、权重同软档）。
	KindTimeout   ErrorKind = "timeout"
	KindHardAuth  ErrorKind = "hard_auth"
	KindHardQuota ErrorKind = "hard_quota"
	// KindUpstreamReject 上游对"该成员"的拒绝（routing-spec §4.1/§4.4）：400/422，
	// 或提交前的流内 error 帧。确定性错误（同一份请求体再打仍是拒绝），
	// 但换一条腿可能就过——所以换成员、同一成员不重试；成员本身无过错——
	// 不写冷却、不计熔断分。全部成员都拒绝时把最后一次的上游错误原样返回客户端（§4.2）。
	KindUpstreamReject ErrorKind = "upstream_reject"
	KindClientError    ErrorKind = "client_error"
	KindBadResponse    ErrorKind = "bad_response"
	KindCanceled       ErrorKind = "canceled"
	// KindLocalThrottle 本地出站准入排队超界/队列满（routing-spec §3.2）。
	// **只用于请求日志**：不是成员失败分类，不写冷却、不计熔断，成员
	// last_error_kind 不出现该值。
	KindLocalThrottle ErrorKind = "local_throttle"
)

// quotaKeywordMatcher 由外部注入的"欠费/额度"关键词判定（deployment 私有条目）。
// 关键词优先于状态码：欠费类上游常以 400 到达，状态码路径捕不到（design-v1 §7.6）。
var quotaKeywordMatcher func(message string) bool

// SetQuotaKeywordMatcher 注入关键词判定实现（由 middleware 在启动时接到基座的关键词表）。
func SetQuotaKeywordMatcher(matcher func(string) bool) {
	quotaKeywordMatcher = matcher
}

// Classify 按 routing-spec §4.1 把一次尝试的错误归类。

// builtInQuotaPatterns 是"上游免费档/配额耗尽"的内置判定词（小写匹配）。
// 部署侧关键词（quotaKeywordMatcher）之外的通用兜底，规范见 routing-spec §4.1。
var builtInQuotaPatterns = []string{
	"free usage limit",
	"free-limit",
	"usage limit",
	"quota exceeded",
	"insufficient quota",
	"quota exceeded for",
}

func isBuiltInQuotaMessage(message string) bool {
	if message == "" {
		return false
	}
	lower := strings.ToLower(message)
	for _, pattern := range builtInQuotaPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func Classify(err *types.NewAPIError) ErrorKind {
	if err == nil {
		return ""
	}
	if types.IsLocalAttemptTimeout(err) {
		return KindTimeout
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// 流式空闲/首事件超时由 helper 显式使用该错误码；其它裸
		// DeadlineExceeded 来自客户端 context，按取消处理，不能冷却健康成员。
		if err.GetErrorCode() == types.ErrorCodeChannelResponseTimeExceeded {
			return KindTimeout
		}
		return KindCanceled
	}
	if errors.Is(err, context.Canceled) {
		return KindCanceled
	}
	// 关键词优先于状态码（欠费类以 400 到达）。
	if quotaKeywordMatcher != nil && quotaKeywordMatcher(err.Error()) {
		return KindHardQuota
	}
	// 内置配额判定（routing-spec §4.1，2026-11-21 立）：上游免费档/配额耗尽是
	// 持续性故障（秒级不会自愈），必须归 hard_quota 拿长冷却，不能每次都花
	// 几十秒撞同一堵墙。放在 upstream_reject 之前：流内 error 帧（提交前）的
	// 报文里带这类字样时同样进长冷却。
	if isBuiltInQuotaMessage(err.Error()) {
		return KindHardQuota
	}
	if err.GetErrorCode() == ErrorCodeNoAvailableChannel {
		// 成员耗尽：语义上是"服务不可用"而非"请求不合法"，日志里不能记成 client_error。
		return KindSoftTransient
	}
	// 注意：基座的 types.NewError 默认把 StatusCode 设成 500，我方内部错误（请求转换失败等）
	// 都是"500 + skipRetry"。因此 skipRetry 必须优先于状态码判定，否则我方校验类错误
	// 会被当成上游软故障，触发换人与冷却——正是 routing-spec §4.1 禁止的"坏请求打冷健康成员"。
	// skipRetry 必须早于错误码与状态码分支：我方构造的"请求本身不合法"错误
	// （请求体解析失败、我方可判定的校验失败）即使带 BadResponse/DoRequestFailed
	// 错误码，也不得重试或冷却健康成员（routing-spec §4.1.2）。
	// NoAvailableChannel 例外已在上面处理（它是"成员耗尽"而非坏请求）。
	if types.IsSkipRetryError(err) {
		return KindClientError
	}
	switch err.GetErrorCode() {
	case types.ErrorCodeUpstreamReject:
		// 流内 error 帧（提交前）由转发管道识别并以此码上报（routing-spec §4.4）。
		return KindUpstreamReject
	case types.ErrorCodeBadResponse, types.ErrorCodeBadResponseBody, types.ErrorCodeEmptyResponse,
		types.ErrorCodeReadResponseBodyFailed:
		// 200 但响应体不合法/无法解析（routing-spec §4.1 的 bad_response）。
		return KindBadResponse
	case types.ErrorCodeDoRequestFailed:
		// 传输层失败：连接失败、EOF 归软故障。我方成员超时已由
		// LocalAttemptTimeout marker 在函数开头归类；裸 deadline 按客户端取消。
		return KindSoftTransient
	case types.ErrorCodeChannelResponseTimeExceeded:
		// 渠道响应超时同样是我方 deadline 判定，归 timeout（§4.1）。
		return KindTimeout
	case types.ErrorCodeChannelNoAvailableKey, types.ErrorCodeChannelInvalidKey:
		return KindHardAuth
	}
	// 200 + 响应体内带 error 对象：上游用 HTTP 200 承载了自己的拒绝（routing-spec §4.4 的
	// 非流式同形）。任一入站格式的错误对象都按成员级拒绝处理（换成员、不冷却）。
	if err.StatusCode == http.StatusOK && isUpstreamErrorObject(err.RelayError) {
		return KindUpstreamReject
	}
	switch code := err.StatusCode; {
	case code == http.StatusTooManyRequests:
		return KindSoftRateLimit
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return KindHardAuth
	case code == http.StatusBadRequest || code == http.StatusUnprocessableEntity,
		code == http.StatusFailedDependency,
		code == http.StatusUnavailableForLegalReasons:
		// 400/422：上游对该成员的拒绝（内容审查、参数不支持等），换成员可能就过。
		// 424：上游明确拒绝重放（如 Automatic replay is not allowed），换成员；
		// 451：因法律原因不可用，属确定性拒绝，不是瞬时故障。
		return KindUpstreamReject
	case code == http.StatusPaymentRequired:
		// 402：欠费（未命中关键词时的兜底），按额度问题处理，不反复撞墙。
		return KindHardQuota
	case code == http.StatusRequestEntityTooLarge:
		// 413：请求过大，同一份请求体重打必然失败；是请求本身的问题。
		return KindClientError
	case code == 499:
		// 非标准码：客户端关闭请求（nginx 约定），按取消处理。
		return KindCanceled
	case code == http.StatusRequestTimeout, code == http.StatusServiceUnavailable, code >= 500:
		return KindSoftTransient
	default:
		// 其余 4xx（404 成员无此模型、405/409 等）与状态码 0/异常：一律按可逃逸的
		// 瞬时失败处理（routing-spec §4.1 只把 400/422/451 定义为成员级拒绝）。
		// 若把 404 这类"上游确实没有该模型"的失败归成不可逃逸形态，请求会不换人、
		// 不冷却，直接撞死在第一个成员上。
		return KindSoftTransient
	}
}

// requestScopedRejectMarkers 是上游明确针对"这一份请求"的拒绝标记
// （内容审查/参数不支持/上下文超限/重放拒绝）。它们仍按 upstream_reject
// 换成员，但不累计 reject_streak（routing-spec §4.1.3）——那不是配置过错。
var requestScopedRejectMarkers = []string{
	"data_inspection_failed",
	"content_filter",
	"content policy",
	"invalid_parameter",
	"invalid_request_error",
	"context_length_exceeded",
	"unsupported_parameter",
	"unsupported_value",
	"automatic replay is not allowed",
}

// IsRequestScopedUpstreamReject 判断一次 upstream_reject 是否只针对本次请求。
// 无法识别的报文按配置可疑处理（保守），仍累计 reject_streak。
func IsRequestScopedUpstreamReject(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	// 424 = 上游明确拒绝重放，属于本次请求/本次重放条件，不是配置过错。
	if err.StatusCode == http.StatusFailedDependency {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range requestScopedRejectMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// isUpstreamErrorObject 判断 RelayError 是否是上游错误对象（值或指针形式）。
// 200 状态码下携带上游 error 表示"上游用 200 承载了拒绝"，与上游构造无关的
// 我方错误不会走到这里。
func isUpstreamErrorObject(relayError any) bool {
	switch relayError.(type) {
	case types.OpenAIError, *types.OpenAIError, types.ClaudeError, *types.ClaudeError:
		return true
	default:
		return false
	}
}

// ShouldSwitchMember 该错误是否应换下一个成员（routing-spec §4.1 最后一列）。
func ShouldSwitchMember(kind ErrorKind) bool {
	switch kind {
	case KindSoftRateLimit, KindSoftTransient, KindTimeout, KindHardAuth, KindHardQuota, KindBadResponse, KindUpstreamReject:
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
	// WaitMs 出站准入实际排队时长（routing-spec §3.2；0 = 未排队）。
	WaitMs int64 `json:"wait_ms,omitempty"`
	// memberKey 仅内部使用：同一车道可有两个标签相同的重复成员（组内成员 +
	// 散成员），清理/回填 throttled 记录时必须按运行态身份而不是展示标签匹配。
	memberKey MemberKey `json:"-"`
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
	// currentWaitMs 本轮尝试在出站准入队列里的实际等待毫秒数
	// （routing-spec §3.2）。成功/失败 attempts 都带它；排队失败的 throttled
	// 记录由 ExhaustThrottled 回填，避免"排队了多久"只存在于部分路径。
	currentWaitMs int64
	// probe 本次请求真正持有的探测租约（nil = 未占）。
	//
	// 探测槽是**车道级单槽**，但只有占槽者可以归还它：一个从未探测的并发请求
	// 若也去清 ProbeMember，就会把别人的槽释放掉，"单探测"在并发下失效
	// （routing-spec §2.2/§6）。因此归属必须记在请求态上。
	probe             *ProbeLease
	runtimeGeneration uint64
	cleanups          []func()
	closed            bool
	// skipRecorded 已记过"被跳过"的成员键，避免同一请求多轮重试时
	// 把同一个冷却中的成员重复写进尝试链。
	skipRecorded map[MemberKey]bool
	// memberCfg 每个成员生效的六键（车道默认叠加成员级覆盖）。
	memberCfg []model.LaneRelayConfig
	// admissionReady 出站准成就绪回调（nil = 不做准入）；由 middleware 注入
	// （routing-spec §3.2）。回调必须无阻塞、不查库。
	admissionReady func(*model.RouteMember) bool
	// throttledIdx 本轮因准入未就绪被跳过的首成员（用于排队/本地 429）。
	throttledIdx int
	// abortErr 本请求需要原样透传的上游错误（当前用于 429 Retry-After 超上限）。
	// 与 throttle 不同：它不是本地准入拒绝，也不合成 503。
	abortErr *types.NewAPIError
	// admissionDeadline 请求级绝对准入预算，只由 PBRServe 入口计算一次；
	// 所有成员与重试共享，不能每次 PBRNextChannel 重新计时。
	admissionDeadline    time.Time
	admissionDeadlineSet bool
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
//
// resolved 是 PBRServe 在请求入口生成的不可变快照；State 生命周期内不再
// 重读车道、渠道或成员配置（ADR 0014）。
func NewState(resolved *model.ResolvedRoute) *State {
	routeKey := laneKeyOf(resolved)
	var runtime *Runtime
	if resolved.Source == model.RouteSourceExplicit && len(resolved.Members) > 0 {
		runtime = Default.For(routeKey)
	} else {
		runtime = newRuntime()
		runtime.laneName = routeKey
	}
	runtime.mu.Lock()
	generation := runtime.generation
	runtime.mu.Unlock()
	s := &State{
		Runtime:           runtime,
		runtimeGeneration: generation,
		current:           -1,
		attempts:          map[int]int{},
	}
	s.applyRouteLocked(resolved)
	return s
}

// applyRouteLocked 固化请求级快照并重建请求内的派生态（成员顺序、六键、预算
// 计数）。只在 NewState 入口调用一次；重试不得替换 Route、Runtime 或 generation。
func (s *State) applyRouteLocked(resolved *model.ResolvedRoute) {
	currentKey := MemberKey{}
	oldAttempts := make(map[MemberKey]int, len(s.attempts))
	if s.Route != nil {
		if s.current >= 0 && s.current < len(s.Route.Members) {
			currentKey = memberKeyOf(&s.Route.Members[s.current])
		}
		for idx, n := range s.attempts {
			if idx >= 0 && idx < len(s.Route.Members) {
				oldAttempts[memberKeyOf(&s.Route.Members[idx])] = n
			}
		}
	}

	cfg := resolved.Config.Normalize()
	signature := routeSnapshotSignature(resolved)
	if resolved.Source == model.RouteSourceExplicit && len(resolved.Members) > 0 {
		s.Runtime = Default.For(laneKeyOf(resolved))
	}
	s.Runtime.withLock(func() {
		if s.Runtime.generation != s.runtimeGeneration {
			return
		}
		if s.Runtime.routeSignature != "" && s.Runtime.routeSignature != signature {
			// A lane edit is an explicit operator decision. Do not let an old
			// affinity window hide a newly promoted member.
			s.Runtime.AffinityUntil = 0
			s.Runtime.AffinityArmed = false
		}
		s.Runtime.routeSignature = signature
	})
	s.Route = resolved
	s.cooldownSeconds = cfg.MemberCooldownSeconds

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
		cfg, _ := resolved.EffectiveConfigE(&resolved.Members[i])
		s.memberCfg[i] = cfg
	}

	s.attempts = make(map[int]int, len(resolved.Members))
	for i := range resolved.Members {
		s.attempts[i] = oldAttempts[memberKeyOf(&resolved.Members[i])]
	}
	s.current = -1
	s.throttledIdx = -1
	if currentKey != (MemberKey{}) {
		s.current = s.indexOfKey(currentKey)
	}

	// 路由解析是运行态维护的安全点：配置写入口负责及时收敛，请求解析再兜底一次。
	// Reset 后旧快照不得用旧成员集合裁剪新一代运行态。
	if s.runtimeGenerationCurrent() {
		valid := make(map[MemberKey]bool, len(resolved.Members))
		for i := range resolved.Members {
			valid[memberKeyOf(&resolved.Members[i])] = true
		}
		s.Runtime.PruneStaleState(valid, resolved.LaneVersion)
	}
}

// routeSnapshotSignature 解析快照的内容签名：成员链（含改名/别名/优先级/成员级
// 覆盖）、模式、active_member、点名与六键。只含影响选路的字段；
// Channel/ChannelEnabled 等展示字段刻意排除——渠道启停由尝试循环每轮直接查库
// 判定（middleware.PBRNextChannel），不需要经快照重建。
func routeSnapshotSignature(resolved *model.ResolvedRoute) string {
	var b strings.Builder
	cfg := resolved.Config.Normalize()
	b.WriteString(resolved.Source)
	b.WriteByte('|')
	b.WriteString(resolved.Mode)
	b.WriteByte('|')
	b.WriteString(resolved.RouteKey)
	b.WriteByte('|')
	b.WriteString(resolved.ActiveMember)
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(resolved.PinnedMemberId))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(resolved.LaneVersion, 10))
	b.WriteByte('|')
	if resolved.ConfigError != nil {
		fmt.Fprintf(&b, "lane_err:%s:%s:%s|", resolved.ConfigError.Code, resolved.ConfigError.Message, resolved.ConfigError.Path)
	}
	for _, v := range [6]int{
		cfg.MemberMaxAttempts, cfg.MemberRetryIntervalSeconds,
		cfg.MemberNonStreamResponseTimeoutSeconds, cfg.MemberStreamFirstEventTimeoutSeconds,
		cfg.MemberCooldownSeconds, cfg.MemberAffinitySeconds,
	} {
		b.WriteString(strconv.FormatInt(int64(v), 10))
		b.WriteByte(',')
	}
	for i := range resolved.Members {
		m := &resolved.Members[i]
		// Disabled 必须在签名里：它是配置态、直接决定该成员是否参与选路。
		// **UpstreamModel（派生真名）也必须在**：渠道 model_mapping 改动不经过成员重插，
		// 只能靠签名变化触发快照重建，否则改了映射的车道会继续把请求打到旧上游名
		// （ADR 0008；routing-spec §1.3）。
		// **Group（成员组来源）也必须在**（ADR 0009）：组的成员/开关/顺序改动不经过
		// 车道保存，车道 UpdatedAt 不变；组展开成员的 Model/Priority/Disabled 变化虽
		// 已被本签名捕获，Group 显式入签名使"换引用的组"也必然触发重建。
		// 不要指望 LaneVersion（秒级）或 MemberId（整体重插必然变）的副作用来驱动重建
		// ——同秒内的两次保存即失效，且渠道级/组级改动根本不经过成员重插。
		fmt.Fprintf(&b, "|%s:%d:%s:%s:%s:%d:%d:%t:%s:%t:%d",
			m.Group, m.ChannelId, m.Model, m.UpstreamModel, m.PublicAlias,
			m.Priority, m.MemberId, m.Disabled, strings.TrimSpace(m.Overrides),
			m.ChannelEnabled, m.ChannelVersion)
		if m.RateLimit != nil {
			fmt.Fprintf(&b, ":rl:%s:%d:%d:%d:%d", m.RateLimit.Model,
				m.RateLimit.MaxConcurrency, m.RateLimit.MaxRPM,
				m.RateLimit.QueueLimit, m.RateLimit.QueueWaitSeconds)
		}
		if m.ConfigError != nil {
			fmt.Fprintf(&b, ":ce:%s:%s:%s", m.ConfigError.Code, m.ConfigError.Message, m.ConfigError.Path)
		}
		if m.OverrideError != nil {
			fmt.Fprintf(&b, ":oe:%s:%s:%s", m.OverrideError.Code, m.OverrideError.Message, m.OverrideError.Path)
		}
	}
	return b.String()
}

// AbortError 返回本请求需要原样透传的上游错误（如 429 Retry-After 超上限）。
// 调用方在 Next 返回 ok=false 后读取；没有中止时返回 nil。
func (s *State) AbortError() *types.NewAPIError {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.abortErr
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
// 返回 ok=false 表示"没有可用成员/不该继续尝试"，调用方先读 AbortError()：
// 非 nil 表示需要原样透传的上游错误（如 Retry-After 超上限的 429）；
// 否则按 §4.2 快抛 503（客户端的错则原样返回，见 §4.1 的 client_error / canceled 两行）。
//
// 请求级快照不可变：上一轮错误按实际尝试的同一份快照归属，随后只在该
// 快照内推进成员/重试预算，不重读车道、渠道或成员配置（ADR 0014）。
func (s *State) Next(lastErr *types.NewAPIError) (*model.RouteMember, time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current >= 0 && lastErr != nil {
		if !types.IsSkipRetryError(lastErr) {
			observeUpstreamBurst(s.Route, s.currentMemberLocked(), lastErr.StatusCode)
		}
		kind := Classify(lastErr)
		if !ShouldSwitchMember(kind) {
			// client_error / canceled：不换人、不冷却，直接把错误交回客户端。
			// 这仍是一次真实尝试，照记耗时（否则最后一次尝试的 duration_ms 缺失）。
			attemptedKey := memberKeyOf(s.currentMemberLocked())
			s.history = append(s.history,
				failedAttemptWithDuration(len(s.history)+1, memberLabel(s.currentMemberLocked()), kind, lastErr, s.elapsedMs(), s.currentWaitMs))
			s.markAttemptedLocked(attemptedKey)
			s.Runtime.withLock(func() { s.releaseOwnedProbeLocked(attemptedKey) })
			return nil, 0, false
		}
		member := s.currentMemberLocked()
		key := memberKeyOf(member)
		budgetLeft := s.attempts[s.current] < s.budgetFor(s.current)
		s.history = append(s.history,
			failedAttemptWithDuration(len(s.history)+1, memberLabel(member), kind, lastErr, s.elapsedMs(), s.currentWaitMs))
		s.markAttemptedLocked(key)

		memberCfg := s.configFor(s.current)
		if kind == KindSoftRateLimit {
			// 429 先落减速（即使接下来还要原地重试）：这是上游的实时背压信号，
			// 运行态必须立刻反映，不能等预算耗尽才记（routing-spec §4.1.1）。
			// recordFailure 对 429 只写 PacedUntil，重复调用只是把截止时间往后推。
			s.Runtime.withLock(func() {
				if s.Runtime.generation != s.runtimeGeneration {
					return
				}
				s.Runtime.recordFailure(key, kind, memberCfg.MemberCooldownSeconds, lastErr.RetryAfterSeconds, false, CurrentCircuitSettings())
				if !budgetLeft || !ShouldRetrySameMember(kind) {
					s.releaseOwnedProbeLocked(key)
				}
			})
			// routing-spec §4.1.1：Retry-After 超过本地上限时不做无谓挂起，
			// 原样把该 429 交回 controller（写 Retry-After 头，不换成员、不合成 503）。
			if lastErr.RetryAfterSeconds > rateLimitMaxPaceSeconds {
				s.abortErr = lastErr
				return nil, 0, false
			}
			if budgetLeft && ShouldRetrySameMember(kind) {
				// 原地重试的等待取 max(retry_interval, 剩余减速)：上游说了
				// Retry-After 5s、本地间隔 3s，就必须等满 5s 再打（§3.1）。
				s.attempts[s.current]++
				s.attemptStartedAt = time.Now()
				s.currentWaitMs = 0
				wait := time.Duration(memberCfg.MemberRetryIntervalSeconds) * time.Second
				if remain := s.Runtime.paceRemaining(key); remain > wait {
					wait = remain
				}
				return member, wait, true
			}
		} else if kind == KindUpstreamReject {
			// 成员级拒绝（routing-spec §4.1/§4.4）：同一份请求体再打同一成员仍是拒绝，
			// 所以不原地重试；一次性耗尽该成员在本请求内的剩余预算以推进成员链，
			// 且**不写冷却、不计熔断分**（成员本身无过错，下一份请求它可能完全正常）。
			if budgetLeft {
				s.attempts[s.current] = s.budgetFor(s.current)
			}
			s.Runtime.withLock(func() {
				if s.Runtime.generation != s.runtimeGeneration {
					return
				}
				s.Runtime.recordReject(key, kind, s.Route.Mode != model.LaneModeManual, IsRequestScopedUpstreamReject(lastErr), CurrentCircuitSettings())
				s.releaseOwnedProbeLocked(key)
			})
		} else if budgetLeft && ShouldRetrySameMember(kind) {
			// 同成员原地重试：按 retry_interval 间隔后再打一次（§3.1）。
			// 这仍是一轮新尝试：计数后下一轮 Next 前重读配置。
			s.attempts[s.current]++
			s.attemptStartedAt = time.Now()
			s.currentWaitMs = 0
			return member, time.Duration(memberCfg.MemberRetryIntervalSeconds) * time.Second, true
		} else if kind != KindSoftRateLimit {
			// 尝试预算耗尽或硬故障：写入冷却与熔断计分后换人。
			// §2.1：manual 车道不参与冷却——人工指定的成员就是唯一选择，冷却在此
			// 没有可换的替代者，只会把请求变成 503；熔断计分照常。
			// 429 已在上面写过减速，落到这里只继续换人，不能重复判罚。
			if kind == KindTimeout {
				// timeout 已送达但无响应：同一份请求/同一成员再打大概率重复失败
				// （routing-spec §4.1）。一次性耗尽本请求预算，防止短冷却到期后
				// 在同一请求内再次选中它（30s×2 的 200s 串行链）。
				s.attempts[s.current] = s.budgetFor(s.current)
			}
			manual := s.Route.Mode == model.LaneModeManual
			s.Runtime.withLock(func() {
				if s.Runtime.generation != s.runtimeGeneration {
					return
				}
				s.Runtime.recordFailure(key, kind, memberCfg.MemberCooldownSeconds, lastErr.RetryAfterSeconds, !manual, CurrentCircuitSettings())
				s.releaseOwnedProbeLocked(key)
			})
		}
	}

	// 请求级快照不可变：新一轮仍消费 PBRServe 入口生成的同一份 Route。
	member, ok := s.pickLocked()
	if !ok {
		return nil, 0, false
	}
	s.attemptStartedAt = time.Now()
	s.currentWaitMs = 0
	return member, 0, true
}

func (s *State) runtimeGenerationCurrent() bool {
	s.Runtime.mu.Lock()
	defer s.Runtime.mu.Unlock()
	return s.Runtime.generation == s.runtimeGeneration
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
func (s *State) markAttemptedLocked(key MemberKey) {
	if s.skipRecorded == nil {
		s.skipRecorded = map[MemberKey]bool{}
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
func (s *State) recordSkipLocked(key MemberKey, member string) {
	status := "skipped"
	if s.Runtime != nil {
		status = s.Runtime.SkipReason(key)
	}
	s.recordSkipWithStatusLocked(key, member, status)
}

// recordSkipWithStatusLocked 以调用方给定的原因记一条"被跳过"的尝试。
//
// 为什么需要一个显式传原因的入口：`disabled`（成员被人工停用）是**配置态**结论，
// 而 Runtime.SkipReason 只能看见熔断表与冷却表两张运行态表，拿不到车道配置
// （routing-spec §5.3/§9）。把开关下沉进运行态推断会让 Runtime 反向依赖车道配置，
// 还会把"运维自己关的"误报成一种故障。所以谁判定出停用，谁在这里交出原因。
func (s *State) recordSkipWithStatusLocked(key MemberKey, member, status string) {
	if s.skipRecorded == nil {
		s.skipRecorded = map[MemberKey]bool{}
	}
	if s.skipRecorded[key] {
		return
	}
	s.skipRecorded[key] = true
	s.history = append(s.history, Attempt{
		AttemptNum: len(s.history) + 1,
		Member:     member,
		Status:     status,
		memberKey:  key,
	})
}

// removeThrottledSkipLocked 删除某成员"准入未就绪"的跳过记录：它一旦真的
// 被选中执行，throttled 的语义（未被使用）就不再成立。按 MemberKey 精确匹配，
// 避免同一车道两个同名成员的记录串写。
func (s *State) removeThrottledSkipLocked(idx int) {
	if idx < 0 || idx >= len(s.Route.Members) {
		return
	}
	key := memberKeyOf(&s.Route.Members[idx])
	for i := len(s.history) - 1; i >= 0; i-- {
		if s.history[i].Status != "throttled" || s.history[i].memberKey != key {
			continue
		}
		s.history = append(s.history[:i], s.history[i+1:]...)
		for j := range s.history {
			s.history[j].AttemptNum = j + 1
		}
		delete(s.skipRecorded, key)
		return
	}
}

func failedAttemptWithDuration(num int, member string, kind ErrorKind, err *types.NewAPIError, durationMs, waitMs int64) Attempt {
	return Attempt{
		AttemptNum: num,
		Member:     member,
		Status:     "failed",
		DurationMs: durationMs,
		ErrorKind:  kind,
		Msg:        truncate(err, 512),
		WaitMs:     waitMs,
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
	if !s.runtimeGenerationCurrent() {
		return -1
	}
	chosen := -1
	pacedIdx := -1
	var pacedUntil int64
	s.throttledIdx = -1
	s.Runtime.withLock(func() {
		// 亲和期内沿用当前成员（§6：不提前切回高优先级成员）。
		if s.Runtime.HasCurrent && s.Runtime.AffinityUntil > nowMs() {
			if idx := s.indexOfKey(s.Runtime.CurrentMember); idx >= 0 {
				key := memberKeyOf(&s.Route.Members[idx])
				availability := s.Runtime.availabilityOf(key, CurrentCircuitSettings())
				if !s.Route.Members[idx].Disabled &&
					s.Route.Members[idx].ConfigError == nil &&
					s.Route.Members[idx].OverrideError == nil &&
					s.attempts[idx] < s.budgetFor(idx) &&
					s.admissionAllowsLocked(&s.Route.Members[idx]) &&
					availability == availOK &&
					s.Runtime.paceUntil(key) == 0 {
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
			// 人工停用排在冷却/熔断判定**之前**，且必须在 takeProbe 之前：探测槽只服务
			// "运行态暂时不可选、可能自己恢复"的成员，一个关着的成员若占了它，
			// 会把整条链每轮唯一的恢复探测机会吃掉（routing-spec §2.2）。
			if s.Route.Members[idx].ConfigError != nil {
				s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "channel_config_error")
				continue
			}
			if s.Route.Members[idx].OverrideError != nil {
				s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "member_config_error")
				continue
			}
			if s.Route.Members[idx].Disabled {
				s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "disabled")
				continue
			}
			// 先过滤配置态与运行态不可用（冷却/熔断），再判准入：已经确定
			// 本轮不可发的成员不应进入 FIFO，排队结束后才被拒绝。
			availability := s.Runtime.availabilityOf(key, CurrentCircuitSettings())
			if availability == availSkip {
				s.recordSkipLocked(key, memberLabel(&s.Route.Members[idx]))
				continue
			}
			// 出站准入：未就绪的成员本轮让位给低优先级但就绪的成员
			// （routing-spec §3.2 "a full one gives way first"）。健康但仍未就绪的
			// 成员记下来，供 middleware 排队或本地 429。
			if !s.admissionAllowsLocked(&s.Route.Members[idx]) {
				s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "throttled")
				if s.throttledIdx < 0 {
					s.throttledIdx = idx
				}
				continue
			}
			// 限流减速不改变“可选”，但同优先级内先让非减速成员上；全部减速时
			// 选最早到期者（routing-spec §4.1.1/§5.3）。探测槽仍优先用于恢复。
			if availability == availOK {
				if until := s.Runtime.paceUntil(key); until > 0 {
					if pacedIdx < 0 || until < pacedUntil {
						pacedIdx, pacedUntil = idx, until
					}
					continue
				}
			}
			switch availability {
			case availProbeReady:
				lease, acquired := s.Runtime.acquireProbeLocked(key, DefaultProbeLeaseTTL)
				if !acquired {
					s.recordSkipLocked(key, memberLabel(&s.Route.Members[idx]))
					continue
				}
				s.probe = lease
				s.Runtime.recordProbeStart(key, s.Runtime.circuitOpen(key))
				chosen = idx
				return
			default:
				chosen = idx
				return
			}
		}

		// 全成员都在减速：选最早到期者，不把车道判成不可用（§5.3）。
		if chosen < 0 && pacedIdx >= 0 {
			chosen = pacedIdx
			return
		}

		// ── 恐慌模式（panic threshold，与 Envoy 同名机制同义）──────────────────
		// 常规选路把「冷却中」一律跳过。若**冷却**是唯一阻止选路的原因（成员没被人工停用、
		// 也没熔断打开——它们只是刚打了个喷嚏在休息），此时直接返回「本车道无可选成员」，
		// 就会把一条本可以服务的车道降级成 503。2026-10-23 实案：两个成员先后抖动、各锁
		// 60s，车道整段不可用，上游 Hermes 据此判定模型降级。
		//
		// 正解：宁可付一次「可能再失败」的上游往返，也不要对外宣布「没人」。挑冷却最早
		// 到期的那个（最可能已经好了的那个）。
		// 人工停用与熔断打开仍然跳过：前者是人工决策，后者是连续失败到阈值后的指数退避，
		// 都属于「确定不可用」，压进去只会白付一次必然失败的往返。
		if chosen < 0 {
			best, bestUntil := -1, int64(0)
			for _, idx := range s.order {
				if s.attempts[idx] > 0 || // 本请求已试过：panic 只做一次新往返，不二次记账
					s.attempts[idx] >= s.budgetFor(idx) ||
					s.Route.Members[idx].Disabled ||
					s.Route.Members[idx].ConfigError != nil ||
					s.Route.Members[idx].OverrideError != nil {
					continue
				}
				key := memberKeyOf(&s.Route.Members[idx])
				if s.Runtime.circuitOpen(key) {
					continue
				}
				until := s.Runtime.cooldownUntil(key)
				if until == 0 || nowMs() >= until {
					continue
				}
				// 准入未就绪的成员不能借恐慌模式绕过闸门。
				if !s.admissionAllowsLocked(&s.Route.Members[idx]) {
					if s.throttledIdx < 0 {
						s.throttledIdx = idx
					}
					continue
				}
				if best < 0 || until < bestUntil {
					best, bestUntil = idx, until
				}
			}
			if best >= 0 {
				s.Runtime.appendEvent(EventPanicPick, memberKeyOf(&s.Route.Members[best]), "no_healthy_member")
				chosen = best
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
		key := memberKeyOf(&s.Route.Members[idx])
		// 人工停用是配置态结论，与运行态冷却/熔断无关；必须在 SkipReason 之前
		// 交出 `disabled`，否则一条被关掉的成员会被解释成"在冷却"
		// （routing-spec §5.3/§9）。
		if s.Route.Members[idx].ConfigError != nil {
			s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "channel_config_error")
			continue
		}
		if s.Route.Members[idx].OverrideError != nil {
			s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "member_config_error")
			continue
		}
		if s.Route.Members[idx].Disabled {
			s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "disabled")
			continue
		}
		s.recordSkipLocked(key, memberLabel(&s.Route.Members[idx]))
	}
}

// pickManualLocked 只用手工指定的成员；不可用即"无可用"，不静默换人（routing-spec §2.1）。
func (s *State) pickManualLocked() (*model.RouteMember, bool) {
	if !s.runtimeGenerationCurrent() {
		return nil, false
	}
	s.throttledIdx = -1
	idx := s.activeMemberIndex()
	if idx < 0 || s.attempts[idx] >= s.budgetFor(idx) {
		return nil, false
	}
	key := memberKeyOf(&s.Route.Members[idx])
	ok := false
	s.Runtime.withLock(func() {
		// 人工停用排在熔断判定之前，并留痕 `disabled`（routing-spec §2.1/§9）：
		// manual 没有替代者，指定的成员被关掉就是"无可用"，**绝不静默换人**，
		// 也不因为这次关闭而写冷却或改动熔断状态。
		if s.Route.Members[idx].Disabled {
			s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "disabled")
			return
		}
		if s.Route.Members[idx].OverrideError != nil {
			s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "member_config_error")
			return
		}
		if s.Route.Members[idx].ConfigError != nil {
			s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "channel_config_error")
			return
		}
		if !s.Route.Members[idx].ChannelEnabled {
			s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "channel_disabled")
			return
		}
		// manual 忽略冷却，但仍先判熔断；已经确定本轮不可发的成员不应进入
		// 准入队列。半开探测仍需独占槽位。
		circuitState := s.Runtime.circuitState(key)
		if circuitState == CircuitOpen && nowMs() < s.Runtime.circuitOpenUntil(key) {
			return
		}
		// 出站准入不改变 manual 的"只走点名成员"语义：未就绪时排队/本地 429，
		// 绝不换人（routing-spec §2.1/§3.2）。
		if !s.admissionAllowsLocked(&s.Route.Members[idx]) {
			s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "throttled")
			s.throttledIdx = idx
			return
		}
		// 退避期到或半开在途：只放行唯一探测者，槽被占则本轮无可用。
		if circuitState == CircuitOpen || circuitState == CircuitHalfOpen {
			lease, acquired := s.Runtime.acquireProbeLocked(key, DefaultProbeLeaseTTL)
			if !acquired {
				return
			}
			s.probe = lease
			s.Runtime.recordProbeStart(key, circuitState == CircuitOpen)
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
	// 真正选中即执行，之前"准入未就绪"的 throttled 记录不再成立；无论
	// 走普通选路还是 CommitThrottled，都由这一个入口保证不变量。
	s.removeThrottledSkipLocked(idx)
	key := memberKeyOf(&s.Route.Members[idx])
	s.Runtime.withLock(func() {
		if s.Runtime.generation != s.runtimeGeneration {
			return
		}
		if s.Runtime.HasCurrent && s.Runtime.CurrentMember != key {
			s.Runtime.AffinityArmed = true
		}
		s.Runtime.CurrentMember = key
		s.Runtime.HasCurrent = true
		s.current = idx
	})
}

func (s *State) indexOfKey(key MemberKey) int {
	for i := range s.Route.Members {
		if memberKeyOf(&s.Route.Members[i]) == key {
			return i
		}
	}
	return -1
}

// activeMemberIndex 解析 manual 模式的 active_member：先按成员别名，再按
// `channel/model` 标签。解析规则只保留在 model.ActiveMemberIndex 一处，
// 健康快照、管理面校验与运行期选路必须同源。
func (s *State) activeMemberIndex() int {
	return model.ActiveMemberIndex(s.Route.ActiveMember, s.Route.Members)
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
	fromProbe := s.probe != nil && s.probe.key == key
	s.Runtime.withLock(func() {
		if s.Runtime.generation != s.runtimeGeneration {
			return
		}
		s.Runtime.recordSuccess(key, affinitySeconds, CurrentCircuitSettings(), fromProbe)
		s.releaseOwnedProbeLocked(key)
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
		WaitMs:     s.currentWaitMs,
	}
}

// ReportMemberUnavailable 记一次"成员自身不可用"（渠道被禁用/删除、循环内
// 取不到 key 或注入上下文失败）。routing-spec §3 第 4.2 步要求这算该成员的一次
// 失败并计入冷却/熔断，否则被禁用渠道的车道会无限重选同一成员。
//
// 成员当前不在 s.current（还没被选中就发现不可用）时，也补一条 failed 尝试，
// 让 attempts 链能解释"为什么这个成员被跳过"。
// ReportMemberUnavailable 记录一次真实的"成员尝试失败"（上游请求已发生但失败），
// 按 soft_transient 写冷却/熔断。异步对象链路（relay/async）在创建失败时用它，
// 与主转发链路的 Next(lastErr) 同口径。
//
// 注意：**配置态不可用**（渠道被停用/缺失、base_url 或 key 缺失）不得走这里，
// 否则一次人工停用会把成员写成"故障"；那类跳过用 ReportMemberConfigSkip。
func (s *State) ReportMemberUnavailable(member *model.RouteMember, reason string) {
	if s == nil || member == nil || s.Runtime == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := memberKeyOf(member)
	// §2.1：manual 车道不参与冷却（与 Next 的失败归属路径一致），熔断计分照常。
	manual := s.Route.Mode == model.LaneModeManual
	// 请求尝试链是请求级日志，即使 Reset 推进代次也必须保留；运行态写入另行校验。
	s.history = append(s.history, Attempt{
		AttemptNum: len(s.history) + 1,
		Member:     memberLabel(member),
		Status:     "failed",
		ErrorKind:  KindSoftTransient,
		Msg:        "member unavailable: " + reason,
	})
	s.Runtime.withLock(func() {
		if s.Runtime.generation != s.runtimeGeneration {
			return
		}
		s.Runtime.recordFailure(key, KindSoftTransient, s.cooldownSeconds, 0, !manual, CurrentCircuitSettings())
		s.releaseOwnedProbeLocked(key)
	})
	s.markAttemptedLocked(key)
}

// ReportMemberConfigSkip 记录"成员因配置态不可用被跳过"（routing-spec §5.3）：
// 渠道停用/缺失、base_url 或 key 缺失属配置问题，**不写冷却、不计熔断分**。
// 只耗尽该成员在本请求内的尝试预算并留一条 `channel_disabled` 记录，
// 避免预算 >1 时反复选中同一个配置错误的成员。
func (s *State) ReportMemberConfigSkip(member *model.RouteMember, reason string) {
	if s == nil || member == nil || s.Runtime == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := memberKeyOf(member)
	for idx := range s.Route.Members {
		if memberKeyOf(&s.Route.Members[idx]) == key {
			s.attempts[idx] = s.budgetFor(idx)
			break
		}
	}
	s.recordSkipWithStatusLocked(key, memberLabel(member), "channel_disabled")
	// 配置态跳过也必须归还本请求可能持有的探测租约；reason 只影响事件详情。
	s.Runtime.withLock(func() {
		if s.Runtime.generation != s.runtimeGeneration {
			return
		}
		s.releaseOwnedProbeLocked(key)
		if reason != "" {
			s.Runtime.appendEvent(events.EventSkip, key, "channel_disabled reason="+reason)
		}
	})
}

// SetAdmissionDeadline 设置请求级绝对准入预算。重复设置忽略，确保重试不会
// 重新获得完整 queue_wait_seconds。
func (s *State) SetAdmissionDeadline(deadline time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.admissionDeadlineSet {
		s.admissionDeadline = deadline
		s.admissionDeadlineSet = true
	}
	s.mu.Unlock()
}

// AdmissionDeadline 返回请求级绝对准入预算；未设置时返回零时间。
func (s *State) AdmissionDeadline() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.admissionDeadline
}

// SetAdmissionReady 注入出站准成就绪回调（routing-spec §3.2）。
// 传 nil 关闭准入。回调在选路锁内执行，必须无阻塞、不查库。
func (s *State) SetAdmissionReady(fn func(*model.RouteMember) bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.admissionReady = fn
	s.mu.Unlock()
}

func (s *State) admissionAllowsLocked(member *model.RouteMember) bool {
	return s.admissionReady == nil || s.admissionReady(member)
}

// ThrottledMember 本轮因准入未就绪被跳过的首成员（无则 nil）。
func (s *State) ThrottledMember() *model.RouteMember {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.throttledIdx >= 0 && s.throttledIdx < len(s.Route.Members) {
		return &s.Route.Members[s.throttledIdx]
	}
	return nil
}

// CommitThrottled 在排队取得槽位后提交该成员为本次尝试：
// 计数一次真实尝试并设为当前成员。预算耗尽或成员在排队期间进入冷却/熔断则返回 false。
//
// 这里**不能**再调用 admissionReady：调用方已在 Wait 里拿到该成员的槽位，inflight
// 已计入本次请求；再按"是否就绪"复核会把自己刚占的槽判成已满，使排队成功的请求
// 必然回落为本地 429（2026-11-24 测试环境复现）。槽位归属由 AdmissionTicket 保证，
// 只需复核成员运行态仍可发（排队期间可能被同车道其它请求打进冷却/熔断）。
func (s *State) CommitThrottled(waitMs int64) (*model.RouteMember, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.throttledIdx
	if idx < 0 || idx >= len(s.Route.Members) {
		return nil, false
	}
	if s.attempts[idx] >= s.budgetFor(idx) {
		return nil, false
	}
	if s.Runtime != nil {
		key := memberKeyOf(&s.Route.Members[idx])
		unavailable := false
		s.Runtime.withLock(func() {
			if s.Route.Mode == model.LaneModeManual {
				state := s.Runtime.circuitState(key)
				if state == CircuitOpen && nowMs() < s.Runtime.circuitOpenUntil(key) {
					unavailable = true
					return
				}
				if state == CircuitOpen || state == CircuitHalfOpen {
					lease, acquired := s.Runtime.acquireProbeLocked(key, DefaultProbeLeaseTTL)
					if !acquired {
						unavailable = true
						return
					}
					s.probe = lease
					s.Runtime.recordProbeStart(key, state == CircuitOpen)
				}
				return
			}
			switch s.Runtime.availabilityOf(key, CurrentCircuitSettings()) {
			case availOK:
				return
			case availProbeReady:
				lease, acquired := s.Runtime.acquireProbeLocked(key, DefaultProbeLeaseTTL)
				if !acquired {
					unavailable = true
					return
				}
				s.probe = lease
				s.Runtime.recordProbeStart(key, s.Runtime.circuitOpen(key))
			default:
				unavailable = true
			}
		})
		if unavailable {
			return nil, false
		}
	}
	s.attempts[idx]++
	s.attemptStartedAt = time.Now()
	s.currentWaitMs = waitMs
	s.markCurrentLocked(idx)
	s.throttledIdx = -1
	return &s.Route.Members[idx], true
}

// ExhaustThrottled 记一次"准入失败"（队列满/排队超时）：attempt 状态 throttled，
// 耗尽该成员本请求内预算，供 middleware 换下一个候选或返回本地 429。
// **不写冷却、不计熔断**（排队不是成员失败，routing-spec §3.2）。
func (s *State) ExhaustThrottled(waitMs int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.throttledIdx
	if idx < 0 || idx >= len(s.Route.Members) {
		return
	}
	key := memberKeyOf(&s.Route.Members[idx])
	// 常规选路已为该成员记过一条 throttled 跳过记录；此处按 MemberKey 精确
	// 回填实际排队时长。只有 panic 分支会绕过 recordSkip 直接设置
	// throttledIdx，此时才需要补记录。
	found := false
	for i := len(s.history) - 1; i >= 0; i-- {
		if s.history[i].Status == "throttled" && s.history[i].memberKey == key {
			s.history[i].WaitMs = waitMs
			found = true
			break
		}
	}
	if !found {
		before := len(s.history)
		s.recordSkipWithStatusLocked(key, memberLabel(&s.Route.Members[idx]), "throttled")
		if len(s.history) > before {
			s.history[len(s.history)-1].WaitMs = waitMs
		}
	}
	s.attempts[idx] = s.budgetFor(idx)
	s.throttledIdx = -1
}

// releaseOwnedProbeLocked 只释放本 State 持有且 key 匹配的租约；调用方持 Runtime 锁。
func (s *State) releaseOwnedProbeLocked(key MemberKey) {
	if s.probe == nil || s.probe.key != key || s.Runtime.probe != s.probe || s.probe.generation != s.Runtime.generation {
		return
	}
	s.Runtime.probe = nil
	s.probe = nil
}

// ReleaseProbe 归还本请求持有的探测租约（幂等）。
func (s *State) ReleaseProbe() {
	if s == nil || s.Runtime == nil {
		return
	}
	s.mu.Lock()
	lease := s.probe
	s.probe = nil
	s.mu.Unlock()
	s.Runtime.ReleaseProbe(lease)
}

// AddCleanup 注册请求级资源清理；已 Close 的 State 立即执行清理。
func (s *State) AddCleanup(cleanup func()) {
	if s == nil || cleanup == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cleanup()
		return
	}
	s.cleanups = append(s.cleanups, cleanup)
	s.mu.Unlock()
}

// Close 幂等收口请求资源，探测租约始终最先释放。
func (s *State) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	lease := s.probe
	s.probe = nil
	cleanups := s.cleanups
	s.cleanups = nil
	s.mu.Unlock()
	if s.Runtime != nil {
		s.Runtime.ReleaseProbe(lease)
	}
	for _, cleanup := range cleanups {
		cleanup()
	}
}

// WaitRetry 等待同成员重试间隔。等待期间客户端取消时，必须归还本请求
// 可能持有的唯一探测槽，否则该车道后续所有半开探测都会永久阻塞。
func (s *State) WaitRetry(ctx context.Context, delay time.Duration) bool {
	if s == nil || delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		s.ReleaseProbe()
		return false
	}
}

// NoteLaneUnavailable 请求因"全部成员不可选"按 §4.2 收尾时调用：登记一次车道级
// 不可用事件（同一停摆期只发一次，恢复由 recordSuccess 收口，routing-spec §7）。
//
// 只对**真实的显式车道**发事件：未配车道的模型名（unconfigured）与停用车道
// （disabled）是配置问题，不是运行态故障，对它们发"车道不可用"是噪声。
func (s *State) NoteLaneUnavailable() {
	if s == nil || s.Route == nil || s.Runtime == nil {
		return
	}
	if s.Route.Source != model.RouteSourceExplicit || len(s.Route.Members) == 0 {
		return
	}
	s.Runtime.withLock(func() {
		if s.Runtime.generation != s.runtimeGeneration {
			return
		}
		s.Runtime.noteLaneUnavailable()
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
			WaitMs:     item.WaitMs,
		})
	}
	return out
}

// memberLabel 成员对外标签：`channel/model`（成员所选模型，ADR 0008）。
//
// 用**所选模型**而非派生真名：成员身份是 `(渠道, 模型)`，标签随之稳定，
// 不随渠道 `model_mapping` 改动漂移（真名变了不该让 attempts 链里的成员换个名字）。
// 代价：历史日志里仍是旧标签 `channel/真名`，与新日志并存——历史只作追溯，
// 本项目无对外兼容包袱，故不回溯改写（ADR 0008）。
func memberLabel(m *model.RouteMember) string {
	if m == nil {
		return ""
	}
	if m.Channel != "" {
		return m.Channel + "/" + m.Model
	}
	return m.Model
}

func truncate(err *types.NewAPIError, max int) string {
	if err == nil {
		return ""
	}
	// 脱敏后再截断：上游错误体可能回显 Authorization/key，而 attempts 链会进日志与 API
	msg := err.MaskSensitiveErrorWithStatusCode()
	return common.TruncateUTF8(msg, max)
}

// ---------- gin.Context 往来 ----------

// Attach 把请求态挂到上下文。
func Attach(c *gin.Context, s *State) {
	if c == nil || s == nil {
		return
	}
	c.Set(contextKey, s)
}

// From 取出请求态；未经 PBRServe 注入路由态的请求（如渠道测试直连）返回 nil。
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

// CodeLocalThrottle 本地出站准入拒绝的稳定错误码（routing-spec §4.2/§3.2）。
const CodeLocalThrottle = "local_throttle"

// LocalThrottleError 构造本地 429（队列满/排队超界）：连接仍然健康，
// 只是本地配额忙——**不是 503、不是成员失败**（ADR 0012 第 9 条）。
func LocalThrottleError(modelName string, wait time.Duration) *types.NewAPIError {
	if wait < time.Second {
		wait = time.Second
	}
	seconds := int((wait + time.Second - 1) / time.Second)
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "Too many concurrent requests for model " + modelName,
		Type:    "rate_limit_error",
		Code:    CodeLocalThrottle,
	}, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
	err.RetryAfterSeconds = seconds
	return err
}

// IsLocalThrottle 判定错误是否为本地准入拒绝。
func IsLocalThrottle(err *types.NewAPIError) bool {
	return err != nil && err.GetErrorCode() == CodeLocalThrottle
}
