package model

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
)

// DBWriteHealth 最近一次数据库写失败的可展示快照（design-v1 §16.5）。
// 只保留最近一条：健康端点要回答"现在写库是否正常"，不是错误历史。
type DBWriteHealth struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	At      int64  `json:"at"`
}

var lastDBWriteFailure atomic.Pointer[DBWriteHealth]

// RecordDBWriteError 记录最近一次写库失败（脱敏 + UTF-8 安全截断）。
// stage 只允许低基数枚举，不能放请求 ID、SQL 或凭据。
func RecordDBWriteError(stage string, err error) {
	if err == nil {
		return
	}
	lastDBWriteFailure.Store(&DBWriteHealth{
		Stage:   stage,
		Message: common.TruncateUTF8(common.MaskSensitiveInfo(err.Error()), 512),
		At:      time.Now().Unix(),
	})
}

// LastDBWriteError 返回最近一次写库失败；从未失败返回 nil。
// 注意：它不会因为后续成功而自动清除，健康端点用时间判断是否为近期故障。
func LastDBWriteError() *DBWriteHealth {
	failure := lastDBWriteFailure.Load()
	if failure == nil {
		return nil
	}
	copied := *failure
	return &copied
}

// isRetryableSQLiteWriteError 只对 SQLite 的锁竞争/临时 I/O 错误做有限重试。
// 磁盘满、库损坏等错误重试无益，应立即暴露，避免把损坏写成"偶发失败"。
func isRetryableSQLiteWriteError(err error) bool {
	if err == nil || common.MainDatabaseType() != common.DatabaseTypeSQLite {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "sqlite_busy"),
		strings.Contains(msg, "database is locked"),
		strings.Contains(msg, "sqlite_locked"):
		return true
	default:
		return false
	}
}
