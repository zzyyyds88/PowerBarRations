package router

import (
	"net/http"

	"pbr/controller"
	"pbr/middleware"

	"github.com/gin-gonic/gin"
)

// 基座渠道运维面（§1.4 保留渠道功能）。
//
// W7 改造（design-v1 §10.2.1）：整组鉴权从基座的用户会话/角色体系改挂 PBR 管理密钥
// （`PBRAuth`，token-spec §2 规定管理密钥即全量权限，因此不再有按角色的
// `RequirePermission` 门）；`POST /channel/:id/key` 上的 2FA/passkey 安全证明被移除——
// PBR 没有用户体系，该证明永远无法满足，只会让这条路由不可用。
type channelRouteDef struct {
	method  string
	path    string
	handler gin.HandlerFunc
}

func registerChannelRoutes(apiRouter *gin.RouterGroup) {
	channelRoute := apiRouter.Group("/channel")
	channelRoute.Use(middleware.PBRAuth())

	channelRoute.POST("/:id/key",
		middleware.CriticalRateLimit(),
		middleware.DisableCache(),
		controller.GetChannelKey,
	)

	for _, route := range channelRoutes {
		channelRoute.Handle(route.method, route.path, route.handler)
	}
}

var channelRoutes = []channelRouteDef{
	{method: http.MethodGet, path: "/", handler: controller.GetAllChannels},
	{method: http.MethodGet, path: "/search", handler: controller.SearchChannels},
	{method: http.MethodGet, path: "/models", handler: controller.ChannelListModels},
	{method: http.MethodGet, path: "/default_base_urls", handler: controller.GetChannelDefaultBaseURLs},
	{method: http.MethodGet, path: "/models_enabled", handler: controller.EnabledListModels},
	{method: http.MethodGet, path: "/ops", handler: controller.GetChannelOps},
	{method: http.MethodGet, path: "/:id", handler: controller.GetChannel},
	{method: http.MethodGet, path: "/test", handler: controller.TestAllChannels},
	{method: http.MethodGet, path: "/test/:id", handler: controller.TestChannel},
	{method: http.MethodGet, path: "/update_balance", handler: controller.UpdateAllChannelsBalance},
	{method: http.MethodGet, path: "/update_balance/:id", handler: controller.UpdateChannelBalance},
	{method: http.MethodPost, path: "/", handler: controller.AddChannel},
	{method: http.MethodPut, path: "/", handler: controller.UpdateChannel},
	{method: http.MethodPost, path: "/status/batch", handler: controller.BatchUpdateChannelStatus},
	{method: http.MethodPost, path: "/:id/status", handler: controller.UpdateChannelStatus},
	{method: http.MethodDelete, path: "/disabled", handler: controller.DeleteDisabledChannel},
	{method: http.MethodPost, path: "/tag/disabled", handler: controller.DisableTagChannels},
	{method: http.MethodPost, path: "/tag/enabled", handler: controller.EnableTagChannels},
	{method: http.MethodPut, path: "/tag", handler: controller.EditTagChannels},
	{method: http.MethodDelete, path: "/:id", handler: controller.DeleteChannel},
	{method: http.MethodPost, path: "/batch", handler: controller.DeleteChannelBatch},
	{method: http.MethodPost, path: "/fix", handler: controller.FixChannelsAbilities},
	{method: http.MethodGet, path: "/fetch_models/:id", handler: controller.FetchUpstreamModels},
	{method: http.MethodPost, path: "/fetch_models", handler: controller.FetchModels},
	{method: http.MethodPost, path: "/:id/codex/refresh", handler: controller.RefreshCodexChannelCredential},
	{method: http.MethodGet, path: "/:id/codex/usage", handler: controller.GetCodexChannelUsage},
	{method: http.MethodGet, path: "/:id/codex/usage/reset-credits", handler: controller.GetCodexChannelRateLimitResetCredits},
	{method: http.MethodPost, path: "/:id/codex/usage/reset", handler: controller.ResetCodexChannelUsage},
	{method: http.MethodPost, path: "/ollama/pull", handler: controller.OllamaPullModel},
	{method: http.MethodPost, path: "/ollama/pull/stream", handler: controller.OllamaPullModelStream},
	{method: http.MethodDelete, path: "/ollama/delete", handler: controller.OllamaDeleteModel},
	{method: http.MethodGet, path: "/ollama/version/:id", handler: controller.OllamaVersion},
	{method: http.MethodPost, path: "/batch/tag", handler: controller.BatchSetChannelTag},
	{method: http.MethodGet, path: "/tag/models", handler: controller.GetTagModels},
	{method: http.MethodPost, path: "/copy/:id", handler: controller.CopyChannel},
	{method: http.MethodPost, path: "/multi_key/manage", handler: controller.ManageMultiKeys},
	{method: http.MethodPost, path: "/upstream_updates/apply", handler: controller.ApplyChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/apply_all", handler: controller.ApplyAllChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/detect", handler: controller.DetectChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/detect_all", handler: controller.DetectAllChannelUpstreamModelUpdates},
}
