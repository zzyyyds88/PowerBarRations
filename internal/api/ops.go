package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
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

// channelNamesByIDs 把渠道 id 列表解析成名字列表（供 dry-run 预览）。
// 未知 id 返回 404 channel_not_found（与真实写入同口径）。
func channelNamesByIDs(ids []int) ([]string, error) {
	names := make([]string, 0, len(ids))
	unknown := make([]int, 0)
	for _, id := range ids {
		channel, err := model.GetChannelById(id, false)
		if err != nil || channel == nil {
			unknown = append(unknown, id)
			continue
		}
		names = append(names, channel.Name)
	}
	if len(unknown) > 0 {
		return nil, &apiError{
			status:  http.StatusNotFound,
			code:    apierr.CodeChannelNotFound,
			message: "unknown channel id(s): " + intsToStrings(unknown),
			hint:    "GET /api/channels",
		}
	}
	return names, nil
}

// intsToStrings 把 int 列表格式化成逗号分隔串（仅用于错误 message）。
func intsToStrings(ids []int) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ", ")
}

// previewUpstreamApply 计算"应用上游变更"将新增/移除的模型（不落库）。
//
// 与 controller.applyChannelUpstreamModelUpdates 同口径：把请求里的
// add/remove 与渠道暂存的 last-detected/last-removed 求交，再对"将移除的模型"
// 跑车道引用守卫。
func previewUpstreamApply(channel *model.Channel, payload map[string]any) (add, remove []string, blocked map[string][]string, err error) {
	settings := channel.GetOtherSettings()
	pendingAdd := settings.UpstreamModelUpdateLastDetectedModels
	pendingRemove := settings.UpstreamModelUpdateLastRemovedModels
	reqAdd := intersectStrings(stringSliceOf(payload["add_models"]), pendingAdd)
	reqRemove := intersectStrings(stringSliceOf(payload["remove_models"]), pendingRemove)
	reqRemove = subtractStrings(reqRemove, reqAdd)
	if len(reqRemove) > 0 {
		refs, refErr := model.RemovedModelLaneRefs(channel.Id, reqRemove)
		if refErr != nil {
			return nil, nil, nil, refErr
		}
		if len(refs) > 0 {
			blocked = map[string][]string{channel.Name: refs}
		}
	}
	return reqAdd, reqRemove, blocked, nil
}

// stringSliceOf 把 payload 字段转成 []string（容忍 null/缺失）。
func stringSliceOf(value any) []string {
	list, _ := stringSlice(value)
	return list
}

// intersectStrings 求交集，保留 a 的顺序（与 controller.intersectModelNames 同语义）。
func intersectStrings(a, b []string) []string {
	seen := map[string]bool{}
	for _, v := range b {
		seen[v] = true
	}
	out := make([]string, 0, len(a))
	for _, v := range a {
		if seen[v] {
			out = append(out, v)
		}
	}
	return out
}

// subtractStrings 返回 a 中不在 b 里的元素。
func subtractStrings(a, b []string) []string {
	drop := map[string]bool{}
	for _, v := range b {
		drop[v] = true
	}
	out := make([]string, 0, len(a))
	for _, v := range a {
		if !drop[v] {
			out = append(out, v)
		}
	}
	return out
}

// previewUpstreamApplyAll 汇总"应用全部渠道上游变更"将新增/移除的模型（不落库）。
func previewUpstreamApplyAll() (add, remove []string, err error) {
	var channels []*model.Channel
	if err = model.DB.Where("status = ?", common.ChannelStatusEnabled).Order("id asc").Find(&channels).Error; err != nil {
		return nil, nil, err
	}
	addSet, removeSet := map[string]bool{}, map[string]bool{}
	for _, ch := range channels {
		settings := ch.GetOtherSettings()
		if !settings.UpstreamModelUpdateCheckEnabled {
			continue
		}
		for _, m := range settings.UpstreamModelUpdateLastDetectedModels {
			addSet[m] = true
		}
		for _, m := range settings.UpstreamModelUpdateLastRemovedModels {
			removeSet[m] = true
		}
	}
	return sortedBoolKeys(addSet), sortedBoolKeys(removeSet), nil
}

// sortedBoolKeys 返回排序后的 map 键（保证预览稳定可断言）。
func sortedBoolKeys(in map[string]bool) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// previewLogFilesCleanup 列出日志文件清理将删除的文件名（不落库）。
// 与 controller.CleanupLogFiles 同口径：by_count 保留最新 value 个，by_days 按天。
func previewLogFilesCleanup(mode, valueStr string) ([]string, error) {
	if mode != "by_count" && mode != "by_days" {
		return nil, &apiError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
			message: "invalid mode, must be by_count or by_days"}
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil || value < 1 {
		return nil, &apiError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
			message: "invalid value, must be a positive integer"}
	}
	files, err := controller.LogFileInfosForDryRun()
	if err != nil {
		return nil, err
	}
	active := controller.ActiveLogPathForDryRun()
	names := make([]string, 0)
	if mode == "by_count" {
		for i, f := range files {
			if i < value {
				continue
			}
			if f.Path == active {
				continue
			}
			names = append(names, f.Name)
		}
		return names, nil
	}
	cutoff := time.Now().AddDate(0, 0, -value)
	for _, f := range files {
		if f.ModTime.Before(cutoff) && f.Path != active {
			names = append(names, f.Name)
		}
	}
	return names, nil
}

// toAnySlice 把 payload 字段转成 []any（容忍 null/缺失）。
func toAnySlice(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return nil
}

// channelNamesByTag 返回某标签下的渠道名（供 by-tag 预览）。
func channelNamesByTag(tag string) ([]string, error) {
	channels, err := model.GetChannelsByTag(tag, true, false)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(channels))
	for _, ch := range channels {
		names = append(names, ch.Name)
	}
	return names, nil
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
	// dry-run：返回将变更的渠道名，不落库（api-spec §2.4/§5.9）。
	if dryRun(c) {
		names, err := channelNamesByIDs(ids)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		previewOpsNames(c, "channels", "update", nil, names, nil, nil)
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
	if dryRun(c) {
		names, err := channelNamesByIDs(ids)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		previewOpsNames(c, "channels", "update", nil, names, nil, nil)
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
	if dryRun(c) {
		suffix := "_复制"
		if s, ok := payload["suffix"].(string); ok && s != "" {
			suffix = s
		}
		previewOpsNames(c, "channels", "add", []string{channel.Name + suffix}, nil, nil, nil)
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
	// dry-run：只列出将删除的目录记录，并跑车道引用守卫（与真实调用同口径）。
	if dryRun(c) {
		names, blocked, err := previewModelCatalogDelete(resolved)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		if len(blocked) > 0 {
			apierr.ConflictDetails(c, apierr.CodeConflict,
				"模型仍被车道引用，已取消删除",
				"先 PUT /api/lanes/{name} 移除成员，或改用 ?force=1", gin.H{"blocked": blocked})
			return
		}
		previewOpsNames(c, "model_metadata", "remove", nil, nil, names, nil)
		return
	}
	payload["model_ids"] = resolved
	writeJSONBody(c, payload)
	controller.BatchDeleteModelMeta(c)
}

// previewModelCatalogDelete 列出将删除的目录记录名，并返回车道引用明细。
func previewModelCatalogDelete(ids []int) (names []string, blocked map[string][]string, err error) {
	blocked = map[string][]string{}
	for _, id := range ids {
		var record model.Model
		if findErr := model.DB.Where("id = ?", id).First(&record).Error; findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return nil, nil, &apiError{status: http.StatusNotFound, code: apierr.CodeModelNotFound,
					message: "model id " + strconv.Itoa(id) + " not found", hint: "GET /api/model-metadata"}
			}
			return nil, nil, findErr
		}
		channels, chErr := model.GetChannelsDeclaringModel(record.ModelName)
		if chErr != nil {
			return nil, nil, chErr
		}
		for _, ch := range channels {
			refs, refErr := model.RemovedModelLaneRefs(ch.Id, []string{record.ModelName})
			if refErr != nil {
				return nil, nil, refErr
			}
			if len(refs) > 0 {
				blocked[ch.Name] = refs
			}
		}
		names = append(names, record.ModelName)
	}
	return names, blocked, nil
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
	// dry-run：get_key_status 是读动作，照常执行；其余动作只预览、不落库。
	if dryRun(c) && action != "get_key_status" {
		previewOpsNames(c, "channels", "update", nil, []string{channel.Name}, nil,
			gin.H{"action": action})
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
	channel, ok := resolveChannelByName(c, c.Param("name"))
	if !ok {
		return
	}
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	// dry-run：只算出将新增/移除的模型与车道引用守卫结果，不落库。
	if dryRun(c) {
		add, remove, blocked, err := previewUpstreamApply(channel, payload)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		if len(blocked) > 0 {
			apierr.ConflictDetails(c, apierr.CodeConflict,
				"上游同步会移除仍被车道引用的模型，已取消",
				"先 PUT /api/lanes/{name} 移除成员，或改用 ?force=1", gin.H{"blocked": blocked})
			return
		}
		previewOpsNames(c, "channels", "update", nil, add, remove,
			gin.H{"channel": channel.Name})
		return
	}
	payload["id"] = channel.Id
	writeJSONBody(c, payload)
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
	if dryRun(c) {
		names, err := channelNamesByTag(tag)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		previewOpsNames(c, "channels", "update", nil, names, nil, gin.H{"tag": tag})
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
	if dryRun(c) {
		names, err := channelNamesByTag(tag)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		previewOpsNames(c, "channels", "update", nil, names, nil,
			gin.H{"tag": tag, "enabled": status == 1})
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
	if dryRun(c) {
		previewOpsNames(c, "system_options", "update", nil, []string{key}, nil, nil)
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
	if dryRun(c) {
		previewOps(c, "prefill_groups", "update", c.Param("id"))
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
	if dryRun(c) {
		previewOps(c, "prefill_groups", "remove", record.Name)
		return
	}
	controller.DeletePrefillGroup(c)
}

// DeleteDisabledChannels DELETE /api/channels/disabled。
//
// 基座 handler 是"先做引用守卫、再整批删除"；dry-run 时必须先返回将删清单，
// 且仍保留引用守卫（被引用时返回 409 + details.blocked，与真实调用同口径）。
func DeleteDisabledChannels(c *gin.Context) {
	if !dryRun(c) {
		controller.DeleteDisabledChannel(c)
		return
	}
	var disabled []model.Channel
	if err := model.DB.Where("status <> ?", common.ChannelStatusEnabled).Find(&disabled).Error; err != nil {
		writeAPIError(c, err)
		return
	}
	names := make([]string, 0, len(disabled))
	blocked := map[string][]string{}
	for _, ch := range disabled {
		refs, refErr := model.LanesReferencingChannel(ch.Id)
		if refErr != nil {
			writeAPIError(c, refErr)
			return
		}
		if len(refs) > 0 {
			blocked[ch.Name] = refs
			continue
		}
		names = append(names, ch.Name)
	}
	if len(blocked) > 0 {
		apierr.ConflictDetails(c, apierr.CodeConflict,
			"部分已禁用渠道被车道引用，已取消删除",
			"先 PUT /api/lanes/{name} 移除成员", gin.H{"blocked": blocked})
		return
	}
	previewOpsNames(c, "channels", "remove", nil, nil, names, nil)
}

// —— 基座 handler 的直接复用（签名一致、无需注入） ——

var (
	DetectAllUpstream      = controller.DetectAllChannelUpstreamModelUpdates
	GetAllSystemOptions    = controller.GetOptions
	ListSystemTasksHandler = controller.ListSystemTasks
	CurrentSystemTask      = controller.GetCurrentSystemTask
	PerformanceStats       = controller.GetPerformanceStats
	ResetPerformanceStats  = controller.ResetPerformanceStats
	ForceGarbageCollection = controller.ForceGC
	ClearDiskCacheHandler  = controller.ClearDiskCache
	ListLogFilesHandler    = controller.GetLogFiles
	ListPrefillGroups      = controller.GetPrefillGroups
	SyncUpstreamPreviewH   = controller.SyncUpstreamPreview
	MissingModelsHandler   = controller.GetMissingModels
)

// —— dry-run 适配层：这些端点复用基座 handler，但必须先拦截 ?dry_run=true ——

// ApplyAllUpstream POST /api/channels/upstream-updates/apply-all
func ApplyAllUpstream(c *gin.Context) {
	if !dryRun(c) {
		controller.ApplyAllChannelUpstreamModelUpdates(c)
		return
	}
	add, remove, err := previewUpstreamApplyAll()
	if err != nil {
		writeAPIError(c, err)
		return
	}
	previewOpsNames(c, "channels", "update", nil, add, remove, nil)
}

// CreateLogCleanupTask POST /api/system-tasks/log-cleanup
func CreateLogCleanupTask(c *gin.Context) {
	if !dryRun(c) {
		controller.CreateLogCleanupSystemTask(c)
		return
	}
	target := strings.TrimSpace(c.Query("target_timestamp"))
	if target == "" || target == "0" {
		apierr.Validation(c, "target timestamp is required")
		return
	}
	previewOpsNames(c, "system_tasks", "add", []string{"log_cleanup"}, nil, nil,
		gin.H{"target_timestamp": target})
}

// CleanupLogFilesHandler DELETE /api/system/log-files
//
// 非 dry-run 时复用基座 handler（含 partial_failure → 500 的映射）；
// dry-run 时只列出将删除的文件名。
func CleanupLogFilesHandler(c *gin.Context) {
	if !dryRun(c) {
		controller.CleanupLogFiles(c)
		return
	}
	names, err := previewLogFilesCleanup(c.Query("mode"), c.Query("value"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	previewOpsNames(c, "log_files", "remove", nil, nil, names, nil)
}

// CreatePrefillGroup POST /api/prefill-groups
func CreatePrefillGroup(c *gin.Context) {
	if !dryRun(c) {
		controller.CreatePrefillGroup(c)
		return
	}
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	name, _ := payload["name"].(string)
	if strings.TrimSpace(name) == "" {
		apierr.Validation(c, "name is required")
		return
	}
	// 重名检查与真实写入同口径（409）。
	if dup, err := model.IsPrefillGroupNameDuplicated(0, name); err != nil {
		writeAPIError(c, err)
		return
	} else if dup {
		apierr.Conflict(c, apierr.CodeConflict, "组名称已存在", "")
		return
	}
	previewOpsNames(c, "prefill_groups", "add", []string{name}, nil, nil, nil)
}

// SyncUpstreamApplyH POST /api/model-catalog/sync-upstream
func SyncUpstreamApplyH(c *gin.Context) {
	if !dryRun(c) {
		controller.SyncUpstreamModels(c)
		return
	}
	payload, _, ok := readJSONBody(c)
	if !ok {
		return
	}
	if payload["source_version"] == nil || len(stringSliceOf(payload["selections"])) == 0 {
		apierr.BadRequest(c, "Preview and select metadata changes before applying")
		return
	}
	// 选择项即"将写入/更新"的目录记录；真实应用前的版本/存在性校验留给 apply。
	names := make([]string, 0)
	for _, raw := range toAnySlice(payload["selections"]) {
		if obj, ok := raw.(map[string]any); ok {
			if n, ok := obj["model_name"].(string); ok && strings.TrimSpace(n) != "" {
				names = append(names, n)
			}
		}
	}
	previewOpsNames(c, "model_metadata", "update", nil, names, nil,
		gin.H{"source_version": payload["source_version"]})
}

// —— 基座 handler 的直接复用（签名一致、无需注入） ——

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
