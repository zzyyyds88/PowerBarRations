package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

const channelSpeedDefaultWindowSeconds = 15 * 60

// channelSpeedItem 是渠道实时速度的一行；字段口径与 §6.13 的模型状态逐项一致，
// 只是分组键换成 member_channel_name。
type channelSpeedItem struct {
	Channel       string  `json:"channel"`
	Enabled       bool    `json:"enabled"`
	Requests      int64   `json:"requests"`
	Successes     int64   `json:"successes"`
	Failures      int64   `json:"failures"`
	SuccessRate   float64 `json:"success_rate"`
	P50TotalMs    int64   `json:"p50_total_ms"`
	P95TotalMs    int64   `json:"p95_total_ms"`
	AvgTTFTMs     float64 `json:"avg_ttft_ms"`
	OutputTPS     float64 `json:"output_tps"`
	TPSSamples    int64   `json:"tps_samples"`
	// OutputRate 是窗口内全部 completion_tokens / 窗口秒数（渠道整体输出速度）。
	OutputRate float64 `json:"output_tokens_per_second"`
	// GenerationRate 是窗口内 Σcompletion_tokens / Σ生成耗时（并发合计的吐字速度）。
	GenerationRate float64 `json:"generation_tokens_per_second"`
	TokensIn      int64   `json:"tokens_in"`
	TokensOut     int64   `json:"tokens_out"`
	TokensTotal   int64   `json:"tokens_total"`
	EstimatedCost float64 `json:"estimated_cost"`
}

// GetChannelSpeed GET /api/channel-speed?from=&to=（api-spec §6.14）
//
// 供控制台「实时仪表盘」在短窗内观察各渠道的输出速度。返回**全部配置渠道**：
// 窗口内没有请求的渠道以零值出现，由界面显示 `—`，避免拿 0 冒充「速度为 0」。
func GetChannelSpeed(c *gin.Context) {
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
	now := time.Now().Unix()
	if from <= 0 {
		from = now - channelSpeedDefaultWindowSeconds
	}
	if to <= 0 {
		to = now + 1
	}
	items, err := channelSpeedRows(from, to)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"from": from, "to": to, "items": items})
}

// channelSpeedRows 把按渠道聚合的明细日志与渠道配置表合并，并按速度降序。
//
// 只展示当前配置的渠道；已删除渠道的历史日志不进入仪表盘（配置面已不存在，
// 继续展示只会制造"幽灵渠道"）。
func channelSpeedRows(from, until int64) ([]channelSpeedItem, error) {
	if model.DB == nil {
		return []channelSpeedItem{}, nil
	}
	metrics := groupedMetricRows(from, until, "member_channel_name")
	byChannel := make(map[string]groupedMetricRow, len(metrics))
	for _, metric := range metrics {
		byChannel[metric.Key] = metric
	}

	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return nil, err
	}
	items := make([]channelSpeedItem, 0, len(channels))
	for _, ch := range channels {
		metric := byChannel[ch.Name]
		items = append(items, channelSpeedItem{
			Channel:       ch.Name,
			Enabled:       ch.Status == common.ChannelStatusEnabled,
			Requests:      metric.Requests,
			Successes:     metric.Successes,
			Failures:      metric.Requests - metric.Successes,
			SuccessRate:   metric.SuccessRate,
			P50TotalMs:    metric.P50TotalMs,
			P95TotalMs:    metric.P95TotalMs,
			AvgTTFTMs:     metric.AvgTTFTMs,
			OutputTPS:     metric.OutputTPS,
			TPSSamples:    metric.TPSSamples,
			OutputRate:     metric.OutputRate,
			GenerationRate: metric.GenerationRate,
			TokensIn:      metric.TokensIn,
			TokensOut:     metric.TokensOut,
			TokensTotal:   metric.TokensIn + metric.TokensOut,
			EstimatedCost: metric.Cost,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].OutputRate != items[j].OutputRate {
			return items[i].OutputRate > items[j].OutputRate
		}
		if items[i].Requests != items[j].Requests {
			return items[i].Requests > items[j].Requests
		}
		return items[i].Channel < items[j].Channel
	})
	return items, nil
}
