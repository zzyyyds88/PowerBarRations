package relay

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/internal/filestore"
	"github.com/zzyyyds88/PowerBarRations/middleware"
	"github.com/zzyyyds88/PowerBarRations/model"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/relayconvert/kitutil"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// materializeClaudeFileRefs 把 `/v1/messages` 里的 Anthropic Files 引用展开成内联
// base64（ADR 0011）。
//
// 为什么必须在转发前展开：
//   - 上游渠道是 OpenAI 兼容协议，不认识 `source.type=file` + `file_id`；
//   - 转换器（claudeImageSourceURL）遇到 file 只能丢弃并记诊断，客户端会拿到
//     一张缺图的回答而毫无察觉。
//
// 展开失败必须是**明确错误**：客户端（Claude Code / DSH 等）的约定是收到
// "File id ... has expired" 这类 400 后重传并重试；静默省略会让问题变成
// "模型说没看到图"这种无从排查的现象。
//
// 调用时机：`preparePrivacy` 之后、协议转换之前。隐私脱敏只看到 file_id，
// 看不到展开后的 base64，因此不会把图片字节当正文处理。
func materializeClaudeFileRefs(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) *types.NewAPIError {
	if request == nil {
		return nil
	}
	tokenID := 0
	if key := middleware.PBRClientKeyFrom(c); key != nil {
		tokenID = key.Id
	}
	// 没有归属键就无法判定文件属于谁：此时若出现 file 引用，宁可明确报错，
	// 也不能错误地读取别人的对象。
	hasRef := claudeRequestHasFileRefs(request)
	if !hasRef {
		return nil
	}
	if tokenID <= 0 {
		return fileRefError("", "the request is not associated with a client key")
	}

	expander := &fileRefExpander{tokenID: tokenID}
	if err := expander.expandSystem(&request.System); err != nil {
		return err
	}
	for i := range request.Messages {
		if err := expander.expandContent(&request.Messages[i].Content); err != nil {
			return err
		}
	}
	return nil
}

// fileRefExpander 持有本次请求的归属键，并提供逐层展开。
type fileRefExpander struct {
	tokenID int
}

// expandSystem 展开 system 字段（字符串或内容块数组两种形态）。
func (e *fileRefExpander) expandSystem(system *any) *types.NewAPIError {
	if system == nil || *system == nil {
		return nil
	}
	if _, isString := (*system).(string); isString {
		return nil
	}
	blocks, err := anyToMediaMessages(*system)
	if err != nil {
		// 解析不了就交给后续转换处理：这里不制造新的失败面。
		return nil
	}
	changed, apiErr := e.expandBlocks(blocks)
	if apiErr != nil {
		return apiErr
	}
	if changed {
		*system = blocks
	}
	return nil
}

// expandContent 展开一条消息的 content（字符串或内容块数组）。
func (e *fileRefExpander) expandContent(content *any) *types.NewAPIError {
	if content == nil || *content == nil {
		return nil
	}
	if _, isString := (*content).(string); isString {
		return nil
	}
	blocks, err := anyToMediaMessages(*content)
	if err != nil {
		return nil
	}
	changed, apiErr := e.expandBlocks(blocks)
	if apiErr != nil {
		return apiErr
	}
	if changed {
		*content = blocks
	}
	return nil
}

// expandBlocks 就地改写一组内容块，递归处理 tool_result 的嵌套 content。
// 返回是否发生了改写。
func (e *fileRefExpander) expandBlocks(blocks []dto.ClaudeMediaMessage) (bool, *types.NewAPIError) {
	changed := false
	for i := range blocks {
		block := &blocks[i]
		// image / document 直接挂 source；tool_result 的 content 是嵌套数组。
		if block.Source != nil && block.Source.IsFileReference() {
			if apiErr := e.materialize(block); apiErr != nil {
				return false, apiErr
			}
			changed = true
		}
		if block.Content == nil {
			continue
		}
		if _, isString := block.Content.(string); isString {
			continue
		}
		nested, err := anyToMediaMessages(block.Content)
		if err != nil {
			continue
		}
		nestedChanged, apiErr := e.expandBlocks(nested)
		if apiErr != nil {
			return false, apiErr
		}
		if nestedChanged {
			block.Content = nested
			changed = true
		}
	}
	return changed, nil
}

// materialize 把一个 file 引用替换成 base64 内联字节。
func (e *fileRefExpander) materialize(block *dto.ClaudeMediaMessage) *types.NewAPIError {
	fileID := strings.TrimSpace(block.Source.FileId)
	record, err := model.GetPBRFile(fileID, e.tokenID)
	if err != nil || record == nil {
		// 不存在与跨密钥同形：不泄露对象是否存在。
		return fileRefError(fileID, "not found")
	}
	now := time.Now().Unix()
	if record.ExpiresAt > 0 && record.ExpiresAt <= now {
		return fileRefError(fileID, "has expired")
	}
	data, readErr := filestore.ReadAll(record.ObjectId)
	if readErr != nil {
		return fileRefError(fileID, "is no longer available")
	}
	block.Source = &dto.ClaudeMessageSource{
		Type:      "base64",
		MediaType: record.MimeType,
		Data:      base64.StdEncoding.EncodeToString(data),
		// FileId 刻意清空：上游不该看到网关内部的对象 ID。
	}
	return nil
}

// fileRefError 统一的 400：客户端据此识别"这个 file_id 不能用了"并重传。
// 用 client_error（skipRetry）语义，不冷却成员——这不是上游的过错。
func fileRefError(fileID, reason string) *types.NewAPIError {
	message := "request references an Anthropic file that " + reason
	if fileID != "" {
		message = fmt.Sprintf("File id %s %s", fileID, reason)
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%s", message),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}

// claudeRequestHasFileRefs 快速预扫描：没有 file 引用时完全跳过展开，
// 避免每个普通请求都走一遍结构遍历。
func claudeRequestHasFileRefs(request *dto.ClaudeRequest) bool {
	if request == nil {
		return false
	}
	if mediaMessageListHasFileRef(parseMediaMessagesLenient(request.System)) {
		return true
	}
	for i := range request.Messages {
		if mediaMessageListHasFileRef(parseMediaMessagesLenient(request.Messages[i].Content)) {
			return true
		}
	}
	return false
}

func mediaMessageListHasFileRef(blocks []dto.ClaudeMediaMessage) bool {
	for i := range blocks {
		if blocks[i].Source != nil && blocks[i].Source.IsFileReference() {
			return true
		}
		if nested := parseMediaMessagesLenient(blocks[i].Content); len(nested) > 0 {
			if mediaMessageListHasFileRef(nested) {
				return true
			}
		}
	}
	return false
}

// anyToMediaMessages 把 JSON 反序列化后的 any 转成内容块数组。
// 复用 kitutil 的转换语义，不自己实现类型断言树。
func anyToMediaMessages(value any) ([]dto.ClaudeMediaMessage, error) {
	return kitutil.Any2Type[[]dto.ClaudeMediaMessage](value)
}

// parseMediaMessagesLenient 宽容版：转换失败返回 nil（预扫描不该改变请求结果）。
func parseMediaMessagesLenient(value any) []dto.ClaudeMediaMessage {
	blocks, err := anyToMediaMessages(value)
	if err != nil {
		return nil
	}
	return blocks
}
