package router

import (
	"net/http"
	"strconv"

	"github.com/zzyyyds88/PowerBarRations/internal/api"
	"github.com/zzyyyds88/PowerBarRations/internal/apiresp"
	"github.com/zzyyyds88/PowerBarRations/internal/metrics"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
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
	// 指标导出（design-v1 §8、api-spec §5.1）：根路径 /metrics，复用管理密钥
	// Bearer（不新增密钥类型），刻意不在 /api 前缀下——Prometheus 抓取约定。
	router.GET("/metrics", middleware.PBRAuth(), metricsHandler())

	for _, prefix := range pbrAPIPrefixes {
		registerPBRAPIRoutes(router.Group(prefix))
	}
}

// metricsHandler 返回 Prometheus 文本格式处理器。
//
// 抓取前先把路由运行态的只读快照推给 gauge（pbr_circuit_state / pbr_channel_up）：
// Registry.SnapshotMemberStates 只读进程内状态、不查库（api-spec §5.1 的抓取约束）。
// 同一渠道有多个成员时按"任一 open → open，否则任一 half_open → half_open，
// 否则 closed"聚合；channel 标签取成员键里的渠道 ID（与日志 member_channel_id 对齐）。
func metricsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		syncChannelGauges()
		metrics.Handler().ServeHTTP(c.Writer, c.Request)
	}
}

// syncChannelGauges 把进程内路由运行态推给指标 gauge。
func syncChannelGauges() {
	type channelState struct {
		state string
		up    bool
	}
	byChannel := map[string]*channelState{}
	for _, member := range route.Default.SnapshotMemberStates() {
		channelID := strconv.Itoa(member.MemberKey.ChannelID)
		if channelID == "" {
			continue
		}
		entry := byChannel[channelID]
		if entry == nil {
			entry = &channelState{state: route.CircuitClosed, up: true}
			byChannel[channelID] = entry
		}
		switch member.Circuit {
		case route.CircuitOpen:
			entry.state = route.CircuitOpen
			entry.up = false
		case route.CircuitHalfOpen:
			if entry.state != route.CircuitOpen {
				entry.state = route.CircuitHalfOpen
			}
		}
	}
	for channelID, entry := range byChannel {
		metrics.SetChannelState(channelID, entry.state, entry.up)
	}
	keep := make(map[string]struct{}, len(byChannel))
	for channelID := range byChannel {
		keep[channelID] = struct{}{}
	}
	metrics.PruneChannelStates(keep)
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
	// 稳定面统一响应信封（design-v1 §5.1）：成功裸资源、失败错误包络 + 真实状态码。
	authed.Use(apiresp.Middleware())
	// dry-run 安全网（api-spec §2.4）：写方法 + ?dry_run=true + 未声明 preview
	// ⇒ 400 且不执行。它覆盖**整个管理面**，因此漏声明/漏实现只会变成"明确拒绝"。
	authed.Use(api.DryRunMiddleware())
	{
		// —— dry-run 声明：非 opsRoutes 表的稳定面写路由 ——
		// preview：已实现预览、绝不落库。
		api.RegisterDryRun(http.MethodPost, "/api/v1/import", api.DryRunPreview,
			"完整 diff 见响应 diff 字段")
		api.RegisterDryRun(http.MethodPost, "/api/v1/logs/prune", api.DryRunPreview,
			"预览清理前后计数")
		api.RegisterDryRun(http.MethodPut, "/api/v1/channels/:name", api.DryRunPreview, "diff.channels")
		api.RegisterDryRun(http.MethodDelete, "/api/v1/channels/:name", api.DryRunPreview, "diff.channels")
		api.RegisterDryRun(http.MethodPost, "/api/v1/channels/:name/sync-models", api.DryRunPreview,
			"diff.models + referenced_by")
		api.RegisterDryRun(http.MethodPut, "/api/v1/lanes/:name", api.DryRunPreview, "diff.lanes")
		api.RegisterDryRun(http.MethodPut, "/api/v1/lanes/:name/members", api.DryRunPreview, "diff.lanes")
		api.RegisterDryRun(http.MethodDelete, "/api/v1/lanes/:name", api.DryRunPreview, "diff.lanes")
		api.RegisterDryRun(http.MethodPost, "/api/v1/stats/rebuild", api.DryRunPreview,
			"将重建的聚合行数与成本合计差异（before/after）")
		api.RegisterDryRun(http.MethodPost, "/api/v1/member-groups", api.DryRunPreview, "diff.member_groups")
		api.RegisterDryRun(http.MethodPut, "/api/v1/member-groups/:name", api.DryRunPreview, "diff.member_groups")
		api.RegisterDryRun(http.MethodDelete, "/api/v1/member-groups/:name", api.DryRunPreview,
			"diff.member_groups + diff.lanes（级联清理组引用）")
		api.RegisterDryRun(http.MethodPost, "/api/v1/lanes/cleanup-members", api.DryRunPreview,
			"{dry_run, lanes:[受影响车道]} ")
		api.RegisterDryRun(http.MethodPost, "/api/v1/lanes/:name/circuits/reset", api.DryRunPreview,
			"运行态重置的预览计数")
		api.RegisterDryRun(http.MethodPut, "/api/v1/system/options", api.DryRunPreview, "diff.system_options")
		api.RegisterDryRun(http.MethodPut, "/api/v1/webhooks", api.DryRunPreview, "diff.webhooks")
		api.RegisterDryRun(http.MethodPost, "/api/v1/keys", api.DryRunPreview, "diff.keys")
		api.RegisterDryRun(http.MethodPut, "/api/v1/keys/:name", api.DryRunPreview, "diff.keys")
		api.RegisterDryRun(http.MethodDelete, "/api/v1/keys/:name", api.DryRunPreview, "diff.keys")
		api.RegisterDryRun(http.MethodPost, "/api/v1/keys/:name/rotate", api.DryRunPreview, "diff.keys")
		api.RegisterDryRun(http.MethodPut, "/api/v1/model-metadata/*model", api.DryRunPreview,
			"diff.model_metadata")
		api.RegisterDryRun(http.MethodDelete, "/api/v1/model-metadata/*model", api.DryRunPreview,
			"diff.model_metadata")
		// reject：有副作用但不支持预览——带 ?dry_run=true 必须明确拒绝，绝不执行。
		api.RegisterDryRun(http.MethodPost, "/api/v1/auth/password", api.DryRunReject,
			"改口令会同时变更管理密钥与会话，没有预览语义")
		api.RegisterDryRun(http.MethodPost, "/api/v1/channels/:name/test", api.DryRunReject,
			"探活本身就是只读的真实上游请求；直接调用即可")
		api.RegisterDryRun(http.MethodPost, "/api/v1/lanes/:name/probe", api.DryRunReject,
			"探活本身就是只读的真实上游请求；直接调用即可")
		api.RegisterDryRun(http.MethodPost, "/api/v1/webhooks/test", api.DryRunReject,
			"测试投递会真的向目标发一条事件；要预检请用 GET /api/webhooks 检查配置")

		authed.POST("/auth/password", api.ChangePassword)
		authed.GET("/capabilities", api.GetCapabilities)
		authed.GET("/export", api.GetExport)
		authed.POST("/import", api.PostImport)
		authed.GET("/audit", api.ListAudit)

		// 模型状态（只读；跨渠道按上游模型聚合，api-spec §6.13）
		authed.GET("/model-status", api.GetModelStatus)
		// 渠道实时速度（只读；短窗按渠道聚合，api-spec §6.14）
		authed.GET("/channel-speed", api.GetChannelSpeed)
		// 请求日志与统计（只读元数据）
		authed.GET("/logs", api.ListLogs)
		// 日志导出（G2，api-spec §5.5.1）：流式 CSV/NDJSON，只读并写一条审计。
		// 静态段 /logs/export 与 /logs/:id 同级共存，由 gin 的静态优先规则命中本行。
		authed.GET("/logs/export", api.ExportLogs)
		// 会话聚合（usage-spec §7；只读，数据源是明细日志）
		authed.GET("/logs/sessions", api.ListLogSessions)
		authed.GET("/logs/sessions/:key", api.GetLogSession)
		authed.GET("/logs/:id", api.GetLog)
		// 明细日志按需清理（design-v1 §16.5：不引入 cron，由外部触发）
		authed.POST("/logs/prune", api.PruneLogs)
		// 管理面 CSV 导出（api-spec §5.5.2，G7）：只读 + 写一条 action=export 审计。
		authed.GET("/audit/export", api.ExportAudit)
		authed.GET("/stats", api.GetStats)
		// 统计口径变更后的历史回填（api-spec §5.5；dry-run 先行）
		authed.POST("/stats/rebuild", api.RebuildStats)
		// SSE：车道运行态增量（控制台实时显示；另有 30s 轮询兜底）
		authed.GET("/route-events", api.RouteEvents)

		// 渠道
		authed.GET("/channels", api.ListChannels)
		// 渠道全量 CSV 导出（不含密钥明文；api-spec §5.5.2）。
		authed.GET("/channels/export", api.ExportChannels)
		// 渠道状态（只读；api-spec §6.12）。静态段与 /channels/:name 同级共存，
		// 由 gin 的静态优先规则命中本行；不做"渠道名恰为 status"的兼容（该名字保留给本端点）。
		authed.GET("/channels/status", api.ListChannelStatus)
		authed.GET("/channels/:name", api.GetChannel)
		authed.GET("/channels/:name/status", api.GetChannelStatusByName)
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

		// 成员组（ADR 0009）：可复用有序成员子链，车道链元素两态之一。
		authed.GET("/member-groups", api.ListMemberGroups)
		authed.GET("/member-groups/:name", api.GetMemberGroup)
		authed.POST("/member-groups", api.CreateMemberGroup)
		authed.PUT("/member-groups/:name", api.UpdateMemberGroup)
		authed.DELETE("/member-groups/:name", api.DeleteMemberGroup)

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
		// 密钥全量 CSV 导出（只含 key_prefix 与策略，不含明文；api-spec §5.5.2）。
		authed.GET("/keys/export", api.ExportKeys)
		authed.POST("/keys", api.CreateKey)
		authed.GET("/keys/:name", api.GetKey)
		authed.PUT("/keys/:name", api.PutKey)
		authed.DELETE("/keys/:name", api.DeleteKey)
		authed.POST("/keys/:name/rotate", api.RotateKey)

		// 模型路由（按路由键解析车道）。用 catch-all 以支持带路径分隔符的模型名（如 vendor/model）。
		authed.GET("/models", api.ListModels)
		authed.GET("/routes/*model", api.GetRoute)

		// 车道顺序摘要（不分页）与模型目录元数据（api-spec §5.7）。
		authed.GET("/lane-summaries", api.ListLaneSummaries)
		authed.GET("/model-metadata", api.ListModelMetadata)
		authed.PUT("/model-metadata/*model", api.PutModelMetadata)
		authed.DELETE("/model-metadata/*model", api.DeleteModelMetadataByModel)

		// 渠道批量运维、系统选项/任务/性能、预填组、模型目录运维
		// （api-spec §5.3.1–§5.3.4）：路由与响应策略同表声明（design-v1 §16.3）。
		api.RegisterOpsRoutes(authed)
	}
}
