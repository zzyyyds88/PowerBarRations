package common

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sqliteDSNQuery(t *testing.T, dsn string) url.Values {
	t.Helper()
	_, raw, ok := strings.Cut(dsn, "?")
	require.True(t, ok, "DSN 必须带查询参数: %s", dsn)
	values, err := url.ParseQuery(raw)
	require.NoError(t, err)
	return values
}

func TestSQLiteDSNAddsRequiredPragmas(t *testing.T) {
	dsn := SQLiteDSN("/data/pbr.db")
	assert.True(t, strings.HasPrefix(dsn, "/data/pbr.db?"))
	values := sqliteDSNQuery(t, dsn)
	assert.Equal(t, "immediate", values.Get("_txlock"))
	assert.Contains(t, values["_pragma"], "busy_timeout(30000)")
	assert.Contains(t, values["_pragma"], "journal_mode(WAL)")
}

func TestSQLiteDSNOverridesConflictingParams(t *testing.T) {
	dsn := SQLiteDSN("pbr.db?_busy_timeout=5&_txlock=exclusive&_pragma=journal_mode(DELETE)&cache=shared")
	values := sqliteDSNQuery(t, dsn)
	assert.Empty(t, values.Get("_busy_timeout"), "plain _busy_timeout 被驱动忽略，必须移除")
	assert.Equal(t, "immediate", values.Get("_txlock"))
	assert.Equal(t, "shared", values.Get("cache"), "无关参数必须保留")
	pragmas := strings.Join(values["_pragma"], ",")
	assert.NotContains(t, pragmas, "journal_mode(DELETE)")
	assert.Contains(t, pragmas, "journal_mode(WAL)")
	assert.Contains(t, pragmas, "busy_timeout(30000)")
}

func TestSQLiteDSNEmptyUsesDefaultFile(t *testing.T) {
	dsn := SQLiteDSN("  ")
	assert.True(t, strings.HasPrefix(dsn, "pbr.db?"))
}
