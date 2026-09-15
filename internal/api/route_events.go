package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"pbr/internal/route"
	"pbr/model"

	"github.com/gin-gonic/gin"
)

// GET /api/v1/route-events（routing-spec §7）
//
// SSE 推送车道运行态增量（当前成员 / 探测占用 / 亲和 / 冷却表），供控制台实时显示。
// 控制台另有 30s 轮询兜底，SSE 只作加速、不作唯一数据源——因此这里在连接建立时
// 先推一份全量快照，之后按固定节奏推增量快照，客户端重连即可自愈。
func RouteEvents(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming unsupported"}})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher.Flush()

	interval := 1 * time.Second
	if raw := c.Query("interval_ms"); raw != "" {
		if parsed, err := time.ParseDuration(raw + "ms"); err == nil && parsed >= 200*time.Millisecond && parsed <= 30*time.Second {
			interval = parsed
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	send := func() bool {
		payload, err := routeStateSnapshot()
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(c.Writer, "event: route-state\ndata: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send() {
		return
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

// routeStateSnapshot 汇总当前有运行态的车道（形状对齐控制台"成员状态"）。
func routeStateSnapshot() ([]byte, error) {
	lanes := route.Default.SnapshotLanes()
	items := make([]route.HealthSnapshot, 0, len(lanes))
	for _, lane := range lanes {
		resolved, err := model.ResolveRoute(lane)
		if err != nil {
			continue
		}
		items = append(items, route.Default.For(lane).Health(resolved, route.CurrentCircuitSettings()))
	}
	return json.Marshal(gin.H{"ts": time.Now().UTC().Format(time.RFC3339), "lanes": items})
}
