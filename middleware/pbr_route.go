package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"pbr/common"
	"pbr/constant"
	"pbr/internal/apierr"
	"pbr/internal/route"
	"pbr/logger"
	"pbr/model"
	"pbr/relaykit/types"
	"pbr/service"

	"github.com/gin-gonic/gin"
)

// 本文件把"模型名键控路由"接进转发管道（routing-spec §1.1 / §3）。
//
// 改造的落点只有两处，且共用同一个请求态（internal/route.State）：
//   - 首次选路：middleware.Distribute() 顶部的 PBRServe()；
//   - 重试选路：controller/relay.go 的 getChannel() → PBRNextChannel()。
//
// 不适用 PBR 的请求（任务插件、显式渠道 pin、非模型面）返回 false，交回迁移前的旧链路，
// 保证"只裁计费与多用户、其余功能保留"（design-v1 §1.4）。

// PBRServe 尝试用 PBR 路由接管本次请求的渠道选择。
//
// 返回 true 表示已处理完毕（成功注入渠道，或已写出 503）；false 表示不适用，调用方应走旧链路。
func PBRServe(c *gin.Context) bool {
	if c.GetString("expected_task_plugin_key") != "" {
		return false
	}
	if _, found, _ := service.GetChannelConstraints(c).ResolvedPin(); found {
		// 显式渠道 pin（如 sk-<key>-<channelId>）语义是"就要这个渠道"，不走模型名键控。
		return false
	}
	modelRequest, shouldSelectChannel, err := getModelRequest(c)
	if err != nil || !shouldSelectChannel {
		return false
	}
	modelName := strings.TrimSpace(modelRequest.Model)
	if modelName == "" {
		// 交给旧链路产出原有的"模型名必填"错误形态。
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
	if _, ok := PBRNextChannel(c, state, modelName, nil); !ok {
		// 无任何渠道声明该模型，或全部成员都不可选，与"全部成员耗尽"同形
		// （routing-spec §1.1 第 3 条）。
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
func PBRNextChannel(c *gin.Context, state *route.State, modelName string, lastErr *types.NewAPIError) (*model.Channel, bool) {
	if state == nil {
		return nil, false
	}
	first := true
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
			// 候选成员自身不可用（渠道缺失/被禁用/取不到 key）：直接换下一个，
			// 不把它计入"尝试失败"——失败计数应来自真实的上游尝试（routing-spec §3.1）。
			member, delay, ok = state.Next(nil)
		}
		if !ok {
			return nil, false
		}
		if delay > 0 {
			// 同成员原地重试的间隔（routing-spec §3.1 / 六键 member_retry_interval_seconds）。
			time.Sleep(delay)
		}

		channel, err := model.GetChannelById(member.ChannelId, true)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			logger.LogWarn(c, fmt.Sprintf("pbr: 跳过不可用成员 channel_id=%d model=%s", member.ChannelId, member.UpstreamModel))
			// 该成员可能是被放行的"探测"，跳过它必须归还探测槽，否则槽被永久占住
			state.ReleaseProbe()
			continue
		}
		if apiErr := SetupContextForSelectedChannel(c, channel, modelName); apiErr != nil {
			logger.LogWarn(c, fmt.Sprintf("pbr: 成员 channel_id=%d model=%s 注入上下文失败: %s", member.ChannelId, member.UpstreamModel, apiErr.Error()))
			state.ReleaseProbe()
			continue
		}
		// 成员级改名：上游真名与路由键解耦（design-v1 §3.3）。
		common.SetContextKey(c, constant.ContextKeyPBRUpstreamModel, member.UpstreamModel)
		if carrier := model.GetPBRLogCarrier(c); carrier != nil {
			carrier.ChannelId = channel.Id
			carrier.ChannelName = channel.Name
			carrier.UpstreamModel = member.UpstreamModel
		}
		// 本次尝试的超时（routing-spec §8）：车道默认 + 成员级覆盖。
		cfg := state.Route.EffectiveConfig(member)
		common.SetContextKey(c, constant.ContextKeyPBRNonStreamTimeout, cfg.MemberNonStreamResponseTimeoutSeconds)
		common.SetContextKey(c, constant.ContextKeyPBRStreamFirstEventTimeout, cfg.MemberStreamFirstEventTimeoutSeconds)
		SetServedByHeader(c, channel, member)
		return channel, true
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
