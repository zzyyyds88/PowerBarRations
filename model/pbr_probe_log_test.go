package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 探活记录（渠道健康页的"探活历史"，api-spec §6.12）：写入、滚动裁剪、读取与清理。
func TestProbeLogWriteCapReadAndPrune(t *testing.T) {
	truncateTables(t)

	const channelId = 7
	base := int64(1_700_000_000)
	for i := 0; i < ProbeLogKeepPerChannel+5; i++ {
		require.NoError(t, WriteProbeLog(&PBRProbeLog{
			Ts:            base + int64(i),
			ChannelId:     channelId,
			ChannelName:   "probe-ch",
			UpstreamModel: "probe-model",
			Status:        "success",
			DurationMs:    int64(i),
			HTTPStatus:    200,
		}))
	}

	var count int64
	require.NoError(t, DB.Model(&PBRProbeLog{}).Where("channel_id = ?", channelId).Count(&count).Error)
	assert.EqualValues(t, ProbeLogKeepPerChannel, count, "每渠道只保留最近 N 条（有界，不依赖外部清理）")

	rows, err := RecentProbeLogs(channelId, 5)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	assert.EqualValues(t, ProbeLogKeepPerChannel+4, rows[0].DurationMs, "读取顺序为新 → 旧")

	// 另一渠道的记录不受本渠道裁剪影响。
	require.NoError(t, WriteProbeLog(&PBRProbeLog{Ts: base, ChannelId: 8, ChannelName: "other-ch", Status: "failed"}))
	var otherCount int64
	require.NoError(t, DB.Model(&PBRProbeLog{}).Where("channel_id = ?", 8).Count(&otherCount).Error)
	assert.EqualValues(t, 1, otherCount)

	// 每个渠道的最近一条（汇总端点用）。
	latest, err := LatestProbeLogByChannel([]int{channelId, 8, 999})
	require.NoError(t, err)
	assert.EqualValues(t, base+int64(ProbeLogKeepPerChannel+4), latest[channelId].Ts)
	assert.Equal(t, "other-ch", latest[8].ChannelName)
	_, ok := latest[999]
	assert.False(t, ok, "没有记录的渠道不返回条目")

	// 按时间清理（POST /logs/prune 的联动）：只删 before 之前的（两个渠道一起清）。
	deleted, err := PruneProbeLogsBefore(base + 10)
	require.NoError(t, err)
	assert.EqualValues(t, 6, deleted, "本渠道 i=5..9 共 5 条 + 另一渠道 1 条")
	remaining, err := CountProbeLogsBefore(0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, remaining, "before<=0 不清理，计数为 0")
}
