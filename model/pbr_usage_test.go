package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/internal/usage"
)

func TestNormalizedInputTokens(t *testing.T) {
	tests := []struct {
		name      string
		prompt    int
		cacheRead int
		cacheW    int
		semantic  string
		want      int64
	}{
		{"openai prompt includes cache", 100, 40, 10, "openai", 100},
		{"empty semantic is openai", 100, 40, 10, "", 100},
		{"gemini prompt includes cache", 100, 40, 10, "gemini", 100},
		{"anthropic adds cache", 100, 40, 10, "anthropic", 150},
		{"anthropic case-insensitive", 100, 40, 10, "Anthropic", 150},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, normalizedInputTokens(test.prompt, test.cacheRead, test.cacheW, test.semantic))
		})
	}
}

func TestContextWindowForModelPrecedence(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM models").Error)
	records := []Model{
		{ModelName: "model-a", NameRule: NameRuleExact, ContextWindow: 1000},
		{ModelName: "model-", NameRule: NameRulePrefix, ContextWindow: 500},
		{ModelName: "upstream-x", NameRule: NameRuleExact, ContextWindow: 2000},
		{ModelName: "zero", NameRule: NameRuleExact, ContextWindow: 0},
	}
	for i := range records {
		require.NoError(t, DB.Create(&records[i]).Error)
	}

	assert.Equal(t, 1000, ContextWindowForModel("model-a", "upstream-x"), "member model exact wins")
	assert.Equal(t, 500, ContextWindowForModel("model-b", "upstream-x"), "prefix rule applies")
	assert.Equal(t, 2000, ContextWindowForModel("unknown", "upstream-x"), "falls back to upstream name")
	assert.Equal(t, 0, ContextWindowForModel("unknown"), "unknown model has no window")
	assert.Equal(t, 0, ContextWindowForModel("zero"), "a zero window is not a match")
	assert.Equal(t, 0, ContextWindowForModel())
}

func TestSaveAndReadRequestContext(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_contexts").Error)

	logEntry := &PBRRequestLog{Ts: 1_700_000_000, LaneName: "lane-ctx", Success: true}
	require.NoError(t, DB.Create(logEntry).Error)

	context := &usage.Context{
		Version:         1,
		Status:          usage.StatusOK,
		Tokens:          100,
		EstimatedTokens: 90,
		Window:          2000,
		Counted:         true,
		Turns:           2,
		Parts: []usage.Part{
			{Kind: usage.PartSystem, Tokens: 40, Items: []usage.Item{{Name: "Agent prompt", Tag: "prompt", Tokens: 40}}},
			{Kind: usage.PartChat, Tokens: 60, Items: []usage.Item{{Name: "1", Tag: "turn", Tokens: 60}}},
		},
	}
	require.NoError(t, SavePBRRequestContext(logEntry.Id, context, 1234, logEntry.Ts))

	row, err := GetPBRRequestContext(logEntry.Id)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, 100, row.Tokens)
	assert.Equal(t, 2000, row.Window)
	assert.True(t, row.Counted)
	assert.Equal(t, 1234, row.BodyBytes)
	assert.Equal(t, logEntry.Ts, row.CreatedAt)

	restored := row.UsageContext()
	require.NotNil(t, restored)
	assert.Equal(t, 100, restored.Tokens)
	require.Len(t, restored.Parts, 2)
	assert.Equal(t, usage.PartSystem, restored.Parts[0].Kind)
	assert.Equal(t, "Agent prompt", restored.Parts[0].Items[0].Name)

	missing, err := GetPBRRequestContext(logEntry.Id + 1)
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestPruneRemovesContextsWithLogs(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_contexts").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)

	logEntry := &PBRRequestLog{Ts: 1_700_000_000, LaneName: "lane-prune-ctx", Success: true}
	require.NoError(t, DB.Create(logEntry).Error)
	require.NoError(t, SavePBRRequestContext(logEntry.Id, &usage.Context{
		Version: 1, Status: usage.StatusOK, Tokens: 10, Parts: []usage.Part{},
	}, 42, logEntry.Ts))

	deleted, err := PrunePBRRequestLogsBefore(logEntry.Ts + 1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	var logs, contexts int64
	require.NoError(t, DB.Model(&PBRRequestLog{}).Count(&logs).Error)
	require.NoError(t, DB.Model(&PBRRequestContext{}).Count(&contexts).Error)
	assert.Zero(t, logs)
	assert.Zero(t, contexts, "上下文必须随明细同批清理")
}

func TestUpsertHourlyStatAddsUsageFieldsAndClientDimension(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)

	entry := &PBRRequestLog{
		Ts:                1_700_000_000,
		LaneName:          "lane-usage",
		MemberChannelName: "channel-usage",
		TokenName:         "key-usage",
		UpstreamModel:     "model-usage",
		Client:            "claude",
		Success:           true,
		PromptTokens:      100,
		CompletionTokens:  20,
		CacheReadTokens:   40,
		CacheWriteTokens:  10,
		ReasoningTokens:   5,
		TTFTMs:            123,
		UsageSemantic:     "anthropic",
	}
	upsertPBRHourlyStat(entry)

	byClient, err := AggregatePBRStatsFromHourly(0, 0, "hour", "client")
	require.NoError(t, err)
	require.Len(t, byClient, 1)
	bucket := byClient[0]
	assert.Equal(t, "claude", bucket.GroupKey)
	assert.Equal(t, "claude", bucket.ClientGroup)
	assert.Equal(t, "Claude Code", bucket.ClientName)
	assert.EqualValues(t, 1, bucket.Requests)
	assert.EqualValues(t, 1, bucket.Successes)
	assert.EqualValues(t, 150, bucket.InputTokens, "anthropic input adds cache read/write")
	assert.EqualValues(t, 40, bucket.CacheReadTokens)
	assert.EqualValues(t, 10, bucket.CacheWriteTokens)
	assert.EqualValues(t, 5, bucket.ReasoningTokens)
	assert.EqualValues(t, 123, bucket.TTFTMsSum)
	assert.EqualValues(t, 1, bucket.TTFTSamples)

	byLane, err := AggregatePBRStatsFromHourly(0, 0, "hour", "lane")
	require.NoError(t, err)
	require.Len(t, byLane, 1)
	assert.EqualValues(t, 150, byLane[0].InputTokens)
}

func TestRebuildStatsBackfillsClientAndUsageFields(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)

	entries := []PBRRequestLog{
		{
			Ts: 1_700_000_000, LaneName: "lane-rebuild", MemberChannelName: "ch-rebuild",
			TokenName: "key-rebuild", UpstreamModel: "model-rebuild", Client: "codex",
			Success: true, PromptTokens: 100, CompletionTokens: 10,
			CacheReadTokens: 30, CacheWriteTokens: 5, ReasoningTokens: 2,
			TTFTMs: 50, UsageSemantic: "anthropic",
		},
		{
			Ts: 1_700_000_100, LaneName: "lane-rebuild", MemberChannelName: "ch-rebuild",
			TokenName: "key-rebuild", UpstreamModel: "model-rebuild", Client: "codex",
			Success: true, PromptTokens: 200, CompletionTokens: 20,
			CacheReadTokens: 50, UsageSemantic: "openai",
		},
	}
	for i := range entries {
		require.NoError(t, DB.Create(&entries[i]).Error)
	}

	result, err := RebuildStatsFromLogs(false)
	require.NoError(t, err)
	assert.False(t, result.DryRun)

	byClient, err := AggregatePBRStatsFromHourly(0, 0, "hour", "client")
	require.NoError(t, err)
	require.Len(t, byClient, 1)
	assert.Equal(t, "codex", byClient[0].GroupKey)
	assert.Equal(t, "codex", byClient[0].ClientGroup)
	assert.Equal(t, "Codex", byClient[0].ClientName)
	assert.EqualValues(t, 2, byClient[0].Requests)
	assert.EqualValues(t, 335, byClient[0].InputTokens, "135 (anthropic) + 200 (openai)")
	assert.EqualValues(t, 80, byClient[0].CacheReadTokens)
	assert.EqualValues(t, 5, byClient[0].CacheWriteTokens)
	assert.EqualValues(t, 2, byClient[0].ReasoningTokens)
	assert.EqualValues(t, 50, byClient[0].TTFTMsSum)
	assert.EqualValues(t, 1, byClient[0].TTFTSamples)
}

func TestPBRLogFilterHasContext(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&PBRRequestLog{
		Ts: 100, RequestModel: "model-a", ContextTokens: 12, ContextStatus: "ok",
	}).Error)
	require.NoError(t, DB.Create(&PBRRequestLog{
		Ts: 200, RequestModel: "model-b",
	}).Error)
	has := true
	rows, err := ListPBRRequestLogs(PBRRequestLogFilter{Context: &has, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "model-a", rows[0].RequestModel)

	without := false
	rows, err = ListPBRRequestLogs(PBRRequestLogFilter{Context: &without, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "model-b", rows[0].RequestModel)
}
