package relay

import (
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/relay/channel"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/advancedcustom"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/ali"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/aws"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/baidu"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/baidu_v2"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/claude"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/cloudflare"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/codex"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/cohere"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/coze"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/deepseek"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/dify"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/gemini"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/jimeng"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/jina"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/minimax"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/mistral"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/mokaai"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/moonshot"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/newapi"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/ollama"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/openai"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/palm"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/perplexity"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/replicate"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/siliconflow"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/sub2api"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/submodel"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/tencent"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/vertex"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/volcengine"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/xai"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/xunfei"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/zhipu"
	"github.com/zzyyyds88/PowerBarRations/relay/channel/zhipu_4v"
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
