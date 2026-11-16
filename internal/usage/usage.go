// Package usage derives per-request usage metadata from what a client sent:
// which agent made the call, which conversation it belongs to, and what the
// prompt was made of.
//
// It keeps names, tags and token counts only — never prompt text, tool
// arguments or file contents. Parsing is best effort: an unreadable or
// oversized body yields a status, never an error that could fail a relay.
package usage

import (
	"encoding/json"
	"net/http"
	"unicode/utf8"

	"github.com/tidwall/gjson"
	relaytypes "github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// Context status values (usage-spec §5.6).
const (
	StatusOK          = "ok"
	StatusUnsupported = "unsupported"
	StatusTooLarge    = "too_large"
	StatusUnavailable = "unavailable"
)

// MaxAnalysisBytes bounds how much of a request body is examined. It is the
// smaller of this cap and the relay's own body limit (the caller passes the
// body it already accepted).
const MaxAnalysisBytes = 16 << 20

// Result is what one inbound model-face request contributes to its log row.
type Result struct {
	SessionID       string
	SessionKey      string
	Subagent        string
	ParentSubagent  string
	ParentSessionID string
	Client          string
	// Context is always non-nil: Status says whether Parts were derived.
	Context *Context
}

// Inspect reads one request. format is the inbound protocol; header and body
// are what the client sent, before any protocol conversion.
func Inspect(format relaytypes.RelayFormat, header http.Header, body []byte) Result {
	return InspectSized(format, header, body, int64(len(body)))
}

// InspectSized is Inspect for a caller that knows the body's size without
// holding it in memory (a disk-backed request body over the analysis cap).
// body may be nil when bodySize is over the cap; the result is then too_large
// and only header-derived session metadata is kept.
func InspectSized(format relaytypes.RelayFormat, header http.Header, body []byte, bodySize int64) Result {
	result := Result{
		Client:          ClientOf(header.Get("User-Agent")),
		Subagent:        clipHeader(header.Get("x-claude-code-agent-id")),
		ParentSubagent:  clipHeader(header.Get("x-claude-code-parent-agent-id")),
		ParentSessionID: clipHeader(header.Get("x-codex-parent-thread-id")),
	}
	result.Context = &Context{Version: 1, Status: StatusUnsupported}

	var parsed gjson.Result
	tooLarge := bodySize > MaxAnalysisBytes || int64(len(body)) > MaxAnalysisBytes
	if !tooLarge && len(body) > 0 && gjson.ValidBytes(body) {
		parsed = gjson.ParseBytes(body)
	}

	// The conversation a request belongs to does not need a parseable body:
	// explicit headers win, then the body's cache key, then the opening turn.
	sessionID := sessionFromHeader(header)
	if sessionID == "" && parsed.Exists() {
		sessionID = sessionFromBody(parsed)
	}
	if sessionID == "" && !tooLarge && len(body) > 0 && chatLike(format) {
		sessionID = fallbackSession(body)
	}
	if sessionID != "" {
		result.SessionID = sessionID
		result.SessionKey = SessionKey(sessionID)
	}

	switch {
	case tooLarge:
		result.Context = &Context{Version: 1, Status: StatusTooLarge}
	case !chatLike(format):
		result.Context = &Context{Version: 1, Status: StatusUnsupported}
	case !parsed.Exists():
		result.Context = &Context{Version: 1, Status: StatusUnavailable}
	case !hasChatFields(format, parsed):
		result.Context = &Context{Version: 1, Status: StatusUnsupported}
	default:
		context := buildPrompt(format, body)
		if context == nil {
			result.Context = &Context{Version: 1, Status: StatusUnavailable}
		} else {
			context.Version = 1
			context.Status = StatusOK
			result.Context = finalizeContext(context)
		}
	}
	return result
}

// chatLike reports whether format carries a conversational prompt we know how
// to partition. Everything else (images, audio, embeddings, realtime) is
// reported as unsupported rather than guessed at.
func chatLike(format relaytypes.RelayFormat) bool {
	switch format {
	case relaytypes.RelayFormatOpenAI,
		relaytypes.RelayFormatClaude,
		relaytypes.RelayFormatGemini,
		relaytypes.RelayFormatOpenAIResponses,
		relaytypes.RelayFormatOpenAIResponsesCompaction:
		return true
	default:
		return false
	}
}

// hasChatFields distinguishes "the caller used an endpoint this format also
// covers for embeddings" from a chat request with a broken body.
func hasChatFields(format relaytypes.RelayFormat, parsed gjson.Result) bool {
	switch format {
	case relaytypes.RelayFormatOpenAI, relaytypes.RelayFormatClaude:
		return parsed.Get("messages").Exists()
	case relaytypes.RelayFormatGemini:
		return parsed.Get("contents").Exists()
	case relaytypes.RelayFormatOpenAIResponses,
		relaytypes.RelayFormatOpenAIResponsesCompaction:
		return parsed.Get("input").Exists() || parsed.Get("instructions").Exists()
	default:
		return false
	}
}

// Context is the prompt's composition. Parts hold the 7 fixed categories;
// Tokens is the vendor-counted prompt once Counted, the estimate before.
type Context struct {
	Version         int    `json:"version"`
	Status          string `json:"status"`
	Tokens          int    `json:"tokens"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Window          int    `json:"window"`
	Counted         bool   `json:"counted"`
	Held            bool   `json:"held"`
	Turns           int    `json:"turns"`
	Truncated       bool   `json:"truncated"`
	Parts           []Part `json:"parts"`
}

// Part is one category of the prompt.
type Part struct {
	Kind   string `json:"kind"`
	Tokens int    `json:"tokens"`
	Items  []Item `json:"items,omitempty"`
}

// Item is one named thing in a part: a tool, an MCP server, a file, a turn.
type Item struct {
	Name   string `json:"name"`
	Tag    string `json:"tag,omitempty"`
	N      int    `json:"n,omitempty"`
	Tokens int    `json:"tokens"`
}

// Calibrate scales the estimated parts to the prompt the vendor counted.
// The remainder from rounding goes to the largest part, so the parts add up
// to exactly counted. A zero count or an already-calibrated context is left
// untouched; window, when known, is recorded either way.
func (c *Context) Calibrate(counted, window int) {
	if c == nil {
		return
	}
	if window > 0 {
		c.Window = window
	}
	if counted <= 0 || c.Counted || c.Tokens <= 0 {
		return
	}
	scale := float64(counted) / float64(c.Tokens)
	sum := 0
	for i := range c.Parts {
		part := &c.Parts[i]
		part.Tokens = int(float64(part.Tokens)*scale + 0.5)
		for j := range part.Items {
			part.Items[j].Tokens = int(float64(part.Items[j].Tokens)*scale + 0.5)
		}
		sum += part.Tokens
	}
	if n := len(c.Parts); n > 0 && sum != counted {
		big := 0
		for i := range c.Parts {
			if c.Parts[i].Tokens > c.Parts[big].Tokens {
				big = i
			}
		}
		c.Parts[big].Tokens += counted - sum
	}
	c.Tokens = counted
	c.Counted = true
}

// finalizeContext enforces the JSON budget. If even the totals exceed it,
// the request is reported unavailable rather than silently losing the fact.
func finalizeContext(c *Context) *Context {
	if c == nil {
		return &Context{Version: 1, Status: StatusUnavailable}
	}
	c.EstimatedTokens = c.Tokens
	data, err := json.Marshal(c)
	if err != nil {
		return &Context{Version: 1, Status: StatusUnavailable}
	}
	if len(data) <= maxContextBytes {
		return c
	}
	c.Truncated = true
	for i := range c.Parts {
		c.Parts[i].Items = nil
	}
	data, err = json.Marshal(c)
	if err != nil || len(data) > maxContextBytes {
		return &Context{Version: 1, Status: StatusUnavailable}
	}
	return c
}

// tokensOf estimates text: about four ASCII bytes a token, one token a
// non-ASCII rune (CJK, mostly). The vendor's count calibrates it later.
func tokensOf(s string) int {
	ascii, other := 0, 0
	for i := 0; i < len(s); {
		if s[i] < 0x80 {
			ascii++
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		other++
		i += size
	}
	return (ascii+3)/4 + other
}

func rawTokens(raw []byte) int { return (len(raw) + 3) / 4 }
