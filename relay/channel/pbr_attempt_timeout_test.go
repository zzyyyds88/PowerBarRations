package channel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	rootconstant "github.com/zzyyyds88/PowerBarRations/constant"
)

// PBR 每成员超时必须区分"我方计时器触发"与"客户端断开"（routing-spec §8）：
// 计时器是 context.WithCancel，两者的底层错误都是 context.Canceled，唯一依据是 fired。
// 漏掉"等待响应头阶段"的翻译会让挂起型上游不触发任何故障转移（审查 F1）。
func TestPBRAttemptTimeoutWrapTranslatesFiredCancel(t *testing.T) {
	timeout := &pbrAttemptTimeout{}
	canceled := &pbrAttemptTimeoutErr{}

	// 未触发：客户端取消原样透传（Classify 会判 canceled：不换人、不冷却）。
	assert.ErrorIs(t, timeout.wrap(canceled), context.Canceled)
	assert.False(t, errors.Is(timeout.wrap(canceled), context.DeadlineExceeded))

	// 计时器已触发：必须翻译成 deadline，Classify 才会判 soft_transient 并换人。
	timeout.fired.Store(true)
	wrapped := timeout.wrap(canceled)
	assert.ErrorIs(t, wrapped, context.DeadlineExceeded)
	assert.ErrorIs(t, wrapped, context.Canceled, "原错误链保留，日志仍能看到底层原因")

	// 幂等：已经是 deadline 的错误不再包一层。
	already := context.DeadlineExceeded
	assert.Equal(t, already, timeout.wrap(already))

	// nil 安全：未配置超时（上下文键缺省）时 wrap 必须原样返回。
	var none *pbrAttemptTimeout
	assert.ErrorIs(t, none.wrap(canceled), context.Canceled)
	assert.NoError(t, none.wrap(nil))
}

// pbrAttemptTimeoutErr 模拟 url.Error 包装 context.Canceled 的形态。
type pbrAttemptTimeoutErr struct{}

func (e *pbrAttemptTimeoutErr) Error() string {
	return "Post \"https://upstream.example\": context canceled"
}
func (e *pbrAttemptTimeoutErr) Unwrap() error { return context.Canceled }

// 端到端一小步：startPBRAttemptTimeout 按 stream/非 stream 取不同上下文键，
// 且计时器触发后 wrap 才翻译（回归"读响应头阶段"这条路径）。
func TestStartPBRAttemptTimeoutUsesStreamKeyAndFires(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	base := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request = base
	common.SetContextKey(c, rootconstant.ContextKeyPBRStreamFirstEventTimeout, 1)
	common.SetContextKey(c, rootconstant.ContextKeyPBRNonStreamTimeout, 60)

	req, timeout := startPBRAttemptTimeout(c, base, true)
	require.NotNil(t, timeout)
	defer timeout.close()
	require.NotNil(t, req)

	select {
	case <-req.Context().Done():
		assert.ErrorIs(t, req.Context().Err(), context.Canceled, "计时器用 cancel 触发")
		assert.True(t, timeout.fired.Load())
		// 客户端看到的错误必须是超时（deadline），不是取消。
		assert.ErrorIs(t, timeout.wrap(req.Context().Err()), context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		t.Fatal("流式首事件超时未在预期时间内触发")
	}
}
