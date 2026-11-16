package model

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// 统计重建（api-spec §5.5 `POST /api/stats/rebuild`）：口径变更后的历史回填。
//
// 做两件事：①按**当前**渠道单价表重折算全部明细的 estimated_cost 与 price_band
// （折算键 = member_model，历史行为空时回退 upstream_model——旧明细拿不到所选模型，
// 这是最近似值；峰谷按每条日志的 ts 在价格行 timezone 下重新匹配）；②从明细全量
// 重建小时聚合表（全部维度，含新的 model/channel_model/key_model 按 upstream_model
// 的口径）。幂等：重复执行结果一致。
//
// 它重写**派生数据**（聚合表与明细的折算列），不触碰明细的用量与请求元数据本身。

// StatsRebuildResult rebuild 的结果形状（dry_run 与实跑同形）。
type StatsRebuildResult struct {
	DryRun        bool    `json:"dry_run"`
	AggregateRows int64   `json:"aggregate_rows"`
	UpdatedLogs   int64   `json:"updated_logs"`
	CostSumBefore float64 `json:"cost_sum_before"`
	CostSumAfter  float64 `json:"cost_sum_after"`
}

// priceKeyForEntry 折算键：优先成员所选模型（member_model，新明细），
// 历史行为空时回退上游真名（upstream_model）。
func priceKeyForEntry(entry *PBRRequestLog) string {
	if key := strings.TrimSpace(entry.MemberModel); key != "" {
		return key
	}
	return strings.TrimSpace(entry.UpstreamModel)
}

// RebuildStatsFromLogs 从明细重建聚合表并重折算成本。
func RebuildStatsFromLogs(dryRun bool) (*StatsRebuildResult, error) {
	if DB == nil {
		return nil, errors.New("database not initialized")
	}
	result := &StatsRebuildResult{DryRun: dryRun}

	var before struct {
		Total float64
	}
	if err := DB.Model(&PBRRequestLog{}).
		Select("COALESCE(SUM(estimated_cost), 0) AS total").Scan(&before).Error; err != nil {
		return nil, err
	}
	result.CostSumBefore = before.Total

	// 聚合累加器：(bucket, kind, key) -> 计数。与 upsertPBRHourlyStat 同一口径，
	// 但在这里一次算完再整表重写（幂等），不依赖逐行 upsert。
	type aggCell struct {
		Requests         int64
		Successes        int64
		PromptTokens     int64
		CompletionTokens int64
		CostSum          float64
		InputTokens      int64
		CacheReadTokens  int64
		CacheWriteTokens int64
		ReasoningTokens  int64
		TTFTMsSum        int64
		TTFTSamples      int64
	}
	agg := map[string]*aggCell{}
	aggKey := func(bucket int64, kind, key string) string {
		return strconv.FormatInt(bucket, 10) + "\u0000" + kind + "\u0000" + key
	}

	const batchSize = 1000
	var lastID int
	costAfter := 0.0
	for {
		var entries []PBRRequestLog
		if err := DB.Where("id > ?", lastID).Order("id asc").Limit(batchSize).Find(&entries).Error; err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			break
		}
		updates := make([]struct {
			id   int
			cost float64
			band int
		}, 0, len(entries))
		for i := range entries {
			e := &entries[i]
			lastID = e.Id
			// 峰谷按**明细的 ts**（秒）在价格行 timezone 下重新匹配：rebuild 的
			// 输入只有落库时刻，这也是 api-spec §5.5 规定的口径。
			newCost, newBand := estimateRequestCost(e.MemberChannelId, priceKeyForEntry(e),
				time.Unix(e.Ts, 0),
				e.PromptTokens, e.CompletionTokens, e.CacheReadTokens, e.CacheWriteTokens,
				e.UsageSemantic)
			costAfter += newCost
			if newCost != e.EstimatedCost || newBand != e.PriceBand {
				result.UpdatedLogs++
				updates = append(updates, struct {
					id   int
					cost float64
					band int
				}{e.Id, newCost, newBand})
			}
			// 聚合（按重折算后的成本；与 upsertPBRHourlyStat 同维度口径）。
			bucket := (e.Ts / 3600) * 3600
			successes := int64(0)
			if e.Success {
				successes = 1
			}
			inputTokens := normalizedInputTokens(e.PromptTokens, e.CacheReadTokens, e.CacheWriteTokens, e.UsageSemantic)
			ttftSample := int64(0)
			if e.TTFTMs > 0 {
				ttftSample = 1
			}
			touch := func(kind, key string) {
				if strings.TrimSpace(key) == "" {
					return
				}
				cell := agg[aggKey(bucket, kind, key)]
				if cell == nil {
					cell = &aggCell{}
					agg[aggKey(bucket, kind, key)] = cell
				}
				cell.Requests++
				cell.Successes += successes
				cell.PromptTokens += int64(e.PromptTokens)
				cell.CompletionTokens += int64(e.CompletionTokens)
				cell.CostSum += newCost
				cell.InputTokens += inputTokens
				cell.CacheReadTokens += int64(e.CacheReadTokens)
				cell.CacheWriteTokens += int64(e.CacheWriteTokens)
				cell.ReasoningTokens += int64(e.ReasoningTokens)
				cell.TTFTMsSum += e.TTFTMs
				cell.TTFTSamples += ttftSample
			}
			touch("lane", e.LaneName)
			touch("channel", e.MemberChannelName)
			touch("key", e.TokenName)
			touch("client", e.Client)
			if strings.TrimSpace(e.UpstreamModel) != "" {
				touch("model", e.UpstreamModel)
				if strings.TrimSpace(e.MemberChannelName) != "" {
					touch("channel_model", e.MemberChannelName+PBRChannelModelSeparator+e.UpstreamModel)
				}
				if strings.TrimSpace(e.TokenName) != "" {
					touch("key_model", e.TokenName+PBRChannelModelSeparator+e.UpstreamModel)
				}
			}
		}
		if !dryRun && len(updates) > 0 {
			for _, u := range updates {
				if err := DB.Model(&PBRRequestLog{}).Where("id = ?", u.id).
					Updates(map[string]any{
						"estimated_cost": u.cost,
						"price_band":     u.band,
					}).Error; err != nil {
					return nil, err
				}
			}
		}
		if len(entries) < batchSize {
			break
		}
	}
	result.CostSumAfter = costAfter
	result.AggregateRows = int64(len(agg))
	if dryRun {
		return result, nil
	}

	// 整表重写聚合（派生数据；清空重建是最直接的幂等形态）。
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&PBRStatsHourly{}).Error; err != nil {
			return err
		}
		rows := make([]PBRStatsHourly, 0, len(agg))
		for key, cell := range agg {
			parts := strings.SplitN(key, "\u0000", 3)
			if len(parts) != 3 {
				continue
			}
			bucket, parseErr := strconv.ParseInt(parts[0], 10, 64)
			if parseErr != nil {
				continue
			}
			rows = append(rows, PBRStatsHourly{
				BucketTs:         bucket,
				GroupKind:        parts[1],
				GroupKey:         parts[2],
				Requests:         cell.Requests,
				Successes:        cell.Successes,
				PromptTokens:     cell.PromptTokens,
				CompletionTokens: cell.CompletionTokens,
				CostSum:          cell.CostSum,
				InputTokens:      cell.InputTokens,
				CacheReadTokens:  cell.CacheReadTokens,
				CacheWriteTokens: cell.CacheWriteTokens,
				ReasoningTokens:  cell.ReasoningTokens,
				TTFTMsSum:        cell.TTFTMsSum,
				TTFTSamples:      cell.TTFTSamples,
			})
		}
		if len(rows) > 0 {
			return tx.CreateInBatches(rows, 500).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
