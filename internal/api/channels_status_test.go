package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 渠道状态（api-spec §6.12）：两个只读端点的形状、窗口校验、404 与运行态联动。

func getChannelStatus(t *testing.T, name, query string) *httptest.ResponseRecorder {
	t.Helper()
	return callAPI(t, http.MethodGet, "/api/v1/channels/"+name+"/status"+query, "", GetChannelStatusByName,
		gin.Params{{Key: "name", Value: name}})
}

func getChannelStatusList(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	return callAPI(t, http.MethodGet, "/api/v1/channels/status"+query, "", ListChannelStatus, nil)
}

func TestChannelStatusRejectsUnknownWindow(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "win-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "m"}
	require.NoError(t, db.Create(ch).Error)

	// 查询参数取值校验按 400（与 /api/stats 的 group_by 同口径，api-spec §6.12）。
	recorder := getChannelStatus(t, ch.Name, "?window=1h")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "validation_failed")

	listRecorder := getChannelStatusList(t, "?window=nope")
	assert.Equal(t, http.StatusBadRequest, listRecorder.Code)
	assert.Contains(t, listRecorder.Body.String(), "validation_failed")
}

func TestChannelStatusUnknownChannelIs404(t *testing.T) {
	setupAPITestDB(t)
	recorder := getChannelStatus(t, "no-such-channel", "")
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "channel_not_found")
}

func TestChannelStatusDetailShapeWithRuntimeAndHistory(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "health-ch", Type: 1, Key: "sk-health", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "health-model"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "health-lane", Enabled: true, Mode: model.LaneModeFailover,
		Config:  `{"member_max_attempts":1,"member_cooldown_seconds":60}`,
		Members: []model.LaneMember{{ChannelId: ch.Id, Model: "health-model", Priority: 1}}}))
	model.InitChannelCache()

	// 把这条车道的运行态推进到"成员冷却中"（真实选路路径，不手改运行态）：
	resolved, err := model.ResolveRoute("health-lane")
	require.NoError(t, err)
	state := route.NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, ok = state.Next(types.WithOpenAIError(types.OpenAIError{Message: "boom"}, http.StatusInternalServerError))
	require.False(t, ok, "预算耗尽应进入冷却")

	// 窗口统计：落一个当前小时桶；最近失败：落一条明细；探活历史：落一条探活记录。
	hour := (time.Now().Unix() / 3600) * 3600
	require.NoError(t, db.Create(&model.PBRStatsHourly{BucketTs: hour, GroupKind: "channel", GroupKey: ch.Name,
		Requests: 120, Successes: 118, CostSum: 0.12}).Error)
	require.NoError(t, db.Create(&model.PBRRequestLog{Ts: time.Now().Unix(), LaneName: "health-lane",
		RequestModel: "health-model", MemberChannelName: ch.Name, Success: false, HTTPStatus: 500,
		ErrorKind: "soft_transient", ErrorSummary: "upstream 500"}).Error)
	require.NoError(t, model.WriteProbeLog(&model.PBRProbeLog{Ts: time.Now().Unix(), ChannelId: ch.Id,
		ChannelName: ch.Name, UpstreamModel: "health-model", Status: "success", DurationMs: 320, HTTPStatus: 200}))

	recorder := getChannelStatus(t, ch.Name, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp struct {
		Channel  map[string]any `json:"channel"`
		Reference struct {
			Lanes []struct {
				Lane     string `json:"lane"`
				Model    string `json:"model"`
				Priority int    `json:"priority"`
				Enabled  bool   `json:"enabled"`
			} `json:"lanes"`
		} `json:"reference"`
		Runtime struct {
			MemberCount    int `json:"member_count"`
			AvailableCount int `json:"available_count"`
			Members        []struct {
				Lane          string  `json:"lane"`
				Member        string  `json:"member"`
				Circuit       string  `json:"circuit"`
				CooldownUntil *string `json:"cooldown_until"`
				LastErrorKind string  `json:"last_error_kind"`
				Available     bool    `json:"available"`
			} `json:"members"`
		} `json:"runtime"`
		Window  string `json:"window"`
		Summary struct {
			Requests    int64   `json:"requests"`
			Successes   int64   `json:"successes"`
			Failures    int64   `json:"failures"`
			SuccessRate float64 `json:"success_rate"`
			AvgTotalMs  float64 `json:"avg_total_ms"`
		} `json:"summary"`
		Daily          []map[string]any `json:"daily"`
		RecentFailures []map[string]any `json:"recent_failures"`
		ProbeHistory   []map[string]any `json:"probe_history"`
		Upstream       struct {
			LastCheckTS  any      `json:"last_check_ts"`
			PendingAdd   []string `json:"pending_add"`
			PendingRemove []string `json:"pending_remove"`
		} `json:"upstream_updates"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))

	assert.Equal(t, ch.Name, resp.Channel["name"])
	assert.True(t, resp.Channel["enabled"].(bool))
	assert.Equal(t, "24h", resp.Window)

	require.Len(t, resp.Reference.Lanes, 1)
	assert.Equal(t, "health-lane", resp.Reference.Lanes[0].Lane)
	assert.Equal(t, "health-model", resp.Reference.Lanes[0].Model)
	assert.True(t, resp.Reference.Lanes[0].Enabled)

	require.Equal(t, 1, resp.Runtime.MemberCount)
	require.Len(t, resp.Runtime.Members, 1)
	assert.Equal(t, "health-lane", resp.Runtime.Members[0].Lane)
	assert.Equal(t, ch.Name+"/health-model", resp.Runtime.Members[0].Member)
	assert.Equal(t, route.CircuitClosed, resp.Runtime.Members[0].Circuit)
	require.NotNil(t, resp.Runtime.Members[0].CooldownUntil, "真实选路产生的冷却必须反映在渠道状态里")
	assert.Equal(t, "soft_transient", resp.Runtime.Members[0].LastErrorKind)
	assert.False(t, resp.Runtime.Members[0].Available)
	assert.Equal(t, 0, resp.Runtime.AvailableCount)

	assert.EqualValues(t, 120, resp.Summary.Requests)
	assert.EqualValues(t, 118, resp.Summary.Successes)
	assert.EqualValues(t, 2, resp.Summary.Failures)
	assert.InDelta(t, 118.0/120.0, resp.Summary.SuccessRate, 1e-9)
	require.Len(t, resp.Daily, 1)
	assert.EqualValues(t, 120, resp.Daily[0]["requests"])

	require.Len(t, resp.RecentFailures, 1)
	assert.EqualValues(t, 500, resp.RecentFailures[0]["http_status"])
	assert.Equal(t, "soft_transient", resp.RecentFailures[0]["error_kind"])
	require.Len(t, resp.ProbeHistory, 1)
	assert.Equal(t, "success", resp.ProbeHistory[0]["status"])
	assert.Nil(t, resp.Upstream.LastCheckTS, "从未探测上游变动时 last_check_ts 为 null")
	assert.Empty(t, resp.Upstream.PendingAdd)
	assert.Empty(t, resp.Upstream.PendingRemove)
}

// 未被任何车道引用的渠道：引用与运行态都是空数组，不是省略字段。
func TestChannelStatusUnreferencedChannelHasEmptyArrays(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "lonely-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "m"}
	require.NoError(t, db.Create(ch).Error)

	recorder := getChannelStatus(t, ch.Name, "?window=7d")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.JSONEq(t, `[]`, string(mustRaw(t, resp, "reference", "lanes")))
	assert.JSONEq(t, `[]`, string(mustRaw(t, resp, "runtime", "members")))
	assert.JSONEq(t, `[]`, string(mustRaw(t, resp, "recent_failures")))
	assert.JSONEq(t, `[]`, string(mustRaw(t, resp, "probe_history")))
	var window struct {
		Window string `json:"window"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &window))
	assert.Equal(t, "7d", window.Window)
}

// 全量汇总：字段齐全；降级渠道标 degraded；探活过就有 last_probe。
func TestChannelStatusListSummary(t *testing.T) {
	db := setupAPITestDB(t)
	okCh := &model.Channel{Name: "list-ok-ch", Type: 1, Key: "sk1", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "m"}
	degCh := &model.Channel{Name: "list-deg-ch", Type: 1, Key: "sk2", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "m"}
	require.NoError(t, db.Create(okCh).Error)
	require.NoError(t, db.Create(degCh).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "list-deg-lane", Enabled: true, Mode: model.LaneModeFailover,
		Config:  `{"member_max_attempts":1,"member_cooldown_seconds":60}`,
		Members: []model.LaneMember{{ChannelId: degCh.Id, Model: "m", Priority: 1}}}))
	model.InitChannelCache()

	resolved, err := model.ResolveRoute("list-deg-lane")
	require.NoError(t, err)
	state := route.NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, ok = state.Next(types.WithOpenAIError(types.OpenAIError{Message: "boom"}, http.StatusInternalServerError))
	require.False(t, ok)

	require.NoError(t, model.WriteProbeLog(&model.PBRProbeLog{Ts: time.Now().Unix(), ChannelId: okCh.Id,
		ChannelName: okCh.Name, UpstreamModel: "m", Status: "success", DurationMs: 111, HTTPStatus: 200}))

	recorder := getChannelStatusList(t, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp struct {
		Window string `json:"window"`
		Items  []struct {
			Channel        string  `json:"channel"`
			Enabled        bool    `json:"enabled"`
			Type           string  `json:"type"`
			LaneCount      int     `json:"lane_count"`
			MemberCount    int     `json:"member_count"`
			AvailableCount int     `json:"available_count"`
			Degraded       bool    `json:"degraded"`
			LastErrorKind  string  `json:"last_error_kind"`
			LastProbe      *struct {
				Status     string `json:"status"`
				DurationMs int64  `json:"duration_ms"`
			} `json:"last_probe"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Equal(t, "24h", resp.Window)

	byName := map[string]int{}
	for i, item := range resp.Items {
		byName[item.Channel] = i
	}
	require.Contains(t, byName, "list-ok-ch")
	require.Contains(t, byName, "list-deg-ch")

	okItem := resp.Items[byName["list-ok-ch"]]
	assert.True(t, okItem.Enabled)
	assert.Equal(t, "openai", okItem.Type)
	assert.False(t, okItem.Degraded)
	require.NotNil(t, okItem.LastProbe)
	assert.Equal(t, "success", okItem.LastProbe.Status)
	assert.EqualValues(t, 111, okItem.LastProbe.DurationMs)

	degItem := resp.Items[byName["list-deg-ch"]]
	assert.Equal(t, 1, degItem.LaneCount)
	assert.Equal(t, 1, degItem.MemberCount)
	assert.Equal(t, 0, degItem.AvailableCount)
	assert.True(t, degItem.Degraded, "成员冷却中 → degraded")
	assert.Equal(t, "soft_transient", degItem.LastErrorKind)
	assert.Nil(t, degItem.LastProbe, "从未探活的渠道不带 last_probe 字段")
}

// mustRaw 逐层取 JSON 子字段的原始串（用于断言"字段存在且为空数组"）。
func mustRaw(t *testing.T, obj map[string]json.RawMessage, path ...string) []byte {
	t.Helper()
	cur := obj
	for i, key := range path {
		raw, ok := cur[key]
		require.Truef(t, ok, "缺少字段 %v", path[:i+1])
		if i == len(path)-1 {
			return raw
		}
		next := map[string]json.RawMessage{}
		require.NoError(t, json.Unmarshal(raw, &next))
		cur = next
	}
	return nil
}
