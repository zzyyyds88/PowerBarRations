package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/logger"
	"github.com/zzyyyds88/PowerBarRations/model"
	perfmetrics "github.com/zzyyyds88/PowerBarRations/pkg/perf_metrics"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// 消费元数据日志（design-v1 §8/G7"只看不扣"）。
//
// 计费执行链（预扣费/结算/倍率折算）已按 §1.3 物理删除；这里只保留转发收尾
// 必需的两件事：把 token 用量回填 PBR 日志载体
// （model.RecordConsumeLog 内按 pbr_prices 折算成本）、写基座用量元数据日志。

func usageSemantic(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) string {
	if usage != nil && usage.UsageSemantic != "" {
		return usage.UsageSemantic
	}
	if relayInfo != nil && relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return "anthropic"
	}
	return "openai"
}

// cacheWriteTokensTotal 归一化缓存写入 token 总数（供日志 UI 展示）。
// 若存在 5m/1h 拆分计数则取其和，否则回落到 cache_creation_tokens。
func cacheWriteTokensTotal(cacheCreationTokens, cacheCreation5m, cacheCreation1h int) int {
	if split := cacheCreation5m + cacheCreation1h; cacheCreation5m > 0 || cacheCreation1h > 0 {
		if cacheCreationTokens > split {
			return cacheCreationTokens
		}
		return split
	}
	return cacheCreationTokens
}

// PostTextUsageLog 记录文本类转发（chat/responses/embedding/rerank/image/claude/
// gemini 等）成功收尾时的用量元数据日志。
func PostTextUsageLog(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []string) {
	originUsage := usage
	billingUsage := effectiveBillingUsage(usage)
	if usage == nil {
		extraContent = append(extraContent, "上游没有返回用量信息")
	}

	modelName := relayInfo.GetBillingModelName()
	tokenName := ctx.GetString("token_name")
	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	isClaude := usageSemantic(relayInfo, billingUsage) == "anthropic"

	var promptTokens, completionTokens, cacheTokens, cacheCreationTokens, cacheCreation5m, cacheCreation1h, imageTokens, reasoningTokens int
	if billingUsage != nil {
		promptTokens = billingUsage.PromptTokens
		completionTokens = billingUsage.CompletionTokens
		cacheTokens = billingUsage.PromptTokensDetails.CachedTokens
		cacheCreationTokens = billingUsage.PromptTokensDetails.CacheCreationTokensTotal()
		cacheCreation5m = billingUsage.ClaudeCacheCreation5mTokens
		cacheCreation1h = billingUsage.ClaudeCacheCreation1hTokens
		imageTokens = billingUsage.PromptTokensDetails.ImageTokens
		reasoningTokens = billingUsage.CompletionTokenDetails.ReasoningTokens
	}

	logModel := modelName
	if strings.HasPrefix(logModel, "gpt-4-gizmo") {
		logModel = "gpt-4-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", modelName))
	}
	if strings.HasPrefix(logModel, "gpt-4o-gizmo") {
		logModel = "gpt-4o-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", modelName))
	}

	var other *model.LogOther
	if isClaude {
		other = GenerateClaudeOtherInfo(ctx, relayInfo, cacheTokens, cacheCreationTokens, cacheCreation5m, cacheCreation1h)
		other.SetPublic("usage_semantic", "anthropic")
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, cacheTokens)
		if cacheCreationTokens > 0 {
			other.SetPublic("cache_creation_tokens", cacheCreationTokens)
		}
		if cacheCreation5m > 0 {
			other.SetPublic("cache_creation_tokens_5m", cacheCreation5m)
		}
		if cacheCreation1h > 0 {
			other.SetPublic("cache_creation_tokens_1h", cacheCreation1h)
		}
	}
	appendUsageBillingPathForLog(other, common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens), originUsage)
	if adminRejectReason := common.GetContextKeyString(ctx, constant.ContextKeyAdminRejectReason); adminRejectReason != "" {
		other.SetAdmin("reject_reason", adminRejectReason)
	}
	if imageTokens != 0 {
		other.SetPublic("image", true)
		other.SetPublic("image_output", imageTokens)
	}
	if cacheWrite := cacheWriteTokensTotal(cacheCreationTokens, cacheCreation5m, cacheCreation1h); cacheWrite > 0 {
		other.SetPublic("cache_write_tokens", cacheWrite)
	}
	if relayInfo.GetFinalRequestRelayFormat() != types.RelayFormatClaude && billingUsage != nil && billingUsage.UsageSource != "" && billingUsage.InputTokens > 0 {
		// input_tokens_total: explicit normalized total input used by the usage log UI.
		other.SetPublic("input_tokens_total", billingUsage.InputTokens)
	}

	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		CacheReadTokens:  cacheTokens,
		CacheWriteTokens: cacheWriteTokensTotal(cacheCreationTokens, cacheCreation5m, cacheCreation1h),
		ReasoningTokens:  reasoningTokens,
		ModelName:        logModel,
		TokenName:        tokenName,
		Content:          strings.Join(extraContent, ", "),
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(useTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(completionTokens))
	})
}

// PostAudioUsageLog 记录语音类转发（TTS/STT、含音频分解的 responses）的用量元数据日志。
func PostAudioUsageLog(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent string) {
	if usage == nil {
		usage = &dto.Usage{PromptTokens: relayInfo.GetEstimatePromptTokens(), TotalTokens: relayInfo.GetEstimatePromptTokens()}
	}

	tokenName := ctx.GetString("token_name")
	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	content := extraContent
	if usage.TotalTokens == 0 {
		if content != "" {
			content += ", "
		}
		content += "上游没有返回用量信息（可能是上游超时）"
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, userId %d, channelId %d, tokenId %d, model %s",
			relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, relayInfo.GetBillingModelName()))
	}

	other := GenerateAudioOtherInfo(ctx, relayInfo, usage)
	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		CacheReadTokens:  usage.PromptTokensDetails.CachedTokens,
		CacheWriteTokens: usage.PromptTokensDetails.CacheCreationTokensTotal(),
		ModelName:        relayInfo.GetBillingModelName(),
		TokenName:        tokenName,
		Content:          content,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(useTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(usage.CompletionTokens))
	})
}

// PostWssUsageLog 记录实时（WebSocket/Realtime）转发的用量元数据日志。
func PostWssUsageLog(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelName string, usage *dto.RealtimeUsage, extraContent string) {
	tokenName := ctx.GetString("token_name")
	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	content := extraContent
	if usage.TotalTokens == 0 {
		if content != "" {
			content += ", "
		}
		content += "上游没有返回用量信息（可能是上游超时）"
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, userId %d, channelId %d, tokenId %d, model %s",
			relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, modelName))
	}

	other := GenerateWssOtherInfo(ctx, relayInfo, usage)
	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		CacheReadTokens:  usage.InputTokenDetails.CachedTokens,
		CacheWriteTokens: usage.InputTokenDetails.CachedCreationTokens + usage.InputTokenDetails.CacheWriteTokens,
		ModelName:        modelName,
		TokenName:        tokenName,
		Content:          content,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(useTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
}
