package api

// 成员级人工停用（`enabled`）的管理面契约。口径真源：api-spec §4.2/§5.2/§5.7/§6.5，
// 用例清单见 test-spec §3.1。本文件重点保护三态默认值与"读回写"闭环——
// 本仓已因前端/读端点漏字段丢过成员上游真名，同一个坑不能再挖第三次。

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// memberShape 是成员响应断言形状（跨 lanes / routes / export 复用）。
type memberShape struct {
	Channel       string         `json:"channel"`
	UpstreamModel string         `json:"model"`
	PublicAlias   string         `json:"public_alias"`
	Priority      int            `json:"priority"`
	Enabled       *bool          `json:"enabled"`
	Overrides     map[string]any `json:"overrides"`
	MemberID      int            `json:"member_id"`
}

type laneEnvelope struct {
	Name    string        `json:"name"`
	Members []memberShape `json:"members"`
}

// modelSummaryItem 是 GET /api/models 的条目形状。DisabledMemberCount 用指针：
// 字段缺席（unconfigured 无成员链）与"显式 0"必须可区分。
type modelSummaryItem struct {
	Model                string `json:"model"`
	Source               string `json:"source"`
	Routable             bool   `json:"routable"`
	MemberCount          int    `json:"member_count"`
	AvailableMemberCount int    `json:"available_member_count"`
	DisabledMemberCount  *int   `json:"disabled_member_count"`
	Degraded             *bool  `json:"degraded"`
}

type modelsEnvelope struct {
	Items []modelSummaryItem `json:"items"`
}

// seedChannel 建一个启用渠道（成员必须指向真实渠道，否则成员写入 422）。
func seedChannel(t *testing.T, db *gorm.DB, name, models string) {
	t.Helper()
	ch := &model.Channel{Name: name, Type: 1, Key: "sk",
		Status: common.ChannelStatusEnabled, Group: "default", Models: models}
	require.NoError(t, db.Create(ch).Error)
}

// seedLane 用 PUT /lanes/{name} 建立车道（成员写入端点要求车道已存在，否则 404）。
func seedLane(t *testing.T, lane, members string) {
	t.Helper()
	recorder := callAPI(t, http.MethodPut, "/api/v1/lanes/"+lane,
		`{"enabled":true,"mode":"failover","members":`+members+`}`, PutLane,
		gin.Params{{Key: "name", Value: lane}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
}

// putMembers 用 PUT /lanes/{name}/members 写入成员链，返回响应成员。
func putMembers(t *testing.T, lane, body string) laneEnvelope {
	t.Helper()
	recorder := callAPI(t, http.MethodPut, "/api/v1/lanes/"+lane+"/members",
		`{"members":`+body+`}`, PutLaneMembers, gin.Params{{Key: "name", Value: lane}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp laneEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	return resp
}

func getLaneMembers(t *testing.T, lane string) laneEnvelope {
	t.Helper()
	recorder := callAPI(t, http.MethodGet, "/api/v1/lanes/"+lane, "", GetLane,
		gin.Params{{Key: "name", Value: lane}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp laneEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	return resp
}

// membersByModel 按上游真名索引成员，避免用例依赖 priority 排序细节。
func membersByModel(t *testing.T, lane laneEnvelope) map[string]memberShape {
	t.Helper()
	out := map[string]memberShape{}
	for _, m := range lane.Members {
		out[m.UpstreamModel] = m
	}
	return out
}

func modelItem(t *testing.T, key string) modelSummaryItem {
	t.Helper()
	recorder := callAPI(t, http.MethodGet, "/api/v1/models", "", ListModels, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp modelsEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	for _, item := range resp.Items {
		if item.Model == key {
			return item
		}
	}
	require.Failf(t, "模型不在清单里", "/api/models 必须列出 %q", key)
	return modelSummaryItem{}
}

// ---------- 1. 写入与写后回读 ----------

func TestPutLaneMembersPersistsDisabledAndReadsBack(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "dis-ch", "dis-model")
	seedLane(t, "dis-lane", `[{"channel":"dis-ch","model":"dis-model","priority":2}]`)

	putMembers(t, "dis-lane",
		`[{"channel":"dis-ch","model":"dis-model","priority":2,"enabled":false}]`)

	got := getLaneMembers(t, "dis-lane")
	require.Len(t, got.Members, 1)
	require.NotNil(t, got.Members[0].Enabled)
	assert.False(t, *got.Members[0].Enabled, "响应体必须是落库后的最终状态")

	saved, err := model.GetLaneByName("dis-lane")
	require.NoError(t, err)
	require.Len(t, saved.Members, 1)
	assert.True(t, saved.Members[0].Disabled, "库里确实记成停用（反向字段）")
}

// ---------- 2-4. 三态默认值（本轮最易写错处）----------

// 省略 enabled 的既有成员必须保留原值：否则任何不带该字段的调用方一发请求
// 就会把整条成员链关掉（成员写入是整体替换）。
func TestOmittedEnabledKeepsExistingMemberState(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "tri-ch", "tri-model")
	seedLane(t, "tri-lane", `[
		{"channel":"tri-ch","model":"tri-model","priority":2,"enabled":false},
		{"channel":"tri-ch","model":"tri-model-2","priority":1,"enabled":true}
	]`)

	// 整链重发，但一个字段的 enabled 都不带。
	putMembers(t, "tri-lane", `[
		{"channel":"tri-ch","model":"tri-model","priority":2},
		{"channel":"tri-ch","model":"tri-model-2","priority":1}
	]`)

	byModel := membersByModel(t, getLaneMembers(t, "tri-lane"))
	require.Len(t, byModel, 2)
	require.NotNil(t, byModel["tri-model"].Enabled)
	assert.False(t, *byModel["tri-model"].Enabled, "原本停用的成员在省略字段后必须仍是停用")
	require.NotNil(t, byModel["tri-model-2"].Enabled)
	assert.True(t, *byModel["tri-model-2"].Enabled, "原本启用的成员在省略字段后必须仍是启用")
}

// 新建成员省略 enabled → 默认启用。
func TestOmittedEnabledDefaultsNewMemberToEnabled(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "new-ch", "new-model")
	seedLane(t, "new-lane", `[{"channel":"new-ch","model":"seed-model","priority":1}]`)

	// 整链替换：seed-model 是既有成员，new-model 是新建成员，两者都不带 enabled。
	putMembers(t, "new-lane", `[
		{"channel":"new-ch","model":"seed-model","priority":1},
		{"channel":"new-ch","model":"new-model","priority":2}
	]`)

	byModel := membersByModel(t, getLaneMembers(t, "new-lane"))
	require.Len(t, byModel, 2)
	require.NotNil(t, byModel["new-model"].Enabled)
	assert.True(t, *byModel["new-model"].Enabled, "新成员省略 enabled 应按启用处理")
}

// 改了成员唯一键 (channel, upstream_model) 即视为新成员 → 按启用处理，
// 不得继承同位置旧成员的停用态。
func TestChangedMemberKeyIsTreatedAsNewAndEnabled(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "key-ch", "key-model")
	seedLane(t, "key-lane",
		`[{"channel":"key-ch","model":"key-model","priority":1,"enabled":false}]`)

	putMembers(t, "key-lane",
		`[{"channel":"key-ch","model":"key-model-renamed","priority":1}]`)

	got := getLaneMembers(t, "key-lane")
	require.Len(t, got.Members, 1)
	assert.Equal(t, "key-model-renamed", got.Members[0].UpstreamModel)
	require.NotNil(t, got.Members[0].Enabled)
	assert.True(t, *got.Members[0].Enabled, "唯一键变化即新成员，省略 enabled 应按启用")
}

// ---------- 5-6. GET /routes/{model} 的读回写闭环 ----------

func TestGetRouteReturnsEnabledOverridesAndMemberID(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "rt-ch", "rt-model")
	seedLane(t, "rt-model", `[
		{"channel":"rt-ch","model":"rt-model","priority":2,"enabled":false,
		 "overrides":{"member_cooldown_seconds":120}},
		{"channel":"rt-ch","model":"rt-model-2","priority":1}
	]`)

	recorder := callAPI(t, http.MethodGet, "/api/v1/routes/rt-model", "", GetRoute,
		gin.Params{{Key: "model", Value: "rt-model"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var detail struct {
		Members []memberShape `json:"members"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &detail))
	require.Len(t, detail.Members, 2)

	off, on := detail.Members[0], detail.Members[1]
	require.NotNil(t, off.Enabled)
	assert.False(t, *off.Enabled, "编排器草稿来源必须能看到停用态")
	assert.NotZero(t, off.MemberID, "member_id 必须回传")
	assert.Equal(t, float64(120), off.Overrides["member_cooldown_seconds"],
		"overrides 必须经本端点回传，否则控制台保存会清空六键覆盖")
	require.NotNil(t, on.Enabled)
	assert.True(t, *on.Enabled)
	require.NotNil(t, on.Overrides, "无覆盖时 overrides 恒回（不得缺席、不得 null）")
	assert.Empty(t, on.Overrides, "无覆盖即空对象")

	// 原始报文层面确认是 {} 而不是 null —— 缺席/null 会让前端把"未回传"当成"无值"。
	var raw map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &raw))
	rawMembers, ok := raw["members"].([]any)
	require.True(t, ok)
	require.Len(t, rawMembers, 2)
	second, ok := rawMembers[1].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, second, "overrides")
	assert.NotNil(t, second["overrides"], "overrides 不得为 null")
}

// ---------- 7. dry-run 不落库 ----------

func TestDryRunToggleDoesNotPersist(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "dr-ch", "dr-model")
	seedLane(t, "dr-lane",
		`[{"channel":"dr-ch","model":"dr-model","priority":1,"enabled":true}]`)

	recorder := callAPI(t, http.MethodPut, "/api/v1/lanes/dr-lane/members?dry_run=true",
		`{"members":[{"channel":"dr-ch","model":"dr-model","priority":1,"enabled":false}]}`,
		PutLaneMembers, gin.Params{{Key: "name", Value: "dr-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"dry_run":true`)

	got := getLaneMembers(t, "dr-lane")
	require.Len(t, got.Members, 1)
	require.NotNil(t, got.Members[0].Enabled)
	assert.True(t, *got.Members[0].Enabled, "?dry_run=true 只预览，不得把成员关掉")
}

// ---------- 8. 导出/导入往返 ----------

func TestExportImportPreservesDisabledMember(t *testing.T) {
	setupImportTestDB(t)
	seedChannel(t, model.DB, "ex-ch", "ex-model")
	seedLane(t, "ex-lane",
		`[{"channel":"ex-ch","model":"ex-model","priority":1,"enabled":false}]`)

	recorder := callAPI(t, http.MethodGet, "/api/v1/export", "", GetExport, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	bundle := recorder.Body.String()

	// 导出报文里必须带着关闭状态（裸 bool 会把"省略"读成 false，三态靠指针保住）。
	var parsed struct {
		Lanes []struct {
			Name    string        `json:"name"`
			Members []memberShape `json:"members"`
		} `json:"lanes"`
	}
	require.NoError(t, json.Unmarshal([]byte(bundle), &parsed))
	require.Len(t, parsed.Lanes, 1)
	require.Len(t, parsed.Lanes[0].Members, 1)
	require.NotNil(t, parsed.Lanes[0].Members[0].Enabled)
	assert.False(t, *parsed.Lanes[0].Members[0].Enabled, "导出必须带上人工停用状态")

	// 模拟"运维手工把成员放回选路"，再用导出文件还原。
	saved, err := model.GetLaneByName("ex-lane")
	require.NoError(t, err)
	require.Len(t, saved.Members, 1)
	require.NoError(t, model.DB.Model(&model.LaneMember{}).
		Where("id = ?", saved.Members[0].Id).Update("disabled", false).Error)

	imported := callAPI(t, http.MethodPost, "/api/v1/import", bundle, PostImport, nil)
	require.Equal(t, http.StatusOK, imported.Code, imported.Body.String())

	got := getLaneMembers(t, "ex-lane")
	require.Len(t, got.Members, 1)
	require.NotNil(t, got.Members[0].Enabled)
	assert.False(t, *got.Members[0].Enabled,
		"导入必须还原停用状态，否则「导出→清空→导入」会把人工停用的成员静默放回选路")
}

// ---------- 9. /api/models 的聚合可辨识 ----------

func TestModelsReportsDisabledMemberCountAndExcludesFromAvailable(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "cnt-ch", "cnt-model")
	// 三个成员的 (channel, upstream_model) 必须互不相同：成员唯一键就是这两元组
	// （ADR 0006），重复键会让三态默认值与计数语义都变得不可判定。
	seedLane(t, "cnt-model", `[
		{"channel":"cnt-ch","model":"cnt-model","priority":3,"enabled":false},
		{"channel":"cnt-ch","model":"cnt-model-2","priority":2,"enabled":false},
		{"channel":"cnt-ch","model":"cnt-model-3","priority":1,"enabled":true}
	]`)

	found := modelItem(t, "cnt-model")
	require.NotNil(t, found.DisabledMemberCount)
	assert.Equal(t, 2, *found.DisabledMemberCount, "disabled_member_count 数的是被人工停用的成员")
	assert.Equal(t, 1, found.AvailableMemberCount,
		"available_member_count 必须排除人工停用成员，否则界面报的是假健康")
	assert.Equal(t, 3, found.MemberCount)
	require.NotNil(t, found.Degraded)
	assert.False(t, *found.Degraded, "还有一名可选成员，不算 degraded")
}

// 全部成员被人工关闭：degraded=true 且 disabled_member_count == member_count，
// 与"上游全挂"（disabled_member_count=0）可区分（routing-spec §7）。
func TestModelsDistinguishesAllDisabledFromAllFailing(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "alloff-ch", "alloff-model")
	seedLane(t, "alloff-model", `[
		{"channel":"alloff-ch","model":"alloff-model","priority":2,"enabled":false},
		{"channel":"alloff-ch","model":"alloff-model-2","priority":1,"enabled":false}
	]`)

	found := modelItem(t, "alloff-model")
	require.NotNil(t, found.DisabledMemberCount)
	assert.Equal(t, 2, *found.DisabledMemberCount)
	assert.Equal(t, found.MemberCount, *found.DisabledMemberCount,
		"全关路径：disabled_member_count 等于成员总数")
	assert.Equal(t, 0, found.AvailableMemberCount)
	require.NotNil(t, found.Degraded)
	assert.True(t, *found.Degraded, "全部成员被关闭即当前无可选成员")
}

// unconfigured（渠道声明但没建车道）没有成员链，不得出现 disabled_member_count。
func TestModelsOmitsDisabledCountForUnconfigured(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "unc-ch", "unc-model")

	item := modelItem(t, "unc-model")
	require.Equal(t, model.RouteSourceUnconfigured, item.Source, "该用例必须落在 unconfigured 分支上")
	assert.Nil(t, item.DisabledMemberCount, "unconfigured 无成员链，字段应缺席而非 0")
	assert.Nil(t, item.Degraded, "degraded 只对 explicit 车道给出")
}

// ---------- 10. 全部成员被关闭是合法人工态，不是写入错误 ----------

func TestDisablingAllMembersIsAcceptedButEmptyChainIsNot(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "all-ch", "all-model")
	seedLane(t, "all-lane", `[{"channel":"all-ch","model":"all-model","priority":1}]`)

	recorder := callAPI(t, http.MethodPut, "/api/v1/lanes/all-lane/members",
		`{"members":[{"channel":"all-ch","model":"all-model","priority":1,"enabled":false}]}`,
		PutLaneMembers, gin.Params{{Key: "name", Value: "all-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String(),
		"关掉一切但保留配置是合法的人工干预状态，不得 422")

	// 对照：空成员链仍按既有 422 拒绝。
	recorder = callAPI(t, http.MethodPut, "/api/v1/lanes/all-lane/members",
		`{"members":[]}`, PutLaneMembers, gin.Params{{Key: "name", Value: "all-lane"}})
	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
}

// ---------- 11. 审计摘要必须能判定"谁把这条成员关掉了" ----------

func TestLaneDigestChangesWhenMemberToggled(t *testing.T) {
	db := setupAPITestDB(t)
	seedChannel(t, db, "dig-ch", "dig-model")
	seedLane(t, "dig-lane",
		`[{"channel":"dig-ch","model":"dig-model","priority":1,"enabled":true}]`)

	before, err := model.GetLaneByName("dig-lane")
	require.NoError(t, err)
	digestBefore := laneDigestOfLane(before)

	putMembers(t, "dig-lane",
		`[{"channel":"dig-ch","model":"dig-model","priority":1,"enabled":false}]`)
	after, err := model.GetLaneByName("dig-lane")
	require.NoError(t, err)

	assert.NotEqual(t, digestBefore, laneDigestOfLane(after),
		"after_digest 必须随成员开关变化，否则一次停用在审计里与不动作同形")
}
