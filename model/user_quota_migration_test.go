package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// User 表配额/邀请列（quota/used_quota/aff_code）的物理删除迁移
// （design-v1 §3.4 配额字段禁令）：有列时 DROP COLUMN，无列时直接返回（幂等），
// request_count 不在禁令清单内、必须保留。

// createLegacyUsersTable 按旧 schema 建 users 表（含已删列），模拟升级前的库。
// 反引号列名与 GORM AutoMigrate 在 SQLite 上生成的 DDL 一致——glebarez 驱动的
// DropColumn 走 DDL 重写，只识别这种带引号的列定义。
func createLegacyUsersTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec("CREATE TABLE `users` (`id` integer PRIMARY KEY AUTOINCREMENT,"+
		"`username` text UNIQUE COLLATE NOCASE,"+
		"`password` text NOT NULL,"+
		"`display_name` text,"+
		"`role` integer DEFAULT 1,"+
		"`status` integer DEFAULT 1,"+
		"`email` text,"+
		"`access_token` char(32) UNIQUE,"+
		"`access_token_created_at` bigint,"+
		"`quota` integer DEFAULT 0,"+
		"`used_quota` integer DEFAULT 0,"+
		"`request_count` integer DEFAULT 0,"+
		"`group` text DEFAULT \"default\","+
		"`aff_code` text UNIQUE COLLATE NOCASE,"+
		"`deleted_at` bigint,"+
		"`setting` text,"+
		"`remark` text,"+
		"`created_at` integer,"+
		"`auth_version` integer DEFAULT 1)").Error)
}

func openMemorySQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: nil,
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// 纯 Go sqlite 的 :memory: 库每连接独立，不钉 MaxOpenConns(1) 时建表与查询
	// 可能落在不同连接上，偶发 no such table（webhook 测试实测 flaky）。
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return db
}

func TestMigrateDropUserQuotaColumnsDropsLegacyColumns(t *testing.T) {
	db := openMemorySQLite(t)
	createLegacyUsersTable(t, db)

	for _, column := range []string{"quota", "used_quota", "aff_code", "request_count"} {
		require.True(t, db.Migrator().HasColumn("users", column), "旧库应有列 "+column)
	}

	require.NoError(t, migrateDropUserQuotaColumns(db))

	for _, column := range []string{"quota", "used_quota", "aff_code"} {
		assert.False(t, db.Migrator().HasColumn("users", column), "列 "+column+" 必须被物理删除")
	}
	// request_count 不在 §3.4 禁令清单内：保守保留，不得被顺手删掉。
	assert.True(t, db.Migrator().HasColumn("users", "request_count"))

	// 幂等：再次调用（列已不存在）必须直接返回，不报错（重复启动）。
	require.NoError(t, migrateDropUserQuotaColumns(db))

	// 删列后锚点读写仍可用：新 schema 的 User 能插入并回读。
	require.NoError(t, db.AutoMigrate(&User{}))
	user := User{Username: "anchor", Password: "x", Role: 1, Status: 1, Group: "default"}
	require.NoError(t, db.Omit(clause.Associations).Create(&user).Error)
	var reloaded User
	require.NoError(t, db.Where("username = ?", "anchor").First(&reloaded).Error)
	assert.Equal(t, "default", reloaded.Group)
}

func TestMigrateDropUserQuotaColumnsNoopWithoutColumns(t *testing.T) {
	db := openMemorySQLite(t)
	require.NoError(t, db.AutoMigrate(&User{}))
	require.False(t, db.Migrator().HasColumn(&User{}, "quota"))

	require.NoError(t, migrateDropUserQuotaColumns(db))
}

func TestMigrateDropUserQuotaColumnsNilDB(t *testing.T) {
	require.NoError(t, migrateDropUserQuotaColumns(nil))
}
