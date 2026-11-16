package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy/redact"
	"github.com/zzyyyds88/PowerBarRations/model"
	relaytypes "github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupRelayPrivacyDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "relay-privacy.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.PBRRequestLog{}))
	previousDB := model.DB
	model.DB = db
	common.OptionMapRWMutex.Lock()
	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	privacy.Invalidate()
	t.Cleanup(func() {
		model.DB = previousDB
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousMap
		common.OptionMapRWMutex.Unlock()
		privacy.Invalidate()
	})
}

func privacyContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestPreparePrivacyMasksBodyAndRestoresSSE(t *testing.T) {
	setupRelayPrivacyDB(t)
	_, err := privacy.Save(privacy.Settings{
		Secrets: true,
		Model:   privacy.ModelSettings{Enabled: false, MinScore: 0.8},
	})
	require.NoError(t, err)

	c, recorder := privacyContext(t, `{"messages":[{"role":"user","content":"key sk-proj-abcdEFGH1234ijklMNOP5678qrst"}]}`)
	done, apiErr := preparePrivacy(c, relaytypes.RelayFormatOpenAI)
	require.Nil(t, apiErr)
	require.NotNil(t, done)
	carrier := model.GetPBRLogCarrier(c)
	require.NotNil(t, carrier)
	assert.Equal(t, 1, carrier.MaskedCount)
	assert.Equal(t, "ok", carrier.MaskStatus)
	assert.NotEmpty(t, carrier.MaskHits)

	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	masked, err := storage.Bytes()
	require.NoError(t, err)
	assert.NotContains(t, string(masked), "sk-proj-abcdEFGH1234ijklMNOP5678qrst")
	assert.Contains(t, string(masked), "{{API_KEY_")

	var hits []privacy.Hit
	require.NoError(t, json.Unmarshal([]byte(carrier.MaskHits), &hits))
	require.Len(t, hits, 1)
	placeholder := hits[0].Placeholder

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	_, _ = c.Writer.Write([]byte("data: {\"text\":\"" + placeholder[:5]))
	_, _ = c.Writer.Write([]byte(placeholder[5:] + "\"}\n\n"))
	done()
	assert.Contains(t, recorder.Body.String(), "sk-proj-abcdEFGH1234ijklMNOP5678qrst")
}

func TestPreparePrivacyBlock(t *testing.T) {
	setupRelayPrivacyDB(t)
	_, err := privacy.Save(privacy.Settings{
		Secrets: true,
		Actions: map[string]string{"API_KEY": redact.Block},
		Model:   privacy.ModelSettings{Enabled: false, MinScore: 0.8},
	})
	require.NoError(t, err)

	c, _ := privacyContext(t, `{"messages":[{"role":"user","content":"sk-proj-abcdEFGH1234ijklMNOP5678qrst"}]}`)
	done, apiErr := preparePrivacy(c, relaytypes.RelayFormatOpenAI)
	assert.Nil(t, done)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	carrier := model.GetPBRLogCarrier(c)
	require.NotNil(t, carrier)
	assert.True(t, carrier.MaskBlocked)
}

func TestPreparePrivacyOffLeavesBodyAlone(t *testing.T) {
	setupRelayPrivacyDB(t)
	c, _ := privacyContext(t, `{"messages":[{"role":"user","content":"sk-proj-abcdEFGH1234ijklMNOP5678qrst"}]}`)
	done, apiErr := preparePrivacy(c, relaytypes.RelayFormatOpenAI)
	assert.Nil(t, apiErr)
	assert.Nil(t, done)
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	body, err := storage.Bytes()
	require.NoError(t, err)
	assert.Contains(t, string(body), "sk-proj-abcdEFGH1234ijklMNOP5678qrst")
}
