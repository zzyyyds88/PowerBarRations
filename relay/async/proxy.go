package async

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/relayconvert/kitutil"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

var (
	errPayloadTooLarge  = errors.New("async payload too large")
	errResponseTooLarge = errors.New("async response too large")
)

// ---------- 请求体上限 ----------

// limitBody 在读取时计数并在超过上限时返回 errPayloadTooLarge。
// 用于不可缓冲的 multipart 上传：不落盘、不整体读入内存。
type limitBody struct {
	rc        io.ReadCloser
	remaining int64
	exceeded  bool
}

func newLimitBody(rc io.ReadCloser, limit int64) *limitBody {
	if limit <= 0 {
		limit = DefaultMaxUploadBytes
	}
	return &limitBody{rc: rc, remaining: limit}
}

func (b *limitBody) Read(p []byte) (int, error) {
	if b == nil || b.rc == nil {
		return 0, io.EOF
	}
	if b.exceeded {
		return 0, errPayloadTooLarge
	}
	if b.remaining < 0 {
		b.exceeded = true
		return 0, errPayloadTooLarge
	}
	// 多读 1 字节用于判定"恰好等于上限"与"超过上限"。
	maxRead := b.remaining + 1
	if int64(len(p)) > maxRead {
		p = p[:maxRead]
	}
	n, err := b.rc.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		b.exceeded = true
		return 0, errPayloadTooLarge
	}
	if err != nil {
		// 全局请求体中间件（DecompressRequestMiddleware）会用 http.MaxBytesReader
		// 施加一个更低的部署级上限（MAX_REQUEST_BODY_MB）。它触发的错误同样属于
		// "请求体过大"，不能降级成上游传输失败。
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			b.exceeded = true
			return n, errPayloadTooLarge
		}
	}
	return n, err
}

func (b *limitBody) Close() error {
	if b == nil || b.rc == nil {
		return nil
	}
	return b.rc.Close()
}

func (b *limitBody) Exceeded() bool { return b != nil && b.exceeded }

// readBodyLimited 把小请求体（batch JSON）读入内存并执行上限。
func readBodyLimited(rc io.ReadCloser, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = DefaultMaxBatchJSONBytes
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errPayloadTooLarge
		}
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errPayloadTooLarge
	}
	return data, nil
}

// ---------- 响应转发 ----------

// forwardedResponseHeaders 只转发无状态、与实体语义相关的头。
// 不转发 Set-Cookie / WWW-Authenticate / Content-Encoding / 逐跳头：
// 异步对象不是登录会话，上游的挑战与压缩头不应泄漏给下游。
var forwardedResponseHeaders = []string{
	"Content-Type",
	"Content-Disposition",
	"Location",
	"ETag",
	"Last-Modified",
	"Cache-Control",
	"OpenAI-Processing-Ms",
	"OpenAI-Version",
	"X-Request-Id",
	"Retry-After",
	"X-Content-Type-Options",
}

func copyResponseHeaders(c *gin.Context, resp *http.Response, defaultContentType string) {
	for _, name := range forwardedResponseHeaders {
		if values := resp.Header.Values(name); len(values) > 0 {
			c.Writer.Header()[name] = append([]string(nil), values...)
		}
	}
	if defaultContentType != "" && c.Writer.Header().Get("Content-Type") == "" {
		c.Writer.Header().Set("Content-Type", defaultContentType)
	}
}

// streamResponse 把上游响应按流写出，不缓冲、不落盘。limit > 0 时对
// 未知 Content-Length 的响应也施加字节上限（超出后截断，避免无界读取）。
func streamResponse(c *gin.Context, resp *http.Response, defaultContentType, defaultFilename string, limit int64) error {
	if resp == nil {
		return errors.New("nil upstream response")
	}
	if limit > 0 && resp.ContentLength > limit {
		return errPayloadTooLarge
	}
	defer resp.Body.Close()
	copyResponseHeaders(c, resp, defaultContentType)
	if defaultFilename != "" && c.Writer.Header().Get("Content-Disposition") == "" {
		c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(defaultFilename)+`"`)
	}
	if resp.ContentLength >= 0 {
		c.Writer.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	c.Writer.WriteHeader(resp.StatusCode)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	var src io.Reader = resp.Body
	if limit > 0 {
		src = io.LimitReader(resp.Body, limit)
	}
	return copyStream(c, src)
}

func copyStream(c *gin.Context, src io.Reader) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			c.Writer.Flush()
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// writeUpstreamError 原样转发上游错误状态与响应体（含流式），同时截取前缀
// 作为日志摘要。返回的摘要已经过脱敏，调用方不得把完整报文写入日志。
func writeUpstreamError(c *gin.Context, resp *http.Response) string {
	if resp == nil {
		return ""
	}
	defer resp.Body.Close()
	copyResponseHeaders(c, resp, "")
	// 只有完整可转发（未截断）的错误体才声明 Content-Length；否则用 chunked，
	// 避免客户端按声明长度等待一个永远不会到来的尾部。
	if resp.ContentLength >= 0 && resp.ContentLength <= maxForwardedErrorBytes {
		c.Writer.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	c.Writer.WriteHeader(resp.StatusCode)

	var summary strings.Builder
	buf := make([]byte, 32*1024)
	remaining := int64(maxForwardedErrorBytes)
	for remaining > 0 {
		readSize := len(buf)
		if int64(readSize) > remaining {
			readSize = int(remaining)
		}
		n, err := resp.Body.Read(buf[:readSize])
		if n > 0 {
			remaining -= int64(n)
			if summary.Len() < maxErrorSummaryBytes {
				summaryRemaining := maxErrorSummaryBytes - summary.Len()
				if n < summaryRemaining {
					summary.Write(buf[:n])
				} else {
					summary.Write(buf[:summaryRemaining])
				}
			}
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				return kitutil.MaskSensitiveInfo(strings.TrimSpace(summary.String()))
			}
			c.Writer.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}
	return kitutil.MaskSensitiveInfo(strings.TrimSpace(summary.String()))
}

// readJSONResponse 读取需要解析的上游 JSON 响应。响应体只进内存、不落盘；
// 超过上限按上游错误处理。
func (h *Handler) readJSONResponse(resp *http.Response) ([]byte, error) {
	if resp == nil {
		return nil, errors.New("nil upstream response")
	}
	defer resp.Body.Close()
	limit := h.jsonResponseLimit()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errResponseTooLarge
	}
	return data, nil
}

func writeBufferedResponse(c *gin.Context, resp *http.Response, body []byte, defaultContentType string) {
	copyResponseHeaders(c, resp, defaultContentType)
	c.Writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	c.Writer.WriteHeader(resp.StatusCode)
	if len(body) > 0 {
		_, _ = c.Writer.Write(body)
	}
}

func safeFilename(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, value)
	cleaned = strings.Trim(cleaned, "-.")
	if cleaned == "" {
		return "file"
	}
	return cleaned
}

// ---------- 上游对象 JSON ----------

// asyncEpoch 兼容 Unix 秒（数字/数字字符串）与 RFC3339 字符串；无法解析按 0
// 处理（上游未声明过期时间）。异步元数据解析失败不应让整个响应不可用。
type asyncEpoch int64

func (e *asyncEpoch) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*e = 0
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			*e = 0
			return nil
		}
		text = strings.TrimSpace(text)
		if text == "" {
			*e = 0
			return nil
		}
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			*e = asyncEpoch(n)
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			*e = asyncEpoch(parsed.Unix())
			return nil
		}
		*e = 0
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		*e = 0
		return nil
	}
	*e = asyncEpoch(n)
	return nil
}

type asyncObjectPayload struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	Bytes     *int64     `json:"bytes"`
	CreatedAt asyncEpoch `json:"created_at"`
	ExpiresAt asyncEpoch `json:"expires_at"`
	Deleted   bool       `json:"deleted"`
}

func parseAsyncObjectPayload(body []byte) (*asyncObjectPayload, error) {
	if len(body) == 0 {
		return nil, errors.New("empty response")
	}
	var payload asyncObjectPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ---------- 结果记账 ----------

func (h *Handler) recordSuccess(sel *selection, log *asyncLog, durationMs int64) {
	if sel != nil && sel.state != nil {
		sel.state.OnSuccess()
	}
	attempts := []model.PBRAttempt{}
	if sel != nil && sel.state != nil {
		attempts = sel.state.LogAttempts()
	}
	attempts = append(attempts, model.PBRAttempt{
		AttemptNum: len(attempts) + 1,
		Member:     sel.attemptMember(log.kind),
		Status:     "success",
		DurationMs: durationMs,
	})
	log.attempts = attempts
}

// recordUpstreamFailure 记一次真实的上游失败。
//
// internal/route 只暴露 ReportMemberUnavailable（按 soft_transient 记冷却/熔断），
// 这里对 upstream_reject / client_error / canceled 不调用它——routing-spec §4.1
// 明确这三类不得冷却成员。其余类型用它把失败计入成员运行态。
func (h *Handler) recordUpstreamFailure(sel *selection, log *asyncLog, kind route.ErrorKind, summary string, durationMs int64) {
	attempts := []model.PBRAttempt{}
	if sel != nil && sel.state != nil {
		attempts = sel.state.LogAttempts()
	}
	shouldRecordHealth := sel != nil && sel.state != nil &&
		kind != route.KindUpstreamReject && kind != route.KindClientError && kind != route.KindCanceled
	if shouldRecordHealth {
		sel.state.ReportMemberUnavailable(sel.member, summary)
		attempts = sel.state.LogAttempts()
		if len(attempts) > 0 {
			last := &attempts[len(attempts)-1]
			last.DurationMs = durationMs
			last.ErrorKind = string(kind)
			last.Msg = truncate(summary, 512)
		}
	} else {
		attempts = append(attempts, model.PBRAttempt{
			AttemptNum: len(attempts) + 1,
			Member:     sel.attemptMember(log.kind),
			Status:     "failed",
			DurationMs: durationMs,
			ErrorKind:  string(kind),
			Msg:        truncate(summary, 512),
		})
	}
	log.attempts = attempts
}

// handleTransportError 上游连接/传输层失败：只打了一个成员，直接返回 502。
func (h *Handler) handleTransportError(c *gin.Context, log *asyncLog, sel *selection, apiErr *types.NewAPIError, durationMs int64) {
	if apiErr == nil {
		apiErr = types.NewError(errors.New("upstream request failed"), types.ErrorCodeDoRequestFailed)
	}
	kind := route.Classify(apiErr)
	summary := apiErr.MaskSensitiveError()
	if c.Request.Context().Err() != nil {
		log.fail(string(route.KindCanceled), summary)
		c.Status(statusClientClosedRequest)
		return
	}
	log.fail(string(kind), summary)
	h.recordUpstreamFailure(sel, log, kind, summary, durationMs)
	apierr.Write(c, http.StatusBadGateway, apierr.CodeUpstreamError,
		"upstream request failed", "")
}

// handleUpstreamHTTPError 上游返回非 2xx：状态码与响应体原样转发。
func (h *Handler) handleUpstreamHTTPError(c *gin.Context, log *asyncLog, sel *selection, resp *http.Response, durationMs int64) {
	summary := writeUpstreamError(c, resp)
	if summary == "" {
		summary = resp.Status
	}
	apiErr := types.NewError(errors.New(summary), types.ErrorCodeBadResponseStatusCode,
		types.ErrOptionWithStatusCode(resp.StatusCode))
	kind := route.Classify(apiErr)
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		// 3xx 只是上游让我们跳转，不是成员故障；网关不跟随，也不因此冷却成员。
		kind = route.KindUpstreamReject
	}
	log.fail(string(kind), summary)
	h.recordUpstreamFailure(sel, log, kind, summary, durationMs)
}

// payloadTooLarge 统一的 413 收尾。它不冷却成员：请求体过大是客户端问题。
func payloadTooLarge(c *gin.Context, log *asyncLog, limit int64) {
	log.fail(string(route.KindClientError), "async payload exceeds limit")
	apierr.Write(c, http.StatusRequestEntityTooLarge, CodeAsyncPayloadTooLarge,
		"request body exceeds the async request limit",
		"limit is "+strconv.FormatInt(limit, 10)+" bytes")
}

func objectCreatedAt(payload *asyncObjectPayload) int64 {
	if payload == nil {
		return time.Now().Unix()
	}
	if payload.CreatedAt > 0 {
		return int64(payload.CreatedAt)
	}
	return time.Now().Unix()
}
