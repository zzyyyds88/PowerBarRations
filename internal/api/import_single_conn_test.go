package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 导入必须在 SQLite 连接池 = 1 时不发生自锁（生产默认值，见 model/main.go）。
//
// 回归背景：applyImport 曾在事务内调用全局 DB 的 GetLaneByName / GetMemberGroupByName /
// ValidateLaneActiveMember / laneDigestOfLane / groupDigestOfGroup 等，等待第二条连接
// 时永久阻塞；L2 ops_runbook 的"导出→清空→导入"步骤因此挂死。
//
// 用例刻意把 MaxOpenConns 钉成 1，并在 10s 超时内要求 applyImport 返回：
// 任何事务内再次引入全局查询都会让本用例失败，而不是在生产 import 时才暴露。
func TestImportDoesNotDeadlockWithSingleConnection(t *testing.T) {
	setupImportTestDB(t)
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	enabled := true
	bundle := &ConfigBundle{
		Version: "v1",
		Channels: []ChannelConfig{
			{
				Name:    "tx-ch",
				Type:    "openai",
				BaseURL: testStringPtr("https://vendor.example/v1"),
				Models:  []string{"m", "m2"},
				Enabled: &enabled,
			},
		},
		MemberGroups: []MemberGroupConfig{
			{
				Name:    "tx-group",
				Enabled: enabled,
				Members: []GroupMemberConfig{
					{Channel: "tx-ch", Model: "m", Priority: 1},
				},
			},
		},
		Lanes: []LaneConfig{
			{
				Name:         "tx-lane",
				Enabled:      &enabled,
				Mode:         testStringPtr(model.LaneModeManual),
				ActiveMember: "tx-ch/m2",
				Members: []LaneMemberConfig{
					{Group: "tx-group"},
					{Channel: "tx-ch", Model: "m2", Priority: 1},
				},
			},
		},
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	result := newImportResult(false)
	done := make(chan error, 1)
	go func() {
		done <- applyImport(c, bundle, result)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("applyImport 在 SQLite 单连接下未返回（事务内仍有全局 DB 查询导致自锁）")
	}

	lane, err := model.GetLaneByName("tx-lane")
	require.NoError(t, err)
	assert.Equal(t, model.LaneModeManual, lane.Mode)
	assert.Equal(t, "tx-ch/m2", lane.ActiveMember)
	require.Len(t, lane.Members, 2)
	groupRefFound := false
	for i := range lane.Members {
		if lane.Members[i].GroupId != 0 {
			groupRefFound = true
		}
	}
	assert.True(t, groupRefFound, "组引用必须已落库并回填 group_id")

	group, err := model.GetMemberGroupByName("tx-group")
	require.NoError(t, err)
	require.Len(t, group.Members, 1)
	assert.Equal(t, "m", group.Members[0].Model)
}
