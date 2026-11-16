package model

import (
	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// abilities 表的物理删除迁移。
//
// 背景：基座旧 priority/weight 选路与显式渠道 pin 已整体删除（design-v1 §3.3、
// routing-spec §1），车道（lanes/lane_members）是唯一选路入口；abilities 只是
// channels.(group, models) 的派生镜像，不再有任何读写方，这里把库里残留的表
// 一并删掉，避免"结构体没有但库里有"的长期漂移。
//
// 幂等：表不存在直接返回；失败只记日志、不阻塞启动（与 migrateDropTaskTables
// 的容错口径一致）——旧表残留不会影响任何行为，因为代码已不再读写它。
func migrateDropAbilitiesTable(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if !db.Migrator().HasTable("abilities") {
		return nil
	}
	if err := db.Migrator().DropTable("abilities"); err != nil {
		common.SysError("pbr: drop legacy abilities table failed: " + err.Error())
		return err
	}
	common.SysLog("pbr: dropped legacy abilities table")
	return nil
}
