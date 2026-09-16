package model

import (
	"strings"

	"pbr/common"

	"gorm.io/gorm"
)

// models.vendor_id 列的物理删除迁移。
//
// 背景（design-v1 §16#14）：供应商（Vendors）前后端已物理删除，模型不再归属
// 任何供应商；字段已从 Model 结构体与全部契约里删除，这里把库里残留的列一并
// 删掉，避免"结构体没有但库里有"的长期漂移。
//
// 幂等：列不存在直接返回；失败只记日志、不阻塞启动（与
// migrateDropChannelPriorityWeight / migrateOptionPrimaryKey 的容错口径一致）
// ——旧列残留不会影响任何行为，因为代码已不再读写它。
func migrateDropModelVendorID(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if !db.Migrator().HasTable(&Model{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&Model{}, "vendor_id") {
		return nil
	}
	if err := db.Migrator().DropColumn(&Model{}, "vendor_id"); err != nil {
		// SQLite 旧版本可能不支持 DROP COLUMN；此时保留列不影响运行（代码不再读写）。
		if strings.Contains(strings.ToLower(err.Error()), "drop column") {
			common.SysError("pbr: drop models.vendor_id skipped: " + err.Error())
			return nil
		}
		common.SysError("pbr: drop models.vendor_id failed: " + err.Error())
		return nil
	}
	common.SysLog("pbr: dropped legacy models.vendor_id column")
	return nil
}
