package model

import (
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// 渠道 priority / weight 列的物理删除迁移。
//
// 背景（design-v1 §16#14）：路由顺序只在车道的成员顺序上，渠道不再参与排序；
// 两个字段已从 Channel 结构体与全部契约里删除，这里把库里残留的列一并删掉，
// 避免"结构体没有但库里有"的长期漂移。
//
// 幂等：列不存在直接返回；失败只记日志、不阻塞启动（与 migrateOptionPrimaryKey
// 的容错口径一致）——旧列残留不会影响任何行为，因为代码已不再读写它们。
func migrateDropChannelPriorityWeight(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if !db.Migrator().HasTable(&Channel{}) {
		return nil
	}
	for _, column := range []string{"priority", "weight"} {
		if !db.Migrator().HasColumn(&Channel{}, column) {
			continue
		}
		if err := db.Migrator().DropColumn(&Channel{}, column); err != nil {
			// SQLite 旧版本可能不支持 DROP COLUMN；此时保留列不影响运行（代码不再读写）。
			if strings.Contains(strings.ToLower(err.Error()), "drop column") {
				common.SysError("pbr: drop channels." + column + " skipped: " + err.Error())
				continue
			}
			common.SysError("pbr: drop channels." + column + " failed: " + err.Error())
			continue
		}
		common.SysLog("pbr: dropped legacy channels." + column + " column")
	}
	return nil
}
