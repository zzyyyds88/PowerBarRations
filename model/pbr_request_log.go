package model

import (
	"encoding/json"
	"strings"
	"time"

	"pbr/common"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PBR 请求日志（design-v1 §8，api-spec §5.5）。
//
// **只存元数据**：不存请求/响应正文，不存密钥明文，不存余额。attempts 链用于解释
// "为什么没用 P1、为什么最后 503"。
//
// 写入分两条路：
//   - 正常计费路径：基座的 RecordConsumeLog 会带上 token 用量与折算金额；
//   - 未走到计费的成功/失败（例如成员耗尽直接 503）：由转发收尾补写，token 记 0。
//
// 两条路共用同一个载体（PBRLogCarrier），并用 Written 去重，保证"一请求一行"。

const ContextKeyPBRLogCarrier = "pbr_log_carrier"

// PBRAttempt 一次尝试（routing-spec §9）。
type PBRAttempt struct {
	AttemptNum int    `json:"attempt_num"`
	Member     string `json:"member"`
	Status     string `json:"status"` // success | failed | cooldown | circuit_break | skipped
	// DurationMs 不加 omitempty：亚毫秒尝试耗时为 0，若省略该字段，调用方
	// 就得区分"缺失"与"0"，契约不稳定（routing-spec §9 要求每条尝试都有耗时）。
	DurationMs int64  `json:"duration_ms"`
	ErrorKind  string `json:"error_kind,omitempty"`
	Msg        string `json:"msg,omitempty"`
}

// PBRLogCarrier 请求作用域的日志载体；由 PBR 路由在各阶段填充。
type PBRLogCarrier struct {
	Written bool

	Lane          string
	RequestModel  string
	RouteSource   string
	InboundFormat string

	ChannelId     int
	ChannelName   string
	UpstreamModel string
	KeyId         int
	KeyName       string

	Success       bool
	HTTPStatus    int
	ErrorKind     string
	ErrorSummary  string
	Attempts      []PBRAttempt
	TotalAttempts int

	StartedAtMs int64
	TTFTMs      int64
	TotalMs     int64

	// Usage 由计费路径回填（token 用量与折算金额）；未走到计费时保持 nil。
	Usage *PBRTokenUsage
}

// PBRRequestLog 单表元数据日志。
type PBRRequestLog struct {
	Id int   `json:"id" gorm:"primaryKey"`
	Ts int64 `json:"ts" gorm:"bigint;index:idx_pbr_log_ts"`

	LaneName     string `json:"lane" gorm:"type:varchar(191);index"`
	RequestModel string `json:"request_model" gorm:"type:varchar(191);index"`
	RouteSource  string `json:"route_source" gorm:"type:varchar(16)"`

	MemberChannelId   int    `json:"channel_id" gorm:"index"`
	MemberChannelName string `json:"channel" gorm:"type:varchar(191)"`
	UpstreamModel     string `json:"upstream_model" gorm:"type:varchar(191)"`

	TokenId   int    `json:"token_id"`
	TokenName string `json:"key_name" gorm:"type:varchar(191);index"`

	InboundFormat string `json:"inbound_format" gorm:"type:varchar(32)"`
	Success       bool   `json:"success" gorm:"index"`
	HTTPStatus    int    `json:"http_status"`
	ErrorKind     string `json:"error_kind,omitempty" gorm:"type:varchar(32)"`
	ErrorSummary  string `json:"error_summary,omitempty" gorm:"type:text"`

	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens"`

	TTFTMs   int64 `json:"ttft_ms"`
	TotalMs  int64 `json:"total_ms"`
	IsStream bool  `json:"is_stream"`

	Attempts      string  `json:"attempts" gorm:"column:attempts;type:text"`
	TotalAttempts int     `json:"total_attempts"`
	EstimatedCost float64 `json:"estimated_cost"`
}

// TableName 固定表名（GORM 默认复数化会得到 pbr_request_logs，这里显式声明避免歧义）。
func (PBRRequestLog) TableName() string { return "pbr_request_logs" }

// GetPBRLogCarrier 取请求作用域的日志载体。
func GetPBRLogCarrier(c *gin.Context) *PBRLogCarrier {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(ContextKeyPBRLogCarrier); ok {
		if carrier, ok := value.(*PBRLogCarrier); ok {
			return carrier
		}
	}
	return nil
}

// EnsurePBRLogCarrier 取或创建载体。
func EnsurePBRLogCarrier(c *gin.Context) *PBRLogCarrier {
	if carrier := GetPBRLogCarrier(c); carrier != nil {
		return carrier
	}
	carrier := &PBRLogCarrier{StartedAtMs: time.Now().UnixMilli()}
	c.Set(ContextKeyPBRLogCarrier, carrier)
	return carrier
}

// WritePBRLog 落一条日志。**唯一写入点**是转发收尾，保证一请求一行且字段完整；
// token 用量由 model.RecordConsumeLog 回填到载体（未走到计费时为 0）。
func WritePBRLog(c *gin.Context) {
	carrier := GetPBRLogCarrier(c)
	WritePBRLogWithUsage(c, func() *PBRTokenUsage {
		if carrier == nil {
			return nil
		}
		return carrier.Usage
	}())
}

func WritePBRLogWithUsage(c *gin.Context, tokenUsage *PBRTokenUsage) {
	carrier := GetPBRLogCarrier(c)
	if carrier == nil || carrier.Written {
		return
	}
	carrier.Written = true

	attempts := carrier.Attempts
	if attempts == nil {
		attempts = []PBRAttempt{}
	}
	encoded, err := json.Marshal(attempts)
	if err != nil {
		encoded = []byte("[]")
	}
	summary := carrier.ErrorSummary
	if len(summary) > 2048 {
		summary = summary[:2048]
	}
	totalMs := carrier.TotalMs
	if totalMs == 0 && carrier.StartedAtMs > 0 {
		totalMs = time.Now().UnixMilli() - carrier.StartedAtMs
	}

	entry := PBRRequestLog{
		Ts:                time.Now().Unix(),
		LaneName:          carrier.Lane,
		RequestModel:      carrier.RequestModel,
		RouteSource:       carrier.RouteSource,
		MemberChannelId:   carrier.ChannelId,
		MemberChannelName: carrier.ChannelName,
		UpstreamModel:     carrier.UpstreamModel,
		TokenId:           carrier.KeyId,
		TokenName:         carrier.KeyName,
		InboundFormat:     carrier.InboundFormat,
		Success:           carrier.Success,
		HTTPStatus:        carrier.HTTPStatus,
		ErrorKind:         carrier.ErrorKind,
		ErrorSummary:      summary,
		TTFTMs:            carrier.TTFTMs,
		TotalMs:           totalMs,
		Attempts:          string(encoded),
		TotalAttempts:     carrier.TotalAttempts,
	}
	if tokenUsage != nil {
		entry.PromptTokens = tokenUsage.PromptTokens
		entry.CompletionTokens = tokenUsage.CompletionTokens
		entry.CacheReadTokens = tokenUsage.CacheReadTokens
		entry.CacheWriteTokens = tokenUsage.CacheWriteTokens
		entry.ReasoningTokens = tokenUsage.ReasoningTokens
		entry.IsStream = tokenUsage.IsStream
		entry.EstimatedCost = tokenUsage.EstimatedCost
		if carrier.TTFTMs == 0 {
			entry.TTFTMs = tokenUsage.TTFTMs
		}
		if tokenUsage.TotalMs > 0 {
			entry.TotalMs = tokenUsage.TotalMs
		}
	}
	if err := DB.Create(&entry).Error; err != nil {
		common.SysError("pbr: write request log failed: " + err.Error())
		return
	}
	upsertPBRHourlyStat(&entry)
}

// PBRStatsHourly 小时级聚合（design-v1 §8：供 /stats 与 UI 图表）。
// 按 (bucket_ts, group_kind, group_key) 主键增量 upsert，四个维度各一行。
// TableName 显式定名：GORM 默认复数化会得到 pbr_stats_hourlies，与文档口径不一致。
func (PBRStatsHourly) TableName() string { return "pbr_stats_hourly" }

type PBRStatsHourly struct {
	BucketTs         int64   `json:"bucket_ts" gorm:"primaryKey"`
	GroupKind        string  `json:"group_kind" gorm:"primaryKey;type:varchar(16)"`
	GroupKey         string  `json:"group" gorm:"primaryKey;type:varchar(191)"`
	Requests         int64   `json:"requests"`
	Successes        int64   `json:"successes"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	CostSum          float64 `json:"estimated_cost"`
}

func upsertPBRHourlyStat(entry *PBRRequestLog) {
	bucket := (entry.Ts / 3600) * 3600
	successes := int64(0)
	if entry.Success {
		successes = 1
	}
	dimensions := map[string]string{
		"lane":    entry.LaneName,
		"channel": entry.MemberChannelName,
		"key":     entry.TokenName,
		"model":   entry.RequestModel,
	}
	for kind, key := range dimensions {
		if strings.TrimSpace(key) == "" {
			continue
		}
		// 首次 INSERT 必须带上本次增量：只靠 ON CONFLICT 的 += 会让每个新维度组恒定少 1 条
		row := PBRStatsHourly{
			BucketTs:         bucket,
			GroupKind:        kind,
			GroupKey:         key,
			Requests:         1,
			Successes:        successes,
			PromptTokens:     int64(entry.PromptTokens),
			CompletionTokens: int64(entry.CompletionTokens),
			CostSum:          entry.EstimatedCost,
		}
		if err := DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "bucket_ts"}, {Name: "group_kind"}, {Name: "group_key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"requests":          gorm.Expr("requests + ?", 1),
				"successes":         gorm.Expr("successes + ?", successes),
				"prompt_tokens":     gorm.Expr("prompt_tokens + ?", entry.PromptTokens),
				"completion_tokens": gorm.Expr("completion_tokens + ?", entry.CompletionTokens),
				"cost_sum":          gorm.Expr("cost_sum + ?", entry.EstimatedCost),
			}),
		}).Create(&row).Error; err != nil {
			// 聚合是派生数据，失败不能影响主流程，但必须可见——否则"少记"会静默发生
			common.SysError("pbr: upsert hourly stat failed: " + err.Error())
		}
	}
}

// PBRTokenUsage 计费路径提供的用量与折算（成本只折算，不参与准入）。
type PBRTokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  int
	CacheWriteTokens int
	ReasoningTokens  int
	IsStream         bool
	TTFTMs           int64
	TotalMs          int64
	EstimatedCost    float64
}

// PBRRequestLogFilter 查询过滤条件。
type PBRRequestLogFilter struct {
	Lane     string
	Channel  string
	Key      string
	Model    string
	Success  *bool
	Since    int64
	Until    int64
	BeforeId int
	Limit    int
}

// ListPBRRequestLogs 按 id 倒序分页。
func ListPBRRequestLogs(filter PBRRequestLogFilter) ([]PBRRequestLog, error) {
	query := DB.Model(&PBRRequestLog{}).Order("id desc").Limit(filter.Limit + 1)
	if filter.Lane != "" {
		query = query.Where("lane_name = ?", filter.Lane)
	}
	if filter.Channel != "" {
		query = query.Where("member_channel_name = ?", filter.Channel)
	}
	if filter.Key != "" {
		query = query.Where("token_name = ?", filter.Key)
	}
	if filter.Model != "" {
		query = query.Where("request_model = ?", filter.Model)
	}
	if filter.Success != nil {
		query = query.Where("success = ?", *filter.Success)
	}
	if filter.Since > 0 {
		query = query.Where("ts >= ?", filter.Since)
	}
	if filter.Until > 0 {
		query = query.Where("ts <= ?", filter.Until)
	}
	if filter.BeforeId > 0 {
		query = query.Where("id < ?", filter.BeforeId)
	}
	var entries []PBRRequestLog
	if err := query.Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// GetPBRRequestLogById 单条（含 attempts 链）。
func GetPBRRequestLogById(id int) (*PBRRequestLog, error) {
	var entry PBRRequestLog
	if err := DB.Where("id = ?", id).First(&entry).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

// CountPBRRequestLogsBefore 统计 ts < before 的明细条数（prune 的 dry-run 用）。
func CountPBRRequestLogsBefore(before int64) (int64, error) {
	var count int64
	if err := DB.Model(&PBRRequestLog{}).Where("ts < ?", before).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// PrunePBRRequestLogsBefore 删除 ts < before 的明细日志，返回删除条数。
//
// 只删明细表：聚合表（pbr_stats_hourly）体积小且长期保留（design-v1 §16.5）。
func PrunePBRRequestLogsBefore(before int64) (int64, error) {
	result := DB.Where("ts < ?", before).Delete(&PBRRequestLog{})
	return result.RowsAffected, result.Error
}

// PBRStatBucket 聚合桶。
type PBRStatBucket struct {
	BucketTs         int64   `json:"bucket_ts"`
	GroupKey         string  `json:"group"`
	Requests         int64   `json:"requests"`
	Successes        int64   `json:"successes"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
}

// AggregatePBRStats 按时间粒度与维度聚合（group_by ∈ lane|channel|key|model）。
func AggregatePBRStats(from, until int64, granularity, groupBy string) ([]PBRStatBucket, error) {
	groupColumn := map[string]string{
		"lane":    "lane_name",
		"channel": "member_channel_name",
		"key":     "token_name",
		"model":   "request_model",
	}[strings.ToLower(groupBy)]
	if groupColumn == "" {
		groupColumn = "lane_name"
	}
	// SQLite：按小时/天取整。
	var bucketExpr string
	if strings.EqualFold(granularity, "day") {
		bucketExpr = "(ts / 86400) * 86400"
	} else {
		bucketExpr = "(ts / 3600) * 3600"
	}

	type row struct {
		BucketTs         int64
		GroupKey         string
		Requests         int64
		Successes        int64
		PromptTokens     int64
		CompletionTokens int64
		EstimatedCost    float64
	}
	var rows []row
	query := DB.Model(&PBRRequestLog{}).
		Select(bucketExpr + " AS bucket_ts, " + groupColumn + " AS group_key, COUNT(*) AS requests, " +
			"SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes, " +
			"SUM(prompt_tokens) AS prompt_tokens, SUM(completion_tokens) AS completion_tokens, " +
			"SUM(estimated_cost) AS estimated_cost").
		Group(bucketExpr + ", " + groupColumn).
		Order("bucket_ts desc")
	if from > 0 {
		query = query.Where("ts >= ?", from)
	}
	if until > 0 {
		query = query.Where("ts <= ?", until)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]PBRStatBucket, 0, len(rows))
	for _, item := range rows {
		out = append(out, PBRStatBucket{
			BucketTs:         item.BucketTs,
			GroupKey:         item.GroupKey,
			Requests:         item.Requests,
			Successes:        item.Successes,
			PromptTokens:     item.PromptTokens,
			CompletionTokens: item.CompletionTokens,
			EstimatedCost:    item.EstimatedCost,
		})
	}
	return out, nil
}
