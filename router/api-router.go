package router

import (
	"pbr/controller"
	"pbr/middleware"

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
	{
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
			optionRoute.GET("/channel_affinity_cache", controller.GetChannelAffinityCacheStats)
			optionRoute.DELETE("/channel_affinity_cache", controller.ClearChannelAffinityCache)
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
			logRoute.GET("/channel_affinity_usage_cache", middleware.PBRAuth(), controller.GetChannelAffinityUsageCacheStats)
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
