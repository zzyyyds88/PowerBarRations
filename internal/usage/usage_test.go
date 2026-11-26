package usage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	relaytypes "github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

func inspectJSON(t *testing.T, format relaytypes.RelayFormat, header http.Header, body string) Result {
	t.Helper()
	return Inspect(format, header, []byte(body))
}

func partOf(t *testing.T, context *Context, kind string) *Part {
	t.Helper()
	if context == nil {
		t.Fatalf("nil context")
	}
	for i := range context.Parts {
		if context.Parts[i].Kind == kind {
			return &context.Parts[i]
		}
	}
	return nil
}

func TestClientOf(t *testing.T) {
	tests := []struct {
		ua   string
		want string
	}{
		{"claude-cli/1.0", "claude"},
		{"Claude-Code/2.0 (darwin)", "claude"},
		{"codex/0.1", "codex"},
		{"opencode/0.2", "opencode"},
		{"gemini-cli/0.3", "gemini"},
		{"qwencode/0.1", "qwen"},
		{"zcode/1.0", "zcode"},
		{"hermes-agent/1.0", "hermes"},
		{"cursor/0.44", "cursor"},
		{"github-copilot/1.0", "copilot"},
		{"minimax-code/1.0", "minimax"},
		{"githubcopilotchat/1.0", "vscode"},
		{"curl/8.5.0", "curl"},
		{"python-httpx/0.27", "python-httpx"},
		{"OpenAI/Python 1.0", "openai"},
		{"SomeAgent/1.0", "someagent"},
		{"", "other"},
	}
	for _, test := range tests {
		if got := ClientOf(test.ua); got != test.want {
			t.Fatalf("ClientOf(%q)=%q, want %q", test.ua, got, test.want)
		}
	}
}

func TestClientGroupAndName(t *testing.T) {
	if !IsLibrary("curl") || IsLibrary("claude") {
		t.Fatalf("IsLibrary classification is wrong")
	}
	if ClientGroup("curl") != "script" || ClientGroup("other") != "script" || ClientGroup("claude") != "claude" {
		t.Fatalf("ClientGroup classification is wrong")
	}
	if ClientName("claude") != "Claude Code" || ClientName("curl") != "curl" || ClientName("other") != "Other" {
		t.Fatalf("ClientName mapping is wrong")
	}
}

func TestSessionPrecedence(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hello"}]}`

	header := http.Header{}
	header.Set(SessionHeader, "explicit")
	header.Set("x-claude-code-session-id", "native")
	result := inspectJSON(t, relaytypes.RelayFormatOpenAI, header, body)
	if result.SessionID != "explicit" {
		t.Fatalf("X-PBR-Session must win, got %q", result.SessionID)
	}
	if len(result.SessionKey) != 32 {
		t.Fatalf("session key length = %d, want 32", len(result.SessionKey))
	}

	header = http.Header{}
	header.Set("x-claude-code-session-id", "native")
	result = inspectJSON(t, relaytypes.RelayFormatOpenAI, header, body)
	if result.SessionID != "native" {
		t.Fatalf("native session header = %q, want native", result.SessionID)
	}

	body = `{"messages":[{"role":"user","content":"hello"}],"prompt_cache_key":"cache-key"}`
	result = inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{}, body)
	if result.SessionID != "pck:cache-key" {
		t.Fatalf("prompt_cache_key session = %q", result.SessionID)
	}
}

func TestFallbackSessionUsesOpeningMessage(t *testing.T) {
	chat := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{},
		`{"messages":[{"role":"user","content":"write a test"}]}`)
	again := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{},
		`{"messages":[{"role":"user","content":"write a test"}]}`)
	anthropic := inspectJSON(t, relaytypes.RelayFormatClaude, http.Header{},
		`{"messages":[{"role":"user","content":"write a test"}]}`)
	if chat.SessionID == "" || anthropic.SessionID == "" {
		t.Fatalf("fallback session missing: chat=%q anthropic=%q", chat.SessionID, anthropic.SessionID)
	}
	if chat.SessionID != again.SessionID {
		t.Fatalf("same opening must hash the same: %q vs %q", chat.SessionID, again.SessionID)
	}
	if chat.SessionID != anthropic.SessionID {
		t.Fatalf("the same opening message must hash the same across protocols: %q vs %q", chat.SessionID, anthropic.SessionID)
	}
	if !strings.HasPrefix(chat.SessionID, "pbr-") || len(chat.SessionID) != len("pbr-")+24 {
		t.Fatalf("unexpected fallback id %q", chat.SessionID)
	}
}

func TestFallbackSessionSkipsAgentWrittenOpening(t *testing.T) {
	// The first message is the agent's environment block, not the user's
	// words; magpie opens the conversation with the user message that
	// follows, so different questions in one folder are different sessions.
	first := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{},
		`{"messages":[
			{"role":"user","content":"<environment_context>\n/workspace\n</environment_context>"},
			{"role":"user","content":"question one"}
		]}`)
	second := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{},
		`{"messages":[
			{"role":"user","content":"<environment_context>\n/workspace\n</environment_context>"},
			{"role":"user","content":"question two"}
		]}`)
	if first.SessionID == "" || second.SessionID == "" {
		t.Fatalf("missing sessions: %q %q", first.SessionID, second.SessionID)
	}
	if first.SessionID == second.SessionID {
		t.Fatalf("agent-written opening must not merge different user questions")
	}
}

func TestImageOpeningFingerprints(t *testing.T) {
	// A first message that is only an image still opens the conversation;
	// its message bytes are the fingerprint (magpie's conversationID).
	image := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`
	other := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,BBBB"}}]}]}`
	first := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{}, image)
	second := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{}, other)
	if first.SessionID == "" || second.SessionID == "" {
		t.Fatalf("image opening must fingerprint: %q %q", first.SessionID, second.SessionID)
	}
	if first.SessionID == second.SessionID {
		t.Fatalf("different image openings must not merge")
	}
}

func TestSubagentAndParentSession(t *testing.T) {
	header := http.Header{}
	header.Set("x-claude-code-agent-id", "agent-1")
	header.Set("x-claude-code-parent-agent-id", "agent-0")
	header.Set("x-codex-parent-thread-id", "thread-0")
	result := inspectJSON(t, relaytypes.RelayFormatOpenAI, header,
		`{"messages":[{"role":"user","content":"hi"}]}`)
	if result.Subagent != "agent-1" || result.ParentSubagent != "agent-0" || result.ParentSessionID != "thread-0" {
		t.Fatalf("subagent fields = %+v", result)
	}
}

func TestChatContextParts(t *testing.T) {
	body := `{
		"tools":[
			{"type":"function","function":{"name":"Read","description":"read a file"}},
			{"type":"function","function":{"name":"mcp__fs__read_file"}},
			{"type":"function","function":{"name":"mcp__fs__write_file"}}
		],
		"messages":[
			{"role":"system","content":"You are helpful.\nThe following skills are available for use with the Skill tool:\n- build: build the project\n- test: run tests\nContents of /repo/CLAUDE.md (project instructions, checked into the codebase):\nFollow rules."},
			{"role":"user","content":"read /tmp/a.txt"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"/tmp/a.txt\"}"}}]},
			{"role":"tool","tool_call_id":"c1","name":"Read","content":"Contents of /tmp/a.txt (project):\nhello world"},
			{"role":"assistant","content":"done"}
		]
	}`
	result := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{}, body)
	context := result.Context
	if context.Status != StatusOK {
		t.Fatalf("status = %q, want ok", context.Status)
	}
	if context.Turns != 1 {
		t.Fatalf("turns = %d, want 1", context.Turns)
	}
	if partOf(t, context, PartSystem) == nil {
		t.Fatalf("missing system part: %+v", context.Parts)
	}
	memory := partOf(t, context, PartMemory)
	if memory == nil || len(memory.Items) == 0 || memory.Items[0].Name != "/repo/CLAUDE.md" {
		t.Fatalf("memory part = %+v", memory)
	}
	skills := partOf(t, context, PartSkills)
	if skills == nil || len(skills.Items) != 1 || skills.Items[0].Name != "skills" {
		t.Fatalf("skills part = %+v", skills)
	}
	tools := partOf(t, context, PartTools)
	if tools == nil {
		t.Fatalf("missing tools part")
	}
	var mcp *Item
	for i := range tools.Items {
		if tools.Items[i].Name == "fs" && tools.Items[i].Tag == "mcp" {
			mcp = &tools.Items[i]
		}
	}
	if mcp == nil || mcp.N != 2 {
		t.Fatalf("mcp tools = %+v", tools.Items)
	}
	files := partOf(t, context, PartFiles)
	if files == nil || len(files.Items) == 0 || files.Items[0].Name != "/tmp/a.txt" {
		t.Fatalf("files part = %+v", files)
	}
	if partOf(t, context, PartChat) == nil {
		t.Fatalf("missing chat part")
	}
	if context.Tokens <= 0 || context.EstimatedTokens != context.Tokens {
		t.Fatalf("tokens = %d estimated = %d", context.Tokens, context.EstimatedTokens)
	}
}

func TestAnthropicContext(t *testing.T) {
	body := `{
		"system":[{"type":"text","text":"You are Claude."}],
		"tools":[{"name":"Read","description":"read","input_schema":{"type":"object"}}],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"read it"}]},
			{"role":"assistant","content":[
				{"type":"text","text":"reading"},
				{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/b.txt"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"Contents of /tmp/b.txt (file):\nbody"}]}
			]}
		]
	}`
	context := inspectJSON(t, relaytypes.RelayFormatClaude, http.Header{}, body).Context
	if context.Status != StatusOK {
		t.Fatalf("status = %q", context.Status)
	}
	if partOf(t, context, PartSystem) == nil || partOf(t, context, PartTools) == nil {
		t.Fatalf("parts = %+v", context.Parts)
	}
	files := partOf(t, context, PartFiles)
	if files == nil || len(files.Items) == 0 || files.Items[0].Name != "/tmp/b.txt" {
		t.Fatalf("files = %+v", files)
	}
}

func TestResponsesContextHeld(t *testing.T) {
	body := `{
		"instructions":"You are Codex.",
		"previous_response_id":"resp_1",
		"tools":[{"type":"function","name":"Read"}],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
			{"type":"function_call","call_id":"c1","name":"Read","arguments":"{\"path\":\"/tmp/c.txt\"}"},
			{"type":"function_call_output","call_id":"c1","output":"file body"}
		]
	}`
	context := inspectJSON(t, relaytypes.RelayFormatOpenAIResponses, http.Header{}, body).Context
	if context.Status != StatusOK {
		t.Fatalf("status = %q", context.Status)
	}
	if !context.Held {
		t.Fatalf("previous_response_id must mark held")
	}
	if partOf(t, context, PartSystem) == nil || partOf(t, context, PartTools) == nil || partOf(t, context, PartChat) == nil {
		t.Fatalf("parts = %+v", context.Parts)
	}
	files := partOf(t, context, PartFiles)
	if files == nil || len(files.Items) == 0 || files.Items[0].Name != "/tmp/c.txt" {
		t.Fatalf("files = %+v", files)
	}
}

func TestGeminiContext(t *testing.T) {
	body := `{
		"systemInstruction":{"parts":[{"text":"You are Gemini."}]},
		"tools":[{"functionDeclarations":[{"name":"Read","description":"read"}]}],
		"contents":[
			{"role":"user","parts":[{"text":"read it"}]},
			{"role":"model","parts":[{"functionCall":{"name":"Read","args":{"file_path":"/tmp/d.txt"}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"Read","response":{"ok":true}}},{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}
		]
	}`
	context := inspectJSON(t, relaytypes.RelayFormatGemini, http.Header{}, body).Context
	if context.Status != StatusOK {
		t.Fatalf("status = %q", context.Status)
	}
	if partOf(t, context, PartSystem) == nil || partOf(t, context, PartTools) == nil {
		t.Fatalf("parts = %+v", context.Parts)
	}
	files := partOf(t, context, PartFiles)
	if files == nil || len(files.Items) < 2 {
		t.Fatalf("files = %+v", files)
	}
	var foundFile bool
	for i := range files.Items {
		if files.Items[i].Name == "/tmp/d.txt" && files.Items[i].Tag == "read" {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatalf("gemini file read missing: %+v", files.Items)
	}
}

func TestCalibrate(t *testing.T) {
	context := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{},
		`{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"}]}`).Context
	before := context.Tokens
	if before <= 0 {
		t.Fatalf("estimated tokens = %d", before)
	}
	context.Calibrate(1000, 200000)
	if !context.Counted || context.Tokens != 1000 || context.Window != 200000 {
		t.Fatalf("calibrated = %+v", context)
	}
	sum := 0
	for _, part := range context.Parts {
		sum += part.Tokens
	}
	if sum != 1000 {
		t.Fatalf("parts sum = %d, want 1000", sum)
	}
	if context.EstimatedTokens != before {
		t.Fatalf("estimated changed to %d, want %d", context.EstimatedTokens, before)
	}
	context.Calibrate(2000, 0)
	if context.Tokens != 1000 {
		t.Fatalf("second calibrate must be a no-op, got %d", context.Tokens)
	}
}

func TestCalibrateZeroContextIsNoop(t *testing.T) {
	context := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{},
		`{"messages":[{"role":"user","content":"hi"}]}`).Context
	context.Calibrate(0, 1000)
	if context.Counted || context.Tokens != context.EstimatedTokens {
		t.Fatalf("zero count changed context: %+v", context)
	}
	if context.Window != 1000 {
		t.Fatalf("window = %d, want 1000", context.Window)
	}
}

func TestItemCap(t *testing.T) {
	var tools []string
	for i := 0; i < 30; i++ {
		tools = append(tools, fmt.Sprintf(`{"type":"function","function":{"name":"tool-%02d"}}`, i))
	}
	body := `{"tools":[` + strings.Join(tools, ",") + `],"messages":[{"role":"user","content":"hi"}]}`
	toolsPart := partOf(t, inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{}, body).Context, PartTools)
	if toolsPart == nil {
		t.Fatalf("missing tools")
	}
	if len(toolsPart.Items) != promptItems {
		t.Fatalf("items = %d, want %d", len(toolsPart.Items), promptItems)
	}
	if toolsPart.Items[len(toolsPart.Items)-1].Tag != "more" {
		t.Fatalf("last item = %+v, want summary", toolsPart.Items[len(toolsPart.Items)-1])
	}
}

func TestTooLargeBodyKeepsHeaderSession(t *testing.T) {
	header := http.Header{}
	header.Set(SessionHeader, "oversized")
	body := make([]byte, MaxAnalysisBytes+1)
	for i := range body {
		body[i] = 'a'
	}
	result := Inspect(relaytypes.RelayFormatOpenAI, header, body)
	if result.SessionID != "oversized" {
		t.Fatalf("session = %q", result.SessionID)
	}
	if result.Context.Status != StatusTooLarge {
		t.Fatalf("status = %q, want too_large", result.Context.Status)
	}
}

func TestUnsupportedFormat(t *testing.T) {
	result := Inspect(relaytypes.RelayFormatOpenAIImage, http.Header{}, []byte(`{"prompt":"hi"}`))
	if result.Context.Status != StatusUnsupported {
		t.Fatalf("status = %q, want unsupported", result.Context.Status)
	}
	if result.SessionID != "" {
		t.Fatalf("image request must not fingerprint, got %q", result.SessionID)
	}
}

func TestInvalidJSONIsUnavailable(t *testing.T) {
	result := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{}, `{not json`)
	if result.Context.Status != StatusUnavailable {
		t.Fatalf("status = %q, want unavailable", result.Context.Status)
	}
}

func TestContextNeverStoresText(t *testing.T) {
	secret := "sk-super-secret-value"
	body := `{"messages":[{"role":"system","content":"` + secret + `"},{"role":"user","content":"` + secret + `"}]}`
	context := inspectJSON(t, relaytypes.RelayFormatOpenAI, http.Header{}, body).Context
	data, err := json.Marshal(context)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("context leaked prompt text: %s", data)
	}
}

func TestClipKeepsUTF8Boundary(t *testing.T) {
	value := strings.Repeat("你", 100)
	got := clip(value, 10)
	if len(got) > 10 {
		t.Fatalf("clip length = %d", len(got))
	}
	if !utf8Valid(got) {
		t.Fatalf("clip split a rune: %q", got)
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == 0xFFFD {
			return false
		}
	}
	return true
}

// 物品名必须有界：异常长的工具/文件名不能把上下文 JSON 撑大。
func TestPromptItemNameIsBounded(t *testing.T) {
	b := newPromptBuilder()
	name := strings.Repeat("中", 200) // 600 字节
	b.add(PartTools, name, "tool", 10)
	ctx := b.build()
	part := partOf(t, ctx, PartTools)
	if part == nil || len(part.Items) != 1 {
		t.Fatalf("expected one tools item, got %+v", part)
	}
	if len(part.Items[0].Name) > maxItemNameBytes {
		t.Fatalf("item name length = %d", len(part.Items[0].Name))
	}
	if !utf8Valid(part.Items[0].Name) {
		t.Fatalf("item name split a rune: %q", part.Items[0].Name)
	}
}
