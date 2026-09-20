package api

import (
	"net/http"

	"github.com/zzyyyds88/PowerBarRations/internal/apiresp"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 运维闭环端点的**路由 + 响应策略同表声明**（design-v1 §16.3、api-spec §5.3.1–§5.3.4）。
//
// 新增端点必须在此登记 ResponsePolicy；`TestEveryOpsRouteDeclaresResponsePolicy`
// 与 `TestOpsRoutesMatchRegisteredRoutes` 把"漏登记/漏注册"变成构建期失败。
//
// 成功体：基座 data 原样裸化（Success == nil），或由 Success 转成契约要求的语义化对象。
// 失败映射：只按**显式 code / HTTP 状态**匹配；适配层已把可预判的错误（未知名、空目标、
// 非法枚举）挡在基座 handler 之前，因此剩余失败以 409 conflict 与 400 兜底为主。

// OpsRoute 是一条运维端点的完整声明。
//
// DryRun 是**强制字段**：任何写方法（POST/PUT/DELETE）都必须声明它，否则
// TestEveryOpsWriteRouteDeclaresDryRun 失败——这是"不允许静默写入"的构建期约束。
type OpsRoute struct {
	Method  string
	Path    string
	Handler gin.HandlerFunc
	Policy  apiresp.Policy
	// DryRun 声明该端点对 ?dry_run=true 的处理（api-spec §2.4/§5.9）。
	DryRun DryRunMode
	// DryRunReason 说明 reject 的替代做法/理由（preview 时是预览体要点）。
	DryRunReason string
}

// opsConflictFirst 是通用失败规则：显式 conflict 优先，其余 200 业务失败兜底 400。
// 基座自己给出 409 的（如 detect-all 的"已有同类任务在跑"）按 conflict 归类
// （api-spec §3），不得退化成兜底的 invalid_request。
func opsFailureRules() []apiresp.FailureRule {
	return []apiresp.FailureRule{
		{BaseCode: "conflict", OutStatus: http.StatusConflict, OutCode: apiresp.CodeConflict},
		{Status: http.StatusConflict, OutStatus: http.StatusConflict, OutCode: apiresp.CodeConflict},
		{Status: http.StatusOK, OutStatus: http.StatusBadRequest, OutCode: apiresp.CodeValidationFailed},
	}
}

// opsConflictDetails 从基座响应提取 blocked 明细（渠道名 → 引用它的车道名）。
func opsConflictDetails(base apiresp.Base) any {
	value, ok := base.DataValue().(map[string]any)
	if !ok {
		return nil
	}
	if blocked, ok := value["blocked"]; ok {
		return gin.H{"blocked": blocked}
	}
	return nil
}

// opsPolicy 构造"data 裸化 + 通用失败映射"的策略。
func opsPolicy() apiresp.Policy {
	return apiresp.Policy{Failures: opsFailureRules(), Details: opsConflictDetails}
}

// logFilesFailureDetails 提取"部分删除失败"的文件清单（api-spec §5.3.2：
// DELETE /api/system/log-files 部分删除失败 → 500 + details.failed_files）。
func logFilesFailureDetails(base apiresp.Base) any {
	value, ok := base.DataValue().(map[string]any)
	if !ok {
		return nil
	}
	if failed, ok := value["failed_files"]; ok {
		return gin.H{"failed_files": failed}
	}
	return nil
}

// opsPolicyLogFilesCleanup 是日志文件清理的策略：在通用规则前插入
// partial_failure → 500 internal_error（+ details.failed_files 明细）。
func opsPolicyLogFilesCleanup() apiresp.Policy {
	return apiresp.Policy{
		Failures: append([]apiresp.FailureRule{
			{BaseCode: "partial_failure", OutStatus: http.StatusInternalServerError, OutCode: apiresp.CodeInternalError},
		}, opsFailureRules()...),
		Details: logFilesFailureDetails,
	}
}

// opsPolicyWith 构造"自定义成功体 + 通用失败映射"的策略。
func opsPolicyWith(success apiresp.SuccessFunc) apiresp.Policy {
	p := opsPolicy()
	p.Success = success
	return p
}

// opsPolicyOptionWrite 是 PUT /api/system/options/all 的专用策略：
// 适配层已挡住"缺 key"这类参数错误，进入基座后的 200 业务失败全部是
// **选项值校验失败**（theme/倍率/JSON 等），契约固定为 422 validation_failed
// （api-spec §5.3.2）。通用规则会把它们映射成 400，故单独建表。
func opsPolicyOptionWrite() apiresp.Policy {
	return apiresp.Policy{
		Failures: []apiresp.FailureRule{
			{BaseCode: "conflict", OutStatus: http.StatusConflict, OutCode: apiresp.CodeConflict},
			{Status: http.StatusOK, OutStatus: http.StatusUnprocessableEntity, OutCode: apiresp.CodeValidationFailed},
		},
		Success: optionUpdatedSuccess,
	}
}

// opsPolicyUpstream 是上游类端点（Codex/Ollama/上游探测）的失败映射：
// 剩余失败按上游错误 502（api-spec §5.3.1）。基座对"上游调用失败"实际给出的
// 状态既有 200（success:false）也有 500，两者都要映射，否则 Ollama pull/delete
// 的上游失败会漏成 500 internal_error。
func opsPolicyUpstream() apiresp.Policy {
	return apiresp.Policy{
		Failures: []apiresp.FailureRule{
			{BaseCode: "conflict", OutStatus: http.StatusConflict, OutCode: apiresp.CodeConflict},
			{Status: http.StatusOK, OutStatus: http.StatusBadGateway, OutCode: apiresp.CodeUpstreamError},
			{Status: http.StatusInternalServerError, OutStatus: http.StatusBadGateway, OutCode: apiresp.CodeUpstreamError},
		},
	}
}

// opsPolicyUpstreamWith 在 opsPolicyUpstream 上叠加自定义成功体。
func opsPolicyUpstreamWith(success apiresp.SuccessFunc) apiresp.Policy {
	p := opsPolicyUpstream()
	p.Success = success
	return p
}

// opsPolicyCodex 是 Codex 端点的策略：失败按上游 502，并带上游状态码明细。
func opsPolicyCodex() apiresp.Policy {
	p := opsPolicyUpstreamWith(codexSuccess)
	p.Details = codexDetails
	return p
}

// opsRoutes 是运维端点的唯一声明表。
func opsRoutes() []OpsRoute {
	return []OpsRoute{
		// —— 渠道批量运维（api-spec §5.3.1）——
		{
			Method: http.MethodPost, Path: "/channels/batch/status", Handler: BatchChannelStatusByName,
			DryRun: DryRunPreview, DryRunReason: "将变更的渠道名",
			Policy: opsPolicyWith(changedCountSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/batch/tag", Handler: BatchChannelTagByName,
			DryRun: DryRunPreview, DryRunReason: "将变更的渠道名",
			Policy: opsPolicyWith(changedCountSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/batch/copy", Handler: CopyChannelByBodyName,
			DryRun: DryRunPreview, DryRunReason: "新渠道名",
			Policy: opsPolicyWith(copyChannelSuccess),
		},
		{
			// 会真实访问上游：剩余失败按上游错误 502。
			Method: http.MethodPost, Path: "/channels/batch/fetch-models", Handler: FetchModelsByName,
			DryRun: DryRunReject, DryRunReason: "只读拉取上游模型清单，不落库",
			Policy: opsPolicyUpstreamWith(modelsSuccess),
		},
		{
			Method: http.MethodPut, Path: "/channels/by-tag", Handler: EditChannelsByTag,
			DryRun: DryRunPreview, DryRunReason: "将变更的渠道名；被引用时仍返回 409",
			Policy: opsPolicyWith(tagUpdatedSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/by-tag/status", Handler: BatchChannelTagStatus,
			DryRun: DryRunPreview, DryRunReason: "将变更的渠道名",
			Policy: opsPolicyWith(tagEnabledSuccess),
		},
		{
			Method: http.MethodGet, Path: "/channels/by-tag/models", Handler: ListChannelsByTagModels,
			Policy: opsPolicyWith(tagModelsSuccess),
		},
		{
			Method: http.MethodDelete, Path: "/channels/disabled", Handler: DeleteDisabledChannels,
			DryRun: DryRunPreview, DryRunReason: "将删除的渠道名；被引用时仍返回 409",
			Policy: opsPolicyWith(deletedCountSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/upstream-updates/detect-all", Handler: DetectAllUpstream,
			DryRun: DryRunReject, DryRunReason: "触发探测任务，不改配置；应用变更走 apply",
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/channels/upstream-updates/apply-all", Handler: ApplyAllUpstream,
			DryRun: DryRunPreview, DryRunReason: "逐渠道将新增/移除的模型",
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodGet, Path: "/channels/:name/key", Handler: GetChannelKeyByName,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/multi-keys", Handler: ManageMultiKeysByName,
			DryRun: DryRunPreview, DryRunReason: "将变更的渠道；get_key_status 是读动作",
			Policy: opsPolicyWith(multiKeySuccess),
		},
		{
			// 会真实访问上游：剩余失败按上游错误 502。
			Method: http.MethodPost, Path: "/channels/:name/upstream-updates/detect", Handler: DetectUpstreamByName,
			DryRun: DryRunReject, DryRunReason: "触发探测，不改配置",
			Policy: opsPolicyUpstream(),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/upstream-updates/apply", Handler: ApplyUpstreamByName,
			DryRun: DryRunPreview, DryRunReason: "将新增/移除的模型",
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/codex/refresh", Handler: CodexRefreshByName,
			DryRun: DryRunReject, DryRunReason: "上游凭据动作，非配置",
			Policy: opsPolicyUpstream(),
		},
		{
			Method: http.MethodGet, Path: "/channels/:name/codex/usage", Handler: CodexUsageByName,
			Policy: opsPolicyCodex(),
		},
		{
			Method: http.MethodGet, Path: "/channels/:name/codex/reset-credits", Handler: CodexResetCreditsByName,
			Policy: opsPolicyCodex(),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/codex/reset", Handler: CodexResetUsageByName,
			DryRun: DryRunReject, DryRunReason: "上游用量动作，非配置",
			Policy: opsPolicyCodex(),
		},
		{
			// 上游失败（基座给 200/500）→ 502 upstream_error（api-spec §5.3.1）。
			Method: http.MethodPost, Path: "/channels/:name/ollama/pull", Handler: OllamaPullByName,
			DryRun: DryRunReject, DryRunReason: "上游动作，会真的拉取模型",
			Policy: opsPolicyUpstreamWith(ollamaPullSuccess),
		},
		{
			// SSE 流式端点：不套信封（design-v1 §5.1）。进入流之前的参数/渠道错误
			// 由适配层直接写成 §3 错误包络。
			Method: http.MethodPost, Path: "/channels/:name/ollama/pull/stream", Handler: OllamaPullStreamByName,
			DryRun: DryRunReject, DryRunReason: "上游动作（SSE 进度）",
			Policy: apiresp.Policy{Passthrough: true},
		},
		{
			Method: http.MethodDelete, Path: "/channels/:name/ollama/models", Handler: OllamaDeleteByName,
			DryRun: DryRunReject, DryRunReason: "上游动作",
			Policy: opsPolicyUpstreamWith(ollamaDeleteSuccess),
		},
		{
			Method: http.MethodGet, Path: "/channels/:name/ollama/version", Handler: OllamaVersionByName,
			Policy: opsPolicyUpstreamWith(ollamaVersionSuccess),
		},

		// —— 系统选项、任务与性能（api-spec §5.3.2）——
		{
			Method: http.MethodGet, Path: "/system/options/all", Handler: GetAllSystemOptions,
			Policy: opsPolicyWith(optionsSuccess),
		},
		{
			Method: http.MethodPut, Path: "/system/options/all", Handler: UpdateSystemOptionsByName,
			DryRun: DryRunPreview, DryRunReason: "将变更的选项键",
			Policy: opsPolicyOptionWrite(),
		},
		{
			Method: http.MethodGet, Path: "/system-tasks", Handler: ListSystemTasksHandler,
			Policy: opsPolicyWith(itemsSuccess),
		},
		{
			Method: http.MethodGet, Path: "/system-tasks/current", Handler: CurrentSystemTask,
			Policy: opsPolicyWith(taskWrappedSuccess),
		},
		{
			Method: http.MethodGet, Path: "/system-tasks/:id", Handler: GetSystemTaskByID,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/system-tasks/log-cleanup", Handler: CreateLogCleanupTask,
			DryRun: DryRunPreview, DryRunReason: "将创建的任务",
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodGet, Path: "/system/performance", Handler: PerformanceStats,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/system/performance/reset", Handler: ResetPerformanceStats,
			DryRun: DryRunReject, DryRunReason: "运行态统计，非配置",
			Policy: opsPolicyWith(flagSuccess("reset")),
		},
		{
			Method: http.MethodPost, Path: "/system/performance/gc", Handler: ForceGarbageCollection,
			DryRun: DryRunReject, DryRunReason: "运行态动作，非配置",
			Policy: opsPolicyWith(flagSuccess("collected")),
		},
		{
			Method: http.MethodDelete, Path: "/system/performance/disk-cache", Handler: ClearDiskCacheHandler,
			DryRun: DryRunReject, DryRunReason: "运行态缓存，非配置",
			Policy: opsPolicyWith(flagSuccess("cleared")),
		},
		{
			Method: http.MethodGet, Path: "/system/log-files", Handler: ListLogFilesHandler,
			Policy: opsPolicy(),
		},
		{
			// 部分删除失败：基座给 success:false + code=partial_failure + data.failed_files，
			// 映射为 500 并把 failed_files 透进 error.details（api-spec §5.3.2）。
			Method: http.MethodDelete, Path: "/system/log-files", Handler: CleanupLogFilesHandler,
			DryRun: DryRunPreview, DryRunReason: "将删除的日志文件名",
			Policy: opsPolicyLogFilesCleanup(),
		},

		// —— 预填组（api-spec §5.3.3）——
		{
			Method: http.MethodGet, Path: "/prefill-groups", Handler: ListPrefillGroups,
			Policy: opsPolicyWith(itemsSuccess),
		},
		{
			Method: http.MethodPost, Path: "/prefill-groups", Handler: CreatePrefillGroup,
			DryRun: DryRunPreview, DryRunReason: "将创建的组",
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPut, Path: "/prefill-groups/:id", Handler: UpdatePrefillGroupByID,
			DryRun: DryRunPreview, DryRunReason: "将更新的组",
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodDelete, Path: "/prefill-groups/:id", Handler: DeletePrefillGroupByID,
			DryRun: DryRunPreview, DryRunReason: "将删除的组",
			Policy: opsPolicyWith(deletedTrueSuccess),
		},

		// —— 模型目录运维（api-spec §5.3.4）——
		{
			Method: http.MethodGet, Path: "/model-catalog/sync-upstream/preview", Handler: SyncUpstreamPreviewH,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/model-catalog/sync-upstream", Handler: SyncUpstreamApplyH,
			DryRun: DryRunPreview, DryRunReason: "将新增/更新/移除的目录记录",
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodGet, Path: "/model-catalog/missing", Handler: MissingModelsHandler,
			Policy: opsPolicyWith(modelsSuccess),
		},
		{
			Method: http.MethodPost, Path: "/model-catalog/batch-delete", Handler: BatchDeleteModelMetaByName,
			DryRun: DryRunPreview, DryRunReason: "将删除的目录记录；被引用时仍返回 409",
			Policy: opsPolicy(),
		},
	}
}

// RegisterOpsRoutes 按声明表注册运维端点，并把响应策略登记进 apiresp。
//
// 路由与策略同源：注册即登记，不可能出现"注册了却没策略"。
// reject 类端点额外挂上强制中间件——它必须在 handler 之前 abort，
// 否则"声明了不支持"就会退回成"静默写入"（api-spec §2.4 铁律）。
func RegisterOpsRoutes(group *gin.RouterGroup) {
	for _, route := range opsRoutes() {
		apiresp.Register(route.Method, route.Path, route.Policy)
		// 声明进 dry-run 登记表；安全网由整个管理面的 DryRunMiddleware 统一执行。
		RegisterDryRun(route.Method, route.Path, route.DryRun, route.DryRunReason)
		group.Handle(route.Method, route.Path, route.Handler)
	}
}

// OpsRoutesForTest 暴露声明表给守卫测试。
func OpsRoutesForTest() []OpsRoute { return opsRoutes() }

// —— 成功体转写 ——

// copyChannelSuccess 把基座返回的 {id} 转成"写后回读的渠道对象"。
func copyChannelSuccess(_ *gin.Context, base apiresp.Base) any {
	value, ok := base.DataValue().(map[string]any)
	if !ok {
		return base.DataValue()
	}
	id := intOf(value["id"])
	cloned, err := model.GetChannelById(id, true)
	if err != nil || cloned == nil {
		return gin.H{"id": id}
	}
	return channelResponse(cloned)
}

func changedCountSuccess(_ *gin.Context, base apiresp.Base) any {
	return gin.H{"changed": intOf(base.DataValue())}
}

func deletedCountSuccess(_ *gin.Context, base apiresp.Base) any {
	return gin.H{"deleted": intOf(base.DataValue())}
}

func deletedTrueSuccess(c *gin.Context, base apiresp.Base) any {
	id := 0
	if raw := c.Param("id"); raw != "" {
		id = atoiOr(raw, 0)
	}
	return gin.H{"deleted": true, "id": id}
}

func modelsSuccess(_ *gin.Context, base apiresp.Base) any {
	value := base.DataValue()
	if list, ok := value.([]any); ok {
		return gin.H{"models": list}
	}
	return gin.H{"models": []string{}}
}

func itemsSuccess(_ *gin.Context, base apiresp.Base) any {
	value := base.DataValue()
	if list, ok := value.([]any); ok {
		return gin.H{"items": list, "next_cursor": nil}
	}
	return gin.H{"items": []any{}, "next_cursor": nil}
}

func taskWrappedSuccess(_ *gin.Context, base apiresp.Base) any {
	return gin.H{"task": base.DataValue()}
}

func tagUpdatedSuccess(c *gin.Context, _ apiresp.Base) any {
	return gin.H{"tag": c.GetString(opsTagKey), "updated": true}
}

func tagEnabledSuccess(c *gin.Context, _ apiresp.Base) any {
	return gin.H{"tag": c.GetString(opsTagKey), "enabled": c.GetBool(opsTagEnabledKey)}
}

func tagModelsSuccess(c *gin.Context, base apiresp.Base) any {
	raw, _ := base.DataValue().(string)
	models := make([]string, 0)
	for _, part := range splitComma(raw) {
		if part != "" {
			models = append(models, part)
		}
	}
	return gin.H{"tag": c.GetString(opsTagKey), "models": models}
}

func multiKeySuccess(c *gin.Context, base apiresp.Base) any {
	if c.GetString(opsMultiKeyActionKey) == "get_key_status" {
		return base.DataValue()
	}
	return gin.H{"applied": true, "message": base.Message}
}

func optionUpdatedSuccess(c *gin.Context, _ apiresp.Base) any {
	return gin.H{"key": c.GetString(opsOptionKey), "updated": true}
}

func optionsSuccess(_ *gin.Context, base apiresp.Base) any {
	value := base.DataValue()
	if list, ok := value.([]any); ok {
		return gin.H{"items": list}
	}
	return gin.H{"items": []any{}}
}

func flagSuccess(flag string) apiresp.SuccessFunc {
	return func(_ *gin.Context, _ apiresp.Base) any { return gin.H{flag: true} }
}

func codexSuccess(_ *gin.Context, base apiresp.Base) any {
	// upstream_status 在基座信封顶层，payload 在 data。
	return gin.H{"upstream_status": base.UpstreamStatus, "body": base.DataValue()}
}

// codexDetails 把上游状态码带进错误明细，便于调用方区分"上游 401/429"等。
func codexDetails(base apiresp.Base) any {
	if base.UpstreamStatus == 0 {
		return nil
	}
	return gin.H{"upstream_status": base.UpstreamStatus}
}

func ollamaPullSuccess(c *gin.Context, base apiresp.Base) any {
	return gin.H{"channel": c.Param("name"), "model": c.GetString(opsModelKey), "pulled": true}
}

func ollamaDeleteSuccess(c *gin.Context, base apiresp.Base) any {
	return gin.H{"channel": c.Param("name"), "model": c.GetString(opsModelKey), "deleted": true}
}

func ollamaVersionSuccess(c *gin.Context, base apiresp.Base) any {
	value, ok := base.DataValue().(map[string]any)
	if !ok {
		return gin.H{"channel": c.Param("name"), "version": ""}
	}
	version, _ := value["version"].(string)
	return gin.H{"channel": c.Param("name"), "version": version}
}
