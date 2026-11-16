package async

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/model"
	"gorm.io/gorm"
)

// installAsyncInsertFailure 让下一次 pbr_async_objects INSERT 必然失败，
// 用于验证"上游创建成功、本地落库失败"的孤儿补偿路径。
func installAsyncInsertFailure(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS fail_async_insert").Error)
	require.NoError(t, model.DB.Exec(
		`CREATE TRIGGER fail_async_insert BEFORE INSERT ON pbr_async_objects
		 BEGIN SELECT RAISE(ABORT, 'forced async insert failure'); END`).Error)
	t.Cleanup(func() {
		model.DB.Exec("DROP TRIGGER IF EXISTS fail_async_insert")
	})
}

func TestCreateFileCompensatesOrphanOnRecordFailure(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/files", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"file_orphan","object":"file","bytes":1,"status":"processed"}`)
	})
	mux.HandleFunc("DELETE /v1/files/file_orphan", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"file_orphan","object":"file","deleted":true}`)
	})
	server := newUpstream(t, recorder, mux)
	newChannelWithLane(t, "lane-orphan-file", constant.ChannelTypeOpenAI, server.URL)
	engine := newEngine(t, NewHandler())
	installAsyncInsertFailure(t)

	rec := doRequest(engine, http.MethodPost, "/v1/files", "lane-orphan-file",
		strings.NewReader("payload"), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Equal(t, CodeAsyncObjectRecordFailed, errorCode(t, rec))

	calls := recorder.snapshot()
	require.Len(t, calls, 2, "expected create followed by compensation delete")
	require.Equal(t, http.MethodPost, calls[0].method)
	require.Equal(t, "/v1/files", calls[0].path)
	require.Equal(t, http.MethodDelete, calls[1].method)
	require.Equal(t, "/v1/files/file_orphan", calls[1].path)
	assertNoLaneHeader(t, calls)
	assertBearer(t, calls)

	_, err := model.GetAsyncObject("lane-orphan-file", model.PBRAsyncObjectKindFile, "file_orphan")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestCreateBatchCompensatesOrphanOnRecordFailure(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/batches", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"batch_orphan","object":"batch","status":"validating"}`)
	})
	mux.HandleFunc("DELETE /v1/batches/batch_orphan", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"batch_orphan","object":"batch","deleted":true}`)
	})
	server := newUpstream(t, recorder, mux)
	newChannelWithLane(t, "lane-orphan-batch", constant.ChannelTypeOpenAI, server.URL)
	engine := newEngine(t, NewHandler())
	installAsyncInsertFailure(t)

	rec := doRequest(engine, http.MethodPost, "/v1/batches", "lane-orphan-batch",
		strings.NewReader(`{"input_file_id":"file_external","endpoint":"/v1/chat/completions","completion_window":"24h"}`),
		"application/json")
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Equal(t, CodeAsyncObjectRecordFailed, errorCode(t, rec))

	calls := recorder.snapshot()
	require.Len(t, calls, 2, "expected create followed by compensation delete")
	require.Equal(t, http.MethodPost, calls[0].method)
	require.Equal(t, "/v1/batches", calls[0].path)
	require.Equal(t, http.MethodDelete, calls[1].method)
	require.Equal(t, "/v1/batches/batch_orphan", calls[1].path)
	assertNoLaneHeader(t, calls)
	assertBearer(t, calls)

	_, err := model.GetAsyncObject("lane-orphan-batch", model.PBRAsyncObjectKindBatch, "batch_orphan")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
