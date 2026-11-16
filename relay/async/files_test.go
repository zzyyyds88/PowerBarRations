package async

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/model"
)

func TestFileListForwardsQuery(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("purpose"); got != "batch" {
			t.Errorf("purpose query = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Errorf("limit query = %q", got)
		}
		writeJSONResponse(w, http.StatusOK, `{"object":"list","data":[]}`)
	})
	server := newUpstream(t, recorder, mux)
	newChannelWithLane(t, "lane-list", constant.ChannelTypeOpenAI, server.URL)
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodGet, "/v1/files?purpose=batch&limit=2", "lane-list", nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"data":[]`)
	calls := recorder.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "purpose=batch&limit=2", calls[0].query)
}

func TestFileListFiltersToLane(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{
			"object":"list",
			"data":[
				{"id":"file_a","object":"file","status":"processed"},
				{"id":"file_b","object":"file","status":"processed"},
				{"id":"file_external","object":"file","status":"processed"},
				{"object":"file","status":"processed"}
			],
			"has_more":true,
			"first_id":"file_a",
			"last_id":"file_external"
		}`)
	})
	server := newUpstream(t, recorder, mux)
	channelA := newChannelWithLane(t, "lane-list-filter-a", constant.ChannelTypeOpenAI, server.URL)
	channelB := newChannelWithLane(t, "lane-list-filter-b", constant.ChannelTypeOpenAI, server.URL)
	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind: model.PBRAsyncObjectKindFile, Lane: "lane-list-filter-a", UpstreamId: "file_a", ChannelId: channelA.Id,
	}))
	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind: model.PBRAsyncObjectKindFile, Lane: "lane-list-filter-b", UpstreamId: "file_b", ChannelId: channelB.Id,
	}))
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodGet, "/v1/files", "lane-list-filter-a", nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
		HasMore bool   `json:"has_more"`
		FirstID string `json:"first_id"`
		LastID  string `json:"last_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, "list", payload.Object)
	require.Len(t, payload.Data, 1, rec.Body.String())
	require.Equal(t, "file_a", payload.Data[0].ID)
	// 分页元数据保持上游原值：G6 只过滤当前页，不替客户端重算分页。
	require.True(t, payload.HasMore)
	require.Equal(t, "file_a", payload.FirstID)
	require.Equal(t, "file_external", payload.LastID)
}

func TestBatchListFiltersToLane(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/batches", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{
			"object":"list",
			"data":[
				{"id":"batch_a","object":"batch","status":"in_progress"},
				{"id":"batch_b","object":"batch","status":"in_progress"},
				{"id":"batch_external","object":"batch","status":"in_progress"}
			],
			"has_more":false,
			"first_id":"batch_a",
			"last_id":"batch_external"
		}`)
	})
	server := newUpstream(t, recorder, mux)
	channelA := newChannelWithLane(t, "lane-batch-list-a", constant.ChannelTypeOpenAI, server.URL)
	channelB := newChannelWithLane(t, "lane-batch-list-b", constant.ChannelTypeOpenAI, server.URL)
	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind: model.PBRAsyncObjectKindBatch, Lane: "lane-batch-list-a", UpstreamId: "batch_a", ChannelId: channelA.Id,
	}))
	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind: model.PBRAsyncObjectKindBatch, Lane: "lane-batch-list-b", UpstreamId: "batch_b", ChannelId: channelB.Id,
	}))
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodGet, "/v1/batches", "lane-batch-list-a", nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Len(t, payload.Data, 1, rec.Body.String())
	require.Equal(t, "batch_a", payload.Data[0].ID)
}

func TestListUnfilterableResponseIsRejected(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"object":"list","data":"not-an-array"}`)
	})
	server := newUpstream(t, recorder, mux)
	newChannelWithLane(t, "lane-list-unfilterable", constant.ChannelTypeOpenAI, server.URL)
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodGet, "/v1/files", "lane-list-unfilterable", nil, "")
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, CodeAsyncListUnfilterable, errorCode(t, rec))
}

func TestFileDeleteUpdatesMetadata(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /v1/files/file_del", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"file_del","object":"file","deleted":true}`)
	})
	server := newUpstream(t, recorder, mux)
	channel := newChannelWithLane(t, "lane-delete", constant.ChannelTypeOpenAI, server.URL)
	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind:       model.PBRAsyncObjectKindFile,
		Lane:       "lane-delete",
		UpstreamId: "file_del",
		ChannelId:  channel.Id,
		Status:     "processed",
	}))
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodDelete, "/v1/files/file_del", "lane-delete", nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	obj, err := model.GetAsyncObject("lane-delete", model.PBRAsyncObjectKindFile, "file_del")
	require.NoError(t, err)
	require.Equal(t, "deleted", obj.Status)
	require.Greater(t, obj.ExpiresAt, int64(0))
}

func TestLaneHeaderValidation(t *testing.T) {
	resetAsyncState(t)
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodPost, "/v1/files", "", strings.NewReader("x"), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeLaneHeaderRequired, errorCode(t, rec))

	rec = doRequest(engine, http.MethodGet, "/v1/files", "missing-lane", nil, "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, CodeLaneNotFound, errorCode(t, rec))

	rec = doRequest(engine, http.MethodGet, "/v1/batches", "", nil, "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeLaneHeaderRequired, errorCode(t, rec))

	rec = doRequest(engine, http.MethodPost, "/v1/batches", "missing-lane", strings.NewReader("{}"), "application/json")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, CodeLaneNotFound, errorCode(t, rec))
}

func TestUnsupportedProtocolForAsync(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"should-not-be-used"}`)
	})
	server := newUpstream(t, recorder, mux)
	newChannelWithLane(t, "lane-anthropic", constant.ChannelTypeAnthropic, server.URL)
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodPost, "/v1/files", "lane-anthropic",
		strings.NewReader("payload"), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeUnsupportedProtocolForAsync, errorCode(t, rec))
	require.Zero(t, recorder.count())

	rec = doRequest(engine, http.MethodGet, "/v1/files", "lane-anthropic", nil, "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeUnsupportedProtocolForAsync, errorCode(t, rec))
	require.Zero(t, recorder.count())
}

func TestFilesCreateRetrieveCrossLaneAndChannelUnavailable(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/files", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("POST /v1/files Authorization = %q", got)
		}
		if got := r.Header.Get(HeaderLane); got != "" {
			t.Errorf("POST /v1/files leaked lane header %q", got)
		}
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Errorf("read upload body: %v", err)
		}
		writeJSONResponse(w, http.StatusOK,
			`{"id":"file_123","object":"file","bytes":42,"status":"processed","created_at":1700000000,"expires_at":1800000000}`)
	})
	mux.HandleFunc("GET /v1/files/file_123", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK,
			`{"id":"file_123","object":"file","bytes":42,"status":"processed","created_at":1700000000,"expires_at":1800000000}`)
	})
	mux.HandleFunc("GET /v1/files/file_123/content", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="server.txt"`)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "hello")
	})
	server := newUpstream(t, recorder, mux)

	channelA := newChannelWithLane(t, "lane-files-a", constant.ChannelTypeOpenAI, server.URL)
	newChannelWithLane(t, "lane-files-b", constant.ChannelTypeOpenAI, server.URL)
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodPost, "/v1/files", "lane-files-a",
		strings.NewReader("multipart-body"), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "file_123")

	obj, err := model.GetAsyncObject("lane-files-a", model.PBRAsyncObjectKindFile, "file_123")
	require.NoError(t, err)
	require.Equal(t, channelA.Id, obj.ChannelId)
	require.Equal(t, int64(42), obj.Size)
	require.Equal(t, "processed", obj.Status)
	require.Equal(t, int64(1700000000), obj.CreatedAt)
	require.Equal(t, int64(1800000000), obj.ExpiresAt)

	rec = doRequest(engine, http.MethodGet, "/v1/files/file_123", "lane-files-a", nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "processed")

	calls := recorder.snapshot()
	require.Len(t, calls, 2)
	assertNoLaneHeader(t, calls)
	assertBearer(t, calls)

	// 跨车道寻址：对象在 lane-files-b 不可见，且不产生新的上游调用。
	before := recorder.count()
	rec = doRequest(engine, http.MethodGet, "/v1/files/file_123", "lane-files-b", nil, "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, CodeAsyncObjectNotFound, errorCode(t, rec))
	require.Equal(t, before, recorder.count())

	// 文件内容：流式转发并保留上游 Content-Type / Content-Disposition。
	rec = doRequest(engine, http.MethodGet, "/v1/files/file_123/content", "lane-files-a", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "hello", rec.Body.String())
	require.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "server.txt")

	// 记录渠道被停用：503 async_object_channel_unavailable，不静默换渠道。
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelA.Id).
		Update("status", common.ChannelStatusManuallyDisabled).Error)
	before = recorder.count()
	rec = doRequest(engine, http.MethodGet, "/v1/files/file_123", "lane-files-a", nil, "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, CodeAsyncObjectChannelUnavailable, errorCode(t, rec))
	require.Equal(t, before, recorder.count())
}

func TestCreateDoesNotFailoverAndUsesNextHealthyMember(t *testing.T) {
	resetAsyncState(t)
	recorderA := &upstreamRecorder{}
	muxA := http.NewServeMux()
	muxA.HandleFunc("POST /v1/files", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)
	})
	serverA := newUpstream(t, recorderA, muxA)

	recorderB := &upstreamRecorder{}
	muxB := http.NewServeMux()
	muxB.HandleFunc("POST /v1/files", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"file_b","object":"file","bytes":1,"status":"processed"}`)
	})
	serverB := newUpstream(t, recorderB, muxB)

	channelA := newChannelWithLane(t, "lane-no-failover", constant.ChannelTypeOpenAI, serverA.URL)
	// 把 A 调到最高优先级：第一次创建必须先打 A。
	require.NoError(t, model.DB.Model(&model.LaneMember{}).
		Where("lane_id = (SELECT id FROM lanes WHERE name = ?) AND channel_id = ?", "lane-no-failover", channelA.Id).
		Update("priority", 10).Error)
	channelB := addChannelToLane(t, "lane-no-failover", constant.ChannelTypeOpenAI, serverB.URL, 0)

	engine := newEngine(t, NewHandler())
	rec := doRequest(engine, http.MethodPost, "/v1/files", "lane-no-failover",
		strings.NewReader("body"), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "boom")
	// 创建阶段不 failover：A 失败不会在同一次请求里打 B。
	require.Equal(t, 1, recorderA.count())
	require.Zero(t, recorderB.count())

	// A 的失败已进入成员运行态；下一次创建应选健康的 B，而不是再打 A。
	rec = doRequest(engine, http.MethodPost, "/v1/files", "lane-no-failover",
		strings.NewReader("body"), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, recorderA.count())
	require.Equal(t, 1, recorderB.count())

	obj, err := model.GetAsyncObject("lane-no-failover", model.PBRAsyncObjectKindFile, "file_b")
	require.NoError(t, err)
	require.Equal(t, channelB.Id, obj.ChannelId)
}

func TestPayloadLimitsAndStreaming(t *testing.T) {
	resetAsyncState(t)
	streamed := bytes.Repeat([]byte("0123456789abcdef"), 16*1024) // 256 KiB

	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/files", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upload body: %v", err)
		}
		writeJSONResponse(w, http.StatusOK,
			`{"id":"file_stream","object":"file","bytes":`+strconv.Itoa(len(body))+`,"status":"processed"}`)
	})
	mux.HandleFunc("GET /v1/files/file_stream/content", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="stream.bin"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(streamed)
	})
	mux.HandleFunc("GET /v1/files/file_big/content", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "2048")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
	})
	server := newUpstream(t, recorder, mux)

	channel := newChannelWithLane(t, "lane-limits", constant.ChannelTypeOpenAI, server.URL)
	h := NewHandler()
	h.UploadLimit = 1024
	h.BatchJSONLimit = 16
	engine := newEngine(t, h)

	// 已知 Content-Length 超过上限：不读请求体、不打上游。
	rec := doRequest(engine, http.MethodPost, "/v1/files", "lane-limits",
		bytes.NewReader(bytes.Repeat([]byte("a"), 2048)), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, CodeAsyncPayloadTooLarge, errorCode(t, rec))
	require.Zero(t, recorder.count())

	// chunked（ContentLength=-1）超过上限：由读取计数器兜住。
	req := httptest.NewRequest(http.MethodPost, "/v1/files", bytes.NewReader(bytes.Repeat([]byte("b"), 2048)))
	req.ContentLength = -1
	req.Header.Set(HeaderLane, "lane-limits")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, CodeAsyncPayloadTooLarge, errorCode(t, rec))

	// 全局请求体中间件（部署级 MAX_REQUEST_BODY_MB）更低的 MaxBytesReader 上限
	// 也必须映射成稳定的 413，而不是上游传输失败 502。
	h.UploadLimit = 1024
	req = httptest.NewRequest(http.MethodPost, "/v1/files", bytes.NewReader(bytes.Repeat([]byte("d"), 64)))
	req.ContentLength = 64
	req.Header.Set(HeaderLane, "lane-limits")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 16)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, CodeAsyncPayloadTooLarge, errorCode(t, rec))

	// batch JSON 上限同样在读取阶段拒绝。
	before := recorder.count()
	rec = doRequest(engine, http.MethodPost, "/v1/batches", "lane-limits",
		strings.NewReader(strings.Repeat("x", 32)), "application/json")
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, CodeAsyncPayloadTooLarge, errorCode(t, rec))
	require.Equal(t, before, recorder.count())

	// 限内上传：请求体逐字节到达上游。
	h.UploadLimit = 1 << 20
	payload := bytes.Repeat([]byte("c"), 900)
	rec = doRequest(engine, http.MethodPost, "/v1/files", "lane-limits",
		bytes.NewReader(payload), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	calls := recorder.snapshot()
	require.Equal(t, 900, len(calls[len(calls)-1].body))

	// 限内下载：256 KiB 完整流式返回。
	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind:       model.PBRAsyncObjectKindFile,
		Lane:       "lane-limits",
		UpstreamId: "file_stream",
		ChannelId:  channel.Id,
		Status:     "processed",
		Size:       int64(len(streamed)),
	}))
	rec = doRequest(engine, http.MethodGet, "/v1/files/file_stream/content", "lane-limits", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, streamed, rec.Body.Bytes())
	require.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "stream.bin")

	// 下载响应 Content-Length 超过上限：413，不把超大响应写给客户端。
	h.UploadLimit = 1024
	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind:       model.PBRAsyncObjectKindFile,
		Lane:       "lane-limits",
		UpstreamId: "file_big",
		ChannelId:  channel.Id,
		Status:     "processed",
		Size:       2048,
	}))
	rec = doRequest(engine, http.MethodGet, "/v1/files/file_big/content", "lane-limits", nil, "")
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, CodeAsyncPayloadTooLarge, errorCode(t, rec))
}

func TestExistingObjectPinnedToRecordedChannel(t *testing.T) {
	resetAsyncState(t)
	recorderA := &upstreamRecorder{}
	muxA := http.NewServeMux()
	muxA.HandleFunc("GET /v1/files/file_pinned", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"file_pinned","status":"processed","bytes":1}`)
	})
	serverA := newUpstream(t, recorderA, muxA)

	recorderB := &upstreamRecorder{}
	muxB := http.NewServeMux()
	muxB.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"wrong-channel"}`)
	})
	serverB := newUpstream(t, recorderB, muxB)

	channelA := newBareChannel(t, "ch-pinned-a", constant.ChannelTypeOpenAI, serverA.URL)
	// 车道成员现在指向 B；已有对象仍必须回创建它的 A。
	newChannelWithLane(t, "lane-pinned", constant.ChannelTypeOpenAI, serverB.URL)

	require.NoError(t, model.CreateAsyncObject(&model.PBRAsyncObject{
		Kind:       model.PBRAsyncObjectKindFile,
		Lane:       "lane-pinned",
		UpstreamId: "file_pinned",
		ChannelId:  channelA.Id,
		Status:     "processed",
	}))

	engine := newEngine(t, NewHandler())
	rec := doRequest(engine, http.MethodGet, "/v1/files/file_pinned", "lane-pinned", nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, recorderA.count())
	require.Zero(t, recorderB.count())
}

func TestRedirectIsNotFollowed(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/v1/files/redirected")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("GET /v1/files/redirected", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"data":[]}`)
	})
	server := newUpstream(t, recorder, mux)
	newChannelWithLane(t, "lane-redirect", constant.ChannelTypeOpenAI, server.URL)
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodGet, "/v1/files", "lane-redirect", nil, "")
	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, "/v1/files/redirected", rec.Header().Get("Location"))
	require.Equal(t, 1, recorder.count())
}

func TestNoAvailableMemberReturnsLane503(t *testing.T) {
	resetAsyncState(t)
	newLaneOnly(t, "lane-empty")
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodPost, "/v1/files", "lane-empty",
		strings.NewReader("x"), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, errorMessage(t, rec), "No available channel for model lane-empty")
}
