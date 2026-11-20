package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy/redact"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupPrivacyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "privacy.db")),
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
	return db
}

func privacyRequest(t *testing.T, method, target string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestPrivacySettingsWriteReadAndDryRun(t *testing.T) {
	setupPrivacyTestDB(t)
	in := privacy.Settings{
		Secrets: true,
		Kinds:   map[string]bool{"ip": true},
		Words:   []string{"Nightjar", "nightjar", " "},
		Rules:   []redact.Rule{{Kind: "GATEWAY_KEY", Prefix: "oc_sk_"}},
		Actions: map[string]string{"API_KEY": "block"},
		Allow:   []string{"ops@acme.io", "OPS@acme.io"},
		Model:   privacy.ModelSettings{Enabled: false, MinScore: 0.8},
	}
	c, recorder := privacyRequest(t, http.MethodPut, "/api/v1/privacy/settings", in)
	UpdatePrivacySettings(c)
	require.Equal(t, http.StatusOK, c.Writer.Status(), recorder.Body.String())

	var saved privacy.Settings
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &saved))
	assert.True(t, saved.Secrets)
	assert.True(t, saved.Kinds["ip"])
	assert.Equal(t, []string{"Nightjar"}, saved.Words)
	assert.Equal(t, "block", saved.Actions["API_KEY"])
	assert.Equal(t, []string{"ops@acme.io"}, saved.Allow)
	assert.True(t, saved.Active)

	// dry-run must not change what is stored.
	dry := privacy.Settings{Secrets: false, Words: []string{"other"}}
	c2, _ := privacyRequest(t, http.MethodPut, "/api/v1/privacy/settings?dry_run=true", dry)
	UpdatePrivacySettings(c2)
	require.Equal(t, http.StatusOK, c2.Writer.Status())
	loaded, err := privacy.Load()
	require.NoError(t, err)
	assert.True(t, loaded.Secrets, "dry-run wrote the settings")

	// Unknown action is a 422, and nothing is written.
	bad := privacy.Settings{Secrets: true, Actions: map[string]string{"API_KEY": "drop"}}
	c3, _ := privacyRequest(t, http.MethodPut, "/api/v1/privacy/settings", bad)
	UpdatePrivacySettings(c3)
	assert.Equal(t, http.StatusUnprocessableEntity, c3.Writer.Status())
}

func TestPrivacyPreview(t *testing.T) {
	setupPrivacyTestDB(t)
	in := privacy.Settings{Secrets: true, Model: privacy.ModelSettings{Enabled: false, MinScore: 0.8}}
	c := privacyRequestNoBody(t, http.MethodPut, "/api/v1/privacy/settings", in)
	UpdatePrivacySettings(c)
	require.Equal(t, http.StatusOK, c.Writer.Status())

	c2, recorder := privacyRequest(t, http.MethodPost, "/api/v1/privacy/preview",
		map[string]string{"text": "key sk-proj-abcdEFGH1234ijklMNOP5678qrst"})
	PreviewPrivacy(c2)
	require.Equal(t, http.StatusOK, c2.Writer.Status())
	var out struct {
		Text   string        `json:"text"`
		Hits   []privacy.Hit `json:"hits"`
		Active bool          `json:"active"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &out))
	assert.True(t, out.Active)
	assert.Contains(t, out.Text, "{{API_KEY_")
	assert.Len(t, out.Hits, 1)
	assert.NotContains(t, out.Text, "sk-proj-abcdEFGH1234ijklMNOP5678qrst")
}

func privacyRequestNoBody(t *testing.T, method, target string, body any) *gin.Context {
	t.Helper()
	c, _ := privacyRequest(t, method, target, body)
	return c
}

func TestPrivacyRecordsAndSummary(t *testing.T) {
	db := setupPrivacyTestDB(t)
	ts := time.Now().Add(-time.Hour).Unix()
	hits := `[{"kind":"API_KEY","by":"rule","placeholder":"{{API_KEY_k3v9x2mq}}","head":"sk-","len":51,"n":2,"action":""}]`
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: ts, LaneName: "lane-a", RequestModel: "model-a", Success: true,
		MaskedCount: 2, MaskStatus: "ok", MaskHits: hits,
	}).Error)
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: ts + 1, LaneName: "lane-a", RequestModel: "model-b", Success: false,
		MaskedCount: 1, MaskBlocked: true, MaskStatus: "ok",
		MaskHits: `[{"kind":"EMAIL","by":"rule","placeholder":"{{EMAIL_abcdefgh}}","head":"o","len":12,"n":1,"action":"block"}]`,
	}).Error)

	c, recorder := privacyRequest(t, http.MethodGet, "/api/v1/privacy/records?kind=API_KEY", nil)
	ListPrivacyRecords(c)
	require.Equal(t, http.StatusOK, c.Writer.Status(), recorder.Body.String())
	var list struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &list))
	assert.Equal(t, 1, list.Total)
	assert.Len(t, list.Items, 1)
	assert.NotEmpty(t, list.Items[0]["mask_hits"])

	c2, recorder2 := privacyRequest(t, http.MethodGet, "/api/v1/privacy/summary?days=7", nil)
	GetPrivacySummary(c2)
	require.Equal(t, http.StatusOK, c2.Writer.Status(), recorder2.Body.String())
	var summary struct {
		Masked   int64 `json:"masked_values"`
		Requests int   `json:"requests"`
		Blocked  int64 `json:"blocked"`
	}
	require.NoError(t, json.Unmarshal(recorder2.Body.Bytes(), &summary))
	assert.EqualValues(t, 3, summary.Masked)
	assert.Equal(t, 2, summary.Requests)
	assert.EqualValues(t, 1, summary.Blocked)
}

// 推荐基线是只读语义：接口返回的形状必须与 privacy.Recommended() 一致，
// 且三个总开关为 false——调用这个端点不会开启脱敏。
func TestPrivacyRecommended(t *testing.T) {
	setupPrivacyTestDB(t)

	c, recorder := privacyRequest(t, http.MethodGet, "/api/privacy/recommended", nil)
	GetPrivacyRecommended(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var got privacy.Settings
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &got))
	assert.False(t, got.Active)
	assert.False(t, got.Secrets)
	assert.False(t, got.Personal)
	assert.False(t, got.Model.Enabled)
	assert.Equal(t, privacy.DefaultModelMinScore, got.Model.MinScore)
	assert.Equal(t, privacy.Recommended().Kinds, got.Kinds)

	// 调用前后当前设置不变（推荐基线不是写入）。
	current, err := privacy.Load()
	require.NoError(t, err)
	assert.False(t, current.Active, "读取推荐基线不得打开脱敏")
}
