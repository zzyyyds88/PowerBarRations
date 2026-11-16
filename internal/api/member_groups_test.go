package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 成员组管理 API（api-spec §5.2.1、ADR 0009）：CRUD、车道链两态写入、展开后判重、
// unknown group、级联删除的 HTTP 形态。

func seedGroupChannel(t *testing.T, name string, models ...string) *model.Channel {
	t.Helper()
	ch := &model.Channel{Name: name, Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: commaJoin(models)}
	require.NoError(t, model.DB.Create(ch).Error)
	return ch
}

func commaJoin(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

func TestMemberGroupCRUD(t *testing.T) {
	setupAPITestDB(t)
	gin.SetMode(gin.TestMode)
	seedGroupChannel(t, "g-ch", "g-m1", "g-m2")

	// 创建：409 重名 / 422 空成员 / 200 写后回读。
	body := `{"name":"grp-1","members":[
		{"channel":"g-ch","model":"g-m1","priority":2},
		{"channel":"g-ch","model":"g-m2","priority":1}]}`
	rec := callAPI(t, http.MethodPost, "/api/v1/member-groups", body, CreateMemberGroup, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var created struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Members []struct {
			Channel string `json:"channel"`
			Model   string `json:"model"`
		} `json:"members"`
		ReferencedBy []string `json:"referenced_by"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "grp-1", created.Name)
	assert.True(t, created.Enabled)
	require.Len(t, created.Members, 2)
	assert.Equal(t, "g-m1", created.Members[0].Model, "组内按 priority 降序")
	assert.Empty(t, created.ReferencedBy)

	rec = callAPI(t, http.MethodPost, "/api/v1/member-groups", body, CreateMemberGroup, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "重名 409")

	rec = callAPI(t, http.MethodPost, "/api/v1/member-groups",
		`{"name":"grp-empty","members":[]}`, CreateMemberGroup, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "空成员 422")

	rec = callAPI(t, http.MethodPost, "/api/v1/member-groups",
		`{"name":"grp-dup","members":[
			{"channel":"g-ch","model":"g-m1"},
			{"channel":"g-ch","model":"g-m1"}]}`, CreateMemberGroup, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "组内 (渠道, 模型) 重复 422")
	assert.Contains(t, rec.Body.String(), "duplicate_member")

	rec = callAPI(t, http.MethodPost, "/api/v1/member-groups",
		`{"name":"grp-ghost","members":[{"channel":"no-such-ch","model":"x"}]}`, CreateMemberGroup, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "成员渠道不存在 422")

	// PUT：members 省略 = 保留（只关组全局开关）。
	rec = callAPI(t, http.MethodPut, "/api/v1/member-groups/grp-1",
		`{"enabled":false}`, UpdateMemberGroup, gin.Params{{Key: "name", Value: "grp-1"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	saved, err := model.GetMemberGroupByName("grp-1")
	require.NoError(t, err)
	assert.True(t, saved.Disabled, "组全局关生效")
	require.Len(t, saved.Members, 2, "members 省略 = 保留组内成员")

	// PUT 显式 [] → 422 group_has_no_members。
	rec = callAPI(t, http.MethodPut, "/api/v1/member-groups/grp-1",
		`{"members":[]}`, UpdateMemberGroup, gin.Params{{Key: "name", Value: "grp-1"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "group_has_no_members")

	// 详情 404。
	rec = callAPI(t, http.MethodGet, "/api/v1/member-groups/no-such", "", GetMemberGroup,
		gin.Params{{Key: "name", Value: "no-such"}})
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "member_group_not_found")
}

// 车道链两态写入：组引用元素 + 展开后判重 + unknown group。
func TestLaneWithGroupReference(t *testing.T) {
	setupAPITestDB(t)
	gin.SetMode(gin.TestMode)
	chA := seedGroupChannel(t, "lg-ch-a", "lg-m1")
	chB := seedGroupChannel(t, "lg-ch-b", "lg-m2")
	rec := callAPI(t, http.MethodPost, "/api/v1/member-groups",
		`{"name":"lg-grp","members":[
			{"channel":"lg-ch-a","model":"lg-m1","priority":2},
			{"channel":"lg-ch-b","model":"lg-m2","priority":1}]}`, CreateMemberGroup, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// 两态链写入：散成员 + 组引用交错。**组不排他（ADR 0009）**：散成员
	// (lg-ch-a, lg-m1) 与组内成员重叠是合法配置——两个独立条目各参与一次选路。
	rec = callAPI(t, http.MethodPut, "/api/v1/lanes/lg-lane", `{
		"members":[
			{"channel":"lg-ch-a","model":"lg-m1","priority":3},
			{"group":"lg-grp","priority":2},
			{"channel":"lg-ch-b","model":"lg-m2","priority":1}
		]}`, PutLane, gin.Params{{Key: "name", Value: "lg-lane"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var overlapped struct {
		Members []map[string]any `json:"members"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &overlapped))
	require.Len(t, overlapped.Members, 3, "组引用不展开进链元素：链 = 散成员+组引用+散成员")
	// 散成员之间重复仍 422。
	rec = callAPI(t, http.MethodPut, "/api/v1/lanes/lg-lane-dup", `{
		"members":[
			{"channel":"lg-ch-a","model":"lg-m1","priority":2},
			{"channel":"lg-ch-a","model":"lg-m1","priority":1}
		]}`, PutLane, gin.Params{{Key: "name", Value: "lg-lane-dup"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "散成员之间重复仍 422")
	assert.Contains(t, rec.Body.String(), "duplicate_member")

	// 换一条不含重叠的车道验证两态回读。
	seedGroupChannel(t, "lg-ch-c", "lg-m3")
	rec = callAPI(t, http.MethodPut, "/api/v1/lanes/lg-lane", `{
		"members":[
			{"group":"lg-grp","priority":2},
			{"channel":"lg-ch-c","model":"lg-m3","priority":1}
		]}`, PutLane, gin.Params{{Key: "name", Value: "lg-lane"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var lane struct {
		Members []map[string]any `json:"members"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &lane))
	require.Len(t, lane.Members, 2)
	assert.Equal(t, "lg-grp", lane.Members[0]["group"], "组引用元素回传组名")
	assert.Equal(t, true, lane.Members[0]["group_enabled"])
	assert.Equal(t, float64(2), lane.Members[0]["group_member_count"])
	assert.Equal(t, float64(2), lane.Members[0]["priority"])
	assert.Equal(t, "lg-ch-c", lane.Members[1]["channel"])

	// 解析展开：两态链展开成 3 个成员（组 2 + 散 1）。
	resolved, err := model.ResolveRoute("lg-lane")
	require.NoError(t, err)
	require.Len(t, resolved.Members, 3)

	// 引用不存在的组 → 422 unknown_member_group。
	rec = callAPI(t, http.MethodPut, "/api/v1/lanes/lg-lane2",
		`{"members":[{"group":"ghost-grp","priority":1}]}`, PutLane,
		gin.Params{{Key: "name", Value: "lg-lane2"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "unknown_member_group")

	_ = chA
	_ = chB
}

// 删除组：级联清理 + cleaned/deleted lanes 形状。
func TestDeleteMemberGroupEndpoint(t *testing.T) {
	setupAPITestDB(t)
	gin.SetMode(gin.TestMode)
	seedGroupChannel(t, "dg-ch", "dg-m")
	rec := callAPI(t, http.MethodPost, "/api/v1/member-groups",
		`{"name":"dg-grp","members":[{"channel":"dg-ch","model":"dg-m","priority":1}]}`, CreateMemberGroup, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = callAPI(t, http.MethodPut, "/api/v1/lanes/dg-lane",
		`{"members":[{"group":"dg-grp","priority":1}]}`, PutLane,
		gin.Params{{Key: "name", Value: "dg-lane"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = callAPI(t, http.MethodDelete, "/api/v1/member-groups/dg-grp", "", DeleteMemberGroup,
		gin.Params{{Key: "name", Value: "dg-grp"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Deleted      bool     `json:"deleted"`
		CleanedLanes []string `json:"cleaned_lanes"`
		DeletedLanes []string `json:"deleted_lanes"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Deleted)
	assert.ElementsMatch(t, []string{"dg-lane"}, resp.DeletedLanes, "链空车道整条删除")
}
