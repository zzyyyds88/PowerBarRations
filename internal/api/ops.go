package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/controller"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 运维闭环端点的适配层（api-spec §5.3.1–§5.3.4）。
//
// 这些能力此前只在控制台基座路径（/api/channel/**、/api/option/** 等）可用，且以**渠道 id**
// 或 body 里的 channel_id 定位渠道。契约统一用**渠道名**（与 §5.3 一致），因此这里做一层
// "名字 → id 注入"的适配，复用基座 handler，避免复制业务逻辑。
//
// 响应沿用基座信封 {success, message, data}（见 api-spec §5.3.1 的说明）。

// withChannelIDByName 把路径 {name} 解析成渠道 id，并注入到 JSON body 的 channel_id / id 字段。
// 渠道不存在时按契约返回 404 channel_not_found，不进入基座 handler。
func withChannelIDByName(bodyKeys ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := strings.TrimSpace(c.Param("name"))
		channel, err := model.GetChannelByName(name)
		if err != nil {
			apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+name+"' not found", "GET /api/channels")
			return
		}
		raw, _ := io.ReadAll(c.Request.Body)
		payload := map[string]any{}
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &payload); err != nil {
				apierr.BadRequest(c, "invalid json body")
				return
			}
		}
		for _, key := range bodyKeys {
			payload[key] = channel.Id
		}
		encoded, _ := json.Marshal(payload)
		c.Request.Body = io.NopCloser(bytes.NewReader(encoded))
		c.Request.ContentLength = int64(len(encoded))
	}
}

// withChannelIDInPath 把 {name} 解析成 id 后写入 gin 的 "id" 路径参数，供基座 handler 读取。
func withChannelIDInPath() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := strings.TrimSpace(c.Param("name"))
		channel, err := model.GetChannelByName(name)
		if err != nil {
			apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+name+"' not found", "GET /api/channels")
			return
		}
		c.Params = append(c.Params, gin.Param{Key: "id", Value: strconv.Itoa(channel.Id)})
		c.Next()
	}
}

// BatchChannelTagStatus POST /api/channels/by-tag/status：按 status 分派到启/停用。
func BatchChannelTagStatus(c *gin.Context) {
	var req struct {
		Tag    string `json:"tag"`
		Status int    `json:"status"`
	}
	raw, _ := io.ReadAll(c.Request.Body)
	// 读完必须复位：基座 handler 还要再读一次 body（否则它看到空 body → "参数错误"）。
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	c.Request.ContentLength = int64(len(raw))
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			apierr.BadRequest(c, "invalid json body")
			return
		}
	}
	if strings.TrimSpace(req.Tag) == "" {
		apierr.Validation(c, "tag is required")
		return
	}
	// status: 1=启用，其它（2/停用）=停用，与渠道状态语义一致。
	if req.Status == 1 {
		controller.EnableTagChannels(c)
		return
	}
	controller.DisableTagChannels(c)
}

// ManageMultiKeysByName POST /api/channels/{name}/multi-keys。
func ManageMultiKeysByName(c *gin.Context) {
	withChannelIDByName("channel_id")(c)
	if c.IsAborted() {
		return
	}
	controller.ManageMultiKeys(c)
}

// DetectUpstreamByName / ApplyUpstreamByName POST /api/channels/{name}/upstream-updates/*。
func DetectUpstreamByName(c *gin.Context) {
	withChannelIDByName("id")(c)
	if c.IsAborted() {
		return
	}
	controller.DetectChannelUpstreamModelUpdates(c)
}

func ApplyUpstreamByName(c *gin.Context) {
	withChannelIDByName("id")(c)
	if c.IsAborted() {
		return
	}
	controller.ApplyChannelUpstreamModelUpdates(c)
}

// Ollama 三个端点：基座从 body 取 channel_id。
func OllamaPullByName(c *gin.Context) {
	withChannelIDByName("channel_id")(c)
	if c.IsAborted() {
		return
	}
	controller.OllamaPullModel(c)
}

func OllamaPullStreamByName(c *gin.Context) {
	withChannelIDByName("channel_id")(c)
	if c.IsAborted() {
		return
	}
	controller.OllamaPullModelStream(c)
}

func OllamaDeleteByName(c *gin.Context) {
	withChannelIDByName("channel_id")(c)
	if c.IsAborted() {
		return
	}
	controller.OllamaDeleteModel(c)
}

// ListChannelsByTagModels GET /api/channels/by-tag/models?tag=
func ListChannelsByTagModels(c *gin.Context) { controller.GetTagModels(c) }

// 以下为基座 handler 的直接复用（签名一致、无需注入）。
var (
	BatchChannelStatus      = controller.BatchUpdateChannelStatus
	BatchChannelTag         = controller.BatchSetChannelTag
	DeleteDisabledChannels  = controller.DeleteDisabledChannel
	DisableChannelsByTag    = controller.DisableTagChannels
	EnableChannelsByTag     = controller.EnableTagChannels
	EditChannelsByTag       = controller.EditTagChannels
	RepairChannelAbilities  = controller.FixChannelsAbilities
	FetchUpstreamModelsBody = controller.FetchModels
	DetectAllUpstream       = controller.DetectAllChannelUpstreamModelUpdates
	ApplyAllUpstream        = controller.ApplyAllChannelUpstreamModelUpdates
	GetAllSystemOptions     = controller.GetOptions
	UpdateSystemOptions     = controller.UpdateOption
	AffinityCacheStats      = controller.GetChannelAffinityCacheStats
	ClearAffinityCache      = controller.ClearChannelAffinityCache
	ListSystemTasksHandler  = controller.ListSystemTasks
	CurrentSystemTask       = controller.GetCurrentSystemTask
	CreateLogCleanupTask    = controller.CreateLogCleanupSystemTask
	PerformanceStats        = controller.GetPerformanceStats
	ResetPerformanceStats   = controller.ResetPerformanceStats
	ForceGarbageCollection  = controller.ForceGC
	ClearDiskCacheHandler   = controller.ClearDiskCache
	ListLogFilesHandler     = controller.GetLogFiles
	CleanupLogFilesHandler  = controller.CleanupLogFiles
	ListPrefillGroups       = controller.GetPrefillGroups
	CreatePrefillGroup      = controller.CreatePrefillGroup
	SyncUpstreamPreviewH    = controller.SyncUpstreamPreview
	SyncUpstreamApplyH      = controller.SyncUpstreamModels
	MissingModelsHandler    = controller.GetMissingModels
	BatchDeleteModelMetaH   = controller.BatchDeleteModelMeta
)

// GetSystemTaskByID GET /api/system-tasks/{id}（基座读的是 task_id）。
func GetSystemTaskByID(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_id", Value: c.Param("id")})
	controller.GetSystemTask(c)
}

// UpdatePrefillGroupByID PUT /api/prefill-groups/{id}（基座从 body 取 id）。
func UpdatePrefillGroupByID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		apierr.Validation(c, "invalid prefill group id")
		return
	}
	raw, _ := io.ReadAll(c.Request.Body)
	payload := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			apierr.BadRequest(c, "invalid json body")
			return
		}
	}
	payload["id"] = id
	encoded, _ := json.Marshal(payload)
	c.Request.Body = io.NopCloser(bytes.NewReader(encoded))
	c.Request.ContentLength = int64(len(encoded))
	controller.UpdatePrefillGroup(c)
}

// DeletePrefillGroupByID DELETE /api/prefill-groups/{id}。
func DeletePrefillGroupByID(c *gin.Context) { controller.DeletePrefillGroup(c) }

// CopyChannelByName POST /api/channels/{name}/copy（基座从路径 :id 取）。
func CopyChannelByName(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	channel, err := model.GetChannelByName(name)
	if err != nil {
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+name+"' not found", "GET /api/channels")
		return
	}
	c.Params = append(c.Params, gin.Param{Key: "id", Value: strconv.Itoa(channel.Id)})
	controller.CopyChannel(c)
}

// OllamaVersionByName GET /api/channels/{name}/ollama/version。
func OllamaVersionByName(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	channel, err := model.GetChannelByName(name)
	if err != nil {
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+name+"' not found", "GET /api/channels")
		return
	}
	c.Params = append(c.Params, gin.Param{Key: "id", Value: strconv.Itoa(channel.Id)})
	controller.OllamaVersion(c)
}

// GetChannelKeyByName GET /api/channels/{name}/key。
func GetChannelKeyByName(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	channel, err := model.GetChannelByName(name)
	if err != nil {
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+name+"' not found", "GET /api/channels")
		return
	}
	c.Params = append(c.Params, gin.Param{Key: "id", Value: strconv.Itoa(channel.Id)})
	controller.GetChannelKey(c)
}

// CodexUsageByName 等三个 Codex 端点：基座从路径 :id 取。
func CodexUsageByName(c *gin.Context) { withChannelIDInPath()(c); controller.GetCodexChannelUsage(c) }
func CodexResetCreditsByName(c *gin.Context) {
	withChannelIDInPath()(c)
	controller.GetCodexChannelRateLimitResetCredits(c)
}
func CodexResetUsageByName(c *gin.Context) {
	withChannelIDInPath()(c)
	controller.ResetCodexChannelUsage(c)
}
func CodexRefreshByName(c *gin.Context) {
	withChannelIDInPath()(c)
	controller.RefreshCodexChannelCredential(c)
}

// withChannelNameToIDParam 是给路由用的中间件形式（先解析再交给基座 handler）。
func withChannelNameToIDParam() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := strings.TrimSpace(c.Param("name"))
		channel, err := model.GetChannelByName(name)
		if err != nil {
			apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+name+"' not found", "GET /api/channels")
			return
		}
		c.Params = append(c.Params, gin.Param{Key: "id", Value: strconv.Itoa(channel.Id)})
		c.Next()
	}
}

var _ = http.StatusOK
