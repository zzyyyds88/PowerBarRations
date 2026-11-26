package outboxcli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func seedOutboxDB(t *testing.T) (string, func() *gorm.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pbr.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PBRStatsOutbox{}))
	require.NoError(t, db.Create(&model.PBRStatsOutbox{
		Payload: "pending", CreatedAt: 2, Attempts: 1,
	}).Error)
	require.NoError(t, db.Create(&model.PBRStatsOutbox{
		Payload: "dead", CreatedAt: 1, Attempts: 10, LastError: "decode payload",
	}).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		return db
	}
	return path, open
}

func TestOutboxCLIStatusRetryDiscard(t *testing.T) {
	path, open := seedOutboxDB(t)

	require.Equal(t, 0, RunCLI([]string{"status", "--db", path, "--json"}))
	require.Equal(t, 0, RunCLI([]string{"retry", "--db", path, "--dead"}))

	db := open()
	var retried model.PBRStatsOutbox
	require.NoError(t, db.Where("payload = ?", "dead").First(&retried).Error)
	assert.Zero(t, retried.Attempts)
	assert.Empty(t, retried.LastError)

	// 再次制造死信并删除。
	require.NoError(t, db.Model(&model.PBRStatsOutbox{}).Where("payload = ?", "dead").
		Updates(map[string]any{"attempts": 10, "last_error": "bad"}).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	require.Equal(t, 2, RunCLI([]string{"discard", "--db", path, "--dead"}),
		"未带 --yes 必须拒绝")
	require.Equal(t, 0, RunCLI([]string{"discard", "--db", path, "--dead", "--yes"}))

	db = open()
	var dead int64
	require.NoError(t, db.Model(&model.PBRStatsOutbox{}).Where("attempts >= ?", 10).Count(&dead).Error)
	assert.Zero(t, dead)
	var pending int64
	require.NoError(t, db.Model(&model.PBRStatsOutbox{}).Where("payload = ?", "pending").Count(&pending).Error)
	assert.EqualValues(t, 1, pending, "discard 不得误删可重试行")
}
