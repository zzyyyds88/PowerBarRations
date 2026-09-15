package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
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
	if dryRun(c) {
		c.JSON(http.StatusOK, gin.H{
			"dry_run": true, "valid": true, "channel": channel.Name,
			"diff":    gin.H{"models": gin.H{"add": added, "remove": removed}},
			"current": local, "remote": remote,
		})
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
