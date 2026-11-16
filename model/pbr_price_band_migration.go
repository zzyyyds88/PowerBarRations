package model

import (
	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// pbr_request_logs.price_band 迁移（design-v1 §16.9#7，api-spec §4.4）。
//
// 语义：命中的峰谷时段下标；-1 = 未命中、使用行级兜底价。历史明细没有峰谷
// 信息，契约上等价于"旧单层价恒为 -1"，因此存量行必须落成 -1 而不是 NULL/0
// （0 会被误读成"命中第 0 个 band"）。
//
// **必须在 AutoMigrate 之前执行**：PBRRequestLog.PriceBand 是 NOT NULL 且刻意
// 不带 default 标签（GORM 会把零值 0 替换成默认值，让"命中第 0 个 band"被写成
// -1），GORM 生成的 `ADD COLUMN price_band integer NOT NULL` 在非空表上会被
// SQLite 拒绝。这里显式带 DEFAULT -1 加列，一次到位地满足存量与新增两侧。
func migratePBRRequestLogPriceBand(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&PBRRequestLog{}) {
		return nil
	}
	columns, err := tableColumns(db, "pbr_request_logs")
	if err != nil {
		common.SysError("pbr: read pbr_request_logs columns failed: " + err.Error())
		return err
	}
	if columns["price_band"] {
		return nil
	}
	if err := db.Exec("ALTER TABLE pbr_request_logs ADD COLUMN price_band integer NOT NULL DEFAULT -1").Error; err != nil {
		common.SysError("pbr: add pbr_request_logs.price_band failed: " + err.Error())
		return err
	}
	// 双保险：极端情况下（驱动忽略 DEFAULT）NULL 也要落成契约值 -1。
	if err := db.Exec("UPDATE pbr_request_logs SET price_band = -1 WHERE price_band IS NULL").Error; err != nil {
		common.SysError("pbr: backfill pbr_request_logs.price_band failed: " + err.Error())
		return err
	}
	common.SysLog("pbr: added pbr_request_logs.price_band (historical rows default -1)")
	return nil
}
