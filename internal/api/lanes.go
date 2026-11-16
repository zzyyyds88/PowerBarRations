package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 车道是**唯一路由入口**（ADR 0005，见 docs/routing-spec-v1.md §1.1）：渠道
// Models 只是候选成员的来源，未固化成同名启用车道前该模型不可调用（503）。
// 自定义顺序 / 成员改名 / 池化不同上游的不同模型名都通过车道表达。

type laneMemberPayload struct {
	Channel string `json:"channel"`
	// Model 是成员所选、由该渠道声明的模型名（成员身份，ADR 0008）。
	// **不接受成员级上游真名**：真名一律由 `Channel.ModelMapping[Model] ?? Model` 推导，
	// 这样上游改名只在渠道配置一次、对所有成员立即生效。
	Model string `json:"model"`
	// Group 非 empty = 本元素是**成员组引用**（ADR 0009，api-spec §4.2 两态）：
	// Channel/Model/PublicAlias/Overrides 不适用（组内成员在组实体上维护），
	// Priority 由链顺序生成、Enabled 是车道级引用开关（三态口径同散成员）。
	Group       string          `json:"group"`
	PublicAlias string          `json:"public_alias"`
	Priority    int             `json:"priority"`
	Overrides   json.RawMessage `json:"overrides"`
	// Enabled 是**可空三态**：nil（请求省略）= 新建成员按启用、既有成员保留原值；
	// 显式 true/false 生效。成员写入是全量替换，若把"省略"当成 false，任何不带该
	// 字段的调用方一发请求就会把整条成员链关掉（api-spec §4.2）。
	Enabled *bool `json:"enabled"`
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

// laneMembersResponse 车道链元素的两态序列化（api-spec §4.2）：散成员含渠道字段，
// 组引用含组名/组全局开关（只读）/组成员数/车道级引用开关。laneResponse、GetRoute、
// ListLaneSummaries 共用同一实现，避免三处漂移。alwaysOverrides 控制"无覆盖时是否
// 恒回 {}"——只有编排器草稿来源（GET /routes/{model}）要求恒回（读写闭环，§4.2），
// 纯查看面按需给出。返回 members 与悬空渠道成员数。
func laneMembersResponse(lane *model.Lane, alwaysOverrides bool) ([]gin.H, int) {
	groupIds := make([]int, 0, 4)
	for _, m := range lane.Members {
		if m.GroupId != 0 {
			groupIds = append(groupIds, m.GroupId)
		}
	}
	groups := map[int]*model.MemberGroup{}
	if len(groupIds) > 0 {
		loaded, err := model.LoadMemberGroupsByIDs(groupIds)
		if err == nil {
			groups = loaded
		}
	}
	members := make([]gin.H, 0, len(lane.Members))
	orphans := 0
	for _, m := range lane.Members {
		if m.GroupId != 0 {
			item := gin.H{
				"group":    "",
				"priority": m.Priority,
				// 车道级引用开关（组实体的全局开关在 group_enabled 上另行给出）。
				"enabled": !m.Disabled,
			}
			if g, ok := groups[m.GroupId]; ok {
				item["group"] = g.Name
				item["group_enabled"] = !g.Disabled
				item["group_member_count"] = len(g.Members)
				// 组内渠道已不存在的成员：路由页的 orphan 统计必须把组展开，
				// 否则“清理悬空成员”入口看不见组内孤儿（ADR 0009）。
				groupOrphans := 0
				for _, item := range g.Members {
					if !model.ChannelExistsByID(item.ChannelId) {
						groupOrphans++
					}
				}
				item["group_orphan_member_count"] = groupOrphans
				orphans += groupOrphans
			}
			members = append(members, item)
			continue
		}
		channelName := ""
		channelEnabled := false
		if ch, err := model.ChannelOrNil(m.ChannelId); err == nil && ch != nil {
			channelName = ch.Name
			channelEnabled = ch.Status == common.ChannelStatusEnabled
		} else {
			// 渠道已不存在：悬空成员，请求打到它只会失败（清理入口见
			// POST /api/v1/lanes/members/cleanup）。
			orphans++
		}
		item := gin.H{
			"channel": channelName,
			// model = 成员所选模型（写回用）；upstream_model = 派生真名（只读展示，ADR 0008）。
			"model":           m.Model,
			"upstream_model":  model.EffectiveUpstreamForMember(m.ChannelId, m.Model),
			"public_alias":    m.PublicAlias,
			"priority":        m.Priority,
			"channel_enabled": channelEnabled,
			// member_id 仅供排障与审计定位（整体替换后必然变化，界面不得用作行标识）。
			"member_id": m.Id,
			// 对外一律正向 enabled（api-spec §4.2 命名规则）：落库是反向 Disabled。
			"enabled": !m.Disabled,
		}
		if channelName == "" {
			item["orphan"] = true
		}
		if overrides := jsonObject(m.Overrides); overrides != nil {
			item["overrides"] = overrides
		} else if alwaysOverrides {
			item["overrides"] = map[string]any{}
		}
		members = append(members, item)
	}
	return members, orphans
}

func laneResponse(c *gin.Context, lane *model.Lane) gin.H {
	members, orphans := laneMembersResponse(lane, false)
	return gin.H{
		"name":                lane.Name,
		"enabled":             lane.Enabled,
		"mode":                lane.Mode,
		"active_member":       lane.ActiveMember,
		"config":              model.ParseLaneRelayConfig(lane.Config),
		"members":             members,
		"orphan_member_count": orphans,
		"created_at":          rfc3339(lane.CreatedAt),
		"updated_at":          rfc3339(lane.UpdatedAt),
	}
}

// CleanupLaneMembers POST /api/v1/lanes/members/cleanup
//
// 清理"渠道已不存在"的悬空成员（历史数据修复入口）：散成员与成员组内成员一并
// 处理；组因此被清空时整组删除（组不允许空成员），并级联清理车道引用与空车道。
// `?dry_run=true` 只返回将受影响的车道/成员组名。
func CleanupLaneMembers(c *gin.Context) {
	if dryRun(c) {
		lanes, groups, err := model.ListOrphanMemberReferences()
		if err != nil {
			writeAPIError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"dry_run": true, "lanes": lanes, "groups": groups})
		return
	}
	result, err := model.CleanupOrphanMembers()
	if err != nil {
		writeAPIError(c, err)
		return
	}
	for _, name := range result.DeletedLanes {
		route.Default.Remove(name)
	}
	manualLanesCleared, _ := model.ReconcileManualActiveMembersByNames(result.CleanedLanes)
	if result.CleanedLanes == nil {
		result.CleanedLanes = []string{}
	}
	if result.DeletedLanes == nil {
		result.DeletedLanes = []string{}
	}
	if result.CleanedGroups == nil {
		result.CleanedGroups = []string{}
	}
	if result.DeletedGroups == nil {
		result.DeletedGroups = []string{}
	}
	if manualLanesCleared == nil {
		manualLanesCleared = []string{}
	}
	if len(result.CleanedLanes) > 0 || len(result.DeletedLanes) > 0 ||
		len(result.CleanedGroups) > 0 || len(result.DeletedGroups) > 0 {
		writeAudit(c, "cleanup-members", "lane", "", gin.H{
			"cleaned": result.CleanedLanes, "deleted": result.DeletedLanes,
			"cleaned_groups": result.CleanedGroups, "deleted_groups": result.DeletedGroups,
			"manual_lanes_cleared": manualLanesCleared,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"cleaned_lanes":        result.CleanedLanes,
		"deleted_lanes":        result.DeletedLanes,
		"cleaned_groups":       result.CleanedGroups,
		"deleted_groups":       result.DeletedGroups,
		"manual_lanes_cleared": manualLanesCleared,
	})
}

// ListLanes GET /api/v1/lanes
//
// 只列车道；渠道声明但未配车道的模型不是车道对象，见 GET /api/v1/models。
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
	if err := model.ValidateLaneActiveMember(lane); err != nil {
		var activeErr *model.LaneActiveMemberError
		if errors.As(err, &activeErr) {
			apierr.Write(c, http.StatusUnprocessableEntity, apierr.CodeInvalidActiveMember,
				activeErr.Error(),
				"pick an active member from the final expanded member chain, or disable the lane")
			return
		}
		writeAPIError(c, err)
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
	// 清理进程内运行态（冷却/熔断/探测槽/亲和）：运行态以车道名为键，车道删除后
	// 不移除会让 SnapshotLanes 一直保留已不存在的车道，SSE 快照每轮为它多解析一次
	// （routing-spec §1.3）。
	route.Default.Remove(name)
	writeAudit(c, "delete", "lane", name)
	c.JSON(http.StatusOK, gin.H{"deleted": true, "name": name})
}

type laneBuildError struct {
	status  int
	code    string
	message string
	hint    string
}

// channelResolver 在构造车道成员时把渠道名解析成已构造的渠道。
//
// 导入是两阶段：构造车道时本 bundle 的新渠道还没落库，因此不能只查数据库。
// 传 nil 表示"只查数据库"（普通 PUT /lanes 路径）。
type channelResolver func(name string) (*model.Channel, bool)

// groupResolver 同理：把组名解析成组（bundle 声明但未落库的组走内存构造体）。
// 传 nil 表示"只查数据库"。
type groupResolver func(name string) (*model.MemberGroup, bool)

// laneBuildLookup 车道构造的外部解析依赖（均可空 = 全部只查数据库）。
//
// db 非空时所有查询走该句柄（事务内必须传 tx：SQLite 连接池默认 1，事务内用
// 全局 DB 会等待第二条连接自锁）。
type laneBuildLookup struct {
	channel channelResolver
	group   groupResolver
	db      *gorm.DB
}

func buildLane(name string, payload *lanePayload, lookups ...*laneBuildLookup) (*model.Lane, *laneBuildError) {
	var lookup *laneBuildLookup
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
	var resolve channelResolver
	var resolveGroup groupResolver
	db := model.DB
	if lookup != nil {
		resolve = lookup.channel
		resolveGroup = lookup.group
		if lookup.db != nil {
			db = lookup.db
		}
	}
	laneID := 0
	var existing *model.Lane
	if e, err := model.GetLaneByNameTx(db, name); err == nil && e != nil {
		existing = e
		laneID = e.Id
	}
	// 反向冲突：新车道名不能撞上任何既有成员别名（散成员或组内成员），
	// 否则 ResolveRoute 的车道名优先会让旧别名静默失效。
	{
		var aliasCount int64
		query := db.Model(&model.LaneMember{}).Where("public_alias = ?", name)
		if laneID > 0 {
			query = query.Where("lane_id <> ?", laneID)
		}
		if err := query.Count(&aliasCount).Error; err != nil {
			return nil, &laneBuildError{status: http.StatusInternalServerError, code: "internal_error", message: err.Error()}
		}
		var groupAliasCount int64
		if err := db.Model(&model.MemberGroupItem{}).Where("public_alias = ?", name).
			Count(&groupAliasCount).Error; err != nil {
			return nil, &laneBuildError{status: http.StatusInternalServerError, code: "internal_error", message: err.Error()}
		}
		if aliasCount > 0 || groupAliasCount > 0 {
			return nil, &laneBuildError{status: http.StatusConflict, code: apierr.CodeConflict,
				message: "lane name '" + name + "' conflicts with an existing member alias"}
		}
	}
	lane := &model.Lane{Name: name, Enabled: true, Mode: model.LaneModeFailover}
	if existing != nil {
		// 部分更新语义：字段缺席 = 保持原值。此前只有 members 做了三态，
		// 只改启用开关会把 manual 静默改回 failover 并重置六键。
		lane.Enabled = existing.Enabled
		lane.Mode = existing.Mode
		lane.Config = existing.Config
		lane.ActiveMember = existing.ActiveMember
	}
	if payload.Enabled != nil {
		lane.Enabled = *payload.Enabled
	}
	if trimmed := strings.TrimSpace(payload.ActiveMember); trimmed != "" {
		lane.ActiveMember = trimmed
	}
	if trimmed := strings.TrimSpace(payload.Mode); trimmed != "" {
		lane.Mode = trimmed
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
	laneNames, err := model.LaneNamesTx(db)
	if err != nil {
		return nil, &laneBuildError{status: http.StatusInternalServerError, code: "internal_error", message: err.Error()}
	}
	seenAliases := map[string]bool{}
	// 既有成员链只作为 `enabled` 三态默认值的比对基线；不写回 lane（写回走下面的
	// 整体替换或 else 分支的"省略即保留"）。
	var baselineMembers []model.LaneMember
	if existing != nil {
		baselineMembers = existing.Members
	}
	if payload.Members != nil {
		// 散成员之间判重（api-spec §4.2；组不排他，ADR 0009：组内成员与散成员、
		// 组与组允许重叠——判重只在散成员集合内做，同一散成员对重复提交 422）。
		seenLoose := map[string]bool{}
		for i := range payload.Members {
			m := payload.Members[i]
			if strings.TrimSpace(m.Group) == "" {
				dedupKey := strings.TrimSpace(m.Channel) + "\u0000" + strings.TrimSpace(m.Model)
				if seenLoose[dedupKey] {
					return nil, &laneBuildError{status: http.StatusUnprocessableEntity, code: apierr.CodeDuplicateMember,
						message: "duplicate member: channel '" + strings.TrimSpace(m.Channel) +
							"' model '" + strings.TrimSpace(m.Model) + "' appears more than once",
						hint: "a loose member is identified by (channel, model); list each pair once"}
				}
				seenLoose[dedupKey] = true
			}
			member, buildErr := buildLaneMember(name, laneID, laneNames, seenAliases, &m, resolve, resolveGroup, baselineMembers, db)
			if buildErr != nil {
				return nil, buildErr
			}
			lane.Members = append(lane.Members, *member)
		}
	} else if existing != nil {
		// members 省略 = 保留现有成员链与点名成员（防呆：只改开关/六键/启停不再
		// 静默清空成员——{"enabled":false} 曾是数据丢失路径）；显式 [] 才清空。
		lane.Members = existing.Members
		if strings.TrimSpace(payload.ActiveMember) == "" {
			lane.ActiveMember = existing.ActiveMember
		}
	}
	if lane.Enabled && len(lane.Members) == 0 {
		return nil, &laneBuildError{status: http.StatusUnprocessableEntity, code: apierr.CodeLaneHasNoMembers,
			message: "enabled lane must have at least one member"}
	}
	return lane, nil
}

// buildLaneMember 解析并校验单个链元素请求（散成员或组引用，api-spec §4.2 两态）。
// existingMembers 是该车道**当前**落库的成员链（新建车道传 nil），仅用于实现
// `enabled` 的三态默认值。
func buildLaneMember(laneName string, laneID int, laneNames []string, seenAliases map[string]bool, m *laneMemberPayload, resolve channelResolver, resolveGroup groupResolver, existingMembers []model.LaneMember, db *gorm.DB) (*model.LaneMember, *laneBuildError) {
	// 组引用元素（ADR 0009）：{group, priority, enabled}；组内成员在组实体上维护。
	if groupName := strings.TrimSpace(m.Group); groupName != "" {
		return buildLaneGroupRef(groupName, m, resolveGroup, existingMembers, db)
	}
	channelName := strings.TrimSpace(m.Channel)
	if channelName == "" {
		return nil, &laneBuildError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
			message: "member.channel is required"}
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
				return nil, &laneBuildError{status: http.StatusConflict, code: apierr.CodeChannelNameAmbiguous,
					message: ambiguous.Error(), hint: "rename or delete the duplicate channels, then retry"}
			}
			return nil, &laneBuildError{status: http.StatusUnprocessableEntity, code: apierr.CodeMemberChannelMissing,
				message: "member channel '" + channelName + "' not found", hint: "PUT /api/v1/channels/" + channelName}
		}
		channel = found
	}
	// 成员所选模型：必填（成员身份的一半，另一半是渠道）。
	selected := strings.TrimSpace(m.Model)
	if selected == "" {
		return nil, &laneBuildError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
			message: "member.model is required", hint: "pick a model declared by the channel"}
	}
	// `enabled` 的三态默认值（api-spec §4.2）：显式给出以给出为准；省略时命中既有成员
	// 则保留其原值、否则按启用。"既有成员"按成员唯一键 (channel_id, model) 匹配
	// （ADR 0008）——改了这个键即视为新成员，按启用处理。
	// 绝不能把"省略"当成关闭：成员写入是全量替换，那样任何不带该字段的调用方
	// 一发请求就会把整条成员链关掉。
	enabled := true
	if m.Enabled != nil {
		enabled = *m.Enabled
	} else {
		for _, prev := range existingMembers {
			if prev.ChannelId == channel.Id && prev.Model == selected {
				enabled = !prev.Disabled
				break
			}
		}
	}
	member := &model.LaneMember{
		ChannelId:   channel.Id,
		Model:       selected,
		PublicAlias: strings.TrimSpace(m.PublicAlias),
		Priority:    m.Priority,
		Disabled:    !enabled,
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
		query := db.Model(&model.LaneMember{}).Where("public_alias = ?", member.PublicAlias)
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
		var groupAliasCount int64
		if err := db.Model(&model.MemberGroupItem{}).
			Where("public_alias = ?", member.PublicAlias).
			Count(&groupAliasCount).Error; err != nil {
			return nil, &laneBuildError{status: http.StatusInternalServerError, code: "internal_error", message: err.Error()}
		}
		if groupAliasCount > 0 {
			return nil, &laneBuildError{status: http.StatusConflict, code: apierr.CodeConflict,
				message: "public_alias '" + member.PublicAlias + "' already used by a member group"}
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

// buildLaneGroupRef 构造组引用元素：只携带 GroupId（组名解析）、Priority（链位置）
// 与车道级引用开关（三态 `enabled`，匹配既有引用按 GroupId）。resolveGroup 可空
// （= 只查数据库）；导入构造阶段用它解析"bundle 声明但未落库"的组。
func buildLaneGroupRef(groupName string, m *laneMemberPayload, resolveGroup groupResolver, existingMembers []model.LaneMember, db *gorm.DB) (*model.LaneMember, *laneBuildError) {
	var group *model.MemberGroup
	if resolveGroup != nil {
		if resolved, ok := resolveGroup(groupName); ok {
			group = resolved
		}
	}
	if group == nil {
		found, err := model.GetMemberGroupByNameTx(db, groupName)
		if err != nil {
			if errors.Is(err, model.ErrMemberGroupNotFound) {
				return nil, &laneBuildError{status: http.StatusUnprocessableEntity, code: apierr.CodeUnknownMemberGroup,
					message: "member group '" + groupName + "' not found",
					hint:    "GET /api/member-groups lists available groups"}
			}
			return nil, &laneBuildError{status: http.StatusInternalServerError, code: apierr.CodeInternalError, message: err.Error()}
		}
		group = found
	}
	// `enabled` 三态同散成员（api-spec §4.2）：显式给出以给出为准；省略时命中既有
	// 引用（按 GroupId 匹配）保留原值、否则按启用。绝不能把"省略"当成关闭。
	enabled := true
	if m.Enabled != nil {
		enabled = *m.Enabled
	} else {
		for _, prev := range existingMembers {
			if prev.GroupId == group.Id {
				enabled = !prev.Disabled
				break
			}
		}
	}
	return &model.LaneMember{
		GroupId:  group.Id,
		Priority: m.Priority,
		Disabled: !enabled,
	}, nil
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
	// 散成员之间判重（组不排他，ADR 0009：组内成员与散成员允许重叠）。
	seenLoose := map[string]bool{}
	members := make([]model.LaneMember, 0, len(payload.Members))
	for i := range payload.Members {
		if strings.TrimSpace(payload.Members[i].Group) == "" {
			dedupKey := strings.TrimSpace(payload.Members[i].Channel) + "\u0000" + strings.TrimSpace(payload.Members[i].Model)
			if seenLoose[dedupKey] {
				apierr.Unprocessable(c, apierr.CodeDuplicateMember,
					"duplicate member: channel '"+strings.TrimSpace(payload.Members[i].Channel)+
						"' model '"+strings.TrimSpace(payload.Members[i].Model)+"' appears more than once")
				return
			}
			seenLoose[dedupKey] = true
		}
		member, buildErr := buildLaneMember(name, existing.Id, laneNames, seenAliases, &payload.Members[i], nil, nil, existing.Members, model.DB)
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
	updated := *existing
	updated.Members = members
	if err := model.ValidateLaneActiveMember(&updated); err != nil {
		var activeErr *model.LaneActiveMemberError
		if errors.As(err, &activeErr) {
			apierr.Write(c, http.StatusUnprocessableEntity, apierr.CodeInvalidActiveMember,
				activeErr.Error(),
				"pick an active member from the final expanded member chain, or disable the lane")
			return
		}
		writeAPIError(c, err)
		return
	}
	if dryRun(c) {
		dryRunResult(c, "lanes", "update", name)
		return
	}
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
