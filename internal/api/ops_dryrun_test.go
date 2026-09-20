package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apiresp"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dry-run 行为测试：每个声明为 preview 的端点，带 ?dry_run=true 调用后
// **回读状态必须与调用前一致**，且响应含 dry_run:true。
//
// 这是"禁止静默写入"的运行时保证：守卫测试只证明"声明了"，这里证明"真的没写"。
func TestOpsDryRunPreviewHasNoSideEffects(t *testing.T) {
	engine := newOpsContractEngine(t)

	// —— 造 fixtures ——
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PrefillGroup{}))
	require.NoError(t, db.Create(&model.Channel{Name: "dry-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "dry-m"}).Error)
	require.NoError(t, db.Create(&model.Channel{Name: "dry-dead", Type: 1, Key: "sk", Status: common.ChannelStatusManuallyDisabled, Group: "default", Models: "dry-d"}).Error)
	require.NoError(t, db.Create(&model.Lane{Name: "dry-m", Enabled: true, Mode: model.LaneModeFailover}).Error)
	var lane model.Lane
	require.NoError(t, db.Where("name = ?", "dry-m").First(&lane).Error)
	var ch model.Channel
	require.NoError(t, db.Where("name = ?", "dry-ch").First(&ch).Error)
	require.NoError(t, db.Create(&model.LaneMember{LaneId: lane.Id, ChannelId: ch.Id, UpstreamModel: "dry-m", Priority: 1}).Error)
	require.NoError(t, db.Create(&model.ClientKey{Name: "dry-key", KeyPlain: "pbr-x", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.PrefillGroup{Name: "dry-pg", Type: "model"}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "dry-meta", NameRule: model.NameRuleExact, Status: 1}).Error)

	// 快照：任何 preview 调用后这些计数都不得变化。
	snapshot := func() map[string]any {
		var channels, lanes, members, keys, pgs, metas int64
		require.NoError(t, db.Model(&model.Channel{}).Count(&channels).Error)
		require.NoError(t, db.Model(&model.Lane{}).Count(&lanes).Error)
		require.NoError(t, db.Model(&model.LaneMember{}).Count(&members).Error)
		require.NoError(t, db.Model(&model.ClientKey{}).Count(&keys).Error)
		require.NoError(t, db.Model(&model.PrefillGroup{}).Count(&pgs).Error)
		require.NoError(t, db.Model(&model.Model{}).Count(&metas).Error)
		var enabledCh model.Channel
		_ = db.Where("name = ?", "dry-ch").First(&enabledCh).Error
		return map[string]any{"channels": channels, "lanes": lanes, "members": members,
			"keys": keys, "prefill_groups": pgs, "model_meta": metas, "dry_ch_enabled": enabledCh.Status}
	}

	type probe struct {
		method, path, body string
	}
	probes := []probe{
		{http.MethodPost, "/api/v1/channels/batch/status?dry_run=true", `{"channels":["dry-ch"],"status":2}`},
		{http.MethodPost, "/api/v1/channels/batch/tag?dry_run=true", `{"channels":["dry-ch"],"tag":"DRY"}`},
		{http.MethodPost, "/api/v1/channels/batch/copy?dry_run=true", `{"channel":"dry-ch","suffix":"-dry"}`},
		{http.MethodDelete, "/api/v1/channels/disabled?dry_run=true", ""},
		{http.MethodPost, "/api/v1/prefill-groups?dry_run=true", `{"name":"dry-pg-new","type":"model","items":[]}`},
		{http.MethodPost, "/api/v1/model-catalog/batch-delete?dry_run=true", `{"models":["dry-meta"]}`},
		{http.MethodPost, "/api/v1/system-tasks/log-cleanup?dry_run=true&target_timestamp=1", ""},
	}

	for _, p := range probes {
		t.Run(p.method+" "+strings.Split(p.path, "?")[0], func(t *testing.T) {
			before := snapshot()
			recorder := doOpsRequest(engine, p.method, p.path, p.body)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
			assert.Equal(t, true, body["dry_run"], "preview 响应必须含 dry_run:true：%s", recorder.Body.String())
			after := snapshot()
			assert.Equal(t, before, after,
				"dry-run 调用不得改变任何状态（%s %s）", p.method, p.path)
		})
	}
}

// reject 类端点（当前稳定面没有，但契约允许）：带 ?dry_run=true 必须 400 且不执行。
// 这里用中间件直接验证语义，保证将来新增 reject 端点时行为正确。
func TestDryRunRejectMiddlewareBlocksHandler(t *testing.T) {
	apiresp.ResetForTest()
	t.Cleanup(apiresp.ResetForTest)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(apiresp.Middleware())
	apiresp.Register(http.MethodDelete, "/api/v1/guarded", opsPolicy())
	executed := false
	group.DELETE("/guarded", dryRunRejectMiddleware("no preview here"), func(c *gin.Context) {
		executed = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	recorder := doOpsRequest(engine, http.MethodDelete, "/api/v1/guarded?dry_run=true", "")
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	assert.False(t, executed, "reject 端点带 ?dry_run=true 时 handler 绝不能执行")
	assert.Contains(t, recorder.Body.String(), "dry_run_not_supported")

	// 不带 dry_run 时照常执行。
	recorder = doOpsRequest(engine, http.MethodDelete, "/api/v1/guarded", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, executed, "不带 dry_run 时必须正常执行")
}
