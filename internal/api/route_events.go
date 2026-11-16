package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/logger"
	"github.com/zzyyyds88/PowerBarRations/model"

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

	// send 返回错误而不是布尔：快照失败必须留下痕迹（此前静默吞掉，表现为
	// "流在跑但数据不动"，排障无从解释）。快照错误不致命（连接还在，下个 tick 重试）；
	// 写失败说明客户端断了，结束本轮。
	send := func() error {
		payload, err := routeStateSnapshot()
		if err != nil {
			return &snapshotError{err: err}
		}
		if _, err := fmt.Fprintf(c.Writer, "event: route-state\ndata: %s\n\n", payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	push := func() bool {
		err := send()
		if err == nil {
			return true
		}
		logger.LogWarn(c, "pbr: route-events 推送失败: "+err.Error())
		var snap *snapshotError
		return errors.As(err, &snap) // 快照错误：继续推流；写失败：结束
	}

	if !push() {
		return
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if !push() {
				return
			}
		}
	}
}

// snapshotError 标记"快照构造失败"（区别于往客户端写失败）。
type snapshotError struct{ err error }

func (e *snapshotError) Error() string { return "snapshot failed: " + e.err.Error() }
func (e *snapshotError) Unwrap() error { return e.err }

// routeStateSnapshotTTL 快照缓存时长。
//
// 每个 SSE 连接每秒都会请求一次全量快照，而每次快照要对**每个有运行态的车道**
// 做一次 ResolveRoute（含逐成员查渠道）。多开几个控制台标签会把 DB 查询量按
// 连接数 × 车道数 × 成员数 放大，因此同进程内按 TTL 复用同一份快照。
const routeStateSnapshotTTL = 900 * time.Millisecond

var (
	routeSnapshotMu      sync.Mutex
	routeSnapshotPayload []byte
	routeSnapshotAt      time.Time
)

// routeStateSnapshot 汇总当前有运行态的车道（形状对齐控制台"成员状态"）。
func routeStateSnapshot() ([]byte, error) {
	routeSnapshotMu.Lock()
	defer routeSnapshotMu.Unlock()
	now := time.Now()
	if routeSnapshotPayload != nil && now.Sub(routeSnapshotAt) < routeStateSnapshotTTL {
		return routeSnapshotPayload, nil
	}
	payload, err := buildRouteStateSnapshot()
	if err != nil {
		return nil, err
	}
	routeSnapshotPayload = payload
	routeSnapshotAt = now
	return payload, nil
}

func buildRouteStateSnapshot() ([]byte, error) {
	lanes := route.Default.SnapshotLanes()
	items := make([]route.HealthSnapshot, 0, len(lanes))
	for _, lane := range lanes {
		resolved, err := model.ResolveRoute(lane)
		if err != nil {
			return nil, err
		}
		items = append(items, route.Default.For(lane).Health(resolved, route.CurrentCircuitSettings()))
	}
	payload, err := json.Marshal(gin.H{"ts": time.Now().UTC().Format(time.RFC3339), "lanes": items})
	if err != nil {
		return nil, err
	}
	return payload, nil
}
