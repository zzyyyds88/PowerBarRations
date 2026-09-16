package model

import (
	"pbr/common"

	"gorm.io/gorm"
)

// tasks / task_plugins 表的物理删除迁移。
//
// 背景：JS 任务插件子系统（task plugins / 异步任务轮询）已整体移除，Task 与
// TaskPlugin 结构体从代码与全部契约里删除，这里把库里残留的表一并删掉，
// 避免"结构体没有但库里有"的长期漂移。Midjourney 与 SystemTask 表保留。
//
// 幂等：表不存在直接返回；失败只记日志、不阻塞启动（与
// migrateDropChannelPriorityWeight 的容错口径一致）——旧表残留不会影响任何
// 行为，因为代码已不再读写它们。
func migrateDropTaskTables(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	for _, table := range []string{"tasks", "task_plugins"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		if err := db.Migrator().DropTable(table); err != nil {
			common.SysError("pbr: drop legacy " + table + " table failed: " + err.Error())
			continue
		}
		common.SysLog("pbr: dropped legacy " + table + " table")
	}
	return nil
}
