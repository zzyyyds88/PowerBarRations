package router

import (
	"net/http"

	"github.com/zzyyyds88/PowerBarRations/controller"
	"github.com/zzyyyds88/PowerBarRations/internal/api"
	"github.com/zzyyyds88/PowerBarRations/internal/apiresp"
	"github.com/zzyyyds88/PowerBarRations/middleware"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// SetApiRouter 注册基座（new-api）遗留的管理面路由。
//
// W7 物理清除（design-v1 §10.2.1）：这里**不再注册**计费/支付与多用户/账号安全两组——
// 用户 CRUD 与登录、2FA/passkey/OAuth/邮箱绑定、签到、排行榜、用户分组、个人令牌、
// 充值/订阅/兑换码/支付回调、定价与倍率同步、用户自助日志与用量统计等一律移除。
//
// 保留的运维面（渠道、模型元数据、厂商、任务插件、系统任务、性能、预填组、
// 管理员日志与审计、选项、静态内容）**全部改挂 PBR 管理密钥**（`PBRAuth`）：
// 基座的 `AdminAuth`/`RootAuth` 依赖用户会话与角色，PBR 没有用户体系，挂它们等于
// 这些路由永远 401。token-spec §2 规定管理密钥即全量权限，因此也不再挂按角色的
// `RequirePermission`。
func SetApiRouter(router *gin.Engine) {
	apiRouter := router.Group("/api")
	apiRouter.Use(middleware.RouteTag("api"))
	// 管理面统一请求体上限（默认 2MB），先于其它中间件读取请求体前生效。
	apiRouter.Use(middleware.AnonymousRequestBodyLimit())
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(middleware.BodyStorageCleanup()) // 清理请求体存储
	apiRouter.Use(middleware.GlobalAPIRateLimit())
	// 全管理面统一响应信封（design-v1 §5.1）：基座遗留的 {success,message,data}
	// 由 apiresp 按逐路由策略机械改写为裸资源 + §3 错误包络 + 真实状态码。
	apiRouter.Use(apiresp.Middleware())
	// dry-run 安全网（api-spec §2.4）：控制台内部面同样适用——写方法 + ?dry_run=true
	// 且未声明 preview ⇒ 400 且不执行。内部面当前一律声明 reject（无预览实现），
	// 调用方要用预览能力请走稳定面（api-spec §5）。
	apiRouter.Use(api.DryRunMiddleware())
	{
		// —— dry-run 声明：控制台内部写路由一律 reject（无预览实现） ——
		// 稳定面已有等价且支持 preview 的端点（api-spec §5.3.1–§5.3.4）。
		for _, def := range consoleInternalWriteRoutes {
			api.RegisterDryRun(def.method, def.path, api.DryRunReject, def.hint)
		}

		apiRouter.GET("/status", controller.GetStatus)
		// 控制台内部接口统一收进 /api/console/*，把 /api/* 让给 PBR 管理面（api-spec §2）。
		apiRouter.GET("/console/models", middleware.PBRAuth(), controller.DashboardListModels)
		apiRouter.GET("/status/test", middleware.PBRAuth(), controller.TestStatus)
		apiRouter.GET("/user-agreement", controller.GetUserAgreement)
		apiRouter.GET("/privacy-policy", controller.GetPrivacyPolicy)
		apiRouter.GET("/about", controller.GetAbout)
		apiRouter.GET("/home_page_content", controller.GetHomePageContent)
		perfMetricsRoute := apiRouter.Group("/perf-metrics")
		perfMetricsRoute.Use(middleware.PBRAuth())
		{
			perfMetricsRoute.GET("/summary", controller.GetPerfMetricsSummary)
			perfMetricsRoute.GET("", controller.GetPerfMetrics)
		}
		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(middleware.PBRAuth())
		{
			optionRoute.GET("/", controller.GetOptions)
			optionRoute.PUT("/", controller.UpdateOption)
		}
		performanceRoute := apiRouter.Group("/performance")
		performanceRoute.Use(middleware.PBRAuth())
		{
			performanceRoute.GET("/stats", controller.GetPerformanceStats)
			performanceRoute.DELETE("/disk_cache", controller.ClearDiskCache)
			performanceRoute.POST("/reset_stats", controller.ResetPerformanceStats)
			performanceRoute.POST("/gc", controller.ForceGC)
			performanceRoute.GET("/logs", controller.GetLogFiles)
			performanceRoute.DELETE("/logs", controller.CleanupLogFiles)
		}
		registerChannelRoutes(apiRouter)
		apiRouter.GET("/console/audit", middleware.PBRAuth(), controller.GetAuditLogs)

		// 基座用量记录（记账视图）：只读，保留给管理员排障。
		// 用户自助视图（/log/self*、/log/token）与额度口径统计（/log/stat）随多用户/计费删除。
		logRoute := apiRouter.Group("/log")
		{
			logRoute.GET("/", middleware.PBRAuth(), controller.GetAllLogs)
			logRoute.GET("/search", middleware.PBRAuth(), controller.SearchAllLogs)
		}

		systemTaskRoute := apiRouter.Group("/system-task")
		systemTaskRoute.Use(middleware.PBRAuth())
		{
			systemTaskRoute.POST("/log-cleanup", controller.CreateLogCleanupSystemTask)
			systemTaskRoute.GET("/list", controller.ListSystemTasks)
			systemTaskRoute.GET("/current", controller.GetCurrentSystemTask)
			systemTaskRoute.GET("/:task_id", controller.GetSystemTask)
		}

		prefillGroupRoute := apiRouter.Group("/prefill_group")
		prefillGroupRoute.Use(middleware.PBRAuth())
		{
			prefillGroupRoute.GET("/", controller.GetPrefillGroups)
			prefillGroupRoute.POST("/", controller.CreatePrefillGroup)
			prefillGroupRoute.PUT("/", controller.UpdatePrefillGroup)
			prefillGroupRoute.DELETE("/:id", controller.DeletePrefillGroup)
		}

		modelsRoute := apiRouter.Group("/console/models")
		modelsRoute.Use(middleware.PBRAuth())
		{
			modelsRoute.GET("/sync_upstream/preview", controller.SyncUpstreamPreview)
			modelsRoute.POST("/sync_upstream", controller.SyncUpstreamModels)
			modelsRoute.POST("/delete", controller.BatchDeleteModelMeta)
			modelsRoute.GET("/missing", controller.GetMissingModels)
			modelsRoute.GET("/", controller.GetAllModelsMeta)
			modelsRoute.GET("/search", controller.SearchModelsMeta)
			modelsRoute.GET("/:id", controller.GetModelMeta)
			modelsRoute.POST("/", controller.CreateModelMeta)
			modelsRoute.PUT("/", controller.UpdateModelMeta)
			modelsRoute.DELETE("/:id", controller.DeleteModelMeta)
		}

	}
}

// consoleInternalWriteRoute 描述一条控制台内部写路由的 dry-run 声明。
type consoleInternalWriteRoute struct {
	method string
	path   string
	hint   string
}

// consoleInternalWriteRoutes 是控制台内部面的**全部**写路由。
//
// 它们一律声明 reject：内部面没有实现预览，而"有副作用却不拒绝 ?dry_run=true"
// 就是静默写入。调用方要预览能力请用稳定面（api-spec §5.3.1–§5.3.4）。
//
// 新增内部写路由时必须在此登记，否则 TestEveryManagementWriteRouteDeclaresDryRun 失败。
var consoleInternalWriteRoutes = []consoleInternalWriteRoute{
	{http.MethodPut, "/api/option/", "改用稳定面 PUT /api/system/options"},
	{http.MethodDelete, "/api/performance/disk_cache", "稳定面无等价预览；直接执行即可"},
	{http.MethodPost, "/api/performance/reset_stats", "运行态统计重置；直接执行即可"},
	{http.MethodPost, "/api/performance/gc", "运行态 GC；直接执行即可"},
	{http.MethodDelete, "/api/performance/logs", "改用稳定面 DELETE /api/system/log-files?dry_run=true"},
	{http.MethodPost, "/api/channel/", "改用稳定面 PUT /api/channels/{name}?dry_run=true"},
	{http.MethodPut, "/api/channel/", "改用稳定面 PUT /api/channels/{name}?dry_run=true"},
	{http.MethodPost, "/api/channel/status/batch", "改用稳定面 POST /api/channels/batch/status?dry_run=true"},
	{http.MethodPost, "/api/channel/:id/status", "改用稳定面 POST /api/channels/batch/status?dry_run=true"},
	{http.MethodDelete, "/api/channel/disabled", "改用稳定面 DELETE /api/channels/disabled?dry_run=true"},
	{http.MethodPost, "/api/channel/tag/disabled", "改用稳定面 POST /api/channels/by-tag/status?dry_run=true"},
	{http.MethodPost, "/api/channel/tag/enabled", "改用稳定面 POST /api/channels/by-tag/status?dry_run=true"},
	{http.MethodPut, "/api/channel/tag", "改用稳定面 PUT /api/channels/by-tag?dry_run=true"},
	{http.MethodDelete, "/api/channel/:id", "改用稳定面 DELETE /api/channels/{name}?dry_run=true"},
	{http.MethodPost, "/api/channel/batch", "改用稳定面 DELETE /api/channels/{name}"},
	{http.MethodPost, "/api/channel/:id/key", "读取密钥明文，无副作用；去掉 ?dry_run=true 即可"},
	{http.MethodPost, "/api/channel/:id/codex/refresh", "上游凭据动作；直接执行即可"},
	{http.MethodPost, "/api/channel/:id/codex/usage/reset", "上游用量动作；直接执行即可"},
	{http.MethodPost, "/api/channel/ollama/pull", "上游动作（会真的拉模型）；直接执行即可"},
	{http.MethodPost, "/api/channel/ollama/pull/stream", "上游动作（SSE）；直接执行即可"},
	{http.MethodDelete, "/api/channel/ollama/delete", "上游动作；直接执行即可"},
	{http.MethodPost, "/api/channel/batch/tag", "改用稳定面 POST /api/channels/batch/tag?dry_run=true"},
	{http.MethodPost, "/api/channel/copy/:id", "改用稳定面 POST /api/channels/batch/copy?dry_run=true"},
	{http.MethodPost, "/api/channel/multi_key/manage", "改用稳定面 POST /api/channels/{name}/multi-keys?dry_run=true"},
	{http.MethodPost, "/api/channel/upstream_updates/apply", "改用稳定面 POST /api/channels/{name}/upstream-updates/apply?dry_run=true"},
	{http.MethodPost, "/api/channel/upstream_updates/apply_all", "改用稳定面 POST /api/channels/upstream-updates/apply-all?dry_run=true"},
	{http.MethodPost, "/api/channel/upstream_updates/detect", "探测任务；直接执行即可"},
	{http.MethodPost, "/api/channel/upstream_updates/detect_all", "探测任务；直接执行即可"},
	{http.MethodPost, "/api/system-task/log-cleanup", "改用稳定面 POST /api/system-tasks/log-cleanup?dry_run=true"},
	{http.MethodPost, "/api/prefill_group/", "改用稳定面 POST /api/prefill-groups?dry_run=true"},
	{http.MethodPut, "/api/prefill_group/", "改用稳定面 PUT /api/prefill-groups/{id}?dry_run=true"},
	{http.MethodDelete, "/api/prefill_group/:id", "改用稳定面 DELETE /api/prefill-groups/{id}?dry_run=true"},
	{http.MethodPost, "/api/console/models/sync_upstream", "改用稳定面 POST /api/model-catalog/sync-upstream?dry_run=true"},
	{http.MethodPost, "/api/console/models/delete", "改用稳定面 POST /api/model-catalog/batch-delete?dry_run=true"},
	{http.MethodPost, "/api/console/models/", "模型目录写入；直接执行即可"},
	{http.MethodPut, "/api/console/models/", "模型目录写入；直接执行即可"},
	{http.MethodDelete, "/api/console/models/:id", "改用稳定面 DELETE /api/model-metadata/{model}?dry_run=true"},
}
