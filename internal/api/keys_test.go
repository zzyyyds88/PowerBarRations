package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listKeyCosts 调用 ListKeys 并返回 密钥名 → cost 的映射（api-spec §5.4）。
func listKeyCosts(t *testing.T) map[string]float64 {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	ListKeys(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var body struct {
		Items []struct {
			Name string  `json:"name"`
			Cost float64 `json:"cost"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
	costs := make(map[string]float64, len(body.Items))
	for _, item := range body.Items {
		costs[item.Name] = item.Cost
	}
	return costs
}

// GET /api/keys 的只读 cost 必须来自 pbr_stats_hourly 中 group_kind='key' 且
// group_key=密钥 Name 的 SUM(cost_sum)（api-spec §5.4、token-spec §3.7）：
// 每个密钥取自己那一份，不得串味，也不得受其它维度影响。
//
// 同时验证「读聚合表而非明细表」：明细被 prune 清空后 cost 不变。
func TestListKeysReportsHourlyCostPerKey(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PBRStatsHourly{}, &model.PBRRequestLog{}))

	require.NoError(t, db.Create(&model.ClientKey{Name: "key-alpha", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.ClientKey{Name: "key-beta", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.ClientKey{Name: "key-empty", Enabled: true}).Error)

	baseTs := int64(1_700_000_000)
	// key-alpha 跨两个小时桶，SUM(cost_sum) = 1.25 + 0.75 = 2.00。
	for _, bucket := range []model.PBRStatsHourly{
		{BucketTs: baseTs, GroupKind: "key", GroupKey: "key-alpha", Requests: 2, Successes: 2, CostSum: 1.25},
		{BucketTs: baseTs + 3600, GroupKind: "key", GroupKey: "key-alpha", Requests: 1, Successes: 1, CostSum: 0.75},
		// key-beta 只在一个桶里。
		{BucketTs: baseTs, GroupKind: "key", GroupKey: "key-beta", Requests: 1, Successes: 1, CostSum: 0.25},
		// 其它维度与未知密钥名不得混进任何密钥的 cost。
		{BucketTs: baseTs, GroupKind: "lane", GroupKey: "key-alpha", Requests: 9, Successes: 9, CostSum: 99},
		{BucketTs: baseTs, GroupKind: "key", GroupKey: "not-a-client-key", Requests: 1, Successes: 1, CostSum: 42},
	} {
		require.NoError(t, db.Create(&bucket).Error)
	}

	// 明细表插入数据只是为了让下面的 prune 有东西可删（证明断言不是"两边都没数据"）。
	for i, name := range []string{"key-alpha", "key-beta"} {
		require.NoError(t, db.Create(&model.PBRRequestLog{
			Ts:            baseTs + int64(i),
			TokenName:     name,
			LaneName:      "lane-cost",
			RequestModel:  "model-cost",
			Success:       true,
			EstimatedCost: 0.1,
		}).Error)
	}

	before := listKeyCosts(t)
	require.Contains(t, before, "key-alpha")
	assert.InDelta(t, 2.00, before["key-alpha"], 1e-9, "cost 必须是该密钥 SUM(cost_sum)")
	assert.InDelta(t, 0.25, before["key-beta"], 1e-9, "每个密钥只取自己那份")
	assert.InDelta(t, 0.0, before["key-empty"], 1e-9, "无聚合数据时为 0")

	// 模拟 POST /logs/prune：清空明细表（聚合表长期保留）。
	deleted, err := model.PrunePBRRequestLogsBefore(baseTs + 100)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	var detailCount int64
	require.NoError(t, db.Model(&model.PBRRequestLog{}).Count(&detailCount).Error)
	require.Zero(t, detailCount, "明细已清空，这是断言 cost 不读明细的前提")

	after := listKeyCosts(t)
	assert.Equal(t, before, after, "明细 prune 后 cost 必须不变（读聚合表）")
}

// GET /api/keys/{name} 详情同样带只读 cost。
func TestGetKeyReportsHourlyCost(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PBRStatsHourly{}))
	require.NoError(t, db.Create(&model.ClientKey{Name: "key-detail", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.PBRStatsHourly{
		BucketTs: 1_700_000_000, GroupKind: "key", GroupKey: "key-detail", CostSum: 3.5,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/keys/key-detail", nil)
	c.Params = gin.Params{{Key: "name", Value: "key-detail"}}
	GetKey(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var body struct {
		Cost float64 `json:"cost"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.InDelta(t, 3.5, body.Cost, 1e-9)
}

func TestGetKeyReturnsPersistedPlaintextAndPreservesItOnMetadataUpdate(t *testing.T) {
	db := setupAPITestDB(t)
	plain := "pbr-persisted-test-key"
	key := &model.ClientKey{
		Name:      "key-plaintext",
		KeyHash:   model.HashClientKey(plain),
		KeyPrefix: model.PrefixOfClientKey(plain),
		KeyPlain:  plain,
		Enabled:   true,
	}
	require.NoError(t, model.UpsertClientKey(key))

	// Metadata updates must not clear the persisted credential.
	require.NoError(t, model.UpsertClientKey(&model.ClientKey{
		Name:    key.Name,
		Enabled: false,
		Notes:   "updated metadata",
	}))
	saved, err := model.GetClientKeyByName(key.Name)
	require.NoError(t, err)
	assert.Equal(t, plain, saved.KeyPlain)
	assert.Equal(t, model.HashClientKey(plain), saved.KeyHash)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/keys/key-plaintext", nil)
	c.Params = gin.Params{{Key: "name", Value: key.Name}}
	GetKey(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var body struct {
		Key       string  `json:"key"`
		KeyPrefix string  `json:"key_prefix"`
		Cost      float64 `json:"cost"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, plain, body.Key)
	assert.Equal(t, model.PrefixOfClientKey(plain), body.KeyPrefix)

	// A migrated legacy row has no plaintext and must be distinguishable.
	require.NoError(t, db.Create(&model.ClientKey{
		Name: "key-legacy", KeyHash: model.HashClientKey("legacy"), KeyPrefix: "sk-legacy", Enabled: true,
	}).Error)
	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/keys/key-legacy", nil)
	c.Params = gin.Params{{Key: "name", Value: "key-legacy"}}
	GetKey(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var legacy struct {
		Key       *string `json:"key"`
		KeyPrefix string  `json:"key_prefix"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &legacy))
	assert.Nil(t, legacy.Key)
	assert.Equal(t, "sk-legacy", legacy.KeyPrefix)
}

// allow_lanes 里既无同名车道、也无渠道声明的键必须被拒绝（否则是静默死键）。
func TestUnknownRouteKeysRejectsDeadKeys(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "declared-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "declared-model"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "laned-model", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 1}}}))

	assert.Empty(t, unknownRouteKeys([]string{"laned-model", "declared-model"}), "有车道或有渠道声明的键应通过")
	assert.Equal(t, []string{"ghost-model"}, unknownRouteKeys([]string{"laned-model", "ghost-model", "ghost-model"}), "不存在的键应去重列出")
	assert.Empty(t, unknownRouteKeys(nil))
}
