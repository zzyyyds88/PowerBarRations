package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 模型目录元数据与车道顺序摘要的稳定契约（api-spec §5.7）。
//
// 这两项此前只存在于控制台内部路径（/api/console/models/**）或受 GET /api/lanes 的
// cursor 上限（200）影响，现提升为稳定端点，供 AI/脚本直接使用。

// ListLaneSummaries GET /api/lane-summaries
//
// 一次返回**全部**车道的成员顺序摘要（不分页），供路由页渲染"成员与顺序"列。
// 走 GET /api/lanes 时该列在车道数超过 cursor 上限后只能退化为只显示数量。
func ListLaneSummaries(c *gin.Context) {
	lanes, err := model.ListLanes()
	if err != nil {
		writeAPIError(c, err)
		return
	}
	items := make([]gin.H, 0, len(lanes))
	for i := range lanes {
		lane := &lanes[i]
		members := make([]gin.H, 0, len(lane.Members))
		orphans := 0
		for _, m := range lane.Members {
			name := ""
			channelEnabled := false
			ch, chErr := model.ChannelOrNil(m.ChannelId)
			if chErr == nil && ch != nil {
				name = ch.Name
				channelEnabled = ch.Status == 1
			} else {
				orphans++
			}
			item := gin.H{
				"channel":         name,
				"upstream_model":  m.UpstreamModel,
				"public_alias":    m.PublicAlias,
				"priority":        m.Priority,
				"channel_enabled": channelEnabled,
			}
			if name == "" {
				item["orphan"] = true
			}
			members = append(members, item)
		}
		items = append(items, gin.H{
			"name":                lane.Name,
			"enabled":             lane.Enabled,
			"mode":                lane.Mode,
			"active_member":       lane.ActiveMember,
			"orphan_member_count": orphans,
			"members":             members,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nil})
}

// modelMetadataPayload PUT /api/model-metadata/{model} 的请求体。
type modelMetadataPayload struct {
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Tags        []string `json:"tags"`
	Endpoints   []string `json:"endpoints"`
	Status      *int     `json:"status"`
	NameRule    *int     `json:"name_rule"`
}

// findModelMetadataByName 按精确名取目录记录；不存在返回 (nil, nil)。
func findModelMetadataByName(name string) (*model.Model, error) {
	var record model.Model
	err := model.DB.Where("model_name = ?", name).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// ListModelMetadata GET /api/model-metadata
//
// 模型目录元数据（不分页）：has_metadata=false 表示仅由渠道声明、尚无目录记录。
func ListModelMetadata(c *gin.Context) {
	// 不分页：直接取全部目录记录（GetAllModels 的 limit=0 会被 GORM 当成 LIMIT 0）。
	var records []*model.Model
	if err := model.DB.Order("model_name asc").Find(&records).Error; err != nil {
		writeAPIError(c, err)
		return
	}
	connections, connErr := model.GetModelConnections()
	if connErr != nil {
		writeAPIError(c, connErr)
		return
	}
	configured := map[string]map[int]bool{}
	for _, conn := range connections {
		if configured[conn.Model] == nil {
			configured[conn.Model] = map[int]bool{}
		}
		configured[conn.Model][conn.ChannelId] = true
	}
	seen := map[string]bool{}
	items := make([]gin.H, 0, len(records))
	for _, record := range records {
		seen[record.ModelName] = true
		items = append(items, modelMetadataItem(record, len(configured[record.ModelName]), true))
	}
	for name, channelIDs := range configured {
		if seen[name] {
			continue
		}
		items = append(items, gin.H{
			"model":                    name,
			"has_metadata":             false,
			"configured_channel_count": len(channelIDs),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i]["model"].(string) < items[j]["model"].(string)
	})
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nil})
}

func modelMetadataItem(record *model.Model, configuredChannels int, hasMetadata bool) gin.H {
	return gin.H{
		"model":                    record.ModelName,
		"description":              record.Description,
		"icon":                     record.Icon,
		"tags":                     splitTagList(record.Tags),
		"endpoints":                splitTagList(record.Endpoints),
		"status":                   record.Status,
		"name_rule":                record.NameRule,
		"has_metadata":             hasMetadata,
		"configured_channel_count": configuredChannels,
	}
}

func splitTagList(raw string) []string {
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// PutModelMetadata PUT /api/model-metadata/{model}
//
// 全量幂等 upsert：body 为完整对象，响应为写后回读。仅允许精确名规则（与 DELETE 约束一致）。
func PutModelMetadata(c *gin.Context) {
	name := strings.TrimSpace(c.Param("model"))
	if name == "" {
		apierr.Validation(c, "model name is required")
		return
	}
	var payload modelMetadataPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	nameRule := model.NameRuleExact
	if payload.NameRule != nil {
		nameRule = *payload.NameRule
	}
	if nameRule != model.NameRuleExact {
		apierr.Unprocessable(c, apierr.CodeValidationFailed, "only exact-match (name_rule=0) metadata is supported")
		return
	}
	status := 1
	if payload.Status != nil {
		status = *payload.Status
	}
	if dryRun(c) {
		dryRunResult(c, "model_metadata", "update", name)
		return
	}
	existing, err := findModelMetadataByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	record := &model.Model{
		ModelName:   name,
		Description: payload.Description,
		Icon:        payload.Icon,
		Tags:        strings.Join(payload.Tags, ","),
		Endpoints:   strings.Join(payload.Endpoints, ","),
		Status:      status,
		NameRule:    nameRule,
	}
	if existing != nil {
		record.Id = existing.Id
		record.SyncOfficial = existing.SyncOfficial
		if err := record.Update(); err != nil {
			writeAPIError(c, err)
			return
		}
	} else if err := record.Insert(); err != nil {
		writeAPIError(c, err)
		return
	}
	writeAudit(c, "update", "model_metadata", name)
	saved, err := findModelMetadataByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, modelMetadataItem(saved, 0, true))
}

// DeleteModelMetadataByModel DELETE /api/model-metadata/{model}
//
// 删除目录记录；?remove_from_channels=true 同时从渠道声明移除；被车道引用时 409（force 覆盖并清理）。
func DeleteModelMetadataByModel(c *gin.Context) {
	name := strings.TrimSpace(c.Param("model"))
	record, err := findModelMetadataByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if record == nil {
		apierr.NotFound(c, apierr.CodeValidationFailed, "model metadata '"+name+"' not found", "GET /api/model-metadata")
		return
	}
	removeFromChannels := strings.EqualFold(c.Query("remove_from_channels"), "true")
	force := isForce(c)
	if removeFromChannels && !force {
		blocked := map[string][]string{}
		channels, chErr := model.GetChannelsDeclaringModel(name)
		if chErr != nil {
			writeAPIError(c, chErr)
			return
		}
		for _, ch := range channels {
			refs, refErr := model.RemovedModelLaneRefs(ch.Id, []string{name})
			if refErr != nil {
				writeAPIError(c, refErr)
				return
			}
			if len(refs) > 0 {
				blocked[ch.Name] = refs
			}
		}
		if len(blocked) > 0 {
			body, _ := json.Marshal(blocked)
			apierr.Conflict(c, apierr.CodeConflict,
				"model is still referenced by lanes: "+string(body),
				"retry with ?force=1 to also remove this model from channels")
			return
		}
	}
	if dryRun(c) {
		dryRunResult(c, "model_metadata", "remove", name)
		return
	}
	result, delErr := model.DeleteModelMetadata([]int{record.Id}, removeFromChannels, false)
	if delErr != nil {
		var laneErr *model.LaneReferenceError
		if errors.As(delErr, &laneErr) {
			c.JSON(http.StatusOK, gin.H{"success": false, "code": "conflict", "message": "model is still referenced by lanes", "data": gin.H{"blocked": laneErr.Blocked}})
			return
		}
		writeAPIError(c, delErr)
		return
	}
	writeAudit(c, "delete", "model_metadata", name)
	c.JSON(http.StatusOK, gin.H{"deleted": true, "model": name, "updated_channels": result.UpdatedChannels})
}
