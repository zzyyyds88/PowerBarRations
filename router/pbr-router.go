package router

import (
	"pbr/internal/api"
	"pbr/middleware"

	"github.com/gin-gonic/gin"
)

// SetPBRRouter 注册 PowerBarRations 管理 API（`/api/v1/*`）。
//
// 认证模型见 docs/token-spec-v1.md §2：无账号，一个登录口令；
// 管理密钥 = Base64(SHA256(口令))，只存其 sha256。
// 免鉴权端点只有健康/版本与初始化相关三个（api-spec §5.1）。
func SetPBRRouter(router *gin.Engine) {
	group := router.Group("/api/v1")

	// 免鉴权：健康检查、版本、初始化状态与首启设口令。
	group.GET("/health", api.Health)
	group.GET("/version", api.Version)
	group.GET("/setup/status", api.SetupStatus)
	group.POST("/setup", api.Setup)
	group.POST("/auth/login", api.Login)

	authed := group.Group("")
	authed.Use(middleware.PBRAuth())
	{
		authed.POST("/auth/password", api.ChangePassword)
		authed.GET("/capabilities", api.GetCapabilities)
		authed.GET("/openapi.json", api.GetOpenAPI)
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

		// 显式车道（可选覆盖层）
		authed.GET("/lanes", api.ListLanes)
		authed.GET("/lanes/:name", api.GetLane)
		authed.PUT("/lanes/:name", api.PutLane)
		authed.DELETE("/lanes/:name", api.DeleteLane)
		authed.GET("/lanes/:name/health", api.GetLaneHealth)
		authed.POST("/lanes/:name/probe", api.ProbeLane)
		authed.PUT("/lanes/:name/members", api.PutLaneMembers)
		authed.POST("/lanes/:name/circuits/reset", api.ResetLaneCircuits)

		// 全局选项（W2 只暴露容错/熔断相关键）
		authed.GET("/system/options", api.GetSystemOptions)
		authed.PUT("/system/options", api.PutSystemOptions)

		// HTTPS 证书：状态 / 导入 / 自签
		authed.GET("/tls", api.GetTLSStatus)
		authed.PUT("/tls/certificate", api.PutTLSCertificate)
		authed.POST("/tls/self-signed", api.PostTLSSelfSigned)

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
	}
}
