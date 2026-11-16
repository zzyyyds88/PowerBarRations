package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/usage"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runListLogSessions(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/logs/sessions?"+query, nil)
	ListLogSessions(c)
	return recorder
}

func runGetLogSession(t *testing.T, key string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/logs/sessions/"+key, nil)
	c.Params = gin.Params{{Key: "key", Value: key}}
	GetLogSession(c)
	return recorder
}

func runGetLog(t *testing.T, id int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/logs/"+strconv.Itoa(id), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(id)}}
	GetLog(c)
	return recorder
}

func TestListLogSessionsAggregates(t *testing.T) {
	db := setupLogsAPITestDB(t)

	sessionA := "sess-a"
	keyA := usage.SessionKey(sessionA)
	sessionB := "sess-b"
	keyB := usage.SessionKey(sessionB)
	logs := []model.PBRRequestLog{
		{Ts: 100, SessionID: sessionA, SessionKey: keyA, Client: "claude", RequestModel: "m1", LaneName: "l1", MemberChannelName: "ch1", Success: true, PromptTokens: 10, CompletionTokens: 5, ContextTokens: 100, ContextWindow: 1000, ContextCounted: true},
		{Ts: 200, SessionID: sessionA, SessionKey: keyA, Client: "claude", RequestModel: "m2", LaneName: "l1", MemberChannelName: "ch1", Success: false, PromptTokens: 20, CompletionTokens: 7, ContextTokens: 200, ContextWindow: 2000, ContextCounted: true, EstimatedCost: 0.5},
		{Ts: 150, SessionID: sessionB, SessionKey: keyB, Client: "curl", RequestModel: "m3", LaneName: "l2", MemberChannelName: "ch2", Success: true, PromptTokens: 30, CompletionTokens: 9, EstimatedCost: 0.1},
	}
	for i := range logs {
		require.NoError(t, db.Create(&logs[i]).Error)
	}

	rec := runListLogSessions(t, "from=1&to=1000")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Items    []map[string]any `json:"items"`
		Total    int              `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
		Summary  struct {
			Sessions int     `json:"sessions"`
			Requests int     `json:"requests"`
			Errors   int     `json:"errors"`
			Cost     float64 `json:"estimated_cost"`
		} `json:"summary"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 2, body.Total)
	require.Len(t, body.Items, 2)
	assert.Equal(t, 2, body.Summary.Sessions)
	assert.Equal(t, 3, body.Summary.Requests)
	assert.Equal(t, 1, body.Summary.Errors)
	assert.InDelta(t, 0.6, body.Summary.Cost, 1e-9)
	// last_ts desc: session A (200) before B (150).
	assert.Equal(t, keyA, body.Items[0]["session_key"])
	assert.EqualValues(t, 2, body.Items[0]["requests"])
	assert.EqualValues(t, 1, body.Items[0]["errors"])
	assert.EqualValues(t, 2, body.Items[0]["context_requests"])
	assert.EqualValues(t, 200, body.Items[0]["context_peak_tokens"])
	assert.EqualValues(t, 2000, body.Items[0]["context_window"])
	assert.Equal(t, "m2", body.Items[0]["last_model"])
	assert.Equal(t, []any{"claude"}, body.Items[0]["clients"])
	assert.Equal(t, []any{"claude"}, body.Items[0]["client_groups"])

	assert.Equal(t, keyB, body.Items[1]["session_key"])
	assert.Equal(t, "curl", body.Items[1]["last_client"])
	assert.Equal(t, []any{"curl"}, body.Items[1]["clients"])
	assert.Equal(t, []any{"script"}, body.Items[1]["client_groups"])

	// Filters.
	filtered := runListLogSessions(t, "from=1&to=1000&client=curl")
	require.Equal(t, http.StatusOK, filtered.Code, filtered.Body.String())
	var filteredBody struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(filtered.Body.Bytes(), &filteredBody))
	require.Len(t, filteredBody.Items, 1)
	assert.Equal(t, keyB, filteredBody.Items[0]["session_key"])

	searched := runListLogSessions(t, "from=1&to=1000&q=sess-a")
	var searchedBody struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(searched.Body.Bytes(), &searchedBody))
	require.Len(t, searchedBody.Items, 1)
	assert.Equal(t, keyA, searchedBody.Items[0]["session_key"])
}

func TestGetLogSessionDetail(t *testing.T) {
	db := setupLogsAPITestDB(t)
	sessionID := "sess-detail"
	key := usage.SessionKey(sessionID)
	first := model.PBRRequestLog{Ts: 100, SessionID: sessionID, SessionKey: key, Client: "claude", RequestModel: "m1", Success: true, PromptTokens: 10, ContextTokens: 100, ContextWindow: 1000, ContextCounted: true}
	second := model.PBRRequestLog{Ts: 200, SessionID: sessionID, SessionKey: key, Client: "claude", RequestModel: "m1", Success: true, PromptTokens: 20, ContextTokens: 200, ContextWindow: 1000, ContextCounted: true}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, db.Create(&model.PBRRequestContext{
		LogId: first.Id, Version: 1, Status: usage.StatusOK, Tokens: 100, Window: 1000, Counted: true,
		Parts: `[{"kind":"system","tokens":40},{"kind":"chat","tokens":60}]`,
	}).Error)

	rec := runGetLogSession(t, key)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, key, body["session_key"])
	assert.EqualValues(t, 2, body["requests"].(float64))
	assert.EqualValues(t, 2, body["total"].(float64))
	requests := body["request_items"].([]any)
	require.Len(t, requests, 2)
	// Default order desc: ts 200 first.
	firstRow := requests[0].(map[string]any)
	assert.Equal(t, "m1", firstRow["request_model"])
	secondRow := requests[1].(map[string]any)
	context := secondRow["context"].(map[string]any)
	assert.EqualValues(t, 100, context["tokens"])
	assert.EqualValues(t, 1000, context["window"])
	assert.Equal(t, true, context["counted"])
	parts := context["parts"].([]any)
	require.Len(t, parts, 2)
	assert.Equal(t, "system", parts[0].(map[string]any)["kind"])
	series := body["context_series"].([]any)
	require.Len(t, series, 2)
	assert.Equal(t, false, body["context_series_truncated"])

	// Invalid and unknown keys are 404 session_not_found.
	for _, bad := range []string{"not-a-key", "00000000000000000000000000000000"} {
		badRec := runGetLogSession(t, bad)
		assert.Equal(t, http.StatusNotFound, badRec.Code)
		assert.Contains(t, badRec.Body.String(), "session_not_found")
	}
}

func TestGetLogIncludesContext(t *testing.T) {
	db := setupLogsAPITestDB(t)
	entry := model.PBRRequestLog{Ts: 100, SessionID: "s", SessionKey: usage.SessionKey("s"), Client: "claude", Success: true}
	require.NoError(t, db.Create(&entry).Error)
	require.NoError(t, db.Create(&model.PBRRequestContext{
		LogId: entry.Id, Version: 1, Status: usage.StatusOK, Tokens: 10, Window: 100, Counted: true,
		Parts: `[{"kind":"system","tokens":10,"items":[{"name":"Agent prompt","tag":"prompt","tokens":10}]}]`,
	}).Error)

	rec := runGetLog(t, entry.Id)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	context := body["context"].(map[string]any)
	assert.EqualValues(t, 10, context["tokens"])
	parts := context["parts"].([]any)
	require.Len(t, parts, 1)
	part := parts[0].(map[string]any)
	assert.Equal(t, "system", part["kind"])
	items := part["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "Agent prompt", items[0].(map[string]any)["name"])
}

func TestListLogSessionsValidation(t *testing.T) {
	setupLogsAPITestDB(t)
	cases := []string{
		"sort=bogus",
		"order=bogus",
		"from=200&to=100",
		"page=0",
		"page_size=0",
	}
	for _, query := range cases {
		rec := runListLogSessions(t, query)
		assert.Equal(t, http.StatusBadRequest, rec.Code, query+" -> "+rec.Body.String())
		assert.Contains(t, rec.Body.String(), "validation_failed")
	}
}
