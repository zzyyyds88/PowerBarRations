package controller

import (
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/usage"
	"github.com/zzyyyds88/PowerBarRations/model"
	relaytypes "github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
)

// attachUsageMetadata reads the inbound body once for the usage view: which
// agent called, which conversation it belongs to, and what the prompt was made
// of. It runs after the request has been validated and before protocol
// conversion, so it sees exactly what the client sent. A failure here only
// records a context status; it never fails the relay.
func attachUsageMetadata(c *gin.Context, format relaytypes.RelayFormat) {
	carrier := model.EnsurePBRLogCarrier(c)
	if carrier == nil {
		if c.Request != nil {
			c.Request.Header.Del(usage.SessionHeader)
		}
		return
	}
	// Channel tests are management actions: they have no agent or session.
	if carrier.RouteSource == "test" {
		if c.Request != nil {
			c.Request.Header.Del(usage.SessionHeader)
		}
		return
	}

	var body []byte
	var size int64
	if storage, err := common.GetBodyStorage(c); err == nil {
		size = storage.Size()
		if size <= usage.MaxAnalysisBytes {
			if data, readErr := storage.Bytes(); readErr == nil {
				body = data
			}
		}
	}
	result := usage.InspectSized(format, c.Request.Header, body, size)
	// X-PBR-Session names a conversation to this gateway; the vendor has no
	// use for it and must not see it. Read it before dropping it.
	if c.Request != nil {
		c.Request.Header.Del(usage.SessionHeader)
	}
	carrier.SessionID = result.SessionID
	carrier.SessionKey = result.SessionKey
	carrier.Subagent = result.Subagent
	carrier.ParentSubagent = result.ParentSubagent
	carrier.ParentSessionID = result.ParentSessionID
	carrier.Client = result.Client
	carrier.Context = result.Context
	carrier.ContextBodySize = int(size)
}
