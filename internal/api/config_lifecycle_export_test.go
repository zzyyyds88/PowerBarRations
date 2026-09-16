package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pbr/common"
	"pbr/model"
	"pbr/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupImportTestDB 在共享测试库基础上补齐导入所需的表与 options 内存表，
// 使 applyImport（含 system_options 落库）可以真实跑通。
func setupImportTestDB(t *testing.T) {
	t.Helper()
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.ClientKey{}))
	originalMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = originalMap })
}

// 导出必须覆盖路由与成本折算所依赖的全部渠道字段（审查 F3）：
// 漏掉 model_mapping 会让恢复后的实例把路由键直发上游，漏掉 prices 会让成本折算归零。
func TestExportBundleIncludesChannelMappingAndPrices(t *testing.T) {
	setupImportTestDB(t)
	mapping := `{"model-1":"vendor-a/model-1"}`
	channel := &model.Channel{
		Name: "vendor-a", Type: 1, Key: "sk-secret", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "model-1,model-2", BaseURL: strPtr("https://vendor.example/v1"),
		ModelMapping: &mapping,
	}
	settingJSON, err := json.Marshal(dto.ChannelSettings{
		Proxy: "http://proxy.example:8080",
		PBRPrices: []dto.ChannelModelPrice{
			{Model: "model-1", Input: 10, Output: 20},
		},
	})
	require.NoError(t, err)
	setting := string(settingJSON)
	channel.Setting = &setting
	require.NoError(t, model.DB.Create(channel).Error)

	bundle, err := BuildConfigBundle()
	require.NoError(t, err)
	require.Len(t, bundle.Channels, 1)
	exported := bundle.Channels[0]
	assert.Equal(t, map[string]string{"model-1": "vendor-a/model-1"}, exported.ModelMapping)
	require.Len(t, exported.Prices, 1)
	assert.Equal(t, 10.0, exported.Prices[0].Input)
	assert.Equal(t, "http://proxy.example:8080", exported.Proxy)
	assert.True(t, exported.KeySet)

	// 明文密钥绝不能出现在导出体里。
	raw, err := json.Marshal(bundle)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "sk-secret")
}

// export → import(dry_run) 必须 diff 为空；改了 model_mapping 必须报 update。
// （此前摘要里没有 model_mapping/prices，改映射会被静默报成"无变更"。）
func TestImportRoundTripDiffStaysEmptyAndDetectsMappingChange(t *testing.T) {
	setupImportTestDB(t)
	mapping := `{"model-1":"real-a"}`
	channel := &model.Channel{
		Name: "vendor-a", Type: 1, Key: "sk-x", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "model-1", BaseURL: strPtr("https://vendor.example/v1"),
		ModelMapping: &mapping,
	}
	require.NoError(t, model.DB.Create(channel).Error)

	exported, err := BuildConfigBundle()
	require.NoError(t, err)
	fileBytes, err := json.Marshal(exported)
	require.NoError(t, err)

	var parsed ConfigBundle
	require.NoError(t, json.Unmarshal(fileBytes, &parsed))

	result := newImportResult(true)
	require.NoError(t, planImport(&parsed, result))
	diff := result.Diff["channels"].(DiffList)
	assert.Equal(t, []string{"vendor-a"}, diff.Unchanged, "未变更的渠道必须报 unchanged")
	assert.Empty(t, diff.Update)

	// 只改映射（其余不动）→ 必须报 update，而不是 unchanged。
	parsed.Channels[0].ModelMapping = map[string]string{"model-1": "real-b"}
	result = newImportResult(true)
	require.NoError(t, planImport(&parsed, result))
	diff = result.Diff["channels"].(DiffList)
	assert.Equal(t, []string{"vendor-a"}, diff.Update, "改 model_mapping 必须被 diff 捕获")

	// 真实导入把映射落库（覆盖原值），再导出应得到新映射。
	parsed.Channels[0].ModelMapping = map[string]string{"model-1": "real-b"}
	result = newImportResult(false)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, applyImport(c, &parsed, result))
	saved, err := findChannelByName("vendor-a")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"model-1": "real-b"}, saved.ModelMappingMap())

	// 显式空表 = 清空映射（缺席才表示保持原值）。
	parsed.Channels[0].ModelMapping = map[string]string{}
	result = newImportResult(false)
	require.NoError(t, applyImport(c, &parsed, result))
	saved, err = findChannelByName("vendor-a")
	require.NoError(t, err)
	assert.Empty(t, saved.ModelMappingMap())

	// 旧格式导出文件（没有 model_mapping/prices 字段）→ 保持原值，不清空。
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", saved.Id).
		Update("model_mapping", `{"model-1":"keep-me"}`).Error)
	var legacy ConfigBundle
	require.NoError(t, json.Unmarshal([]byte(`{"version":"v1","channels":[{"name":"vendor-a","type":"openai","base_url":"https://vendor.example/v1","models":["model-1"],"enabled":true,"key_set":true}]}`), &legacy))
	result = newImportResult(false)
	require.NoError(t, applyImport(c, &legacy, result))
	saved, err = findChannelByName("vendor-a")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"model-1": "keep-me"}, saved.ModelMappingMap(),
		"字段缺席 = 保持原值")
}

// 导入校验失败时响应体只能有一个 JSON 对象（审查 F6：helper 与 handler 双写）。
func TestImportWritesExactlyOneErrorBody(t *testing.T) {
	setupAPITestDB(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"version":"v1","channels":[{"name":"imp-ch","type":"openai","enabled":true,"models":["model-1"]}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	PostImport(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	decoder := json.NewDecoder(recorder.Body)
	var first map[string]any
	require.NoError(t, decoder.Decode(&first), "响应体必须是合法 JSON")
	errorBody, ok := first["error"].(map[string]any)
	require.True(t, ok, "响应体必须是错误包络: %s", recorder.Body.String())
	assert.Equal(t, "validation_failed", errorBody["code"])
	var trailing any
	err := decoder.Decode(&trailing)
	assert.ErrorIs(t, err, io.EOF, "响应体只能有一个 JSON 对象（不得双写）: %s", recorder.Body.String())
}

// dry-run 与真实导入必须对 system_options 给出同一个 changed 口径（审查 F13）。
func TestImportOptionsDiffMatchesDryRun(t *testing.T) {
	setupImportTestDB(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	bundle, err := BuildConfigBundle()
	require.NoError(t, err)
	require.NotNil(t, bundle.SystemOptions)

	// 未变更：dry-run 与真实导入都应报 changed 为空。
	dry := newImportResult(true)
	require.NoError(t, planImport(bundle, dry))
	assert.Empty(t, dry.Diff["options"].(gin.H)["changed"])

	applied := newImportResult(false)
	require.NoError(t, applyImport(c, bundle, applied))
	assert.Empty(t, applied.Diff["options"].(gin.H)["changed"],
		"真实导入未变更时不得谎报 changed")

	// 显式改一个选项：dry-run 与真实导入都必须报 changed。
	updated := *bundle.SystemOptions
	updated.LogRetentionDays = 7
	changedBundle := *bundle
	changedBundle.SystemOptions = &updated

	dry = newImportResult(true)
	require.NoError(t, planImport(&changedBundle, dry))
	assert.NotEmpty(t, dry.Diff["options"].(gin.H)["changed"])

	applied = newImportResult(false)
	require.NoError(t, applyImport(c, &changedBundle, applied))
	assert.NotEmpty(t, applied.Diff["options"].(gin.H)["changed"])
}

// PBRRequestLog.Attempts 的 json 字段名必须是 attempts（回归 F17 的拼写错误）。
func TestRequestLogAttemptsJSONFieldName(t *testing.T) {
	raw, err := json.Marshal(model.PBRRequestLog{Attempts: "[]"})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"attempts"`)
	assert.NotContains(t, string(raw), "expects_attempts")
}

func newImportResult(dryRun bool) *ImportResult {
	return &ImportResult{
		DryRun: dryRun,
		Valid:  true,
		Diff: map[string]any{
			"channels": newDiffList(),
			"lanes":    newDiffList(),
			"keys":     newDiffList(),
			"options":  gin.H{"changed": []string{}},
		},
	}
}
