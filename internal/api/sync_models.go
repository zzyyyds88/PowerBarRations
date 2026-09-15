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

	"pbr/constant"
	"pbr/internal/apierr"
	"pbr/model"
	relaycommon "pbr/relay/common"

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
	// 正在被隐式路由/显式车道使用的成员会被静默摘掉。空清单默认拒绝，需 ?force=1。
	emptyWipe := len(remote) == 0 && len(local) > 0
	// 审查 B5：被移除的模型若仍被显式车道成员点名，摘掉会让该成员立即断流。
	referenced := findRemovedModelReferences(channel.Id, removed)

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
			"refusing to remove models still referenced: "+strings.Join(referenced, ", "),
			"update the lanes first, or retry with ?force=1 to override")
		return
	}

	encoded := strings.Join(remote, ",")
	if err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("models", encoded).Error; err != nil {
		writeAPIError(c, err)
		return
	}
	// models 变了要重建渠道缓存与 abilities，隐式链才会立刻反映新清单。
	if err := channel.UpdateAbilities(nil); err != nil {
		writeAPIError(c, err)
		return
	}
	model.InitChannelCache()

	saved, err := findChannelByName(channel.Name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	response := channelResponse(saved)
	writeAudit(c, "sync-models", "channel", channel.Name, gin.H{"add": added, "remove": removed})
	c.JSON(http.StatusOK, gin.H{"channel": channel.Name, "models": response["models"], "added": added, "removed": removed})
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

// findRemovedModelReferences 找出仍引用这些将被移除模型的显式车道成员。
// 返回形如 "lane lane-a -> channel/upstream_model" 的清单，供 409 提示。
func findRemovedModelReferences(channelID int, removed []string) []string {
	removedSet := map[string]bool{}
	for _, m := range removed {
		if m = strings.TrimSpace(m); m != "" {
			removedSet[m] = true
		}
	}
	if len(removedSet) == 0 {
		return nil
	}
	var members []model.LaneMember
	if err := model.DB.Where("channel_id = ?", channelID).Find(&members).Error; err != nil {
		return nil
	}
	refs := make([]string, 0, len(members))
	for _, member := range members {
		if !removedSet[strings.TrimSpace(member.UpstreamModel)] {
			continue
		}
		var lane model.Lane
		laneName := strconv.Itoa(member.LaneId)
		if err := model.DB.Select("name").Where("id = ?", member.LaneId).First(&lane).Error; err == nil {
			laneName = lane.Name
		}
		refs = append(refs, "lane "+laneName+" uses upstream_model "+member.UpstreamModel)
	}
	sort.Strings(refs)
	return refs
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
