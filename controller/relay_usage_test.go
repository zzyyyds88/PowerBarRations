package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/usage"
	"github.com/zzyyyds88/PowerBarRations/model"
	relaytypes "github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachUsageMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"hello"}]}`
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "claude-cli/1.0")
	c.Request.Header.Set(usage.SessionHeader, "session-1")

	attachUsageMetadata(c, relaytypes.RelayFormatOpenAI)

	carrier := model.GetPBRLogCarrier(c)
	require.NotNil(t, carrier)
	assert.Equal(t, "session-1", carrier.SessionID)
	assert.Equal(t, usage.SessionKey("session-1"), carrier.SessionKey)
	assert.Equal(t, "claude", carrier.Client)
	require.NotNil(t, carrier.Context)
	assert.Equal(t, usage.StatusOK, carrier.Context.Status)
	assert.Positive(t, carrier.Context.Tokens)
	assert.NotEmpty(t, carrier.Context.Parts)
	assert.Equal(t, len(body), carrier.ContextBodySize)
	assert.Empty(t, c.Request.Header.Get(usage.SessionHeader), "X-PBR-Session must not go upstream")
}

func TestAttachUsageMetadataSkipsChannelTest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	carrier := model.EnsurePBRLogCarrier(c)
	carrier.RouteSource = "test"

	attachUsageMetadata(c, relaytypes.RelayFormatOpenAI)

	assert.Empty(t, carrier.SessionID)
	assert.Empty(t, carrier.Client)
	assert.Nil(t, carrier.Context)
}
