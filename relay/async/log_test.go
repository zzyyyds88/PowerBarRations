package async

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/model"
)

func TestAsyncRequestLogMetadata(t *testing.T) {
	resetAsyncState(t)
	recorder := &upstreamRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/files", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writeJSONResponse(w, http.StatusOK,
			`{"id":"file_logged","object":"file","bytes":3,"status":"processed"}`)
	})
	server := newUpstream(t, recorder, mux)
	channel := newChannelWithLane(t, "lane-log", constant.ChannelTypeOpenAI, server.URL)
	engine := newEngine(t, NewHandler())

	rec := doRequest(engine, http.MethodPost, "/v1/files", "lane-log",
		strings.NewReader("abc"), "multipart/form-data; boundary=x")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var entry model.PBRRequestLog
	require.NoError(t, model.DB.Where("lane_name = ?", "lane-log").Order("id desc").First(&entry).Error)
	require.True(t, entry.Success)
	require.Equal(t, http.StatusOK, entry.HTTPStatus)
	require.Equal(t, channel.Id, entry.MemberChannelId)
	require.Equal(t, channel.Name, entry.MemberChannelName)
	require.Zero(t, entry.EstimatedCost, "async objects must not be priced")
	require.Empty(t, entry.PromptTokens)
	require.Empty(t, entry.CompletionTokens)
	require.NotEmpty(t, entry.Attempts)
	require.Equal(t, 1, entry.TotalAttempts)
	require.Equal(t, "openai", entry.InboundFormat)
	require.Equal(t, model.LogTypeConsume, entry.Type)
	require.WithinDuration(t, time.Now(), time.Unix(entry.Ts, 0), 2*time.Minute)
}
