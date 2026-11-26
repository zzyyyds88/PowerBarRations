package common

import (
	"net/url"
	"strings"
)

type DatabaseType string

const (
	DatabaseTypeMySQL      DatabaseType = "mysql"
	DatabaseTypeSQLite     DatabaseType = "sqlite"
	DatabaseTypePostgreSQL DatabaseType = "postgres"
	DatabaseTypeClickHouse DatabaseType = "clickhouse"
)

var mainDatabaseType = DatabaseTypeSQLite
var logDatabaseType = DatabaseTypeSQLite

func MainDatabaseType() DatabaseType {
	return mainDatabaseType
}

func LogDatabaseType() DatabaseType {
	return logDatabaseType
}

func SetMainDatabaseType(databaseType DatabaseType) {
	mainDatabaseType = databaseType
}

func SetLogDatabaseType(databaseType DatabaseType) {
	logDatabaseType = databaseType
}

func SetDatabaseTypes(mainType DatabaseType, logType DatabaseType) {
	mainDatabaseType = mainType
	logDatabaseType = logType
}

func UsingMainDatabase(databaseType DatabaseType) bool {
	return mainDatabaseType == databaseType
}

func UsingLogDatabase(databaseType DatabaseType) bool {
	return logDatabaseType == databaseType
}

// SQLitePath is the DSN for the default SQLite database. The file name is
// pbr.db (design-v1 §17 替换清单⑩) unless SQLITE_PATH overrides it; it uses
// WAL journal mode so readers are never blocked by the single writer, plus a
// 30s busy timeout for writers to queue.
//
// Two details are non-obvious and both are required for concurrent correctness:
//
//  1. The busy timeout must be passed as a `_pragma=busy_timeout(30000)` DSN
//     parameter. The pure-Go driver (modernc.org/sqlite, used through
//     github.com/glebarez/sqlite) silently ignores the plain `_busy_timeout=`
//     form, so without this the effective timeout stays at SQLite's 5s default
//     and concurrent writes surface as "database is locked" (see #6805).
//
//  2. `_txlock=immediate` (BEGIN IMMEDIATE) must be enabled. Without it, a
//     transaction that first SELECTs (establishing a read snapshot) and then
//     writes can hit SQLITE_BUSY_SNAPSHOT when another connection commits in
//     between; the busy handler does not cover that case, so the write fails
//     instantly no matter the timeout. BEGIN IMMEDIATE takes the write lock up
//     front, so writers serialize through the busy timeout instead of dying on
//     a stale snapshot. Autocommit SELECTs stay concurrent because WAL keeps
//     readers unlocked.
var SQLitePath = SQLiteDSN("pbr.db")

// SQLiteDSN 在调用方给的路径/DSN 上强制补齐并发正确性所需的参数。
//
// SQLITE_PATH 允许用户只写 `pbr.db`（README 的简写），也允许带自定义参数。
// 但 busy_timeout(WAL/_txlock=immediate 是正确性依赖，不能因为用户漏写或
// 写了冲突值（例如 journal_mode(DELETE)）而失效：这里删除同名旧值后追加
// 规范值，其余参数（mode/cache 等）原样保留。空路径回落到默认 pbr.db。
func SQLiteDSN(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "pbr.db"
	}
	base, rawQuery, _ := strings.Cut(path, "?")
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		// 无法解析的查询串不猜测：保留原样，由驱动报错，避免静默改坏路径。
		return path
	}
	values.Del("_busy_timeout")
	values.Del("_txlock")

	// 只保留用户自定义的 pragma；busy_timeout/journal_mode 由这里统一。
	pragmas := make([]string, 0, 3)
	for _, pragma := range values["_pragma"] {
		lower := strings.ToLower(strings.TrimSpace(pragma))
		if strings.HasPrefix(lower, "busy_timeout(") || strings.HasPrefix(lower, "journal_mode(") {
			continue
		}
		pragmas = append(pragmas, pragma)
	}
	pragmas = append(pragmas, "busy_timeout(30000)", "journal_mode(WAL)")
	values["_pragma"] = pragmas
	values.Set("_txlock", "immediate")
	return base + "?" + values.Encode()
}
