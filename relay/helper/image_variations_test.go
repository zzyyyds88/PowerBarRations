package helper

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
	relayconstant "github.com/zzyyyds88/PowerBarRations/relay/constant"
)

// variations 与 edits 共用 multipart 解析，但多两条入站约束（design-v1 §4）：
// image 文件必填、且只允许一个。缺文件/多文件必须在入站按 400 返回，
// 不能留到转换期变成一个笼统的 5xx。
func TestGetAndValidOpenAIImageRequestVariations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(t *testing.T, fields map[string]string, imageField string, images []string) *gin.Context {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for key, value := range fields {
			require.NoError(t, writer.WriteField(key, value))
		}
		for _, name := range images {
			part, err := writer.CreateFormFile(imageField, name)
			require.NoError(t, err)
			_, err = part.Write([]byte("fake image " + name))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/variations", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return c
	}

	t.Run("single image is accepted and body stays replayable", func(t *testing.T) {
		c := newContext(t,
			map[string]string{"model": "dall-e-2", "size": "512x512", "n": "2"},
			"image", []string{"input.png"})

		request, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesVariations)
		require.NoError(t, err)
		assert.Equal(t, "dall-e-2", request.Model)
		assert.Equal(t, "512x512", request.Size)
		require.NotNil(t, request.N)
		assert.Equal(t, uint(2), *request.N)

		// 校验不得消费请求体：路由/重试阶段还要重放给上游。
		replayed, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		assert.NotEmpty(t, replayed)
		// 解析结果已缓存到 Request 上，后续转换/转发直接复用，不再二次读体。
		require.NotNil(t, c.Request.MultipartForm)
		assert.Len(t, c.Request.MultipartForm.File["image"], 1)
	})

	t.Run("image[] upload naming is accepted", func(t *testing.T) {
		c := newContext(t, map[string]string{"model": "dall-e-2"}, "image[]", []string{"input.png"})
		_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesVariations)
		require.NoError(t, err)
	})

	t.Run("missing image is a client error", func(t *testing.T) {
		c := newContext(t, map[string]string{"model": "dall-e-2"}, "image", nil)
		_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesVariations)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "image is required")
	})

	t.Run("multiple images are rejected", func(t *testing.T) {
		c := newContext(t, map[string]string{"model": "dall-e-2"}, "image", []string{"a.png", "b.png"})
		_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesVariations)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "image must be a single file")
	})

	t.Run("json body falls back to normal image parsing", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/variations",
			bytes.NewBufferString(`{"model":"dall-e-2","image":"https://example.test/a.png"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		request, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesVariations)
		require.NoError(t, err)
		assert.Equal(t, "dall-e-2", request.Model)
	})
}
