package controller

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/middleware"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relay"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/ai360"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/lingyiwanwu"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/minimax"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/moonshot"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// https://platform.openai.com/docs/api-reference/models/list

var openAIModels []dto.OpenAIModels
var openAIModelsMap map[string]dto.OpenAIModels
var channelId2Models map[int][]string

func init() {
	// https://platform.openai.com/docs/models/model-endpoint-compatibility
	for i := range constant.APITypeDummy {
		if i == constant.APITypeAIProxyLibrary {
			continue
		}
		adaptor := relay.GetAdaptor(i)
		channelName := adaptor.GetChannelName()
		modelNames := adaptor.GetModelList()
		for _, modelName := range modelNames {
			openAIModels = append(openAIModels, dto.OpenAIModels{
				Id:      modelName,
				Object:  "model",
				Created: 1626777600,
				OwnedBy: channelName,
			})
		}
	}
	for _, modelName := range ai360.ModelList {
		openAIModels = append(openAIModels, dto.OpenAIModels{
			Id:      modelName,
			Object:  "model",
			Created: 1626777600,
			OwnedBy: ai360.ChannelName,
		})
	}
	for _, modelName := range moonshot.ModelList {
		openAIModels = append(openAIModels, dto.OpenAIModels{
			Id:      modelName,
			Object:  "model",
			Created: 1626777600,
			OwnedBy: moonshot.ChannelName,
		})
	}
	for _, modelName := range lingyiwanwu.ModelList {
		openAIModels = append(openAIModels, dto.OpenAIModels{
			Id:      modelName,
			Object:  "model",
			Created: 1626777600,
			OwnedBy: lingyiwanwu.ChannelName,
		})
	}
	for _, modelName := range minimax.ModelList {
		openAIModels = append(openAIModels, dto.OpenAIModels{
			Id:      modelName,
			Object:  "model",
			Created: 1626777600,
			OwnedBy: minimax.ChannelName,
		})
	}
	openAIModelsMap = make(map[string]dto.OpenAIModels)
	for _, aiModel := range openAIModels {
		openAIModelsMap[aiModel.Id] = aiModel
	}
	channelId2Models = make(map[int][]string)
	for i := 1; i <= constant.ChannelTypeDummy; i++ {
		apiType, success := common.ChannelType2APIType(i)
		if !success || apiType == constant.APITypeAIProxyLibrary {
			continue
		}
		meta := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: i,
		}}
		adaptor := relay.GetAdaptor(apiType)
		adaptor.Init(meta)
		channelId2Models[i] = adaptor.GetModelList()
	}
	openAIModels = lo.UniqBy(openAIModels, func(m dto.OpenAIModels) string {
		return m.Id
	})
}

func channelOwnerName(channelType int) string {
	apiType, success := common.ChannelType2APIType(channelType)
	if !success {
		return strings.ToLower(constant.GetChannelTypeName(channelType))
	}
	adaptor := relay.GetAdaptor(apiType)
	if adaptor == nil {
		return strings.ToLower(constant.GetChannelTypeName(channelType))
	}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType: channelType,
	}})
	if name := strings.TrimSpace(adaptor.GetChannelName()); name != "" {
		return name
	}
	return strings.ToLower(constant.GetChannelTypeName(channelType))
}

func buildOpenAIModel(modelName string, ownerByModel map[string]string) dto.OpenAIModels {
	var oaiModel dto.OpenAIModels
	if staticModel, ok := openAIModelsMap[modelName]; ok {
		oaiModel = staticModel
	} else {
		oaiModel = dto.OpenAIModels{
			Id:      modelName,
			Object:  "model",
			Created: 1626777600,
			OwnedBy: "custom",
		}
	}
	if owner, ok := ownerByModel[modelName]; ok && owner != "" {
		oaiModel.OwnedBy = owner
	}
	oaiModel.SupportedEndpointTypes = model.GetModelSupportEndpointTypes(modelName)
	return oaiModel
}

// visibleLane 一条对当前客户端密钥可见的车道（design-v1 §4：GET /v1/models 列出
// 当前密钥可见的车道）。routing key 即车道名，preferredMember 是车道内优先级最高的
// 成员（ListLanes 已按 priority desc, id asc 排好成员），用于给 owned_by 标注首选归属。
type visibleLane struct {
	name            string
	preferredMember *model.LaneMember
}

// visibleLanesFor 从车道注册表枚举密钥可见的模型清单：
//
//   - 身份只认 PBR 客户端密钥（W7 后模型面唯一凭据）；取不到身份 → 空清单，
//     **不回退 group/abilities 枚举**；
//   - LanePolicy（token-spec §3.2）：Mode=all（含脏值/未配置，ParseLanePolicy 宽容回落）
//     枚举全部车道；Mode=allow 时候选 = AllowLanes，再减去 DenyLanes；
//   - 只计入"启用车道且有成员"的路由键：停用车道与空成员车道在运行期必然 503
//     （ResolveRoute 返回空链），列出来只会误导客户端；
//   - manual 车道同样是车道名的唯一路由入口且可调用（routing-spec §2.1），
//     与 failover 一并计入。
func visibleLanesFor(c *gin.Context) ([]visibleLane, error) {
	key := middleware.PBRClientKeyFrom(c)
	if key == nil {
		return nil, nil
	}
	policy := model.ParseLanePolicy(key.LanePolicy)
	lanes, err := model.ListLanes()
	if err != nil {
		return nil, err
	}
	visible := make([]visibleLane, 0, len(lanes))
	for i := range lanes {
		lane := &lanes[i]
		if !lane.Enabled || len(lane.Members) == 0 {
			continue
		}
		if !policy.AllowsLane(lane.Name) {
			continue
		}
		visible = append(visible, visibleLane{
			name:            lane.Name,
			preferredMember: &lane.Members[0],
		})
	}
	return visible, nil
}

// laneOwnerNames 计算"车道名 → owned_by"：取首选成员的渠道类型对应的适配器渠道名。
// 渠道已删/查询失败时留空，由 buildOpenAIModel 回落静态表或 "custom"。
func laneOwnerNames(lanes []visibleLane) map[string]string {
	ownerByChannelType := make(map[int]string)
	owners := make(map[string]string, len(lanes))
	for _, lane := range lanes {
		if lane.preferredMember == nil {
			continue
		}
		channel, err := model.ChannelOrNil(lane.preferredMember.ChannelId)
		if err != nil || channel == nil {
			continue
		}
		owner, ok := ownerByChannelType[channel.Type]
		if !ok {
			owner = channelOwnerName(channel.Type)
			ownerByChannelType[channel.Type] = owner
		}
		if owner != "" {
			owners[lane.name] = owner
		}
	}
	return owners
}

func ListModels(c *gin.Context, modelType int) {
	lanes, err := visibleLanesFor(c)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "get lanes failed",
		})
		return
	}

	ownerByModel := laneOwnerNames(lanes)
	userOpenAiModels := make([]dto.OpenAIModels, 0, len(lanes))
	for _, lane := range lanes {
		userOpenAiModels = append(userOpenAiModels, buildOpenAIModel(lane.name, ownerByModel))
	}

	switch modelType {
	case constant.ChannelTypeAnthropic:
		useranthropicModels := make([]dto.AnthropicModel, len(userOpenAiModels))
		for i, model := range userOpenAiModels {
			useranthropicModels[i] = dto.AnthropicModel{
				ID:          model.Id,
				CreatedAt:   time.Unix(int64(model.Created), 0).UTC().Format(time.RFC3339),
				DisplayName: model.Id,
				Type:        "model",
			}
		}
		firstID := ""
		lastID := ""
		if len(useranthropicModels) > 0 {
			firstID = useranthropicModels[0].ID
			lastID = useranthropicModels[len(useranthropicModels)-1].ID
		}
		c.JSON(200, gin.H{
			"data":     useranthropicModels,
			"first_id": firstID,
			"has_more": false,
			"last_id":  lastID,
		})
	case constant.ChannelTypeGemini:
		userGeminiModels := make([]dto.GeminiModel, len(userOpenAiModels))
		for i, model := range userOpenAiModels {
			userGeminiModels[i] = dto.GeminiModel{
				Name:        model.Id,
				DisplayName: model.Id,
			}
		}
		c.JSON(200, gin.H{
			"models":        userGeminiModels,
			"nextPageToken": nil,
		})
	default:
		c.JSON(200, gin.H{
			"success": true,
			"data":    userOpenAiModels,
			"object":  "list",
		})
	}
}

func ChannelListModels(c *gin.Context) {
	c.JSON(200, gin.H{
		"success": true,
		"data":    openAIModels,
	})
}

func DashboardListModels(c *gin.Context) {
	modelsByChannel := make(map[int][]string, len(channelId2Models))
	for channelType, models := range channelId2Models {
		modelsByChannel[channelType] = append([]string(nil), models...)
	}
	c.JSON(200, gin.H{
		"success": true,
		"data":    modelsByChannel,
	})
}

func EnabledListModels(c *gin.Context) {
	c.JSON(200, gin.H{
		"success": true,
		"data":    model.GetEnabledModels(),
	})
}

func RetrieveModel(c *gin.Context, modelType int) {
	modelId := c.Param("model")
	if aiModel, ok := openAIModelsMap[modelId]; ok {
		switch modelType {
		case constant.ChannelTypeAnthropic:
			c.JSON(200, dto.AnthropicModel{
				ID:          aiModel.Id,
				CreatedAt:   time.Unix(int64(aiModel.Created), 0).UTC().Format(time.RFC3339),
				DisplayName: aiModel.Id,
				Type:        "model",
			})
		default:
			c.JSON(200, aiModel)
		}
	} else {
		openAIError := types.OpenAIError{
			Message: fmt.Sprintf("The model '%s' does not exist", modelId),
			Type:    "invalid_request_error",
			Param:   "model",
			Code:    "model_not_found",
		}
		c.JSON(200, gin.H{
			"error": openAIError,
		})
	}
}
