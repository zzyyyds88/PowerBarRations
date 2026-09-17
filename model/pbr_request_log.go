package model

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PBRChannelModelSeparator 是「渠道 × 模型」聚合维度的键分隔符。
// group_key = <渠道名> + PBRChannelModelSeparator + <请求模型>。
const PBRChannelModelSeparator = "␟"

// pbrChannelModelBackfillOptionKey 是 channel_model 一次性回填的完成标记；
// 标记存在即不再重放（design/breakdown W9）。
const pbrChannelModelBackfillOptionKey = "PBRChannelModelStatsBackfilled"

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
	// 「渠道 × 模型」维度要求两者都非空；任一为空则跳过（与其它维度的空值跳过口径一致）。
	if strings.TrimSpace(entry.MemberChannelName) != "" && strings.TrimSpace(entry.RequestModel) != "" {
		dimensions["channel_model"] = entry.MemberChannelName + PBRChannelModelSeparator + entry.RequestModel
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
//
// ⚠️ 这里读的是**明细表**：`POST /logs/prune` 删掉明细后，历史统计会一起消失。
// 对外 `/api/stats` 必须用 AggregatePBRStatsFromHourly 读聚合表；本函数只用于
// 明细与聚合不一致时的对账/排障。
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

// AggregatePBRStatsFromHourly 从**小时聚合表**汇总统计（`GET /api/stats` 的唯一数据源）。
//
// 为什么必须读聚合表：`POST /logs/prune` 会删除明细（design-v1 §16.5 要求明细按策略清理、
// 聚合长期保留）。若 /stats 读明细，一次清理就会让看板与历史统计凭空消失——聚合表
// 只写不读也会变成死数据。
//
// 粒度：`hour` 直读小时桶；`day` 由小时桶上卷（bucket_ts/86400*86400）。
// 时间过滤按"**桶与 [from, until] 相交**"判定：一个桶代表 [bucket_ts, bucket_ts+窗长)
// 区间，桶起点可能早于 from（例如 from 落在某小时中间），只比 bucket_ts 会把
// "包含 from 的那个桶"整个丢掉。until 侧则按桶起点（起点晚于 until 的桶不含窗口内数据）。
func AggregatePBRStatsFromHourly(from, until int64, granularity, groupBy string) ([]PBRStatBucket, error) {
	groupKind := strings.ToLower(strings.TrimSpace(groupBy))
	switch groupKind {
	case "lane", "channel", "key", "model", "channel_model":
	default:
		groupKind = "lane"
	}
	bucketExpr := "bucket_ts"
	windowSeconds := int64(3600)
	if strings.EqualFold(granularity, "day") {
		bucketExpr = "(bucket_ts / 86400) * 86400"
		windowSeconds = 86400
	}

	var rows []PBRStatBucket
	// 别名必须是 group_key（GORM 按字段名 group_key 扫描）；对外的 JSON 字段名
	// 仍由 PBRStatBucket 的 json tag 决定为 "group"。
	query := DB.Model(&PBRStatsHourly{}).
		Select(bucketExpr+" AS bucket_ts, group_key AS group_key, "+
			"SUM(requests) AS requests, SUM(successes) AS successes, "+
			"SUM(prompt_tokens) AS prompt_tokens, SUM(completion_tokens) AS completion_tokens, "+
			"SUM(cost_sum) AS estimated_cost").
		Where("group_kind = ?", groupKind).
		Group(bucketExpr + ", group_key").
		Order("bucket_ts desc")
	if from > 0 {
		query = query.Where(bucketExpr+" + ? >= ?", windowSeconds, from)
	}
	if until > 0 {
		query = query.Where("bucket_ts <= ?", until)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []PBRStatBucket{}
	}
	return rows, nil
}

// BackfillPBRChannelModelStats 一次性回填「渠道 × 模型」聚合桶（breakdown W9）。
//
// 背景：channel_model 是新增维度，升级前写入的明细从未聚合过该维度。本函数从
// request_logs 明细按 (小时桶, 渠道, 模型) 重新聚合，只补写缺失的桶
// （OnConflict DoNothing），全部写入成功后落 option 标记，避免每次启动重放。
//
// 与 migrateDB 的其它派生数据迁移一致：失败由调用方记日志、不阻塞启动；明细为空
// 时直接跳过（此后新请求由 upsertPBRHourlyStat 实时写入，不需要回填）。
func BackfillPBRChannelModelStats() error {
	if DB == nil {
		return nil
	}

	var marker Option
	err := DB.Where(&Option{Key: pbrChannelModelBackfillOptionKey}).First(&marker).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	var detailCount int64
	if err := DB.Model(&PBRRequestLog{}).Count(&detailCount).Error; err != nil {
		return err
	}
	if detailCount == 0 {
		return nil
	}

	type channelModelRow struct {
		BucketTs          int64
		MemberChannelName string
		RequestModel      string
		Requests          int64
		Successes         int64
		PromptTokens      int64
		CompletionTokens  int64
		EstimatedCost     float64
	}
	var rows []channelModelRow
	// 不做 SQL 字符串拼接：直接按 (桶, 渠道, 模型) 两列分组，兼容 SQLite/MySQL/PostgreSQL。
	err = DB.Model(&PBRRequestLog{}).
		Select("(ts / 3600) * 3600 AS bucket_ts, member_channel_name AS member_channel_name, " +
			"request_model AS request_model, COUNT(*) AS requests, " +
			"SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes, " +
			"SUM(prompt_tokens) AS prompt_tokens, SUM(completion_tokens) AS completion_tokens, " +
			"SUM(estimated_cost) AS estimated_cost").
		Where("TRIM(member_channel_name) <> '' AND TRIM(request_model) <> ''").
		Group("(ts / 3600) * 3600, member_channel_name, request_model").
		Scan(&rows).Error
	if err != nil {
		return err
	}

	for _, item := range rows {
		row := PBRStatsHourly{
			BucketTs:         item.BucketTs,
			GroupKind:        "channel_model",
			GroupKey:         item.MemberChannelName + PBRChannelModelSeparator + item.RequestModel,
			Requests:         item.Requests,
			Successes:        item.Successes,
			PromptTokens:     item.PromptTokens,
			CompletionTokens: item.CompletionTokens,
			CostSum:          item.EstimatedCost,
		}
		// 实时写入已存在的桶不得叠加：缺失才补，已存在则原样保留。
		if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}

	// 只有全部桶成功落库后才写标记；失败则本次不写，下次启动重试。
	return DB.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&Option{Key: pbrChannelModelBackfillOptionKey, Value: "true"}).Error
}
