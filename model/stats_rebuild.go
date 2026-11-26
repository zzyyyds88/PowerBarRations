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
	DryRun           bool    `json:"dry_run"`
	AggregateRows    int64   `json:"aggregate_rows"`
	UpdatedLogs      int64   `json:"updated_logs"`
	CostSumBefore    float64 `json:"cost_sum_before"`
	CostSumAfter     float64 `json:"cost_sum_after"`
	PreservedBuckets int64   `json:"preserved_buckets"`
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

	// rebuild 与 outbox worker 互斥：先暂停增量 worker，再从明细全量重建。
	// outbox 只是聚合的加速缓存；rebuild 成功后在同一事务里清空水位以内的
	// 行（含死信），因此死信/积压不会阻止权威重建。
	pbrStatsOutboxMu.Lock()
	defer pbrStatsOutboxMu.Unlock()

	// 请求写入（明细+outbox）不持有 pbrStatsOutboxMu，重建期间仍会继续落库。
	// 记录开始时的最大明细 id 作为水位：只重算 <= 水位的明细，最终也只清
	// <= 水位的 outbox；水位之后的新请求由 worker 增量补算，避免“聚合漏计但
	// outbox 被清掉”的永久缺口。
	var maxRow struct{ MaxID int64 }
	if err := DB.Model(&PBRRequestLog{}).
		Select("COALESCE(MAX(id), 0) AS max_id").Scan(&maxRow).Error; err != nil {
		return nil, err
	}
	maxLogID := maxRow.MaxID

	// 明细已被保留策略 prune 时，prune 边界之前的聚合没有明细来源，必须保留。
	// 边界小时桶（min(ts) 所在小时）也整体保留：prune 只删旧明细，边界桶里
	// 被删掉的请求已经计入既有聚合，重算剩余明细会把它们抹掉。
	var minRow struct{ MinTs int64 }
	if err := DB.Model(&PBRRequestLog{}).
		Select("COALESCE(MIN(ts), 0) AS min_ts").Scan(&minRow).Error; err != nil {
		return nil, err
	}
	preserveAll := minRow.MinTs <= 0
	preserveThrough := int64(-1)
	preservedBuckets := map[int64]bool{}
	deleteFrom := int64(0)
	if !preserveAll {
		preserveThrough = (minRow.MinTs / 3600) * 3600
		// 早于边界小时的桶已经没有明细来源，必须整桶保留。
		var earlier []int64
		if err := DB.Model(&PBRStatsHourly{}).Where("bucket_ts < ?", preserveThrough).
			Distinct("bucket_ts").Pluck("bucket_ts", &earlier).Error; err != nil {
			return nil, err
		}
		for _, bucket := range earlier {
			preservedBuckets[bucket] = true
		}
		// 边界桶可能同时含已 prune 与仍保留的明细。若既有聚合的请求数大于
		// 仍保留的明细数，说明桶里有已 prune 的行，整桶保留（重算会抹掉它们）；
		// 否则可以安全重算，价格变更也能作用到这一小时。
		hasPruned, err := boundaryBucketHasPrunedRows(preserveThrough)
		if err != nil {
			return nil, err
		}
		if hasPruned {
			preservedBuckets[preserveThrough] = true
			deleteFrom = preserveThrough + 3600
		} else {
			deleteFrom = preserveThrough
		}
	} else {
		var buckets []int64
		if err := DB.Model(&PBRStatsHourly{}).Distinct("bucket_ts").Pluck("bucket_ts", &buckets).Error; err != nil {
			return nil, err
		}
		for _, bucket := range buckets {
			preservedBuckets[bucket] = true
		}
	}
	result.PreservedBuckets = int64(len(preservedBuckets))

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
		if err := DB.Where("id > ? AND id <= ?", lastID, maxLogID).Order("id asc").Limit(batchSize).Find(&entries).Error; err != nil {
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
			// 诊断流量（渠道/模型测试）只重算成本，不进小时聚合——与
			// upsertPBRHourlyStat 同口径（api-spec §6.12）。
			if e.RouteSource == PBRRouteSourceTest {
				continue
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
	if dryRun {
		for key := range agg {
			parts := strings.SplitN(key, "\u0000", 3)
			if len(parts) != 3 {
				continue
			}
			bucket, parseErr := strconv.ParseInt(parts[0], 10, 64)
			if parseErr != nil {
				continue
			}
			if preserveAll || preservedBuckets[bucket] {
				continue
			}
			result.AggregateRows++
		}
		return result, nil
	}

	// 重写聚合（派生数据）：只覆盖仍有明细来源的小时桶；prune 边界及更早的
	// 聚合没有明细可重算，原样保留（BucketTs <= preserveThrough 的行不插入）。
	err := DB.Transaction(func(tx *gorm.DB) error {
		if !preserveAll {
			if err := tx.Where("bucket_ts >= ?", deleteFrom).Delete(&PBRStatsHourly{}).Error; err != nil {
				return err
			}
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
			if preserveAll || preservedBuckets[bucket] {
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
		result.AggregateRows = int64(len(rows))
		if len(rows) > 0 {
			if err := tx.CreateInBatches(rows, 500).Error; err != nil {
				return err
			}
		}
		// 全量重建已覆盖水位以内的所有明细；只清这部分 outbox（含历史
		// log_id=0 的旧行）。水位之后的新请求保留，由 worker 增量补算。
		return tx.Where("log_id = 0 OR log_id <= ?", maxLogID).Delete(&PBRStatsOutbox{}).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// boundaryBucketHasPrunedRows 判断边界小时桶的既有聚合是否包含已 prune 的明细。
//
// 每个参与聚合的日志会在多个维度各加 1；取任一维度的请求数都不可能超过
// 仍未 prune 的明细数。若既有聚合的最大维度请求数大于剩余明细数，说明桶里
// 有已删除明细贡献的计数，重算会把它们抹掉。
func boundaryBucketHasPrunedRows(bucket int64) (bool, error) {
	var rows []struct {
		GroupKind string
		Requests  int64
	}
	if err := DB.Model(&PBRStatsHourly{}).
		Select("group_kind, COALESCE(SUM(requests), 0) AS requests").
		Where("bucket_ts = ?", bucket).Group("group_kind").Scan(&rows).Error; err != nil {
		return false, err
	}
	var maxRequests int64
	for _, row := range rows {
		if row.Requests > maxRequests {
			maxRequests = row.Requests
		}
	}
	var remaining int64
	if err := DB.Model(&PBRRequestLog{}).
		Where("ts >= ? AND ts < ? AND COALESCE(route_source, '') <> ?",
			bucket, bucket+3600, PBRRouteSourceTest).
		Count(&remaining).Error; err != nil {
		return false, err
	}
	return maxRequests > remaining, nil
}
