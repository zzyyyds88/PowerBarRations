package apiresp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEngine(t *testing.T, policy Policy, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ResetForTest()
	Register("POST", "/api/v1/probe", policy)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(Middleware())
	group.POST("/probe", handler)
	return engine
}

func doRequest(engine *gin.Engine) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil))
	return recorder
}

// 基座 success:true 且带 data → 裸化 data。
func TestSuccessUnwrapsData(t *testing.T) {
	engine := newEngine(t, Policy{}, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"name": "ch-a"}})
	})
	rec := doRequest(engine)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"name":"ch-a"}`, rec.Body.String())
}

// 基座 success:true 但无 data → 204。
func TestSuccessWithoutDataIsNoContent(t *testing.T) {
	engine := newEngine(t, Policy{}, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
	})
	assert.Equal(t, http.StatusNoContent, doRequest(engine).Code)
}

// 自定义成功体生效。
func TestCustomSuccessBody(t *testing.T) {
	engine := newEngine(t, Policy{Success: func(_ *gin.Context, _ Base) any {
		return gin.H{"changed": 3}
	}}, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": 3})
	})
	assert.JSONEq(t, `{"changed":3}`, doRequest(engine).Body.String())
}

// 基座 200 + success:false（无 code）→ 400 validation_failed。
func TestBusinessFailureBecomes400(t *testing.T) {
	engine := newEngine(t, Policy{}, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "参数错误"})
	})
	rec := doRequest(engine)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	assert.Equal(t, CodeValidationFailed, payload.Error.Code)
	assert.Equal(t, "参数错误", payload.Error.Message)
}

// 显式 code=conflict → 409 + details.blocked。
func TestConflictMapsTo409WithDetails(t *testing.T) {
	policy := Policy{
		Failures: []FailureRule{{BaseCode: "conflict", OutStatus: http.StatusConflict, OutCode: CodeConflict}},
		Details:  blockedDetails,
	}
	engine := newEngine(t, policy, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": false, "code": "conflict", "message": "被引用", "data": gin.H{"blocked": gin.H{"ch-a": []string{"m1"}}}})
	})
	rec := doRequest(engine)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"conflict"`)
	assert.Contains(t, rec.Body.String(), `"blocked"`)
}

// 基座自己给的 404 不得被降级成 500。
func TestBaseStatusIsPreserved(t *testing.T) {
	engine := newEngine(t, Policy{}, func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "not found"})
	})
	assert.Equal(t, http.StatusNotFound, doRequest(engine).Code)
}

// 非信封（已是裸资源）原样透传。
func TestBareResourcePassesThrough(t *testing.T) {
	engine := newEngine(t, Policy{}, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"items": []string{}, "next_cursor": nil})
	})
	rec := doRequest(engine)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"items":[],"next_cursor":null}`, rec.Body.String())
}

// SSE 不缓冲、不改写。
func TestSSEPassesThrough(t *testing.T) {
	engine := newEngine(t, Policy{}, func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
	})
	rec := doRequest(engine)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "data: [DONE]")
	assert.NotContains(t, rec.Body.String(), `"error"`)
}

// SSE 走 Flush（gin 的 c.Writer.Flush 与 http.Flusher 断言都到 WriteHeader/Write）时
// 必须立即直通——SSE handler 通常不返回，缓冲会让客户端永远收不到字节。
func TestSSEPromotesOnWriteHeader(t *testing.T) {
	engine := newEngine(t, Policy{}, func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.Write([]byte("event: x\ndata: {}\n\n"))
		c.Writer.Flush()
		// 故意不返回：模拟无限推流；缓冲实现下客户端会一个字节都收不到。
	})
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未在 2s 内写出流式数据（响应被缓冲了）")
	}
	assert.Contains(t, recorder.Body.String(), "event: x")
}

// 未登记路由回落 Default：仍然裸化，不漏信封。
func TestUnregisteredRouteUsesDefault(t *testing.T) {
	ResetForTest()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(Middleware())
	group.POST("/anything", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"ok": 1}})
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/anything", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"ok":1}`, recorder.Body.String())
}

// 前缀归一化：/api 与 /api/v1 共用一份登记。
func TestPrefixNormalization(t *testing.T) {
	ResetForTest()
	Register("GET", "/api/x", Policy{Passthrough: true})
	for _, path := range []string{"/api/x", "/api/v1/x"} {
		_, ok := Lookup("GET", path)
		assert.True(t, ok, path)
	}
}
