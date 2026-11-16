package usage

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

// SessionHeader is PBR's own conversation override. It is consumed here and
// stripped before the request goes upstream (it names nothing the vendor
// knows).
const SessionHeader = "X-PBR-Session"

const maxSessionIDBytes = 128

// sessionHeaders are where agents already name their conversation. The order
// is the precedence used by magpie; the first non-empty value wins.
var sessionHeaders = []string{
	"x-opencode-session",
	"x-session-affinity",
	"x-session-id",
	"session_id",
	"session-id",
	"x-claude-code-session-id",
	"x-conversation-id",
	"x-deepseek-harness-session-id",
}

// sessionFromHeader is the explicit half of usage-spec §3.1.
func sessionFromHeader(header http.Header) string {
	if value := clipHeader(header.Get(SessionHeader)); value != "" {
		return value
	}
	for _, name := range sessionHeaders {
		if value := clipHeader(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

// sessionFromBody takes prompt_cache_key, which names a conversation on the
// vendor side. The prefix keeps it from being confused with a header that
// happens to carry the same text.
func sessionFromBody(parsed gjson.Result) string {
	key := clipHeader(parsed.Get("prompt_cache_key").String())
	if key == "" {
		return ""
	}
	return clip("pck:"+key, maxSessionIDBytes)
}

// fallbackSession is magpie's conversation fingerprint: the body's first user
// message, or the conversation's opening when the first message is one the
// agent wrote for the model (an instruction file, an environment block) and
// the user's own words follow. An undecodable body is hashed whole.
func fallbackSession(body []byte) string {
	var m struct {
		Messages json.RawMessage `json:"messages"`
		Input    json.RawMessage `json:"input"`
		Contents json.RawMessage `json:"contents"`
	}
	_ = json.Unmarshal(body, &m)
	items, contents := elementsOf(m.Messages), elementsOf(m.Contents)
	geminiContents := len(items) == 0 && len(contents) > 0
	if geminiContents {
		items = contents // Gemini generateContent repeats the first user turn.
	}
	if len(items) == 0 {
		items = elementsOf(m.Input)
	}
	first := []byte(m.Input)
	if len(items) > 0 {
		first = items[0]
	}
	if len(first) == 0 {
		first = body
	}
	opening := [][]byte{first}
	for i, item := range items {
		var role struct {
			Role string `json:"role"`
		}
		// Gemini treats an omitted role as user; Chat and Responses do not.
		if json.Unmarshal(item, &role) == nil && (role.Role == "user" || (geminiContents && role.Role == "")) {
			opening = conversationOpening(items[i:], geminiContents)
			break
		}
	}
	h := sha256.New()
	if len(opening) == 1 {
		_, _ = h.Write(opening[0])
	} else {
		w := bufio.NewWriterSize(h, 512)
		for i, message := range opening {
			if i > 0 {
				_ = w.WriteByte('\n') // two messages never hash as one message's bytes
			}
			writeOpening(w, message)
		}
		_ = w.Flush()
	}
	return "pbr-" + hex.EncodeToString(h.Sum(nil)[:12])
}

// SessionKey is the URL-safe, fixed-length index key for a session id.
func SessionKey(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:16])
}

// clipHeader trims and bounds an identifier without splitting a UTF-8 rune.
func clipHeader(value string) string {
	return clip(strings.TrimSpace(value), maxSessionIDBytes)
}

func clip(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

// elementsOf is the elements of raw when it is an array. It is the semantics
// of magpie's zero-copy reader, decoded by encoding/json here.
func elementsOf(raw json.RawMessage) []json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	return items
}

// userMedia are the content types of an image, a file or a recording a user
// sends in a message of theirs.
var userMedia = []string{"image", "image_url", "input_image", "document", "file", "input_file", "input_audio"}

// userOpens is whether a user message holds anything of the user's own: their
// words (userWords) or an image, a file or a recording. result is whether it
// holds a tool's result. One that can't be read is taken as the user's.
func userOpens(message []byte) (theirs, result bool) {
	var m struct {
		Content json.RawMessage `json:"content"`
		Parts   json.RawMessage `json:"parts"` // Gemini's
	}
	if json.Unmarshal(message, &m) != nil {
		return true, false
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return userWords(s) != "", false
	}
	for _, parts := range [][]json.RawMessage{elementsOf(m.Content), elementsOf(m.Parts)} {
		for _, part := range parts {
			var block struct {
				Type             string          `json:"type"`
				Text             string          `json:"text"`
				Thought          bool            `json:"thought"`
				InlineData       json.RawMessage `json:"inlineData"`
				FileData         json.RawMessage `json:"fileData"`
				FunctionResponse json.RawMessage `json:"functionResponse"`
			}
			if json.Unmarshal(part, &block) != nil {
				continue
			}
			switch {
			case block.Type == "tool_result" || block.FunctionResponse != nil:
				result = true
			case block.InlineData != nil || block.FileData != nil || slices.Contains(userMedia, block.Type):
				theirs = true
			case block.Type == "text" || block.Type == "input_text" || block.Type == "" && !block.Thought:
				theirs = theirs || userWords(block.Text) != ""
			}
		}
	}
	return theirs, result
}

// conversationOpening is how a conversation opens. items are its turns from
// its first user message on (gemini: Gemini's contents, where an omitted role
// is the user's). A first message that holds anything of the user's own
// opens it alone; one the agent wrote alone opens it with the user messages
// after it, through the first that holds the user's own. A reply, a call, its
// output or a tool's result ends the opening; with nothing of the user's
// before it, the first message alone is the opening.
func conversationOpening(items []json.RawMessage, gemini bool) [][]byte {
	open := [][]byte{items[0]}
	for _, item := range items[1:] {
		var role struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(item, &role) != nil {
			break
		}
		if role.Role == "system" || role.Role == "developer" {
			continue
		}
		if role.Role != "user" && !(gemini && role.Role == "") {
			break
		}
		if len(open) == 1 {
			// read only once another user message follows: most conversations
			// open on one, which goes alone either way
			if theirs, result := userOpens(open[0]); theirs || result {
				break
			}
		}
		theirs, result := userOpens(item)
		if result {
			break
		}
		open = append(open, item)
		if theirs {
			return open
		}
	}
	return open[:1]
}

// writeOpening writes one message of an opening the way the hash reads it:
// every object's members in key order with no space between, without a cache
// mark at any depth, and a content of one text part as that part's text.
func writeOpening(w *bufio.Writer, message []byte) {
	message = bytes.TrimSpace(message)
	switch {
	case len(message) == 0:
	case message[0] == '{':
		var object map[string]json.RawMessage
		if json.Unmarshal(message, &object) != nil {
			_, _ = w.Write(message)
			return
		}
		keys := make([]string, 0, len(object))
		for key := range object {
			if key == "cache_control" {
				continue
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		_, _ = w.WriteString("{")
		wrote := 0
		for _, key := range keys {
			if wrote > 0 {
				_ = w.WriteByte(',')
			}
			_, _ = w.WriteString(strconv.Quote(key))
			_ = w.WriteByte(':')
			value := object[key]
			if key == "content" {
				if text, ok := oneTextPart(value); ok {
					value = text
				}
			}
			writeOpening(w, value)
			wrote++
		}
		_, _ = w.WriteString("}")
	case message[0] == '[':
		var elements []json.RawMessage
		if json.Unmarshal(message, &elements) != nil {
			_, _ = w.Write(message)
			return
		}
		_, _ = w.WriteString("[")
		for i, element := range elements {
			if i > 0 {
				_ = w.WriteByte(',')
			}
			writeOpening(w, element)
		}
		_, _ = w.WriteString("]")
	default:
		_, _ = w.Write(message)
	}
}

// oneTextPart is the text of content when it is a list of one text part and
// nothing else beside a cache mark.
func oneTextPart(content []byte) ([]byte, bool) {
	content = bytes.TrimSpace(content)
	if len(content) == 0 || content[0] != '[' {
		return nil, false
	}
	var elements []json.RawMessage
	if json.Unmarshal(content, &elements) != nil || len(elements) != 1 {
		return nil, false
	}
	var part map[string]json.RawMessage
	if json.Unmarshal(elements[0], &part) != nil {
		return nil, false
	}
	var kind string
	if json.Unmarshal(part["type"], &kind) != nil {
		return nil, false
	}
	if kind != "text" && kind != "input_text" {
		return nil, false
	}
	text, ok := part["text"]
	if !ok || len(text) == 0 || text[0] != '"' {
		return nil, false
	}
	for key := range part {
		switch key {
		case "type", "text", "cache_control":
		default:
			return nil, false
		}
	}
	return text, true
}
