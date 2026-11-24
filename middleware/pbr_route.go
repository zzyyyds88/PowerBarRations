package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/logger"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
)

// 本文件把"模型名键控路由"接进转发管道（routing-spec §1.1 / §3）。
//
// 改造的落点只有两处，且共用同一个请求态（internal/route.State）：
//   - 首次选路：middleware.Distribute() 顶部的 PBRServe()；
//   - 重试选路：controller/relay.go 的 getChannel() → PBRNextChannel()。
//
// PBRServe 返回 false 仅表示"模型名解析失败 / 模型名为空"这类无法确定路由键的
// 请求，由 Distribute 产出 400；显式渠道 pin（sk-<key>-<channelId>）已随产品收敛
// 物理删除，车道是唯一选路入口（design-v1 §3.3）。

// PBRServe 尝试用 PBR 路由接管本次请求的渠道选择。
//
// 返回 true 表示已处理完毕（成功注入渠道，或已写出 403 / 503）；false 表示请求
// 无法确定路由键（模型名解析失败或为空），调用方应产出 400。
func PBRServe(c *gin.Context) bool {
	modelRequest, err := getModelRequest(c)
	if err != nil {
		return false
	}
	modelName := strings.TrimSpace(modelRequest.Model)
	if modelName == "" {
		// 交给 Distribute 产出原有的"模型名必填"400 错误形态。
		return false
	}
	// 令牌的车道权限判在"规范化后的路由键"上：别名点名先归一到所属车道。
	policySubject := modelName
	if prefetched, preErr := model.ResolveRoute(modelName); preErr == nil && prefetched.RouteKey != "" {
		policySubject = prefetched.RouteKey
	}

	// 日志载体：鉴权已过即建立，使"路由阶段就结束"的请求（403 / 503）也留一行日志。
	carrier := model.EnsurePBRLogCarrier(c)
	carrier.Lane = modelName
	carrier.RequestModel = modelName
	carrier.InboundFormat = inboundFormatOf(c.Request.URL.Path)

	// 车道权限：默认放行全部车道，只能显式拒绝（token-spec §3.2）。
	// 拒绝返回 403 forbidden_scope，而不是 400——便于 AI 区分"权限"与"请求不合法"。
	if clientKey := PBRClientKeyFrom(c); clientKey != nil {
		if policy := model.ParseLanePolicy(clientKey.LanePolicy); !policy.AllowsLane(policySubject) {
			message := "client key '" + clientKey.Name + "' is not allowed to use lane '" + policySubject + "'"
			carrier.Lane = policySubject
			carrier.Success = false
			carrier.HTTPStatus = http.StatusForbidden
			carrier.ErrorKind = string(route.KindClientError)
			carrier.ErrorSummary = message
			model.WritePBRLog(c)
			apierr.Write(c, http.StatusForbidden, apierr.CodeForbiddenScope, message, "")
			return true
		}
	}

	resolved, err := model.ResolveRoute(modelName)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("pbr: 解析路由失败 model=%q: %s", modelName, err.Error()))
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "route resolve failed"}})
		return true
	}

	carrier.RouteSource = resolved.Source
	carrier.Lane = resolved.Model

	state := route.NewState(resolved)
	if _, throttleErr, ok := PBRNextChannel(c, state, modelName, nil); !ok {
		if throttleErr != nil {
			// 本地准入拒绝（不是车道不可用）：429 + Retry-After，不登记 lane_unavailable。
			writeLocalThrottle(c, state, throttleErr)
			return true
		}
		// 无任何渠道声明该模型，或全部成员都不可选，与"全部成员耗尽"同形
		// （routing-spec §1.1 第 3 条）。
		// 车道级事件：请求按"全部成员不可选"收尾时登记一次 lane_unavailable
		// （同一停摆期只发一次；未配车道/停用车道是配置问题，内部会自行过滤，§7）。
		state.NoteLaneUnavailable()
		// 快抛前先落日志：事后要能解释"为什么是 503"（routing-spec §4.2）。
		// 这里必须把**本次已选路记录**（被冷却/熔断跳过的成员）一并写入，
		// 否则"为什么没用 P1"这一类关键证据在最需要它的快抛路径上丢失。
		if attempts := state.LogAttempts(); len(attempts) > 0 {
			carrier.Attempts = attempts
			carrier.TotalAttempts = len(attempts)
		}
		carrier.Success = false
		carrier.HTTPStatus = http.StatusServiceUnavailable
		carrier.ErrorKind = string(route.KindSoftTransient)
		carrier.ErrorSummary = route.NoAvailableMessage(modelName)
		model.WritePBRLog(c)
		// 快抛路径不得残留 X-Served-By（本路径尚未选中成员时为无操作）。
		ClearServedByHeader(c)
		route.WriteNoAvailableChannel(c, modelName)
		return true
	}
	route.Attach(c, state)
	return true
}

// PBRNextChannel 从请求态取出下一个应尝试的成员，并完成渠道上下文注入。
//
// lastErr 为上一次尝试的错误；首次调用传 nil。返回 false 表示没有可用成员，
// 调用方应按 routing-spec §4.2 快抛 503。
// pbrAdmissionTicketKey 当前尝试持有的准入槽位（controller 在尝试结束时释放）。
const pbrAdmissionTicketKey = "pbr_admission_ticket"

// TakeAdmissionTicket 取出并清空本请求当前的准入槽位（幂等）。
func TakeAdmissionTicket(c *gin.Context) *route.AdmissionTicket {
	if c == nil {
		return nil
	}
	value, ok := c.Get(pbrAdmissionTicketKey)
	if !ok {
		return nil
	}
	c.Set(pbrAdmissionTicketKey, nil)
	ticket, _ := value.(*route.AdmissionTicket)
	return ticket
}

func setAdmissionTicket(c *gin.Context, ticket *route.AdmissionTicket) {
	if c == nil || ticket == nil {
		return
	}
	c.Set(pbrAdmissionTicketKey, ticket)
}

// writeLocalThrottle 写出本地 429（routing-spec §4.2 例外）并落日志。
func writeLocalThrottle(c *gin.Context, state *route.State, throttleErr *types.NewAPIError) {
	if throttleErr == nil {
		return
	}
	if throttleErr.RetryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.Itoa(throttleErr.RetryAfterSeconds))
	}
	carrier := model.EnsurePBRLogCarrier(c)
	carrier.Success = false
	carrier.HTTPStatus = http.StatusTooManyRequests
	carrier.ErrorKind = string(route.KindLocalThrottle)
	carrier.ErrorSummary = throttleErr.MaskSensitiveError()
	if state != nil {
		if attempts := state.LogAttempts(); len(attempts) > 0 {
			carrier.Attempts = attempts
			carrier.TotalAttempts = len(attempts)
		}
	}
	model.WritePBRLog(c)
	ClearServedByHeader(c)
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": throttleErr.ToOpenAIError()})
}

// channelRateLimitFor 取该成员（按所选模型寻址）的准入配置。
func channelRateLimitFor(channel *model.Channel, member *model.RouteMember) route.RateLimitConfig {
	if channel == nil || member == nil {
		return route.RateLimitConfig{}
	}
	for _, item := range channel.GetSetting().PBRRateLimits {
		if strings.TrimSpace(item.Model) != strings.TrimSpace(member.Model) {
			continue
		}
		limit := item.QueueLimit
		if limit == 0 {
			limit = 32
		}
		return route.RateLimitConfig{
			MaxConcurrency:   item.MaxConcurrency,
			MaxRPM:           item.MaxRPM,
			QueueLimit:       limit,
			QueueWaitSeconds: item.QueueWaitSeconds,
		}
	}
	return route.RateLimitConfig{}
}

// PBRNextChannel 推进成员链并完成出站准入（routing-spec §3/§3.2）。
//
// 返回 (*channel, *localThrottleError, ok)：
//   - ok=true 表示已取到渠道与准入槽位（槽位可能为 nil = 未启用限流）；
//   - ok=false 且 err!=nil 表示本地准入拒绝（429 + Retry-After）；
//   - ok=false 且 err==nil 表示车道真正不可用（503）。
func PBRNextChannel(c *gin.Context, state *route.State, modelName string, lastErr *types.NewAPIError) (*model.Channel, *types.NewAPIError, bool) {
	if state == nil {
		return nil, nil, false
	}
	cfgCache := map[string]route.RateLimitConfig{}
	cfgFor := func(member *model.RouteMember) route.RateLimitConfig {
		key := route.AdmissionKey(member.ChannelId, member.Model)
		if cfg, ok := cfgCache[key]; ok {
			return cfg
		}
		cfg := route.RateLimitConfig{}
		if channel, err := model.GetChannelById(member.ChannelId, true); err == nil && channel != nil {
			cfg = channelRateLimitFor(channel, member)
		}
		cfgCache[key] = cfg
		return cfg
	}
	state.SetAdmissionReady(func(member *model.RouteMember) bool {
		cfg := cfgFor(member)
		if !cfg.Enabled() {
			return true
		}
		return route.DefaultAdmission.Ready(route.AdmissionKey(member.ChannelId, member.Model), cfg)
	})

	first := true
	throttledAny := false
	var retryWait time.Duration
	for {
		var (
			member *model.RouteMember
			delay  time.Duration
			ok     bool
		)
		if first {
			member, delay, ok = state.Next(lastErr)
			first = false
		} else {
			// 候选成员自身不可用（渠道缺失/被禁用/取不到 key）或准入未就绪：
			// 直接换下一个，不计入"尝试失败"（routing-spec §3.1）。
			member, delay, ok = state.Next(nil)
		}
		if !ok {
			candidate := state.ThrottledMember()
			if candidate == nil {
				if throttledAny {
					if retryWait < time.Second {
						retryWait = time.Second
					}
					return nil, route.LocalThrottleError(modelName, retryWait), false
				}
				return nil, nil, false
			}
			key := route.AdmissionKey(candidate.ChannelId, candidate.Model)
			cfg := cfgFor(candidate)
			throttledAny = true
			if estimate := route.DefaultAdmission.Estimate(key, cfg); estimate > 0 &&
				(retryWait == 0 || estimate < retryWait) {
				retryWait = estimate
			}
			if cfg.QueueWaitSeconds <= 0 {
				state.ExhaustThrottled(0)
				continue
			}
			ticket, wait, waitErr := route.DefaultAdmission.Wait(c.Request.Context(),
				key, state.Route.Model, cfg)
			if wait > 0 && (retryWait == 0 || wait < retryWait) {
				retryWait = wait
			}
			if waitErr != nil {
				state.ExhaustThrottled(wait.Milliseconds())
				continue
			}
			committed, committedOK := state.CommitThrottled()
			if !committedOK {
				ticket.Release("", 0)
				state.ExhaustThrottled(wait.Milliseconds())
				continue
			}
			member = committed
			setAdmissionTicket(c, ticket)
		}
		if delay > 0 {
			// 同成员原地重试的间隔（routing-spec §3.1 / member_retry_interval_seconds）。
			select {
			case <-time.After(delay):
			case <-c.Request.Context().Done():
				return nil, nil, false
			}
		}

		channel, err := model.GetChannelById(member.ChannelId, true)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			if ticket := TakeAdmissionTicket(c); ticket != nil {
				ticket.Release("", 0)
			}
			logger.LogWarn(c, fmt.Sprintf("pbr: 跳过不可用成员 channel_id=%d model=%s", member.ChannelId, member.UpstreamModel))
			// routing-spec §5.3：渠道停用/缺失属配置态跳过（channel_disabled），
			// 不写冷却、不计熔断分；只耗尽本请求内该成员预算并留痕。
			state.ReportMemberConfigSkip(member, "channel disabled or missing")
			// 该成员可能是被放行的"探测"，跳过它必须归还探测槽，否则槽被永久占住
			state.ReleaseProbe()
			continue
		}
		if apiErr := SetupContextForSelectedChannel(c, channel, modelName); apiErr != nil {
			if ticket := TakeAdmissionTicket(c); ticket != nil {
				ticket.Release("", 0)
			}
			logger.LogWarn(c, fmt.Sprintf("pbr: 成员 channel_id=%d model=%s 注入上下文失败: %s", member.ChannelId, member.UpstreamModel, apiErr.Error()))
			// 上下文注入失败 = 该成员当前配置不可用（缺 key/base_url 等），
			// 同样是配置态跳过，不惩罚成员。
			state.ReportMemberConfigSkip(member, "context injection failed: "+apiErr.Error())
			state.ReleaseProbe()
			continue
		}

		// 未经排队路径的成员（已选路但尚未取槽）在此非阻塞取槽。
		if existing := TakeAdmissionTicket(c); existing != nil {
			setAdmissionTicket(c, existing)
		} else {
			cfg := cfgFor(member)
			if cfg.Enabled() {
				key := route.AdmissionKey(member.ChannelId, member.Model)
				ticket, acquireErr := route.DefaultAdmission.Acquire(key, cfg)
				if acquireErr != nil {
					// 竞态：选路时还就绪，取槽时已被同车道并发请求占满。
					throttledAny = true
					if estimate := route.DefaultAdmission.Estimate(key, cfg); estimate > 0 &&
						(retryWait == 0 || estimate < retryWait) {
						retryWait = estimate
					}
					state.ExhaustThrottled(0)
					continue
				}
				setAdmissionTicket(c, ticket)
			}
		}

		// 成员级改名：上游真名与路由键解耦（design-v1 §3.3）。
		common.SetContextKey(c, constant.ContextKeyPBRUpstreamModel, member.UpstreamModel)
		if carrier := model.GetPBRLogCarrier(c); carrier != nil {
			carrier.ChannelId = channel.Id
			carrier.ChannelName = channel.Name
			carrier.UpstreamModel = member.UpstreamModel
			// 计价键 = 成员所选模型（design §16.9#7：渠道 × 模型，与车道无关）。
			carrier.MemberModel = member.Model
		}
		// 本次尝试的超时（routing-spec §8）：车道默认 + 成员级覆盖。
		cfg := state.Route.EffectiveConfig(member)
		common.SetContextKey(c, constant.ContextKeyPBRNonStreamTimeout, cfg.MemberNonStreamResponseTimeoutSeconds)
		common.SetContextKey(c, constant.ContextKeyPBRStreamFirstEventTimeout, cfg.MemberStreamFirstEventTimeoutSeconds)
		SetServedByHeader(c, channel, member)
		return channel, nil, true
	}
}

// inboundFormatOf 由路径推断入站协议（日志用，不参与路由）。
func inboundFormatOf(path string) string {
	switch {
	case strings.Contains(path, "/messages"):
		return "anthropic"
	case strings.Contains(path, "embeddings"):
		return "embeddings"
	case strings.Contains(path, "/responses"):
		return "openai_responses"
	case strings.Contains(path, "/v1beta"):
		return "gemini"
	default:
		return "openai"
	}
}

// SetServedByHeader 记录并回传实际服务者，形态固定为
// `channel=<id>:<name>, model=<upstream>`（design-v1 §4.1）。
func SetServedByHeader(c *gin.Context, channel *model.Channel, member *model.RouteMember) {
	if c == nil || channel == nil || member == nil {
		return
	}
	value := fmt.Sprintf("channel=%d:%s, model=%s", channel.Id, channel.Name, member.UpstreamModel)
	c.Header("X-Served-By", value)
	common.SetContextKey(c, constant.ContextKeyPBRServedBy, value)
}

// ClearServedByHeader 清除"实际服务者"标记：失败/503/快抛收尾时调用，保证
// X-Served-By 只出现在成功响应上（design-v1 §4.1）。
//
// 选中成员即写头，若错误收尾不清，失败响应的下游会看到一个实际并未成功服务的
// channel/model 组合，误判"这次到底走的谁"。上下文记录一并清除，避免下游把它
// 当成"本次确实被该成员服务过"。
func ClearServedByHeader(c *gin.Context) {
	if c == nil {
		return
	}
	c.Writer.Header().Del("X-Served-By")
	delete(c.Keys, string(constant.ContextKeyPBRServedBy))
}
