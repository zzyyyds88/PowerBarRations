package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 渠道是 PBR 的厂商接入点：base_url + key + 模型清单 + 协议类型。
//
// 复用迁移基座的 channels 表与 40 家适配器（design-v1 §10.1）：
// type 对外是字符串 slug（openai/anthropic/gemini/...），对内是适配器枚举 int；
// models 对外是数组，对内是逗号分隔串；enabled 对内是 status（1 启用 / 2 手工禁用）。

// codeInvalidPrices 渠道价格结构校验失败（api-spec §4.1：非法 HH:MM、空 days、
// days 越界、超过 64 条 band、非法 timezone、负单价）。稳定 code：调用方据此把
// "价格写法不合法"与其它 400 区分开；信封仍走 apierr 的统一形状。
const codeInvalidPrices = "invalid_prices"

var channelTypeBySlug = func() map[string]int {
	m := make(map[string]int, len(constant.ChannelTypeNames))
	for t, name := range constant.ChannelTypeNames {
		m[slugify(name)] = t
	}
	return m
}()

func slugify(name string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "")
	return strings.ToLower(replacer.Replace(name))
}

// ChannelTypeSlug 渠道类型的对外字符串表示。
func ChannelTypeSlug(channelType int) string {
	return slugify(constant.GetChannelTypeName(channelType))
}

// AdapterList 返回全部可用适配器 slug（供 /capabilities 与错误提示使用）。
func AdapterList() []string {
	out := make([]string, 0, len(constant.ChannelTypeNames))
	for _, name := range constant.ChannelTypeNames {
		out = append(out, slugify(name))
	}
	return out
}

// channelPayload PUT 的请求体：完整对象 upsert。
//
// 指针/切片字段用来区分"字段缺席"（保持原值）与"显式置空"（清空）。
// key 特殊：省略即保留原值（api-spec §4.1），读侧永不返回明文。
type channelPayload struct {
	Name          string                   `json:"name"`
	Type          *string                  `json:"type"`
	BaseURL       *string                  `json:"base_url"`
	Models        []string                 `json:"models"`
	ParamOverride json.RawMessage          `json:"param_override"`
	Enabled       *bool                    `json:"enabled"`
	Proxy         *string                  `json:"proxy"`
	Key           string                   `json:"key"`
	Prices        *[]dto.ChannelModelPrice `json:"prices"`
	// ModelMapping 模型名 → 上游真名（ADR 0005/0008）；省略则保持原值。
	ModelMapping *map[string]string `json:"model_mapping"`
}

func channelResponse(ch *model.Channel) gin.H {
	models := make([]string, 0, 4)
	for _, m := range ch.GetModels() {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	// key_prefix 只是给人认的展示前缀，必须短于原文：短密钥不能整串回显（api-spec §4.1）。
	key := strings.TrimSpace(ch.Key)
	prefix := ""
	if len(key) > 4 {
		prefix = key[:4]
	} else if len(key) > 1 {
		prefix = key[:len(key)-1]
	}
	prices := ch.GetSetting().PBRPrices
	if prices == nil {
		prices = []dto.ChannelModelPrice{}
	}
	resp := gin.H{
		"name":           ch.Name,
		"type":           ChannelTypeSlug(ch.Type),
		"base_url":       ch.GetBaseURL(),
		"models":         models,
		"param_override": jsonObject(derefString(ch.ParamOverride)),
		"enabled":        ch.Status == common.ChannelStatusEnabled,
		"proxy":          ch.GetSetting().Proxy,
		"prices":         prices,
		"model_mapping":  ch.ModelMappingMap(),
		"key_set":        strings.TrimSpace(key) != "",
		"key_prefix":     prefix,
		"created_at":     rfc3339(ch.CreatedTime),
		"updated_at":     rfc3339(ch.UpdatedAt),
	}
	return resp
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ListChannels GET /api/v1/channels
func ListChannels(c *gin.Context) {
	limit, cursor, err := pageParams(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	query := model.DB.Model(&model.Channel{}).Order("name asc").Limit(limit + 1)
	if cursor != "" {
		query = query.Where("name > ?", cursor)
	}
	var channels []*model.Channel
	if err := query.Find(&channels).Error; err != nil {
		writeAPIError(c, err)
		return
	}
	items := make([]gin.H, 0, len(channels))
	var nextCursor any
	if len(channels) > limit {
		nextCursor = encodeCursor(channels[limit-1].Name)
		channels = channels[:limit]
	}
	for _, ch := range channels {
		items = append(items, channelResponse(ch))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor})
}

// GetChannel GET /api/v1/channels/{name}
func GetChannel(c *gin.Context) {
	ch, err := findChannelByName(c.Param("name"))
	if err != nil {
		if writeChannelLookupError(c, err) {
			return
		}
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+c.Param("name")+"' not found", "GET /api/v1/channels")
		return
	}
	c.JSON(http.StatusOK, channelResponse(ch))
}

// PutChannel PUT /api/v1/channels/{name}：全量 upsert，写后回读。
func PutChannel(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		apierr.Validation(c, "channel name is required")
		return
	}
	var payload channelPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	// body name 与 path 一致性校验前先 trim（与车道成员解析、legacy 入口同口径）。
	if trimmedBodyName := strings.TrimSpace(payload.Name); trimmedBodyName != "" && trimmedBodyName != name {
		apierr.Validation(c, "body name does not match path")
		return
	}

	existing, err := findChannelByName(name)
	isCreate := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isCreate {
		writeAPIError(c, err)
		return
	}

	channel, applyErr := buildChannel(name, existing, &payload, isCreate)
	if applyErr != nil {
		apierr.Write(c, http.StatusBadRequest, applyErr.code, applyErr.message, "")
		return
	}
	// 从渠道模型清单移除的模型，若仍被车道或成员组引用，默认拒绝（与 DELETE
	// 渠道、sync-models 同一口径）。?force=1 只允许清理散成员；组是跨车道复用
	// 单元，必须由用户显式编辑，不能由渠道写操作静默改组。
	var removedModels, referencedLanes, referencedGroups, cleanedLanes, deletedLanes, manualLanesCleared []string
	force := isForce(c)
	if !isCreate && payload.Models != nil {
		_, removedModels = diffModels(existing.GetModels(), channel.GetModels())
		refs, refErr := model.RemovedModelReferences(existing.Id, removedModels)
		if refErr != nil {
			writeAPIError(c, refErr)
			return
		}
		referencedLanes, referencedGroups = refs.Lanes, refs.Groups
		if len(referencedGroups) > 0 {
			apierr.WriteDetails(c, http.StatusConflict, apierr.CodeConflict,
				"refusing to remove models still referenced by member groups: "+strings.Join(referencedGroups, ", "),
				"edit the member groups to remove the members first",
				gin.H{"lanes": referencedLanes, "groups": referencedGroups})
			return
		}
		if len(referencedLanes) > 0 && !force {
			apierr.WriteDetails(c, http.StatusConflict, apierr.CodeConflict,
				"refusing to remove models still referenced by lanes: "+strings.Join(referencedLanes, ", "),
				"retry with ?force=1 to also remove this channel's members from those lanes",
				gin.H{"lanes": referencedLanes, "groups": referencedGroups})
			return
		}
	}
	if dryRun(c) {
		action := "update"
		if isCreate {
			action = "add"
		}
		dryRunResult(c, "channels", action, name)
		return
	}

	if isCreate {
		// 名字是寻址键：并发创建时 find 可能都返回 not-found，落库前再查一次。
		if taken, err := model.ChannelNameTaken(name, 0); err != nil {
			writeAPIError(c, err)
			return
		} else if taken {
			writeAPIError(c, &model.ChannelNameConflictError{Name: name, Reason: "taken"})
			return
		}
		channel.CreatedTime = time.Now().Unix()
		channel.UpdatedAt = channel.CreatedTime
		if err := model.DB.Create(channel).Error; err != nil {
			writeAPIError(c, err)
			return
		}
	} else {
		beforeStatus := existing.Status
		channel.UpdatedAt = time.Now().Unix()
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			// 提交前在同一事务里重查引用：防止检查后被并发插入车道/组成员，
			// 尤其不能把用户没在确认框里看到的“新引用”静默 force 清掉。
			if payload.Models != nil {
				refsNow, refErr := model.RemovedModelReferencesTx(tx, existing.Id, removedModels)
				if refErr != nil {
					return refErr
				}
				approved := make(map[string]bool, len(referencedLanes))
				for _, lane := range referencedLanes {
					approved[lane] = true
				}
				newLanes := make([]string, 0)
				for _, lane := range refsNow.Lanes {
					if !approved[lane] {
						newLanes = append(newLanes, lane)
					}
				}
				if len(refsNow.Groups) > 0 ||
					(len(refsNow.Lanes) > 0 && !force) ||
					len(newLanes) > 0 {
					return &apiError{
						status:  http.StatusConflict,
						code:    apierr.CodeConflict,
						message: "channel model references changed while saving; retry after reviewing the lanes",
						hint:    "reload the channel and confirm the new lane/member-group references",
						details: gin.H{
							"lanes":  refsNow.Lanes,
							"groups": refsNow.Groups,
							"new":    newLanes,
						},
					}
				}
			}
			if err := tx.Model(&model.Channel{}).Where("id = ?", channel.Id).
				Select("*").Omit("id", "created_time").Updates(channel).Error; err != nil {
				return err
			}
			if strings.TrimSpace(existing.Name) != strings.TrimSpace(channel.Name) {
				if _, err := model.RewriteManualActiveMemberChannelName(tx, existing.Name, channel.Name); err != nil {
					return err
				}
			}
			// 渠道模型收窄与车道成员清理必须同事务提交：任一步失败都不能留下
			// “渠道已不声明模型、车道仍引用”或“成员已删、车道空壳”的半状态。
			if len(referencedLanes) > 0 {
				cleaned, deleted, cleanupErr := model.CleanupLanesForRemovedModelsTx(tx, existing.Id, removedModels)
				if cleanupErr != nil {
					return cleanupErr
				}
				cleanedLanes, deletedLanes = cleaned, deleted
			}
			return nil
		})
		if err != nil {
			writeAPIError(c, err)
			return
		}
		model.NotifyChannelStatusChanged(channel, beforeStatus, channel.Status, "management upsert")
		for _, laneName := range deletedLanes {
			route.Default.Remove(laneName)
		}
		if len(cleanedLanes) > 0 {
			manualLanesCleared, _ = model.ReconcileManualActiveMembersByNames(cleanedLanes)
		}
	}
	model.InitChannelCache()

	// 写后回读：响应体是落库后重新读取的最终状态（api-spec §2.2）。
	saved, err := findChannelByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	response := channelResponse(saved)
	if len(cleanedLanes) > 0 || len(deletedLanes) > 0 {
		// 数组契约：空结果也要是 []，不能序列化成 null（api-spec §3）。
		if cleanedLanes == nil {
			cleanedLanes = []string{}
		}
		if deletedLanes == nil {
			deletedLanes = []string{}
		}
		response["cleaned_lanes"] = cleanedLanes
		response["deleted_lanes"] = deletedLanes
	}
	if len(manualLanesCleared) > 0 {
		response["manual_lanes_cleared"] = manualLanesCleared
	}
	action := "update"
	if isCreate {
		action = "create"
	}
	writeAudit(c, action, "channel", name, response)
	c.JSON(http.StatusOK, response)
}

// DeleteChannel DELETE /api/v1/channels/{name}
func DeleteChannel(c *gin.Context) {
	name := c.Param("name")
	ch, err := findChannelByName(name)
	if err != nil {
		if writeChannelLookupError(c, err) {
			return
		}
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+name+"' not found", "GET /api/v1/channels")
		return
	}
	// 被车道或成员组引用时禁止删除（api-spec §5.3 / ui-spec §6.5 要求给出
	// 引用清单）。ADR 0009 之后车道可以只引用成员组，必须两者一起查。
	refs, refErr := model.ChannelReferences(ch.Id)
	if refErr != nil {
		writeAPIError(c, refErr)
		return
	}
	if !refs.Empty() {
		parts := make([]string, 0, 2)
		if len(refs.Lanes) > 0 {
			parts = append(parts, "lanes: "+strings.Join(refs.Lanes, ", "))
		}
		if len(refs.Groups) > 0 {
			parts = append(parts, "member groups: "+strings.Join(refs.Groups, ", "))
		}
		// 引用清单同时进 message 与 error.details（lanes/groups）：message 给人读，
		// details 给调用方 AI 机器判定"被哪些车道/成员组引用"以自助解阻。
		apierr.WriteDetails(c, http.StatusConflict, apierr.CodeConflict,
			"channel is referenced by "+strings.Join(parts, "; "),
			"remove the members from the lanes or member groups first",
			gin.H{"lanes": refs.Lanes, "groups": refs.Groups})
		return
	}
	if dryRun(c) {
		dryRunResult(c, "channels", "remove", name)
		return
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		refsNow, refErr := model.ChannelReferencesTx(tx, ch.Id)
		if refErr != nil {
			return refErr
		}
		if !refsNow.Empty() {
			return &apiError{
				status:  http.StatusConflict,
				code:    apierr.CodeConflict,
				message: "channel is still referenced by lanes or member groups",
				hint:    "remove the members from the lanes or member groups first",
				details: gin.H{"lanes": refsNow.Lanes, "groups": refsNow.Groups},
			}
		}
		return tx.Delete(&model.Channel{}, ch.Id).Error
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	model.NotifyChannelDeleted(ch)
	model.InitChannelCache()
	writeAudit(c, "delete", "channel", name, channelResponse(ch))
	c.JSON(http.StatusOK, gin.H{"deleted": true, "name": name})
}

func findChannelByName(name string) (*model.Channel, error) {
	return model.GetChannelByName(name)
}

// buildChannel 把请求体叠加到已有渠道（或新建渠道）上。
func buildChannel(name string, existing *model.Channel, payload *channelPayload, isCreate bool) (*model.Channel, *apiError) {
	if strings.Contains(strings.TrimSpace(name), "/") {
		return nil, &apiError{code: apierr.CodeValidationFailed, message: "channel name cannot contain '/'"}
	}
	channel := &model.Channel{
		Name:   name,
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		Setting: func() *string {
			s := "{}"
			return &s
		}(),
	}
	if !isCreate && existing != nil {
		*channel = *existing
	}

	if payload.Type != nil {
		slug := slugify(*payload.Type)
		channelType, ok := channelTypeBySlug[slug]
		if !ok {
			return nil, &apiError{code: apierr.CodeValidationFailed, message: "unknown channel type '" + *payload.Type + "'"}
		}
		channel.Type = channelType
	} else if isCreate {
		return nil, &apiError{code: apierr.CodeValidationFailed, message: "type is required"}
	}

	if payload.BaseURL != nil {
		base := strings.TrimSpace(*payload.BaseURL)
		channel.BaseURL = &base
	}
	if isCreate && strings.TrimSpace(derefString(channel.BaseURL)) == "" {
		return nil, &apiError{code: apierr.CodeValidationFailed, message: "base_url is required"}
	}

	// 渠道 priority/weight 已删除：请求体里出现这两个字段一律忽略（保持旧客户端
	// 与旧导出文件的向后兼容），路由顺序只在车道上。
	if payload.Enabled != nil {
		if *payload.Enabled {
			channel.Status = common.ChannelStatusEnabled
		} else {
			channel.Status = common.ChannelStatusManuallyDisabled
		}
	}
	// models 是"路由键"的声明处：写进来即自动成链（routing-spec §1.1）。
	if payload.Models != nil {
		cleaned := make([]string, 0, len(payload.Models))
		for _, m := range payload.Models {
			if m = strings.TrimSpace(m); m != "" {
				cleaned = append(cleaned, m)
			}
		}
		channel.Models = strings.Join(cleaned, ",")
	} else if isCreate {
		channel.Models = ""
	}

	if len(payload.ParamOverride) > 0 && string(payload.ParamOverride) != "null" {
		// 必须是 JSON 对象：裸字符串/数组/数字都是合法 JSON，但合并时会出错，
		// 必须在写库前拒绝（api-spec §4.1 的 param_override 是对象）。
		trimmed := strings.TrimSpace(string(payload.ParamOverride))
		if !json.Valid(payload.ParamOverride) || !strings.HasPrefix(trimmed, "{") {
			return nil, &apiError{code: apierr.CodeValidationFailed, message: "param_override must be a json object"}
		}
		raw := string(payload.ParamOverride)
		channel.ParamOverride = &raw
	} else if isCreate {
		raw := "{}"
		channel.ParamOverride = &raw
	}

	if payload.Proxy != nil {
		setting := channel.GetSetting()
		setting.Proxy = strings.TrimSpace(*payload.Proxy)
		raw, err := json.Marshal(setting)
		if err != nil {
			return nil, &apiError{code: apierr.CodeValidationFailed, message: "invalid channel setting"}
		}
		encoded := string(raw)
		channel.Setting = &encoded
	}

	if payload.Prices != nil {
		// 渠道级上游单价（成本折算用）：模型名去空、去重，单价不得为负。
		cleaned := make([]dto.ChannelModelPrice, 0, len(*payload.Prices))
		seen := map[string]bool{}
		for _, item := range *payload.Prices {
			item.Model = strings.TrimSpace(item.Model)
			if item.Model == "" {
				return nil, &apiError{code: apierr.CodeValidationFailed, message: "prices: model name must not be empty"}
			}
			if seen[item.Model] {
				return nil, &apiError{code: apierr.CodeValidationFailed, message: "prices: duplicate model '" + item.Model + "'"}
			}
			seen[item.Model] = true
			// 峰谷结构校验（api-spec §4.1）：timezone 可解析、bands ≤64、days 1–7、
			// start/end 为 HH:MM 且不相等、band 单价非负。Normalize 先去掉首尾空白，
			// 保证落库值与匹配口径一致。
			item.Normalize()
			if err := item.Validate(); err != nil {
				return nil, &apiError{
					status:  http.StatusBadRequest,
					code:    codeInvalidPrices,
					message: "prices: model '" + item.Model + "': " + err.Error(),
				}
			}
			cleaned = append(cleaned, item)
		}
		setting := channel.GetSetting()
		setting.PBRPrices = cleaned
		raw, err := json.Marshal(setting)
		if err != nil {
			return nil, &apiError{code: apierr.CodeValidationFailed, message: "invalid channel prices"}
		}
		encoded := string(raw)
		channel.Setting = &encoded
	}

	if payload.ModelMapping != nil {
		// 渠道模型映射（模型名 → 上游真名）：两边都不得为空，值去空。
		cleaned := map[string]string{}
		for routeKey, upstream := range *payload.ModelMapping {
			routeKey = strings.TrimSpace(routeKey)
			upstream = strings.TrimSpace(upstream)
			if routeKey == "" {
				return nil, &apiError{code: apierr.CodeValidationFailed, message: "model_mapping: route key must not be empty"}
			}
			if upstream == "" {
				return nil, &apiError{code: apierr.CodeValidationFailed, message: "model_mapping: upstream model for '" + routeKey + "' must not be empty"}
			}
			cleaned[routeKey] = upstream
		}
		raw, err := json.Marshal(cleaned)
		if err != nil {
			return nil, &apiError{code: apierr.CodeValidationFailed, message: "invalid model_mapping"}
		}
		encoded := string(raw)
		channel.ModelMapping = &encoded
	}

	// key 只写不读：省略则保留原值（新建时为空）。
	if strings.TrimSpace(payload.Key) != "" {
		channel.Key = payload.Key
	}
	return channel, nil
}
