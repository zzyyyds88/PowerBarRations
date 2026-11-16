package async

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/model"
)

func TestBatchCreateGetCancel(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/batches", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(HeaderLane); got != "" {
			t.Errorf("POST /v1/batches leaked lane header %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("POST /v1/batches Authorization = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read batch body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("batch body is not JSON: %v (%s)", err, string(body))
		}
		if payload["input_file_id"] != "file_1" {
			t.Errorf("input_file_id = %v", payload["input_file_id"])
		}
		writeJSONResponse(w, http.StatusOK,
			`{"id":"batch_123","object":"batch","status":"validating","created_at":1700000100,"expires_at":1800000100}`)
	})
	mux.HandleFunc("GET /v1/batches/batch_123", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"batch_123","object":"batch","status":"in_progress"}`)
	})
	mux.HandleFunc("POST /v1/batches/batch_123/cancel", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(HeaderLane); got != "" {
			t.Errorf("cancel leaked lane header %q", got)
		}
		writeJSONResponse(w, http.StatusOK, `{"id":"batch_123","object":"batch","status":"cancelling"}`)
	})
	server := newUpstream(t, recorder, mux)
	channel := newChannelWithLane(t, "lane-batches", constant.ChannelTypeOpenAI, server.URL)
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodPost, "/v1/batches", "lane-batches", strings.NewReader(
		`{"input_file_id":"file_1","endpoint":"/v1/chat/completions","completion_window":"24h"}`),
		"application/json")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "batch_123")

	obj, err := model.GetAsyncObject("lane-batches", model.PBRAsyncObjectKindBatch, "batch_123")
	require.NoError(t, err)
	require.Equal(t, channel.Id, obj.ChannelId)
	require.Equal(t, "validating", obj.Status)
	require.Equal(t, int64(1700000100), obj.CreatedAt)
	require.Equal(t, int64(1800000100), obj.ExpiresAt)

	rec = doRequest(engine, http.MethodGet, "/v1/batches/batch_123", "lane-batches", nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	obj, err = model.GetAsyncObject("lane-batches", model.PBRAsyncObjectKindBatch, "batch_123")
	require.NoError(t, err)
	require.Equal(t, "in_progress", obj.Status)

	rec = doRequest(engine, http.MethodPost, "/v1/batches/batch_123/cancel", "lane-batches", nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "cancelling")
	obj, err = model.GetAsyncObject("lane-batches", model.PBRAsyncObjectKindBatch, "batch_123")
	require.NoError(t, err)
	require.Equal(t, "cancelling", obj.Status)

	calls := recorder.snapshot()
	require.Len(t, calls, 3)
	assertNoLaneHeader(t, calls)
	assertBearer(t, calls)
}

func TestBatchInputFileLaneCheck(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/batches", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read batch body: %v", err)
		}
		if !strings.Contains(string(body), `"input_file_id"`) {
			t.Errorf("batch body missing input_file_id: %s", body)
		}
		writeJSONResponse(w, http.StatusOK, `{"id":"batch_owner_check","status":"validating"}`)
	})
	server := newUpstream(t, recorder, mux)
	channelA := newChannelWithLane(t, "lane-batch-owner-a", constant.ChannelTypeOpenAI, server.URL)
	channelB := newChannelWithLane(t, "lane-batch-owner-b", constant.ChannelTypeOpenAI, server.URL)

	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind: model.PBRAsyncObjectKindFile, Lane: "lane-batch-owner-a", UpstreamId: "file_same_lane", ChannelId: channelA.Id,
	}))
	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind: model.PBRAsyncObjectKindFile, Lane: "lane-batch-owner-b", UpstreamId: "file_other_lane", ChannelId: channelB.Id,
	}))
	engine := newEngine(t, NewHandler())

	// 已知属于其它车道：404，且不请求上游。
	rec := doRequest(engine, http.MethodPost, "/v1/batches", "lane-batch-owner-a",
		strings.NewReader(`{"input_file_id":"file_other_lane","endpoint":"/v1/chat/completions","completion_window":"24h"}`),
		"application/json")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Equal(t, CodeAsyncObjectNotFound, errorCode(t, rec))
	require.Zero(t, recorder.count())

	// PBR 无记录的外部文件：放行，由上游校验。
	rec = doRequest(engine, http.MethodPost, "/v1/batches", "lane-batch-owner-a",
		strings.NewReader(`{"input_file_id":"file_external","endpoint":"/v1/chat/completions","completion_window":"24h"}`),
		"application/json")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, recorder.count())
	require.Contains(t, string(recorder.snapshot()[0].body), `"file_external"`)

	// 本车道记录的文件：放行。
	rec = doRequest(engine, http.MethodPost, "/v1/batches", "lane-batch-owner-a",
		strings.NewReader(`{"input_file_id":"file_same_lane","endpoint":"/v1/chat/completions","completion_window":"24h"}`),
		"application/json")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 2, recorder.count())
}

func TestBatchCrossLaneIsIsolated(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"batch_x","status":"in_progress"}`)
	})
	server := newUpstream(t, recorder, mux)
	channelA := newChannelWithLane(t, "lane-batch-a", constant.ChannelTypeOpenAI, server.URL)
	newChannelWithLane(t, "lane-batch-b", constant.ChannelTypeOpenAI, server.URL)
	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind:       model.PBRAsyncObjectKindBatch,
		Lane:       "lane-batch-a",
		UpstreamId: "batch_x",
		ChannelId:  channelA.Id,
		Status:     "in_progress",
	}))

	engine := newEngine(t, NewHandler())
	before := recorder.count()
	rec := doRequest(engine, http.MethodPost, "/v1/batches/batch_x/cancel", "lane-batch-b", nil, "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, CodeAsyncObjectNotFound, errorCode(t, rec))
	require.Equal(t, before, recorder.count())
}
