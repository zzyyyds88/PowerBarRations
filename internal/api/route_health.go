package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	"github.com/zzyyyds88/PowerBarRations/relay/helper"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

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
	snapshot := route.Default.Health(key, resolved, route.CurrentCircuitSettings())
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
	var cleared route.ResetResult
	if runtime := route.Default.Get(key); runtime != nil {
		cleared = runtime.Reset()
	}
	writeAudit(c, "reset-circuits", "lane", key, cleared)
	c.JSON(http.StatusOK, gin.H{
		"lane": key,
		// reset 是兼容字段 = circuits；cleared 是结构化明细（api-spec §6.6）。
		"reset":                    cleared.Circuits,
		"generation":               cleared.Generation,
		"previous_generation":      cleared.PreviousGeneration,
		"invalidated_probe_leases": cleared.InvalidatedProbeLeases,
		"cleared":                  cleared,
	})
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
	// PenaltyCleared 仅渠道级探活（POST /channels/{name}/test）回填：
	// 本次成功探活清除了哪些车道的成员惩罚态（routing-spec §5.4）。
	PenaltyCleared *PenaltyCleared `json:"penalty_cleared,omitempty"`
}

// PenaltyCleared 探活成功后的惩罚态清理摘要（api-spec §5.3 / §6.4）。
type PenaltyCleared struct {
	Lanes          []string `json:"lanes"`
	Members        int      `json:"members"`
	ProbeBusyLanes []string `json:"probe_busy_lanes,omitempty"`
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
		p.ErrorKind = ""
		p.Msg = ""
		return
	}
	p.Status = "failed"
	if p.Msg == "" {
		p.Msg = p.Error
	}
	if p.ErrorKind == "" {
		p.ErrorKind = classifyProbeError(p.StatusCode, p.Msg)
	}
}

// classifyProbeError 把探活失败归入路由错误分类（共享 route.Classify，
// ADR 0013 第 5 条）：探活与转发必须用同一套分类，否则"探活说鉴权失败、
// 转发却冷却 60s"这类口径漂移无法排查。
func classifyProbeError(statusCode int, message string) string {
	if statusCode == 0 {
		return "network_error"
	}
	err := types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, statusCode)
	if kind := route.Classify(err); kind != "" {
		return string(kind)
	}
	return "unknown"
}

// ProbeLane POST /api/v1/lanes/{name}/probe：逐成员探活（routing-spec §7.7）。
//
// 并发上限可配（design-v1 §16.6，默认 4）：此前是串行 for 循环，探活耗时
// 随成员数线性增长，与"避免对上游造成突发压力"的设计意图相反；现在并发
// 执行但受上限约束。结果按成员原顺序回填，便于调用方按位置比对。
func ProbeLane(c *gin.Context) {
	if rejectDryRunProbe(c) {
		return
	}
	resolved, key, err := resolveRuntimeRoute(c.Param("name"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if resolved == nil {
		apierr.NotFound(c, apierr.CodeLaneNotFound, "lane '"+c.Param("name")+"' not found", "GET /api/v1/lanes")
		return
	}

	generation := route.Default.Generation(key)
	generations := route.Default.GenerationSnapshot()
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

	// 探活成功 = 一次真实的上游成功往返：清除该成员在所有车道的惩罚态
	// （routing-spec §5.4）。失败不动运行态。
	lanes := map[string]struct{}{}
	busy := map[string]struct{}{}
	clearedMembers := 0
	for i := range results {
		if !results[i].OK {
			continue
		}
		member := resolved.Members[i]
		cleared := route.Default.ClearMemberPenaltyAtGenerations(route.MemberKeyOf(&member), generations)
		clearedMembers += cleared.Members
		for _, lane := range cleared.Lanes {
			lanes[lane] = struct{}{}
		}
		for _, lane := range cleared.ProbeBusyLanes {
			busy[lane] = struct{}{}
		}
	}
	penalty := PenaltyCleared{Lanes: sortedStringSetKeys(lanes), Members: clearedMembers, ProbeBusyLanes: sortedStringSetKeys(busy)}
	c.JSON(http.StatusOK, gin.H{
		"lane":            key,
		"generation":      generation,
		"probed":          len(results),
		"penalty_cleared": penalty,
		"results":         results,
	})
}

// rejectDryRunProbe 在 handler 内再做一次 dry_run 拒绝：路由层中间件已经拒绝，
// 这里保证即使 handler 被复用/直调也不会真的向上游发请求（ADR 0013 第 5 条）。
func rejectDryRunProbe(c *gin.Context) bool {
	if !dryRun(c) {
		return false
	}
	apierr.Write(c, http.StatusBadRequest, apierr.CodeDryRunNotSupported, dryRunRejectMessage,
		"probe performs a real upstream request; remove ?dry_run=true")
	return true
}

func sortedStringSetKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
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
	if parsed > route.MaxProbeConcurrency {
		return route.MaxProbeConcurrency
	}
	return parsed
}

// TestChannel POST /api/v1/channels/{name}/test：单渠道探活。
//
// 每次探活写一条探活记录（`pbr_probe_logs`），供渠道健康详情的"探活历史"
// （api-spec §6.12）。留痕失败不影响探活结论本身。
func TestChannel(c *gin.Context) {
	if rejectDryRunProbe(c) {
		return
	}
	channel, err := findChannelByName(c.Param("name"))
	if err != nil {
		if writeChannelLookupError(c, err) {
			return
		}
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+c.Param("name")+"' not found", "GET /api/v1/channels")
		return
	}
	selectedModel := strings.TrimSpace(c.Query("model"))
	if selectedModel == "" {
		models := channel.GetModels()
		if len(models) > 0 {
			selectedModel = strings.TrimSpace(models[0])
		}
	}
	result := ProbeMember{Channel: channel.Name, UpstreamModel: selectedModel}
	if selectedModel == "" {
		// 不再回落 gpt-4o-mini：用未声明的模型探活会把"配置缺失"伪装成
		// "上游失败/成功"，结论不可用于排障（ADR 0013 第 5 条）。
		result.Error = "no model declared or selected for probe"
		result.ErrorKind = "invalid_request"
		result.finish()
		writeProbeRecord(channel, result)
		c.JSON(http.StatusOK, result)
		return
	}
	upstreamModel := selectedModel
	if mapped := channel.ModelMappingMap()[selectedModel]; strings.TrimSpace(mapped) != "" {
		upstreamModel = strings.TrimSpace(mapped)
	}
	generations := route.Default.GenerationSnapshot()
	result = probeChannel(c.Request.Context(), channel, upstreamModel)
	result.finish()
	if result.OK {
		cleared := route.Default.ClearChannelModelPenaltyAtGenerations(channel.Id, upstreamModel, generations)
		result.PenaltyCleared = &PenaltyCleared{
			Lanes:          cleared.Lanes,
			Members:        cleared.Members,
			ProbeBusyLanes: cleared.ProbeBusyLanes,
		}
	}
	writeProbeRecord(channel, result)
	c.JSON(http.StatusOK, result)
}

// writeProbeRecord 把一次探活落成探活记录（api-spec §6.12 的"探活历史"）。
func writeProbeRecord(channel *model.Channel, result ProbeMember) {
	msg := result.Msg
	if len(msg) > model.ProbeLogMsgLimit {
		msg = msg[:model.ProbeLogMsgLimit]
	}
	entry := &model.PBRProbeLog{
		Ts:            time.Now().Unix(),
		ChannelId:     channel.Id,
		ChannelName:   channel.Name,
		UpstreamModel: result.UpstreamModel,
		Status:        result.Status,
		DurationMs:    result.DurationMs,
		HTTPStatus:    result.StatusCode,
		ErrorKind:     result.ErrorKind,
		Msg:           msg,
	}
	model.LogProbeWriteFailure(model.WriteProbeLog(entry))
}

// probeChannel 用 OpenAI Chat 形态发一个最小真实请求探活。
//
// 说明：探活走的是带内请求（不是只连 TCP），因此能区分"通但模型不存在"与"不通"。
// 该形态只对 OpenAI 兼容类上游有意义；原生协议渠道（Anthropic/Gemini/Vertex 等）
// 会以 unsupported 明确返回，不做假阳性判定。
//
// max_tokens 不能取 1：推理模型（带 reasoning_content 的 DeepSeek/o 系等）在
// 1 个 token 内连推理都放不下，上游会直接回 502 之类错误，把健康的渠道探成
// soft_transient 误报。128 足够容纳"推理 + 一两个输出 token"，同时保持探活
// 成本可忽略。
func probeChannel(ctx context.Context, channel *model.Channel, upstreamModel string) ProbeMember {
	result := ProbeMember{Channel: channel.Name, UpstreamModel: upstreamModel}
	if isProbeUnsupported(channel.Type) {
		result.Error = "unsupported: channel type does not accept the OpenAI chat probe shape"
		result.ErrorKind = "unsupported"
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
	upstreamModel = strings.TrimSpace(upstreamModel)
	if upstreamModel == "" {
		result.Error = "no model declared or selected for probe"
		result.ErrorKind = "invalid_request"
		return result
	}
	payload := `{"model":` + strconv.Quote(upstreamModel) +
		`,"messages":[{"role":"user","content":"ping"}],"max_tokens":128,"stream":false}`
	requestURL := probeRequestURL(channel, baseURL, upstreamModel)

	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, requestURL, strings.NewReader(payload))
	if err != nil {
		result.Error = err.Error()
		result.ErrorKind = "invalid_request"
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
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			result.ErrorKind = string(route.KindTimeout)
		case errors.Is(err, context.Canceled):
			result.ErrorKind = string(route.KindCanceled)
		default:
			result.ErrorKind = "network_error"
		}
		return result
	}
	defer resp.Body.Close()
	// 上游错误体可能很大；探活只需要足够判断错误类型与报文摘要。
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	result.StatusCode = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = strings.TrimSpace(string(body))
		if result.Error == "" {
			result.Error = fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode)
		}
		result.ErrorKind = classifyProbeError(resp.StatusCode, result.Error)
		return result
	}

	// 200 不一定是成功：上游可能用 200 承载错误对象（ADR 0013 第 5 条）。
	if msg, isErr := helper.IsUpstreamErrorFrame(body); isErr {
		result.Error = msg
		result.ErrorKind = string(route.KindUpstreamReject)
		return result
	}
	var completion struct {
		Choices []json.RawMessage `json:"choices"`
	}
	if err := common.Unmarshal(body, &completion); err != nil {
		result.Error = "invalid probe response: " + err.Error()
		result.ErrorKind = string(route.KindBadResponse)
		return result
	}
	if len(completion.Choices) == 0 {
		result.Error = "upstream returned no completion choices"
		result.ErrorKind = string(route.KindBadResponse)
		return result
	}
	result.OK = true
	return result
}

// probeRequestURL 与转发适配器保持同一 URL 口径：Azure 需要 deployment 路径与
// api-version，其他 OpenAI 兼容渠道走 /v1/chat/completions（自定义渠道保留
// base_url 自带的版本段）。
func probeRequestURL(channel *model.Channel, baseURL, upstreamModel string) string {
	if channel.Type == constant.ChannelTypeAzure {
		deployment := upstreamModel
		// 与 openai adaptor 一致：2025-05-10 之前创建的渠道按旧约定去掉点名中的点。
		if channel.CreatedTime > 0 && channel.CreatedTime < constant.AzureNoRemoveDotTime {
			deployment = strings.ReplaceAll(deployment, ".", "")
		}
		apiVersion := strings.TrimSpace(channel.Other)
		if apiVersion == "" {
			apiVersion = constant.AzureDefaultAPIVersion
		}
		requestPath := fmt.Sprintf("/openai/deployments/%s/chat/completions?api-version=%s",
			url.PathEscape(deployment), url.QueryEscape(apiVersion))
		return relaycommon.GetFullRequestURL(baseURL, requestPath, channel.Type)
	}
	if channel.Type == constant.ChannelTypeCustom {
		if strings.Contains(baseURL, "{model}") {
			return strings.ReplaceAll(baseURL, "{model}", upstreamModel)
		}
	}
	return relaycommon.GetFullRequestURL(baseURL, "/v1/chat/completions", channel.Type)
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
