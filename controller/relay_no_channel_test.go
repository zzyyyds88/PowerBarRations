package controller

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pbrroute "github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// 全部成员 upstream_reject 是"这份请求被拒绝"，不是车道停摆：
// 必须原样透传上游错误，且不得写 lane_unavailable 事件。
func TestNoChannelErrorDoesNotMarkLaneUnavailableOnReject(t *testing.T) {
	lane := "lane-no-channel-reject"
	resolved := &model.ResolvedRoute{
		Model:  lane,
		Source: model.RouteSourceExplicit,
		Members: []model.RouteMember{{
			ChannelId: 1, Channel: "channel-a", Model: "model-a", UpstreamModel: "model-a",
		}},
	}
	state := &pbrroute.State{Route: resolved, Runtime: pbrroute.Default.For(lane)}
	t.Cleanup(func() { pbrroute.Default.Remove(lane) })

	reject := types.NewOpenAIError(errors.New("content filtered"), types.ErrorCode(""), 400)
	info := &relaycommon.RelayInfo{LastError: reject}
	got := noChannelError(info, state)
	require.Same(t, reject, got)
	assert.False(t, pbrroute.IsNoAvailable(got))

	health := state.Runtime.Health(resolved, pbrroute.CurrentCircuitSettings())
	for _, ev := range health.Events {
		assert.NotEqual(t, pbrroute.EventLaneUnavailable, ev.Type,
			"reject 耗尽不得伪造 lane_unavailable")
	}
}

// 真正没有可用成员时仍按 §4.2 合成 503 并登记 lane_unavailable。
func TestNoChannelErrorMarksLaneUnavailableWithoutReject(t *testing.T) {
	lane := "lane-no-channel-503"
	resolved := &model.ResolvedRoute{
		Model:  lane,
		Source: model.RouteSourceExplicit,
		Members: []model.RouteMember{{
			ChannelId: 1, Channel: "channel-a", Model: "model-a", UpstreamModel: "model-a",
		}},
	}
	state := &pbrroute.State{Route: resolved, Runtime: pbrroute.Default.For(lane)}
	t.Cleanup(func() { pbrroute.Default.Remove(lane) })

	info := &relaycommon.RelayInfo{OriginModelName: lane}
	got := noChannelError(info, state)
	require.NotNil(t, got)
	assert.True(t, pbrroute.IsNoAvailable(got))

	health := state.Runtime.Health(resolved, pbrroute.CurrentCircuitSettings())
	found := false
	for _, ev := range health.Events {
		if ev.Type == pbrroute.EventLaneUnavailable {
			found = true
		}
	}
	assert.True(t, found, "真正无可用成员必须登记 lane_unavailable")
}
