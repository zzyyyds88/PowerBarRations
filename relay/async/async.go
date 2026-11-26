// Package async 实现 OpenAI 兼容的异步对象（Files / Batch）入口。
//
// 语义真源：docs/adr/0010-async-objects-lane-header.md、docs/design-v1.md §4.2、
// docs/routing-spec-v1.md §4.5。
//
// 与同步请求的区别只有两点：
//   - 路由键不是 model，而是显式请求头 X-PBR-Lane；
//   - 创建成功即固定到某个渠道，后续 retrieve / content / cancel 不换渠道、
//     不 failover、不重试。
//
// 本包只做协议映射与流代理；渠道/成员选择、冷却、熔断、人工停用过滤仍复用
// internal/route 的同一套运行态。
package async

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"

	"gorm.io/gorm"
)

const (
	// HeaderLane 异步对象的显式路由键。该头只在本网关消费，不转发上游。
	HeaderLane = "X-PBR-Lane"

	// 稳定错误码（ADR 0010 / design §4.2）。code 不随文案变化。
	CodeLaneHeaderRequired            = "lane_header_required"
	CodeLaneNotFound                  = "lane_not_found"
	CodeAsyncObjectNotFound           = "async_object_not_found"
	CodeAsyncObjectChannelUnavailable = "async_object_channel_unavailable"
	CodeUnsupportedProtocolForAsync   = "unsupported_protocol_for_async"
	CodeAsyncPayloadTooLarge          = "async_payload_too_large"
	CodeAsyncResponseTooLarge         = "async_response_too_large"
	CodeAsyncListUnfilterable         = "async_list_unfilterable"
	CodeAsyncObjectIdMissing          = "async_object_id_missing"
	CodeAsyncObjectRecordFailed       = "async_object_record_failed"

	// DefaultMaxUploadBytes 单请求（上传/下载）上限 512 MiB。
	DefaultMaxUploadBytes int64 = 512 << 20
	// DefaultMaxBatchJSONBytes batch JSON 上限 8 MiB。
	DefaultMaxBatchJSONBytes int64 = 8 << 20
	// DefaultMaxJSONResponseBytes 需要解析的 JSON 响应上限。响应体本身不落盘；
	// 超过该上限说明上游返回了异常大的元数据，按上游错误处理。
	DefaultMaxJSONResponseBytes int64 = 4 << 20

	// maxErrorSummaryBytes 上游错误摘要只保留前缀，避免把整个错误体写进日志。
	maxErrorSummaryBytes = 64 << 10
	// maxForwardedErrorBytes 上游错误体转发上限。错误体不是业务载荷，超过该值
	// 直接截断并关闭上游连接，避免异常巨大的错误响应占用无界带宽。
	maxForwardedErrorBytes = 8 << 20

	// statusClientClosedRequest 非标准但常用的"客户端已断开"状态码，仅用于日志。
	statusClientClosedRequest = 499

	// inboundFormatAsync 日志里的入站协议标记。上游仍是 OpenAI 协议；
	// 用固定值避免把 Files/Batch 误标成 chat。
	inboundFormatAsync = "openai"
)

// Handler 持有可注入的上限，便于测试与后续按部署配置收紧。
// 零值 Handler 使用默认上限（NewHandler 只是显式表达）。
type Handler struct {
	// UploadLimit 单请求上传/下载上限（字节）。
	UploadLimit int64
	// BatchJSONLimit POST /batches 请求体上限（字节）。
	BatchJSONLimit int64
	// JSONResponseLimit 需要解析的上游 JSON 响应上限（字节）。
	JSONResponseLimit int64
}

// NewHandler 返回使用合同默认上限的 Handler。
func NewHandler() *Handler {
	return &Handler{
		UploadLimit:       DefaultMaxUploadBytes,
		BatchJSONLimit:    DefaultMaxBatchJSONBytes,
		JSONResponseLimit: DefaultMaxJSONResponseBytes,
	}
}

// RegisterRoutes 注册 Files / Batch 路由。调用方传入已带 /v1 前缀与鉴权中间件的
// gin.RouterGroup（G5 接线）；本函数不重复挂鉴权。
func RegisterRoutes(r *gin.RouterGroup) {
	NewHandler().RegisterRoutes(r)
}

// RegisterRoutes 在给定 group 上注册全部异步对象端点。
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	if r == nil {
		return
	}
	r.GET("/files", h.handleListFiles)
	r.POST("/files", h.handleCreateFile)
	r.GET("/files/:id/content", h.handleFileContent)
	r.GET("/files/:id", h.handleGetFile)
	r.DELETE("/files/:id", h.handleDeleteFile)
	r.GET("/batches", h.handleListBatches)
	r.POST("/batches", h.handleCreateBatch)
	r.GET("/batches/:id", h.handleGetBatch)
	r.POST("/batches/:id/cancel", h.handleCancelBatch)
}

func (h *Handler) uploadLimit() int64 {
	if h != nil && h.UploadLimit > 0 {
		return h.UploadLimit
	}
	return DefaultMaxUploadBytes
}

func (h *Handler) batchJSONLimit() int64 {
	if h != nil && h.BatchJSONLimit > 0 {
		return h.BatchJSONLimit
	}
	return DefaultMaxBatchJSONBytes
}

func (h *Handler) jsonResponseLimit() int64 {
	if h != nil && h.JSONResponseLimit > 0 {
		return h.JSONResponseLimit
	}
	return DefaultMaxJSONResponseBytes
}

// requireLane 解析并校验 X-PBR-Lane。缺失 → 400，未知车道 → 404。
// 车道存在但被停用/没有可用成员由 chooseChannel / resolveObject 按 503 处理。
func (h *Handler) requireLane(c *gin.Context, log *asyncLog) (string, bool) {
	laneName := strings.TrimSpace(c.GetHeader(HeaderLane))
	log.lane = laneName
	if laneName == "" {
		log.fail(string(route.KindClientError), "missing "+HeaderLane+" header")
		apierr.Write(c, http.StatusBadRequest, CodeLaneHeaderRequired,
			HeaderLane+" header is required for /v1/files and /v1/batches",
			"set "+HeaderLane+" to a configured lane name")
		return "", false
	}
	if _, err := model.GetLaneByName(laneName); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.fail(string(route.KindClientError), "lane not found: "+laneName)
			apierr.Write(c, http.StatusNotFound, CodeLaneNotFound,
				fmt.Sprintf("lane '%s' not found", laneName), "GET /api/v1/lanes")
			return "", false
		}
		log.fail(string(route.KindSoftTransient), "resolve lane failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"resolve lane failed", "")
		return "", false
	}
	log.laneResolved = true
	return laneName, true
}

// asyncLog 一次异步请求的日志载体，收尾时一次性写入 pbr_request_logs。
// 只记元数据：不记请求/响应正文，estimated_cost 恒为 0。
type asyncLog struct {
	lane         string
	laneResolved bool
	kind         string
	channel      *model.Channel
	channelId    int
	channelName  string
	errKind      string
	errSummary   string
	attempts     []model.PBRAttempt
}

func (l *asyncLog) setChannel(channel *model.Channel) {
	if channel == nil {
		return
	}
	l.channel = channel
	l.channelId = channel.Id
	l.channelName = channel.Name
}

func (l *asyncLog) fail(kind, summary string) {
	if kind != "" {
		l.errKind = kind
	}
	l.errSummary = strings.TrimSpace(summary)
}

func (l *asyncLog) finish(c *gin.Context) {
	carrier := model.EnsurePBRLogCarrier(c)
	status := c.Writer.Status()
	if status == 0 {
		status = http.StatusOK
	}
	carrier.Lane = l.lane
	carrier.RequestModel = l.lane
	if l.laneResolved {
		carrier.RouteSource = model.RouteSourceExplicit
	}
	carrier.InboundFormat = inboundFormatAsync
	if l.channel != nil {
		carrier.ChannelId = l.channel.Id
		carrier.ChannelName = l.channel.Name
	} else {
		carrier.ChannelId = l.channelId
		carrier.ChannelName = l.channelName
	}
	carrier.Success = status >= 200 && status < 300
	carrier.HTTPStatus = status
	if l.errKind != "" {
		carrier.ErrorKind = l.errKind
		carrier.ErrorSummary = truncate(l.errSummary, 2048)
	}
	carrier.Attempts = l.attempts
	carrier.TotalAttempts = len(l.attempts)
	// estimated_cost 不赋值：WritePBRLog 在没有 usage 时按 0 落库，
	// PriceBand 落兜底 -1（异步对象不参与计价）。
	model.WritePBRLog(c)
}

func newAsyncLog(kind string) *asyncLog {
	return &asyncLog{kind: kind}
}

func memberLabel(m *model.RouteMember) string {
	if m == nil {
		return ""
	}
	if m.Channel != "" {
		return m.Channel + "/" + m.Model
	}
	return m.Model
}

func truncate(value string, max int) string {
	return common.TruncateUTF8(value, max)
}
