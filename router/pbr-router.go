package router

import (
	"github.com/zzyyyds88/PowerBarRations/internal/api"
	"github.com/zzyyyds88/PowerBarRations/middleware"

	"github.com/gin-gonic/gin"
)

// pbrAPIPrefixes 是管理面的前缀：
//
//   - `/api`   —— 面向 AI / 脚本的规范前缀（design-v1 §7.7、api-spec §2）。
//   - `/api/v1` —— 兼容别名，控制台前端与既有验收脚本仍在使用。
//
// 两个前缀注册完全相同的处理器，因此 `/api/health` 与 `/api/v1/health` 等价。
var pbrAPIPrefixes = []string{"/api", "/api/v1"}

// SetPBRRouter 注册 PowerBarRations 管理 API（`/api/*`，兼容 `/api/v1/*`）。
//
// 认证模型见 docs/token-spec-v1.md §2：无账号，一个登录口令；
// 管理密钥 = Base64(SHA256(口令))，只存其 sha256。
// 免鉴权端点只有健康/版本、初始化相关三个与文档页（api-spec §5.1）。
// 面向 AI 的纯文本手册挂在根路径 `/doc` 与 `/llms.txt`。
func SetPBRRouter(router *gin.Engine) {
	router.GET("/doc", api.Doc)
	router.GET("/doc/ui", api.DocUI)
	router.GET("/llms.txt", api.LLMs)

	for _, prefix := range pbrAPIPrefixes {
		registerPBRAPIRoutes(router.Group(prefix))
	}
}

func registerPBRAPIRoutes(group *gin.RouterGroup) {
	// 管理面统一请求体上限（默认 2MB）：免鉴权的 setup/login 也在其中，
	// 必须在读取请求体之前挂上。模型面 /v1/** 不经过这里，不受影响。
	group.Use(middleware.AnonymousRequestBodyLimit())
	// 免鉴权：健康检查、版本、初始化状态与首启设口令。
	group.GET("/health", api.Health)
	group.GET("/version", api.Version)
	group.GET("/setup/status", api.SetupStatus)
	group.POST("/setup", api.Setup)
	group.POST("/auth/login", api.Login)
	// 登出只清 Cookie，幂等且无需先鉴权（未带 Cookie 也返回成功）。
	group.POST("/auth/logout", api.Logout)
	// 会话状态查询：免鉴权，200 承载布尔值（控制台启动判定）。
	group.GET("/auth/session", api.SessionStatus)
	// 机器可读契约免鉴权：/doc 与 /doc/ui 需要它，且只暴露端点形状。
	group.GET("/openapi.json", api.GetOpenAPI)

	authed := group.Group("")
	authed.Use(middleware.PBRAuth())
	{
		authed.POST("/auth/password", api.ChangePassword)
		authed.GET("/capabilities", api.GetCapabilities)
		authed.GET("/export", api.GetExport)
		authed.POST("/import", api.PostImport)
		authed.GET("/audit", api.ListAudit)

		// 请求日志与统计（只读元数据）
		authed.GET("/logs", api.ListLogs)
		authed.GET("/logs/:id", api.GetLog)
		// 明细日志按需清理（design-v1 §16.5：不引入 cron，由外部触发）
		authed.POST("/logs/prune", api.PruneLogs)
		authed.GET("/stats", api.GetStats)
		// SSE：车道运行态增量（控制台实时显示；另有 30s 轮询兜底）
		authed.GET("/route-events", api.RouteEvents)

		// 渠道
		authed.GET("/channels", api.ListChannels)
		authed.GET("/channels/:name", api.GetChannel)
		authed.PUT("/channels/:name", api.PutChannel)
		authed.DELETE("/channels/:name", api.DeleteChannel)
		authed.POST("/channels/:name/test", api.TestChannel)
		authed.POST("/channels/:name/sync-models", api.SyncChannelModels)

		// 车道：唯一路由入口（ADR 0005）
		authed.GET("/lanes", api.ListLanes)
		authed.GET("/lanes/:name", api.GetLane)
		authed.PUT("/lanes/:name", api.PutLane)
		authed.POST("/lanes/cleanup-members", api.CleanupLaneMembers)
		authed.DELETE("/lanes/:name", api.DeleteLane)
		authed.GET("/lanes/:name/health", api.GetLaneHealth)
		authed.POST("/lanes/:name/probe", api.ProbeLane)
		authed.PUT("/lanes/:name/members", api.PutLaneMembers)
		authed.POST("/lanes/:name/circuits/reset", api.ResetLaneCircuits)

		// 全局选项（W2 只暴露容错/熔断相关键）
		authed.GET("/system/options", api.GetSystemOptions)
		authed.PUT("/system/options", api.PutSystemOptions)

		// Webhook 事件通知（design-v1 §16.10：配置 / 测试投递 / 投递日志）
		authed.GET("/webhooks", api.ListWebhooks)
		authed.PUT("/webhooks", api.PutWebhooks)
		authed.POST("/webhooks/test", api.TestWebhook)
		authed.GET("/webhooks/deliveries", api.ListWebhookDeliveries)

		// 客户端密钥
		authed.GET("/keys", api.ListKeys)
		authed.POST("/keys", api.CreateKey)
		authed.GET("/keys/:name", api.GetKey)
		authed.PUT("/keys/:name", api.PutKey)
		authed.DELETE("/keys/:name", api.DeleteKey)
		authed.POST("/keys/:name/rotate", api.RotateKey)

		// 模型路由（隐式车道）。用 catch-all 以支持带路径分隔符的模型名（如 vendor/model）。
		authed.GET("/models", api.ListModels)
		authed.GET("/routes/*model", api.GetRoute)

		// 车道顺序摘要（不分页）与模型目录元数据（api-spec §5.7）。
		authed.GET("/lane-summaries", api.ListLaneSummaries)
		authed.GET("/model-metadata", api.ListModelMetadata)
		authed.PUT("/model-metadata/*model", api.PutModelMetadata)
		authed.DELETE("/model-metadata/*model", api.DeleteModelMetadataByModel)
	}
}
