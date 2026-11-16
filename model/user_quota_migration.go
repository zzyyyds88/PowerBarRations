package model

import (
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// User 表配额 / 邀请列的物理删除迁移。
//
// 背景（design-v1 §3.4）：User 在多用户面删除后只保留为记账/归属锚点，
// 设计明令"不允许出现配额字段（quota/remain_quota/used_quota）"；aff_code
// （邀请返利）同属已删除的多用户/邀请子系统，结构体字段与全部读写方已移除
// （GetUserQuota / UpdateUserUsedQuota / 批量写回聚合器），这里把库里残留的列
// 一并删掉，避免"结构体没有但库里有"的长期漂移。request_count 不在禁令清单内，
// 保守保留。
//
// 幂等：列不存在直接返回；失败只记日志、不阻塞启动（与 migrateDropChannelPriorityWeight
// 的容错口径一致）——旧列残留不会影响任何行为，因为代码已不再读写它们。
// SQLite 需 3.35+ 才支持 DROP COLUMN，更旧的版本会在这里报错留档，列残留无害。
func migrateDropUserQuotaColumns(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if !db.Migrator().HasTable(&User{}) {
		return nil
	}
	for _, column := range []string{"quota", "used_quota", "aff_code"} {
		if !db.Migrator().HasColumn(&User{}, column) {
			continue
		}
		if err := db.Migrator().DropColumn(&User{}, column); err != nil {
			// SQLite 旧版本可能不支持 DROP COLUMN；此时保留列不影响运行（代码不再读写）。
			if strings.Contains(strings.ToLower(err.Error()), "drop column") {
				common.SysError("pbr: drop users." + column + " skipped: " + err.Error())
				continue
			}
			common.SysError("pbr: drop users." + column + " failed: " + err.Error())
			continue
		}
		common.SysLog("pbr: dropped legacy users." + column + " column")
	}
	return nil
}
