package service

// 自动禁用白名单（ADR 0013 第 6 条）：只有 hard_auth / hard_quota 且状态码
// 不是 429/5xx 时才允许进入禁用判定；429、5xx、成员级拒绝与普通客户端错误
// 一律不得把渠道关掉。

import (
	"errors"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
	"github.com/zzyyyds88/PowerBarRations/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newServiceAPIError(status int, message string) *types.NewAPIError {
	return types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, status)
}

func TestShouldDisableChannelWhitelist(t *testing.T) {
	origEnabled := common.AutomaticDisableChannelEnabled
	origRanges := operation_setting.AutomaticDisableStatusCodesToString()
	origKeywords := operation_setting.AutomaticDisableKeywordsToString()
	common.AutomaticDisableChannelEnabled = true
	require.NoError(t, operation_setting.AutomaticDisableStatusCodesFromString("401,402,500-599"))
	operation_setting.AutomaticDisableKeywordsFromString("your account is not authorized")
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = origEnabled
		_ = operation_setting.AutomaticDisableStatusCodesFromString(origRanges)
		operation_setting.AutomaticDisableKeywordsFromString(origKeywords)
	})

	cases := []struct {
		name string
		err  *types.NewAPIError
		want bool
	}{
		{"401 鉴权失败允许禁用", newServiceAPIError(http.StatusUnauthorized, "invalid key"), true},
		{"403 + 完整厂商报文允许禁用", newServiceAPIError(http.StatusForbidden, "Your account is not authorized"), true},
		{"402 欠费允许禁用", newServiceAPIError(http.StatusPaymentRequired, "payment required"), true},
		{"429 只减速，禁止禁用", newServiceAPIError(http.StatusTooManyRequests, "rate limited"), false},
		{"5xx 瞬时故障即使命中范围也禁止禁用", newServiceAPIError(http.StatusInternalServerError, "boom"), false},
		{"400 成员级拒绝禁止禁用", newServiceAPIError(http.StatusBadRequest, "bad request"), false},
		{"404 瞬时故障禁止禁用", newServiceAPIError(http.StatusNotFound, "not found"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ShouldDisableChannel(tc.err))
		})
	}

	assert.False(t, ShouldDisableChannel(nil), "nil 错误不得触发禁用")
}

// 关键词命中也不能突破分类白名单：5xx 上带欠费字样仍按瞬时故障处理。
func TestShouldDisableChannelKeywordCannotBypassWhitelist(t *testing.T) {
	origEnabled := common.AutomaticDisableChannelEnabled
	origKeywords := operation_setting.AutomaticDisableKeywordsToString()
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableKeywordsFromString("quota exhausted")
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = origEnabled
		operation_setting.AutomaticDisableKeywordsFromString(origKeywords)
	})

	err := newServiceAPIError(http.StatusInternalServerError, "quota exhausted on upstream")
	assert.False(t, ShouldDisableChannel(err), "5xx 不得因关键词被禁用")
}
