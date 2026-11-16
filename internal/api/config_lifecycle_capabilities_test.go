package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /api/capabilities 是 AI 的发现入口（api-spec §5.1）：G5 起必须真实反映
// responses / files+batches（G6 前为 false 占位）/ metrics，且不删既有键。
func TestCapabilitiesExposeModelFeaturesAndMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	GetCapabilities(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))

	// 向后兼容：既有键一个都不能少。
	for _, key := range []string{"api_version", "lane_modes", "adapters", "inbound_formats", "circuit"} {
		assert.Contains(t, payload, key)
	}
	assert.Equal(t, "v1", payload["api_version"])

	features, ok := payload["features"].(map[string]any)
	require.True(t, ok, "features 缺失：%s", recorder.Body.String())
	expected := map[string]bool{
		"responses":         true,
		"count_tokens":      true,
		"images_variations": true,
		"moderations":       true,
		"files":             true,
		"batches":           true,
		"metrics":           true,
	}
	for feature, want := range expected {
		assert.Equal(t, want, features[feature], "features.%s", feature)
	}

	asyncObjects, ok := payload["async_objects"].(map[string]any)
	require.True(t, ok, "async_objects 缺失")
	assert.Equal(t, "X-PBR-Lane", asyncObjects["lane_header"])
	assert.Equal(t, true, asyncObjects["files"])
	assert.Equal(t, true, asyncObjects["batches"])

	metricCaps, ok := payload["metrics"].(map[string]any)
	require.True(t, ok, "metrics 缺失")
	assert.Equal(t, "/metrics", metricCaps["path"])
	assert.Equal(t, "prometheus", metricCaps["format"])
	assert.Equal(t, "admin_key", metricCaps["auth"])
}
