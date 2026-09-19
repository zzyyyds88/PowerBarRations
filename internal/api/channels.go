package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
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
	// ModelMapping 路由键 → 上游真名（ADR 0005）；省略则保持原值。
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
	if payload.Name != "" && payload.Name != name {
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
	if dryRun(c) {
		action := "update"
		if isCreate {
			action = "add"
		}
		dryRunResult(c, "channels", action, name)
		return
	}

	// 从渠道模型清单移除的模型，若命中同名车道且该车道有本渠道成员，默认拒绝
	// （与 DELETE 渠道、sync-models 同一口径）；?force=1 则继续并在落库后清理这些
	// 车道上的本渠道成员（成员清空的空车道整条删除）。否则会出现"渠道不再声明、
	// 路由页仍显示可调用"的静默不一致。
	var removedModels, referencedLanes, cleanedLanes, deletedLanes []string
	if !isCreate && payload.Models != nil {
		_, removedModels = diffModels(existing.GetModels(), channel.GetModels())
		refs, refErr := model.RemovedModelLaneRefs(existing.Id, removedModels)
		if refErr != nil {
			writeAPIError(c, refErr)
			return
		}
		if len(refs) > 0 && !isForce(c) {
			apierr.WriteDetails(c, http.StatusConflict, apierr.CodeConflict,
				"refusing to remove models still referenced by lanes: "+strings.Join(refs, ", "),
				"retry with ?force=1 to also remove this channel's members from those lanes",
				gin.H{"lanes": refs})
			return
		}
		referencedLanes = refs
	}

	if isCreate {
		channel.CreatedTime = time.Now().Unix()
		channel.UpdatedAt = channel.CreatedTime
		if err := model.DB.Create(channel).Error; err != nil {
			writeAPIError(c, err)
			return
		}
	} else {
		channel.UpdatedAt = time.Now().Unix()
		if err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Select("*").Omit("id", "created_time").Updates(channel).Error; err != nil {
			writeAPIError(c, err)
			return
		}
	}
	model.InitChannelCache()

	// force 覆盖后的清理：从受影响车道移除本渠道成员；空车道删除并清运行态。
	if len(referencedLanes) > 0 {
		cleaned, deleted, cleanupErr := model.CleanupLanesForRemovedModels(existing.Id, removedModels)
		if cleanupErr != nil {
			writeAPIError(c, cleanupErr)
			return
		}
		for _, laneName := range deleted {
			route.Default.Remove(laneName)
		}
		cleanedLanes, deletedLanes = cleaned, deleted
	}

	// 写后回读：响应体是落库后重新读取的最终状态（api-spec §2.2）。
	saved, err := findChannelByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	response := channelResponse(saved)
	if len(cleanedLanes) > 0 || len(deletedLanes) > 0 {
		response["cleaned_lanes"] = cleanedLanes
		response["deleted_lanes"] = deletedLanes
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
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+name+"' not found", "GET /api/v1/channels")
		return
	}
	// 被显式车道成员引用时禁止删除（api-spec §5.3 / ui-spec §6.5 要求给出引用清单）。
	var members []model.LaneMember
	if err := model.DB.Model(&model.LaneMember{}).Where("channel_id = ?", ch.Id).Find(&members).Error; err != nil {
		writeAPIError(c, err)
		return
	}
	if len(members) > 0 {
		laneNames := make([]string, 0, len(members))
		seen := map[int]bool{}
		for _, m := range members {
			if seen[m.LaneId] {
				continue
			}
			seen[m.LaneId] = true
			var lane model.Lane
			if err := model.DB.Select("name").Where("id = ?", m.LaneId).First(&lane).Error; err == nil {
				laneNames = append(laneNames, lane.Name)
			}
		}
		sort.Strings(laneNames)
		// 引用清单同时进 message 与 error.details.lanes（api-spec §3"details.lanes"、
		// §5.3 DELETE）：message 给人读，details 给调用方 AI 机器判定"被哪些车道
		// 引用"以自助解阻，不得要求解析 message。与 PUT 收窄模型的口径一致。
		apierr.WriteDetails(c, http.StatusConflict, apierr.CodeConflict,
			"channel is referenced by lanes: "+strings.Join(laneNames, ", "),
			"PUT /api/v1/lanes/{name} to remove the members first",
			gin.H{"lanes": laneNames})
		return
	}
	if dryRun(c) {
		dryRunResult(c, "channels", "remove", name)
		return
	}
	if err := model.DB.Delete(&model.Channel{}, ch.Id).Error; err != nil {
		writeAPIError(c, err)
		return
	}
	model.InitChannelCache()
	writeAudit(c, "delete", "channel", name, channelResponse(ch))
	c.JSON(http.StatusOK, gin.H{"deleted": true, "name": name})
}

func findChannelByName(name string) (*model.Channel, error) {
	var ch model.Channel
	if err := model.DB.Where("name = ?", name).First(&ch).Error; err != nil {
		return nil, err
	}
	return &ch, nil
}

// buildChannel 把请求体叠加到已有渠道（或新建渠道）上。
func buildChannel(name string, existing *model.Channel, payload *channelPayload, isCreate bool) (*model.Channel, *apiError) {
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
			if item.Input < 0 || item.Output < 0 || item.CacheRead < 0 || item.CacheWrite < 0 {
				return nil, &apiError{code: apierr.CodeValidationFailed, message: "prices: unit price must be >= 0"}
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
		// 渠道模型映射（路由键 → 上游真名）：两边都不得为空，值去空。
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
