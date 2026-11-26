package backupcli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunCLICreatesVerifiedSnapshot(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "pbr.db")
	db, err := gorm.Open(sqlite.Open(src), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE t (x integer)").Error)
	require.NoError(t, db.Exec("INSERT INTO t VALUES (7)").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	dst := filepath.Join(dir, "backup.db")
	require.Equal(t, 0, RunCLI([]string{"--db", src, "--out", dst}))

	copyDB, err := gorm.Open(sqlite.Open(dst), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var value int
	require.NoError(t, copyDB.Raw("SELECT x FROM t").Scan(&value).Error)
	assert.Equal(t, 7, value)
	if sqlDB, err := copyDB.DB(); err == nil {
		_ = sqlDB.Close()
	}
	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// 不覆盖已有备份。
	require.Equal(t, 1, RunCLI([]string{"--db", src, "--out", dst}))
}
