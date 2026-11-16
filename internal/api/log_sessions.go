package api

import (
	"encoding/hex"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/usage"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 会话聚合读面（usage-spec §7，api-spec §4.6/§5.5）。
//
// 会话只来自明细日志，因此只覆盖保留期；管理动作没有 session_key，不进入列表。

// ListLogSessions GET /api/logs/sessions
func ListLogSessions(c *gin.Context) {
	filter, err := parseSessionFilter(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	items, total, err := model.ListPBRSessions(filter)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	totals, err := model.PBRSessionTotals(filter)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	out := make([]gin.H, 0, len(items))
	for i := range items {
		out = append(out, sessionResponse(&items[i]))
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     out,
		"total":     total,
		"page":      filter.Page,
		"page_size": filter.PageSize,
		"summary":   totals,
	})
}

// GetLogSession GET /api/logs/sessions/{key}
func GetLogSession(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	if !validSessionKey(key) {
		apierr.NotFound(c, apierr.CodeSessionNotFound, "session not found", "GET /api/logs/sessions")
		return
	}
	summary, err := model.GetPBRSession(key)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if summary == nil {
		apierr.NotFound(c, apierr.CodeSessionNotFound, "session not found", "GET /api/logs/sessions")
		return
	}

	page, pageSize, err := parseSessionPage(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	asc := false
	switch strings.ToLower(strings.TrimSpace(c.DefaultQuery("order", "desc"))) {
	case "desc":
	case "asc":
		asc = true
	default:
		apierr.Validation(c, "order must be asc or desc")
		return
	}

	entries, total, err := model.ListPBRSessionRequests(key, page, pageSize, asc)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	ids := make([]int, 0, len(entries))
	for i := range entries {
		ids = append(ids, entries[i].Id)
	}
	contexts, err := model.PBRRequestContextsByLogIDs(ids)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	requests := make([]gin.H, 0, len(entries))
	for i := range entries {
		item := logResponse(&entries[i])
		item["context"] = compactContext(contexts[entries[i].Id])
		requests = append(requests, item)
	}

	series, truncated, err := model.PBRSessionContextSeries(key, 500)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	points := make([]gin.H, 0, len(series))
	for _, point := range series {
		points = append(points, gin.H{
			"ts":     rfc3339(point.Ts),
			"id":     point.Id,
			"tokens": point.Tokens,
			"window": point.Window,
		})
	}

	response := sessionResponse(summary)
	response["request_items"] = requests
	response["total"] = total
	response["page"] = page
	response["page_size"] = pageSize
	response["context_series"] = points
	response["context_series_truncated"] = truncated
	c.JSON(http.StatusOK, response)
}

func parseSessionFilter(c *gin.Context) (model.PBRSessionFilter, error) {
	page, pageSize, err := parseSessionPage(c)
	if err != nil {
		return model.PBRSessionFilter{}, err
	}
	filter := model.PBRSessionFilter{
		Client:   strings.TrimSpace(c.Query("client")),
		Model:    strings.TrimSpace(c.Query("model")),
		Lane:     strings.TrimSpace(c.Query("lane")),
		Query:    strings.TrimSpace(c.Query("q")),
		Sort:     strings.ToLower(strings.TrimSpace(c.DefaultQuery("sort", "last_ts"))),
		Order:    strings.ToLower(strings.TrimSpace(c.DefaultQuery("order", "desc"))),
		Page:     page,
		PageSize: pageSize,
	}
	switch filter.Sort {
	case "last_ts", "first_ts", "requests", "tokens", "cost":
	default:
		return filter, &apiError{code: apierr.CodeValidationFailed, message: "sort must be last_ts, first_ts, requests, tokens or cost"}
	}
	if filter.Order != "asc" && filter.Order != "desc" {
		return filter, &apiError{code: apierr.CodeValidationFailed, message: "order must be asc or desc"}
	}
	if len(filter.Query) > 128 {
		return filter, &apiError{code: apierr.CodeValidationFailed, message: "q must be at most 128 characters"}
	}
	from, err := parseTimeQuery(c.Query("from"))
	if err != nil {
		return filter, &apiError{code: apierr.CodeValidationFailed, message: "from must be RFC3339 or unix seconds"}
	}
	until, err := parseTimeQuery(c.Query("to"))
	if err != nil {
		return filter, &apiError{code: apierr.CodeValidationFailed, message: "to must be RFC3339 or unix seconds"}
	}
	if from == 0 {
		from = time.Now().AddDate(0, 0, -CurrentLogRetentionDays()).Unix()
	}
	if until == 0 {
		until = time.Now().Unix() + 1
	}
	if until <= from {
		return filter, &apiError{code: apierr.CodeValidationFailed, message: "to must be after from"}
	}
	filter.From, filter.Until = from, until
	return filter, nil
}

func parseSessionPage(c *gin.Context) (int, int, error) {
	page := 1
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return 0, 0, &apiError{code: apierr.CodeValidationFailed, message: "page must be a positive integer"}
		}
		page = parsed
	}
	pageSize := 50
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return 0, 0, &apiError{code: apierr.CodeValidationFailed, message: "page_size must be a positive integer"}
		}
		pageSize = parsed
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, nil
}

func validSessionKey(key string) bool {
	if len(key) != 32 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func sessionResponse(item *model.PBRSessionSummary) gin.H {
	return gin.H{
		"session_id":          item.SessionID,
		"session_key":         item.SessionKey,
		"first_ts":            rfc3339(item.FirstTs),
		"last_ts":             rfc3339(item.LastTs),
		"requests":            item.Requests,
		"successes":           item.Successes,
		"errors":              item.Errors,
		"prompt_tokens":       item.PromptTokens,
		"completion_tokens":   item.CompletionTokens,
		"cache_read_tokens":   item.CacheReadTokens,
		"cache_write_tokens":  item.CacheWriteTokens,
		"reasoning_tokens":    item.ReasoningTokens,
		"estimated_cost":      item.EstimatedCost,
		"clients":             nonNilStrings(item.Clients),
		"client_groups":       clientGroups(item.Clients),
		"models":              nonNilStrings(item.Models),
		"lanes":               nonNilStrings(item.Lanes),
		"last_model":          item.LastModel,
		"last_client":         item.LastClient,
		"context_peak_tokens": item.ContextPeakTokens,
		"context_window":      item.ContextWindow,
		"context_requests":    item.ContextRequests,
		"parent_session_id":   item.ParentSessionID,
	}
}

// compactContext is what a session timeline row carries: totals per part, no
// item names. The full card is GET /api/logs/{id}.
func compactContext(row *model.PBRRequestContext) any {
	if row == nil {
		return nil
	}
	context := row.UsageContext()
	if context == nil {
		return nil
	}
	parts := make([]gin.H, 0, len(context.Parts))
	for _, part := range context.Parts {
		parts = append(parts, gin.H{"kind": part.Kind, "tokens": part.Tokens})
	}
	return gin.H{
		"tokens":  context.Tokens,
		"window":  context.Window,
		"counted": context.Counted,
		"status":  context.Status,
		"parts":   parts,
	}
}

// clientGroups maps a session's raw clients to the display groups: known
// agents keep their id; libraries and other requests are scripts.
func clientGroups(clients []string) []string {
	seen := map[string]bool{}
	groups := make([]string, 0, len(clients))
	for _, client := range clients {
		group := usage.ClientGroup(client)
		if group == "" || seen[group] {
			continue
		}
		seen[group] = true
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}
