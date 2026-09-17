package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PBR 已解析出的上游真名是权威值：成员级 upstream_model 不得被渠道 model_mapping
// 二次改写（routing-spec §1.2 / ADR 0005，审查 F2）。
func TestModelMappedHelperKeepsPBRResolvedUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("model_mapping", `{"model-1":"mapped-a"}`)
	common.SetContextKey(c, constant.ContextKeyPBRUpstreamModel, "real-a")

	request := &dto.GeneralOpenAIRequest{Model: "model-1"}
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-1",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "real-a"},
	}

	require.NoError(t, ModelMappedHelper(c, info, request))
	assert.Equal(t, "real-a", info.UpstreamModelName, "成员级改名必须胜出，不能被渠道映射覆盖")
	assert.Equal(t, "real-a", request.Model)
	assert.True(t, info.IsModelMapped, "上游名与请求名不同，应标记为已改名")
}

// 没有成员级改名时，PBR 解析结果就是渠道映射值；这里只验证"不会重复改写/不回退"。
func TestModelMappedHelperKeepsPBRMappingResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("model_mapping", `{"model-1":"mapped-a"}`)
	common.SetContextKey(c, constant.ContextKeyPBRUpstreamModel, "mapped-a")

	request := &dto.GeneralOpenAIRequest{Model: "model-1"}
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-1",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "mapped-a"},
	}

	require.NoError(t, ModelMappedHelper(c, info, request))
	assert.Equal(t, "mapped-a", info.UpstreamModelName)
	assert.Equal(t, "mapped-a", request.Model)
}

// 非 PBR 链路（任务插件 / 显式 pin / 迁移期）没有 PBR 上下文键，渠道映射行为必须不变。
func TestModelMappedHelperStillAppliesMappingWithoutPBRContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("model_mapping", `{"model-1":"mapped-a"}`)

	request := &dto.GeneralOpenAIRequest{Model: "model-1"}
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-1",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "model-1"},
	}

	require.NoError(t, ModelMappedHelper(c, info, request))
	assert.Equal(t, "mapped-a", info.UpstreamModelName, "非 PBR 链路仍按渠道映射重写")
	assert.Equal(t, "mapped-a", request.Model)
	assert.True(t, info.IsModelMapped)
}
