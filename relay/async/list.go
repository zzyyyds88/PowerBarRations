package async

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
)

var errAsyncListUnfilterable = errors.New("async list response is not filterable")

// handleListResponse 读取、按车道过滤并写出 Files/Batch 列表响应。
//
// 上游列表是**渠道级**的：同一个渠道可能被多条车道共享，列表里会混入其它
// 车道创建的对象，甚至直接在上游创建、PBR 从未记录过的外部对象。因此这里
// 不能像内容下载那样原样流式透传，必须按 pbr_async_objects 做车道归属过滤。
//
// 过滤边界（ADR 0010 的跨车道隔离）：
//   - 只保留 (kind, lane, upstream_id) 在 pbr_async_objects 中命中的对象；
//   - 已知属于其它车道的对象剔除；
//   - PBR 无记录的对象无法证明属于本车道（可能是外部直接创建或另一节点创建），
//     按严格白名单剔除，宁可不列，也不跨车道泄漏。
//
// 分页边界：本函数只过滤**当前页**，不替客户端遍历上游分页。被剔除的项不会
// 触发展开下一页，`has_more` / `first_id` / `last_id` 保持上游原值，因此过滤后
// 的 data 可能稀疏，客户端必须按上游原分页参数继续翻页；本函数不承诺"单页即
// 完整车道视图"。
func (h *Handler) handleListResponse(c *gin.Context, log *asyncLog, sel *selection, resp *http.Response, kind, laneName string, durationMs int64) {
	data, err := h.readJSONResponse(resp)
	if err != nil {
		h.handleResponseReadError(c, log, sel, err, durationMs)
		return
	}
	filtered, err := filterAsyncListPayload(data, kind, laneName)
	if err != nil {
		if errors.Is(err, errAsyncListUnfilterable) {
			summary := "upstream list response is not filterable by lane"
			kind := route.KindBadResponse
			log.fail(string(kind), summary)
			h.recordUpstreamFailure(sel, log, kind, summary, durationMs)
			apierr.Write(c, http.StatusBadGateway, CodeAsyncListUnfilterable, summary,
				"the upstream list payload must be a JSON object with a data array")
			return
		}
		// 本地归属表查询失败：上游响应本身是好的，不能因此冷却成员。
		log.fail(string(route.KindSoftTransient), "filter async list by lane failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"filter async list by lane failed", "")
		return
	}

	h.recordSuccess(sel, log, durationMs)
	writeBufferedResponse(c, resp, filtered, "application/json")
}

func filterAsyncListPayload(data []byte, kind, laneName string) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, errAsyncListUnfilterable
	}
	dataRaw, ok := payload["data"]
	if !ok {
		return nil, errAsyncListUnfilterable
	}
	var items []json.RawMessage
	if err := json.Unmarshal(dataRaw, &items); err != nil {
		return nil, errAsyncListUnfilterable
	}

	itemIDs := make([]string, len(items))
	ids := make([]string, 0, len(items))
	for i, item := range items {
		var meta struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(item, &meta); err != nil {
			continue
		}
		itemIDs[i] = strings.TrimSpace(meta.ID)
		if itemIDs[i] != "" {
			ids = append(ids, itemIDs[i])
		}
	}

	owners, err := model.GetAsyncObjectOwners(kind, ids)
	if err != nil {
		return nil, err
	}

	filtered := make([]json.RawMessage, 0, len(items))
	for i, item := range items {
		id := itemIDs[i]
		if id == "" {
			// 没有 id 的列表项无法归属任何车道，按严格白名单剔除。
			continue
		}
		keep := false
		for _, owner := range owners[id] {
			if owner == laneName {
				keep = true
				break
			}
		}
		if keep {
			filtered = append(filtered, item)
		}
	}

	encoded, err := json.Marshal(filtered)
	if err != nil {
		return nil, errAsyncListUnfilterable
	}
	payload["data"] = encoded
	result, err := json.Marshal(payload)
	if err != nil {
		return nil, errAsyncListUnfilterable
	}
	return result, nil
}
