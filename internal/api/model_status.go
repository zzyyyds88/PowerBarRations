package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 模型状态（api-spec §6.13）：按**上游模型**（默认）或**对外路由键**聚合的只读
// 运行表现，供控制台首页「模型健康」与 Token 排行榜使用。
//
// 与渠道状态（§6.12）的区别：那里按"渠道 × 模型"看某个渠道，这里跨全部渠道
// 按模型看全局；口径完全一致（成功率、平均延迟、输出速度 tps）。
// 数据源同为明细日志（会被 POST /logs/prune 影响，已明示）。

// GetModelStatus GET /api/model-status?from=&to=
//
// from/to 为 Unix 秒或 RFC3339（可省略：默认最近 24 小时）。
func GetModelStatus(c *gin.Context) {
	from, err := parseTimeQuery(c.Query("from"))
	if err != nil {
		apierr.Validation(c, "from must be RFC3339 or unix seconds")
		return
	}
	to, err := parseTimeQuery(c.Query("to"))
	if err != nil {
		apierr.Validation(c, "to must be RFC3339 or unix seconds")
		return
	}
	if from <= 0 {
		from = time.Now().Unix() - 24*3600
	}
	if to <= 0 {
		to = time.Now().Unix() + 1
	}
	// group_by=lane 是控制台首页「模型健康」的口径：只显示对外路由键
	// （即用户请求里的 model），不泄露上游真名；默认仍按 upstream_model 聚合
	// （api-spec §6.13 的既有契约，渠道状态与 Token 排行榜继续用它）。
	groupBy := strings.ToLower(strings.TrimSpace(c.DefaultQuery("group_by", "model")))
	if groupBy != "model" && groupBy != "lane" {
		apierr.Validation(c, "group_by must be model or lane")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"from":     from,
		"to":       to,
		"group_by": groupBy,
		"items":    modelStatusRows(from, to, groupBy),
	})
}

// modelStatusRows 跨渠道按上游模型聚合（含输出速度）。
//
// 口径（api-spec §6.13，2026-10-25 起）：延迟 = **成功请求**的 p50/p95 分位
// （total_ms 是含重试链的端到端累计时长，均值会被失败/多尝试请求拉到失真——
// 实测 2 条样本的窗口因一条 3 连重试请求平均出 172s）；吞吐量 = **干净样本**
// （成功且 total_attempts=1）的 Σcompletion_tokens / Σ生成耗时，多次尝试的累计
// 时长含等待与失败往返，不代表生成速度。样本 <5 时分位数为 0（界面显示 `-`）。
//
// groupBy="lane" 时按 `lane_name`（对外路由键）而不是 `upstream_model` 聚合，
// 供首页「模型健康」只展示对外模型；其余字段口径完全相同。
func modelStatusRows(from, until int64, groupBy string) []gin.H {
	groupColumn := "upstream_model"
	if groupBy == "lane" {
		groupColumn = "lane_name"
	}
	rows := groupedMetricRows(from, until, groupColumn)
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{
			"model":        r.Key,
			"requests":     r.Requests,
			"successes":    r.Successes,
			"failures":     r.Requests - r.Successes,
			"success_rate": r.SuccessRate,
			"p50_total_ms": r.P50TotalMs,
			"p95_total_ms": r.P95TotalMs,
			"avg_ttft_ms":  r.AvgTTFTMs,
			"output_tps":   r.OutputTPS,
			"tps_samples":  r.TPSSamples,
			// 渠道/模型的整体输出速度：窗口内全部 completion_tokens / 窗口秒数。
			// 与 output_tps（干净样本的生成阶段速度）是不同口径。
			"output_tokens_per_second": r.OutputRate,
			// 生成阶段聚合速率：Σcompletion_tokens / Σ生成耗时（含并发合计）。
			"generation_tokens_per_second": r.GenerationRate,
			"tokens_in":                    r.TokensIn,
			"tokens_out":                   r.TokensOut,
			"tokens_total":                 r.TokensIn + r.TokensOut,
			"estimated_cost":               r.Cost,
		})
	}
	return out
}

// groupedMetricRow 是一组按任意渠道维度聚合后的运行指标；模型状态与渠道实时
// 速度共用这一份 SQL/口径，避免两处各写一遍分位与 tps 折算。
type groupedMetricRow struct {
	Key         string
	Requests    int64
	Successes   int64
	SuccessRate float64
	AvgTTFTMs   float64
	TokensIn    int64
	TokensOut   int64
	Cost        float64
	P50TotalMs  int64
	P95TotalMs  int64
	OutputTPS   float64
	TPSSamples  int64
	// OutputRate 是窗口内全部 completion_tokens / 窗口秒数，回答"这个渠道/模型
	// 整体每秒产出多少输出 token"；不按单请求生成耗时归一。
	OutputRate float64
	// GenerationRate 是窗口内**生成阶段**的聚合速率：
	// Σ completion_tokens / Σ(流式 total_ms-ttft_ms，非流式 total_ms)。
	// 它排除排队、首字等待和空闲间隙，等价于"多个流同时生成时合计每秒吐字"，
	// 操作员用秒表实测单请求得到的就是这个口径。
	GenerationRate float64
}

// groupedMetricRows 按 groupColumn 聚合明细日志；groupColumn 只能是代码内常量
// （upstream_model / lane_name / member_channel_name），不接收外部输入。
func groupedMetricRows(from, until int64, groupColumn string) []groupedMetricRow {
	if model.DB == nil {
		return []groupedMetricRow{}
	}
	nonEmpty := groupColumn + " <> ''"
	var rows []struct {
		Key       string
		Requests  int64
		Successes int64
		AvgTTFTMs float64
		TokensIn  int64
		TokensOut int64
		Cost      float64
		GenMs     float64
	}
	// GenMs 用**生成阶段**耗时（流式 total_ms-ttft_ms，非流式 total_ms），
	// 覆盖窗口内全部请求（不只干净样本），用于 GenerationRate 聚合。
	query := model.DB.Table("pbr_request_logs").
		Select(groupColumn + " AS key, COUNT(*) AS requests, " +
			"SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes, " +
			"COALESCE(AVG(CASE WHEN is_stream THEN ttft_ms END), 0) AS avg_ttft_ms, " +
			"COALESCE(SUM(prompt_tokens), 0) AS tokens_in, " +
			"COALESCE(SUM(completion_tokens), 0) AS tokens_out, " +
			"COALESCE(SUM(estimated_cost), 0) AS cost, " +
			"COALESCE(SUM(CASE WHEN is_stream THEN MAX(total_ms - ttft_ms, 1) ELSE total_ms END), 0) AS gen_ms").
		Where(nonEmpty).
		Group(groupColumn).
		Order("tokens_out desc")
	if from > 0 {
		query = query.Where("ts >= ?", from)
	}
	if until > 0 {
		query = query.Where("ts < ?", until)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return []groupedMetricRow{}
	}

	// 干净样本的 tps 聚合（成功且单次尝试）。
	type tpsRow struct {
		Key       string
		TokensOut int64
		GenMs     float64
		Samples   int64
	}
	var tpsRows []tpsRow
	tpsQuery := model.DB.Table("pbr_request_logs").
		Select(groupColumn + " AS key, COALESCE(SUM(completion_tokens), 0) AS tokens_out, " +
			"COALESCE(SUM(CASE WHEN is_stream THEN MAX(total_ms - ttft_ms, 1) ELSE total_ms END), 0) AS gen_ms, " +
			"COUNT(*) AS samples").
		Where(nonEmpty + " AND success = 1 AND total_attempts <= 1").
		Group(groupColumn)
	if from > 0 {
		tpsQuery = tpsQuery.Where("ts >= ?", from)
	}
	if until > 0 {
		tpsQuery = tpsQuery.Where("ts < ?", until)
	}
	tpsByKey := map[string]tpsRow{}
	if err := tpsQuery.Scan(&tpsRows).Error; err == nil {
		for _, r := range tpsRows {
			tpsByKey[strings.TrimSpace(r.Key)] = r
		}
	}

	// 成功请求的耗时样本（p50/p95 分位，内存计算；单用户窗口量级可行）。
	type sampleRow struct {
		Key     string
		TotalMs int64
	}
	var samples []sampleRow
	sampleQuery := model.DB.Table("pbr_request_logs").
		Select(groupColumn + " AS key, total_ms").
		Where(nonEmpty + " AND success = 1")
	if from > 0 {
		sampleQuery = sampleQuery.Where("ts >= ?", from)
	}
	if until > 0 {
		sampleQuery = sampleQuery.Where("ts < ?", until)
	}
	samplesByKey := map[string][]int64{}
	if err := sampleQuery.Scan(&samples).Error; err == nil {
		for _, s := range samples {
			key := strings.TrimSpace(s.Key)
			samplesByKey[key] = append(samplesByKey[key], s.TotalMs)
		}
	}

	out := make([]groupedMetricRow, 0, len(rows))
	windowSeconds := until - from
	for _, r := range rows {
		key := strings.TrimSpace(r.Key)
		row := groupedMetricRow{
			Key:       key,
			Requests:  r.Requests,
			Successes: r.Successes,
			AvgTTFTMs: r.AvgTTFTMs,
			TokensIn:  r.TokensIn,
			TokensOut: r.TokensOut,
			Cost:      r.Cost,
		}
		if r.Requests > 0 {
			row.SuccessRate = float64(r.Successes) / float64(r.Requests)
		}
		if t, ok := tpsByKey[key]; ok {
			row.TPSSamples = t.Samples
			if t.GenMs > 0 {
				row.OutputTPS = float64(t.TokensOut) * 1000 / t.GenMs
			}
		}
		row.P50TotalMs, row.P95TotalMs = percentileMs(samplesByKey[key])
		if windowSeconds > 0 {
			row.OutputRate = float64(r.TokensOut) / float64(windowSeconds)
		}
		if r.GenMs > 0 {
			row.GenerationRate = float64(r.TokensOut) * 1000 / r.GenMs
		}
		out = append(out, row)
	}
	return out
}

// percentileMs 就地排序后取中位与 95 分位（毫秒）。样本 <5 时两值为 0：
// 小样本的分位数没有代表性（实测曾因 2 条样本窗口的 3 连重试请求得出 288s 的
// "分位"），界面按 0 显示 `-`，不拿噪声冒充指标。
func percentileMs(samples []int64) (p50, p95 int64) {
	if len(samples) < 5 {
		return 0, 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	idx := func(frac float64) int {
		i := int(frac * float64(len(samples)))
		if i >= len(samples) {
			i = len(samples) - 1
		}
		return i
	}
	return samples[idx(0.5)], samples[idx(0.95)]
}
