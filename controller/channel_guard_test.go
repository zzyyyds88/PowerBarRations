package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 基座单条删除：被车道引用时必须拒绝并保留渠道（此前会静默删除、留下悬空成员）。
func TestBaseDeleteChannelBlockedByLaneReference(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	ch := &model.Channel{Name: "del-guard", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m1"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "m1", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 1}}}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	id := strconv.Itoa(ch.Id)
	ctx.Params = gin.Params{{Key: "id", Value: id}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/"+id, nil)
	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "m1")
	var count int64
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", ch.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count, "被引用渠道必须保留")
}

// 基座批量删除：任一被引用就整批拒绝，不留半删状态。
func TestBaseDeleteChannelBatchBlockedByLaneReference(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	ch := &model.Channel{Name: "batch-guard", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m2"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "m2", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 1}}}))

	body, _ := common.Marshal(ChannelBatch{Ids: []int{ch.Id}})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	DeleteChannelBatch(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", ch.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count, "整批拒绝时渠道必须保留")
}
