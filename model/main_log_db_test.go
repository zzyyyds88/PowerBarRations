package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zzyyyds88/PowerBarRations/common"
)

// `LOG_SQL_DSN=local` 是基座别名，会被 chooseDB 解析为主库 SQLite 路径；
// 放行等于对同一个 pbr.db 开第二套连接，必须在启动期拒绝（design-v1 §16.4）。
func TestInitLogDBRejectsLocalAlias(t *testing.T) {
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		common.SetDatabaseTypes(previousMain, previousLog)
	})
	t.Setenv("LOG_SQL_DSN", "local")

	err := InitLogDB()

	require.Error(t, err)
	require.Contains(t, err.Error(), "LOG_SQL_DSN")
	require.Contains(t, err.Error(), "main SQLite database")
}
