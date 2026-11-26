package perfmetrics

import (
	"fmt"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/setting/perf_metrics_setting"
)

func flushLoop() {
	defer perfMetricsWG.Done()
	for {
		interval := perf_metrics_setting.GetFlushIntervalMinutes()
		timer := time.NewTimer(time.Duration(interval) * time.Minute)
		select {
		case <-perfMetricsStop:
			timer.Stop()
			return
		case <-timer.C:
		}
		setting := perf_metrics_setting.GetSetting()
		if !setting.Enabled {
			continue
		}
		flushCompletedBuckets()
		cleanupExpiredMetrics(setting.RetentionDays)
	}
}

func flushCompletedBuckets() {
	currentBucket := bucketStart(time.Now().Unix())
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.bucketTs >= currentBucket {
			return true
		}

		bucket := value.(*atomicBucket)
		drained := bucket.drain()
		if drained.requestCount == 0 {
			deleteOldEmptyBucket(k, key)
			return true
		}

		err := model.UpsertPerfMetric(&model.PerfMetric{
			ModelName:      k.model,
			Group:          k.group,
			BucketTs:       k.bucketTs,
			RequestCount:   drained.requestCount,
			SuccessCount:   drained.successCount,
			TotalLatencyMs: drained.totalLatencyMs,
			TtftSumMs:      drained.ttftSumMs,
			TtftCount:      drained.ttftCount,
			OutputTokens:   drained.outputTokens,
			GenerationMs:   drained.generationMs,
		})
		if err != nil {
			// 持续 DB 失败时不能无限保留失败桶：超过重试上界即丢弃并计数，
			//避免 hotBuckets 随模型/分组 cardinality 无界增长。
			if bucket.flushFailures.Add(1) >= 5 {
				hotBuckets.Delete(key)
				relayFlushDropped.Add(uint64(drained.requestCount))
				common.SysError(fmt.Sprintf("dropping perf metric bucket after repeated flush failures model=%s group=%s bucket=%d requests=%d", k.model, k.group, k.bucketTs, drained.requestCount))
				return true
			}
			bucket.addCounters(drained)
			common.SysError(fmt.Sprintf("failed to flush perf metric bucket model=%s group=%s bucket=%d: %s", k.model, k.group, k.bucketTs, err.Error()))
			return true
		}
		bucket.flushFailures.Store(0)

		deleteOldEmptyBucket(k, key)
		return true
	})
}

func deleteOldEmptyBucket(k bucketKey, rawKey any) {
	if k.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) {
		hotBuckets.Delete(rawKey)
	}
}

func cleanupExpiredMetrics(retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if err := model.DeletePerfMetricsBefore(cutoff); err != nil {
		common.SysError("failed to cleanup expired perf metrics: " + err.Error())
	}
}
