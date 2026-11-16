package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListPBRSessions(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)

	keyA := "11111111111111111111111111111111"
	keyB := "22222222222222222222222222222222"
	logs := []PBRRequestLog{
		{Ts: 100, SessionID: "sess-a", SessionKey: keyA, Client: "claude", RequestModel: "m1", LaneName: "l1", Success: true, PromptTokens: 10, CompletionTokens: 5, ContextTokens: 100, ContextWindow: 1000},
		{Ts: 300, SessionID: "sess-a", SessionKey: keyA, Client: "claude", RequestModel: "m2", LaneName: "l1", Success: false, PromptTokens: 20, CompletionTokens: 7, ContextTokens: 200, ContextWindow: 2000, EstimatedCost: 0.5},
		{Ts: 200, SessionID: "sess-b", SessionKey: keyB, Client: "curl", RequestModel: "m3", LaneName: "l2", Success: true, PromptTokens: 30, CompletionTokens: 9, EstimatedCost: 0.1},
		{Ts: 400, SessionID: "", SessionKey: "", Client: "claude", RequestModel: "m4", LaneName: "l1", Success: true},
	}
	for i := range logs {
		require.NoError(t, DB.Create(&logs[i]).Error)
	}

	items, total, err := ListPBRSessions(PBRSessionFilter{From: 1, Until: 1000, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, total, "requests without a session key are not sessions")
	require.Len(t, items, 2)
	// last_ts desc: A (300) then B (200).
	assert.Equal(t, keyA, items[0].SessionKey)
	assert.Equal(t, "sess-a", items[0].SessionID)
	assert.EqualValues(t, 2, items[0].Requests)
	assert.EqualValues(t, 1, items[0].Errors)
	assert.EqualValues(t, 30, items[0].PromptTokens)
	assert.EqualValues(t, 12, items[0].CompletionTokens)
	assert.EqualValues(t, 2, items[0].ContextRequests)
	assert.EqualValues(t, 200, items[0].ContextPeakTokens)
	assert.EqualValues(t, 2000, items[0].ContextWindow)
	assert.Equal(t, "m2", items[0].LastModel)
	assert.Equal(t, "claude", items[0].LastClient)
	assert.Equal(t, []string{"claude"}, items[0].Clients)
	assert.Equal(t, []string{"m1", "m2"}, items[0].Models)
	assert.Equal(t, []string{"l1"}, items[0].Lanes)

	assert.Equal(t, keyB, items[1].SessionKey)
	assert.Equal(t, []string{"curl"}, items[1].Clients)

	totals, err := PBRSessionTotals(PBRSessionFilter{From: 1, Until: 1000})
	require.NoError(t, err)
	assert.EqualValues(t, 2, totals.Sessions)
	assert.EqualValues(t, 3, totals.Requests)
	assert.EqualValues(t, 2, totals.Successes)
	assert.EqualValues(t, 1, totals.Errors)
	assert.EqualValues(t, 60, totals.PromptTokens)
	assert.EqualValues(t, 21, totals.CompletionTokens)
	assert.InDelta(t, 0.6, totals.EstimatedCost, 1e-9)

	// Filters: client, model, lane, q.
	filtered, total, err := ListPBRSessions(PBRSessionFilter{From: 1, Until: 1000, Client: "curl"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, filtered, 1)
	assert.Equal(t, keyB, filtered[0].SessionKey)

	filtered, _, err = ListPBRSessions(PBRSessionFilter{From: 1, Until: 1000, Model: "m1"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, keyA, filtered[0].SessionKey)

	filtered, _, err = ListPBRSessions(PBRSessionFilter{From: 1, Until: 1000, Lane: "l2"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, keyB, filtered[0].SessionKey)

	filtered, _, err = ListPBRSessions(PBRSessionFilter{From: 1, Until: 1000, Query: "sess-b"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, keyB, filtered[0].SessionKey)

	// Sorting by cost desc.
	sorted, _, err := ListPBRSessions(PBRSessionFilter{From: 1, Until: 1000, Sort: "cost", Order: "desc"})
	require.NoError(t, err)
	require.Len(t, sorted, 2)
	assert.Equal(t, keyA, sorted[0].SessionKey)

	// Pagination.
	page1, total, err := ListPBRSessions(PBRSessionFilter{From: 1, Until: 1000, Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, page1, 1)
	page2, _, err := ListPBRSessions(PBRSessionFilter{From: 1, Until: 1000, Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.NotEqual(t, page1[0].SessionKey, page2[0].SessionKey)

	// Time range excludes the other session.
	windowed, _, err := ListPBRSessions(PBRSessionFilter{From: 250, Until: 1000})
	require.NoError(t, err)
	require.Len(t, windowed, 1)
	assert.Equal(t, keyA, windowed[0].SessionKey)
}

func TestGetPBRSessionAndRequests(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	key := "33333333333333333333333333333333"
	for i := 0; i < 3; i++ {
		require.NoError(t, DB.Create(&PBRRequestLog{
			Ts: int64(100 + i), SessionID: "sess", SessionKey: key, Client: "claude",
			RequestModel: "m", Success: true,
		}).Error)
	}

	summary, err := GetPBRSession(key)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.EqualValues(t, 3, summary.Requests)

	missing, err := GetPBRSession("44444444444444444444444444444444")
	require.NoError(t, err)
	assert.Nil(t, missing)

	desc, total, err := ListPBRSessionRequests(key, 1, 2, false)
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Len(t, desc, 2)
	assert.EqualValues(t, 102, desc[0].Ts)

	asc, _, err := ListPBRSessionRequests(key, 1, 2, true)
	require.NoError(t, err)
	require.Len(t, asc, 2)
	assert.EqualValues(t, 100, asc[0].Ts)
}

func TestPBRSessionContextSeriesTruncates(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	key := "55555555555555555555555555555555"
	for i := 0; i < 5; i++ {
		require.NoError(t, DB.Create(&PBRRequestLog{
			Ts: int64(100 + i), SessionID: "sess", SessionKey: key, Client: "claude",
			RequestModel: "m", Success: true, ContextTokens: 10 * (i + 1), ContextWindow: 1000,
		}).Error)
	}
	points, truncated, err := PBRSessionContextSeries(key, 3)
	require.NoError(t, err)
	assert.True(t, truncated)
	require.Len(t, points, 3)
	// The latest three, ascending by time: ts 102,103,104.
	assert.EqualValues(t, 102, points[0].Ts)
	assert.EqualValues(t, 104, points[2].Ts)
	assert.EqualValues(t, 50, points[2].Tokens)
}

func TestPBRRequestContextsByLogIDs(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_contexts").Error)
	entries := []PBRRequestLog{
		{Ts: 1, Success: true},
		{Ts: 2, Success: true},
	}
	for i := range entries {
		require.NoError(t, DB.Create(&entries[i]).Error)
	}
	require.NoError(t, DB.Create(&PBRRequestContext{LogId: entries[0].Id, Status: "ok", Tokens: 1}).Error)

	rows, err := PBRRequestContextsByLogIDs([]int{entries[0].Id, entries[1].Id})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[entries[0].Id].Tokens)
	assert.Nil(t, rows[entries[1].Id])

	empty, err := PBRRequestContextsByLogIDs(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
