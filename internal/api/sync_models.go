package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"

	"github.com/gin-gonic/gin"
)

// POST /api/v1/channels/{name}/sync-models（api-spec §5.3）
//
// 从上游拉取模型清单并回写渠道的 models（路由键）。`?dry_run=` 只返回差异。
// 拉取走 OpenAI 兼容的 GET {base_url}/v1/models；原生协议渠道明确回 unsupported，
// 不做"看起来成功"的假同步。
func SyncChannelModels(c *gin.Context) {
	channel, err := findChannelByName(c.Param("name"))
	if err != nil {
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+c.Param("name")+"' not found", "GET /api/v1/channels")
		return
	}
	if isProbeUnsupported(channel.Type) {
		apierr.Write(c, http.StatusBadRequest, "unsupported_channel_type",
			"channel type '"+ChannelTypeSlug(channel.Type)+"' does not expose an OpenAI-compatible /v1/models", "")
		return
	}
	key, _, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil {
		apierr.Write(c, http.StatusBadGateway, "upstream_error", "no enabled key: "+keyErr.Error(), "")
		return
	}
	baseURL := channel.GetBaseURL()
	if strings.TrimSpace(baseURL) == "" {
		apierr.Write(c, http.StatusBadGateway, "upstream_error", "channel base_url is empty", "")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	requestURL := relaycommon.GetFullRequestURL(baseURL, "/v1/models", channel.Type)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		apierr.Write(c, http.StatusBadGateway, "upstream_error", err.Error(), "")
		return
	}
	if channel.Type == constant.ChannelTypeAzure {
		req.Header.Set("api-key", key)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		apierr.Write(c, http.StatusBadGateway, "upstream_error", err.Error(), "上游不可达或超时")
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apierr.Write(c, http.StatusBadGateway, "upstream_error",
			"upstream returned "+resp.Status, string(body[:min(len(body), 200)]))
		return
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		apierr.Write(c, http.StatusBadGateway, "upstream_error", "upstream /v1/models is not a model list", "")
		return
	}

	remote := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			remote = append(remote, id)
		}
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
	// 被本次移除命中的同名车道（且该车道有本渠道成员）要先处理，否则该车道对
	// 这个路由键就失去成员来源（api-spec §5.3 守卫口径）。
	referenced, refErr := model.RemovedModelLaneRefs(channel.Id, removed)
	if refErr != nil {
		writeAPIError(c, refErr)
		return
	}

	// dry_run 是预览：始终返回差异与"是否会被拦截"，不写库、不报错，让调用方先看清楚。
	if dryRun(c) {
		c.JSON(http.StatusOK, gin.H{
			"dry_run": true, "valid": true, "channel": channel.Name,
			"diff":           gin.H{"models": gin.H{"add": added, "remove": removed}},
			"current":        local,
			"remote":         remote,
			"blocked":        emptyWipe || (len(referenced) > 0 && !force),
			"empty_upstream": emptyWipe,
			"referenced_by":  referenced,
		})
		return
	}

	if emptyWipe && !force {
		apierr.Write(c, http.StatusConflict, apierr.CodeConflict,
			"upstream returned an empty model list; refusing to wipe "+strconv.Itoa(len(local))+" local model(s)",
			"retry with ?force=1 to confirm the wipe")
		return
	}
	if len(referenced) > 0 && !force {
		apierr.Write(c, http.StatusConflict, apierr.CodeConflict,
			"refusing to remove models still referenced by lanes: "+strings.Join(referenced, ", "),
			"update the lanes first, or retry with ?force=1 to also remove this channel's members from those lanes")
		return
	}

	encoded := strings.Join(remote, ",")
	if err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("models", encoded).Error; err != nil {
		writeAPIError(c, err)
		return
	}
	// models 变了要重建渠道缓存。
	model.InitChannelCache()

	// force 覆盖后同样要清理受影响车道上的本渠道成员（空车道删除并清运行态），
	// 否则"渠道已不再声明、路由页仍显示可调用"的不一致只是被推迟到下一次同步。
	var cleanedLanes, deletedLanes []string
	if len(referenced) > 0 {
		cleaned, deleted, cleanupErr := model.CleanupLanesForRemovedModels(channel.Id, removed)
		if cleanupErr != nil {
			writeAPIError(c, cleanupErr)
			return
		}
		for _, laneName := range deleted {
			route.Default.Remove(laneName)
		}
		cleanedLanes, deletedLanes = cleaned, deleted
	}

	saved, err := findChannelByName(channel.Name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	response := channelResponse(saved)
	writeAudit(c, "sync-models", "channel", channel.Name, gin.H{"add": added, "remove": removed, "cleaned_lanes": cleanedLanes, "deleted_lanes": deletedLanes})
	c.JSON(http.StatusOK, gin.H{"channel": channel.Name, "models": response["models"], "added": added, "removed": removed,
		"cleaned_lanes": cleanedLanes, "deleted_lanes": deletedLanes})
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
