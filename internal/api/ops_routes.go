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
type OpsRoute struct {
	Method  string
	Path    string
	Handler gin.HandlerFunc
	Policy  apiresp.Policy
}

// opsConflictFirst 是通用失败规则：显式 conflict 优先，其余 200 业务失败兜底 400。
func opsFailureRules() []apiresp.FailureRule {
	return []apiresp.FailureRule{
		{BaseCode: "conflict", OutStatus: http.StatusConflict, OutCode: apiresp.CodeConflict},
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

// opsPolicyWith 构造"自定义成功体 + 通用失败映射"的策略。
func opsPolicyWith(success apiresp.SuccessFunc) apiresp.Policy {
	p := opsPolicy()
	p.Success = success
	return p
}

// opsPolicyUpstream 是上游类端点（Codex/Ollama）的失败映射：剩余失败按上游错误 502。
func opsPolicyUpstream() apiresp.Policy {
	return apiresp.Policy{
		Failures: []apiresp.FailureRule{
			{BaseCode: "conflict", OutStatus: http.StatusConflict, OutCode: apiresp.CodeConflict},
			{Status: http.StatusOK, OutStatus: http.StatusBadGateway, OutCode: apiresp.CodeUpstreamError},
		},
	}
}

// opsRoutes 是运维端点的唯一声明表。
func opsRoutes() []OpsRoute {
	return []OpsRoute{
		// —— 渠道批量运维（api-spec §5.3.1）——
		{
			Method: http.MethodPost, Path: "/channels/batch/status", Handler: BatchChannelStatusByName,
			Policy: opsPolicyWith(changedCountSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/batch/tag", Handler: BatchChannelTagByName,
			Policy: opsPolicyWith(changedCountSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/batch/copy", Handler: CopyChannelByBodyName,
			Policy: opsPolicyWith(copyChannelSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/batch/fetch-models", Handler: FetchModelsByName,
			Policy: opsPolicyWith(modelsSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/batch/repair", Handler: RepairChannelAbilities,
			Policy: opsPolicyWith(repairSuccess),
		},
		{
			Method: http.MethodPut, Path: "/channels/by-tag", Handler: EditChannelsByTag,
			Policy: opsPolicyWith(tagUpdatedSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/by-tag/status", Handler: BatchChannelTagStatus,
			Policy: opsPolicyWith(tagEnabledSuccess),
		},
		{
			Method: http.MethodGet, Path: "/channels/by-tag/models", Handler: ListChannelsByTagModels,
			Policy: opsPolicyWith(tagModelsSuccess),
		},
		{
			Method: http.MethodDelete, Path: "/channels/disabled", Handler: DeleteDisabledChannels,
			Policy: opsPolicyWith(deletedCountSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/upstream-updates/detect-all", Handler: DetectAllUpstream,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/channels/upstream-updates/apply-all", Handler: ApplyAllUpstream,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodGet, Path: "/channels/:name/key", Handler: GetChannelKeyByName,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/multi-keys", Handler: ManageMultiKeysByName,
			Policy: opsPolicyWith(multiKeySuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/upstream-updates/detect", Handler: DetectUpstreamByName,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/upstream-updates/apply", Handler: ApplyUpstreamByName,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/codex/refresh", Handler: CodexRefreshByName,
			Policy: opsPolicyUpstream(),
		},
		{
			Method: http.MethodGet, Path: "/channels/:name/codex/usage", Handler: CodexUsageByName,
			Policy: opsPolicyWith(codexSuccess),
		},
		{
			Method: http.MethodGet, Path: "/channels/:name/codex/reset-credits", Handler: CodexResetCreditsByName,
			Policy: opsPolicyWith(codexSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/codex/reset", Handler: CodexResetUsageByName,
			Policy: opsPolicyWith(codexSuccess),
		},
		{
			Method: http.MethodPost, Path: "/channels/:name/ollama/pull", Handler: OllamaPullByName,
			Policy: opsPolicyWith(ollamaPullSuccess),
		},
		{
			// SSE 流式端点：不套信封（design-v1 §5.1）。进入流之前的参数/渠道错误
			// 由适配层直接写成 §3 错误包络。
			Method: http.MethodPost, Path: "/channels/:name/ollama/pull/stream", Handler: OllamaPullStreamByName,
			Policy: apiresp.Policy{Passthrough: true},
		},
		{
			Method: http.MethodDelete, Path: "/channels/:name/ollama/models", Handler: OllamaDeleteByName,
			Policy: opsPolicyWith(ollamaDeleteSuccess),
		},
		{
			Method: http.MethodGet, Path: "/channels/:name/ollama/version", Handler: OllamaVersionByName,
			Policy: opsPolicyWith(ollamaVersionSuccess),
		},

		// —— 系统选项、任务与性能（api-spec §5.3.2）——
		{
			Method: http.MethodGet, Path: "/system/options/all", Handler: GetAllSystemOptions,
			Policy: opsPolicyWith(optionsSuccess),
		},
		{
			Method: http.MethodPut, Path: "/system/options/all", Handler: UpdateSystemOptionsByName,
			Policy: opsPolicyWith(optionUpdatedSuccess),
		},
		{
			Method: http.MethodGet, Path: "/system/affinity-cache", Handler: AffinityCacheStats,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodDelete, Path: "/system/affinity-cache", Handler: ClearAffinityCache,
			Policy: opsPolicyWith(deletedSuccess),
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
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodGet, Path: "/system/performance", Handler: PerformanceStats,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/system/performance/reset", Handler: ResetPerformanceStats,
			Policy: opsPolicyWith(flagSuccess("reset")),
		},
		{
			Method: http.MethodPost, Path: "/system/performance/gc", Handler: ForceGarbageCollection,
			Policy: opsPolicyWith(flagSuccess("collected")),
		},
		{
			Method: http.MethodDelete, Path: "/system/performance/disk-cache", Handler: ClearDiskCacheHandler,
			Policy: opsPolicyWith(flagSuccess("cleared")),
		},
		{
			Method: http.MethodGet, Path: "/system/log-files", Handler: ListLogFilesHandler,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodDelete, Path: "/system/log-files", Handler: CleanupLogFilesHandler,
			Policy: opsPolicy(),
		},

		// —— 预填组（api-spec §5.3.3）——
		{
			Method: http.MethodGet, Path: "/prefill-groups", Handler: ListPrefillGroups,
			Policy: opsPolicyWith(itemsSuccess),
		},
		{
			Method: http.MethodPost, Path: "/prefill-groups", Handler: CreatePrefillGroup,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPut, Path: "/prefill-groups/:id", Handler: UpdatePrefillGroupByID,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodDelete, Path: "/prefill-groups/:id", Handler: DeletePrefillGroupByID,
			Policy: opsPolicyWith(deletedTrueSuccess),
		},

		// —— 模型目录运维（api-spec §5.3.4）——
		{
			Method: http.MethodGet, Path: "/model-catalog/sync-upstream/preview", Handler: SyncUpstreamPreviewH,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodPost, Path: "/model-catalog/sync-upstream", Handler: SyncUpstreamApplyH,
			Policy: opsPolicy(),
		},
		{
			Method: http.MethodGet, Path: "/model-catalog/missing", Handler: MissingModelsHandler,
			Policy: opsPolicyWith(modelsSuccess),
		},
		{
			Method: http.MethodPost, Path: "/model-catalog/batch-delete", Handler: BatchDeleteModelMetaByName,
			Policy: opsPolicy(),
		},
	}
}

// RegisterOpsRoutes 按声明表注册运维端点，并把响应策略登记进 apiresp。
//
// 路由与策略同源：注册即登记，不可能出现"注册了却没策略"。
func RegisterOpsRoutes(group *gin.RouterGroup) {
	for _, route := range opsRoutes() {
		apiresp.Register(route.Method, route.Path, route.Policy)
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

func deletedSuccess(_ *gin.Context, base apiresp.Base) any {
	value, ok := base.DataValue().(map[string]any)
	if !ok {
		return gin.H{"deleted": 0}
	}
	return gin.H{"deleted": intOf(value["deleted"])}
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

func repairSuccess(_ *gin.Context, base apiresp.Base) any {
	value, ok := base.DataValue().(map[string]any)
	if !ok {
		return gin.H{"repaired": 0, "failed": 0}
	}
	return gin.H{"repaired": intOf(value["success"]), "failed": intOf(value["fails"])}
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
	value, ok := base.DataValue().(map[string]any)
	if !ok {
		return gin.H{"upstream_status": 0, "body": nil}
	}
	status := 0
	if raw, ok := value["upstream_status"]; ok {
		status = intOf(raw)
	}
	return gin.H{"upstream_status": status, "body": value["data"]}
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
