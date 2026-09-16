package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"pbr/internal/apierr"
	"pbr/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 显式车道是**可选的覆盖层**：绝大多数模型靠渠道 Models 声明形成的隐式链路由，
// 只有需要"自定义顺序 / 成员改名 / 池化不同上游的不同模型名"时才建车道。
// 见 docs/routing-spec-v1.md §1.1。

type laneMemberPayload struct {
	Channel       string          `json:"channel"`
	UpstreamModel string          `json:"upstream_model"`
	PublicAlias   string          `json:"public_alias"`
	Priority      int             `json:"priority"`
	Overrides     json.RawMessage `json:"overrides"`
}

type lanePayload struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled"`
	Mode    string `json:"mode"`
	// ActiveMember 仅 manual 模式使用：成员别名，或 "channel/upstream_model" 标签。
	ActiveMember string              `json:"active_member"`
	Config       json.RawMessage     `json:"config"`
	Members      []laneMemberPayload `json:"members"`
}

func laneResponse(c *gin.Context, lane *model.Lane) gin.H {
	members := make([]gin.H, 0, len(lane.Members))
	for _, m := range lane.Members {
		channelName := ""
		if ch, err := model.GetChannelById(m.ChannelId, false); err == nil && ch != nil {
			channelName = ch.Name
		}
		item := gin.H{
			"channel":        channelName,
			"upstream_model": m.UpstreamModel,
			"public_alias":   m.PublicAlias,
			"priority":       m.Priority,
		}
		if overrides := jsonObject(m.Overrides); overrides != nil {
			item["overrides"] = overrides
		}
		members = append(members, item)
	}
	return gin.H{
		"name":          lane.Name,
		"enabled":       lane.Enabled,
		"mode":          lane.Mode,
		"active_member": lane.ActiveMember,
		"config":        model.ParseLaneRelayConfig(lane.Config),
		"members":       members,
		"created_at":    rfc3339(lane.CreatedAt),
		"updated_at":    rfc3339(lane.UpdatedAt),
	}
}

// ListLanes GET /api/v1/lanes
//
// 只列显式车道。隐式车道按定义不存在对象，见 GET /api/v1/models。
func ListLanes(c *gin.Context) {
	limit, cursor, err := pageParams(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	query := model.DB.Model(&model.Lane{}).Order("name asc").Limit(limit + 1)
	if cursor != "" {
		query = query.Where("name > ?", cursor)
	}
	var lanes []model.Lane
	if err := query.Find(&lanes).Error; err != nil {
		writeAPIError(c, err)
		return
	}
	var nextCursor any
	if len(lanes) > limit {
		nextCursor = encodeCursor(lanes[limit-1].Name)
		lanes = lanes[:limit]
	}
	items := make([]gin.H, 0, len(lanes))
	for i := range lanes {
		lane, err := model.GetLaneById(lanes[i].Id)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		items = append(items, laneResponse(c, lane))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor})
}

// GetLane GET /api/v1/lanes/{name}
func GetLane(c *gin.Context) {
	lane, err := model.GetLaneByName(c.Param("name"))
	if err != nil {
		apierr.NotFound(c, apierr.CodeLaneNotFound, "lane '"+c.Param("name")+"' not found", "GET /api/v1/lanes")
		return
	}
	c.JSON(http.StatusOK, laneResponse(c, lane))
}

// PutLane PUT /api/v1/lanes/{name}：全量 upsert（成员数组顺序即写库顺序），写后回读。
func PutLane(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		apierr.Validation(c, "lane name is required")
		return
	}
	var payload lanePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	if payload.Name != "" && payload.Name != name {
		apierr.Validation(c, "body name does not match path")
		return
	}

	lane, buildErr := buildLane(name, &payload)
	if buildErr != nil {
		apierr.Write(c, buildErr.status, buildErr.code, buildErr.message, buildErr.hint)
		return
	}

	_, existingErr := model.GetLaneByName(name)
	action := "update"
	if errors.Is(existingErr, gorm.ErrRecordNotFound) {
		action = "add"
	}
	if dryRun(c) {
		dryRunResult(c, "lanes", action, name)
		return
	}

	if err := model.UpsertLane(lane); err != nil {
		writeAPIError(c, err)
		return
	}
	saved, err := model.GetLaneByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	response := laneResponse(c, saved)
	writeAudit(c, action, "lane", name, response)
	c.JSON(http.StatusOK, response)
}

// SeedLanes POST /api/v1/lanes/seed：为所有"渠道已声明但无车道"的模型生成
// failover 车道（初始顺序按渠道 id 升序，成员 upstream_model 留空 → 用渠道映射）。
// 幂等；`?dry_run=true` 只返回将创建的车道名（ADR 0005）。
func SeedLanes(c *gin.Context) {
	dry := dryRun(c)
	created, skipped, err := model.SeedLanes(dry)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if created == nil {
		created = []string{}
	}
	if skipped == nil {
		skipped = []string{}
	}
	if dry {
		c.JSON(http.StatusOK, gin.H{"dry_run": true, "created": created, "skipped": skipped})
		return
	}
	if len(created) > 0 {
		writeAudit(c, "seed", "lane", "", gin.H{"created": created})
	}
	c.JSON(http.StatusOK, gin.H{"created": created, "skipped": skipped})
}

// DeleteLane DELETE /api/v1/lanes/{name}
func DeleteLane(c *gin.Context) {
	name := c.Param("name")
	if _, err := model.GetLaneByName(name); err != nil {
		apierr.NotFound(c, apierr.CodeLaneNotFound, "lane '"+name+"' not found", "GET /api/v1/lanes")
		return
	}
	if dryRun(c) {
		dryRunResult(c, "lanes", "remove", name)
		return
	}
	if err := model.DeleteLaneByName(name); err != nil {
		writeAPIError(c, err)
		return
	}
	writeAudit(c, "delete", "lane", name)
	c.JSON(http.StatusOK, gin.H{"deleted": true, "name": name})
}

type laneBuildError struct {
	status  int
	code    string
	message string
	hint    string
}

func buildLane(name string, payload *lanePayload) (*model.Lane, *laneBuildError) {
	laneID := 0
	if existing, err := model.GetLaneByName(name); err == nil && existing != nil {
		laneID = existing.Id
	}
	lane := &model.Lane{Name: name, Enabled: true, Mode: model.LaneModeFailover}
	if payload.Enabled != nil {
		lane.Enabled = *payload.Enabled
	}
	lane.ActiveMember = strings.TrimSpace(payload.ActiveMember)
	if strings.TrimSpace(payload.Mode) != "" {
		lane.Mode = strings.TrimSpace(payload.Mode)
	}
	if !model.ValidLaneMode(lane.Mode) {
		return nil, &laneBuildError{status: http.StatusUnprocessableEntity, code: apierr.CodeInvalidMode,
			message: "invalid lane mode '" + lane.Mode + "'", hint: "failover|manual"}
	}

	if len(payload.Config) > 0 && string(payload.Config) != "null" {
		if !json.Valid(payload.Config) {
			return nil, &laneBuildError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
				message: "config must be valid json"}
		}
		var parsed model.LaneRelayConfig
		if err := json.Unmarshal(payload.Config, &parsed); err != nil {
			return nil, &laneBuildError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
				message: "config has invalid fields"}
		}
		normalized := parsed.Normalize()
		raw, _ := json.Marshal(normalized)
		lane.Config = string(raw)
	}
	// 车道名若同时被某个成员用作别名，解析会歧义，提前拒绝（api-spec §3 的 409 场景）。
	laneNames, err := model.LaneNames()
	if err != nil {
		return nil, &laneBuildError{status: http.StatusInternalServerError, code: "internal_error", message: err.Error()}
	}
	seenAliases := map[string]bool{}
	for i := range payload.Members {
		m := payload.Members[i]
		member, buildErr := buildLaneMember(name, laneID, laneNames, seenAliases, &m)
		if buildErr != nil {
			return nil, buildErr
		}
		lane.Members = append(lane.Members, *member)
	}
	if lane.Enabled && len(lane.Members) == 0 {
		return nil, &laneBuildError{status: http.StatusUnprocessableEntity, code: apierr.CodeLaneHasNoMembers,
			message: "enabled lane must have at least one member"}
	}
	return lane, nil
}

func buildLaneMember(laneName string, laneID int, laneNames []string, seenAliases map[string]bool, m *laneMemberPayload) (*model.LaneMember, *laneBuildError) {
	channelName := strings.TrimSpace(m.Channel)
	if channelName == "" {
		return nil, &laneBuildError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
			message: "member.channel is required"}
	}
	channel, err := findChannelByName(channelName)
	if err != nil {
		return nil, &laneBuildError{status: http.StatusUnprocessableEntity, code: apierr.CodeMemberChannelMissing,
			message: "member channel '" + channelName + "' not found", hint: "PUT /api/v1/channels/" + channelName}
	}
	// 成员 upstream_model 是可选覆盖：留空 → 运行期用渠道 model_mapping，再退回路由键（ADR 0005）。
	upstream := strings.TrimSpace(m.UpstreamModel)
	member := &model.LaneMember{
		ChannelId:     channel.Id,
		UpstreamModel: upstream,
		PublicAlias:   strings.TrimSpace(m.PublicAlias),
		Priority:      m.Priority,
	}
	if len(m.Overrides) > 0 && string(m.Overrides) != "null" {
		if !json.Valid(m.Overrides) {
			return nil, &laneBuildError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
				message: "member.overrides must be valid json"}
		}
		var parsed model.LaneRelayOverrides
		if err := json.Unmarshal(m.Overrides, &parsed); err != nil {
			return nil, &laneBuildError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
				message: "member.overrides has invalid fields"}
		}
		member.Overrides = string(m.Overrides)
	}
	if member.PublicAlias != "" {
		if member.PublicAlias == laneName || containsString(laneNames, member.PublicAlias) || seenAliases[member.PublicAlias] {
			return nil, &laneBuildError{status: http.StatusConflict, code: apierr.CodeConflict,
				message: "public_alias '" + member.PublicAlias + "' conflicts with an existing lane or alias"}
		}
		// 别名必须全局唯一：跨车道重名会让点名解析到任意车道（GetLaneByAlias 无排序）
		query := model.DB.Model(&model.LaneMember{}).Where("public_alias = ?", member.PublicAlias)
		if laneID > 0 {
			query = query.Where("lane_id <> ?", laneID)
		}
		var existing int64
		if err := query.Count(&existing).Error; err != nil {
			return nil, &laneBuildError{status: http.StatusInternalServerError, code: "internal_error", message: err.Error()}
		}
		if existing > 0 {
			return nil, &laneBuildError{status: http.StatusConflict, code: apierr.CodeConflict,
				message: "public_alias '" + member.PublicAlias + "' already used by another lane"}
		}
		seenAliases[member.PublicAlias] = true
	}
	return member, nil
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// PutLaneMembers PUT /api/v1/lanes/{name}/members：只替换成员列表（有序全量），
// 车道本身（模式/六键/启用状态）保持不变。
func PutLaneMembers(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	existing, err := model.GetLaneByName(name)
	if err != nil {
		apierr.NotFound(c, apierr.CodeLaneNotFound, "lane '"+name+"' not found", "PUT /api/v1/lanes/"+name)
		return
	}
	var payload struct {
		Members []laneMemberPayload `json:"members"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	laneNames, err := model.LaneNames()
	if err != nil {
		writeAPIError(c, err)
		return
	}
	seenAliases := map[string]bool{}
	members := make([]model.LaneMember, 0, len(payload.Members))
	for i := range payload.Members {
		member, buildErr := buildLaneMember(name, existing.Id, laneNames, seenAliases, &payload.Members[i])
		if buildErr != nil {
			apierr.Write(c, buildErr.status, buildErr.code, buildErr.message, buildErr.hint)
			return
		}
		members = append(members, *member)
	}
	if existing.Enabled && len(members) == 0 {
		apierr.Unprocessable(c, apierr.CodeLaneHasNoMembers, "enabled lane must have at least one member")
		return
	}
	if dryRun(c) {
		dryRunResult(c, "lanes", "update", name)
		return
	}
	updated := *existing
	updated.Members = members
	if err := model.UpsertLane(&updated); err != nil {
		writeAPIError(c, err)
		return
	}
	saved, err := model.GetLaneByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	response := laneResponse(c, saved)
	writeAudit(c, "update", "lane_members", name, response)
	c.JSON(http.StatusOK, response)
}
