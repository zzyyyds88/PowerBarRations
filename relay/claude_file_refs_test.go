package relay

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bytes"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/filestore"
	"github.com/zzyyyds88/PowerBarRations/middleware"
	"github.com/zzyyyds88/PowerBarRations/model"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
	"gorm.io/gorm"
)

// Anthropic Files 的 file_id 展开（ADR 0011）。
//
// 展开是必需的：上游是 OpenAI 兼容协议，不认识 source.type=file；转换器遇到它
// 只会丢弃并记诊断，客户端会拿到缺图的回答而毫无察觉。

func setupFileRefDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, model.AutoMigratePBRFiles(db))
	t.Setenv("PBR_FILES_DIR", t.TempDir())
}

// seedStoredFile 落一条元数据 + 字节，返回对象 ID。
func seedStoredFile(t *testing.T, tokenID int, mimeType string, data []byte, expiresIn time.Duration) string {
	t.Helper()
	id, err := model.NewPBRFileID(nil)
	require.NoError(t, err)
	written, digest, err := filestore.Save(id, bytes.NewReader(data), model.PBRFileMaxBytes)
	require.NoError(t, err)
	require.EqualValues(t, len(data), written)
	now := time.Now()
	record := &model.PBRFile{
		ObjectId:  id,
		TokenId:   tokenID,
		Filename:  "upload.bin",
		MimeType:  mimeType,
		SizeBytes: written,
		Sha256:    digest,
		CreatedAt: now.Unix(),
	}
	// expiresIn 为 0 表示不设过期；负值用于构造"已过期"测试数据。
	if expiresIn != 0 {
		record.ExpiresAt = now.Add(expiresIn).Unix()
	}
	require.NoError(t, record.Create())
	return id
}

func newFileRefContext(t *testing.T, tokenID int) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	if tokenID > 0 {
		middleware.SetupContextForPBRClientKey(c, &model.ClientKey{Id: tokenID, Name: "k"})
	}
	return c
}

func imageFileRefRequest(fileID string) *dto.ClaudeRequest {
	return &dto.ClaudeRequest{
		Model: "lane",
		Messages: []dto.ClaudeMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "看这张图"},
				map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":    "file",
						"file_id": fileID,
					},
				},
			},
		}},
	}
}

func imageSourceOf(t *testing.T, request *dto.ClaudeRequest) *dto.ClaudeMessageSource {
	t.Helper()
	blocks, err := parseMediaMessagesTestHelper(request.Messages[0].Content)
	require.NoError(t, err)
	require.Len(t, blocks, 2)
	require.NotNil(t, blocks[1].Source)
	return blocks[1].Source
}

// 正向：file_id 被展开成 base64，且 MediaType 取自落库记录（不是客户端声明）。
func TestMaterializeClaudeFileRefsExpandsToBase64(t *testing.T) {
	setupFileRefDB(t)
	payload := []byte("\x89PNG\r\n\x1a\nreal-image-bytes")
	id := seedStoredFile(t, 7, "image/png", payload, time.Hour)

	c := newFileRefContext(t, 7)
	request := imageFileRefRequest(id)

	apiErr := materializeClaudeFileRefs(c, &relaycommon.RelayInfo{}, request)

	require.Nil(t, apiErr)
	source := imageSourceOf(t, request)
	assert.Equal(t, "base64", source.Type, "file 引用必须被展开")
	assert.Equal(t, "image/png", source.MediaType)
	assert.Equal(t, base64.StdEncoding.EncodeToString(payload), source.Data.(string))
	assert.Empty(t, source.FileId, "展开后不得再把网关内部对象 ID 传给上游")
}

// 不存在 / 已过期 / 跨密钥都必须返回 400，且 message 含对象 ID 与原因——
// 客户端据此重传（它的既有重试约定）。
func TestMaterializeClaudeFileRefsFailsExplicitly(t *testing.T) {
	setupFileRefDB(t)
	expiredID := seedStoredFile(t, 7, "image/png", []byte("old"), -time.Minute)
	freshID := seedStoredFile(t, 7, "image/png", []byte("new"), time.Hour)

	cases := []struct {
		name   string
		token  int
		fileID string
		reason string
	}{
		{"不存在", 7, "pbrfile-0123456789abcdef01234567", "not found"},
		{"已过期", 7, expiredID, "has expired"},
		{"跨密钥", 8, freshID, "not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newFileRefContext(t, tc.token)
			request := imageFileRefRequest(tc.fileID)

			apiErr := materializeClaudeFileRefs(c, &relaycommon.RelayInfo{}, request)

			require.NotNil(t, apiErr, "展开失败不能静默省略")
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode(),
				"这是客户端可修正的问题，不应冷却成员")
			assert.Contains(t, apiErr.Error(), tc.fileID)
			assert.Contains(t, apiErr.Error(), tc.reason)
		})
	}
}

// 没有 file 引用的普通请求完全不受影响（预扫描应短路）。
func TestMaterializeClaudeFileRefsLeavesInlineUntouched(t *testing.T) {
	setupFileRefDB(t)
	c := newFileRefContext(t, 7)
	request := &dto.ClaudeRequest{
		Model: "lane",
		Messages: []dto.ClaudeMessage{{
			Role: "user",
			Content: []any{
				map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": "image/jpeg",
						"data":       "AAAA",
					},
				},
			},
		}},
	}
	before := request.Messages[0].Content

	apiErr := materializeClaudeFileRefs(c, &relaycommon.RelayInfo{}, request)

	require.Nil(t, apiErr)
	assert.Equal(t, before, request.Messages[0].Content, "内联图片不应被改写")
}

// tool_result 里嵌套的 file 引用也要展开（客户端会把用户上传放进工具结果）。
func TestMaterializeClaudeFileRefsHandlesNestedToolResult(t *testing.T) {
	setupFileRefDB(t)
	payload := []byte("pdf-bytes")
	id := seedStoredFile(t, 7, "application/pdf", payload, time.Hour)

	c := newFileRefContext(t, 7)
	request := &dto.ClaudeRequest{
		Model: "lane",
		Messages: []dto.ClaudeMessage{{
			Role: "user",
			Content: []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "toolu_1",
					"content": []any{
						map[string]any{
							"type": "document",
							"source": map[string]any{
								"type":    "file",
								"file_id": id,
							},
						},
					},
				},
			},
		}},
	}

	apiErr := materializeClaudeFileRefs(c, &relaycommon.RelayInfo{}, request)
	require.Nil(t, apiErr)

	outer, err := parseMediaMessagesTestHelper(request.Messages[0].Content)
	require.NoError(t, err)
	require.Len(t, outer, 1)
	nested, err := parseMediaMessagesTestHelper(outer[0].Content)
	require.NoError(t, err)
	require.Len(t, nested, 1)
	require.NotNil(t, nested[0].Source)
	assert.Equal(t, "base64", nested[0].Source.Type)
	assert.Equal(t, "application/pdf", nested[0].Source.MediaType)
}

// 没有归属键（未经 PBRTokenAuth）时出现 file 引用：明确报错，
// 不能去读一个不存在的属主对象。
func TestMaterializeClaudeFileRefsRequiresToken(t *testing.T) {
	setupFileRefDB(t)
	id := seedStoredFile(t, 7, "image/png", []byte("x"), time.Hour)

	c := newFileRefContext(t, 0)
	apiErr := materializeClaudeFileRefs(c, &relaycommon.RelayInfo{}, imageFileRefRequest(id))

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Contains(t, apiErr.Error(), "not associated with a client key")
}

// 来源语义未变：ClaudeMessageSource 必须能反序列化出 file_id，
// 否则客户端发来的引用在解析阶段就消失了（这正是修复前的状态）。
func TestClaudeMessageSourceParsesFileId(t *testing.T) {
	var source dto.ClaudeMessageSource
	require.NoError(t, common.Unmarshal([]byte(`{"type":"file","file_id":"pbrfile-0123456789abcdef01234567"}`), &source))
	assert.Equal(t, "file", source.Type)
	assert.Equal(t, "pbrfile-0123456789abcdef01234567", source.FileId)
	assert.True(t, source.IsFileReference())
}

// parseMediaMessagesTestHelper 把 any 转成内容块数组（测试用薄封装）。
func parseMediaMessagesTestHelper(value any) ([]dto.ClaudeMediaMessage, error) {
	return anyToMediaMessages(value)
}
