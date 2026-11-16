package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/controller"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// POST /api/v1/channels/{name}/sync-models（api-spec §5.3）
//
// 从上游拉取模型清单并回写渠道的 models（路由键）。`?dry_run=` 只返回差异。
// 拉取走 OpenAI 兼容的 GET {base_url}/v1/models；原生协议渠道明确回 unsupported，
// 不做"看起来成功"的假同步。
func SyncChannelModels(c *gin.Context) {
	channel, err := findChannelByName(c.Param("name"))
	if err != nil {
		if writeChannelLookupError(c, err) {
			return
		}
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+c.Param("name")+"' not found", "GET /api/v1/channels")
		return
	}
	// 复用渠道编辑器/上游检测的同一套厂商 URL、代理、头部覆盖与错误脱敏逻辑。
	// 之前这里自己拼 /v1/models + http.DefaultClient，代理渠道和 Gemini/VolcEngine
	// 等厂商会直接失败，且绕过渠道出站代理。
	remote, fetchErr := controller.FetchChannelUpstreamModelIDs(channel)
	if fetchErr != nil {
		apierr.Write(c, http.StatusBadGateway, "upstream_error", fetchErr.Error(), "上游不可达或不支持模型列表")
		return
	}
	sort.Strings(remote)
	local := make([]string, 0, 4)
	for _, name := range channel.GetModels() {
		if name = strings.TrimSpace(name); name != "" {
			local = append(local, name)
		}
	}

	added, removed := diffModels(local, remote)
	// added/removed 为空时必须序列化成 []，不能是 null（api-spec §3 数组契约）。
	if added == nil {
		added = []string{}
	}
	if removed == nil {
		removed = []string{}
	}
	force := isForce(c)

	// 审查 B5：上游鉴权异常但返回 200 + 空清单时，整表覆盖会把渠道 models 清空，
	// 那些"靠渠道声明做候选来源"的模型会失去来源。空清单默认拒绝，需 ?force=1。
	emptyWipe := len(remote) == 0 && len(local) > 0
	// 被本次移除命中的车道与成员组要先处理，否则车道/成员组会失去模型来源
	// （api-spec §5.3 守卫口径；ADR 0009 之后必须同时查组）。
	refs, refErr := model.RemovedModelReferences(channel.Id, removed)
	if refErr != nil {
		writeAPIError(c, refErr)
		return
	}
	referenced, referencedGroups := refs.Lanes, refs.Groups

	// dry_run 是预览：始终返回差异与"是否会被拦截"，不写库、不报错，让调用方先看清楚。
	if dryRun(c) {
		blocked := emptyWipe && !force
		if len(referencedGroups) > 0 {
			blocked = true
		}
		if len(referenced) > 0 && !force {
			blocked = true
		}
		c.JSON(http.StatusOK, gin.H{
			"dry_run": true, "valid": true, "channel": channel.Name,
			"diff":           gin.H{"models": gin.H{"add": added, "remove": removed}},
			"current":        local,
			"remote":         remote,
			"blocked":        blocked,
			"empty_upstream": emptyWipe,
			"referenced_by":  referenced,
			"groups":         referencedGroups,
		})
		return
	}

	if emptyWipe && !force {
		apierr.Write(c, http.StatusConflict, apierr.CodeConflict,
			"upstream returned an empty model list; refusing to wipe "+strconv.Itoa(len(local))+" local model(s)",
			"retry with ?force=1 to confirm the wipe")
		return
	}
	if len(referencedGroups) > 0 {
		apierr.WriteDetails(c, http.StatusConflict, apierr.CodeConflict,
			"refusing to remove models still referenced by member groups: "+strings.Join(referencedGroups, ", "),
			"edit the member groups to remove the members first",
			gin.H{"lanes": referenced, "groups": referencedGroups})
		return
	}
	if len(referenced) > 0 && !force {
		apierr.WriteDetails(c, http.StatusConflict, apierr.CodeConflict,
			"refusing to remove models still referenced by lanes: "+strings.Join(referenced, ", "),
			"update the lanes first, or retry with ?force=1 to also remove this channel's members from those lanes",
			gin.H{"lanes": referenced, "groups": referencedGroups})
		return
	}

	encoded := strings.Join(remote, ",")
	var cleanedLanes, deletedLanes, manualLanesCleared []string
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		refsNow, refErr := model.RemovedModelReferencesTx(tx, channel.Id, removed)
		if refErr != nil {
			return refErr
		}
		approved := make(map[string]bool, len(referenced))
		for _, lane := range referenced {
			approved[lane] = true
		}
		newLanes := make([]string, 0)
		for _, lane := range refsNow.Lanes {
			if !approved[lane] {
				newLanes = append(newLanes, lane)
			}
		}
		if len(refsNow.Groups) > 0 ||
			(len(refsNow.Lanes) > 0 && !force) ||
			len(newLanes) > 0 {
			return &apiError{
				status:  http.StatusConflict,
				code:    apierr.CodeConflict,
				message: "channel model references changed while syncing; retry after reviewing the lanes",
				hint:    "reload the channel and confirm the new lane/member-group references",
				details: gin.H{
					"lanes":  refsNow.Lanes,
					"groups": refsNow.Groups,
					"new":    newLanes,
				},
			}
		}
		if err := tx.Model(&model.Channel{}).Where("id = ?", channel.Id).
			Update("models", encoded).Error; err != nil {
			return err
		}
		// force 覆盖后的成员清理与渠道 models 写入同事务：不能出现“渠道已不再
		// 声明、车道仍引用”或“成员已删、空车道还在”的半状态。
		if len(referenced) > 0 {
			cleaned, deleted, err := model.CleanupLanesForRemovedModelsTx(tx, channel.Id, removed)
			if err != nil {
				return err
			}
			cleanedLanes, deletedLanes = cleaned, deleted
		}
		return nil
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	// models 变了要重建渠道缓存。
	model.InitChannelCache()
	for _, laneName := range deletedLanes {
		route.Default.Remove(laneName)
	}
	if len(cleanedLanes) > 0 {
		manualLanesCleared, _ = model.ReconcileManualActiveMembersByNames(cleanedLanes)
	}

	saved, err := findChannelByName(channel.Name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	response := channelResponse(saved)
	if cleanedLanes == nil {
		cleanedLanes = []string{}
	}
	if deletedLanes == nil {
		deletedLanes = []string{}
	}
	if manualLanesCleared == nil {
		manualLanesCleared = []string{}
	}
	writeAudit(c, "sync-models", "channel", channel.Name, gin.H{
		"add": added, "remove": removed,
		"cleaned_lanes": cleanedLanes, "deleted_lanes": deletedLanes,
		"manual_lanes_cleared": manualLanesCleared, "groups": referencedGroups,
	})
	c.JSON(http.StatusOK, gin.H{"channel": channel.Name, "models": response["models"], "added": added, "removed": removed,
		"cleaned_lanes": cleanedLanes, "deleted_lanes": deletedLanes,
		"manual_lanes_cleared": manualLanesCleared, "groups": referencedGroups})
}

// isForce 解析 ?force=1/true/yes 覆盖开关。
func isForce(c *gin.Context) bool {
	switch strings.ToLower(strings.TrimSpace(c.Query("force"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func diffModels(local, remote []string) (added, removed []string) {
	have := map[string]bool{}
	for _, name := range local {
		have[name] = true
	}
	want := map[string]bool{}
	for _, name := range remote {
		want[name] = true
		if !have[name] {
			added = append(added, name)
		}
	}
	for _, name := range local {
		if !want[name] {
			removed = append(removed, name)
		}
	}
	return added, removed
}
