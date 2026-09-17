package legacy

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func writeRoutingFixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	stmts := []string{
		"CREATE TABLE channels (id integer primary key, name text, type text, enabled numeric, base_url text, key text)",
		"CREATE TABLE channel_models (id integer primary key, channel_id integer, name text, source text)",
		"CREATE TABLE groups (id integer primary key, name text, mode text, active_item_id integer)",
		"CREATE TABLE group_items (id integer primary key, group_id integer, channel_model_id integer, priority integer)",
		"CREATE TABLE api_keys (id integer primary key, name text, api_key text, enabled numeric, expire_at integer, supported_models text)",
		"INSERT INTO channels (id,name,type,enabled,base_url,key) VALUES (1,'legacy-a','openai',1,'https://legacy.example/v1','sk-legacy')",
		"INSERT INTO channel_models (id,channel_id,name,source) VALUES (1,1,'model-one','auto'),(2,1,'model-two','auto')",
		"INSERT INTO groups (id,name,mode,active_item_id) VALUES (1,'lane-one','failover',0),(2,'lane-two','manual',4)",
		"INSERT INTO group_items (id,group_id,channel_model_id,priority) VALUES (1,1,1,20),(2,1,2,10),(3,2,1,5),(4,2,2,3)",
		"INSERT INTO api_keys (id,name,api_key,enabled,expire_at,supported_models) VALUES (1,'legacy-key','sk-downstream-1',1,0,'[\"model-one\"]')",
	}
	for _, stmt := range stmts {
		_, execErr := db.Exec(stmt)
		require.NoError(t, execErr, stmt)
	}
}

func writeVendorFixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	stmts := []string{
		"CREATE TABLE channels (id integer primary key, type integer, key text, status integer, name text, weight integer, base_url text, models text, [group] text, model_mapping text, priority integer)",
		"CREATE TABLE abilities ([group] text, model text, channel_id integer, enabled numeric, priority integer, weight integer)",
		"CREATE TABLE tokens (id integer primary key, user_id integer, key text, status integer, name text, expired_time integer, model_limits_enabled numeric, model_limits text, allow_ips text, [group] text, deleted_at datetime)",
		"INSERT INTO channels (id,type,key,status,name,weight,base_url,models,[group],model_mapping,priority) VALUES (1,1,'sk-vendor-1',1,'vendor-one',0,'https://vendor-one.example/v1','model-one','default','{\"model-one\":\"real-one\"}',10),(2,1,'sk-vendor-2',1,'vendor-two',0,'https://vendor-two.example/v1','model-two','default',NULL,5)",
		"INSERT INTO abilities ([group],model,channel_id,enabled,priority,weight) VALUES ('default','model-one',1,1,10,0),('default','model-two',2,1,5,0)",
		"INSERT INTO tokens (id,user_id,key,status,name,expired_time,model_limits_enabled,model_limits,allow_ips,[group],deleted_at) VALUES (1,1,'sk-downstream-2',1,'token-two',0,0,NULL,'','default',NULL),(2,1,'sk-deleted',1,'token-deleted',0,0,NULL,'','default','2026-01-01 00:00:00')",
	}
	for _, stmt := range stmts {
		_, execErr := db.Exec(stmt)
		require.NoError(t, execErr, stmt)
	}
}

func buildFixturePlan(t *testing.T) *Plan {
	t.Helper()
	dir := t.TempDir()
	routingPath := filepath.Join(dir, "routing.db")
	vendorPath := filepath.Join(dir, "vendor.db")
	writeRoutingFixture(t, routingPath)
	writeVendorFixture(t, vendorPath)

	routing, err := ReadRouting(routingPath)
	require.NoError(t, err)
	vendor, err := ReadVendor(vendorPath)
	require.NoError(t, err)
	plan, err := BuildPlan(routing, vendor, "both")
	require.NoError(t, err)
	return plan
}

func TestBuildPlanMapsTwoLayersIntoPBR(t *testing.T) {
	plan := buildFixturePlan(t)

	require.Len(t, plan.Channels, 2)
	require.Len(t, plan.Lanes, 2)
	// 1 个 octopus 密钥 + 1 个未删除的 new-api 令牌（删除的那条跳过）。
	require.Len(t, plan.Keys, 2)

	byName := map[string]ChannelPlan{}
	for _, channel := range plan.Channels {
		byName[channel.Name] = channel
	}
	// model_mapping 必须生效：vendor-one 对外声明的是真名 real-one。
	assert.Equal(t, []string{"real-one"}, byName["vendor-one"].Models)
	assert.Equal(t, []string{"model-two"}, byName["vendor-two"].Models)

	var laneOne *LanePlan
	for i := range plan.Lanes {
		if plan.Lanes[i].Name == "lane-one" {
			laneOne = &plan.Lanes[i]
		}
	}
	require.NotNil(t, laneOne)
	require.Len(t, laneOne.Members, 2)
	assert.Equal(t, 20, laneOne.Members[0].Priority)
	assert.Equal(t, "vendor-one", laneOne.Members[0].Channel)
	assert.Equal(t, "real-one", laneOne.Members[0].UpstreamModel)

	for _, lane := range plan.Lanes {
		if lane.Name == "lane-two" {
			assert.Equal(t, "manual", lane.Mode)
			assert.NotEmpty(t, lane.ActiveMember)
		}
	}

	// supported_models 白名单必须被记为"放宽"。
	require.NotEmpty(t, plan.Report.Widened)
	assert.Empty(t, plan.Report.Unresolved)
}

func TestBuildPlanReportsUnresolvedAndAmbiguous(t *testing.T) {
	dir := t.TempDir()
	routingPath := filepath.Join(dir, "routing.db")
	vendorPath := filepath.Join(dir, "vendor.db")
	writeRoutingFixture(t, routingPath)
	writeVendorFixture(t, vendorPath)

	// 追加一个与 vendor-two 同 (priority,weight) 的 model-two 能力 → 平局。
	vendorDB, err := sql.Open("sqlite", vendorPath)
	require.NoError(t, err)
	_, err = vendorDB.Exec("INSERT INTO channels (id,type,key,status,name,weight,base_url,models,[group],model_mapping,priority) VALUES (3,1,'sk-vendor-3',1,'vendor-three',0,'https://vendor-three.example/v1','model-two','default',NULL,5)")
	require.NoError(t, err)
	_, err = vendorDB.Exec("INSERT INTO abilities ([group],model,channel_id,enabled,priority,weight) VALUES ('default','model-two',3,1,5,0)")
	require.NoError(t, err)
	require.NoError(t, vendorDB.Close())

	// 路由层引用一个厂商层根本不存在的模型 → 必须进 unresolved，不许静默丢弃。
	routingDB, err := sql.Open("sqlite", routingPath)
	require.NoError(t, err)
	_, err = routingDB.Exec("INSERT INTO channel_models (id,channel_id,name,source) VALUES (3,1,'model-ghost','manual')")
	require.NoError(t, err)
	_, err = routingDB.Exec("INSERT INTO group_items (id,group_id,channel_model_id,priority) VALUES (5,1,3,1)")
	require.NoError(t, err)
	require.NoError(t, routingDB.Close())

	routing, err := ReadRouting(routingPath)
	require.NoError(t, err)
	vendor, err := ReadVendor(vendorPath)
	require.NoError(t, err)
	plan, err := BuildPlan(routing, vendor, "octopus")
	require.NoError(t, err)

	assert.NotEmpty(t, plan.Report.Ambiguous)
	assert.NotEmpty(t, plan.Report.Unresolved)
	assert.Len(t, plan.Keys, 1)
}

func TestApplyIsIdempotent(t *testing.T) {
	plan := buildFixturePlan(t)

	targetPath := filepath.Join(t.TempDir(), "pbr.db")
	db, err := gorm.Open(sqlite.Open(targetPath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, EnsureSchema(db))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	require.NoError(t, Apply(db, plan))
	var channelCount, laneCount, keyCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	require.NoError(t, db.Model(&model.Lane{}).Count(&laneCount).Error)
	require.NoError(t, db.Model(&model.ClientKey{}).Count(&keyCount).Error)
	require.Equal(t, int64(2), channelCount)
	require.Equal(t, int64(2), laneCount)
	require.Equal(t, int64(2), keyCount)

	// 第二次执行必须幂等：计数不变，成员集合不变，密钥哈希不变。
	require.NoError(t, Apply(db, plan))
	var channelCount2, laneCount2, keyCount2, memberCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount2).Error)
	require.NoError(t, db.Model(&model.Lane{}).Count(&laneCount2).Error)
	require.NoError(t, db.Model(&model.ClientKey{}).Count(&keyCount2).Error)
	require.NoError(t, db.Model(&model.LaneMember{}).Count(&memberCount).Error)
	assert.Equal(t, channelCount, channelCount2)
	assert.Equal(t, laneCount, laneCount2)
	assert.Equal(t, keyCount, keyCount2)
	assert.Equal(t, int64(4), memberCount)

	var key model.ClientKey
	require.NoError(t, db.Where("name = ?", "legacy-key").First(&key).Error)
	assert.Equal(t, model.HashClientKey("sk-downstream-1"), key.KeyHash)
	assert.Equal(t, model.LanePolicyModeAll, model.ParseLanePolicy(key.LanePolicy).Mode)
}
