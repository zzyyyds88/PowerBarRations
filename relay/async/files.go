package async

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
)

// Files 端点（相对 /v1）：GET/POST /files、GET/DELETE /files/:id、
// GET /files/:id/content。全部要求 X-PBR-Lane（ADR 0010）。

func (h *Handler) handleCreateFile(c *gin.Context) {
	// Anthropic Files 语义由网关自己暂存字节（ADR 0011）：不要求车道、不选路、
	// 不转发上游。分流必须在 requireLane 之前，否则永远拿不到 400 以外的结果。
	if h.dispatchAnthropicFiles(c) {
		return
	}

	log := newAsyncLog(model.PBRAsyncObjectKindFile)
	defer log.finish(c)

	laneName, ok := h.requireLane(c, log)
	if !ok {
		return
	}
	sel, ok := h.chooseChannel(c, laneName, log)
	if !ok {
		return
	}
	defer sel.state.ReleaseProbe()

	limit := h.uploadLimit()
	if c.Request.ContentLength > limit {
		payloadTooLarge(c, log, limit)
		return
	}
	body := newLimitBody(c.Request.Body, limit)
	defer body.Close()

	started := time.Now()
	resp, apiErr := h.doUpstream(c, sel, http.MethodPost, "/files", body, -1)
	durationMs := time.Since(started).Milliseconds()
	if body.Exceeded() {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		payloadTooLarge(c, log, limit)
		return
	}
	if apiErr != nil {
		h.handleTransportError(c, log, sel, apiErr, durationMs)
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		h.handleUpstreamHTTPError(c, log, sel, resp, durationMs)
		return
	}

	data, err := h.readJSONResponse(resp)
	if err != nil {
		h.handleResponseReadError(c, log, sel, err, durationMs)
		return
	}
	payload, err := parseAsyncObjectPayload(data)
	if err != nil || payload == nil || payload.ID == "" {
		h.handleInvalidCreatedObject(c, log, sel, "upstream file response is missing a valid id", durationMs)
		return
	}

	h.recordSuccess(sel, log, durationMs)

	size := int64(0)
	if payload.Bytes != nil {
		size = *payload.Bytes
	}
	obj := &model.PBRAsyncObject{
		Kind:       model.PBRAsyncObjectKindFile,
		Lane:       laneName,
		UpstreamId: payload.ID,
		ChannelId:  sel.channel.Id,
		Status:     payload.Status,
		Size:       size,
		CreatedAt:  objectCreatedAt(payload),
		ExpiresAt:  int64(payload.ExpiresAt),
	}
	if err := model.CreateAsyncObject(obj); err != nil {
		h.compensateOrphanUnlessRecorded(c, sel, laneName, model.PBRAsyncObjectKindFile, payload.ID)
		log.fail(string(route.KindSoftTransient), "record async object failed")
		apierr.Write(c, http.StatusInternalServerError, CodeAsyncObjectRecordFailed,
			"file was created upstream but its lane mapping could not be recorded", "")
		return
	}

	writeBufferedResponse(c, resp, data, "application/json")
}

func (h *Handler) handleListFiles(c *gin.Context) {
	// Anthropic Files 语义由网关自己暂存字节（ADR 0011）：不要求车道、不选路、
	// 不转发上游。分流必须在 requireLane 之前，否则永远拿不到 400 以外的结果。
	if h.dispatchAnthropicFiles(c) {
		return
	}

	log := newAsyncLog(model.PBRAsyncObjectKindFile)
	defer log.finish(c)

	laneName, ok := h.requireLane(c, log)
	if !ok {
		return
	}
	sel, ok := h.chooseChannel(c, laneName, log)
	if !ok {
		return
	}
	defer sel.state.ReleaseProbe()

	started := time.Now()
	resp, apiErr := h.doUpstream(c, sel, http.MethodGet, "/files", nil, 0)
	durationMs := time.Since(started).Milliseconds()
	if apiErr != nil {
		h.handleTransportError(c, log, sel, apiErr, durationMs)
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		h.handleUpstreamHTTPError(c, log, sel, resp, durationMs)
		return
	}
	if resp.ContentLength > h.uploadLimit() {
		resp.Body.Close()
		payloadTooLarge(c, log, h.uploadLimit())
		return
	}
	h.handleListResponse(c, log, sel, resp, model.PBRAsyncObjectKindFile, laneName, durationMs)
}

func (h *Handler) handleGetFile(c *gin.Context) {
	// Anthropic Files 语义由网关自己暂存字节（ADR 0011）：不要求车道、不选路、
	// 不转发上游。分流必须在 requireLane 之前，否则永远拿不到 400 以外的结果。
	if h.dispatchAnthropicFiles(c) {
		return
	}

	log := newAsyncLog(model.PBRAsyncObjectKindFile)
	defer log.finish(c)

	laneName, ok := h.requireLane(c, log)
	if !ok {
		return
	}
	id := c.Param("id")
	sel, _, ok := h.resolveObject(c, log, laneName, model.PBRAsyncObjectKindFile, id)
	if !ok {
		return
	}

	started := time.Now()
	resp, apiErr := h.doUpstream(c, sel, http.MethodGet, "/files/"+url.PathEscape(id), nil, 0)
	durationMs := time.Since(started).Milliseconds()
	if apiErr != nil {
		h.handleTransportError(c, log, sel, apiErr, durationMs)
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		h.handleUpstreamHTTPError(c, log, sel, resp, durationMs)
		return
	}

	data, err := h.readJSONResponse(resp)
	if err != nil {
		h.handleResponseReadError(c, log, sel, err, durationMs)
		return
	}
	h.recordSuccess(sel, log, durationMs)

	if payload, parseErr := parseAsyncObjectPayload(data); parseErr == nil {
		update := model.PBRAsyncObjectUpdate{Status: payload.Status, Size: payload.Bytes}
		if payload.ExpiresAt > 0 {
			expiresAt := int64(payload.ExpiresAt)
			update.ExpiresAt = &expiresAt
		}
		_ = model.UpdateAsyncObjectMeta(laneName, model.PBRAsyncObjectKindFile, id, update)
	}

	writeBufferedResponse(c, resp, data, "application/json")
}

func (h *Handler) handleDeleteFile(c *gin.Context) {
	// Anthropic Files 语义由网关自己暂存字节（ADR 0011）：不要求车道、不选路、
	// 不转发上游。分流必须在 requireLane 之前，否则永远拿不到 400 以外的结果。
	if h.dispatchAnthropicFiles(c) {
		return
	}

	log := newAsyncLog(model.PBRAsyncObjectKindFile)
	defer log.finish(c)

	laneName, ok := h.requireLane(c, log)
	if !ok {
		return
	}
	id := c.Param("id")
	sel, _, ok := h.resolveObject(c, log, laneName, model.PBRAsyncObjectKindFile, id)
	if !ok {
		return
	}

	started := time.Now()
	resp, apiErr := h.doUpstream(c, sel, http.MethodDelete, "/files/"+url.PathEscape(id), nil, 0)
	durationMs := time.Since(started).Milliseconds()
	if apiErr != nil {
		h.handleTransportError(c, log, sel, apiErr, durationMs)
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		h.handleUpstreamHTTPError(c, log, sel, resp, durationMs)
		return
	}

	data, err := h.readJSONResponse(resp)
	if err != nil {
		h.handleResponseReadError(c, log, sel, err, durationMs)
		return
	}
	h.recordSuccess(sel, log, durationMs)

	status := "deleted"
	if payload, parseErr := parseAsyncObjectPayload(data); parseErr == nil && payload.Status != "" {
		status = payload.Status
	}
	// 已删除对象不会再被上游使用：把过期时间设为当前时刻，让元数据按
	// "过期后再保留 30 天"的统一规则被清理，而不是永久留在表里。
	deletedAt := time.Now().Unix()
	_ = model.UpdateAsyncObjectMeta(laneName, model.PBRAsyncObjectKindFile, id,
		model.PBRAsyncObjectUpdate{Status: status, ExpiresAt: &deletedAt})

	writeBufferedResponse(c, resp, data, "application/json")
}

func (h *Handler) handleFileContent(c *gin.Context) {
	// Anthropic Files 语义由网关自己暂存字节（ADR 0011）：不要求车道、不选路、
	// 不转发上游。分流必须在 requireLane 之前，否则永远拿不到 400 以外的结果。
	if h.dispatchAnthropicFiles(c) {
		return
	}

	log := newAsyncLog(model.PBRAsyncObjectKindFile)
	defer log.finish(c)

	laneName, ok := h.requireLane(c, log)
	if !ok {
		return
	}
	id := c.Param("id")
	sel, _, ok := h.resolveObject(c, log, laneName, model.PBRAsyncObjectKindFile, id)
	if !ok {
		return
	}

	started := time.Now()
	resp, apiErr := h.doUpstream(c, sel, http.MethodGet, "/files/"+url.PathEscape(id)+"/content", nil, 0)
	durationMs := time.Since(started).Milliseconds()
	if apiErr != nil {
		h.handleTransportError(c, log, sel, apiErr, durationMs)
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		h.handleUpstreamHTTPError(c, log, sel, resp, durationMs)
		return
	}
	if resp.ContentLength > h.uploadLimit() {
		resp.Body.Close()
		payloadTooLarge(c, log, h.uploadLimit())
		return
	}
	h.recordSuccess(sel, log, durationMs)
	if err := streamResponse(c, resp, "application/octet-stream", id, h.uploadLimit()); err != nil {
		log.fail(string(route.KindSoftTransient), err.Error())
	}
}

func (h *Handler) handleResponseReadError(c *gin.Context, log *asyncLog, sel *selection, err error, durationMs int64) {
	kind := route.KindBadResponse
	summary := "upstream response could not be read"
	code := apierr.CodeUpstreamError
	if errors.Is(err, errResponseTooLarge) {
		code = CodeAsyncResponseTooLarge
		summary = "upstream response exceeds the async metadata size limit"
	}
	log.fail(string(kind), summary)
	h.recordUpstreamFailure(sel, log, kind, summary, durationMs)
	apierr.Write(c, http.StatusBadGateway, code, summary, "")
}

func (h *Handler) handleInvalidCreatedObject(c *gin.Context, log *asyncLog, sel *selection, summary string, durationMs int64) {
	kind := route.KindBadResponse
	log.fail(string(kind), summary)
	h.recordUpstreamFailure(sel, log, kind, summary, durationMs)
	apierr.Write(c, http.StatusBadGateway, CodeAsyncObjectIdMissing, summary,
		"the upstream did not return an object id; the object cannot be addressed through this gateway")
}
