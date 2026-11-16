package model

import (
	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// system_instances 表的物理删除迁移。
//
// 背景（design-v1 §1.3 / §1.5）：PBR 只支持单节点 SQLite，基座继承的
// 「实例上报 + 系统信息页 + 多节点实例视图」已整体物理删除，SystemInstance
// 结构体从代码与全部契约里删除；这里把库里残留的表一并删掉，避免
// "结构体没有但库里有"的长期漂移。NODE_NAME 仅继续用于日志与任务 runner 标识。
//
// 幂等：表不存在直接返回；失败只记日志、不阻塞启动（与 migrateDropTaskTables
// 的容错口径一致）——旧表残留不会影响任何行为，因为代码已不再读写它。
func migrateDropSystemInstanceTable(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if !db.Migrator().HasTable("system_instances") {
		return nil
	}
	if err := db.Migrator().DropTable("system_instances"); err != nil {
		common.SysError("pbr: drop legacy system_instances table failed: " + err.Error())
		return nil
	}
	common.SysLog("pbr: dropped legacy system_instances table")
	return nil
}
