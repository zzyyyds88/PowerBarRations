package async

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/logger"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/relayconvert/kitutil"
	"github.com/zzyyyds88/PowerBarRations/service"
)

// orphanCompensationTimeout 补偿删除的上限。创建响应可能已经返回给客户端，
// 补偿只是后台清理，不允许无限期拖住错误响应。
const orphanCompensationTimeout = 10 * time.Second

// compensateOrphanUnlessRecorded 在落库失败后先确认该归属行确实不存在，再删除
// 上游对象。若归属行已经存在（例如上游对重复创建返回了同一 ID），删除上游对象
// 反而会让既有映射指向已删除对象，因此跳过补偿。
func (h *Handler) compensateOrphanUnlessRecorded(c *gin.Context, sel *selection, laneName, kind, upstreamID string) {
	if _, err := model.GetAsyncObject(laneName, kind, upstreamID); err == nil {
		return
	}
	h.compensateOrphan(c, sel, kind, upstreamID)
}

// compensateOrphan 在"上游创建成功、本地归属落库失败"时 best-effort 删除上游
// 对象，避免留下客户端再也无法通过 PBR 寻址的孤儿。
//
// 语义边界：
//   - 只是补偿，不是重试；删除失败只记 warning，绝不把 500 改成成功；
//   - 使用后台 context + 超时，客户端断开也继续尝试清理；
//   - 复用渠道代理与 channel key，但不经过 route.State，不影响成员冷却/熔断；
//   - batch 同样发 DELETE /batches/{id}；上游若不支持该方法，记录状态码后放弃。
func (h *Handler) compensateOrphan(c *gin.Context, sel *selection, kind, upstreamID string) {
	if sel == nil || sel.channel == nil || upstreamID == "" {
		return
	}
	var relPath string
	switch kind {
	case model.PBRAsyncObjectKindFile:
		relPath = "/files/" + url.PathEscape(upstreamID)
	case model.PBRAsyncObjectKindBatch:
		relPath = "/batches/" + url.PathEscape(upstreamID)
	default:
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), orphanCompensationTimeout)
	defer cancel()
	target, err := buildUpstreamURL(sel.channel, relPath, "")
	if err != nil {
		logger.LogWarn(c, fmt.Sprintf(
			"pbr async: orphan compensation skipped kind=%s id=%s channel_id=%d: %s",
			kind, upstreamID, sel.channel.Id, kitutil.MaskSensitiveInfo(err.Error())))
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, target, nil)
	if err != nil {
		logger.LogWarn(c, fmt.Sprintf(
			"pbr async: orphan compensation request failed kind=%s id=%s channel_id=%d: %s",
			kind, upstreamID, sel.channel.Id, kitutil.MaskSensitiveInfo(err.Error())))
		return
	}
	applyUpstreamHeaders(req, c, sel)
	// DELETE 没有请求体，不把原始上传的 multipart Content-Type 带给上游。
	req.Header.Del("Content-Type")

	settings := sel.channel.GetSetting()
	client, err := service.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil || client == nil {
		logger.LogWarn(c, fmt.Sprintf(
			"pbr async: orphan compensation client failed kind=%s id=%s channel_id=%d: %v",
			kind, upstreamID, sel.channel.Id, err))
		return
	}
	// 缓存客户端禁止原地修改重定向策略。
	relayClient := *client
	relayClient.CheckRedirect = keepUpstreamRedirectResponse
	resp, err := relayClient.Do(req)
	if err != nil {
		logger.LogWarn(c, fmt.Sprintf(
			"pbr async: orphan compensation failed kind=%s id=%s channel_id=%d: %s",
			kind, upstreamID, sel.channel.Id, kitutil.MaskSensitiveInfo(err.Error())))
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		logger.LogWarn(c, fmt.Sprintf(
			"pbr async: orphan compensation returned status=%d kind=%s id=%s channel_id=%d",
			resp.StatusCode, kind, upstreamID, sel.channel.Id))
		return
	}
	logger.LogInfo(c, fmt.Sprintf(
		"pbr async: compensated orphan kind=%s id=%s channel_id=%d",
		kind, upstreamID, sel.channel.Id))
}
