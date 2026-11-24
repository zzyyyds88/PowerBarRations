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

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
	}
}

func ShouldDisableChannel(err *types.NewAPIError) bool {
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	if err == nil {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	// 分类白名单（ADR 0013 第 6 条）：只有凭据失效/额度耗尽这类**持续性**故障
	// 允许自动禁用。429（减速）、5xx/超时（瞬时）、成员级拒绝、客户端取消与
	// 坏请求一律跳过——它们的正确处置是冷却/换人与退避，不是关渠道。
	kind := pbrroute.Classify(err)
	if kind != pbrroute.KindHardAuth && kind != pbrroute.KindHardQuota {
		return false
	}
	// 状态码范围同样限制：即便关键词命中，429 与 5xx 也不得成为禁用依据。
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
