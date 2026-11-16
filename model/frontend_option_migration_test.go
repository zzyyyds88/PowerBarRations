package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"gorm.io/gorm"
)

func useFrontendOptionMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// 纯 Go sqlite 的 :memory: 库每连接独立，不钉 MaxOpenConns(1) 时建表与查询
	// 可能落在不同连接上，偶发 no such table（webhook 测试实测 flaky）。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	require.NoError(t, db.AutoMigrate(&Option{}))
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
	})
	return db
}

func requireOptionValue(t *testing.T, db *gorm.DB, key string) string {
	t.Helper()
	var option Option
	require.NoError(t, db.Where(&Option{Key: key}).First(&option).Error)
	return option.Value
}

func requireOptionMissing(t *testing.T, db *gorm.DB, key string) {
	t.Helper()
	var option Option
	assert.ErrorIs(t, db.Where(&Option{Key: key}).First(&option).Error, gorm.ErrRecordNotFound)
}

// API 信息面板已整体删除：历史遗留的旧键与新键都必须被幂等清理。
func TestMigrateRetiredFrontendOptionsRemovesAPIInfoOptions(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	legacy := []Option{
		{Key: retiredThemeOptionKey, Value: "classic"},
		{Key: "ApiInfo", Value: `[{"url":"https://api.example.com","route":"primary","description":"API","color":"blue"}]`},
		{Key: "console_setting.api_info", Value: `[{"url":"https://new.example.com"}]`},
		{Key: "console_setting.api_info_enabled", Value: "true"},
	}
	require.NoError(t, db.Create(&legacy).Error)

	require.NoError(t, MigrateRetiredFrontendOptions())
	assert.Equal(t, "default", requireOptionValue(t, db, retiredThemeOptionKey))
	for _, key := range []string{"ApiInfo", "console_setting.api_info", "console_setting.api_info_enabled"} {
		requireOptionMissing(t, db, key)
	}

	// 幂等：再跑一次结果不变。
	before, err := AllOption()
	require.NoError(t, err)
	require.NoError(t, MigrateRetiredFrontendOptions())
	after, err := AllOption()
	require.NoError(t, err)
	assert.ElementsMatch(t, before, after)
}

// 退役键不存在时清理必须是 no-op，不得报错。
func TestMigrateRetiredFrontendOptionsIsNoopWhenOptionsAbsent(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	require.NoError(t, MigrateRetiredFrontendOptions())
	assert.Equal(t, "default", requireOptionValue(t, db, retiredThemeOptionKey))
	for _, key := range retiredOptionKeys {
		requireOptionMissing(t, db, key)
	}
}

func TestRetiredThemeOptionIsPersistedButNotPublished(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	previousMap := common.OptionMap
	t.Cleanup(func() { common.OptionMap = previousMap })
	common.OptionMap = map[string]string{}

	require.NoError(t, UpdateOption(retiredThemeOptionKey, "default"))
	assert.Equal(t, "default", requireOptionValue(t, db, retiredThemeOptionKey))
	_, published := common.OptionMap[retiredThemeOptionKey]
	assert.False(t, published)
}
