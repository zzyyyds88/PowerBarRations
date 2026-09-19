package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"

	"github.com/gin-gonic/gin"
)

// W2 观测与管理面：车道运行态快照、逐成员探活、熔断/冷却清除、单渠道探活。
// 契约见 docs/api-spec-v1.md §5.2/§5.3、docs/routing-spec-v1.md §7。

// resolveRuntimeRoute 把 {name} 解析为运行态路由：先按车道名（含被禁用车道的快照），
// 再按成员别名点名（解析到所属车道）。返回的 key 必须与转发链路的运行态 key 一致
// （route.laneKeyOf）。
//
// 关键：车道是唯一路由入口，运行态按车道键聚合；健康/探活/熔断清除因此都以车道
// （或别名解析出的车道）为入口，不存在"按请求模型名直接聚合"的隐式通道。
// 解析不到任何成员时返回 (nil, "", nil) 由调用方给 404。
func resolveRuntimeRoute(name string) (*model.ResolvedRoute, string, error) {
	if lane, err := model.GetLaneByName(name); err == nil && lane != nil {
		resolved, resolveErr := model.ResolveRoute(lane.Name)
		return resolved, lane.Name, resolveErr
	}
	resolved, err := model.ResolveRoute(name)
	if err != nil || resolved == nil || len(resolved.Members) == 0 {
		return nil, "", err
	}
	key := resolved.RouteKey
	if key == "" {
		key = resolved.Model
	}
	return resolved, key, nil
}

// GetLaneHealth GET /api/v1/lanes/{name}/health
//
// 快照含每成员 circuit / cooldown_until / consecutive_failures / rolling_success_rate /
// last_error_kind，以及 current_member / probe_member / affinity（对象）与 events
// （带时间戳，用于验收"熔断打开 → 半开 → 复通"的时间线证据）。
// 时间字段为 RFC3339 UTC 字符串，无冷却/无亲和为 null（api-spec §6.5 / §2.6）。
func GetLaneHealth(c *gin.Context) {
	resolved, key, err := resolveRuntimeRoute(c.Param("name"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if resolved == nil {
		apierr.NotFound(c, apierr.CodeLaneNotFound, "lane '"+c.Param("name")+"' not found", "GET /api/v1/lanes")
		return
	}
	snapshot := route.Default.For(key).Health(resolved, route.CurrentCircuitSettings())
	c.JSON(http.StatusOK, snapshot)
}

// ResetLaneCircuits POST /api/v1/lanes/{name}/circuits/reset
func ResetLaneCircuits(c *gin.Context) {
	resolved, key, err := resolveRuntimeRoute(c.Param("name"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if resolved == nil {
		apierr.NotFound(c, apierr.CodeLaneNotFound, "lane '"+c.Param("name")+"' not found", "GET /api/v1/lanes")
		return
	}
	if dryRun(c) {
		dryRunResult(c, "lanes", "update", key+"#circuits")
		return
	}
	cleared := route.Default.For(key).Reset()
	writeAudit(c, "reset-circuits", "lane", key)
	c.JSON(http.StatusOK, gin.H{"reset": cleared, "lane": key})
}

// ProbeMember 单成员探活结果。
//
// 对外字段遵循 api-spec §6.4：status（success|failed）、duration_ms、
// error_kind、msg。ok/status_code/latency_ms 是早期实现的字段，保留以避免
// 破坏既有调用方（控制台曾按它们取值）。
type ProbeMember struct {
	Channel       string `json:"channel"`
	UpstreamModel string `json:"upstream_model"`
	Status        string `json:"status"`
	DurationMs    int64  `json:"duration_ms"`
	ErrorKind     string `json:"error_kind,omitempty"`
	Msg           string `json:"msg,omitempty"`
	OK            bool   `json:"ok"`
	StatusCode    int    `json:"status_code"`
	LatencyMs     int64  `json:"latency_ms"`
	Error         string `json:"error,omitempty"`
}

// finish 统一补齐 status/duration_ms/error_kind/msg 与兼容字段。
//
// 注意：probeChannel 实测耗时写在 LatencyMs 上，这里要把 duration_ms 对齐到
// 它（而不是反向覆盖），否则对外 duration_ms 恒为 0。
func (p *ProbeMember) finish() {
	if p.DurationMs == 0 {
		p.DurationMs = p.LatencyMs
	}
	p.LatencyMs = p.DurationMs
	if p.OK {
		p.Status = "success"
		return
	}
	p.Status = "failed"
	if p.Msg == "" {
		p.Msg = p.Error
	}
	if p.ErrorKind == "" {
		p.ErrorKind = classifyProbeError(p.StatusCode)
	}
}

// classifyProbeError 把探活失败归类，便于 AI 区分"上游不可达/超时/鉴权失败"。
func classifyProbeError(statusCode int) string {
	switch {
	case statusCode == 0:
		return "network_error" // 连接失败/超时/DNS 等，未拿到 HTTP 状态
	case statusCode == http.StatusUnauthorized, statusCode == http.StatusForbidden:
		return "hard_auth"
	case statusCode == http.StatusTooManyRequests:
		return "soft_rate_limit"
	case statusCode >= 500:
		return "soft_transient"
	case statusCode >= 400:
		return "client_error"
	default:
		return "unknown"
	}
}

// ProbeLane POST /api/v1/lanes/{name}/probe：逐成员探活（routing-spec §7.7）。
//
// 并发上限可配（design-v1 §16.6，默认 4）：此前是串行 for 循环，探活耗时
// 随成员数线性增长，与"避免对上游造成突发压力"的设计意图相反；现在并发
// 执行但受上限约束。结果按成员原顺序回填，便于调用方按位置比对。
func ProbeLane(c *gin.Context) {
	resolved, key, err := resolveRuntimeRoute(c.Param("name"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if resolved == nil {
		apierr.NotFound(c, apierr.CodeLaneNotFound, "lane '"+c.Param("name")+"' not found", "GET /api/v1/lanes")
		return
	}

	results := make([]ProbeMember, len(resolved.Members))
	limit := CurrentProbeConcurrency()
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range resolved.Members {
		member := resolved.Members[i]
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, member model.RouteMember) {
			defer wg.Done()
			defer func() { <-sem }()
			channel, err := model.GetChannelById(member.ChannelId, true)
			if err != nil || channel == nil {
				result := ProbeMember{Channel: member.Channel, UpstreamModel: member.UpstreamModel,
					Error: "channel not found"}
				result.finish()
				results[idx] = result
				return
			}
			result := probeChannel(c.Request.Context(), channel, member.UpstreamModel)
			result.finish()
			results[idx] = result
		}(i, member)
	}
	wg.Wait()
	c.JSON(http.StatusOK, gin.H{"lane": key, "probed": len(results), "results": results})
}

// CurrentProbeConcurrency 探活并发上限（design-v1 §16.6，默认 4）。
func CurrentProbeConcurrency() int {
	common.OptionMapRWMutex.RLock()
	raw, ok := common.OptionMap[route.OptionProbeConcurrency]
	common.OptionMapRWMutex.RUnlock()
	if !ok {
		return route.DefaultProbeConcurrency
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed <= 0 {
		return route.DefaultProbeConcurrency
	}
	return parsed
}

// TestChannel POST /api/v1/channels/{name}/test：单渠道探活。
func TestChannel(c *gin.Context) {
	channel, err := findChannelByName(c.Param("name"))
	if err != nil {
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+c.Param("name")+"' not found", "GET /api/v1/channels")
		return
	}
	testModel := strings.TrimSpace(c.Query("model"))
	if testModel == "" {
		models := channel.GetModels()
		if len(models) > 0 {
			testModel = strings.TrimSpace(models[0])
		}
	}
	result := probeChannel(c.Request.Context(), channel, testModel)
	result.finish()
	c.JSON(http.StatusOK, result)
}

// probeChannel 用 OpenAI Chat 形态发一个最小真实请求探活。
//
// 说明：探活走的是带内请求（不是只连 TCP），因此能区分"通但模型不存在"与"不通"。
// 该形态只对 OpenAI 兼容类上游有意义；原生协议渠道（Anthropic/Gemini/Vertex 等）
// 会以 unsupported 明确返回，不做假阳性判定。
func probeChannel(ctx context.Context, channel *model.Channel, upstreamModel string) ProbeMember {
	result := ProbeMember{Channel: channel.Name, UpstreamModel: upstreamModel}
	if isProbeUnsupported(channel.Type) {
		result.Error = "unsupported: channel type does not accept the OpenAI chat probe shape"
		return result
	}
	key, _, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil {
		result.Error = "no enabled key: " + keyErr.Error()
		return result
	}
	baseURL := channel.GetBaseURL()
	if strings.TrimSpace(baseURL) == "" {
		result.Error = "channel base_url is empty"
		return result
	}
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = "gpt-4o-mini"
		result.UpstreamModel = upstreamModel
	}
	payload := `{"model":` + strconv.Quote(upstreamModel) +
		`,"messages":[{"role":"user","content":"ping"}],"max_tokens":1,"stream":false}`
	requestURL := relaycommon.GetFullRequestURL(baseURL, "/v1/chat/completions", channel.Type)

	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, requestURL, strings.NewReader(payload))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	if channel.Type == constant.ChannelTypeAzure {
		req.Header.Set("api-key", key)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	result.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	result.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
	if !result.OK {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		result.Error = strings.TrimSpace(string(buf[:n]))
	}
	return result
}

// 与 new-api 自身标注"不支持测试"的类型保持一致，另加原生协议渠道。
func isProbeUnsupported(channelType int) bool {
	switch channelType {
	case constant.ChannelTypeSunoAPI,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeAnthropic, constant.ChannelTypeGemini, constant.ChannelTypeVertexAi,
		constant.ChannelTypePaLM, constant.ChannelTypeAws, constant.ChannelTypeCohere:
		return true
	}
	return false
}
