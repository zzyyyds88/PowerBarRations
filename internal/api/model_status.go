package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 模型状态（api-spec §6.13）：按**上游模型**聚合的只读运行表现，供控制台首页
// 「服务状态 / Token 排行榜」两块卡片使用。
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
	c.JSON(http.StatusOK, gin.H{
		"from":  from,
		"to":    to,
		"items": modelStatusRows(from, to),
	})
}

// modelStatusRows 跨渠道按上游模型聚合（含输出速度）。
func modelStatusRows(from, until int64) []gin.H {
	if model.DB == nil {
		return []gin.H{}
	}
	var rows []struct {
		Model      string
		Requests   int64
		Successes  int64
		AvgTotalMs float64
		AvgTTFTMs  float64
		TokensIn   int64
		TokensOut  int64
		GenMs      float64
		Cost       float64
	}
	query := model.DB.Table("pbr_request_logs").
		Select("upstream_model AS model, COUNT(*) AS requests, " +
			"SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes, " +
			"COALESCE(AVG(total_ms), 0) AS avg_total_ms, " +
			"COALESCE(AVG(CASE WHEN is_stream THEN ttft_ms END), 0) AS avg_ttft_ms, " +
			"COALESCE(SUM(prompt_tokens), 0) AS tokens_in, " +
			"COALESCE(SUM(completion_tokens), 0) AS tokens_out, " +
			"COALESCE(SUM(CASE WHEN is_stream THEN MAX(total_ms - ttft_ms, 1) ELSE total_ms END), 0) AS gen_ms, " +
			"COALESCE(SUM(estimated_cost), 0) AS cost").
		Where("upstream_model <> ''").
		Group("upstream_model").
		Order("tokens_out desc")
	if from > 0 {
		query = query.Where("ts >= ?", from)
	}
	if until > 0 {
		query = query.Where("ts < ?", until)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return []gin.H{}
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		rate := 0.0
		if r.Requests > 0 {
			rate = float64(r.Successes) / float64(r.Requests)
		}
		tps := 0.0
		if r.GenMs > 0 {
			tps = float64(r.TokensOut) * 1000 / r.GenMs
		}
		out = append(out, gin.H{
			"model":          strings.TrimSpace(r.Model),
			"requests":       r.Requests,
			"successes":      r.Successes,
			"failures":       r.Requests - r.Successes,
			"success_rate":   rate,
			"avg_total_ms":   r.AvgTotalMs,
			"avg_ttft_ms":    r.AvgTTFTMs,
			"output_tps":     tps,
			"tokens_in":      r.TokensIn,
			"tokens_out":     r.TokensOut,
			"tokens_total":   r.TokensIn + r.TokensOut,
			"estimated_cost": r.Cost,
		})
	}
	return out
}
