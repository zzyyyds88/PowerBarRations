package async

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
)

// Batch 端点（相对 /v1）：POST/GET /batches、GET /batches/:id、
// POST /batches/:id/cancel。全部要求 X-PBR-Lane（ADR 0010）。

func (h *Handler) handleCreateBatch(c *gin.Context) {
	log := newAsyncLog(model.PBRAsyncObjectKindBatch)
	defer log.finish(c)

	laneName, ok := h.requireLane(c, log)
	if !ok {
		return
	}

	limit := h.batchJSONLimit()
	if c.Request.ContentLength > limit {
		payloadTooLarge(c, log, limit)
		return
	}
	body, err := readBodyLimited(c.Request.Body, limit)
	if err != nil {
		if errors.Is(err, errPayloadTooLarge) {
			payloadTooLarge(c, log, limit)
			return
		}
		log.fail(string(route.KindClientError), "read batch request body failed")
		apierr.Write(c, http.StatusBadRequest, apierr.CodeInvalidRequest,
			"batch request body could not be read", "")
		return
	}
	if c.Request.Header.Get("Content-Type") == "" {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	if !h.checkBatchInputFileLane(c, log, laneName, body) {
		return
	}

	sel, ok := h.chooseChannel(c, laneName, log)
	if !ok {
		return
	}
	defer sel.state.ReleaseProbe()

	started := time.Now()
	resp, apiErr := h.doUpstream(c, sel, http.MethodPost, "/batches", bytes.NewReader(body), int64(len(body)))
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
	payload, err := parseAsyncObjectPayload(data)
	if err != nil || payload == nil || payload.ID == "" {
		h.handleInvalidCreatedObject(c, log, sel, "upstream batch response is missing a valid id", durationMs)
		return
	}

	h.recordSuccess(sel, log, durationMs)

	obj := &model.PBRAsyncObject{
		Kind:       model.PBRAsyncObjectKindBatch,
		Lane:       laneName,
		UpstreamId: payload.ID,
		ChannelId:  sel.channel.Id,
		Status:     payload.Status,
		CreatedAt:  objectCreatedAt(payload),
		ExpiresAt:  int64(payload.ExpiresAt),
	}
	if err := model.CreateAsyncObject(obj); err != nil {
		h.compensateOrphanUnlessRecorded(c, sel, laneName, model.PBRAsyncObjectKindBatch, payload.ID)
		log.fail(string(route.KindSoftTransient), "record async object failed")
		apierr.Write(c, http.StatusInternalServerError, CodeAsyncObjectRecordFailed,
			"batch was created upstream but its lane mapping could not be recorded", "")
		return
	}

	writeBufferedResponse(c, resp, data, "application/json")
}

func (h *Handler) handleListBatches(c *gin.Context) {
	log := newAsyncLog(model.PBRAsyncObjectKindBatch)
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
	resp, apiErr := h.doUpstream(c, sel, http.MethodGet, "/batches", nil, 0)
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
	h.handleListResponse(c, log, sel, resp, model.PBRAsyncObjectKindBatch, laneName, durationMs)
}

func (h *Handler) handleGetBatch(c *gin.Context) {
	log := newAsyncLog(model.PBRAsyncObjectKindBatch)
	defer log.finish(c)

	laneName, ok := h.requireLane(c, log)
	if !ok {
		return
	}
	id := c.Param("id")
	sel, _, ok := h.resolveObject(c, log, laneName, model.PBRAsyncObjectKindBatch, id)
	if !ok {
		return
	}

	started := time.Now()
	resp, apiErr := h.doUpstream(c, sel, http.MethodGet, "/batches/"+url.PathEscape(id), nil, 0)
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
		update := model.PBRAsyncObjectUpdate{Status: payload.Status}
		if payload.ExpiresAt > 0 {
			expiresAt := int64(payload.ExpiresAt)
			update.ExpiresAt = &expiresAt
		}
		_ = model.UpdateAsyncObjectMeta(laneName, model.PBRAsyncObjectKindBatch, id, update)
	}

	writeBufferedResponse(c, resp, data, "application/json")
}

func (h *Handler) handleCancelBatch(c *gin.Context) {
	log := newAsyncLog(model.PBRAsyncObjectKindBatch)
	defer log.finish(c)

	laneName, ok := h.requireLane(c, log)
	if !ok {
		return
	}
	id := c.Param("id")
	sel, _, ok := h.resolveObject(c, log, laneName, model.PBRAsyncObjectKindBatch, id)
	if !ok {
		return
	}

	started := time.Now()
	resp, apiErr := h.doUpstream(c, sel, http.MethodPost, "/batches/"+url.PathEscape(id)+"/cancel", nil, 0)
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
		update := model.PBRAsyncObjectUpdate{Status: payload.Status}
		_ = model.UpdateAsyncObjectMeta(laneName, model.PBRAsyncObjectKindBatch, id, update)
	}

	writeBufferedResponse(c, resp, data, "application/json")
}

// checkBatchInputFileLane 校验 batch 的 input_file_id 归属。
//
// 规则（ADR 0010 的跨车道隔离）：
//   - pbr_async_objects 中已有该 file 的记录，且记录里没有当前车道 →
//     404 async_object_not_found，不把请求发给上游；
//   - PBR 无记录（外部预存文件）→ 放行，由上游校验文件是否存在/可用；
//   - 记录里包含当前车道 → 放行（即使同一 ID 也被别的车道记录过）。
//
// 只做车道归属，不校验记录渠道是否等于本次选中的渠道：车道成员变化后，上游
// 可能确实找不到文件，此时由上游的 4xx 如实返回。
func (h *Handler) checkBatchInputFileLane(c *gin.Context, log *asyncLog, laneName string, body []byte) bool {
	fileID := parseBatchInputFileID(body)
	if fileID == "" {
		// 没有 input_file_id，或请求体不是可解析的 JSON：交给上游校验。
		return true
	}
	owners, err := model.GetAsyncObjectOwners(model.PBRAsyncObjectKindFile, []string{fileID})
	if err != nil {
		log.fail(string(route.KindSoftTransient), "read async object ownership failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"read async object ownership failed", "")
		return false
	}
	lanes := owners[fileID]
	if len(lanes) == 0 {
		// 外部预存文件：PBR 无法证明它属于任何车道，按契约放行。
		return true
	}
	for _, lane := range lanes {
		if lane == laneName {
			return true
		}
	}
	log.fail(string(route.KindClientError), "input_file_id belongs to another lane: "+fileID)
	apierr.Write(c, http.StatusNotFound, CodeAsyncObjectNotFound,
		fmt.Sprintf("async file '%s' was not created by lane '%s'", fileID, laneName),
		"async objects are only visible in the lane that created them")
	return false
}

func parseBatchInputFileID(body []byte) string {
	var payload struct {
		InputFileID string `json:"input_file_id"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.InputFileID)
}
