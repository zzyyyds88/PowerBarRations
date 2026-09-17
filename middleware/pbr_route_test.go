package middleware

import (
	"net/http/httptest"
	"testing"

	"pbr/common"
	"pbr/constant"
	"pbr/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// X-Served-By 只描述"实际成功服务的成员"：失败/503 收尾必须清除，
// 否则下游会把重试链里最后选中的成员误当成服务者（design-v1 §4.1）。
func TestClearServedByHeaderRemovesHeaderAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	SetServedByHeader(c, &model.Channel{Id: 7, Name: "channel-a"}, &model.RouteMember{UpstreamModel: "model-x"})
	require.Equal(t, "channel=7:channel-a, model=model-x", c.Writer.Header().Get("X-Served-By"))
	_, hasContext := common.GetContextKey(c, constant.ContextKeyPBRServedBy)
	require.True(t, hasContext, "前提：选路已写入上下文记录")

	ClearServedByHeader(c)
	assert.Empty(t, c.Writer.Header().Get("X-Served-By"), "失败收尾必须删除响应头")
	_, hasContext = common.GetContextKey(c, constant.ContextKeyPBRServedBy)
	assert.False(t, hasContext, "上下文记录也应清除")

	// 幂等：未设置时清理不 panic。
	ClearServedByHeader(c)
	ClearServedByHeader(nil)
}
