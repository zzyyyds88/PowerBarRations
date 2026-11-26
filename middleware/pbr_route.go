package middleware

import (
	"context"
	"errors"
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

	// 请求级不可变路由快照只解析一次：policy、成员、渠道配置、模型映射、六键与
	// 准入配置必须来自同一版本（ADR 0014）。
	resolved, err := model.ResolveRoute(modelName)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("pbr: 解析路由失败 model=%q: %s", modelName, err.Error()))
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "route resolve failed"}})
		return true
	}

	// 日志载体：鉴权已过即建立，使"路由阶段就结束"的请求（403 / 503）也留一行日志。
	carrier := model.EnsurePBRLogCarrier(c)
	carrier.Lane = resolved.RouteKey
	if carrier.Lane == "" {
		carrier.Lane = modelName
	}
	carrier.RequestModel = modelName
	carrier.InboundFormat = inboundFormatOf(c.Request.URL.Path)
	carrier.RouteSource = resolved.Source
	if resolved.ConfigError != nil {
		carrier.Success = false
		carrier.HTTPStatus = http.StatusServiceUnavailable
		carrier.ErrorKind = string(route.KindSoftTransient)
		carrier.ErrorSummary = "lane config invalid: " + resolved.ConfigError.Code
		model.WritePBRLog(c)
		logger.LogError(c, fmt.Sprintf("pbr: 车道配置无效 model=%q code=%s path=%s", modelName, resolved.ConfigError.Code, resolved.ConfigError.Path))
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "lane config invalid"}})
		return true
	}

	// 车道权限：默认放行全部车道，只能显式拒绝（token-spec §3.2）。
	if clientKey := PBRClientKeyFrom(c); clientKey != nil {
		if policy := model.ParseLanePolicy(clientKey.LanePolicy); !policy.AllowsLane(carrier.Lane) {
			message := "client key '" + clientKey.Name + "' is not allowed to use lane '" + carrier.Lane + "'"
			carrier.Success = false
			carrier.HTTPStatus = http.StatusForbidden
			carrier.ErrorKind = string(route.KindClientError)
			carrier.ErrorSummary = message
			model.WritePBRLog(c)
			apierr.Write(c, http.StatusForbidden, apierr.CodeForbiddenScope, message, "")
			return true
		}
	}

	state := route.NewState(resolved)
	state.SetAdmissionDeadline(admissionDeadlineFor(c, state))
	keepState := false
	defer func() {
		if !keepState {
			state.Close()
		}
	}()
	if lease := PBRClientKeyLeaseFrom(c); lease != nil {
		state.AddCleanup(lease.Release)
	}
	if _, nextErr, ok := PBRNextChannel(c, state, modelName, nil); !ok {
		if nextErr != nil {
			if route.IsLocalThrottle(nextErr) {
				writeLocalThrottle(c, state, nextErr)
				return true
			}
			if isPBRCanceled(nextErr) {
				writePBRCanceled(c, state, nextErr)
				return true
			}
		}
		// 无任何渠道声明该模型，或全部成员都不可选，与"全部成员耗尽"同形。
		state.NoteLaneUnavailable()
		if attempts := state.LogAttempts(); len(attempts) > 0 {
			carrier.Attempts = attempts
			carrier.TotalAttempts = len(attempts)
		}
		carrier.Success = false
		carrier.HTTPStatus = http.StatusServiceUnavailable
		carrier.ErrorKind = string(route.KindSoftTransient)
		carrier.ErrorSummary = route.NoAvailableMessage(modelName)
		model.WritePBRLog(c)
		ClearServedByHeader(c)
		route.WriteNoAvailableChannel(c, modelName)
		return true
	}
	route.Attach(c, state)
	keepState = true
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

func setAdmissionTicket(c *gin.Context, state *route.State, ticket *route.AdmissionTicket) {
	if c == nil || ticket == nil {
		return
	}
	c.Set(pbrAdmissionTicketKey, ticket)
	if state != nil {
		state.AddCleanup(func() { ticket.Release(route.AdmissionNeutral, "", 0) })
	}
}

// ReleasePBRRequestResources 幂等释放当前请求的准入槽位与路由 State。relay、取消
// 回调与 defer 可以重复调用；AdmissionTicket.Release 与 State.Close 都保证只生效一次。
func ReleasePBRRequestResources(c *gin.Context) {
	if c == nil {
		return
	}
	if ticket := TakeAdmissionTicket(c); ticket != nil {
		ticket.Release(route.AdmissionNeutral, "", 0)
	}
	if state := route.From(c); state != nil {
		state.Close()
	}
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

func pbrCanceledError(err error) *types.NewAPIError {
	if err == nil {
		err = context.Canceled
	}
	return types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, 499, types.ErrOptionWithSkipRetry())
}

func isPBRCanceled(err *types.NewAPIError) bool {
	return err != nil && (err.StatusCode == 499 || errors.Is(err, context.Canceled))
}

func writePBRCanceled(c *gin.Context, state *route.State, canceledErr *types.NewAPIError) {
	carrier := model.EnsurePBRLogCarrier(c)
	carrier.Success = false
	carrier.HTTPStatus = 499
	carrier.ErrorKind = string(route.KindCanceled)
	carrier.ErrorSummary = "client canceled the request"
	if state != nil {
		if attempts := state.LogAttempts(); len(attempts) > 0 {
			carrier.Attempts = attempts
			carrier.TotalAttempts = len(attempts)
		}
	}
	model.WritePBRLog(c)
	ClearServedByHeader(c)
	if canceledErr != nil {
		c.AbortWithStatusJSON(499, gin.H{"error": canceledErr.ToOpenAIError()})
		return
	}
	c.AbortWithStatusJSON(499, gin.H{"error": gin.H{"message": "client canceled the request"}})
}

// channelRateLimitFor 从成员快照读取准入配置；请求路径不解析渠道 JSON、不查库。
func channelRateLimitFor(member *model.RouteMember) route.RateLimitConfig {
	if member == nil || member.RateLimit == nil {
		return route.RateLimitConfig{Mode: route.AdmissionModeDisabled}
	}
	item := member.RateLimit
	mode := route.AdmissionModeFixed
	if item.MaxConcurrency == -1 {
		mode = route.AdmissionModeAdaptive
	}
	queueLimit := item.QueueLimit
	if queueLimit <= 0 {
		queueLimit = 32
	}
	return route.RateLimitConfig{
		Mode:             mode,
		MaxConcurrency:   item.MaxConcurrency,
		MaxRPM:           item.MaxRPM,
		QueueLimit:       queueLimit,
		QueueWaitSeconds: item.QueueWaitSeconds,
	}
}

// admissionDeadlineFor 为一次请求的所有候选建立共享的绝对准入 deadline。客户端
// context deadline 优先；否则以候选配置中最长的 queue_wait_seconds 作为总预算，
// 单个成员的 queue_wait_seconds 只能缩短、不能延长它。
func admissionDeadlineFor(c *gin.Context, state *route.State) time.Time {
	if c == nil || c.Request == nil {
		return time.Time{}
	}
	deadline, _ := c.Request.Context().Deadline()
	var longest time.Duration
	if state != nil && state.Route != nil {
		for i := range state.Route.Members {
			limit := state.Route.Members[i].RateLimit
			if limit == nil || limit.QueueWaitSeconds <= 0 {
				continue
			}
			if wait := time.Duration(limit.QueueWaitSeconds) * time.Second; wait > longest {
				longest = wait
			}
		}
	}
	if longest > 0 {
		candidate := time.Now().Add(longest)
		if deadline.IsZero() || candidate.Before(deadline) {
			deadline = candidate
		}
	}
	return deadline
}

// PBRNextChannel 推进成员链并完成出站准入（routing-spec §3/§3.2）。所有选路、渠道
// 配置与准入配置均消费请求级快照；本函数不访问数据库。
func PBRNextChannel(c *gin.Context, state *route.State, modelName string, lastErr *types.NewAPIError) (*model.Channel, *types.NewAPIError, bool) {
	if state == nil {
		return nil, nil, false
	}
	ctx := c.Request.Context()
	if err := ctx.Err(); err != nil {
		return nil, pbrCanceledError(err), false
	}
	admissionDeadline := state.AdmissionDeadline()
	if admissionDeadline.IsZero() {
		// 兼容直接调用 PBRNextChannel 的测试/内部入口；生产 PBRServe 已设置。
		admissionDeadline = admissionDeadlineFor(c, state)
		state.SetAdmissionDeadline(admissionDeadline)
	}
	state.SetAdmissionReady(func(member *model.RouteMember) bool {
		cfg := channelRateLimitFor(member)
		if !cfg.Enabled() {
			return true
		}
		return route.DefaultAdmission.Ready(route.AdmissionKeyOf(member), cfg)
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
			member, delay, ok = state.Next(nil)
		}
		if !ok {
			if err := ctx.Err(); err != nil {
				return nil, pbrCanceledError(err), false
			}
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
			key := route.AdmissionKeyOf(candidate)
			cfg := channelRateLimitFor(candidate)
			throttledAny = true
			if estimate := route.DefaultAdmission.Estimate(key, cfg); estimate > 0 &&
				(retryWait == 0 || estimate < retryWait) {
				retryWait = estimate
			}
			if cfg.QueueWaitSeconds <= 0 {
				state.ExhaustThrottled(0)
				continue
			}
			ticket, wait, waitErr := route.DefaultAdmission.Wait(ctx, key, state.Route.Model, cfg, admissionDeadline)
			if wait > 0 && (retryWait == 0 || wait < retryWait) {
				retryWait = wait
			}
			if waitErr != nil {
				if errors.Is(waitErr, context.Canceled) {
					return nil, pbrCanceledError(waitErr), false
				}
				if errors.Is(waitErr, context.DeadlineExceeded) {
					if ctx.Err() != nil {
						return nil, pbrCanceledError(waitErr), false
					}
					return nil, route.LocalThrottleError(modelName, retryWait), false
				}
				if errors.Is(waitErr, route.ErrAdmissionWaitTimeout) &&
					!admissionDeadline.IsZero() && !time.Now().Before(admissionDeadline) {
					return nil, route.LocalThrottleError(modelName, retryWait), false
				}
				state.ExhaustThrottled(wait.Milliseconds())
				continue
			}
			committed, committedOK := state.CommitThrottled()
			if !committedOK {
				ticket.Release(route.AdmissionNeutral, "", 0)
				state.ReleaseProbe()
				state.ExhaustThrottled(wait.Milliseconds())
				continue
			}
			member = committed
			setAdmissionTicket(c, state, ticket)
		}
		if delay > 0 {
			if !state.WaitRetry(ctx, delay) {
				if err := ctx.Err(); err != nil {
					return nil, pbrCanceledError(err), false
				}
				return nil, nil, false
			}
		}

		channel := member.ChannelSnapshot
		if channel == nil || !member.ChannelEnabled || member.ConfigError != nil || member.OverrideError != nil {
			if ticket := TakeAdmissionTicket(c); ticket != nil {
				ticket.Release(route.AdmissionNeutral, "", 0)
			}
			reason := "channel disabled or missing"
			if member.ConfigError != nil {
				reason = "channel config error: " + member.ConfigError.Code
			} else if member.OverrideError != nil {
				reason = "member config error: " + member.OverrideError.Code
			}
			logger.LogWarn(c, fmt.Sprintf("pbr: 跳过不可用成员 channel_id=%d model=%s reason=%s", member.ChannelId, member.UpstreamModel, reason))
			state.ReportMemberConfigSkip(member, reason)
			state.ReleaseProbe()
			continue
		}
		if apiErr := SetupContextForSelectedChannel(c, channel, modelName); apiErr != nil {
			if ticket := TakeAdmissionTicket(c); ticket != nil {
				ticket.Release(route.AdmissionNeutral, "", 0)
			}
			logger.LogWarn(c, fmt.Sprintf("pbr: 成员 channel_id=%d model=%s 注入上下文失败: %s", member.ChannelId, member.UpstreamModel, apiErr.Error()))
			state.ReportMemberConfigSkip(member, "context injection failed: "+apiErr.Error())
			state.ReleaseProbe()
			continue
		}

		// 未经排队路径的成员在此非阻塞取槽。
		if existing := TakeAdmissionTicket(c); existing != nil {
			setAdmissionTicket(c, state, existing)
		} else {
			cfg := channelRateLimitFor(member)
			if cfg.Enabled() {
				key := route.AdmissionKeyOf(member)
				ticket, acquireErr := route.DefaultAdmission.Acquire(key, cfg)
				if acquireErr != nil {
					if err := ctx.Err(); err != nil {
						return nil, pbrCanceledError(err), false
					}
					state.ReleaseProbe()
					throttledAny = true
					if estimate := route.DefaultAdmission.Estimate(key, cfg); estimate > 0 &&
						(retryWait == 0 || estimate < retryWait) {
						retryWait = estimate
					}
					if !admissionDeadline.IsZero() && !time.Now().Before(admissionDeadline) {
						return nil, route.LocalThrottleError(modelName, retryWait), false
					}
					state.ExhaustThrottled(0)
					continue
				}
				setAdmissionTicket(c, state, ticket)
			}
		}

		common.SetContextKey(c, constant.ContextKeyPBRUpstreamModel, member.UpstreamModel)
		if carrier := model.GetPBRLogCarrier(c); carrier != nil {
			carrier.ChannelId = channel.Id
			carrier.ChannelName = channel.Name
			carrier.UpstreamModel = member.UpstreamModel
			carrier.MemberModel = member.Model
		}
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
