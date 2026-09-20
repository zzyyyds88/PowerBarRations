package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /doc 与 /llms.txt 是面向 AI 的**唯一内嵌手册**（api-spec §5.1）。它此前完全没有
// 测试，导致示例里的客户端密钥字段写成不存在的 `allowed_models` 也无人发现——
// 照抄的 AI 会写出"看起来成功、实际无权限约束"的密钥。
//
// 本文件把手册里"必须说对"的事实变成构建期失败：端点存在性、字段名、以及
// 已删除入口不得再出现。
func TestDocEndpointsServeGuide(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		path        string
		contentType string
		handler     gin.HandlerFunc
	}{
		{"/doc", "text/markdown", Doc},
		{"/llms.txt", "text/plain", LLMs},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
			tc.handler(c)

			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Contains(t, recorder.Header().Get("Content-Type"), tc.contentType)
			body := recorder.Body.String()
			assert.Contains(t, body, "Base64( SHA256(", "手册必须给出管理密钥派生规则")
			assert.Contains(t, body, "/api/openapi.json", "手册必须指向机器可读契约")
			assert.Contains(t, body, "/api/lanes", "手册必须列出车道端点")
		})
	}
}

// 手册必须用真实存在的 `lane_policy` 描述客户端密钥权限，
// 且**任何 JSON 示例里都不得出现**管理 API 从未接受过的 `allowed_models` 字段。
//
// 注意：正文里可以出现"没有 allowed_models 字段"这类**警示文字**，所以只禁止
// JSON 键形态（`"allowed_models"` 后跟冒号），而不是禁止这个词本身。
func TestDocGuideUsesRealClientKeyField(t *testing.T) {
	assert.Contains(t, apiGuide, "lane_policy")
	assert.NotContains(t, apiGuide, `"allowed_models":`,
		"allowed_models 不是管理 API 的字段，照抄会写出无权限约束的密钥")
}

// 已按 ADR 0005/0006 整体移除的入口不得在手册里被宣传为可用。
func TestDocGuideDoesNotAdvertiseRemovedEndpoints(t *testing.T) {
	for _, removed := range []string{"/api/lanes/seed", "/api/v1/lanes/seed"} {
		assert.NotContains(t, apiGuide, removed,
			"%s 已从代码与契约移除（ADR 0005/0006），手册不得再宣传", removed)
	}
}
