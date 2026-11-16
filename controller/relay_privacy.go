package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy"
	"github.com/zzyyyds88/PowerBarRations/model"
	relaytypes "github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
)

// maxPrivacyBody is the largest body masking reads. The base relay already
// caps bodies; a body above this is refused rather than sent unmasked,
// because masking is fail-closed.
const maxPrivacyBody = 16 << 20

// preparePrivacy masks the inbound body before protocol parsing and wraps
// the response writer so the reply has its values back. It returns nil
// when masking is off. finish is set when a writer was installed and must
// run after every response write (including the error handler's).
func preparePrivacy(c *gin.Context, format relaytypes.RelayFormat) (finish func(), apiErr *relaytypes.NewAPIError) {
	carrier := model.EnsurePBRLogCarrier(c)
	settings, err := privacy.Current()
	if err != nil {
		if carrier != nil {
			carrier.MaskStatus = string(privacy.StatusModelUnavailable)
		}
		return nil, privacyError(err, relaytypes.ErrorCodePrivacyUnavailable, http.StatusServiceUnavailable)
	}
	if !settings.ActiveNow() {
		if carrier != nil {
			carrier.MaskStatus = string(privacy.StatusOff)
		}
		return nil, nil
	}
	if carrier != nil && carrier.RouteSource == "test" {
		carrier.MaskStatus = string(privacy.StatusOff)
		return nil, nil
	}
	if !privacyJSON(c, format) {
		if carrier != nil {
			carrier.MaskStatus = string(privacy.StatusUnsupported)
		}
		return nil, nil
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, privacyError(err, relaytypes.ErrorCodePrivacyUnavailable, http.StatusServiceUnavailable)
	}
	if storage.Size() > maxPrivacyBody {
		return nil, privacyError(errors.New("request body is too large to mask"), relaytypes.ErrorCodePrivacyUnavailable, http.StatusRequestEntityTooLarge)
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, privacyError(err, relaytypes.ErrorCodePrivacyUnavailable, http.StatusServiceUnavailable)
	}

	engine := privacy.Engine{Settings: settings}
	ctx := c.Request.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	res, maskErr := engine.MaskJSON(ctx, body)
	if carrier != nil {
		carrier.MaskedCount = res.Count
		carrier.MaskBlocked = res.Blocked
		carrier.MaskStatus = string(res.Status)
		carrier.MaskTookMs = res.Took.Milliseconds()
		if len(res.Hits) > 0 {
			carrier.MaskHits = marshalHits(res.Hits)
		}
	}
	if maskErr != nil {
		switch {
		case errors.Is(maskErr, privacy.ErrBlocked):
			return nil, privacyError(errors.New(blockedMessage(res.Hits)), relaytypes.ErrorCodePrivacyBlocked, http.StatusBadRequest)
		case errors.Is(maskErr, privacy.ErrModelUnavailable):
			return nil, privacyError(maskErr, relaytypes.ErrorCodePrivacyModelUnavailable, http.StatusServiceUnavailable)
		default:
			return nil, privacyError(maskErr, relaytypes.ErrorCodePrivacyUnavailable, http.StatusServiceUnavailable)
		}
	}
	if len(res.Hits) > 0 {
		if err := replaceBody(c, storage, res.Body); err != nil {
			return nil, privacyError(err, relaytypes.ErrorCodePrivacyUnavailable, http.StatusServiceUnavailable)
		}
	}
	// Wrap even with no hits: a vendor-held conversation can answer with a
	// placeholder this process masked on an earlier turn (privacy-spec §6.2).
	rw, done := privacy.NewResponseWriter(c.Writer)
	c.Writer = rw
	return done, nil
}

func privacyJSON(c *gin.Context, format relaytypes.RelayFormat) bool {
	ct := c.GetHeader("Content-Type")
	if strings.Contains(ct, "multipart/form-data") {
		return false
	}
	if strings.Contains(ct, "json") {
		return true
	}
	if ct != "" {
		return false
	}
	switch format {
	case relaytypes.RelayFormatOpenAI, relaytypes.RelayFormatClaude, relaytypes.RelayFormatGemini,
		relaytypes.RelayFormatOpenAIResponses, relaytypes.RelayFormatOpenAIResponsesCompaction,
		relaytypes.RelayFormatEmbedding, relaytypes.RelayFormatRerank,
		relaytypes.RelayFormatOpenAIAlphaSearch, relaytypes.RelayFormatOpenAIImage,
		relaytypes.RelayFormatOpenAIAudio:
		return true
	}
	return false
}

func replaceBody(c *gin.Context, old common.BodyStorage, body []byte) error {
	next, err := common.CreateBodyStorage(body)
	if err != nil {
		return err
	}
	_ = old.Close()
	c.Set(common.KeyBodyStorage, next)
	c.Set(common.KeyRequestBody, nil)
	c.Request.Body = io.NopCloser(next)
	c.Request.ContentLength = int64(len(body))
	return nil
}

func marshalHits(hits []privacy.Hit) string {
	// A hit is small; the cap protects the log row from a pathological
	// request that masks thousands of distinct values.
	if len(hits) > 200 {
		hits = hits[:200]
	}
	type hit struct {
		Kind        string `json:"kind"`
		By          string `json:"by"`
		Placeholder string `json:"placeholder"`
		Head        string `json:"head,omitempty"`
		Len         int    `json:"len"`
		N           int    `json:"n"`
		Action      string `json:"action,omitempty"`
	}
	out := make([]hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, hit{h.Kind, h.By, h.Placeholder, h.Head, h.Len, h.N, h.Action})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

func blockedMessage(hits []privacy.Hit) string {
	for _, h := range hits {
		if h.Action == privacyBlockAction {
			head := h.Head
			if head == "" {
				head = "…"
			}
			return "privacy masking blocked this request: " + h.Kind + " (" + head + "…, " + strconv.Itoa(h.Len) + " characters); nothing was sent"
		}
	}
	return "privacy masking blocked this request; nothing was sent"
}

const privacyBlockAction = "block"

func privacyError(err error, code relaytypes.ErrorCode, status int) *relaytypes.NewAPIError {
	return relaytypes.NewErrorWithStatusCode(err, code, status, relaytypes.ErrOptionWithSkipRetry())
}
