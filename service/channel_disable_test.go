package service

// 自动禁用白名单（ADR 0013 第 6 条、design-v1 §7.11、routing-spec §8）：先做
// 分类白名单，只有 hard_auth / hard_quota 且状态码不是 429/5xx 时才允许进入
// 禁用判定；`channel:` 前缀不能绕过白名单，429、5xx、timeout、成员级拒绝与
// 普通客户端错误一律不得把渠道关掉。

import (
	"context"
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

// `channel:` 前缀不再是无条件禁用开关：先分类、再看凭据类错误码，最后才允许
// 状态码/关键词命中。表内同时覆盖 design-v1 §7.11 与 routing-spec §8 点名的
// timeout、5xx、429 与 upstream_reject。
func TestShouldDisableChannelClassifiesBeforeChannelPrefix(t *testing.T) {
	origEnabled := common.AutomaticDisableChannelEnabled
	origRanges := operation_setting.AutomaticDisableStatusCodesToString()
	origKeywords := operation_setting.AutomaticDisableKeywordsToString()
	common.AutomaticDisableChannelEnabled = true
	// 故意把 429/5xx 也放进状态码范围：分类白名单必须在此之上先行拦截。
	require.NoError(t, operation_setting.AutomaticDisableStatusCodesFromString("401,402,429,500-599"))
	operation_setting.AutomaticDisableKeywordsFromString("quota exhausted")
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
		{
			name: "channel:invalid_key/401 是凭据故障",
			err:  types.NewOpenAIError(errors.New("invalid key"), types.ErrorCodeChannelInvalidKey, http.StatusUnauthorized),
			want: true,
		},
		{
			// 与 model.GetNextEnabledKey 的生产构造一致：NewError 默认 500。
			name: "channel:no_available_key 是凭据故障",
			err:  types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey),
			want: true,
		},
		{
			name: "channel:response_time_exceeded/504 是超时",
			err:  types.NewOpenAIError(errors.New("stream first event timeout"), types.ErrorCodeChannelResponseTimeExceeded, http.StatusGatewayTimeout),
			want: false,
		},
		{
			// 对齐 relay/helper.streamTimeoutError 的生产构造（保留 DeadlineExceeded 错误链）。
			name: "流式首事件超时带 deadline 错误链也不得禁用",
			err: types.NewError(context.DeadlineExceeded, types.ErrorCodeChannelResponseTimeExceeded,
				types.ErrOptionWithStatusCode(http.StatusGatewayTimeout)),
			want: false,
		},
		{
			name: "channel: 前缀 5xx 仍是瞬时故障",
			err:  types.NewOpenAIError(errors.New("upstream boom"), types.ErrorCodeChannelAwsClientError, http.StatusInternalServerError),
			want: false,
		},
		{
			name: "channel: 前缀 429 仍是减速",
			err:  types.NewOpenAIError(errors.New("slow down"), types.ErrorCodeChannelParamOverrideInvalid, http.StatusTooManyRequests),
			want: false,
		},
		{
			name: "普通 429 只减速",
			err:  newServiceAPIError(http.StatusTooManyRequests, "rate limited"),
			want: false,
		},
		{
			name: "upstream_reject 不关渠道",
			err:  types.NewOpenAIError(errors.New("content filter"), types.ErrorCodeUpstreamReject, http.StatusBadRequest),
			want: false,
		},
		{
			name: "hard_quota/402 允许禁用",
			err:  newServiceAPIError(http.StatusPaymentRequired, "payment required"),
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ShouldDisableChannel(tc.err))
		})
	}
}
