package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/apiresp"
	"github.com/zzyyyds88/PowerBarRations/internal/testutil/fakeupstream"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 本文件补齐「登记在 opsRoutes 表、但此前没有任何行为测试」的 5 个端点：
// DELETE /channels/disabled、DELETE /channels/{name}/ollama/models、
// DELETE /prefill-groups/{id}、DELETE /system/log-files、
// DELETE /system/performance/disk-cache。
//
// 断言口径是契约响应（api-spec §5.3.1–§5.3.4），因此全部走真实的
// apiresp.Middleware + opsRoutes 策略，而不是直接看基座 {success,message,data} 信封。

func newOpsContractEngine(t *testing.T) *gin.Engine {
	t.Helper()
	apiresp.ResetForTest()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(apiresp.Middleware())
	RegisterOpsRoutes(group)
	t.Cleanup(apiresp.ResetForTest)
	return engine
}

func doOpsRequest(engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder
}

func decodeOpsJSON(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &out), recorder.Body.String())
	return out
}

func setupOpsContractTest(t *testing.T) (*gin.Engine, *gorm.DB) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PrefillGroup{}))
	return newOpsContractEngine(t), db
}

// DELETE /api/channels/disabled：任一被车道引用的已禁用渠道 → 整批 409，
// 且机器可判定的 details.blocked 必须保留（api-spec §5.3.1）。
func TestDeleteDisabledChannelsBlockedByLane(t *testing.T) {
	engine, db := setupOpsContractTest(t)

	ch := &model.Channel{Name: "dead-ch", Type: 1, Key: "sk", Status: common.ChannelStatusManuallyDisabled, Group: "default", Models: "m"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, db.Create(&model.Lane{Name: "m", Enabled: true, Mode: model.LaneModeFailover}).Error)
	var lane model.Lane
	require.NoError(t, db.Where("name = ?", "m").First(&lane).Error)
	require.NoError(t, db.Create(&model.LaneMember{LaneId: lane.Id, ChannelId: ch.Id, Model: "m", Priority: 1}).Error)

	recorder := doOpsRequest(engine, http.MethodDelete, "/api/v1/channels/disabled", "")
	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	body := decodeOpsJSON(t, recorder)
	errObj, ok := body["error"].(map[string]any)
	require.True(t, ok, recorder.Body.String())
	assert.Equal(t, "conflict", errObj["code"])
	details, ok := errObj["details"].(map[string]any)
	require.True(t, ok, "409 必须带 details.blocked：%s", recorder.Body.String())
	blocked, ok := details["blocked"].(map[string]any)
	require.True(t, ok, recorder.Body.String())
	assert.Contains(t, blocked, "dead-ch", "被引用渠道名必须在 blocked 里")

	var stillThere int64
	require.NoError(t, db.Model(&model.Channel{}).Where("name = ?", "dead-ch").Count(&stillThere).Error)
	assert.EqualValues(t, 1, stillThere, "整批拒绝时渠道不得被删")
}

// DELETE /api/channels/disabled：无引用的已禁用渠道 → 200 {"deleted":n}。
func TestDeleteDisabledChannelsDeletesUnreferenced(t *testing.T) {
	engine, db := setupOpsContractTest(t)

	require.NoError(t, db.Create(&model.Channel{Name: "dead-ch", Type: 1, Key: "sk", Status: common.ChannelStatusManuallyDisabled, Group: "default", Models: "m"}).Error)
	require.NoError(t, db.Create(&model.Channel{Name: "live-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m"}).Error)

	recorder := doOpsRequest(engine, http.MethodDelete, "/api/v1/channels/disabled", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decodeOpsJSON(t, recorder)
	assert.EqualValues(t, 1, body["deleted"])

	var names []string
	require.NoError(t, db.Model(&model.Channel{}).Pluck("name", &names).Error)
	assert.Equal(t, []string{"live-ch"}, names)
}

// DELETE /api/prefill-groups/{id}：不存在 → 404 prefill_group_not_found（不是 400/500）。
func TestDeletePrefillGroupNotFound(t *testing.T) {
	engine, _ := setupOpsContractTest(t)

	recorder := doOpsRequest(engine, http.MethodDelete, "/api/v1/prefill-groups/999999", "")
	require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
	body := decodeOpsJSON(t, recorder)
	errObj := body["error"].(map[string]any)
	assert.Equal(t, "prefill_group_not_found", errObj["code"])
}

// DELETE /api/prefill-groups/{id}：{id} 非数字 → 400；成功 → {"deleted":true,"id":n} 且落库消失。
func TestDeletePrefillGroupSuccess(t *testing.T) {
	engine, db := setupOpsContractTest(t)

	bad := doOpsRequest(engine, http.MethodDelete, "/api/v1/prefill-groups/abc", "")
	require.Equal(t, http.StatusBadRequest, bad.Code, bad.Body.String())

	group := &model.PrefillGroup{Name: "sweep-group", Type: "model"}
	require.NoError(t, group.Insert())
	recorder := doOpsRequest(engine, http.MethodDelete, "/api/v1/prefill-groups/"+strconv.Itoa(group.Id), "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decodeOpsJSON(t, recorder)
	assert.Equal(t, true, body["deleted"])
	assert.EqualValues(t, group.Id, body["id"])

	var count int64
	require.NoError(t, db.Model(&model.PrefillGroup{}).Where("id = ?", group.Id).Count(&count).Error)
	assert.Zero(t, count, "删除后回读必须为空")
}

// DELETE /api/system/log-files：mode 非法 → 400；by_count 保留最新 N 个并回读成功体三字段。
func TestCleanupLogFilesContract(t *testing.T) {
	engine, _ := setupOpsContractTest(t)

	previousLogDir := *common.LogDir
	dir := t.TempDir()
	*common.LogDir = dir
	t.Cleanup(func() { *common.LogDir = previousLogDir })

	invalid := doOpsRequest(engine, http.MethodDelete, "/api/v1/system/log-files?mode=bogus&value=1", "")
	require.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())

	for _, name := range []string{
		"oneapi-20260101000000.log",
		"oneapi-20260102000000.log",
		"oneapi-20260103000000.log",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("log-line"), 0o644))
	}

	recorder := doOpsRequest(engine, http.MethodDelete, "/api/v1/system/log-files?mode=by_count&value=1", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decodeOpsJSON(t, recorder)
	assert.EqualValues(t, 2, body["deleted_count"], "保留最新 1 个，应删 2 个：%s", recorder.Body.String())
	assert.Greater(t, body["freed_bytes"].(float64), float64(0))
	assert.Empty(t, body["failed_files"])

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "oneapi-20260103000000.log", entries[0].Name())
}

// 部分删除失败 → 500 + error.details.failed_files（api-spec §5.3.2）。
//
// 直接构造基座的 partial_failure 信封并复用同一条策略，避免依赖"让 os.Remove 失败"
// 这种不可移植的文件系统技巧。
func TestLogFilesPartialFailureMapsTo500WithDetails(t *testing.T) {
	apiresp.ResetForTest()
	t.Cleanup(apiresp.ResetForTest)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(apiresp.Middleware())
	apiresp.Register(http.MethodDelete, "/api/v1/test-log-files-partial", opsPolicyLogFilesCleanup())
	group.DELETE("/test-log-files-partial", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"code":    "partial_failure",
			"message": "部分文件删除失败",
			"data":    gin.H{"deleted_count": 1, "freed_bytes": 4, "failed_files": []string{"oneapi-locked.log"}},
		})
	})

	recorder := doOpsRequest(engine, http.MethodDelete, "/api/v1/test-log-files-partial", "")
	require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	body := decodeOpsJSON(t, recorder)
	errObj := body["error"].(map[string]any)
	assert.Equal(t, "internal_error", errObj["code"])
	details := errObj["details"].(map[string]any)
	assert.Equal(t, []any{"oneapi-locked.log"}, details["failed_files"])
}

// DELETE /api/system/performance/disk-cache：清理不活跃缓存 → 200 {"cleared":true}。
func TestClearDiskCacheContract(t *testing.T) {
	engine, _ := setupOpsContractTest(t)

	previous := common.GetDiskCacheConfig()
	dir := t.TempDir()
	common.SetDiskCacheConfig(common.DiskCacheConfig{Path: dir})
	t.Cleanup(func() { common.SetDiskCacheConfig(previous) })

	cacheDir := common.GetDiskCacheDir()
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	stale := filepath.Join(cacheDir, "stale-cache.bin")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o644))
	old := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	recorder := doOpsRequest(engine, http.MethodDelete, "/api/v1/system/performance/disk-cache", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decodeOpsJSON(t, recorder)
	assert.Equal(t, true, body["cleared"])

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("不活跃缓存文件应被清理：err=%v", err)
	}
}

// DELETE /api/channels/{name}/ollama/models：非 Ollama 渠道 → 400；
// Ollama 渠道 → 200 语义化成功体，且假上游真的收到 DELETE /api/delete。
func TestOllamaDeleteModelContract(t *testing.T) {
	engine, db := setupOpsContractTest(t)

	require.NoError(t, db.Create(&model.Channel{Name: "plain-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m"}).Error)
	notOllama := doOpsRequest(engine, http.MethodDelete, "/api/v1/channels/plain-ch/ollama/models", `{"model_name":"m"}`)
	require.Equal(t, http.StatusBadRequest, notOllama.Code, notOllama.Body.String())
	assert.Equal(t, "validation_failed", decodeOpsJSON(t, notOllama)["error"].(map[string]any)["code"])

	upstream := fakeupstream.New(fakeupstream.Config{})
	t.Cleanup(upstream.Close)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Name: "ollama-ch", Type: constant.ChannelTypeOllama, Key: "sk", BaseURL: &baseURL,
		Status: common.ChannelStatusEnabled, Group: "default", Models: "m",
	}).Error)

	recorder := doOpsRequest(engine, http.MethodDelete, "/api/v1/channels/ollama-ch/ollama/models", `{"model_name":"m"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decodeOpsJSON(t, recorder)
	assert.Equal(t, "ollama-ch", body["channel"])
	assert.Equal(t, "m", body["model"])
	assert.Equal(t, true, body["deleted"])

	last, ok := upstream.LastRequest()
	require.True(t, ok, "假上游必须真的收到删除请求")
	assert.Equal(t, http.MethodDelete, last.Method)
	assert.Equal(t, "/api/delete", last.Path)
}
