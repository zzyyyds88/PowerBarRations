package relay

import (
	"pbr/constant"
	"pbr/relay/channel"
	"pbr/relay/channel/advancedcustom"
	"pbr/relay/channel/ali"
	"pbr/relay/channel/aws"
	"pbr/relay/channel/baidu"
	"pbr/relay/channel/baidu_v2"
	"pbr/relay/channel/claude"
	"pbr/relay/channel/cloudflare"
	"pbr/relay/channel/codex"
	"pbr/relay/channel/cohere"
	"pbr/relay/channel/coze"
	"pbr/relay/channel/deepseek"
	"pbr/relay/channel/dify"
	"pbr/relay/channel/gemini"
	"pbr/relay/channel/jimeng"
	"pbr/relay/channel/jina"
	"pbr/relay/channel/minimax"
	"pbr/relay/channel/mistral"
	"pbr/relay/channel/mokaai"
	"pbr/relay/channel/moonshot"
	"pbr/relay/channel/newapi"
	"pbr/relay/channel/ollama"
	"pbr/relay/channel/openai"
	"pbr/relay/channel/palm"
	"pbr/relay/channel/perplexity"
	"pbr/relay/channel/replicate"
	"pbr/relay/channel/siliconflow"
	"pbr/relay/channel/sub2api"
	"pbr/relay/channel/submodel"
	"pbr/relay/channel/tencent"
	"pbr/relay/channel/vertex"
	"pbr/relay/channel/volcengine"
	"pbr/relay/channel/xai"
	"pbr/relay/channel/xunfei"
	"pbr/relay/channel/zhipu"
	"pbr/relay/channel/zhipu_4v"
)

func GetAdaptor(apiType int) channel.Adaptor {
	switch apiType {
	case constant.APITypeAli:
		return &ali.Adaptor{}
	case constant.APITypeAnthropic:
		return &claude.Adaptor{}
	case constant.APITypeBaidu:
		return &baidu.Adaptor{}
	case constant.APITypeGemini:
		return &gemini.Adaptor{}
	case constant.APITypeOpenAI:
		return &openai.Adaptor{}
	case constant.APITypePaLM:
		return &palm.Adaptor{}
	case constant.APITypeTencent:
		return &tencent.DispatchAdaptor{}
	case constant.APITypeXunfei:
		return &xunfei.Adaptor{}
	case constant.APITypeZhipu:
		return &zhipu.Adaptor{}
	case constant.APITypeZhipuV4:
		return &zhipu_4v.Adaptor{}
	case constant.APITypeOllama:
		return &ollama.Adaptor{}
	case constant.APITypePerplexity:
		return &perplexity.Adaptor{}
	case constant.APITypeAws:
		return &aws.Adaptor{}
	case constant.APITypeCohere:
		return &cohere.Adaptor{}
	case constant.APITypeDify:
		return &dify.Adaptor{}
	case constant.APITypeJina:
		return &jina.Adaptor{}
	case constant.APITypeCloudflare:
		return &cloudflare.Adaptor{}
	case constant.APITypeSiliconFlow:
		return &siliconflow.Adaptor{}
	case constant.APITypeVertexAi:
		return &vertex.Adaptor{}
	case constant.APITypeMistral:
		return &mistral.Adaptor{}
	case constant.APITypeDeepSeek:
		return &deepseek.Adaptor{}
	case constant.APITypeMokaAI:
		return &mokaai.Adaptor{}
	case constant.APITypeVolcEngine:
		return &volcengine.Adaptor{}
	case constant.APITypeBaiduV2:
		return &baidu_v2.Adaptor{}
	case constant.APITypeOpenRouter:
		return &openai.Adaptor{}
	case constant.APITypeXinference:
		return &openai.Adaptor{}
	case constant.APITypeXai:
		return &xai.Adaptor{}
	case constant.APITypeCoze:
		return &coze.Adaptor{}
	case constant.APITypeJimeng:
		return &jimeng.Adaptor{}
	case constant.APITypeMoonshot:
		return &moonshot.Adaptor{} // Moonshot uses Claude API
	case constant.APITypeSubmodel:
		return &submodel.Adaptor{}
	case constant.APITypeMiniMax:
		return &minimax.Adaptor{}
	case constant.APITypeReplicate:
		return &replicate.Adaptor{}
	case constant.APITypeCodex:
		return &codex.Adaptor{}
	case constant.APITypeAdvancedCustom:
		return &advancedcustom.Adaptor{}
	case constant.APITypeSub2API:
		return &sub2api.Adaptor{}
	case constant.APITypeNewAPI:
		return &newapi.Adaptor{}
	}
	return nil
}
