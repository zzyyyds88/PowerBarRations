package model

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/zzyyyds88/PowerBarRations/common"
	"gorm.io/gorm"
)

// TestMain was originally defined in the removed task_cas_test.go; it is the
// shared DB fixture for the retained model tests (system task, channel, lane,
// PBR request log, ...). The tasks table is gone with the task subsystem.
func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&User{},
		&Log{},
		&Channel{},
		&Ability{},
		&PerfMetric{},
		&SystemTask{},
		&SystemTaskLock{},
		&Lane{},
		&LaneMember{},
		&PBRAdminCredential{},
		&ClientKey{},
		&PBRAuditLog{},
		&PBRRequestLog{},
		&PBRStatsHourly{},
		&Option{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM channels")
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM perf_metrics")
		DB.Exec("DELETE FROM system_task_locks")
		DB.Exec("DELETE FROM system_tasks")
		DB.Exec("DELETE FROM lane_members")
		DB.Exec("DELETE FROM lanes")
		DB.Exec("DELETE FROM pbr_admin_credentials")
		DB.Exec("DELETE FROM client_keys")
		DB.Exec("DELETE FROM pbr_audit_logs")
		DB.Exec("DELETE FROM pbr_request_logs")
		DB.Exec("DELETE FROM pbr_stats_hourly")
	})
}
