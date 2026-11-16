package async

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/logger"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relay"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/openai"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
	"github.com/zzyyyds88/PowerBarRations/service"

	"gorm.io/gorm"
)

// selection 一次异步请求实际要打的渠道。状态字段只对"创建/列表"这类需要
// 参与运行态（冷却/熔断/探测槽）的请求有值；已有对象的 retrieve/content/cancel
// 固定回记录的渠道，不使用运行态（ADR 0010 第 3/4 条）。
type selection struct {
	channel *model.Channel
	key     string
	member  *model.RouteMember
	state   *route.State
}

func (s *selection) attemptMember(kind string) string {
	if s == nil {
		return ""
	}
	if s.member != nil {
		return memberLabel(s.member)
	}
	if s.channel != nil {
		return s.channel.Name + "/" + kind
	}
	return kind
}

// chooseChannel 按车道当前可用成员选**第一个**，与同步请求共用同一套冷却/熔断/
// 人工停用过滤。选中后只打这一个成员，不重试、不 failover。
//
// 注意：这里跳过"渠道缺失/停用/无 key"的成员，这不是上游 failover——那些成员
// 根本没有可用的上游身份；真正的上游失败由调用方直接返回。
func (h *Handler) chooseChannel(c *gin.Context, laneName string, log *asyncLog) (*selection, bool) {
	resolved, err := model.ResolveRoute(laneName)
	if err != nil || resolved == nil {
		log.fail(string(route.KindSoftTransient), "resolve lane route failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"resolve lane route failed", "")
		return nil, false
	}
	if resolved.Source != model.RouteSourceExplicit || len(resolved.Members) == 0 {
		log.fail(string(route.KindSoftTransient), route.NoAvailableMessage(laneName))
		route.WriteNoAvailableChannel(c, laneName)
		return nil, false
	}

	state := route.NewState(resolved)
	seen := make(map[int]bool, len(resolved.Members))
	for i := 0; i <= len(resolved.Members); i++ {
		member, _, ok := state.Next(nil)
		if !ok {
			h.writeNoAvailable(c, laneName, log, state)
			return nil, false
		}
		if member == nil {
			h.writeNoAvailable(c, laneName, log, state)
			return nil, false
		}
		if seen[member.ChannelId] {
			// 同一渠道被不同成员重复选中（manual 点名、别名等）。异步对象只认
			// 渠道，重复没有意义；避免在某些模式下死循环。
			h.writeNoAvailable(c, laneName, log, state)
			return nil, false
		}
		seen[member.ChannelId] = true

		channel, err := model.GetChannelById(member.ChannelId, true)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			logger.LogWarn(c, fmt.Sprintf(
				"pbr async: skip unavailable member channel_id=%d lane=%s", member.ChannelId, laneName))
			state.ReportMemberUnavailable(member, "channel disabled or missing")
			state.ReleaseProbe()
			continue
		}
		if _, err := asyncBaseURL(channel); err != nil {
			logger.LogWarn(c, fmt.Sprintf(
				"pbr async: skip member with invalid base URL channel_id=%d lane=%s: %s",
				member.ChannelId, laneName, err.Error()))
			state.ReportMemberUnavailable(member, "invalid channel base URL")
			state.ReleaseProbe()
			continue
		}
		key, _, keyErr := channel.GetNextEnabledKey()
		if keyErr != nil || !usableChannelKey(key) {
			logger.LogWarn(c, fmt.Sprintf(
				"pbr async: skip member without key channel_id=%d lane=%s", member.ChannelId, laneName))
			state.ReportMemberUnavailable(member, "no available channel key")
			state.ReleaseProbe()
			continue
		}

		log.setChannel(channel)
		if !isOpenAICompatibleChannelType(channel.Type) {
			// 协议边界是硬拒绝，不尝试下一个成员：ADR 0010 第 5 条要求明确
			// 告诉调用方"这条车道不能承载异步对象"，而不是悄悄换一个渠道。
			state.ReleaseProbe()
			log.fail(string(route.KindClientError), fmt.Sprintf(
				"channel type %d does not speak the OpenAI Files/Batches protocol", channel.Type))
			apierr.Write(c, http.StatusBadRequest, CodeUnsupportedProtocolForAsync,
				fmt.Sprintf("channel '%s' (type %d) does not speak the OpenAI Files/Batches protocol", channel.Name, channel.Type),
				"attach an OpenAI-compatible channel to lane '"+laneName+"'")
			return nil, false
		}

		return &selection{
			channel: channel,
			key:     key,
			member:  member,
			state:   state,
		}, true
	}

	h.writeNoAvailable(c, laneName, log, state)
	return nil, false
}

func (h *Handler) writeNoAvailable(c *gin.Context, laneName string, log *asyncLog, state *route.State) {
	if state != nil {
		state.NoteLaneUnavailable()
		state.ReleaseProbe()
		log.attempts = state.LogAttempts()
	}
	log.fail(string(route.KindSoftTransient), route.NoAvailableMessage(laneName))
	route.WriteNoAvailableChannel(c, laneName)
}

// resolveObject 取已有异步对象并固定到记录的渠道。
// 跨车道（或对象不存在）→ 404；渠道被删除/停用/无 key → 503，不静默换渠道。
func (h *Handler) resolveObject(c *gin.Context, log *asyncLog, laneName, kind, upstreamID string) (*selection, *model.PBRAsyncObject, bool) {
	obj, err := model.GetAsyncObject(laneName, kind, upstreamID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.fail(string(route.KindClientError), "async object not found: "+upstreamID)
			apierr.Write(c, http.StatusNotFound, CodeAsyncObjectNotFound,
				fmt.Sprintf("async object '%s' was not created by lane '%s'", upstreamID, laneName),
				"async objects are only visible in the lane that created them")
			return nil, nil, false
		}
		log.fail(string(route.KindSoftTransient), "read async object failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"read async object failed", "")
		return nil, nil, false
	}

	channel, err := model.CacheGetChannel(obj.ChannelId)
	if err != nil || channel == nil {
		log.channelId = obj.ChannelId
		h.writeObjectChannelUnavailable(c, log, upstreamID, "recorded channel no longer exists")
		return nil, nil, false
	}
	// 即使渠道随后被判定不可用，日志仍保留它的身份，便于定位。
	log.channelId = channel.Id
	log.channelName = channel.Name
	if channel.Status != common.ChannelStatusEnabled {
		h.writeObjectChannelUnavailable(c, log, upstreamID, "recorded channel is disabled")
		return nil, nil, false
	}
	if !isOpenAICompatibleChannelType(channel.Type) {
		log.fail(string(route.KindClientError), fmt.Sprintf(
			"channel type %d does not speak the OpenAI Files/Batches protocol", channel.Type))
		apierr.Write(c, http.StatusBadRequest, CodeUnsupportedProtocolForAsync,
			fmt.Sprintf("channel '%s' (type %d) does not speak the OpenAI Files/Batches protocol", channel.Name, channel.Type),
			"restore an OpenAI-compatible channel for this object")
		return nil, nil, false
	}
	if _, err := asyncBaseURL(channel); err != nil {
		h.writeObjectChannelUnavailable(c, log, upstreamID, "recorded channel has no usable base URL")
		return nil, nil, false
	}
	key, _, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil || !usableChannelKey(key) {
		h.writeObjectChannelUnavailable(c, log, upstreamID, "recorded channel has no available key")
		return nil, nil, false
	}

	log.setChannel(channel)
	return &selection{
		channel: channel,
		key:     key,
	}, obj, true
}

func (h *Handler) writeObjectChannelUnavailable(c *gin.Context, log *asyncLog, upstreamID, reason string) {
	log.fail(string(route.KindSoftTransient), "async object channel unavailable: "+reason)
	apierr.Write(c, http.StatusServiceUnavailable, CodeAsyncObjectChannelUnavailable,
		fmt.Sprintf("the channel that owns async object '%s' is unavailable", upstreamID),
		reason+"; re-enable the channel or delete the object")
}

// isOpenAICompatibleChannelType 判定渠道是否走 OpenAI 协议。
//
// 显式允许 OpenAI(1)、Custom(8)、AdvancedCustom(58)；其余按实际适配器判定：
// 适配器就是 openai.Adaptor 的渠道（如 OpenRouter/Xinference）同样说 OpenAI 协议。
// Claude/Gemini/签名类适配器一律拒绝（ADR 0010 第 5 条）。
func isOpenAICompatibleChannelType(channelType int) bool {
	switch channelType {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeCustom, constant.ChannelTypeAdvancedCustom:
		return true
	}
	apiType, _ := common.ChannelType2APIType(channelType)
	adaptor := relay.GetAdaptor(apiType)
	_, ok := adaptor.(*openai.Adaptor)
	return ok
}

// asyncBaseURL 求异步对象的版本根。渠道 base_url 允许写到 /v1 或更长的
// 自定义前缀；这里保证最终拼出 "<root>/v1/files..." 而不是 /v1/v1。
func asyncBaseURL(channel *model.Channel) (string, error) {
	if channel == nil {
		return "", errors.New("nil channel")
	}
	base := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	if base == "" {
		return "", errors.New("channel base URL is empty")
	}
	if strings.Contains(base, "{model}") {
		return "", errors.New("channel base URL contains {model} placeholder")
	}
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		return base, nil
	}
	return base + "/v1", nil
}

func buildUpstreamURL(channel *model.Channel, relPath, rawQuery string) (string, error) {
	base, err := asyncBaseURL(channel)
	if err != nil {
		return "", err
	}
	target := base + relPath
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	return target, nil
}

// usableChannelKey 拒绝空 key 与带换行的 key：后者会让 net/http 在设置
// Authorization 时直接报 invalid header field value，表现为难以定位的传输失败。
func usableChannelKey(key string) bool {
	return strings.TrimSpace(key) != "" && !strings.ContainsAny(key, "\r\n")
}

// keepUpstreamRedirectResponse 不跟随上游 3xx：异步对象语义要求客户端看到
// 上游的真实响应，而不是网关替它跳转（最多 0 跳）。
func keepUpstreamRedirectResponse(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

// doUpstream 发出**一次**上游请求。调用方不得在此之后重试或换成员。
func (h *Handler) doUpstream(c *gin.Context, sel *selection, method, relPath string, body io.Reader, contentLength int64) (*http.Response, *types.NewAPIError) {
	if sel == nil || sel.channel == nil {
		return nil, types.NewError(errors.New("async: no channel selected"), types.ErrorCodeGetChannelFailed)
	}
	target, err := buildUpstreamURL(sel.channel, relPath, c.Request.URL.RawQuery)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), method, target, body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	req.ContentLength = contentLength
	applyUpstreamHeaders(req, c, sel)
	if contentLength < 0 {
		// 流式上传没有可重放的长度：明确走 chunked，避免 ContentLength 与实际
		// 解压后字节数不一致时上游截断/挂起。
		req.ContentLength = -1
	}

	settings := sel.channel.GetSetting()
	client, err := service.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeGetChannelFailed)
	}
	if client == nil {
		return nil, types.NewError(errors.New("async: nil upstream http client"), types.ErrorCodeGetChannelFailed)
	}
	// 客户端按代理与传输策略缓存，禁止原地修改；浅拷贝只覆盖重定向策略。
	relayClient := *client
	relayClient.CheckRedirect = keepUpstreamRedirectResponse

	resp, err := relayClient.Do(req)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeDoRequestFailed)
	}
	return resp, nil
}

func applyUpstreamHeaders(req *http.Request, c *gin.Context, sel *selection) {
	inbound := c.Request.Header
	if contentType := inbound.Get("Content-Type"); contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if accept := inbound.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}
	if userAgent := inbound.Get("User-Agent"); userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	// OpenAI-Beta 是少数异步/新功能端点需要的客户端头，属于安全透传。
	if beta := inbound.Get("OpenAI-Beta"); beta != "" {
		req.Header.Set("OpenAI-Beta", beta)
	}
	if sel.channel.Type == constant.ChannelTypeOpenAI &&
		sel.channel.OpenAIOrganization != nil && *sel.channel.OpenAIOrganization != "" {
		req.Header.Set("OpenAI-Organization", *sel.channel.OpenAIOrganization)
	}
	req.Header.Set("Authorization", "Bearer "+sel.key)
	// X-PBR-Lane 刻意不写：它是网关内部路由键，不能泄漏给上游。
}
