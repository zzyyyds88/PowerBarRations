package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"pbr/common"
	"pbr/constant"
	"pbr/internal/apierr"
	"pbr/model"

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
	Name          string          `json:"name"`
	Type          *string         `json:"type"`
	BaseURL       *string         `json:"base_url"`
	Priority      *int            `json:"priority"`
	Models        []string        `json:"models"`
	ParamOverride json.RawMessage `json:"param_override"`
	Enabled       *bool           `json:"enabled"`
	Proxy         *string         `json:"proxy"`
	Weight        *int            `json:"weight"`
	Key           string          `json:"key"`
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
	resp := gin.H{
		"name":           ch.Name,
		"type":           ChannelTypeSlug(ch.Type),
		"base_url":       ch.GetBaseURL(),
		"priority":       int(ch.GetPriority()),
		"weight":         ch.GetWeight(),
		"models":         models,
		"param_override": jsonObject(derefString(ch.ParamOverride)),
		"enabled":        ch.Status == common.ChannelStatusEnabled,
		"proxy":          ch.GetSetting().Proxy,
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

	if isCreate {
		channel.CreatedTime = time.Now().Unix()
		channel.UpdatedAt = channel.CreatedTime
		if err := model.DB.Create(channel).Error; err != nil {
			writeAPIError(c, err)
			return
		}
		if err := channel.AddAbilities(nil); err != nil {
			writeAPIError(c, err)
			return
		}
	} else {
		channel.UpdatedAt = time.Now().Unix()
		if err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Select("*").Omit("id", "created_time").Updates(channel).Error; err != nil {
			writeAPIError(c, err)
			return
		}
		if err := channel.UpdateAbilities(nil); err != nil {
			writeAPIError(c, err)
			return
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
		// 引用清单放进 message：错误包络固定为 {code,message,hint}（api-spec §3），
		// 调用方需要"被哪些车道引用"才能自助解阻。
		apierr.Conflict(c, apierr.CodeConflict,
			"channel is referenced by lanes: "+strings.Join(laneNames, ", "),
			"PUT /api/v1/lanes/{name} to remove the members first")
		return
	}
	if dryRun(c) {
		dryRunResult(c, "channels", "remove", name)
		return
	}
	if err := ch.DeleteAbilities(); err != nil {
		writeAPIError(c, err)
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

	if payload.Priority != nil {
		priority := int64(*payload.Priority)
		channel.Priority = &priority
	}
	if payload.Weight != nil {
		// 负权重会被 uint 回绕成巨大值、落库成负数，再读回 *uint 时报 scan error，
		// 从而污染整张渠道缓存并让所有路由 500（实测 P0）。这里必须在写库前拒绝。
		if *payload.Weight < 0 || *payload.Weight > 2147483647 {
			return nil, &apiError{code: apierr.CodeValidationFailed, message: "weight must be between 0 and 2147483647"}
		}
		weight := uint(*payload.Weight)
		channel.Weight = &weight
	}
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

	// key 只写不读：省略则保留原值（新建时为空）。
	if strings.TrimSpace(payload.Key) != "" {
		channel.Key = payload.Key
	}
	return channel, nil
}
