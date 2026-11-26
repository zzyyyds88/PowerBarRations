package service

import (
	"fmt"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
	pbrroute "github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
	"github.com/zzyyyds88/PowerBarRations/setting/operation_setting"
)

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelError.ChannelName, channelError.ChannelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason)
		NotifyRootUser(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
	}
}

// DisableChannelAtKeyIndex 禁用指定 key 索引，后台闭包无需携带明文 key。
// notify=false 用于有界队列饱和时的同步收口：状态必须落库，但不能在请求
// 线程同步发送邮件/webhook。
func DisableChannelAtKeyIndex(channelID int, keyIndex int, channelName string, reason string, notify bool) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelName, channelID, common.LocalLogPreview(reason)))
	var success bool
	if keyIndex >= 0 {
		success = model.UpdateChannelStatusAtKeyIndex(channelID, keyIndex, common.ChannelStatusAutoDisabled, reason)
	} else {
		success = model.UpdateChannelStatus(channelID, "", common.ChannelStatusAutoDisabled, reason)
	}
	if success && notify {
		subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelName, channelID)
		content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelName, channelID, reason)
		NotifyRootUser(formatNotifyType(channelID, common.ChannelStatusAutoDisabled), subject, content)
	}
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
	}
}

// isCredentialChannelError 判定渠道自身的凭据类错误：多 key 渠道没有可用 key
// （channel:no_available_key）或适配器判定渠道 key 无效（channel:invalid_key）。
// 这两类由基座以默认 500 构造，语义是渠道配置的持续性故障，Classify 已归
// hard_auth；它们不是上游 5xx，不能因为状态码范围而被当成瞬时故障放行。
func isCredentialChannelError(err *types.NewAPIError) bool {
	switch err.GetErrorCode() {
	case types.ErrorCodeChannelInvalidKey, types.ErrorCodeChannelNoAvailableKey:
		return true
	default:
		return false
	}
}

func ShouldDisableChannel(err *types.NewAPIError) bool {
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	if err == nil {
		return false
	}
	// 分类白名单必须最先判定（design-v1 §7.11、routing-spec §8）：只有凭据失效/
	// 额度耗尽这类**持续性**故障允许自动禁用。`channel:` 前缀本身不是禁用依据——
	// 流式首事件超时（channel:response_time_exceeded）归 timeout，429 归
	// soft_rate_limit，5xx 归 soft_transient，成员级拒绝、客户端取消与坏请求
	// 同样不在白名单；它们的正确处置是冷却/换人与退避，不是关渠道。
	kind := pbrroute.Classify(err)
	if kind != pbrroute.KindHardAuth && kind != pbrroute.KindHardQuota {
		return false
	}
	// 我方构造的 skipRetry 错误（请求校验失败等）不得关掉健康渠道；Classify
	// 通常在状态码之前就把它归为 client_error，这里再兜一层，避免欠费关键词
	// 命中时把本地校验错误升级成 hard_quota。
	if types.IsSkipRetryError(err) {
		return false
	}
	// 凭据类通道错误与 hard_auth 分类保持一致：它们可能携带基座默认的 500，
	// 但属于渠道自身配置错误，允许自动禁用。
	if isCredentialChannelError(err) {
		return true
	}
	// 状态码范围同样限制：即便关键词或状态码区间命中，429（减速）与 5xx
	// （瞬时）也不得成为禁用依据。
	if err.StatusCode == 429 || err.StatusCode >= 500 {
		return false
	}
	if operation_setting.ShouldDisableByStatusCode(err.StatusCode) {
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	search, _ := AcSearch(lowerMessage, operation_setting.AutomaticDisableKeywordsSnapshot(), true)
	return search
}

func ShouldEnableChannel(newAPIError *types.NewAPIError, status int) bool {
	if !common.AutomaticEnableChannelEnabled {
		return false
	}
	if newAPIError != nil {
		return false
	}
	if status != common.ChannelStatusAutoDisabled {
		return false
	}
	return true
}
