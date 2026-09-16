package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"pbr/internal/apierr"
	"pbr/internal/webhook"
	"pbr/model"

	"github.com/gin-gonic/gin"
)

// Webhook 事件通知管理面（design-v1 §16.10，api-spec §5.8，/doc 手册 §5）。
//
// 配置存 system/options 键 PBRWebhookTargets（JSON 数组）；读取时 secret 只
// 回显掩码，PUT 时 secret 留空 = 保留原值（按 name 匹配原条目）。

type webhookTargetsBody struct {
	Targets []webhook.Target `json:"targets"`
}

type webhookTestBody struct {
	Name string `json:"name"`
}

// ListWebhooks GET /api/webhooks（/api/v1 别名同）：`{"targets":[...]}`，secret 掩码。
func ListWebhooks(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"targets": maskedTargets(webhook.CurrentTargets())})
}

// maskedTargets 输出回读形状：secret 掩码；events 保持数组（空 = 全部）。
func maskedTargets(targets []webhook.Target) []gin.H {
	out := make([]gin.H, 0, len(targets))
	for _, t := range targets {
		events := t.Events
		if events == nil {
			events = []string{}
		}
		out = append(out, gin.H{
			"name":    t.Name,
			"url":     t.URL,
			"secret":  webhook.MaskSecret(t.Secret),
			"enabled": t.Enabled,
			"events":  events,
		})
	}
	return out
}

// PutWebhooks PUT /api/webhooks：全量 upsert，写后回读（响应 = GET 形状）。
func PutWebhooks(c *gin.Context) {
	var body webhookTargetsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	// 全量替换语义下省略 targets 视为"没说要写什么"，拒绝而不是清空——
	// 避免调用方少带字段把全部目标静默删光。
	if body.Targets == nil {
		apierr.Validation(c, "targets is required (empty array clears all targets)")
		return
	}
	if err := webhook.ValidateTargets(body.Targets); err != nil {
		apierr.Validation(c, err.Error())
		return
	}
	// secret 留空 = 保留原值：按 name 匹配现存条目回填（api-spec §5.8）。
	existing := make(map[string]webhook.Target, len(body.Targets))
	for _, t := range webhook.CurrentTargets() {
		existing[t.Name] = t
	}
	merged := make([]webhook.Target, len(body.Targets))
	for i, t := range body.Targets {
		if strings.TrimSpace(t.Secret) == "" {
			if old, ok := existing[t.Name]; ok {
				t.Secret = old.Secret
			}
		}
		merged[i] = t
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		apierr.BadRequest(c, "invalid webhook targets")
		return
	}
	if dryRun(c) {
		writeAudit(c, "update", "webhooks", "webhooks")
		c.JSON(http.StatusOK, gin.H{
			"dry_run": true,
			"valid":   true,
			"diff": gin.H{"webhooks": gin.H{
				"update": []string{"targets=" + strconv.Itoa(len(merged))},
			}},
		})
		return
	}
	if err := model.UpdateOption(webhook.OptionKey, string(encoded)); err != nil {
		writeAPIError(c, err)
		return
	}
	writeAudit(c, "update", "webhooks", "webhooks")
	ListWebhooks(c)
}

// TestWebhook POST /api/webhooks/test：向指定 target 同步发一条测试事件，
// 同步等待最终结果（含重试），响应即投递结果。
func TestWebhook(c *gin.Context) {
	var body webhookTestBody
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		apierr.Validation(c, "name is required")
		return
	}
	name := strings.TrimSpace(body.Name)
	targets := webhook.CurrentTargets()
	var found *webhook.Target
	for i := range targets {
		if targets[i].Name == name {
			found = &targets[i]
			break
		}
	}
	if found == nil {
		apierr.NotFound(c, apierr.CodeWebhookTargetNotFound,
			"webhook target '"+name+"' not found", "GET /api/webhooks")
		return
	}
	result := webhook.DeliverTest(*found)
	c.JSON(http.StatusOK, gin.H{
		"status":      result.Status,
		"http_status": result.HTTPStatus,
		"error":       result.Error,
		"attempts":    result.Attempts,
	})
}

// ListWebhookDeliveries GET /api/webhooks/deliveries：cursor 分页，按时间倒序
// （与 /api/logs 同一模式：id 自增即时间序，游标是 base64 的裸 id）。
func ListWebhookDeliveries(c *gin.Context) {
	limit, cursor, err := pageParams(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	beforeId := 0
	if cursor != "" {
		id, convErr := strconv.Atoi(cursor)
		if convErr != nil {
			apierr.Validation(c, "cursor is not a valid cursor")
			return
		}
		beforeId = id
	}
	entries, err := model.ListWebhookDeliveries(limit, beforeId)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	var nextCursor any
	if len(entries) > limit {
		nextCursor = encodeCursor(strconv.Itoa(entries[limit-1].Id))
		entries = entries[:limit]
	}
	items := make([]gin.H, 0, len(entries))
	for i := range entries {
		entry := &entries[i]
		items = append(items, gin.H{
			"id":          entry.Id,
			"ts":          rfc3339(entry.Ts),
			"target":      entry.Target,
			"event_type":  entry.EventType,
			"lane":        entry.Lane,
			"member":      entry.Member,
			"status":      entry.Status,
			"http_status": entry.HTTPStatus,
			"error":       entry.Error,
			"attempt":     entry.Attempt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor})
}
