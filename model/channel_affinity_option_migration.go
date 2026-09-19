package model

import (
	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// 渠道亲和（channel_affinity_setting.*）选项行的物理删除迁移。
//
// 背景：基座"渠道亲和"机制（规则表 → 用户-渠道粘滞 + param_override 参数覆盖
// 模板）超出产品范围，整套物理删除；裁决依据 routing-spec §6——本项目只有
// 车道级亲和（internal/route），渠道层不做任何粘滞。规则与开关以扁平选项键
// （channel_affinity_setting.enabled / rules / ...）存于 options 表，注册已随
// setting 删除，这里把库里残留的行一并删掉，避免"注册没有但库里有"的长期漂移
// （否则 loadOptionsFromDatabase 会持续把孤儿键灌进 OptionMap）。
//
// 幂等：没有残留行直接返回；失败只记日志、不阻塞启动（与
// migrateDropAbilitiesTable 的容错口径一致）。
func migrateDropChannelAffinityOptions(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	res := db.Where("key LIKE ?", "channel_affinity_setting.%").Delete(&Option{})
	if res.Error != nil {
		common.SysError("pbr: drop legacy channel affinity options failed: " + res.Error.Error())
		return res.Error
	}
	if res.RowsAffected > 0 {
		common.SysLog("pbr: dropped legacy channel affinity options")
	}
	return nil
}
