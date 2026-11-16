package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 默认六键必须可经 system/options 读写，并真正影响"新建/一键固化车道"的初值
// 与"车道未显式配置时的回落值"（ui-spec §6.7 / design-v1 §7.3，审查 F12）。
func TestLaneDefaultsAreConfigurableAndAffectNewLanes(t *testing.T) {
	setupImportTestDB(t)
	gin.SetMode(gin.TestMode)

	// 默认（未配置）时是内置值。
	require.EqualValues(t, 2, model.DefaultLaneRelayConfig().MemberMaxAttempts)
	require.EqualValues(t, 3, model.DefaultLaneRelayConfig().MemberRetryIntervalSeconds)

	// PUT 默认六键。
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"lane_defaults":{"member_max_attempts":5,"member_retry_interval_seconds":0,
	  "member_non_stream_response_timeout_seconds":90,"member_stream_first_event_timeout_seconds":15,
	  "member_cooldown_seconds":30,"member_affinity_seconds":7}}`
	c.Request = httptest.NewRequest(http.MethodPut, "/api/system/options", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	PutSystemOptions(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	updated := model.DefaultLaneRelayConfig()
	assert.EqualValues(t, 5, updated.MemberMaxAttempts)
	assert.EqualValues(t, 0, updated.MemberRetryIntervalSeconds, "显式 0 必须被尊重（无重试间隔）")
	assert.EqualValues(t, 90, updated.MemberNonStreamResponseTimeoutSeconds)
	assert.EqualValues(t, 15, updated.MemberStreamFirstEventTimeoutSeconds)
	assert.EqualValues(t, 30, updated.MemberCooldownSeconds)
	assert.EqualValues(t, 7, updated.MemberAffinitySeconds)

	// 车道未显式配置的字段回落到新的默认值。
	effective := model.LaneRelayConfig{MemberCooldownSeconds: 45}.Normalize()
	assert.EqualValues(t, 5, effective.MemberMaxAttempts)
	assert.EqualValues(t, 45, effective.MemberCooldownSeconds)

	// 一键固化写库的初值是新的默认六键。
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "lane-defaults-probe", Enabled: true, Mode: model.LaneModeFailover,
		Config: `{}`, Members: []model.LaneMember{{ChannelId: 1, Priority: 1}}}))
	seeded, err := model.GetLaneByName("lane-defaults-probe")
	require.NoError(t, err)
	assert.EqualValues(t, 5, model.ParseLaneRelayConfig(seeded.Config).MemberMaxAttempts)

	// GET 回读同一份值，且导出体里带上 lane_defaults。
	getRecorder := httptest.NewRecorder()
	getCtx, _ := gin.CreateTestContext(getRecorder)
	getCtx.Request = httptest.NewRequest(http.MethodGet, "/api/system/options", nil)
	GetSystemOptions(getCtx)
	var options systemOptions
	require.NoError(t, json.Unmarshal(getRecorder.Body.Bytes(), &options))
	require.NotNil(t, options.LaneDefaults)
	assert.EqualValues(t, 5, options.LaneDefaults.MemberMaxAttempts)

	bundle, err := BuildConfigBundle()
	require.NoError(t, err)
	require.NotNil(t, bundle.SystemOptions)
	require.NotNil(t, bundle.SystemOptions.LaneDefaults)
	assert.EqualValues(t, 5, bundle.SystemOptions.LaneDefaults.MemberMaxAttempts)

	// 清理：删除选项，避免影响同进程的其它用例（provider 按原始字符串失效缓存）。
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, model.OptionLaneDefaults)
		common.OptionMapRWMutex.Unlock()
		assert.EqualValues(t, 2, model.DefaultLaneRelayConfig().MemberMaxAttempts)
	})
}

// 非法默认六键必须 422（api-spec §6.9：四个时长/预算键 > 0、两个间隔键 ≥ 0），
// 而不是把坏值写进 option 表，也不得误报参数错误 400。
func TestLaneDefaultsValidation(t *testing.T) {
	setupImportTestDB(t)
	gin.SetMode(gin.TestMode)

	cases := []string{
		`{"lane_defaults":{"member_max_attempts":0,"member_retry_interval_seconds":3,"member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":30,"member_cooldown_seconds":60,"member_affinity_seconds":0}}`,
		`{"lane_defaults":{"member_max_attempts":2,"member_retry_interval_seconds":-1,"member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":30,"member_cooldown_seconds":60,"member_affinity_seconds":0}}`,
		`{"lane_defaults":{"member_max_attempts":2,"member_retry_interval_seconds":3,"member_non_stream_response_timeout_seconds":0,"member_stream_first_event_timeout_seconds":30,"member_cooldown_seconds":60,"member_affinity_seconds":0}}`,
		`{"lane_defaults":{"member_max_attempts":2,"member_retry_interval_seconds":3,"member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":0,"member_cooldown_seconds":60,"member_affinity_seconds":0}}`,
		`{"lane_defaults":{"member_max_attempts":2,"member_retry_interval_seconds":3,"member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":30,"member_cooldown_seconds":0,"member_affinity_seconds":0}}`,
		`{"lane_defaults":{"member_max_attempts":2,"member_retry_interval_seconds":3,"member_non_stream_response_timeout_seconds":120,"member_stream_first_event_timeout_seconds":30,"member_cooldown_seconds":60,"member_affinity_seconds":-1}}`,
	}
	for _, body := range cases {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/system/options", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		PutSystemOptions(c)
		assert.Equalf(t, http.StatusUnprocessableEntity, recorder.Code, "body=%s", body)
	}
	assert.EqualValues(t, 2, model.DefaultLaneRelayConfig().MemberMaxAttempts, "校验失败不得写库")
}
