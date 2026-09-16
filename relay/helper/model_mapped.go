package helper

import (
	"errors"
	"fmt"

	rootcommon "pbr/common"
	"pbr/constant"
	relaycommon "pbr/relay/common"
	"pbr/relaykit/dto"
	hostreasoning "pbr/setting/reasoning"
	"github.com/gin-gonic/gin"
)

func ModelMappedHelper(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}

	// PBR 路由已经在选路阶段按"成员级 upstream_model（非空且 ≠ 路由键）> 渠道
	// model_mapping > 路由键"解析出上游真名并注入上下文（routing-spec §1.2）。
	// 此时**不得**再用渠道 model_mapping 二次改写：那是以 OriginModelName 为起点
	// 重新查一遍映射，会把成员级显式改名反向覆盖成渠道映射值。
	// 非 PBR 链路（任务插件、显式渠道 pin、未走 PBR 的迁移期请求）没有这个上下文键，行为不变。
	if pbrUpstream := rootcommon.GetContextKeyString(c, constant.ContextKeyPBRUpstreamModel); pbrUpstream != "" {
		if info.UpstreamModelName == "" {
			info.UpstreamModelName = pbrUpstream
		}
		info.IsModelMapped = info.UpstreamModelName != info.OriginModelName
		if request != nil {
			request.SetModelName(info.UpstreamModelName)
		}
		return nil
	}

	// map model name
	modelMapping := c.GetString("model_mapping")
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := rootcommon.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return fmt.Errorf("unmarshal_model_mapping_failed")
		}

		// 支持链式模型重定向，最终使用链尾的模型
		currentModel := info.OriginModelName
		visitedModels := map[string]bool{
			currentModel: true,
		}
		for {
			mappedModel, exists := modelMap[currentModel]
			baseModel := hostreasoning.BaseModelName(currentModel)
			if (!exists || mappedModel == "") && baseModel != currentModel {
				mappedModel, exists = modelMap[baseModel]
			}
			if exists && mappedModel != "" {
				// 模型重定向循环检测，避免无限循环
				if visitedModels[mappedModel] {
					if mappedModel == currentModel {
						if currentModel == info.OriginModelName {
							info.IsModelMapped = false
							return nil
						}

						info.IsModelMapped = true
						break
					}
					return errors.New("model_mapping_contains_cycle")
				}
				visitedModels[mappedModel] = true
				currentModel = mappedModel
				info.IsModelMapped = true
			} else {
				break
			}
		}
		if info.IsModelMapped {
			info.UpstreamModelName = currentModel
		}
	}

	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}
