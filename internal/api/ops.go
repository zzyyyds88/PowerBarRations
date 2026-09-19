package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/controller"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 运维闭环端点的适配层（api-spec §5.3.1–§5.3.4）。
//
// 契约统一用**渠道名**寻址（与 §5.3 一致），而基座 handler 以数字 id 或 body 里的
// channel_id 定位。这里做一层"名字 → id 注入"的适配，复用基座 handler，避免复制业务逻辑。
//
// 响应信封由 `ops_routes.go` 的响应策略 + `apiresp.Middleware` 统一归一化；
// 适配层只负责**可预判的失败**（未知名、空目标、非法枚举、互斥字段）在进入基座前拦下，
// 保证失败映射不需要猜中文文案。

// gin.Context 键：适配层写入、成功体转写函数读取。
const (
	opsTagKey            = "pbr_ops_tag"
	opsTagEnabledKey     = "pbr_ops_tag_enabled"
	opsMultiKeyActionKey = "pbr_ops_multikey_action"
	opsOptionKey         = "pbr_ops_option_key"
	opsModelKey          = "pbr_ops_model"
)

// —— 名字解析 ——

// resolveChannelByName 按名取渠道；不存在时写 404 并返回 false。
func resolveChannelByName(c *gin.Context, name string) (*model.Channel, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		apierr.Validation(c, "channel name is required")
		return nil, false
	}
	channel, err := model.GetChannelByName(name)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apierr.NotFoundChannel(c, name)
			return nil, false
		}
		apierr.Internal(c, err.Error())
		return nil, false
	}
	return channel, true
}

// resolveChannelNames 把渠道名列表解析成 id 列表；任一名未知即 404 并列出全部未知名。
func resolveChannelNames(c *gin.Context, names []string) ([]int, bool) {
	if len(names) == 0 {
		apierr.Validation(c, "channels is required")
		return nil, false
	}
	ids := make([]int, 0, len(names))
	unknown := make([]string, 0)
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		channel, err := model.GetChannelByName(name)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				unknown = append(unknown, name)
				continue
			}
			apierr.Internal(c, err.Error())
			return nil, false
		}
		ids = append(ids, channel.Id)
	}
	if len(unknown) > 0 {
		apierr.WriteDetails(c, http.StatusNotFound, apierr.CodeChannelNotFound,
			"unknown channel(s): "+strings.Join(unknown, ", "), "GET /api/channels",
			gin.H{"unknown": unknown})
		return nil, false
	}
	if len(ids) == 0 {
		apierr.Validation(c, "channels is required")
		return nil, false
	}
	return ids, true
}

// resolveModelNames 把模型名列表解析成目录记录 id；任一未知即 404 并列出全部未知名。
func resolveModelNames(c *gin.Context, names []string) ([]int, bool) {
	if len(names) == 0 {
		apierr.Validation(c, "models is required")
		return nil, false
	}
	ids := make([]int, 0, len(names))
	unknown := make([]string, 0)
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		record, err := findModelMetadataByName(name)
		if err != nil {
			apierr.Internal(c, err.Error())
			return nil, false
		}
		if record == nil {
			unknown = append(unknown, name)
			continue
		}
		ids = append(ids, record.Id)
	}
	if len(unknown) > 0 {
		apierr.WriteDetails(c, http.StatusNotFound, apierr.CodeModelNotFound,
			"unknown model(s): "+strings.Join(unknown, ", "), "GET /api/model-metadata",
			gin.H{"unknown": unknown})
		return nil, false
	}
	if len(ids) == 0 {
		apierr.Validation(c, "models is required")
		return nil, false
	}
	return ids, true
}

// —— body 读写 ——

// readJSONBody 读并复位请求体（基座 handler 还要再读一次，必须重建）。
func readJSONBody(c *gin.Context) (map[string]any, []byte, bool) {
	raw, _ := io.ReadAll(c.Request.Body)
	restoreBody(c, raw)
	payload := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			apierr.BadRequest(c, "invalid json body")
			return nil, nil, false
		}
	}
	return payload, raw, true
}

// restoreBody 复位请求体与 Content-Length。
func restoreBody(c *gin.Context, raw []byte) {
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	c.Request.ContentLength = int64(len(raw))
}

// writeJSONBody 用改写后的 payload 覆盖请求体。
func writeJSONBody(c *gin.Context, payload map[string]any) {
	encoded, _ := json.Marshal(payload)
	restoreBody(c, encoded)
}

// —— 渠道名 → id 注入 ——

// withChannelIDByName 把路径 {name} 解析成渠道 id，并注入到 JSON body 的指定字段。
func withChannelIDByName(bodyKeys ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := resolveChannelByName(c, c.Param("name"))
		if !ok {
			return
		}
		payload, _, ok := readJSONBody(c)
		if !ok {
			return
		}
		for _, key := range bodyKeys {
			payload[key] = channel.Id
		}
		writeJSONBody(c, payload)
	}
}

// withChannelIDInPath 把 {name} 解析成 id 后写入 gin 的 "id" 路径参数，供基座 handler 读取。
func withChannelIDInPath() gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := resolveChannelByName(c, c.Param("name"))
		if !ok {
			return
		}
		c.Params = append(c.Params, gin.Param{Key: "id", Value: strconv.Itoa(channel.Id)})
		c.Next()
	}
}

// —— 批量端点：channels:[name] 与 ids:[int] 二选一 ——

// batchIDs 解析批量端点的目标渠道：优先 channels，其次 ids，同时给出即 400。
func batchIDs(c *gin.Context, payload map[string]any) ([]int, bool) {
	names, hasNames := stringSlice(payload["channels"])
	ids, hasIDs := intSlice(payload["ids"])
	if hasNames && hasIDs {
		apierr.BadRequest(c, "provide either 'channels' or 'ids', not both")
		return nil, false
	}
	if hasNames {
		return resolveChannelNames(c, names)
	}
	if hasIDs {
		if len(ids) == 0 {
			apierr.Validation(c, "ids is required")
			return nil, false
		}
		return ids, true
	}
	apierr.Validation(c, "either 'channels' or 'ids' is required")
	return nil, false
}

// BatchChannelStatusByName POST /api/channels/batch/status
func BatchChannelStatusByName(c *gin.Context) {
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	ids, ok := batchIDs(c, payload)
	if !ok {
		return
	}
	status := intOf(payload["status"])
	if status != 1 && status != 2 {
		apierr.Validation(c, "status must be 1 (enabled) or 2 (disabled)")
		return
	}
	payload["ids"] = ids
	payload["status"] = status
	writeJSONBody(c, payload)
	controller.BatchUpdateChannelStatus(c)
}

// BatchChannelTagByName POST /api/channels/batch/tag
func BatchChannelTagByName(c *gin.Context) {
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	ids, ok := batchIDs(c, payload)
	if !ok {
		return
	}
	payload["ids"] = ids
	writeJSONBody(c, payload)
	controller.BatchSetChannelTag(c)
}

// CopyChannelByBodyName POST /api/channels/batch/copy
//
// 基座 CopyChannel 从路径 :id 取渠道；契约以 body 的 {channel} 寻址。
func CopyChannelByBodyName(c *gin.Context) {
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	name, _ := payload["channel"].(string)
	channel, ok := resolveChannelByName(c, name)
	if !ok {
		return
	}
	c.Params = append(c.Params, gin.Param{Key: "id", Value: strconv.Itoa(channel.Id)})
	controller.CopyChannel(c)
}

// FetchModelsByName POST /api/channels/batch/fetch-models
func FetchModelsByName(c *gin.Context) {
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	if name, ok := payload["channel"].(string); ok && strings.TrimSpace(name) != "" {
		channel, resolved := resolveChannelByName(c, name)
		if !resolved {
			return
		}
		payload["channel_id"] = channel.Id
		delete(payload, "channel")
		writeJSONBody(c, payload)
	}
	controller.FetchModels(c)
}

// BatchDeleteModelMetaByName POST /api/model-catalog/batch-delete
func BatchDeleteModelMetaByName(c *gin.Context) {
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	names, hasNames := stringSlice(payload["models"])
	ids, hasIDs := intSlice(payload["model_ids"])
	if hasNames && hasIDs {
		apierr.BadRequest(c, "provide either 'models' or 'model_ids', not both")
		return
	}
	var resolved []int
	switch {
	case hasNames:
		resolved, ok = resolveModelNames(c, names)
	case hasIDs:
		if len(ids) == 0 {
			apierr.Validation(c, "model_ids is required")
			return
		}
		resolved = ids
	default:
		apierr.Validation(c, "either 'models' or 'model_ids' is required")
		return
	}
	if !ok {
		return
	}
	payload["model_ids"] = resolved
	writeJSONBody(c, payload)
	controller.BatchDeleteModelMeta(c)
}

// —— 按名寻址的单渠道端点 ——

// ManageMultiKeysByName POST /api/channels/{name}/multi-keys。
func ManageMultiKeysByName(c *gin.Context) {
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	action, _ := payload["action"].(string)
	if !validMultiKeyAction(action) {
		apierr.Validation(c, "unsupported action")
		return
	}
	channel, ok := resolveChannelByName(c, c.Param("name"))
	if !ok {
		return
	}
	if !channel.ChannelInfo.IsMultiKey {
		apierr.Conflict(c, apierr.CodeConflict, "channel is not in multi-key mode", "")
		return
	}
	c.Set(opsMultiKeyActionKey, action)
	payload["channel_id"] = channel.Id
	writeJSONBody(c, payload)
	controller.ManageMultiKeys(c)
}

func validMultiKeyAction(action string) bool {
	switch action {
	case "get_key_status", "disable_key", "enable_key", "delete_key",
		"delete_disabled_keys", "enable_all_keys", "disable_all_keys":
		return true
	default:
		return false
	}
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

// —— Codex：先按名解析并校验渠道类型，避免把"类型不对"误报成上游故障 ——

func withCodexChannelIDByName(bodyKeys ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := resolveChannelByName(c, c.Param("name"))
		if !ok {
			return
		}
		if channel.Type != constant.ChannelTypeCodex {
			apierr.Validation(c, "this operation is only supported for Codex channels")
			return
		}
		if channel.ChannelInfo.IsMultiKey {
			apierr.Validation(c, "multi-key channel is not supported")
			return
		}
		if len(bodyKeys) == 0 {
			c.Params = append(c.Params, gin.Param{Key: "id", Value: strconv.Itoa(channel.Id)})
			return
		}
		payload, _, ok := readJSONBody(c)
		if !ok {
			return
		}
		for _, key := range bodyKeys {
			payload[key] = channel.Id
		}
		writeJSONBody(c, payload)
	}
}

func CodexRefreshByName(c *gin.Context) {
	withCodexChannelIDByName()(c)
	if c.IsAborted() {
		return
	}
	controller.RefreshCodexChannelCredential(c)
}

func CodexUsageByName(c *gin.Context) {
	withCodexChannelIDByName()(c)
	if c.IsAborted() {
		return
	}
	controller.GetCodexChannelUsage(c)
}

func CodexResetCreditsByName(c *gin.Context) {
	withCodexChannelIDByName()(c)
	if c.IsAborted() {
		return
	}
	controller.GetCodexChannelRateLimitResetCredits(c)
}

func CodexResetUsageByName(c *gin.Context) {
	withCodexChannelIDByName()(c)
	if c.IsAborted() {
		return
	}
	controller.ResetCodexChannelUsage(c)
}

// —— Ollama：同样先按名解析并校验渠道类型 ——

func withOllamaChannelID(bodyKeys ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := resolveChannelByName(c, c.Param("name"))
		if !ok {
			return
		}
		if channel.Type != constant.ChannelTypeOllama {
			apierr.Validation(c, "this operation is only supported for Ollama channels")
			return
		}
		payload, _, ok := readJSONBody(c)
		if !ok {
			return
		}
		if modelName, ok := payload["model_name"].(string); ok {
			c.Set(opsModelKey, strings.TrimSpace(modelName))
		}
		for _, key := range bodyKeys {
			payload[key] = channel.Id
		}
		writeJSONBody(c, payload)
	}
}

func OllamaPullByName(c *gin.Context) {
	withOllamaChannelID("channel_id")(c)
	if c.IsAborted() {
		return
	}
	controller.OllamaPullModel(c)
}

func OllamaPullStreamByName(c *gin.Context) {
	withOllamaChannelID("channel_id")(c)
	if c.IsAborted() {
		return
	}
	controller.OllamaPullModelStream(c)
}

func OllamaDeleteByName(c *gin.Context) {
	withOllamaChannelID("channel_id")(c)
	if c.IsAborted() {
		return
	}
	controller.OllamaDeleteModel(c)
}

func OllamaVersionByName(c *gin.Context) {
	channel, ok := resolveChannelByName(c, c.Param("name"))
	if !ok {
		return
	}
	if channel.Type != constant.ChannelTypeOllama {
		apierr.Validation(c, "this operation is only supported for Ollama channels")
		return
	}
	c.Params = append(c.Params, gin.Param{Key: "id", Value: strconv.Itoa(channel.Id)})
	controller.OllamaVersion(c)
}

// GetChannelKeyByName GET /api/channels/{name}/key。
func GetChannelKeyByName(c *gin.Context) {
	withChannelIDInPath()(c)
	if c.IsAborted() {
		return
	}
	controller.GetChannelKey(c)
}

// —— by-tag 三条：记录 tag 供成功体回填 ——

// EditChannelsByTag PUT /api/channels/by-tag。
func EditChannelsByTag(c *gin.Context) {
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	tag, _ := payload["tag"].(string)
	if strings.TrimSpace(tag) == "" {
		apierr.Validation(c, "tag is required")
		return
	}
	c.Set(opsTagKey, tag)
	writeJSONBody(c, payload)
	controller.EditTagChannels(c)
}

// BatchChannelTagStatus POST /api/channels/by-tag/status：按 status 分派到启/停用。
func BatchChannelTagStatus(c *gin.Context) {
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	tag, _ := payload["tag"].(string)
	if strings.TrimSpace(tag) == "" {
		apierr.Validation(c, "tag is required")
		return
	}
	status := intOf(payload["status"])
	if status != 1 && status != 2 {
		apierr.Validation(c, "status must be 1 (enabled) or 2 (disabled)")
		return
	}
	c.Set(opsTagKey, tag)
	c.Set(opsTagEnabledKey, status == 1)
	if status == 1 {
		controller.EnableTagChannels(c)
		return
	}
	controller.DisableTagChannels(c)
}

// ListChannelsByTagModels GET /api/channels/by-tag/models?tag=
func ListChannelsByTagModels(c *gin.Context) {
	tag := strings.TrimSpace(c.Query("tag"))
	if tag == "" {
		apierr.Validation(c, "tag is required")
		return
	}
	c.Set(opsTagKey, tag)
	controller.GetTagModels(c)
}

// —— 系统选项 / 任务 / 预填组 ——

// UpdateSystemOptionsByName PUT /api/system/options/all：记录被改的键供成功体回填。
func UpdateSystemOptionsByName(c *gin.Context) {
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	key, _ := payload["key"].(string)
	if strings.TrimSpace(key) == "" {
		apierr.Validation(c, "key is required")
		return
	}
	c.Set(opsOptionKey, key)
	writeJSONBody(c, payload)
	controller.UpdateOption(c)
}

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
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	payload["id"] = id
	writeJSONBody(c, payload)
	controller.UpdatePrefillGroup(c)
}

// DeletePrefillGroupByID DELETE /api/prefill-groups/{id}。
func DeletePrefillGroupByID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		apierr.Validation(c, "invalid prefill group id")
		return
	}
	var record model.PrefillGroup
	if err := model.DB.Where("id = ?", id).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apierr.NotFoundPrefillGroup(c, c.Param("id"))
			return
		}
		apierr.Internal(c, err.Error())
		return
	}
	controller.DeletePrefillGroup(c)
}

// —— 基座 handler 的直接复用（签名一致、无需注入） ——

var (
	DeleteDisabledChannels = controller.DeleteDisabledChannel
	DetectAllUpstream      = controller.DetectAllChannelUpstreamModelUpdates
	ApplyAllUpstream       = controller.ApplyAllChannelUpstreamModelUpdates
	GetAllSystemOptions    = controller.GetOptions
	ListSystemTasksHandler = controller.ListSystemTasks
	CurrentSystemTask      = controller.GetCurrentSystemTask
	CreateLogCleanupTask   = controller.CreateLogCleanupSystemTask
	PerformanceStats       = controller.GetPerformanceStats
	ResetPerformanceStats  = controller.ResetPerformanceStats
	ForceGarbageCollection = controller.ForceGC
	ClearDiskCacheHandler  = controller.ClearDiskCache
	ListLogFilesHandler    = controller.GetLogFiles
	CleanupLogFilesHandler = controller.CleanupLogFiles
	ListPrefillGroups      = controller.GetPrefillGroups
	CreatePrefillGroup     = controller.CreatePrefillGroup
	SyncUpstreamPreviewH   = controller.SyncUpstreamPreview
	SyncUpstreamApplyH     = controller.SyncUpstreamModels
	MissingModelsHandler   = controller.GetMissingModels
)

// —— 小工具 ——

func intOf(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		parsed, _ := v.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(v))
		return parsed
	default:
		return 0
	}
}

func atoiOr(raw string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return parsed
}

func splitComma(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.TrimSpace(part))
	}
	return out
}

func stringSlice(value any) ([]string, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out, true
}

func intSlice(value any) ([]int, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]int, 0, len(list))
	for _, item := range list {
		out = append(out, intOf(item))
	}
	return out, true
}
