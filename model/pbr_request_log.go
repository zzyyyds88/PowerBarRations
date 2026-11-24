package model

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/usage"
	"github.com/zzyyyds88/PowerBarRations/setting/pricing_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PBRChannelModelSeparator 是「渠道 × 模型」聚合维度的键分隔符。
// group_key = <渠道名> + PBRChannelModelSeparator + <请求模型>。
const PBRChannelModelSeparator = "␟"

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
	// MemberModel 成员**所选模型**（lane_members.model / 组内成员的所选模型，
	// ADR 0008）：成本折算的计价键（design §16.9#7——渠道 × 模型，与车道无关）。
	// 选路注入；渠道测试等管理路径没有所选模型，留空回退 RequestModel。
	MemberModel string
	// UsageSemantic 上游用量语义（dto.BillingUsageSemantic*）：决定缓存折算是否
	// 从 prompt 扣减（openai/gemini 扣、anthropic 不扣）。空串按 openai。
	UsageSemantic string
	KeyId         int
	KeyName       string

	// 统一日志表：管理动作（渠道测试等）记录管理员与来源 IP；模型面请求为空。
	UserId   int
	Username string
	Ip       string

	Success       bool
	HTTPStatus    int
	ErrorKind     string
	ErrorSummary  string
	Attempts      []PBRAttempt
	TotalAttempts int

	// StartedAtMs 请求开始时刻（入站建载体时捕获，毫秒）。峰谷计价按它匹配
	// 时段（design-v1 §16.9#7），首字耗时也用它算间隔。
	StartedAtMs int64
	TTFTMs      int64
	TotalMs     int64

	// Usage 由计费路径回填（token 用量与折算金额）；未走到计费时保持 nil。
	Usage *PBRTokenUsage

	// 用量元数据（usage-spec §3–§6）：由转发入口在读取入站 body 时填充。
	SessionID       string
	SessionKey      string
	Subagent        string
	ParentSubagent  string
	ParentSessionID string
	Client          string
	Context         *usage.Context
	ContextBodySize int

	// 隐私脱敏（privacy-spec §7）：只存命中元数据，不存原值/正文。
	MaskedCount int
	MaskBlocked bool
	MaskStatus  string
	MaskHits    string
	MaskTookMs  int64
}

// PBRRequestLog 单表元数据日志。
type PBRRequestLog struct {
	Id int   `json:"id" gorm:"primaryKey"`
	Ts int64 `json:"ts" gorm:"bigint;index:idx_pbr_log_ts;index:idx_pbr_log_session_ts,priority:2;index:idx_pbr_log_client_ts,priority:2"`

	LaneName     string `json:"lane" gorm:"type:varchar(191);index"`
	RequestModel string `json:"request_model" gorm:"type:varchar(191);index"`
	RouteSource  string `json:"route_source" gorm:"type:varchar(16)"`

	MemberChannelId   int    `json:"channel_id" gorm:"index"`
	MemberChannelName string `json:"channel" gorm:"type:varchar(191)"`
	UpstreamModel     string `json:"upstream_model" gorm:"type:varchar(191)"`
	// MemberModel 成员所选模型（计价键，design §16.9#7）；历史行为空——
	// stats/rebuild 重折算时空值回退 upstream_model。
	MemberModel string `json:"member_model" gorm:"type:varchar(191)"`
	// UsageSemantic 上游用量语义（anthropic/gemini/openai，空=openai）；决定
	// 缓存折算公式。历史行为空——rebuild 空值按 openai（历史明细绝大多数是
	// OpenAI 兼容渠道；Anthropic 历史行会有微小偏差，已在 api-spec 注明）。
	UsageSemantic string `json:"usage_semantic" gorm:"type:varchar(16)"`

	TokenId   int    `json:"token_id"`
	TokenName string `json:"key_name" gorm:"type:varchar(191);index"`

	// 统一日志表（design-v1 §8）：管理动作（渠道测试等）记录管理员与来源 IP，
	// 模型面请求为空（PBR 无用户体系，调用方身份是客户端密钥 key_name）。
	UserId   int    `json:"user_id"`
	Username string `json:"username" gorm:"type:varchar(191);index"`
	Ip       string `json:"ip" gorm:"type:varchar(64)"`
	// Type 区分消耗（2）与错误（5）——控制台一个日志视图按类型渲染徽章。
	Type int `json:"type"`

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
	// PriceBand 命中的峰谷时段下标（-1 = 未命中、使用行级兜底价；旧单层价
	// 恒为 -1）。**不能加 default 标签**：GORM 会把零值 0 替换成默认值，
	// 让"命中第 0 个 band"被误写成 -1；列的默认值由迁移显式负责。
	PriceBand int `json:"price_band" gorm:"column:price_band;not null"`

	// 用量元数据（usage-spec §3–§6）。session_id 是原始标识，session_key 是
	// sha256 前 16 字节（URL 安全、可索引）；client 是 UA 归一后的展示标签。
	SessionID       string `json:"session_id" gorm:"type:varchar(128);index"`
	SessionKey      string `json:"session_key" gorm:"type:varchar(32);index:idx_pbr_log_session_ts,priority:1"`
	Subagent        string `json:"subagent" gorm:"type:varchar(128)"`
	ParentSubagent  string `json:"parent_subagent" gorm:"type:varchar(128)"`
	ParentSessionID string `json:"parent_session_id" gorm:"type:varchar(128)"`
	Client          string `json:"client" gorm:"type:varchar(64);index:idx_pbr_log_client_ts,priority:1"`
	ContextTokens   int    `json:"context_tokens"`
	ContextWindow   int    `json:"context_window"`
	ContextCounted  bool   `json:"context_counted"`
	ContextStatus   string `json:"context_status" gorm:"type:varchar(16)"`

	MaskedCount int    `json:"masked_count"`
	MaskBlocked bool   `json:"mask_blocked"`
	MaskStatus  string `json:"mask_status" gorm:"type:varchar(24)"`
	MaskHits    string `json:"mask_hits,omitempty" gorm:"type:text"`
	MaskTookMs  int64  `json:"mask_took_ms"`
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
	now := time.Now()
	carrier := &PBRLogCarrier{StartedAtMs: now.UnixMilli()}
	c.Set(ContextKeyPBRLogCarrier, carrier)
	// 起始时刻同时写进上下文：首字耗时（relay/helper.MarkFirstByte）要用它算
	// "请求开始 → 第一帧数据"的间隔。此前这个键**只有读没有写**，基座几处读者
	// 只能各自兜底 time.Now()（耗时恒为 0）；在这里补上写点，两边同时受益。
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, now)
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
		MemberModel:       carrier.MemberModel,
		UsageSemantic:     carrier.UsageSemantic,
		TokenId:           carrier.KeyId,
		TokenName:         carrier.KeyName,
		UserId:            carrier.UserId,
		Username:          carrier.Username,
		Ip:                carrier.Ip,
		SessionID:         carrier.SessionID,
		SessionKey:        carrier.SessionKey,
		Subagent:          carrier.Subagent,
		ParentSubagent:    carrier.ParentSubagent,
		ParentSessionID:   carrier.ParentSessionID,
		Client:            carrier.Client,
		// 消耗（2）/错误（5）按成功与否判定，前端一个视图按类型渲染徽章。
		Type:          LogTypeConsume,
		InboundFormat: carrier.InboundFormat,
		Success:       carrier.Success,
		HTTPStatus:    carrier.HTTPStatus,
		ErrorKind:     carrier.ErrorKind,
		ErrorSummary:  summary,
		TTFTMs:        carrier.TTFTMs,
		TotalMs:       totalMs,
		Attempts:      string(encoded),
		TotalAttempts: carrier.TotalAttempts,
		// 未走到计费（无 usage）时没有峰谷命中，按契约记兜底 -1。
		PriceBand:   pricing_setting.FallbackPriceBand,
		MaskedCount: carrier.MaskedCount,
		MaskBlocked: carrier.MaskBlocked,
		MaskStatus:  carrier.MaskStatus,
		MaskHits:    carrier.MaskHits,
		MaskTookMs:  carrier.MaskTookMs,
	}
	if !entry.Success {
		entry.Type = LogTypeError
	}
	if tokenUsage != nil {
		entry.PromptTokens = tokenUsage.PromptTokens
		entry.CompletionTokens = tokenUsage.CompletionTokens
		entry.CacheReadTokens = tokenUsage.CacheReadTokens
		entry.CacheWriteTokens = tokenUsage.CacheWriteTokens
		entry.ReasoningTokens = tokenUsage.ReasoningTokens
		entry.IsStream = tokenUsage.IsStream
		entry.EstimatedCost = tokenUsage.EstimatedCost
		// nil = 调用方未提供峰谷信息，保持兜底 -1（不能用零值判断：
		// 0 是合法的"命中第 0 个 band"）。
		if tokenUsage.PriceBand != nil {
			entry.PriceBand = *tokenUsage.PriceBand
		}
		if carrier.TTFTMs == 0 {
			entry.TTFTMs = tokenUsage.TTFTMs
		}
		if tokenUsage.TotalMs > 0 {
			entry.TotalMs = tokenUsage.TotalMs
		}
	}
	if carrier.Context != nil {
		entry.ContextStatus = carrier.Context.Status
		if carrier.Context.Status == usage.StatusOK {
			counted := 0
			if tokenUsage != nil {
				counted = tokenUsage.PromptTokens
			}
			carrier.Context.Calibrate(counted, ContextWindowForModel(
				carrier.MemberModel, carrier.UpstreamModel, carrier.RequestModel))
			entry.ContextTokens = carrier.Context.Tokens
			entry.ContextWindow = carrier.Context.Window
			entry.ContextCounted = carrier.Context.Counted
		}
	}
	if err := DB.Create(&entry).Error; err != nil {
		common.SysError("pbr: write request log failed: " + err.Error())
		return
	}
	if carrier.Context != nil && carrier.Context.Status == usage.StatusOK {
		if err := SavePBRRequestContext(entry.Id, carrier.Context, carrier.ContextBodySize, entry.Ts); err != nil {
			common.SysError("pbr: write request context failed: " + err.Error())
		}
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

	// 用量归一与缓存/推理/首字汇总（usage-spec §6.3）。input_tokens 已按
	// usage_semantic 归一：Anthropic 的 prompt 不含缓存，需加上 cache_read/write；
	// OpenAI/Gemini 的 prompt 已含 cache_read。命中率由调用方计算。
	InputTokens      int64 `json:"input_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
	TTFTMsSum        int64 `json:"ttft_ms_sum"`
	TTFTSamples      int64 `json:"ttft_samples"`
}

// normalizedInputTokens is the denominator of the cache hit rate. Anthropic's
// input excludes cache tokens; OpenAI/Gemini's prompt includes cache reads.
func normalizedInputTokens(prompt, cacheRead, cacheWrite int, semantic string) int64 {
	if strings.EqualFold(strings.TrimSpace(semantic), "anthropic") {
		return int64(prompt + cacheRead + cacheWrite)
	}
	return int64(prompt)
}

func upsertPBRHourlyStat(entry *PBRRequestLog) {
	if entry.RouteSource == PBRRouteSourceTest {
		// 渠道/模型测试是诊断流量：写明细供排障，但不得污染小时聚合
		// （否则看板会出现"测试跑多了成功率变好/变差"的假信号）。
		return
	}
	bucket := (entry.Ts / 3600) * 3600
	successes := int64(0)
	if entry.Success {
		successes = 1
	}
	inputTokens := normalizedInputTokens(entry.PromptTokens, entry.CacheReadTokens, entry.CacheWriteTokens, entry.UsageSemantic)
	ttftSample := int64(0)
	if entry.TTFTMs > 0 {
		ttftSample = 1
	}
	dimensions := map[string]string{
		"lane":    entry.LaneName,
		"channel": entry.MemberChannelName,
		"key":     entry.TokenName,
		"client":  entry.Client,
	}
	// 「模型」维度的口径 = **上游真实模型名**（upstream_model，2026-10-25 起）：
	// 旧口径把 request_model（车道名）当"模型"聚合，池化车道下看不到真实模型分布，
	// 且与请求日志/渠道状态的模型列口径不一致。车道入口的分布由 lane 维度单独承担。
	// upstream_model 为空（管理动作等）时跳过该维度（与其它维度的空值跳过口径一致）。
	if strings.TrimSpace(entry.UpstreamModel) != "" {
		dimensions["model"] = entry.UpstreamModel
		// 「渠道 × 模型」「令牌 × 模型」要求两个成分都非空。
		if strings.TrimSpace(entry.MemberChannelName) != "" {
			dimensions["channel_model"] = entry.MemberChannelName + PBRChannelModelSeparator + entry.UpstreamModel
		}
		if strings.TrimSpace(entry.TokenName) != "" {
			dimensions["key_model"] = entry.TokenName + PBRChannelModelSeparator + entry.UpstreamModel
		}
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
			InputTokens:      inputTokens,
			CacheReadTokens:  int64(entry.CacheReadTokens),
			CacheWriteTokens: int64(entry.CacheWriteTokens),
			ReasoningTokens:  int64(entry.ReasoningTokens),
			TTFTMsSum:        entry.TTFTMs,
			TTFTSamples:      ttftSample,
		}
		if err := DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "bucket_ts"}, {Name: "group_kind"}, {Name: "group_key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"requests":           gorm.Expr("requests + ?", 1),
				"successes":          gorm.Expr("successes + ?", successes),
				"prompt_tokens":      gorm.Expr("prompt_tokens + ?", entry.PromptTokens),
				"completion_tokens":  gorm.Expr("completion_tokens + ?", entry.CompletionTokens),
				"cost_sum":           gorm.Expr("cost_sum + ?", entry.EstimatedCost),
				"input_tokens":       gorm.Expr("input_tokens + ?", inputTokens),
				"cache_read_tokens":  gorm.Expr("cache_read_tokens + ?", entry.CacheReadTokens),
				"cache_write_tokens": gorm.Expr("cache_write_tokens + ?", entry.CacheWriteTokens),
				"reasoning_tokens":   gorm.Expr("reasoning_tokens + ?", entry.ReasoningTokens),
				"ttft_ms_sum":        gorm.Expr("ttft_ms_sum + ?", entry.TTFTMs),
				"ttft_samples":       gorm.Expr("ttft_samples + ?", ttftSample),
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
	// PriceBand 命中的峰谷时段下标（-1 = 行级兜底价）。nil 表示调用方未提供，
	// 落库按兜底 -1 处理——不用 int 零值，避免"未提供"被当成"命中第 0 个 band"。
	PriceBand *int
}

// PBRRequestLogFilter 查询过滤条件。偏移分页（Page>0）与游标分页（BeforeId）
// 二选一：传 Page 走偏移（供控制台跳页/总数），传 BeforeId 走游标（向后兼容）。
type PBRRequestLogFilter struct {
	Lane       string
	Channel    string
	Key        string
	Model      string
	Client     string
	Session    string
	Success    *bool
	Masked     *bool
	Context    *bool
	MaskKind   string
	MaskBy     string
	MaskAction string
	// ErrorKind 精确过滤 error_kind（/api/logs?error_kind=…，空 = 不过滤）。
	ErrorKind string
	// RouteSource / ExcludeRouteSource 精确匹配/排除 route_source。
	// 渠道测试（route_source=test）是诊断流量，必须能被排除出
	// "最近失败"与小时聚合（api-spec §6.12）。
	RouteSource        string
	ExcludeRouteSource string
	Since              int64
	Until              int64
	BeforeId           int
	Limit              int
	Page               int
	PageSize           int
}

// PBRRouteSourceTest 渠道/模型测试产生的诊断请求（不进小时聚合与"最近失败"）。
const PBRRouteSourceTest = "test"

// applyPBRLogFilter 把过滤条件收口到一处，列表与计数共用，避免口径分叉。
// BeforeId 是游标分页专属条件，不在此处（Count 不应带它）。
func applyPBRLogFilter(query *gorm.DB, filter PBRRequestLogFilter) *gorm.DB {
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
	if filter.Client != "" {
		query = query.Where("client = ?", filter.Client)
	}
	if filter.Session != "" {
		query = query.Where("session_key = ? OR session_id = ?", filter.Session, filter.Session)
	}
	if filter.Success != nil {
		query = query.Where("success = ?", *filter.Success)
	}
	if filter.Masked != nil {
		if *filter.Masked {
			query = query.Where("masked_count > 0")
		} else {
			query = query.Where("masked_count = 0")
		}
	}
	if filter.Context != nil {
		if *filter.Context {
			query = query.Where("context_status <> '' OR context_tokens > 0")
		} else {
			query = query.Where("context_status = '' AND context_tokens = 0")
		}
	}
	if filter.MaskKind != "" {
		query = query.Where("mask_hits LIKE ?", "%"+filter.MaskKind+"%")
	}
	if filter.MaskBy != "" {
		query = query.Where("mask_hits LIKE ?", "%\"by\":\""+filter.MaskBy+"\"%")
	}
	if filter.MaskAction != "" {
		query = query.Where("mask_hits LIKE ?", "%\"action\":\""+filter.MaskAction+"\"%")
	}
	if filter.ErrorKind != "" {
		query = query.Where("error_kind = ?", filter.ErrorKind)
	}
	if filter.RouteSource != "" {
		query = query.Where("route_source = ?", filter.RouteSource)
	}
	if filter.ExcludeRouteSource != "" {
		query = query.Where("route_source <> ?", filter.ExcludeRouteSource)
	}
	if filter.Since > 0 {
		query = query.Where("ts >= ?", filter.Since)
	}
	if filter.Until > 0 {
		query = query.Where("ts <= ?", filter.Until)
	}
	return query
}

// ListPBRRequestLogs 按 id 倒序分页。偏移模式（Page>0）用 Offset/Limit 返回精确切片，
// total 由 CountPBRRequestLogs 单独查；游标模式（BeforeId）用 Limit+1 探测下一页，
// 由调用方解析 next_cursor。两种模式由是否传 Page 判定（api-spec §5.5）。
func ListPBRRequestLogs(filter PBRRequestLogFilter) ([]PBRRequestLog, error) {
	query := DB.Model(&PBRRequestLog{}).Order("id desc")
	query = applyPBRLogFilter(query, filter)
	if filter.Page > 0 {
		pageSize := filter.PageSize
		if pageSize <= 0 {
			pageSize = 50
		}
		offset := (filter.Page - 1) * pageSize
		if offset < 0 {
			offset = 0
		}
		query = query.Limit(pageSize).Offset(offset)
	} else {
		if filter.BeforeId > 0 {
			query = query.Where("id < ?", filter.BeforeId)
		}
		query = query.Limit(filter.Limit + 1)
	}
	var entries []PBRRequestLog
	if err := query.Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// CountPBRRequestLogs 按过滤条件统计总数（偏移分页的 total）。
func CountPBRRequestLogs(filter PBRRequestLogFilter) (int64, error) {
	var count int64
	query := DB.Model(&PBRRequestLog{})
	query = applyPBRLogFilter(query, filter)
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
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
// 上下文明细按 log_id 与明细同批清理（usage-spec §6.2）；两者在同一事务里完成，
// 避免"日志没了、上下文还在"的孤儿行。
func PrunePBRRequestLogsBefore(before int64) (int64, error) {
	var deleted int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("created_at < ?", before).Delete(&PBRRequestContext{}).Error; err != nil {
			return err
		}
		result := tx.Where("ts < ?", before).Delete(&PBRRequestLog{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
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
	InputTokens      int64   `json:"input_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	TTFTMsSum        int64   `json:"ttft_ms_sum"`
	TTFTSamples      int64   `json:"ttft_samples"`
	// ClientGroup/ClientName are set only for group_kind=client: libraries
	// and unknown "other" requests group as script; known clients keep their
	// display name. They are presentation fields, not storage keys.
	ClientGroup string `json:"client_group,omitempty"`
	ClientName  string `json:"client_name,omitempty"`
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
		"client":  "client",
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
		InputTokens      int64
		CacheReadTokens  int64
		CacheWriteTokens int64
		ReasoningTokens  int64
		TTFTMsSum        int64
		TTFTSamples      int64
	}
	var rows []row
	query := DB.Model(&PBRRequestLog{}).
		Select(bucketExpr + " AS bucket_ts, " + groupColumn + " AS group_key, COUNT(*) AS requests, " +
			"SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes, " +
			"SUM(prompt_tokens) AS prompt_tokens, SUM(completion_tokens) AS completion_tokens, " +
			"SUM(estimated_cost) AS estimated_cost, " +
			"SUM(prompt_tokens + CASE WHEN LOWER(COALESCE(usage_semantic, '')) = 'anthropic' " +
			"THEN cache_read_tokens + cache_write_tokens ELSE 0 END) AS input_tokens, " +
			"SUM(cache_read_tokens) AS cache_read_tokens, SUM(cache_write_tokens) AS cache_write_tokens, " +
			"SUM(reasoning_tokens) AS reasoning_tokens, " +
			"SUM(CASE WHEN ttft_ms > 0 THEN ttft_ms ELSE 0 END) AS ttft_ms_sum, " +
			"SUM(CASE WHEN ttft_ms > 0 THEN 1 ELSE 0 END) AS ttft_samples").
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
			InputTokens:      item.InputTokens,
			CacheReadTokens:  item.CacheReadTokens,
			CacheWriteTokens: item.CacheWriteTokens,
			ReasoningTokens:  item.ReasoningTokens,
			TTFTMsSum:        item.TTFTMsSum,
			TTFTSamples:      item.TTFTSamples,
		})
	}
	decorateClientBuckets(out, strings.ToLower(groupBy))
	return out, nil
}

// decorateClientBuckets adds the presentation fields the client dimension
// carries. They are not storage keys: the stored client stays AgentOf's
// result (a library keeps its own name).
func decorateClientBuckets(rows []PBRStatBucket, groupKind string) {
	if groupKind != "client" {
		return
	}
	for i := range rows {
		rows[i].ClientGroup = usage.ClientGroup(rows[i].GroupKey)
		rows[i].ClientName = usage.ClientName(rows[i].GroupKey)
	}
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
	return aggregatePBRStatsFromHourly(from, until, granularity, groupBy, "")
}

// AggregatePBRChannelStats 单渠道的小时/日聚合（渠道健康详情用，口径与 /api/stats 同源）。
// groupKey 为空表示不按 key 过滤（等价于 AggregatePBRStatsFromHourly）。
func AggregatePBRChannelStats(channelName string, from, until int64, granularity string) ([]PBRStatBucket, error) {
	return aggregatePBRStatsFromHourly(from, until, granularity, "channel", channelName)
}

// aggregatePBRStatsFromHourly 是聚合读的唯一实现：groupKind 选维度，
// groupKey 非空时再按该维度的键过滤（渠道健康只要一条渠道的桶）。
func aggregatePBRStatsFromHourly(from, until int64, granularity, groupBy, groupKey string) ([]PBRStatBucket, error) {
	groupKind := strings.ToLower(strings.TrimSpace(groupBy))
	switch groupKind {
	case "lane", "channel", "key", "model", "channel_model", "key_model", "client":
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
			"SUM(cost_sum) AS estimated_cost, "+
			"SUM(input_tokens) AS input_tokens, SUM(cache_read_tokens) AS cache_read_tokens, "+
			"SUM(cache_write_tokens) AS cache_write_tokens, SUM(reasoning_tokens) AS reasoning_tokens, "+
			"SUM(ttft_ms_sum) AS ttft_ms_sum, SUM(ttft_samples) AS ttft_samples").
		Where("group_kind = ?", groupKind).
		Group(bucketExpr + ", group_key").
		Order("bucket_ts desc")
	if strings.TrimSpace(groupKey) != "" {
		query = query.Where("group_key = ?", strings.TrimSpace(groupKey))
	}
	if from > 0 {
		// 严格相交：桶 [bucket_ts, bucket_ts+窗长) 与 [from, until) 有交集才算。
		// 用 `>` 而不是 `>=`——桶正好在 from 处**结束**时并不含窗口内的数据，
		// 放进来会让"把桶相加成窗口总数"的调用方多算一整个桶（注释与实现必须同义）。
		query = query.Where(bucketExpr+" + ? > ?", windowSeconds, from)
	}
	if until > 0 {
		// 同理：桶起点正好等于 until 的桶不含窗口内数据。
		query = query.Where("bucket_ts < ?", until)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []PBRStatBucket{}
	}
	decorateClientBuckets(rows, groupKind)
	return rows, nil
}

// ChannelLatencyStats 渠道在 [from, until) 内的延迟分位与平均首字耗时（毫秒）。
//
// 口径（api-spec §6.12，2026-10-25 起）：p50/p95 只统计**成功请求**的 total_ms
// （total_ms 是含重试链的端到端累计时长，均值会被失败/多尝试请求拉到失真）；
// 成功样本 <5 时分位为 0。数据源是**明细日志**（聚合表不存耗时——不为此引入
// 第二份耗时常量表，是刻意的取舍，api-spec §6.12 已明示）：`POST /logs/prune`
// 清理后这些值回落 0。ttft 只对流式请求有样本，无样本时返回 0。
func ChannelLatencyStats(channelName string, from, until int64) (p50Ms, p95Ms, avgTTFTMs float64, err error) {
	if DB == nil || strings.TrimSpace(channelName) == "" {
		return 0, 0, 0, nil
	}
	query := DB.Model(&PBRRequestLog{}).
		Select("total_ms").
		Where("member_channel_name = ? AND success = 1", strings.TrimSpace(channelName))
	if from > 0 {
		query = query.Where("ts >= ?", from)
	}
	if until > 0 {
		query = query.Where("ts < ?", until)
	}
	var totals []int64
	if err := query.Pluck("total_ms", &totals).Error; err != nil {
		return 0, 0, 0, err
	}
	if len(totals) >= 5 {
		sort.Slice(totals, func(i, j int) bool { return totals[i] < totals[j] })
		pick := func(frac float64) float64 {
			i := int(frac * float64(len(totals)))
			if i >= len(totals) {
				i = len(totals) - 1
			}
			return float64(totals[i])
		}
		p50Ms, p95Ms = pick(0.5), pick(0.95)
	}
	var row struct {
		AvgTTFT float64
	}
	ttftQuery := DB.Model(&PBRRequestLog{}).
		Select("COALESCE(AVG(CASE WHEN is_stream THEN ttft_ms END), 0) AS avg_ttft").
		Where("member_channel_name = ?", strings.TrimSpace(channelName))
	if from > 0 {
		ttftQuery = ttftQuery.Where("ts >= ?", from)
	}
	if until > 0 {
		ttftQuery = ttftQuery.Where("ts < ?", until)
	}
	if err := ttftQuery.Scan(&row).Error; err != nil {
		return p50Ms, p95Ms, 0, err
	}
	return p50Ms, p95Ms, row.AvgTTFT, nil
}
