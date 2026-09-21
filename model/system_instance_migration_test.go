package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// system_instances 废弃表迁移：有表时物理删除，无表时直接返回（幂等）。
// 单节点 SQLite 部署下该表随系统信息 / 多节点实例视图一并删除（design-v1 §1.3/§1.5）。
func TestMigrateDropSystemInstanceTableDropsExistingTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.NoError(t, db.Exec(
		"CREATE TABLE system_instances (node_name varchar(128) PRIMARY KEY, info text, started_at bigint, last_seen_at bigint)",
	).Error)
	require.True(t, db.Migrator().HasTable("system_instances"))

	require.NoError(t, migrateDropSystemInstanceTable(db))
	assert.False(t, db.Migrator().HasTable("system_instances"), "有表时必须 drop 成功")

	// 再次调用：表已不存在，必须直接返回（重复启动不报错）。
	require.NoError(t, migrateDropSystemInstanceTable(db))
	assert.False(t, db.Migrator().HasTable("system_instances"))
}

func TestMigrateDropSystemInstanceTableNoopWhenMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.False(t, db.Migrator().HasTable("system_instances"))
	require.NoError(t, migrateDropSystemInstanceTable(db))
	assert.False(t, db.Migrator().HasTable("system_instances"))
}

// db 为 nil 时必须直接返回，不得 panic（master 判定等边界场景）。
func TestMigrateDropSystemInstanceTableNilDB(t *testing.T) {
	require.NoError(t, migrateDropSystemInstanceTable(nil))
}
