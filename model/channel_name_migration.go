package model

import (
	"fmt"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// migrateChannelNameUniqueness 为渠道名建立唯一索引（api-spec §4.1）。
//
// 历史数据可能已经存在同名渠道；SQLite 在有重复值时无法创建唯一索引，而让服务
// 启动失败会把运维锁在门外。因此这里采用"能建则建、不能建则大声报告"的策略：
//   - 无重名 → CREATE UNIQUE INDEX（幂等）；
//   - 有重名 → 逐条列出 name(count)，跳过索引创建，启动继续；写路径仍由
//     EnsureChannelNamesAvailable / Channel.Update 拒绝新增重名。
//
// 运维改名或删除重复渠道后，下次启动会自动补上索引。
func migrateChannelNameUniqueness(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate channel name uniqueness: database is nil")
	}
	if db.Dialector.Name() != "sqlite" {
		// 部署矩阵只支持 SQLite；其它方言保留应用层校验。
		return nil
	}
	var dups []ChannelNameDuplicate
	if err := db.Model(&Channel{}).
		Select("name, count(*) as count").
		Group("name").
		Having("count(*) > 1").
		Order("name asc").
		Scan(&dups).Error; err != nil {
		return fmt.Errorf("scan duplicate channel names: %w", err)
	}
	if len(dups) > 0 {
		names := make([]string, 0, len(dups))
		for _, dup := range dups {
			names = append(names, fmt.Sprintf("%q(%d)", dup.Name, dup.Count))
		}
		common.SysError("[pbr] duplicate channel names detected: " + strings.Join(names, ", ") +
			" — rename or delete the duplicates; channel name must be unique (api-spec §4.1)")
		return nil
	}
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_name_unique ON channels(name)").Error; err != nil {
		return fmt.Errorf("create unique channel name index: %w", err)
	}
	return nil
}
