package model

import (
	"sort"
	"strings"

	"gorm.io/gorm"
)

// 会话聚合（usage-spec §3/§7，api-spec §4.6/§5.5）：按 session_key 汇总窗口内的
// 模型面请求。会话只来自保留期内的明细；prune 之后随之消失，聚合表不参与。

// PBRSessionFilter is the list filter. SessionKey is internal (detail lookup);
// the list API uses Client/Model/Lane/Query.
type PBRSessionFilter struct {
	From       int64
	Until      int64
	Client     string
	Model      string
	Lane       string
	Query      string
	SessionKey string
	Sort       string
	Order      string
	Page       int
	PageSize   int
}

// PBRSessionSummary is one row of GET /api/logs/sessions.
type PBRSessionSummary struct {
	SessionID         string
	SessionKey        string
	FirstTs           int64
	LastTs            int64
	Requests          int64
	Successes         int64
	Errors            int64
	PromptTokens      int64
	CompletionTokens  int64
	CacheReadTokens   int64
	CacheWriteTokens  int64
	ReasoningTokens   int64
	EstimatedCost     float64
	Clients           []string
	Models            []string
	Lanes             []string
	LastModel         string
	LastClient        string
	ContextPeakTokens int
	ContextWindow     int
	ContextRequests   int64
	ParentSessionID   string
}

// PBRSessionContextPoint is one point of the session's context-fill series.
type PBRSessionContextPoint struct {
	Ts     int64
	Id     int
	Tokens int
	Window int
}

// PBRSessionWindowTotals is the whole filtered window, not the current page.
type PBRSessionWindowTotals struct {
	Sessions         int64   `json:"sessions"`
	Requests         int64   `json:"requests"`
	Successes        int64   `json:"successes"`
	Errors           int64   `json:"errors"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
}

// PBRSessionTotals aggregates every matching session in the window.
func PBRSessionTotals(filter PBRSessionFilter) (PBRSessionWindowTotals, error) {
	var row struct {
		Sessions         int64
		Requests         int64
		Successes        int64
		PromptTokens     int64
		CompletionTokens int64
		EstimatedCost    float64
	}
	query := applyPBRSessionFilter(DB.Model(&PBRRequestLog{}), filter)
	if err := query.Select(
		"COUNT(DISTINCT session_key) AS sessions, COUNT(*) AS requests, " +
			"SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes, " +
			"SUM(prompt_tokens) AS prompt_tokens, SUM(completion_tokens) AS completion_tokens, " +
			"SUM(estimated_cost) AS estimated_cost").Scan(&row).Error; err != nil {
		return PBRSessionWindowTotals{}, err
	}
	return PBRSessionWindowTotals{
		Sessions:         row.Sessions,
		Requests:         row.Requests,
		Successes:        row.Successes,
		Errors:           row.Requests - row.Successes,
		PromptTokens:     row.PromptTokens,
		CompletionTokens: row.CompletionTokens,
		EstimatedCost:    row.EstimatedCost,
	}, nil
}

func applyPBRSessionFilter(query *gorm.DB, filter PBRSessionFilter) *gorm.DB {
	query = query.Where("session_key <> ''")
	if filter.From > 0 {
		query = query.Where("ts >= ?", filter.From)
	}
	if filter.Until > 0 {
		query = query.Where("ts < ?", filter.Until)
	}
	if filter.Client != "" {
		query = query.Where("client = ?", filter.Client)
	}
	if filter.Model != "" {
		query = query.Where("request_model = ?", filter.Model)
	}
	if filter.Lane != "" {
		query = query.Where("lane_name = ?", filter.Lane)
	}
	if filter.SessionKey != "" {
		query = query.Where("session_key = ?", filter.SessionKey)
	}
	if filter.Query != "" {
		query = query.Where("(session_id LIKE ? OR session_key = ?)", "%"+filter.Query+"%", filter.Query)
	}
	return query
}

func pbrSessionOrder(filter PBRSessionFilter) string {
	column := map[string]string{
		"last_ts":  "last_ts",
		"first_ts": "first_ts",
		"requests": "requests",
		"tokens":   "(SUM(prompt_tokens) + SUM(completion_tokens))",
		"cost":     "estimated_cost",
	}[strings.ToLower(strings.TrimSpace(filter.Sort))]
	if column == "" {
		column = "last_ts"
	}
	direction := "desc"
	if strings.EqualFold(strings.TrimSpace(filter.Order), "asc") {
		direction = "asc"
	}
	// session_key 作稳定次级键：同一时间/同一数值的会话不会在翻页时漂移。
	return column + " " + direction + ", session_key " + direction
}

// ListPBRSessions returns one page of sessions and the total number of
// matching sessions.
func ListPBRSessions(filter PBRSessionFilter) ([]PBRSessionSummary, int64, error) {
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var total int64
	countQuery := applyPBRSessionFilter(DB.Model(&PBRRequestLog{}), filter)
	if err := countQuery.Distinct("session_key").Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type row struct {
		SessionID         string
		SessionKey        string
		FirstTs           int64
		LastTs            int64
		Requests          int64
		Successes         int64
		PromptTokens      int64
		CompletionTokens  int64
		CacheReadTokens   int64
		CacheWriteTokens  int64
		ReasoningTokens   int64
		EstimatedCost     float64
		ContextPeakTokens int
		ContextWindow     int
		ContextRequests   int64
		ParentSessionID   string
	}
	var rows []row
	base := applyPBRSessionFilter(DB.Model(&PBRRequestLog{}), filter)
	query := base.Select(
		"session_key AS session_key, MIN(session_id) AS session_id, " +
			"MIN(ts) AS first_ts, MAX(ts) AS last_ts, COUNT(*) AS requests, " +
			"SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes, " +
			"SUM(prompt_tokens) AS prompt_tokens, SUM(completion_tokens) AS completion_tokens, " +
			"SUM(cache_read_tokens) AS cache_read_tokens, SUM(cache_write_tokens) AS cache_write_tokens, " +
			"SUM(reasoning_tokens) AS reasoning_tokens, SUM(estimated_cost) AS estimated_cost, " +
			"MAX(context_tokens) AS context_peak_tokens, MAX(context_window) AS context_window, " +
			"SUM(CASE WHEN context_tokens > 0 THEN 1 ELSE 0 END) AS context_requests, " +
			"MAX(parent_session_id) AS parent_session_id").
		Group("session_key").
		Order(pbrSessionOrder(filter)).
		Limit(pageSize).
		Offset((page - 1) * pageSize)
	if err := query.Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return []PBRSessionSummary{}, total, nil
	}

	keys := make([]string, 0, len(rows))
	for _, item := range rows {
		keys = append(keys, item.SessionKey)
	}
	clients, models, lanes, lastModels, lastClients, err := loadPBRSessionFacets(filter, keys)
	if err != nil {
		return nil, 0, err
	}
	items := make([]PBRSessionSummary, 0, len(rows))
	for _, item := range rows {
		items = append(items, PBRSessionSummary{
			SessionID:         item.SessionID,
			SessionKey:        item.SessionKey,
			FirstTs:           item.FirstTs,
			LastTs:            item.LastTs,
			Requests:          item.Requests,
			Successes:         item.Successes,
			Errors:            item.Requests - item.Successes,
			PromptTokens:      item.PromptTokens,
			CompletionTokens:  item.CompletionTokens,
			CacheReadTokens:   item.CacheReadTokens,
			CacheWriteTokens:  item.CacheWriteTokens,
			ReasoningTokens:   item.ReasoningTokens,
			EstimatedCost:     item.EstimatedCost,
			Clients:           clients[item.SessionKey],
			Models:            models[item.SessionKey],
			Lanes:             lanes[item.SessionKey],
			LastModel:         lastModels[item.SessionKey],
			LastClient:        lastClients[item.SessionKey],
			ContextPeakTokens: item.ContextPeakTokens,
			ContextWindow:     item.ContextWindow,
			ContextRequests:   item.ContextRequests,
			ParentSessionID:   item.ParentSessionID,
		})
	}
	return items, total, nil
}

// loadPBRSessionFacets fills the distinct clients/models/lanes and the latest
// model/client for the page's sessions. One extra query per facet group keeps
// the list query free of GROUP_CONCAT comma ambiguity.
func loadPBRSessionFacets(filter PBRSessionFilter, keys []string) (
	map[string][]string, map[string][]string, map[string][]string,
	map[string]string, map[string]string, error,
) {
	clients := map[string]map[string]bool{}
	models := map[string]map[string]bool{}
	lanes := map[string]map[string]bool{}
	var rows []struct {
		SessionKey   string
		Client       string
		RequestModel string
		LaneName     string
		Ts           int64
		Id           int
	}
	query := applyPBRSessionFilter(DB.Model(&PBRRequestLog{}), filter).
		Select("session_key, client, request_model, lane_name, ts, id").
		Where("session_key IN ?", keys)
	if err := query.Find(&rows).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	lastModels := map[string]string{}
	lastClients := map[string]string{}
	lastTs := map[string]int64{}
	lastIDs := map[string]int{}
	for _, row := range rows {
		if row.Client != "" {
			addFacet(clients, row.SessionKey, row.Client)
		}
		if row.RequestModel != "" {
			addFacet(models, row.SessionKey, row.RequestModel)
		}
		if row.LaneName != "" {
			addFacet(lanes, row.SessionKey, row.LaneName)
		}
		if row.Ts > lastTs[row.SessionKey] ||
			(row.Ts == lastTs[row.SessionKey] && row.Id > lastIDs[row.SessionKey]) {
			lastTs[row.SessionKey] = row.Ts
			lastIDs[row.SessionKey] = row.Id
			lastModels[row.SessionKey] = row.RequestModel
			lastClients[row.SessionKey] = row.Client
		}
	}
	return flattenFacets(clients), flattenFacets(models), flattenFacets(lanes), lastModels, lastClients, nil
}

func addFacet(index map[string]map[string]bool, key, value string) {
	if index[key] == nil {
		index[key] = map[string]bool{}
	}
	index[key][value] = true
}

func flattenFacets(index map[string]map[string]bool) map[string][]string {
	out := make(map[string][]string, len(index))
	for key, values := range index {
		list := make([]string, 0, len(values))
		for value := range values {
			list = append(list, value)
		}
		sort.Strings(list)
		if len(list) > 20 {
			list = list[:20]
		}
		out[key] = list
	}
	return out
}

// GetPBRSession returns one session summary, nil when the key is absent.
func GetPBRSession(key string) (*PBRSessionSummary, error) {
	items, _, err := ListPBRSessions(PBRSessionFilter{SessionKey: key, Page: 1, PageSize: 1})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return &items[0], nil
}

// ListPBRSessionRequests returns one page of a session's requests. The
// default order is newest first; asc returns the conversation in order.
func ListPBRSessionRequests(key string, page, pageSize int, asc bool) ([]PBRRequestLog, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var total int64
	if err := DB.Model(&PBRRequestLog{}).Where("session_key = ?", key).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := "ts desc, id desc"
	if asc {
		order = "ts asc, id asc"
	}
	var entries []PBRRequestLog
	if err := DB.Where("session_key = ?", key).
		Order(order).
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&entries).Error; err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// PBRSessionContextSeries returns the latest points of a session's context
// fill, ascending by time. truncated is true when older points were dropped.
func PBRSessionContextSeries(key string, limit int) ([]PBRSessionContextPoint, bool, error) {
	if limit <= 0 {
		limit = 500
	}
	var rows []PBRSessionContextPoint
	if err := DB.Model(&PBRRequestLog{}).
		Select("ts, id, context_tokens AS tokens, context_window AS window").
		Where("session_key = ?", key).
		Order("ts desc, id desc").
		Limit(limit + 1).
		Find(&rows).Error; err != nil {
		return nil, false, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows, truncated, nil
}

// PBRRequestContextsByLogIDs loads context details for a page of requests.
func PBRRequestContextsByLogIDs(ids []int) (map[int]*PBRRequestContext, error) {
	out := map[int]*PBRRequestContext{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []PBRRequestContext
	if err := DB.Where("log_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].LogId] = &rows[i]
	}
	return out, nil
}
