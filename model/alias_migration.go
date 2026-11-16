package model

import (
	"fmt"

	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

type aliasDuplicate struct {
	Alias string
	Count int64
}

// migrateMemberAliasUniqueness 为成员别名建立库级唯一约束（SQLite 部分索引，
// 空别名不参与）。历史重复别名不会阻塞启动：只记录并跳过该表的索引，应用层
// 仍会拒绝新的重名。
func migrateMemberAliasUniqueness(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate member alias uniqueness: database is nil")
	}
	if db.Dialector.Name() != "sqlite" {
		return nil
	}
	if err := ensureAliasUniqueIndex(db, "lane_members", "idx_lane_members_public_alias_unique"); err != nil {
		return err
	}
	if hasMemberGroupItemsTable(db) {
		if err := ensureAliasUniqueIndex(db, "member_group_items", "idx_member_group_items_public_alias_unique"); err != nil {
			return err
		}
	}
	return nil
}

func ensureAliasUniqueIndex(db *gorm.DB, table, index string) error {
	var dups []aliasDuplicate
	if err := db.Table(table).
		Select("public_alias as alias, count(*) as count").
		Where("public_alias <> ''").
		Group("public_alias").
		Having("count(*) > 1").
		Order("public_alias asc").
		Scan(&dups).Error; err != nil {
		return fmt.Errorf("scan duplicate aliases in %s: %w", table, err)
	}
	if len(dups) > 0 {
		names := make([]string, 0, len(dups))
		for _, dup := range dups {
			names = append(names, fmt.Sprintf("%q(%d)", dup.Alias, dup.Count))
		}
		common.SysError("[pbr] duplicate member aliases in " + table + ": " +
			fmt.Sprint(names) + " — resolve them before a unique index can be created")
		return nil
	}
	if err := db.Exec(
		"CREATE UNIQUE INDEX IF NOT EXISTS " + index + " ON " + table + "(public_alias) WHERE public_alias <> ''",
	).Error; err != nil {
		return fmt.Errorf("create unique alias index on %s: %w", table, err)
	}
	return nil
}
