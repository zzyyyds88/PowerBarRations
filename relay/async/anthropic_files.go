package async

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/filestore"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/middleware"
	"github.com/zzyyyds88/PowerBarRations/model"
)

// Anthropic Files（ADR 0011）：`anthropic-beta` 含 `files-api-` 的 /v1/files*
// 由网关自己暂存字节，不要求 X-PBR-Lane、不参与选路、不转发上游。
//
// 依据：上传不产生模型调用，字节只被后续 /v1/messages 引用，真正选路的是那条
// messages 请求。因此这不违反 ADR 0005「车道是唯一路由入口」。
//
// 与 ADR 0010 的 OpenAI Files 语义按 beta 头分流，共用同一组路由。

const (
	// CodeAnthropicFileNotFound 归属不匹配与不存在同形，避免泄露对象存在性。
	CodeAnthropicFileNotFound = "file_not_found"
	// CodeAnthropicFilesQuotaExceeded 总量配额用尽。
	CodeAnthropicFilesQuotaExceeded = "files_quota_exceeded"
	// CodeAnthropicFileTooLarge 单文件超过上限。
	CodeAnthropicFileTooLarge = "file_too_large"
	// CodeAnthropicFileUnsupportedType MIME 不在白名单内。
	CodeAnthropicFileUnsupportedType = "unsupported_media_type_for_files"
	// CodeAnthropicFileInvalidRequest 请求本身不合法（缺 multipart、缺 file 字段等）。
	CodeAnthropicFileInvalidRequest = "invalid_request"

	// inboundFormatAnthropicFiles 日志里的入站标记。与 OpenAI 异步对象的
	// inboundFormatAsync 区分开，避免把两类端点混进同一条统计口径。
	inboundFormatAnthropicFiles = "anthropic"
)

// anthropicFilesBetaMarker 触发 Anthropic Files 语义的 beta 头片段。
const anthropicFilesBetaMarker = "files-api-"

// allowedFileMimeTypes 可暂存的 MIME 白名单。
// 与"能被展开进 messages"一一对应：图片、PDF、纯文本、CSV；其余在上传时就拒绝，
// 而不是等到引用时才失败。
var allowedFileMimeTypes = []string{
	"image/",
	"application/pdf",
	"text/plain",
	"text/csv",
}

// isAnthropicFilesRequest 判断这次 /v1/files* 请求是否属于 Anthropic Files 语义。
//
// 分流依据是 anthropic-beta 头（Anthropic 官方 Files API 的准入头），不是路径、
// 不是 UA——UA 不可靠，路径两者共用。
func isAnthropicFilesRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	for _, value := range c.Request.Header.Values("anthropic-beta") {
		if strings.Contains(strings.ToLower(value), anthropicFilesBetaMarker) {
			return true
		}
	}
	return false
}

// dispatchAnthropicFiles 在 HTTP 方法维度上分流。返回 true 表示已处理完毕，
// 调用方必须立即 return，不得再走车道选路。
func (h *Handler) dispatchAnthropicFiles(c *gin.Context) bool {
	if !isAnthropicFilesRequest(c) {
		return false
	}
	switch c.Request.Method {
	case http.MethodPost:
		h.createAnthropicFile(c)
	case http.MethodGet:
		// GET /files 是列表、GET /files/:id 是详情、GET /files/:id/content 是内容；
		// 由路由参数区分。
		switch {
		case c.Param("id") == "":
			h.listAnthropicFiles(c)
		case strings.HasSuffix(c.FullPath(), "/content"):
			h.contentAnthropicFile(c)
		default:
			h.getAnthropicFile(c)
		}
	case http.MethodDelete:
		h.deleteAnthropicFile(c)
	default:
		apierr.Write(c, http.StatusMethodNotAllowed, CodeAnthropicFileInvalidRequest,
			"method not allowed for Anthropic Files", "")
	}
	return true
}

// createAnthropicFile 处理 multipart 上传。
//
// 流式解析 multipart：不整体读入内存，逐 part 判断字段名，`file` part 直接交给
// filestore.Save（边写边限流）。其余字段（purpose / expires_after[...]）都很小，
// 限量读入。
func (h *Handler) createAnthropicFile(c *gin.Context) {
	log := newAnthropicFilesLog()
	defer log.finish(c)

	contentType := c.GetHeader("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data") {
		log.fail(string(route.KindClientError), "Anthropic Files upload requires multipart/form-data")
		apierr.Write(c, http.StatusBadRequest, CodeAnthropicFileInvalidRequest,
			"Anthropic Files upload requires multipart/form-data",
			"send the file as a multipart form field named 'file'")
		return
	}

	if c.Request.ContentLength > 0 && c.Request.ContentLength > maxAnthropicUploadBodyBytes() {
		h.rejectAnthropicFileTooLarge(c, log)
		return
	}

	reader, err := c.Request.MultipartReader()
	if err != nil {
		log.fail(string(route.KindClientError), "parse multipart failed")
		apierr.Write(c, http.StatusBadRequest, CodeAnthropicFileInvalidRequest,
			"parse multipart request failed", "")
		return
	}

	var (
		filename   string
		mimeType   string
		sizeBytes  int64
		sha256Hex  string
		objectID   string
		expiresTTL = model.PBRFileDefaultTTL
		sawFile    bool
	)

	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			log.fail(string(route.KindClientError), "read multipart part failed")
			apierr.Write(c, http.StatusBadRequest, CodeAnthropicFileInvalidRequest,
				"read multipart part failed", "")
			return
		}

		name := part.FormName()
		switch {
		case name == "file":
			sawFile = true
			filename = sanitizeFilename(part.FileName())
			// MIME 优先取 part 自带的 Content-Type，缺失时按扩展名推断。
			mimeType = strings.TrimSpace(part.Header.Get("Content-Type"))
			if mimeType == "" {
				mimeType = mimeByExtension(filename)
			}
			mimeType = normalizeMimeType(mimeType)
			if !isAllowedFileMimeType(mimeType) {
				_ = part.Close()
				log.fail(string(route.KindClientError),
					"unsupported media type for Anthropic Files: "+mimeType)
				apierr.Write(c, http.StatusUnsupportedMediaType, CodeAnthropicFileUnsupportedType,
					fmt.Sprintf("unsupported media type %q for Anthropic Files", mimeType),
					"supported: image/*, application/pdf, text/plain, text/csv")
				return
			}

			// 配额判定放在写入前：先跑一次过期清理，仍超才拒（ADR 0011）。
			if !h.reserveAnthropicFileQuota(c, log) {
				_ = part.Close()
				return
			}

			id, idErr := model.NewPBRFileID(osStatPBRFile)
			if idErr != nil {
				_ = part.Close()
				log.fail(string(route.KindSoftTransient), "allocate file id failed")
				apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
					"allocate file id failed", "")
				return
			}
			objectID = id

			written, digest, saveErr := filestore.Save(objectID, part, model.PBRFileMaxBytes)
			_ = part.Close()
			if saveErr != nil {
				switch {
				case errors.Is(saveErr, filestore.ErrTooLarge):
					h.rejectAnthropicFileTooLarge(c, log)
				case errors.Is(saveErr, filestore.ErrEmpty):
					log.fail(string(route.KindClientError), "uploaded file is empty")
					apierr.Write(c, http.StatusBadRequest, CodeAnthropicFileInvalidRequest,
						"uploaded file is empty", "")
				default:
					log.fail(string(route.KindSoftTransient), "store uploaded file failed")
					apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
						"store uploaded file failed", "")
				}
				return
			}
			sizeBytes, sha256Hex = written, digest

		case strings.HasPrefix(name, "expires_after["):
			// expires_after[anchor] / expires_after[seconds]：只认 created_at 锚点。
			raw, readErr := readSmallPart(part, 128)
			_ = part.Close()
			if readErr != nil {
				continue
			}
			if name == "expires_after[seconds]" {
				if seconds, convErr := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64); convErr == nil && seconds > 0 {
					expiresTTL = time.Duration(seconds) * time.Second
				}
			}

		default:
			// purpose 等其它字段与网关侧存储无关：限量读掉，避免阻塞 multipart 解析。
			_, _ = readSmallPart(part, 4096)
			_ = part.Close()
		}
	}

	if !sawFile {
		log.fail(string(route.KindClientError), "multipart request has no 'file' field")
		apierr.Write(c, http.StatusBadRequest, CodeAnthropicFileInvalidRequest,
			"multipart request has no 'file' field", "")
		return
	}

	// TTL 上限裁剪（ADR 0011：缺省 24h、上限 7d）。
	if expiresTTL > model.PBRFileMaxTTL {
		expiresTTL = model.PBRFileMaxTTL
	}
	if expiresTTL < time.Minute {
		expiresTTL = time.Minute
	}
	now := time.Now()
	record := &model.PBRFile{
		ObjectId:  objectID,
		TokenId:   anthropicFileTokenID(c),
		Filename:  filename,
		MimeType:  mimeType,
		SizeBytes: sizeBytes,
		Sha256:    sha256Hex,
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(expiresTTL).Unix(),
	}
	if err := record.Create(); err != nil {
		// 元数据落库失败必须回收字节，否则留下无主文件。
		_ = filestore.Remove(objectID)
		log.fail(string(route.KindSoftTransient), "persist file metadata failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"persist file metadata failed", "")
		return
	}

	writeAnthropicFileObject(c, http.StatusOK, record)
}

// getAnthropicFile 返回对象元数据（Anthropic 形状）。
func (h *Handler) getAnthropicFile(c *gin.Context) {
	log := newAnthropicFilesLog()
	defer log.finish(c)

	record, ok := h.lookupAnthropicFile(c, log)
	if !ok {
		return
	}
	writeAnthropicFileObject(c, http.StatusOK, record)
}

// deleteAnthropicFile 删除元数据行与字节。字节删除失败只告警：元数据已删，
// 对象对外不存在；残留字节由清理任务在下一轮按孤儿文件回收。
func (h *Handler) deleteAnthropicFile(c *gin.Context) {
	log := newAnthropicFilesLog()
	defer log.finish(c)

	id := strings.TrimSpace(c.Param("id"))
	if !model.IsPBRFileID(id) {
		writeAnthropicFileNotFound(c, log)
		return
	}
	deleted, err := model.DeletePBRFile(id, anthropicFileTokenID(c))
	if err != nil {
		log.fail(string(route.KindSoftTransient), "delete file metadata failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"delete file metadata failed", "")
		return
	}
	if !deleted {
		writeAnthropicFileNotFound(c, log)
		return
	}
	_ = filestore.Remove(id)

	c.JSON(http.StatusOK, gin.H{"id": id, "type": "file", "deleted": true})
}

// contentAnthropicFile 流式返回文件字节。
func (h *Handler) contentAnthropicFile(c *gin.Context) {
	log := newAnthropicFilesLog()
	defer log.finish(c)

	record, ok := h.lookupAnthropicFile(c, log)
	if !ok {
		return
	}
	reader, err := filestore.Open(record.ObjectId)
	if err != nil {
		// 元数据在、字节不在：视为不存在（不暴露内部不一致）。
		writeAnthropicFileNotFound(c, log)
		return
	}
	defer reader.Close()

	c.Header("Content-Type", record.MimeType)
	c.Header("Content-Length", strconv.FormatInt(record.SizeBytes, 10))
	if record.Filename != "" {
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", record.Filename))
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
}

// listAnthropicFiles 列出当前客户端密钥名下的文件。
//
// 返回 Anthropic 分页形状（data[] + has_more + first_id + last_id）。网关侧列表
// 只包含自己的对象——不像 OpenAI Files 那样需要和上游列表做车道过滤。
func (h *Handler) listAnthropicFiles(c *gin.Context) {
	log := newAnthropicFilesLog()
	defer log.finish(c)

	files, err := model.ListPBRFiles(anthropicFileTokenID(c), time.Now())
	if err != nil {
		log.fail(string(route.KindSoftTransient), "list files failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"list files failed", "")
		return
	}
	data := make([]gin.H, 0, len(files))
	for i := range files {
		data = append(data, anthropicFileJSON(&files[i]))
	}
	payload := gin.H{
		"data":     data,
		"has_more": false,
		"first_id": "",
		"last_id":  "",
	}
	if len(files) > 0 {
		payload["first_id"] = files[0].ObjectId
		payload["last_id"] = files[len(files)-1].ObjectId
	}
	c.JSON(http.StatusOK, payload)
}

// lookupAnthropicFile 取路径里的对象并做归属校验。失败时已写好响应。
func (h *Handler) lookupAnthropicFile(c *gin.Context, log *anthropicFilesLog) (*model.PBRFile, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if !model.IsPBRFileID(id) {
		writeAnthropicFileNotFound(c, log)
		return nil, false
	}
	record, err := model.GetPBRFile(id, anthropicFileTokenID(c))
	if err != nil || record == nil {
		writeAnthropicFileNotFound(c, log)
		return nil, false
	}
	// 过期但尚未被清理任务删除的行，对外一律视为不存在。
	if record.ExpiresAt > 0 && record.ExpiresAt <= time.Now().Unix() {
		writeAnthropicFileNotFound(c, log)
		return nil, false
	}
	return record, true
}

// reserveAnthropicFileQuota 配额判定：先跑一次过期清理，仍超则拒绝。
//
// 不预先扣减：写入完成后的实际字节数才是准数，预扣会让失败上传永久占用配额。
// 并发上传可能短暂超额（最多 N × 单文件上限），这在自用单用户场景下可接受，
// 不做跨请求锁——加锁会把上传串行化，代价远大于收益。
func (h *Handler) reserveAnthropicFileQuota(c *gin.Context, log *anthropicFilesLog) bool {
	total, err := model.PBRFilesTotalBytes()
	if err != nil {
		log.fail(string(route.KindSoftTransient), "read files quota failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"read files quota failed", "")
		return false
	}
	if total+model.PBRFileMaxBytes <= model.PBRFilesTotalQuotaBytes {
		return true
	}
	// 接近上限：先清理过期对象再判（ADR 0011 明确要求）。
	if _, cleanupErr := h.cleanupAnthropicFiles(time.Now()); cleanupErr != nil {
		log.fail(string(route.KindSoftTransient), "cleanup expired files failed")
	}
	total, err = model.PBRFilesTotalBytes()
	if err != nil {
		log.fail(string(route.KindSoftTransient), "read files quota failed")
		apierr.Write(c, http.StatusInternalServerError, apierr.CodeInternalError,
			"read files quota failed", "")
		return false
	}
	if total+model.PBRFileMaxBytes <= model.PBRFilesTotalQuotaBytes {
		return true
	}
	log.fail(string(route.KindClientError), "Anthropic Files quota exceeded")
	apierr.Write(c, http.StatusRequestEntityTooLarge, CodeAnthropicFilesQuotaExceeded,
		"Anthropic Files storage quota exceeded",
		fmt.Sprintf("stored files total %.1f MiB, quota %.1f MiB; delete old files or wait for expiry",
			float64(total)/(1<<20), float64(model.PBRFilesTotalQuotaBytes)/(1<<20)))
	return false
}

// cleanupAnthropicFiles 删除所有已过期对象（行 + 字节），返回删除行数。
// 被上传配额判定与 file_cleanup 系统任务共用。
func (h *Handler) cleanupAnthropicFiles(now time.Time) (int64, error) {
	ids, err := model.PBRFileExpiredIDs(now)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	// 先删行再删字节：行没了对象就对外不存在，残留字节由下一轮的孤儿回收带走。
	deleted, err := model.DeleteExpiredPBRFiles(now, ids)
	if err != nil {
		return deleted, err
	}
	for _, id := range ids {
		_ = filestore.Remove(id)
	}
	return deleted, nil
}

func (h *Handler) rejectAnthropicFileTooLarge(c *gin.Context, log *anthropicFilesLog) {
	log.fail(string(route.KindClientError), "uploaded file exceeds the Anthropic Files size limit")
	apierr.Write(c, http.StatusRequestEntityTooLarge, CodeAnthropicFileTooLarge,
		fmt.Sprintf("uploaded file exceeds the %d MiB limit", model.PBRFileMaxBytes/(1<<20)),
		"reduce the file size or send it inline within the message")
}

func writeAnthropicFileNotFound(c *gin.Context, log *anthropicFilesLog) {
	log.fail(string(route.KindClientError), "file not found")
	apierr.Write(c, http.StatusNotFound, CodeAnthropicFileNotFound, "file not found", "")
}

func writeAnthropicFileObject(c *gin.Context, status int, record *model.PBRFile) {
	c.JSON(status, anthropicFileJSON(record))
}

// anthropicFileJSON 渲染 Anthropic Files 对象形状。
//
// 只暴露 Anthropic 契约里的字段；token_id / sha256 属于网关内部信息，不外泄。
func anthropicFileJSON(record *model.PBRFile) gin.H {
	if record == nil {
		return gin.H{}
	}
	out := gin.H{
		"id":         record.ObjectId,
		"type":       "file",
		"mime_type":  record.MimeType,
		"size_bytes": record.SizeBytes,
		"filename":   record.Filename,
		"created_at": time.Unix(record.CreatedAt, 0).UTC().Format(time.RFC3339),
	}
	if record.ExpiresAt > 0 {
		out["expires_at"] = time.Unix(record.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	return out
}

// anthropicFilesLog 一次 Anthropic Files 请求的日志载体。
//
// 与 asyncLog 分开是刻意的：asyncLog.finish 会写 lane 与 route_source（那是
// "选了哪个车道"的证据），而这条路径**没有选路**，写空值是正确语义、写车道名
// 反而会污染按车道聚合的统计。其余口径（只记元数据、estimated_cost=0）一致。
type anthropicFilesLog struct {
	errKind    string
	errSummary string
}

func newAnthropicFilesLog() *anthropicFilesLog { return &anthropicFilesLog{} }

// fail 记失败分类与摘要。kind 用 route.ErrorKind 的取值，与模型面同一套分类口径。
func (l *anthropicFilesLog) fail(kind, summary string) {
	if kind != "" {
		l.errKind = kind
	}
	l.errSummary = strings.TrimSpace(summary)
}

// finish 落一条请求日志。lane / request_model / route_source 全部留空。
func (l *anthropicFilesLog) finish(c *gin.Context) {
	carrier := model.EnsurePBRLogCarrier(c)
	if carrier == nil {
		return
	}
	status := c.Writer.Status()
	if status == 0 {
		status = http.StatusOK
	}
	carrier.InboundFormat = inboundFormatAnthropicFiles
	carrier.Success = status >= 200 && status < 300
	carrier.HTTPStatus = status
	if l.errKind != "" {
		carrier.ErrorKind = l.errKind
		carrier.ErrorSummary = truncate(l.errSummary, 2048)
	}
	// estimated_cost 不赋值：没有 usage 时按 0 落库，PriceBand 落兜底 -1。
	model.WritePBRLog(c)
}

// maxAnthropicUploadBodyBytes multipart 请求体上限：单文件上限 + 元数据开销。
// 留 1 MiB 给 multipart 边界、字段名与其它小字段。
func maxAnthropicUploadBodyBytes() int64 {
	return model.PBRFileMaxBytes + (1 << 20)
}

// anthropicFileTokenID 取当前请求归属的客户端密钥 ID 作为归属键。
// 该值来自 PBRTokenAuth 写入上下文的 *model.ClientKey，不是裸字符串键。
func anthropicFileTokenID(c *gin.Context) int {
	if key := middleware.PBRClientKeyFrom(c); key != nil {
		return key.Id
	}
	return 0
}

// readSmallPart 限量读取一个非文件 part。
func readSmallPart(part io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(part, limit))
}

// sanitizeFilename 清洗客户端文件名：只保留 basename，去掉路径与控制字符。
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if idx := strings.LastIndexAny(name, `/\`); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if len(name) > 255 {
		name = name[:255]
	}
	return name
}

// normalizeMimeType 去掉参数并转小写：`image/png; charset=binary` → `image/png`。
func normalizeMimeType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "application/octet-stream"
	}
	if mediaType, _, err := mime.ParseMediaType(value); err == nil {
		return strings.ToLower(mediaType)
	}
	if idx := strings.IndexByte(value, ';'); idx >= 0 {
		value = value[:idx]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

// isAllowedFileMimeType 白名单判定。前缀项（`image/`）按前缀匹配，其余精确匹配。
func isAllowedFileMimeType(mimeType string) bool {
	for _, allowed := range allowedFileMimeTypes {
		if strings.HasSuffix(allowed, "/") {
			if strings.HasPrefix(mimeType, allowed) {
				return true
			}
			continue
		}
		if mimeType == allowed {
			return true
		}
	}
	return false
}

// mimeByExtension 按扩展名推断 MIME，供 part 未带 Content-Type 时兜底。
func mimeByExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}

// AnthropicFileContentBlock 把网关侧文件展开成 Claude 图片/文档内容块所需的
// base64 数据（供 relay/claude_handler.go 的 file_id 展开使用）。
//
// 返回的 mimeType 来自落库记录而不是客户端声明，避免被伪造的 media_type 误导。
func AnthropicFileContentBytes(record *model.PBRFile) (string, string, error) {
	if record == nil {
		return "", "", errors.New("filestore: nil file record")
	}
	data, err := filestore.ReadAll(record.ObjectId)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(data), record.MimeType, nil
}

// osStatPBRFile 判断磁盘上是否已存在该对象（ID 碰撞时的兜底检查）。
func osStatPBRFile(objectID string) bool {
	reader, err := filestore.Open(objectID)
	if err != nil {
		return false
	}
	_ = reader.Close()
	return true
}
