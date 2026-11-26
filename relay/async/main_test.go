package async

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open async test db: " + err.Error())
	}
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	model.InitColumnNames()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&model.Channel{},
		&model.Lane{},
		&model.LaneMember{},
		&model.PBRRequestLog{},
		&model.PBRStatsHourly{},
		&model.PBRStatsOutbox{},
	); err != nil {
		panic("failed to migrate async test db: " + err.Error())
	}
	if err := model.AutoMigrateAsyncObjects(db); err != nil {
		panic("failed to migrate async objects: " + err.Error())
	}
	if err := model.AutoMigratePBRFiles(db); err != nil {
		panic("failed to migrate pbr files: " + err.Error())
	}
	os.Exit(m.Run())
}

func resetAsyncState(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"pbr_async_objects",
		"pbr_files",
		"pbr_request_logs",
		"pbr_stats_hourly",
		"pbr_stats_outbox",
		"lane_members",
		"lanes",
		"channels",
	} {
		require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
	}
}

type recordedCall struct {
	method string
	path   string
	query  string
	header http.Header
	body   []byte
}

type upstreamRecorder struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (u *upstreamRecorder) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		u.mu.Lock()
		u.calls = append(u.calls, recordedCall{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			header: r.Header.Clone(),
			body:   body,
		})
		u.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (u *upstreamRecorder) snapshot() []recordedCall {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]recordedCall(nil), u.calls...)
}

func (u *upstreamRecorder) count() int {
	return len(u.snapshot())
}

func newUpstream(t *testing.T, recorder *upstreamRecorder, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(recorder.middleware(mux))
	t.Cleanup(server.Close)
	return server
}

func newChannelWithLane(t *testing.T, laneName string, channelType int, baseURL string) *model.Channel {
	t.Helper()
	channel := newBareChannel(t, "ch-"+laneName, channelType, baseURL)

	lane := &model.Lane{
		Name:      laneName,
		Mode:      model.LaneModeFailover,
		Enabled:   true,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, model.DB.Create(lane).Error)
	require.NoError(t, model.DB.Create(&model.LaneMember{
		LaneId:    lane.Id,
		ChannelId: channel.Id,
		Model:     "test-model",
		Priority:  0,
	}).Error)
	t.Cleanup(func() { route.Default.Remove(laneName) })
	return channel
}

func newBareChannel(t *testing.T, name string, channelType int, baseURL string) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Name:    name,
		Type:    channelType,
		Key:     "sk-test",
		Status:  common.ChannelStatusEnabled,
		Models:  "test-model",
		Group:   "default",
		BaseURL: &baseURL,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	return channel
}

func addChannelToLane(t *testing.T, laneName string, channelType int, baseURL string, priority int) *model.Channel {
	t.Helper()
	channel := newBareChannel(t, fmt.Sprintf("ch-%s-p%d", laneName, priority), channelType, baseURL)
	lane, err := model.GetLaneByName(laneName)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.LaneMember{
		LaneId:    lane.Id,
		ChannelId: channel.Id,
		Model:     "test-model",
		Priority:  priority,
	}).Error)
	return channel
}

func newLaneOnly(t *testing.T, laneName string) *model.Lane {
	t.Helper()
	lane := &model.Lane{
		Name:      laneName,
		Mode:      model.LaneModeFailover,
		Enabled:   true,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, model.DB.Create(lane).Error)
	t.Cleanup(func() { route.Default.Remove(laneName) })
	return lane
}

func newEngine(t *testing.T, h *Handler) *gin.Engine {
	t.Helper()
	engine := gin.New()
	h.RegisterRoutes(engine.Group("/v1"))
	return engine
}

func doRequest(engine http.Handler, method, target, lane string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	if lane != "" {
		req.Header.Set(HeaderLane, lane)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func writeJSONResponse(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload), rec.Body.String())
	return payload.Error.Code
}

func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload), rec.Body.String())
	return payload.Error.Message
}

func assertNoLaneHeader(t *testing.T, calls []recordedCall) {
	t.Helper()
	for _, call := range calls {
		if got := call.header.Get(HeaderLane); got != "" {
			t.Errorf("%s %s leaked %s=%q upstream", call.method, call.path, HeaderLane, got)
		}
	}
}

func assertBearer(t *testing.T, calls []recordedCall) {
	t.Helper()
	for _, call := range calls {
		if got := call.header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("%s %s Authorization = %q", call.method, call.path, got)
		}
	}
}
