package legacy

import (
	"database/sql"
	"path/filepath"
	"testing"

	"pbr/common"
	"pbr/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 审查 B1 回归：路由层两把「同名不同明文」的客户端密钥，迁移后必须都落库。
// 旧实现按明文去重却在哈希索引里查重名，改名永不触发，后写覆盖先写丢凭据。
func TestBuildPlanRenamesDuplicateKeyNames(t *testing.T) {
	dir := t.TempDir()
	routingPath := filepath.Join(dir, "routing.db")
	vendorPath := filepath.Join(dir, "vendor.db")
	writeRoutingFixture(t, routingPath)
	writeVendorFixture(t, vendorPath)

	routingDB, err := sql.Open("sqlite", routingPath)
	require.NoError(t, err)
	_, err = routingDB.Exec("INSERT INTO api_keys (id,name,api_key,enabled,expire_at,supported_models) VALUES (2,'legacy-key','sk-downstream-1-second',1,0,NULL)")
	require.NoError(t, err)
	require.NoError(t, routingDB.Close())

	routing, err := ReadRouting(routingPath)
	require.NoError(t, err)
	vendor, err := ReadVendor(vendorPath)
	require.NoError(t, err)
	plan, err := BuildPlan(routing, vendor, "octopus")
	require.NoError(t, err)

	require.Len(t, plan.Keys, 2, "两把不同明文必须都保留")
	names := map[string]bool{}
	hashes := map[string]bool{}
	for _, key := range plan.Keys {
		assert.False(t, names[key.Name], "最终名字必须唯一: %s", key.Name)
		names[key.Name] = true
		hashes[model.HashClientKey(key.Plain)] = true
	}
	assert.Len(t, hashes, 2)
	require.Len(t, plan.Report.DuplicateKeyNames, 1)
	dup := plan.Report.DuplicateKeyNames[0]
	assert.Equal(t, "legacy-key", dup.Name)
	assert.NotEmpty(t, dup.RenamedTo)
	assert.NotEqual(t, dup.Name, dup.RenamedTo)
	assert.Len(t, dup.Hash8, 8)
	assert.NotContains(t, dup.KeyPrefix, "downstream", "报告不得泄漏完整明文")
}

// 审查 B1 回归：即使 plan 被外部构造出重名，Apply 也必须拒绝而不是静默覆盖。
func TestApplyRejectsDuplicateKeyNames(t *testing.T) {
	plan := buildFixturePlan(t)
	plan.Keys = []KeyPlan{
		{Name: "dup", Plain: "pbr-first-secret"},
		{Name: "dup", Plain: "pbr-second-secret"},
	}
	targetPath := filepath.Join(t.TempDir(), "pbr.db")
	db, err := gorm.Open(sqlite.Open(targetPath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, EnsureSchema(db))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	err = Apply(db, plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate client key name")
	var count int64
	require.NoError(t, db.Model(&model.ClientKey{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "拒绝时不得写入任何密钥")
}

// 审查 B1 回归：改名后的两把同名密钥都要在目标库可查，且哈希各异。
func TestApplyKeepsBothDuplicateNamedKeys(t *testing.T) {
	dir := t.TempDir()
	routingPath := filepath.Join(dir, "routing.db")
	vendorPath := filepath.Join(dir, "vendor.db")
	writeRoutingFixture(t, routingPath)
	writeVendorFixture(t, vendorPath)
	routingDB, err := sql.Open("sqlite", routingPath)
	require.NoError(t, err)
	_, err = routingDB.Exec("INSERT INTO api_keys (id,name,api_key,enabled,expire_at,supported_models) VALUES (2,'legacy-key','sk-downstream-1-second',1,0,NULL)")
	require.NoError(t, err)
	require.NoError(t, routingDB.Close())

	routing, err := ReadRouting(routingPath)
	require.NoError(t, err)
	vendor, err := ReadVendor(vendorPath)
	require.NoError(t, err)
	plan, err := BuildPlan(routing, vendor, "octopus")
	require.NoError(t, err)

	targetPath := filepath.Join(dir, "pbr.db")
	db, err := gorm.Open(sqlite.Open(targetPath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, EnsureSchema(db))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, Apply(db, plan))

	var keys []model.ClientKey
	require.NoError(t, db.Order("id asc").Find(&keys).Error)
	require.Len(t, keys, 2)
	assert.NotEqual(t, keys[0].KeyHash, keys[1].KeyHash)
	for _, key := range keys {
		assert.Equal(t, model.HashClientKey(planKeyPlain(plan, key.Name)), key.KeyHash)
	}
}

// 审查 B2 回归：非法 --keys 必须在计划期就报错，不能静默产出 0 把凭据。
func TestBuildPlanRejectsInvalidKeySource(t *testing.T) {
	routing, err := ReadRouting(writeRoutingOnly(t))
	require.NoError(t, err)
	vendor, err := ReadVendor(writeVendorOnly(t))
	require.NoError(t, err)
	for _, bad := range []string{"bogus", "new-api", "newapi ", "OCTOPUS"} {
		_, buildErr := BuildPlan(routing, vendor, bad)
		require.Error(t, buildErr, "非法 key_source %q 必须报错", bad)
	}
	assert.True(t, ValidKeySource("octopus"))
	assert.True(t, ValidKeySource("newapi"))
	assert.True(t, ValidKeySource("both"))
}

func planKeyPlain(plan *Plan, name string) string {
	for _, key := range plan.Keys {
		if key.Name == name {
			return key.Plain
		}
	}
	return ""
}

func writeRoutingOnly(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "routing.db")
	writeRoutingFixture(t, path)
	return path
}

func writeVendorOnly(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vendor.db")
	writeVendorFixture(t, path)
	return path
}
