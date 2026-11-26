package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zzyyyds88/PowerBarRations/internal/usage"
)

// 保留期压缩只清排障细节，汇总行必须保留（用户明确要求历史明细行仍可检索）。
func TestCompactPBRRequestLogsKeepsSummaryAndClearsDetails(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_contexts").Error)

	old := &PBRRequestLog{
		Ts:                1_700_000_000,
		LaneName:          "lane-old",
		RequestModel:      "lane-old",
		MemberChannelName: "channel-old",
		UpstreamModel:     "model-old",
		Success:           true,
		PromptTokens:      1000,
		CompletionTokens:  200,
		EstimatedCost:     0.25,
		TTFTMs:            1200,
		TotalMs:           3400,
		SessionID:         "session-old",
		TotalAttempts:     3,
		Attempts:          `[{"attempt_num":1,"member":"channel-old/model-old","status":"failed"}]`,
		ErrorSummary:      "upstream said no",
		MaskHits:          `[{"kind":"api_key"}]`,
	}
	require.NoError(t, DB.Create(old).Error)
	require.NoError(t, SavePBRRequestContext(old.Id, &usage.Context{
		Version: 1, Status: usage.StatusOK, Tokens: 10,
	}, 42, old.Ts))

	recent := &PBRRequestLog{
		Ts: 1_800_000_000, LaneName: "lane-recent", Success: true,
		Attempts: `[{"attempt_num":1,"status":"success"}]`, ErrorSummary: "recent error",
	}
	require.NoError(t, DB.Create(recent).Error)

	compacted, err := CompactPBRRequestLogsBefore(1_750_000_000)
	require.NoError(t, err)
	assert.EqualValues(t, 1, compacted.CompactedRows)
	assert.EqualValues(t, 1, compacted.DeletedContexts)

	var after PBRRequestLog
	require.NoError(t, DB.First(&after, old.Id).Error)
	assert.Equal(t, "lane-old", after.LaneName, "汇总列必须保留")
	assert.EqualValues(t, 1000, after.PromptTokens)
	assert.EqualValues(t, 0.25, after.EstimatedCost)
	assert.EqualValues(t, 3400, after.TotalMs)
	assert.Equal(t, "session-old", after.SessionID)
	assert.EqualValues(t, 3, after.TotalAttempts)
	assert.Equal(t, "[]", after.Attempts, "attempts 链必须清空")
	assert.Empty(t, after.ErrorSummary)
	assert.Empty(t, after.MaskHits)

	var contexts int64
	require.NoError(t, DB.Model(&PBRRequestContext{}).Count(&contexts).Error)
	assert.Zero(t, contexts, "上下文分区必须删除")

	var untouched PBRRequestLog
	require.NoError(t, DB.First(&untouched, recent.Id).Error)
	assert.Equal(t, "recent error", untouched.ErrorSummary, "保留期内细节不得清理")
	assert.Contains(t, untouched.Attempts, "success")
}

func TestCompactPBRRequestLogsIsIdempotent(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_contexts").Error)

	entry := &PBRRequestLog{
		Ts: 1_700_000_000, LaneName: "lane-idem", Success: true,
		Attempts: `[{"attempt_num":1}]`, ErrorSummary: "e",
	}
	require.NoError(t, DB.Create(entry).Error)

	first, err := CompactPBRRequestLogsBefore(1_750_000_000)
	require.NoError(t, err)
	assert.EqualValues(t, 1, first.CompactedRows)
	second, err := CompactPBRRequestLogsBefore(1_750_000_000)
	require.NoError(t, err)
	assert.Zero(t, second.CompactedRows, "已压缩行不得重复计为待处理")
	assert.Zero(t, second.DeletedContexts)
}
