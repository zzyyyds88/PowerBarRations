package model

import (
	"strings"

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
// SQLite 拒绝。这里显式带 DEFAULT -1 加列并回填，然后**再改回无 default 的
// 结构**：否则 AutoMigrate 每次启动都会发现"库里有 default、结构体没有"，
// 触发整表重建。one-time 归一后，建表形态与结构体一致，后续启动 no-op。
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
		// 旧版本迁移可能留下了 DEFAULT -1；只做一次归一，之后不再重建表。
		hasDefault, err := columnHasDefault(db, "pbr_request_logs", "price_band")
		if err != nil {
			return err
		}
		if !hasDefault {
			return nil
		}
		if err := db.Migrator().AlterColumn(&PBRRequestLog{}, "price_band"); err != nil {
			common.SysError("pbr: normalize pbr_request_logs.price_band default failed: " + err.Error())
			return err
		}
		common.SysLog("pbr: normalized pbr_request_logs.price_band (removed one-time default -1)")
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
	// 加列时用 DEFAULT -1 只是为了满足 SQLite 对非空表加 NOT NULL 列的限制；
	// 随后归一为结构体的"无 default"形态，避免每次启动触发 AutoMigrate 重建。
	if err := db.Migrator().AlterColumn(&PBRRequestLog{}, "price_band"); err != nil {
		common.SysError("pbr: normalize pbr_request_logs.price_band default failed: " + err.Error())
		return err
	}
	common.SysLog("pbr: added pbr_request_logs.price_band (historical rows default -1)")
	return nil
}

// columnHasDefault 判断列是否有非空默认值（SQLite 的 ColumnTypes 返回文本形式）。
func columnHasDefault(db *gorm.DB, table, column string) (bool, error) {
	types, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return false, err
	}
	for _, ct := range types {
		if !strings.EqualFold(ct.Name(), column) {
			continue
		}
		value, ok := ct.DefaultValue()
		return ok && strings.TrimSpace(value) != "", nil
	}
	return false, nil
}
