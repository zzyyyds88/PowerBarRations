package async

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/internal/filestore"
	"github.com/zzyyyds88/PowerBarRations/middleware"
	"github.com/zzyyyds88/PowerBarRations/model"
	"gorm.io/gorm"
)

// Anthropic Files（ADR 0011）：按 anthropic-beta 头分流、网关侧暂存字节、
// 不需要 X-PBR-Lane、不参与选路。

// newAnthropicFilesEngine 构造只挂 Files 路由的引擎，并把指定客户端密钥写进上下文
// ——归属判定依赖它，而 PBRTokenAuth 在真实链路上由中间件写入。
func newAnthropicFilesEngine(t *testing.T, tokenID int) *gin.Engine {
	t.Helper()
	h := NewHandler()
	engine := gin.New()
	group := engine.Group("/v1", func(c *gin.Context) {
		middleware.SetupContextForPBRClientKey(c, &model.ClientKey{Id: tokenID, Name: "test-key"})
		c.Next()
	})
	h.RegisterRoutes(group)
	return engine
}

// anthropicFilesDir 把字节目录指向测试临时目录，避免污染工作区。
func anthropicFilesDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PBR_FILES_DIR", dir)
	return dir
}

func buildUploadBody(t *testing.T, filename, contentType string, data []byte, expiresSeconds string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	// 用 CreatePart 而非 CreateFormFile：后者写死 application/octet-stream，
	// 而真实客户端（DSH/Claude Code）会带正确的 Content-Type。
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	if expiresSeconds != "" {
		require.NoError(t, writer.WriteField("expires_after[anchor]", "created_at"))
		require.NoError(t, writer.WriteField("expires_after[seconds]", expiresSeconds))
	}
	require.NoError(t, writer.Close())
	return body, writer.FormDataContentType()
}

func doAnthropicFilesRequest(engine http.Handler, method, target string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	// httptest.NewRequest 不接受 nil body：无体的 GET/DELETE 传 http.NoBody。
	var reader io.Reader = http.NoBody
	if body != nil {
		reader = body
	}
	req := httptest.NewRequest(method, target, reader)
	// 关键：只带 beta 头，不带 X-PBR-Lane——这正是 DSH 的真实行为。
	req.Header.Set("anthropic-beta", "files-api-2025-04-14")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func decodeFileObject(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

// 没有 lane 头也能上传成功——这是本次修复的核心断言（此前一律 400）。
func TestAnthropicFilesUploadWithoutLaneHeader(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	payload := []byte("\x89PNG\r\n\x1a\nfake-image-bytes")
	body, contentType := buildUploadBody(t, "shot.png", "image/png", payload, "3600")

	rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, contentType)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	obj := decodeFileObject(t, rec)
	assert.Equal(t, "file", obj["type"])
	assert.Equal(t, "image/png", obj["mime_type"])
	assert.EqualValues(t, len(payload), obj["size_bytes"])
	assert.Equal(t, "shot.png", obj["filename"])

	id, _ := obj["id"].(string)
	require.True(t, model.IsPBRFileID(id), "返回的 id 必须是网关侧格式: %s", id)

	// 元数据落库、字节落盘。
	record, err := model.GetPBRFile(id, 7)
	require.NoError(t, err)
	assert.Equal(t, "image/png", record.MimeType)
	stored, err := filestore.ReadAll(id)
	require.NoError(t, err)
	assert.Equal(t, payload, stored)
}

// 只有 OpenAI 语义（无 beta 头）才要求 lane 头：两条路径必须分流干净。
func TestAnthropicFilesDispatchOnlyWithBetaHeader(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	payload := []byte("hello")
	body, contentType := buildUploadBody(t, "a.txt", "text/plain", payload, "")

	req := httptest.NewRequest(http.MethodPost, "/v1/files", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "没有 beta 头 = OpenAI Files 语义，缺 lane 头应 400")
	assert.Contains(t, rec.Body.String(), CodeLaneHeaderRequired)
}

// 跨密钥一律 404：不泄露对象存在性。
func TestAnthropicFilesOwnershipIsolation(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)

	ownerEngine := newAnthropicFilesEngine(t, 7)
	body, contentType := buildUploadBody(t, "a.txt", "text/plain", []byte("secret"), "")
	rec := doAnthropicFilesRequest(ownerEngine, http.MethodPost, "/v1/files", body, contentType)
	require.Equal(t, http.StatusOK, rec.Code)
	id := decodeFileObject(t, rec)["id"].(string)

	// 另一个密钥读同一对象：与不存在同形。
	otherEngine := newAnthropicFilesEngine(t, 8)
	for _, target := range []string{"/v1/files/" + id, "/v1/files/" + id + "/content"} {
		otherRec := doAnthropicFilesRequest(otherEngine, http.MethodGet, target, nil, "")
		assert.Equal(t, http.StatusNotFound, otherRec.Code, target)
		assert.Contains(t, otherRec.Body.String(), CodeAnthropicFileNotFound)
	}
	// 删除同样被拒，且对象仍属于原主。
	delRec := doAnthropicFilesRequest(otherEngine, http.MethodDelete, "/v1/files/"+id, nil, "")
	assert.Equal(t, http.StatusNotFound, delRec.Code)
	_, err := model.GetPBRFile(id, 7)
	assert.NoError(t, err, "别人的删除请求不得影响原对象")
}

// 非法对象 ID 必须被拒，且不得参与路径派生（目录穿越防线）。
func TestAnthropicFilesRejectsPathTraversalIDs(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	for _, id := range []string{
		"pbrfile-../../etc/passwd",
		"pbrfile-..%2f..%2fetc%2fpasswd",
		"../../etc/passwd",
		"pbrfile-zzzz",
		"file-abc123",
	} {
		rec := doAnthropicFilesRequest(engine, http.MethodGet, "/v1/files/"+id, nil, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, "id=%s 必须被拒", id)
	}
	// filestore.Path 自身也不接受非法 ID。
	_, err := filestore.Path("pbrfile-../../etc/passwd")
	assert.Error(t, err)
}

// MIME 白名单：不支持的类别在上传时就拒（415），而不是等引用时才失败。
func TestAnthropicFilesRejectsUnsupportedMimeType(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	body, contentType := buildUploadBody(t, "x.zip", "application/zip", []byte("PK\x03\x04"), "")
	rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, contentType)

	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	assert.Contains(t, rec.Body.String(), CodeAnthropicFileUnsupportedType)
}

// 过期对象对外一律视为不存在，且会被清理任务删掉。
func TestAnthropicFilesExpiryAndCleanup(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	body, contentType := buildUploadBody(t, "a.txt", "text/plain", []byte("bye"), "60")
	rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, contentType)
	require.Equal(t, http.StatusOK, rec.Code)
	id := decodeFileObject(t, rec)["id"].(string)

	// 手工把过期时间推到过去，模拟时间流逝。
	require.NoError(t, model.DB.Model(&model.PBRFile{}).
		Where("object_id = ?", id).
		Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)

	getRec := doAnthropicFilesRequest(engine, http.MethodGet, "/v1/files/"+id, nil, "")
	assert.Equal(t, http.StatusNotFound, getRec.Code, "已过期对象对外不存在")

	ids, err := model.PBRFileExpiredIDs(time.Now())
	require.NoError(t, err)
	require.Contains(t, ids, id)

	deleted, err := model.DeleteExpiredPBRFiles(time.Now(), ids)
	require.NoError(t, err)
	require.GreaterOrEqual(t, deleted, int64(1))
	for _, expired := range ids {
		require.NoError(t, filestore.Remove(expired))
	}
	_, statErr := filestore.Open(id)
	assert.ErrorIs(t, statErr, filestore.ErrNotFound, "字节也应被删除")
}

// 单文件超限 -> 413，且不留下半截文件。
func TestAnthropicFilesRejectsOversizedFile(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	// 比上限多 1 字节，避免构造 64 MiB 负载。
	tooBig := bytes.Repeat([]byte("a"), int(model.PBRFileMaxBytes)+1)
	body, contentType := buildUploadBody(t, "big.txt", "text/plain", tooBig, "")
	rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, contentType)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Body.String(), CodeAnthropicFileTooLarge)

	var count int64
	require.NoError(t, model.DB.Model(&model.PBRFile{}).Count(&count).Error)
	assert.Zero(t, count, "超限上传不得留下元数据行")
}

// 列表只返回本密钥名下未过期的对象。
func TestAnthropicFilesListIsScopedToKey(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	for _, name := range []string{"a.txt", "b.txt"} {
		body, contentType := buildUploadBody(t, name, "text/plain", []byte(name), "")
		rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, contentType)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	otherEngine := newAnthropicFilesEngine(t, 8)
	otherBody, otherType := buildUploadBody(t, "other.txt", "text/plain", []byte("other"), "")
	require.Equal(t, http.StatusOK,
		doAnthropicFilesRequest(otherEngine, http.MethodPost, "/v1/files", otherBody, otherType).Code)

	rec := doAnthropicFilesRequest(engine, http.MethodGet, "/v1/files", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)

	var payload struct {
		Data    []map[string]any `json:"data"`
		HasMore bool             `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Len(t, payload.Data, 2, "列表不得泄漏其它密钥的对象")
	assert.False(t, payload.HasMore)
	for _, item := range payload.Data {
		assert.NotEqual(t, "other.txt", item["filename"])
	}
}

// content 端点必须回原始字节。
func TestAnthropicFilesContentServesBytes(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	payload := []byte("raw-bytes-here")
	body, contentType := buildUploadBody(t, "a.txt", "text/plain", payload, "")
	rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, contentType)
	require.Equal(t, http.StatusOK, rec.Code)
	id := decodeFileObject(t, rec)["id"].(string)

	contentRec := doAnthropicFilesRequest(engine, http.MethodGet, "/v1/files/"+id+"/content", nil, "")
	require.Equal(t, http.StatusOK, contentRec.Code)
	assert.Equal(t, "text/plain", contentRec.Header().Get("Content-Type"))
	assert.Equal(t, payload, contentRec.Body.Bytes())
}

// 删除后元数据与字节都不在。
func TestAnthropicFilesDeleteRemovesBytes(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	body, contentType := buildUploadBody(t, "a.txt", "text/plain", []byte("gone"), "")
	rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, contentType)
	require.Equal(t, http.StatusOK, rec.Code)
	id := decodeFileObject(t, rec)["id"].(string)

	delRec := doAnthropicFilesRequest(engine, http.MethodDelete, "/v1/files/"+id, nil, "")
	require.Equal(t, http.StatusOK, delRec.Code)

	_, err := model.GetPBRFile(id, 7)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, openErr := filestore.Open(id)
	assert.ErrorIs(t, openErr, filestore.ErrNotFound)
}

// 缺少 file 字段的 multipart 请求要给出明确 400，而不是空对象。
func TestAnthropicFilesRejectsMissingFileField(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("purpose", "vision"))
	require.NoError(t, writer.Close())

	rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, writer.FormDataContentType())
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), CodeAnthropicFileInvalidRequest)
}

// 日志载体必须不写车道：这条路径没有选路，写车道名会污染按车道聚合的统计。
func TestAnthropicFilesLogHasNoLane(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	body, contentType := buildUploadBody(t, "a.txt", "text/plain", []byte("log"), "")
	rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, contentType)
	require.Equal(t, http.StatusOK, rec.Code)

	var entry model.PBRRequestLog
	require.NoError(t, model.DB.Order("id DESC").First(&entry).Error)
	assert.Empty(t, entry.LaneName, "Anthropic Files 没有车道")
	assert.Empty(t, entry.RouteSource)
	assert.Equal(t, "anthropic", entry.InboundFormat)
	assert.True(t, entry.Success)
	assert.Zero(t, entry.EstimatedCost, "不参与计价")
}

// 过滤/统计：上传失败时 error_summary 保留稳定原因，便于排障。
func TestAnthropicFilesFailureIsRecorded(t *testing.T) {
	resetAsyncState(t)
	anthropicFilesDir(t)
	engine := newAnthropicFilesEngine(t, 7)

	body, contentType := buildUploadBody(t, "x.zip", "application/zip", []byte("PK"), "")
	rec := doAnthropicFilesRequest(engine, http.MethodPost, "/v1/files", body, contentType)
	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code)

	var entry model.PBRRequestLog
	require.NoError(t, model.DB.Order("id DESC").First(&entry).Error)
	assert.False(t, entry.Success)
	assert.NotEmpty(t, entry.ErrorKind)
	assert.True(t, strings.Contains(entry.ErrorSummary, "media type") ||
		strings.Contains(entry.ErrorSummary, "unsupported"), entry.ErrorSummary)
}
