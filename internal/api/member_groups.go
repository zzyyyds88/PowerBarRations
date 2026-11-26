package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 成员组 CRUD（api-spec §5.2.1、ADR 0009）：组 = 可复用的有序成员子链。
// 组的启停即 `PUT /api/member-groups/{name}` 的 `enabled`——组全局关闭后，所有
// 引用它的车道里该组成员立即按"人工停用"跳过（不写冷却、不动熔断、留痕 disabled），
// 重新打开立即恢复（routing-spec §1.4）。

type memberGroupItemPayload struct {
	Channel string `json:"channel"`
	// Model 组内成员所选模型（成员身份，ADR 0008；上游真名由渠道映射推导）。
	Model       string          `json:"model"`
	PublicAlias string          `json:"public_alias"`
	Priority    int             `json:"priority"`
	Overrides   json.RawMessage `json:"overrides"`
	// Enabled 三态（api-spec §4.2 同口径）：省略 = 新建启用 / 既有保留原值。
	Enabled *bool `json:"enabled"`
}

type memberGroupPayload struct {
	Name    string                    `json:"name"`
	Enabled *bool                     `json:"enabled"`
	Members *[]memberGroupItemPayload `json:"members"`
}

// memberGroupResponse 组对象序列化（写后回读同形，api-spec §4.5）。
func memberGroupResponse(group *model.MemberGroup) gin.H {
	members := make([]gin.H, 0, len(group.Members))
	orphans := 0
	for _, m := range group.Members {
		channelName := ""
		if ch, err := model.ChannelOrNil(m.ChannelId); err == nil && ch != nil {
			channelName = ch.Name
		} else {
			orphans++
		}
		item := gin.H{
			"channel":      channelName,
			"model":        m.Model,
			"public_alias": m.PublicAlias,
			"priority":     m.Priority,
			// 对外正向 enabled（api-spec §4.2 命名规则）；落库是反向 Disabled。
			"enabled": !m.Disabled,
		}
		if channelName == "" {
			item["orphan"] = true
		}
		if overrides := jsonObject(m.Overrides); overrides != nil {
			item["overrides"] = overrides
		}
		members = append(members, item)
	}
	referencedBy, err := model.GroupsReferencedBy(group.Id)
	if err != nil {
		referencedBy = []string{}
	}
	return gin.H{
		"name":                group.Name,
		"enabled":             !group.Disabled,
		"members":             members,
		"orphan_member_count": orphans,
		"referenced_by":       referencedBy,
		"created_at":          rfc3339(group.CreatedAt),
		"updated_at":          rfc3339(group.UpdatedAt),
	}
}

// buildMemberGroup 解析并校验组写入 payload。existingMembers 用于 `enabled` 三态
// 默认值（省略时命中既有组内成员——按 (渠道, 所选模型) 匹配——保留原值）。
// resolve 可空（= 只查数据库）；导入构造阶段用它解析"bundle 声明但未落库"的渠道。
func buildMemberGroup(payload *memberGroupPayload, existing *model.MemberGroup, resolve channelResolver, db *gorm.DB) (*model.MemberGroup, error) {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, &apiError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed, message: "member group name is required"}
	}
	// members 省略 = 保留现有成员（PUT 语义，api-spec §5.2.1）；显式 [] = 422。
	if payload.Members == nil {
		if existing == nil {
			return nil, &apiError{status: http.StatusUnprocessableEntity, code: apierr.CodeGroupHasNoMembers,
				message: "member group must have at least one member"}
		}
		kept := *existing
		if payload.Enabled != nil {
			kept.Disabled = !*payload.Enabled
		}
		kept.Name = name
		return &kept, nil
	}
	if len(*payload.Members) == 0 {
		return nil, &apiError{status: http.StatusUnprocessableEntity, code: apierr.CodeGroupHasNoMembers,
			message: "member group must have at least one member"}
	}
	group := &model.MemberGroup{Name: name, Disabled: false}
	if payload.Enabled != nil {
		group.Disabled = !*payload.Enabled
	}
	seen := map[string]bool{}
	seenAliases := map[string]bool{}
	laneNames, laneErr := model.LaneNamesTx(db)
	if laneErr != nil {
		return nil, &apiError{status: http.StatusInternalServerError, code: "internal_error", message: laneErr.Error()}
	}
	var baseline []model.MemberGroupItem
	if existing != nil {
		baseline = existing.Members
	}
	for i := range *payload.Members {
		m := &(*payload.Members)[i]
		channelName := strings.TrimSpace(m.Channel)
		if channelName == "" {
			return nil, &apiError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
				message: "group member.channel is required"}
		}
		var channel *model.Channel
		if resolve != nil {
			if resolved, ok := resolve(channelName); ok {
				channel = resolved
			}
		}
		if channel == nil {
			found, err := model.GetChannelByNameTx(db, channelName)
			if err != nil {
				var ambiguous *model.ChannelNameAmbiguousError
				if errors.As(err, &ambiguous) {
					return nil, &apiError{status: http.StatusConflict, code: apierr.CodeChannelNameAmbiguous,
						message: ambiguous.Error()}
				}
				return nil, &apiError{status: http.StatusUnprocessableEntity, code: apierr.CodeMemberChannelMissing,
					message: "group member channel '" + channelName + "' not found"}
			}
			channel = found
		}
		selected := strings.TrimSpace(m.Model)
		if selected == "" {
			return nil, &apiError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
				message: "group member.model is required"}
		}
		key := strconv.Itoa(channel.Id) + "\u0000" + selected
		if seen[key] {
			return nil, &apiError{status: http.StatusUnprocessableEntity, code: apierr.CodeDuplicateMember,
				message: "duplicate group member: channel '" + channelName + "' model '" + selected + "' appears more than once"}
		}
		seen[key] = true
		enabled := true
		if m.Enabled != nil {
			enabled = *m.Enabled
		} else {
			for _, prev := range baseline {
				if prev.ChannelId == channel.Id && prev.Model == selected {
					enabled = !prev.Disabled
					break
				}
			}
		}
		item := model.MemberGroupItem{
			ChannelId:   channel.Id,
			Model:       selected,
			PublicAlias: strings.TrimSpace(m.PublicAlias),
			Priority:    m.Priority,
			Disabled:    !enabled,
		}
		if len(m.Overrides) > 0 && string(m.Overrides) != "null" {
			if _, configErr := model.ParseLaneRelayOverridesE(string(m.Overrides)); configErr != nil {
				return nil, &apiError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
					message: configErr.Message}
			}
			item.Overrides = string(m.Overrides)
		}
		if alias := item.PublicAlias; alias != "" {
			if seenAliases[alias] || containsString(laneNames, alias) {
				return nil, &apiError{status: http.StatusConflict, code: apierr.CodeConflict,
					message: "public_alias '" + alias + "' conflicts with an existing lane or group member"}
			}
			var laneAliasCount int64
			if err := db.Model(&model.LaneMember{}).Where("public_alias = ?", alias).
				Count(&laneAliasCount).Error; err != nil {
				return nil, &apiError{status: http.StatusInternalServerError, code: "internal_error", message: err.Error()}
			}
			aliasQuery := db.Model(&model.MemberGroupItem{}).Where("public_alias = ?", alias)
			if existing != nil {
				aliasQuery = aliasQuery.Where("group_id <> ?", existing.Id)
			}
			var groupAliasCount int64
			if err := aliasQuery.Count(&groupAliasCount).Error; err != nil {
				return nil, &apiError{status: http.StatusInternalServerError, code: "internal_error", message: err.Error()}
			}
			if laneAliasCount > 0 || groupAliasCount > 0 {
				return nil, &apiError{status: http.StatusConflict, code: apierr.CodeConflict,
					message: "public_alias '" + alias + "' is already used by another member"}
			}
			seenAliases[alias] = true
		}
		group.Members = append(group.Members, item)
	}
	return group, nil
}

// ListMemberGroups GET /api/member-groups：列表（不分页，含 referenced_by）。
func ListMemberGroups(c *gin.Context) {
	groups, err := model.ListMemberGroups()
	if err != nil {
		writeAPIError(c, err)
		return
	}
	items := make([]gin.H, 0, len(groups))
	for i := range groups {
		items = append(items, memberGroupResponse(&groups[i]))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nil})
}

// GetMemberGroup GET /api/member-groups/{name}。
func GetMemberGroup(c *gin.Context) {
	group, err := model.GetMemberGroupByName(c.Param("name"))
	if err != nil {
		if errors.Is(err, model.ErrMemberGroupNotFound) {
			apierr.NotFound(c, apierr.CodeMemberGroupNotFound, "member group '"+c.Param("name")+"' not found", "GET /api/member-groups")
			return
		}
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, memberGroupResponse(group))
}

// CreateMemberGroup POST /api/member-groups。
func CreateMemberGroup(c *gin.Context) {
	var payload memberGroupPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	name := strings.TrimSpace(payload.Name)
	payload.Name = name
	if _, err := model.GetMemberGroupByName(name); err == nil {
		apierr.Write(c, http.StatusConflict, apierr.CodeConflict, "member group '"+name+"' already exists", "")
		return
	} else if !errors.Is(err, model.ErrMemberGroupNotFound) {
		writeAPIError(c, err)
		return
	}
	group, buildErr := buildMemberGroup(&payload, nil, nil, model.DB)
	if buildErr != nil {
		writeAPIError(c, buildErr)
		return
	}
	if dryRun(c) {
		dryRunResult(c, "member_groups", "add", name)
		return
	}
	if err := model.UpsertMemberGroup(group); err != nil {
		writeAPIError(c, err)
		return
	}
	saved, err := model.GetMemberGroupByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	maintainAllRouteRuntime("create_member_group")
	response := memberGroupResponse(saved)
	writeAudit(c, "add", "member_group", name, response)
	c.JSON(http.StatusOK, response)
}

// UpdateMemberGroup PUT /api/member-groups/{name}：全量 upsert（members 省略=保留，
// 显式 [] → 422 group_has_no_members）。组配置改动对引用它的车道立即生效。
func UpdateMemberGroup(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	existing, err := model.GetMemberGroupByName(name)
	if err != nil {
		if errors.Is(err, model.ErrMemberGroupNotFound) {
			apierr.NotFound(c, apierr.CodeMemberGroupNotFound, "member group '"+name+"' not found", "PUT /api/member-groups/"+name)
			return
		}
		writeAPIError(c, err)
		return
	}
	var payload memberGroupPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	if payload.Name != "" && strings.TrimSpace(payload.Name) != name {
		apierr.Validation(c, "body name does not match path")
		return
	}
	payload.Name = name
	group, buildErr := buildMemberGroup(&payload, existing, nil, model.DB)
	if buildErr != nil {
		writeAPIError(c, buildErr)
		return
	}
	if dryRun(c) {
		dryRunResult(c, "member_groups", "update", name)
		return
	}
	if err := model.UpsertMemberGroup(group); err != nil {
		writeAPIError(c, err)
		return
	}
	// 组内成员被替换后，引用它的 manual 车道可能失去点名成员。只清空悬空
	// active_member，不自动改点其他人（manual 不静默换人），并把车道名回给前端。
	referencedLanes, refErr := model.GroupsReferencedBy(existing.Id)
	var manualLanesCleared []string
	if refErr == nil {
		manualLanesCleared, _ = model.ReconcileManualActiveMembersByNames(referencedLanes)
	}
	saved, err := model.GetMemberGroupByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	maintainAllRouteRuntime("update_member_group")
	if manualLanesCleared == nil {
		manualLanesCleared = []string{}
	}
	response := memberGroupResponse(saved)
	response["manual_lanes_cleared"] = manualLanesCleared
	writeAudit(c, "update", "member_group", name, response)
	c.JSON(http.StatusOK, response)
}

// DeleteMemberGroup DELETE /api/member-groups/{name}：级联删除所有车道上的组引用；
// 链因此为空的车道整条删除（api-spec §5.2.1）。被删车道的运行态一并清理。
func DeleteMemberGroup(c *gin.Context) {
	name := c.Param("name")
	if _, err := model.GetMemberGroupByName(name); err != nil {
		if errors.Is(err, model.ErrMemberGroupNotFound) {
			apierr.NotFound(c, apierr.CodeMemberGroupNotFound, "member group '"+name+"' not found", "DELETE /api/member-groups/"+name)
			return
		}
		writeAPIError(c, err)
		return
	}
	if dryRun(c) {
		dryRunResult(c, "member_groups", "remove", name)
		return
	}
	cleaned, deleted, err := model.DeleteMemberGroupByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	for _, laneName := range deleted {
		route.Default.Remove(laneName)
	}
	maintainAllRouteRuntime("delete_member_group")
	manualLanesCleared, _ := model.ReconcileManualActiveMembersByNames(cleaned)
	if cleaned == nil {
		cleaned = []string{}
	}
	if deleted == nil {
		deleted = []string{}
	}
	if manualLanesCleared == nil {
		manualLanesCleared = []string{}
	}
	writeAudit(c, "delete", "member_group", name, gin.H{
		"cleaned_lanes":        cleaned,
		"deleted_lanes":        deleted,
		"manual_lanes_cleared": manualLanesCleared,
	})
	c.JSON(http.StatusOK, gin.H{
		"deleted":              true,
		"name":                 name,
		"cleaned_lanes":        cleaned,
		"deleted_lanes":        deleted,
		"manual_lanes_cleared": manualLanesCleared,
	})
}
