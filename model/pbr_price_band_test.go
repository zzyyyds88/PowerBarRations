package model

// 峰谷计价（design-v1 §16.9#7，api-spec §4.4/§5.5）：
//   - 按**请求开始时间**在价格行 timezone 下匹配第一条命中的 band；
//   - price_band 落库/回读（0 是合法下标，不能被 GORM 默认值吞掉）；
//   - stats/rebuild 按每条日志 ts 重匹配并重写 price_band 与 estimated_cost。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/setting/pricing_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// bandTestChannel 建一个带峰谷价格行的渠道：行级 input=1/output=2，每天早上
// 09:00–18:00 的 band 输入价 10（Asia/Shanghai）。
func bandTestChannel(t *testing.T) *Channel {
	t.Helper()
	channel := newTestChannel(t, "ch-band", "model-band")
	setting := `{"pbr_prices":[{"model":"model-band","input":1,"output":2,"timezone":"Asia/Shanghai",` +
		`"bands":[{"days":[1,2,3,4,5,6,7],"start":"09:00","end":"18:00","input":10}]}]}`
	channel.Setting = &setting
	require.NoError(t, DB.Save(channel).Error)
	InitChannelCache()
	return channel
}

func bandTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, location)
	require.NoError(t, err)
	return parsed
}

// 命中下标 0 必须原样落库：GORM 对带 default 标签的字段会用默认值替换零值，
// 一旦给 PriceBand 加了 default，第 0 条 band 会被写成 -1。
func TestWritePBRLogPriceBandRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)

	write := func(label string, band *int) PBRRequestLog {
		t.Helper()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		carrier := EnsurePBRLogCarrier(c)
		carrier.Lane = "lane-band"
		carrier.RequestModel = "lane-band"
		carrier.UpstreamModel = "model-band"
		carrier.MemberModel = "model-band-" + label
		carrier.InboundFormat = "openai"
		carrier.Success = true
		carrier.HTTPStatus = http.StatusOK
		WritePBRLogWithUsage(c, &PBRTokenUsage{PromptTokens: 1, PriceBand: band})

		var row PBRRequestLog
		require.NoError(t, DB.Where("member_model = ?", "model-band-"+label).First(&row).Error)
		return row
	}

	first := 0
	assert.Equal(t, 0, write("first", &first).PriceBand, "命中第 0 个 band 必须原样落库")

	third := 2
	assert.Equal(t, 2, write("third", &third).PriceBand)

	fallback := pricing_setting.FallbackPriceBand
	assert.Equal(t, -1, write("fallback", &fallback).PriceBand)

	// 调用方未提供峰谷信息 → 契约值 -1，不能落成零值 0（那是"命中第 0 个 band"）。
	assert.Equal(t, -1, write("nil", nil).PriceBand)
}

// 计价必须按**请求开始时间**匹配时段，而不是收尾时刻。
func TestRecordConsumeLogPricesAtRequestStartTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM options").Error)
	channel := bandTestChannel(t)

	run := func(at time.Time) *PBRTokenUsage {
		t.Helper()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		carrier := EnsurePBRLogCarrier(c)
		// 模拟入站捕获的请求开始时刻（EnsurePBRLogCarrier 默认取 now()，
		// 这里覆盖成可控时刻，验证计价读的是这个字段）。
		carrier.StartedAtMs = at.UnixMilli()
		carrier.Lane = "lane-band"
		carrier.RequestModel = "lane-band"
		carrier.UpstreamModel = "model-band"
		carrier.MemberModel = "model-band"
		carrier.ChannelId = channel.Id
		carrier.InboundFormat = "openai"
		carrier.Success = true
		carrier.HTTPStatus = http.StatusOK
		RecordConsumeLog(c, 0, RecordConsumeLogParams{
			ChannelId:     channel.Id,
			ModelName:     "model-band",
			PromptTokens:  1_000_000,
			UsageSemantic: "openai",
		})
		require.NotNil(t, carrier.Usage)
		WritePBRLog(c)
		return carrier.Usage
	}

	peak := run(bandTestTime(t, "2026-11-02 10:00")) // 周一 10:00，命中 band
	require.NotNil(t, peak.PriceBand)
	assert.Equal(t, 0, *peak.PriceBand)
	assert.InDelta(t, 10, peak.EstimatedCost, 1e-9, "命中 band 的输入价 10 元/M")

	offPeak := run(bandTestTime(t, "2026-11-02 20:00")) // 周一 20:00，兜底
	require.NotNil(t, offPeak.PriceBand)
	assert.Equal(t, pricing_setting.FallbackPriceBand, *offPeak.PriceBand)
	assert.InDelta(t, 1, offPeak.EstimatedCost, 1e-9, "未命中用行级兜底价 1 元/M")

	// 落库回读：时段信息随日志保留（导出/明细展示都读这一列）。
	var rows []PBRRequestLog
	require.NoError(t, DB.Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, 0, rows[0].PriceBand)
	assert.InDelta(t, 10, rows[0].EstimatedCost, 1e-9)
	assert.Equal(t, -1, rows[1].PriceBand)
	assert.InDelta(t, 1, rows[1].EstimatedCost, 1e-9)
}

// stats/rebuild：按每条日志 ts 在价格行 timezone 下重匹配 band，重写
// price_band 与 estimated_cost；dry-run 不落库；重复执行幂等。
func TestRebuildStatsRematchesPriceBand(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM options").Error)
	channel := bandTestChannel(t)

	// 两条按旧口径落下的明细：price_band/cost 都与当前价格表不符。
	peakEntry := &PBRRequestLog{
		Ts: bandTestTime(t, "2026-11-02 10:00").Unix(), LaneName: "lane-band",
		MemberChannelId: channel.Id, MemberChannelName: "ch-band",
		RequestModel: "lane-band", UpstreamModel: "model-band", MemberModel: "model-band",
		TokenName: "key-band", Success: true, PromptTokens: 1_000_000,
		EstimatedCost: 1, PriceBand: pricing_setting.FallbackPriceBand,
	}
	offPeakEntry := &PBRRequestLog{
		Ts: bandTestTime(t, "2026-11-02 20:00").Unix(), LaneName: "lane-band",
		MemberChannelId: channel.Id, MemberChannelName: "ch-band",
		RequestModel: "lane-band", UpstreamModel: "model-band", MemberModel: "model-band",
		TokenName: "key-band", Success: true, PromptTokens: 1_000_000,
		EstimatedCost: 10, PriceBand: 0,
	}
	require.NoError(t, DB.Create(peakEntry).Error)
	require.NoError(t, DB.Create(offPeakEntry).Error)

	preview, err := RebuildStatsFromLogs(true)
	require.NoError(t, err)
	assert.True(t, preview.DryRun)
	assert.EqualValues(t, 2, preview.UpdatedLogs, "两条明细都需要重算")
	assert.InDelta(t, 11, preview.CostSumAfter, 1e-9)

	var untouched PBRRequestLog
	require.NoError(t, DB.First(&untouched, peakEntry.Id).Error)
	assert.Equal(t, pricing_setting.FallbackPriceBand, untouched.PriceBand, "dry-run 不得改动明细")
	assert.InDelta(t, 1, untouched.EstimatedCost, 1e-9)

	result, err := RebuildStatsFromLogs(false)
	require.NoError(t, err)
	assert.InDelta(t, 11, result.CostSumAfter, 1e-9)

	var peakRow PBRRequestLog
	require.NoError(t, DB.First(&peakRow, peakEntry.Id).Error)
	assert.Equal(t, 0, peakRow.PriceBand, "按 ts 重新命中 band 0")
	assert.InDelta(t, 10, peakRow.EstimatedCost, 1e-9)

	var offPeakRow PBRRequestLog
	require.NoError(t, DB.First(&offPeakRow, offPeakEntry.Id).Error)
	assert.Equal(t, pricing_setting.FallbackPriceBand, offPeakRow.PriceBand, "未命中回落兜底")
	assert.InDelta(t, 1, offPeakRow.EstimatedCost, 1e-9)

	// 幂等：再跑一遍没有明细需要更新，成本合计不变。
	again, err := RebuildStatsFromLogs(false)
	require.NoError(t, err)
	assert.EqualValues(t, 0, again.UpdatedLogs)
	assert.InDelta(t, result.CostSumAfter, again.CostSumAfter, 1e-9)
}

// 存量库补列：历史行没有峰谷信息，必须回填契约值 -1（不能是 0 或 NULL）。
func TestMigratePBRRequestLogPriceBandBackfillsFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "price-band-migration.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE pbr_request_logs (id integer PRIMARY KEY AUTOINCREMENT, ts integer)").Error)
	require.NoError(t, db.Exec("INSERT INTO pbr_request_logs (ts) VALUES (123)").Error)

	require.NoError(t, migratePBRRequestLogPriceBand(db))
	require.True(t, db.Migrator().HasColumn(&PBRRequestLog{}, "price_band"))

	var band int
	require.NoError(t, db.Raw("SELECT price_band FROM pbr_request_logs WHERE id = 1").Scan(&band).Error)
	assert.Equal(t, pricing_setting.FallbackPriceBand, band, "历史明细等价于旧单层价（恒 -1）")

	// 一次性 DEFAULT -1 必须被归一掉，否则 AutoMigrate 每次启动都会重建表。
	var ddl string
	require.NoError(t, db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='pbr_request_logs'").Scan(&ddl).Error)
	assert.NotContains(t, ddl, "DEFAULT -1", "迁移后列不得保留 default，结构体是无 default 形态")

	// 幂等：第二次执行不改列、不改值。
	require.NoError(t, migratePBRRequestLogPriceBand(db))
	require.NoError(t, db.Raw("SELECT price_band FROM pbr_request_logs WHERE id = 1").Scan(&band).Error)
	assert.Equal(t, pricing_setting.FallbackPriceBand, band)
	var ddlAgain string
	require.NoError(t, db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='pbr_request_logs'").Scan(&ddlAgain).Error)
	assert.Equal(t, ddl, ddlAgain, "重复迁移不得再次重建表")

	// 升级路径闭环：随后按当前结构 AutoMigrate，存量值不得被改动，新行能写入
	// band 0（证明"NOT NULL 无 default"的列语义与写入路径一致）。
	require.NoError(t, db.AutoMigrate(&PBRRequestLog{}))
	require.NoError(t, db.Raw("SELECT price_band FROM pbr_request_logs WHERE id = 1").Scan(&band).Error)
	assert.Equal(t, pricing_setting.FallbackPriceBand, band, "AutoMigrate 不得改动存量兜底值")
	zero := &PBRRequestLog{Ts: 456, PriceBand: 0}
	require.NoError(t, db.Create(zero).Error)
	var reRead PBRRequestLog
	require.NoError(t, db.First(&reRead, zero.Id).Error)
	assert.Equal(t, 0, reRead.PriceBand, "命中第 0 个 band 必须原样落库")

	// 全新库没有表：交给 AutoMigrate 建表，迁移函数不报错。
	fresh, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fresh.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, migratePBRRequestLogPriceBand(fresh))
}
