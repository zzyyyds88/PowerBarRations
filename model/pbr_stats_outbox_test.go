package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zzyyyds88/PowerBarRations/internal/usage"
)

func newPBRLogWriteContext(t *testing.T, lane, modelName string) (*gin.Context, *PBRLogCarrier) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	carrier := EnsurePBRLogCarrier(c)
	carrier.Lane = lane
	carrier.RequestModel = modelName
	carrier.MemberModel = modelName
	carrier.UpstreamModel = modelName
	carrier.ChannelName = "channel-" + lane
	carrier.InboundFormat = "openai"
	carrier.Success = true
	carrier.HTTPStatus = http.StatusOK
	return c, carrier
}

// 明细、上下文、聚合 outbox 必须在一个事务里落库；聚合由 worker 异步补齐。
func TestWritePBRLogWritesDetailContextAndOutboxAtomically(t *testing.T) {
	truncateTables(t)
	c, carrier := newPBRLogWriteContext(t, "lane-outbox", "model-outbox")
	carrier.Context = &usage.Context{
		Status: usage.StatusOK,
		Parts:  []usage.Part{{Kind: "system", Tokens: 3}},
	}
	carrier.ContextBodySize = 128

	WritePBRLogWithUsage(c, &PBRTokenUsage{PromptTokens: 10, CompletionTokens: 5})

	var logRow PBRRequestLog
	require.NoError(t, DB.Where("lane_name = ?", "lane-outbox").First(&logRow).Error)
	var contextRow PBRRequestContext
	require.NoError(t, DB.Where("log_id = ?", logRow.Id).First(&contextRow).Error)
	assert.Equal(t, 128, contextRow.BodyBytes)

	var outbox []PBRStatsOutbox
	require.NoError(t, DB.Find(&outbox).Error)
	require.Len(t, outbox, 1, "明细事务必须写入且只写入一行聚合 outbox")

	var aggregateCount int64
	require.NoError(t, DB.Model(&PBRStatsHourly{}).Count(&aggregateCount).Error)
	assert.Zero(t, aggregateCount, "聚合必须异步写入，不在请求事务内同步扣一次")

	require.NoError(t, FlushPBRStatsOutbox(context.Background()))
	var stat PBRStatsHourly
	require.NoError(t, DB.Where("group_kind = ? AND group_key = ?", "lane", "lane-outbox").First(&stat).Error)
	assert.EqualValues(t, 1, stat.Requests)
	assert.EqualValues(t, 10, stat.PromptTokens)
	outbox = nil
	require.NoError(t, DB.Find(&outbox).Error)
	assert.Empty(t, outbox, "worker 处理成功后必须删除 outbox 行")
}

// 重建期间的新请求（id 高于水位）必须保留其 outbox，由 worker 增量补算。
func TestRebuildStatsKeepsOutboxAboveWatermark(t *testing.T) {
	truncateTables(t)
	detail := &PBRRequestLog{
		Ts: 1_700_000_000, LaneName: "lane-watermark", MemberChannelName: "ch",
		UpstreamModel: "m", Success: true,
	}
	require.NoError(t, DB.Create(detail).Error)
	require.NoError(t, DB.Create(&PBRStatsOutbox{
		LogID: int64(detail.Id), Payload: "covered", CreatedAt: 1,
	}).Error)
	require.NoError(t, DB.Create(&PBRStatsOutbox{
		LogID: int64(detail.Id) + 1000, Payload: "newer", CreatedAt: 2,
	}).Error)

	_, err := RebuildStatsFromLogs(false)
	require.NoError(t, err)

	var rows []PBRStatsOutbox
	require.NoError(t, DB.Order("id").Find(&rows).Error)
	require.Len(t, rows, 1, "水位以内的 outbox 被重建覆盖，水位以上的必须保留")
	assert.Greater(t, rows[0].LogID, int64(detail.Id))
	assert.Equal(t, "newer", rows[0].Payload)
}

// rebuild 以明细为唯一真源：死信不得阻断重建，成功后 outbox（含死信）清空。
func TestRebuildStatsIgnoresDeadOutboxAndClearsIt(t *testing.T) {
	truncateTables(t)
	detail := &PBRRequestLog{
		Ts: 1_700_000_000, LaneName: "lane-dead-outbox", MemberChannelName: "ch",
		UpstreamModel: "m", Success: true, PromptTokens: 10,
	}
	require.NoError(t, DB.Create(detail).Error)
	require.NoError(t, DB.Create(&PBRStatsOutbox{
		Payload: "not-json", CreatedAt: 1, Attempts: pbrStatsOutboxMaxAttempts, LastError: "decode",
	}).Error)

	// dry-run 不得改动 outbox。
	preview, err := RebuildStatsFromLogs(true)
	require.NoError(t, err)
	assert.True(t, preview.DryRun)
	var outboxCount int64
	require.NoError(t, DB.Model(&PBRStatsOutbox{}).Count(&outboxCount).Error)
	assert.EqualValues(t, 1, outboxCount, "dry-run 不得清空 outbox")

	result, err := RebuildStatsFromLogs(false)
	require.NoError(t, err, "死信不得阻断 rebuild")
	assert.Greater(t, result.AggregateRows, int64(0))
	var stat PBRStatsHourly
	require.NoError(t, DB.Where("group_kind = ? AND group_key = ?", "lane", "lane-dead-outbox").First(&stat).Error)
	assert.EqualValues(t, 1, stat.Requests)

	require.NoError(t, DB.Model(&PBRStatsOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount, "重建成功后必须清空全部 outbox 行")
}

// 同一请求重复调用 WritePBRLog 不得写两行（Written 闸门在事务失败后允许重试）。
func TestWritePBRLogIsIdempotentPerRequest(t *testing.T) {
	truncateTables(t)
	c, _ := newPBRLogWriteContext(t, "lane-once", "model-once")
	WritePBRLogWithUsage(c, nil)
	WritePBRLogWithUsage(c, nil)

	var count int64
	require.NoError(t, DB.Model(&PBRRequestLog{}).Where("lane_name = ?", "lane-once").Count(&count).Error)
	assert.EqualValues(t, 1, count)
	var outboxCount int64
	require.NoError(t, DB.Model(&PBRStatsOutbox{}).Count(&outboxCount).Error)
	assert.EqualValues(t, 1, outboxCount)
}

// 无法解析的 outbox 行重试到上限后成为死信，不再被自动处理，但必须可见。
func TestPBRStatsOutboxDeadLettersAreVisible(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&PBRStatsOutbox{
		Payload:       "not-json",
		CreatedAt:     1,
		Attempts:      pbrStatsOutboxMaxAttempts - 1,
		NextAttemptAt: 0,
	}).Error)

	_, _ = drainPBRStatsOutbox(context.Background())

	var row PBRStatsOutbox
	require.NoError(t, DB.First(&row).Error)
	assert.Equal(t, pbrStatsOutboxMaxAttempts, row.Attempts)
	assert.NotEmpty(t, row.LastError)
	pending, dead, err := PBRStatsOutboxBacklog()
	require.NoError(t, err)
	assert.Zero(t, pending)
	assert.EqualValues(t, 1, dead)
}

// rebuild 必须保留早于明细覆盖范围的聚合（那些桶已无明细可重算）。
func TestRebuildPreservesAggregatesBeforeDetailCoverage(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&PBRStatsHourly{
		BucketTs: 1000, GroupKind: "lane", GroupKey: "old-lane", Requests: 5, Successes: 5,
	}).Error)
	detail := &PBRRequestLog{
		Ts: 7200 + 10, LaneName: "new-lane", MemberChannelName: "new-channel",
		UpstreamModel: "new-model", Success: true,
	}
	require.NoError(t, DB.Create(detail).Error)

	result, err := RebuildStatsFromLogs(false)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.PreservedBuckets, int64(1))

	var old PBRStatsHourly
	require.NoError(t, DB.Where("bucket_ts = ? AND group_kind = ? AND group_key = ?", 1000, "lane", "old-lane").First(&old).Error)
	assert.EqualValues(t, 5, old.Requests, "prune 前的聚合不得被清空重建")

	var current PBRStatsHourly
	require.NoError(t, DB.Where("bucket_ts = ? AND group_kind = ? AND group_key = ?", 7200, "lane", "new-lane").First(&current).Error)
	assert.EqualValues(t, 1, current.Requests)
}

// 边界桶若含已 prune 明细（既有聚合请求数 > 剩余明细数），整桶保留；
// 否则可以安全重算（价格变更能作用到这一小时）。
func TestRebuildPreservesOnlyBoundaryBucketWithPrunedRows(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&PBRStatsHourly{
		BucketTs: 7200, GroupKind: "lane", GroupKey: "pruned-lane", Requests: 2, Successes: 2,
	}).Error)
	require.NoError(t, DB.Create(&PBRRequestLog{
		Ts: 7250, LaneName: "remaining-lane", MemberChannelName: "ch", UpstreamModel: "m", Success: true,
	}).Error)

	result, err := RebuildStatsFromLogs(false)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.PreservedBuckets, int64(1))

	var preserved PBRStatsHourly
	require.NoError(t, DB.Where("bucket_ts = ? AND group_kind = ? AND group_key = ?", 7200, "lane", "pruned-lane").First(&preserved).Error)
	assert.EqualValues(t, 2, preserved.Requests)
	var rebuilt PBRStatsHourly
	err = DB.Where("bucket_ts = ? AND group_kind = ? AND group_key = ?", 7200, "lane", "remaining-lane").First(&rebuilt).Error
	assert.Error(t, err, "被保留的边界桶不得混入重算行，否则会重复计数")
}
