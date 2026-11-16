package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 偏移分页（api-spec §5.5）：传 Page 走 Offset/Limit 返回精确切片，total 由
// CountPBRRequestLogs 单独查；游标模式（BeforeId）保留向后兼容，Count 不应带 BeforeId。
func TestListPBRRequestLogsOffsetAndCursor(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)

	// 种 25 条：success 交替，lane 固定。不假设 id 从 1 开始（整包跑时
	// sqlite_sequence/rowid 可能不重置），用读回的实际 id 做期望。
	createdIds := make([]int, 0, 25)
	for i := 0; i < 25; i++ {
		entry := &PBRRequestLog{
			Ts:                int64(1_700_000_000 + i),
			LaneName:          "lane-x",
			MemberChannelName: "ch-a",
			TokenName:         "key-x",
			RequestModel:      "model-x",
			Success:           i%2 == 0,
			PromptTokens:      i,
		}
		require.NoError(t, DB.Create(entry).Error)
		createdIds = append(createdIds, entry.Id)
	}
	// 期望顺序：id desc。page2 = 第 11-20 大的 id。
	descIds := make([]int, 0, 25)
	require.NoError(t, DB.Model(&PBRRequestLog{}).Order("id desc").Pluck("id", &descIds).Error)
	require.Len(t, descIds, 25)
	expectedPage2 := descIds[10:20]

	entries, err := ListPBRRequestLogs(PBRRequestLogFilter{Page: 2, PageSize: 10})
	require.NoError(t, err)
	assert.Len(t, entries, 10, "page2 应返回 10 条")
	assert.EqualValues(t, expectedPage2[0], entries[0].Id, "page2 首条应是降序第 11 个 id")
	assert.EqualValues(t, expectedPage2[9], entries[9].Id, "page2 末条应是降序第 20 个 id")

	// total 不受分页参数影响。
	total, err := CountPBRRequestLogs(PBRRequestLogFilter{Page: 2, PageSize: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 25, total)

	// 越界页返回空切片，total 仍准确。
	overEntries, err := ListPBRRequestLogs(PBRRequestLogFilter{Page: 10, PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, overEntries)

	// page_size 覆盖全部。
	allEntries, err := ListPBRRequestLogs(PBRRequestLogFilter{Page: 1, PageSize: 100})
	require.NoError(t, err)
	assert.Len(t, allEntries, 25)

	// 游标回归：不传 Page，用 BeforeId + Limit+1 探测下一页。BeforeId 取降序第 11 个 id，
	// 查 id<它 的最新 11 条（id<descIds[10] 共 14 条，Limit+1=11 探测到下一页存在），
	// 首条应是降序第 12 个 id。
	cursorAnchor := descIds[10]
	cursorEntries, err := ListPBRRequestLogs(PBRRequestLogFilter{BeforeId: cursorAnchor, Limit: 10})
	require.NoError(t, err)
	assert.Len(t, cursorEntries, 11, "游标模式 Limit+1 探测下一页")
	assert.EqualValues(t, descIds[11], cursorEntries[0].Id)
	// Count 不应带 BeforeId，否则 total 会随翻页缩小。
	cursorTotal, err := CountPBRRequestLogs(PBRRequestLogFilter{BeforeId: cursorAnchor})
	require.NoError(t, err)
	assert.EqualValues(t, 25, cursorTotal, "Count 不应受游标 BeforeId 影响")
}

// filter 条件在偏移与游标模式下口径一致（共用 applyPBRLogFilter）。
func TestCountPBRRequestLogsRespectsFilters(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)

	for i := 0; i < 10; i++ {
		entry := &PBRRequestLog{
			Ts:                int64(1_700_000_000 + i),
			LaneName:          "lane-a",
			MemberChannelName: "ch-a",
			TokenName:         "key-a",
			RequestModel:      "model-a",
			Success:           i < 3, // 3 成功 7 失败
		}
		require.NoError(t, DB.Create(entry).Error)
	}

	count, err := CountPBRRequestLogs(PBRRequestLogFilter{Lane: "lane-a"})
	require.NoError(t, err)
	assert.EqualValues(t, 10, count)

	success := true
	countSuccess, err := CountPBRRequestLogs(PBRRequestLogFilter{Success: &success})
	require.NoError(t, err)
	assert.EqualValues(t, 3, countSuccess)

	countEmpty, err := CountPBRRequestLogs(PBRRequestLogFilter{Lane: "nope"})
	require.NoError(t, err)
	assert.EqualValues(t, 0, countEmpty)
}
