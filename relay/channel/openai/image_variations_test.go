package openai

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/constant"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	relayconstant "github.com/zzyyyds88/PowerBarRations/relay/constant"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
)

// /v1/images/variations 复用 edits 的 multipart 流代理（design-v1 §4）：
// 非文件字段与 image 原样转发，mask 不转发（variations 没有该参数）。
func TestConvertImageVariationsRequestMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "dall-e-2"))
	require.NoError(t, writer.WriteField("size", "512x512"))
	require.NoError(t, writer.WriteField("n", "2"))
	require.NoError(t, writer.WriteField("response_format", "url"))
	imagePart, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = imagePart.Write([]byte("fake variation image"))
	require.NoError(t, err)
	maskPart, err := writer.CreateFormFile("mask", "mask.png")
	require.NoError(t, err)
	_, err = maskPart.Write([]byte("fake mask"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/variations", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesVariations}
	request := dto.ImageRequest{Model: "dall-e-2", Size: "512x512"}

	converted, err := (&Adaptor{}).ConvertImageRequest(c, info, request)
	require.NoError(t, err)
	convertedBody, ok := converted.(*bytes.Buffer)
	require.True(t, ok, "variations 必须走 multipart 重写而不是 JSON")

	replayed := httptest.NewRequest(http.MethodPost, "/v1/images/variations", bytes.NewReader(convertedBody.Bytes()))
	replayed.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	require.NoError(t, replayed.ParseMultipartForm(32<<20))

	assert.Equal(t, "dall-e-2", replayed.PostForm.Get("model"))
	assert.Equal(t, "512x512", replayed.PostForm.Get("size"))
	assert.Equal(t, "2", replayed.PostForm.Get("n"))
	assert.Equal(t, "url", replayed.PostForm.Get("response_format"))
	require.Len(t, replayed.MultipartForm.File["image"], 1)
	assert.Empty(t, replayed.MultipartForm.File["mask"], "variations 不得转发 mask")
	file, err := replayed.MultipartForm.File["image"][0].Open()
	require.NoError(t, err)
	defer file.Close()
	fileBytes, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, []byte("fake variation image"), fileBytes)
}

// DoRequest 必须对 variations 的 multipart 请求使用 form 发送（与 edits 同路径），
// 而不是按 JSON 发送到 chat/completions。
func TestDoRequestSendsImageVariationsAsMultipartForm(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type captured struct {
		path        string
		contentType string
	}
	requests := make(chan captured, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requests <- captured{path: r.URL.Path, contentType: r.Header.Get("Content-Type")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://example.test/v.png"}]}`))
	}))
	defer server.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "dall-e-2"))
	imagePart, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = imagePart.Write([]byte("fake variation image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/variations", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeImagesVariations,
		OriginModelName: "dall-e-2",
		RequestURLPath:  "/v1/images/variations",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    server.URL,
			ApiKey:            "sk-test",
			UpstreamModelName: "dall-e-2",
		},
	}
	adaptor.Init(info)

	converted, err := adaptor.ConvertImageRequest(c, info, dto.ImageRequest{Model: "dall-e-2"})
	require.NoError(t, err)
	convertedBody, ok := converted.(*bytes.Buffer)
	require.True(t, ok)

	resp, err := adaptor.DoRequest(c, info, convertedBody)
	require.NoError(t, err)
	httpResp, ok := resp.(*http.Response)
	require.True(t, ok, "DoRequest 必须返回 *http.Response")
	require.NotNil(t, httpResp)
	defer httpResp.Body.Close()

	got := <-requests
	assert.Equal(t, "/v1/images/variations", got.path, "上游路径必须是 variations，不是 chat/completions")
	assert.Contains(t, got.contentType, "multipart/form-data")
}
