package relay

import (
	"fmt"
	"io"
	"net/http"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/logger"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relay/helper"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
	"github.com/zzyyyds88/PowerBarRations/service"
	"github.com/zzyyyds88/PowerBarRations/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ModerationsHelper 处理 `/v1/moderations`（design-v1 §4：透传；独立 relay mode，
// 不混入 chat 的 usage 与流式语义）。
//
// 为什么不能复用 TextHelper：TextHelper 按 chat completion 解析并**重写**响应体
// （choices / usage），moderation 对象里的 flagged / categories / category_scores
// 会被静默丢掉，客户端只能拿到一个空壳。这里只做 URL、鉴权与模型映射，
// 响应体原样回传；仅在存在 model 字段时按 design §4.1 回填请求名（车道名）。
//
// 该端点不产生 token 用量：日志由 controller.Relay 的统一收尾写入（tokens=0、
// cost=0），因此这里既不调用 PostTextUsageLog，也不设置 ShouldIncludeUsage。
func ModerationsHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	moderationReq, ok := info.Request.(*dto.GeneralOpenAIRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected *dto.GeneralOpenAIRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(moderationReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	var requestBody io.Reader
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.NewReplayableBodyReader(storage)
	} else {
		jsonData, err := common.Marshal(request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}
		logger.LogDebug(c, "moderations request body: %s", jsonData)
		body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		requestBody = body
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResp, ok := resp.(*http.Response)
	if !ok || httpResp == nil {
		return types.NewError(fmt.Errorf("invalid response type"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
	}
	if httpResp.StatusCode != http.StatusOK {
		newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	defer service.CloseResponseBodyGracefully(httpResp)

	responseBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	// design-v1 §4.1：成功响应里的 model 回填请求名（车道名），不暴露上游真名。
	// moderation 对象带该字段时改写；没有该字段或不是合法 JSON 就原样回传，
	// 不伪造结构（该端点按契约是透传）。
	if gjson.ValidBytes(responseBody) && gjson.GetBytes(responseBody, "model").Exists() {
		if rewritten, setErr := sjson.SetBytes(responseBody, "model", info.OriginModelName); setErr == nil {
			responseBody = rewritten
		}
	}

	contentType := httpResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(http.StatusOK, contentType, responseBody)
	return nil
}
