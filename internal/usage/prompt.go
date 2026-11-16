package usage

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	relaytypes "github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// The kinds of a prompt's parts, in the order they are shown (usage-spec §5.1).
const (
	PartSystem  = "system"
	PartTools   = "tools"
	PartSkills  = "skills"
	PartMemory  = "memory"
	PartFiles   = "files"
	PartResults = "results"
	PartChat    = "chat"
)

var partOrder = []string{PartSystem, PartTools, PartSkills, PartMemory, PartFiles, PartResults, PartChat}

const (
	promptItems     = 24
	imageTokens     = 1600
	maxContextBytes = 64 << 10
)

// promptBuilder accumulates a prompt while the body is read once. The rules,
// names and caps below are ported from magpie's gateway prompt reader; only
// the JSON decoder (encoding/json instead of its zero-copy view) differs.
type promptBuilder struct {
	parts map[string]map[string]*Item
	turn  int
	held  bool
	calls map[string]toolCall // call id → what was called, for its result
}

type toolCall struct {
	name  string
	files []string // the files it reads, when it reads files
}

func newPromptBuilder() *promptBuilder {
	return &promptBuilder{parts: map[string]map[string]*Item{}, calls: map[string]toolCall{}}
}

func (b *promptBuilder) add(kind, name, tag string, tokens int) {
	if tokens <= 0 {
		return
	}
	m := b.parts[kind]
	if m == nil {
		m = map[string]*Item{}
		b.parts[kind] = m
	}
	key := itemKey(tag, name)
	item := m[key]
	if item == nil {
		item = &Item{Name: name, Tag: tag}
		m[key] = item
	}
	item.N++
	item.Tokens += tokens
}

// chat adds to the conversation's current turn.
func (b *promptBuilder) chat(tokens int) {
	if b.turn == 0 {
		b.turn = 1
	}
	b.add(PartChat, strconv.Itoa(b.turn), "turn", tokens)
}

func (b *promptBuilder) newTurn() { b.turn++ }

// itemKey is an item's key in its part: its tag and name.
func itemKey(tag, name string) string { return tag + "\x00" + name }

func (b *promptBuilder) build() *Context {
	p := &Context{Held: b.held, Turns: b.turn, Parts: []Part{}}
	for _, kind := range partOrder {
		m := b.parts[kind]
		if len(m) == 0 {
			continue
		}
		part := Part{Kind: kind}
		items := make([]Item, 0, len(m))
		for _, item := range m {
			part.Tokens += item.Tokens
			copied := *item
			if copied.Tag != "mcp" && copied.Tag != "namespace" && copied.Tag != "result" {
				copied.N = 0 // a count only where it says something
			}
			if copied.N == 1 {
				copied.N = 0
			}
			items = append(items, copied)
		}
		if kind == PartChat {
			slices.SortFunc(items, func(a, b Item) int {
				x, _ := strconv.Atoi(a.Name)
				y, _ := strconv.Atoi(b.Name)
				return x - y
			})
			// the turns keep the latest ones, oldest first
			if len(items) > promptItems {
				rest := Item{Name: "earlier", Tag: "turns", N: len(items) - promptItems + 1}
				for _, item := range items[:len(items)-promptItems+1] {
					rest.Tokens += item.Tokens
				}
				items = append([]Item{rest}, items[len(items)-promptItems+1:]...)
			}
		} else {
			slices.SortFunc(items, func(a, b Item) int {
				if a.Tokens != b.Tokens {
					return b.Tokens - a.Tokens
				}
				return strings.Compare(a.Name, b.Name)
			})
			if len(items) > promptItems {
				rest := Item{Name: "other", Tag: "more", N: len(items) - promptItems + 1}
				for _, item := range items[promptItems-1:] {
					rest.Tokens += item.Tokens
				}
				items = append(items[:promptItems-1:promptItems-1], rest)
			}
		}
		part.Items = items
		p.Tokens += part.Tokens
		p.Parts = append(p.Parts, part)
	}
	return p
}

// buildPrompt reads what a request's prompt holds; nil when the body isn't
// one it can read.
func buildPrompt(format relaytypes.RelayFormat, body []byte) (p *Context) {
	defer func() {
		if recover() != nil {
			p = nil
		}
	}()
	b := newPromptBuilder()
	var ok bool
	switch format {
	case relaytypes.RelayFormatOpenAI:
		ok = b.chatCompletions(body)
	case relaytypes.RelayFormatClaude:
		ok = b.anthropic(body)
	case relaytypes.RelayFormatOpenAIResponses,
		relaytypes.RelayFormatOpenAIResponsesCompaction:
		ok = b.responses(body)
	case relaytypes.RelayFormatGemini:
		ok = b.gemini(body)
	}
	if !ok {
		return nil
	}
	return b.build()
}

// --- text: instruction files, reminders, harness messages ---

var (
	// Claude Code: "Contents of /path/CLAUDE.md (project instructions, …):"
	contentsOf = regexp.MustCompile(`(?m)^Contents of (\S.*?) \(([^()\n]*(?:\([^()\n]*\)[^()\n]*)*)\):[ \t]*$`)
	// OpenCode: "Instructions from: /path/AGENTS.md"
	instructionsFrom = regexp.MustCompile(`(?m)^Instructions from: (\S.*?)[ \t]*$`)
	// Codex: "# AGENTS.md instructions for /path"
	agentsFor = regexp.MustCompile(`^# (\S+\.md) instructions for (\S.*?)[ \t]*\n`)
	leadTag   = regexp.MustCompile(`^\s*<([a-zA-Z_][\w -]{0,40})>`)
)

// memoryTag says whose an instruction file is from how the agent describes
// it, or its path.
func memoryTag(path, desc string) string {
	d := strings.ToLower(desc)
	switch {
	case strings.Contains(d, "user's") || strings.Contains(d, "global"):
		return "user"
	case strings.Contains(d, "local"):
		return "local"
	case strings.Contains(d, "auto-memory") || strings.Contains(d, "memory"):
		return "memory"
	case d != "":
		return "project"
	}
	if strings.Contains(path, "/.claude/") || strings.Contains(path, "/.codex/") || strings.Contains(path, "/.config/") {
		return "user"
	}
	return "project"
}

// splitMemory takes the instruction files out of a text: each is added as
// memory, and what is left is returned.
func (b *promptBuilder) splitMemory(text string) string {
	type section struct {
		at, body   int
		path, desc string
	}
	var sections []section
	for _, m := range contentsOf.FindAllStringSubmatchIndex(text, -1) {
		sections = append(sections, section{at: m[0], body: m[1], path: text[m[2]:m[3]], desc: text[m[4]:m[5]]})
	}
	for _, m := range instructionsFrom.FindAllStringSubmatchIndex(text, -1) {
		sections = append(sections, section{at: m[0], body: m[1], path: text[m[2]:m[3]]})
	}
	if len(sections) == 0 {
		return text
	}
	slices.SortFunc(sections, func(a, b section) int { return a.at - b.at })
	var rest strings.Builder
	rest.WriteString(text[:sections[0].at])
	for i, section := range sections {
		end := len(text)
		if i+1 < len(sections) {
			end = sections[i+1].at
		}
		chunk := text[section.at:end]
		// a reminder's closing tag and what follows it aren't the file's
		if j := strings.Index(chunk, "</system-reminder>"); j >= 0 {
			rest.WriteString(chunk[j:])
			chunk = chunk[:j]
		}
		b.add(PartMemory, section.path, memoryTag(section.path, section.desc), tokensOf(chunk))
	}
	return rest.String()
}

// skillsList heads the skills Claude Code lists, in its own prompt or a
// reminder; the list runs to the next blank line.
const skillsList = "The following skills are available for use with the Skill tool:"

// splitSkills takes the list of skills out of a text, as skills, and returns
// what is left.
func (b *promptBuilder) splitSkills(text string) string {
	at := strings.Index(text, skillsList)
	if at < 0 {
		return text
	}
	body := text[at+len(skillsList):]
	items := strings.TrimLeft(body, "\n")
	end := len(text)
	if j := strings.Index(items, "\n\n"); j >= 0 {
		end -= len(items) - j
	}
	list := text[at:end]
	b.add(PartSkills, "skills", "skills", tokensOf(list))
	return text[:at] + strings.TrimLeft(text[end:], "\n")
}

// harnessName names a system or developer text by its leading tag, or what
// it is.
func harnessName(text, fallback string) string {
	if m := leadTag.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	if strings.HasPrefix(text, "# Environment") {
		return "environment"
	}
	return fallback
}

// systemText adds the agent's own prompt, with the instruction files in it
// taken out as memory.
func (b *promptBuilder) systemText(text, name string) {
	rest := b.splitSkills(b.splitMemory(text))
	if strings.HasPrefix(strings.TrimSpace(rest), "<skills_instructions>") {
		b.add(PartSkills, "skills", "skills", tokensOf(rest))
		return
	}
	if strings.TrimSpace(rest) == "" {
		return
	}
	b.add(PartSystem, harnessName(rest, name), "", tokensOf(rest))
}

// userText adds a user's text: an instruction file or reminder the agent put
// in as the user, or what the user said. It says which.
func (b *promptBuilder) userText(text string) (said bool) {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "<system-reminder>"):
		rest := b.splitSkills(b.splitMemory(t))
		rest = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(rest), "<system-reminder>"), "</system-reminder>"))
		if rest != "" {
			b.add(PartMemory, "reminders", "reminder", tokensOf(rest))
		}
		return false
	case agentsFor.MatchString(t):
		m := agentsFor.FindStringSubmatch(t)
		path := strings.TrimRight(m[2], "/") + "/" + m[1]
		b.add(PartMemory, path, "project", tokensOf(t))
		return false
	case strings.HasPrefix(t, "<environment_context>") || strings.HasPrefix(t, "<user_instructions>") ||
		strings.HasPrefix(t, "<turn_aborted>") || strings.HasPrefix(t, "<user_shell_command>"):
		b.add(PartSystem, harnessName(t, "context"), "", tokensOf(t))
		return false
	}
	b.chat(tokensOf(text))
	return true
}

// --- tool calls and what they read ---

var (
	// the tools that read a file, and where they take its path
	readTools = map[string]bool{"Read": true, "read": true, "read_file": true, "ReadFile": true, "view": true,
		"View": true, "NotebookRead": true, "readFile": true, "open_file": true, "cat": true}
	pathKeys  = []string{"file_path", "path", "filePath", "target_file", "notebook_path", "file", "filename", "uri"}
	cmdOfExec = regexp.MustCompile(`\bcmd\s*:\s*"((?:[^"\\]|\\.)*)"`)
	toolOfJS  = regexp.MustCompile(`\btools\.(\w+)\s*\(`)
	readCmds  = map[string]bool{"cat": true, "head": true, "tail": true, "nl": true, "bat": true, "less": true, "more": true}
)

// filesOfCommand is the files a shell command reads, when all it does is
// read files: cat, head, tail, nl, bat, sed -n.
func filesOfCommand(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	// bash -lc "…", as Codex sends its shell tool's
	for _, pre := range []string{"bash -lc ", "bash -c ", "sh -c ", "zsh -lc "} {
		if strings.HasPrefix(cmd, pre) {
			cmd = strings.Trim(strings.TrimSpace(cmd[len(pre):]), `"'`)
		}
	}
	if strings.ContainsAny(cmd, "|;&><`$") {
		// a pipeline or more than one command: not a plain read; but
		// "cd dir && cat x" still is
		if i := strings.Index(cmd, "&&"); i >= 0 && strings.HasPrefix(cmd, "cd ") && !strings.ContainsAny(cmd[i+2:], "|;&><`$") {
			cmd = strings.TrimSpace(cmd[i+2:])
		} else {
			return nil
		}
	}
	f := strings.Fields(cmd)
	if len(f) < 2 {
		return nil
	}
	sed := f[0] == "sed" && slices.Contains(f, "-n")
	if !readCmds[f[0]] && !sed {
		return nil
	}
	var files []string
	for i := 1; i < len(f); i++ {
		a := strings.Trim(f[i], `"'`)
		if strings.HasPrefix(a, "-") {
			if (a == "-n" || a == "-c") && !sed && i+1 < len(f) {
				i++ // head -n 40
			}
			continue
		}
		if sed && strings.HasSuffix(a, "p") && strings.ContainsAny(a, "0123456789,") && !strings.Contains(a, "/") && !strings.Contains(a, ".") {
			continue // sed -n 1,80p
		}
		if _, err := strconv.Atoi(a); err == nil {
			continue
		}
		files = append(files, a)
	}
	return files
}

// callOf reads what a tool call does from its name and input: the files it
// reads, and the name its results go by.
func callOf(name string, input []byte) toolCall {
	c := toolCall{name: name}
	var args map[string]json.RawMessage
	_ = json.Unmarshal(input, &args)
	str := func(k string) string {
		var s string
		if json.Unmarshal(args[k], &s) == nil {
			return s
		}
		return ""
	}
	if readTools[name] {
		for _, k := range pathKeys {
			if s := str(k); s != "" {
				c.files = []string{s}
				return c
			}
		}
	}
	cmd := str("command")
	if cmd == "" {
		cmd = str("cmd")
	}
	if cmd == "" {
		var argv []string
		if json.Unmarshal(args["command"], &argv) == nil {
			cmd = strings.Join(argv, " ")
		}
	}
	if cmd == "" && args == nil {
		// Codex's exec takes JavaScript: text(await tools.exec_command({cmd:"cat x"}))
		s := string(input)
		if m := toolOfJS.FindStringSubmatch(s); m != nil {
			c.name = m[1]
		}
		if m := cmdOfExec.FindStringSubmatch(s); m != nil {
			cmd, _ = strconv.Unquote(`"` + m[1] + `"`)
		}
	}
	if cmd != "" {
		c.files = filesOfCommand(cmd)
	}
	return c
}

// result adds what a tool answered: the files it read, or its output.
func (b *promptBuilder) result(callID string, tokens int) {
	c, ok := b.calls[callID]
	if !ok {
		c.name = "tool"
	}
	if len(c.files) > 0 {
		each := tokens / len(c.files)
		for i, f := range c.files {
			n := each
			if i == 0 {
				n += tokens - each*len(c.files)
			}
			tag := "read"
			if !readTools[c.name] {
				tag = "shell"
			}
			b.add(PartFiles, f, tag, n)
		}
		return
	}
	b.add(PartResults, c.name, "result", tokens)
}

// tool adds a tool definition: an MCP server's tools as one.
func (b *promptBuilder) tool(name string, tokens int) {
	if strings.HasPrefix(name, "mcp__") {
		rest := strings.TrimPrefix(name, "mcp__")
		server := rest
		if i := strings.Index(rest, "__"); i > 0 {
			server = rest[:i]
		}
		b.add(PartTools, server, "mcp", tokens)
		return
	}
	b.add(PartTools, name, "", tokens)
}

// --- Anthropic Messages ---

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Data      string          `json:"data"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	Source    json.RawMessage `json:"source"`
}

// blocksOf reads content that is a string or a list of blocks.
func blocksOf(c json.RawMessage) []rawBlock {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return []rawBlock{{Type: "text", Text: s}}
	}
	var bs []rawBlock
	_ = json.Unmarshal(c, &bs)
	return bs
}

func (b *promptBuilder) anthropic(body []byte) bool {
	var req struct {
		System   json.RawMessage `json:"system"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	for _, s := range blocksOf(req.System) {
		if strings.HasPrefix(s.Text, "x-anthropic-billing-hdr") {
			b.add(PartSystem, "billing", "", tokensOf(s.Text))
			continue
		}
		b.systemText(s.Text, "prompt")
	}
	for _, t := range req.Tools {
		var d struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(t, &d)
		b.tool(d.Name, rawTokens(t))
	}
	for _, m := range req.Messages {
		bs := blocksOf(m.Content)
		switch m.Role {
		case "system", "developer":
			for _, x := range bs {
				b.systemText(x.Text, "harness")
			}
			continue
		case "user":
			// a message that answers tool calls goes on with the turn;
			// one where the user says something begins one
			results := slices.ContainsFunc(bs, func(x rawBlock) bool { return x.Type == "tool_result" })
			if !results && slices.ContainsFunc(bs, func(x rawBlock) bool { return x.Type == "text" && userSays(x.Text) }) {
				b.newTurn()
			}
		}
		for _, x := range bs {
			b.anthropicBlock(m.Role, x)
		}
	}
	return true
}

// userSays tells what the user said from what the agent put in for them.
func userSays(text string) bool { return userWords(text) != "" }

var (
	// reminders are what agents add to a user's message for the model alone.
	reminders = regexp.MustCompile(`(?s)<system-reminder(?:\s[^>]*)?>.*?</system-reminder>`)
	// myRequest heads what the user typed in the Codex app, after the context
	// it puts first.
	myRequest = regexp.MustCompile(`(?m)^## My request(?: for Codex)?:[ \t]*\n?`)
	// injected are how a text the agent put in as the user begins.
	injected = []string{
		"<system-reminder", "<environment_context>", "<user_instructions>", "<turn_aborted>",
		"<user_shell_command>", "<subagent_notification>", "<in-app-browser-context",
		"<external_codex_apps_open_page>", "<session-context",
		"<task-notification>", "<subagent-notification>", "<plugin_reference>",
		"Available agent types for the Agent tool:", "Available agent types and the tools they have access to:",
		"New agent types are now available for the Agent tool:",
		"The TodoWrite tool hasn't been used recently.", "The task tools haven't been used recently.",
		"Conversation rewind applied.", "Workspace rewind applied.",
		"The following deferred tools are", "# MCP Server Instructions", "The following MCP servers",
	}
)

// userWords is what the user said in a text of theirs: "" for a text the
// agent put in (an instruction file, a reminder, a notice), else the text
// without the reminders in it, and after the Codex app's context.
func userWords(text string) string {
	t := text
	if strings.Contains(t, "<system-reminder") {
		t = reminders.ReplaceAllString(t, "")
	}
	t = strings.TrimSpace(t)
	if strings.Contains(t, "## My request") {
		if at := myRequest.FindAllStringIndex(t, -1); len(at) > 0 {
			t = strings.TrimSpace(t[at[len(at)-1][1]:])
		}
	}
	if t == "" || agentsFor.MatchString(t) {
		return ""
	}
	for _, lead := range injected {
		if strings.HasPrefix(t, lead) {
			return ""
		}
	}
	return t
}

func (b *promptBuilder) anthropicBlock(role string, x rawBlock) {
	switch x.Type {
	case "text":
		if role == "user" {
			b.userText(x.Text)
		} else {
			b.chat(tokensOf(x.Text))
		}
	case "thinking":
		b.chat(tokensOf(x.Thinking))
	case "redacted_thinking":
		b.chat(tokensOf(x.Data) / 4)
	case "tool_use", "server_tool_use":
		b.calls[x.ID] = callOf(x.Name, x.Input)
		b.chat(tokensOf(x.Name) + rawTokens(x.Input))
	case "tool_result", "web_search_tool_result", "web_fetch_tool_result":
		n := 0
		for _, c := range blocksOf(x.Content) {
			switch c.Type {
			case "image", "document":
				b.add(PartFiles, c.Type, "image", imageTokens)
			default:
				n += tokensOf(c.Text)
			}
		}
		if len(x.Content) > 0 && x.Content[0] != '"' && x.Content[0] != '[' {
			n = rawTokens(x.Content)
		}
		b.result(x.ToolUseID, n)
	case "image", "document":
		if x.Type == "document" && len(x.Source) > 0 && !strings.Contains(string(x.Source), `"base64"`) {
			b.add(PartFiles, "document", "image", rawTokens(x.Source))
			return
		}
		b.add(PartFiles, x.Type, "image", imageTokens)
	default:
		b.chat(tokensOf(x.Text))
	}
}

// --- OpenAI Responses ---

func (b *promptBuilder) responses(body []byte) bool {
	var req struct {
		Instructions json.RawMessage   `json:"instructions"`
		Input        json.RawMessage   `json:"input"`
		Tools        []json.RawMessage `json:"tools"`
		Previous     string            `json:"previous_response_id"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	b.held = req.Previous != ""
	var instructions string
	if json.Unmarshal(req.Instructions, &instructions) == nil && instructions != "" {
		b.systemText(instructions, "prompt")
	}
	for _, t := range req.Tools {
		b.responsesTool(t)
	}
	var input string
	if json.Unmarshal(req.Input, &input) == nil {
		b.newTurn()
		b.userText(input)
		return true
	}
	items, _ := inputList(req.Input)
	for _, item := range items {
		b.responsesItem(item)
	}
	return true
}

func (b *promptBuilder) responsesTool(t json.RawMessage) {
	var d struct {
		Type     string            `json:"type"`
		Name     string            `json:"name"`
		Tools    []json.RawMessage `json:"tools"`
		Function *struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	_ = json.Unmarshal(t, &d)
	name := d.Name
	if name == "" && d.Function != nil {
		name = d.Function.Name
	}
	if name == "" {
		name = d.Type // web_search, image_generation…
	}
	if d.Type == "namespace" || len(d.Tools) > 0 {
		// a namespace of tools: Codex's own functions one by one, any other
		// as one, as an MCP server's
		if name == "functions" {
			for _, x := range d.Tools {
				b.responsesTool(x)
			}
			return
		}
		// an MCP server's namespace is named as its tools are, mcp__<server>…;
		// Codex's own (clock, collaboration…) aren't MCP
		tag := "namespace"
		if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
			tag, name = "mcp", strings.TrimRight(strings.SplitN(rest, "__", 2)[0], "_")
		}
		b.add(PartTools, name, tag, rawTokens(t))
		if part := b.parts[PartTools][itemKey(tag, name)]; part != nil {
			part.N += len(d.Tools) - 1
		}
		return
	}
	b.tool(name, rawTokens(t))
}

func (b *promptBuilder) responsesItem(raw json.RawMessage) {
	var it struct {
		Type      string            `json:"type"`
		Role      string            `json:"role"`
		Content   json.RawMessage   `json:"content"`
		CallID    string            `json:"call_id"`
		Name      string            `json:"name"`
		Arguments string            `json:"arguments"`
		Input     string            `json:"input"`
		Output    json.RawMessage   `json:"output"`
		Tools     []json.RawMessage `json:"tools"`
		Summary   []struct {
			Text string `json:"text"`
		} `json:"summary"`
		Encrypted string          `json:"encrypted_content"`
		Action    json.RawMessage `json:"action"`
	}
	if json.Unmarshal(raw, &it) != nil {
		return
	}
	if it.Type == "" && it.Role != "" {
		it.Type = "message"
	}
	switch it.Type {
	case "additional_tools":
		for _, t := range it.Tools {
			b.responsesTool(t)
		}
	case "message":
		parts := responsesContent(it.Content)
		switch it.Role {
		case "system", "developer":
			for _, p := range parts {
				b.systemText(p.text, "prompt")
			}
		case "user":
			if slices.ContainsFunc(parts, func(p rPart) bool { return p.image || userSays(p.text) }) {
				b.newTurn()
			}
			for _, p := range parts {
				if p.image {
					b.add(PartFiles, "image", "image", imageTokens)
					continue
				}
				b.userText(p.text)
			}
		default:
			for _, p := range parts {
				b.chat(tokensOf(p.text))
			}
		}
	case "reasoning":
		n := 0
		for _, s := range it.Summary {
			n += tokensOf(s.Text)
		}
		// the reasoning itself, sealed: about three quarters of its base64
		// is what was sealed
		n += len(it.Encrypted) * 3 / 4 / 4
		b.chat(n)
	case "function_call", "custom_tool_call", "local_shell_call", "tool_search_call":
		input := []byte(it.Arguments)
		if it.Type == "custom_tool_call" {
			input = []byte(it.Input)
		}
		if it.Type == "local_shell_call" {
			input = it.Action
		}
		b.calls[it.CallID] = callOf(it.Name, input)
		b.chat(tokensOf(it.Name) + (len(input)+3)/4)
	case "function_call_output", "custom_tool_call_output", "local_shell_call_output", "tool_search_output":
		n := 0
		var s string
		if json.Unmarshal(it.Output, &s) == nil {
			n = tokensOf(s)
		} else {
			for _, p := range responsesContent(it.Output) {
				if p.image {
					b.add(PartFiles, "image", "image", imageTokens)
					continue
				}
				n += tokensOf(p.text)
			}
		}
		if it.Type == "tool_search_output" {
			for _, t := range it.Tools {
				b.responsesTool(t)
			}
		}
		b.result(it.CallID, n)
	case "compaction", "compaction_summary":
		b.add(PartChat, "compacted", "compacted", rawTokens(raw))
	default:
		b.chat(rawTokens(raw))
	}
}

type rPart struct {
	text  string
	image bool
	src   json.RawMessage // an image's image_url
}

func responsesContent(c json.RawMessage) []rPart {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return []rPart{{text: s}}
	}
	var ps []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		ImageURL json.RawMessage `json:"image_url"`
	}
	_ = json.Unmarshal(c, &ps)
	out := make([]rPart, 0, len(ps))
	for _, p := range ps {
		out = append(out, rPart{text: p.Text, image: p.Type == "input_image" || p.Type == "input_file" || p.Type == "image_url", src: p.ImageURL})
	}
	return out
}

// --- OpenAI Chat Completions ---

func (b *promptBuilder) chatCompletions(body []byte) bool {
	var req struct {
		Messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
			Reasoning  string          `json:"reasoning_content"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	for _, t := range req.Tools {
		b.responsesTool(t)
	}
	for _, m := range req.Messages {
		parts := responsesContent(m.Content)
		switch m.Role {
		case "system", "developer":
			for _, p := range parts {
				b.systemText(p.text, "prompt")
			}
		case "user":
			if slices.ContainsFunc(parts, func(p rPart) bool { return p.image || userSays(p.text) }) {
				b.newTurn()
			}
			for _, p := range parts {
				if p.image {
					b.add(PartFiles, "image", "image", imageTokens)
					continue
				}
				b.userText(p.text)
			}
		case "tool", "function":
			n := 0
			for _, p := range parts {
				if p.image {
					b.add(PartFiles, "image", "image", imageTokens)
					continue
				}
				n += tokensOf(p.text)
			}
			b.result(m.ToolCallID, n)
		default:
			// its reasoning, what it said and its calls, a piece each
			b.chat(tokensOf(m.Reasoning))
			n := 0
			for _, p := range parts {
				n += tokensOf(p.text)
			}
			b.chat(n)
			for _, c := range m.ToolCalls {
				b.calls[c.ID] = callOf(c.Function.Name, []byte(c.Function.Arguments))
				b.chat(tokensOf(c.Function.Name) + tokensOf(c.Function.Arguments))
			}
		}
	}
	return true
}

// --- Gemini ---

func (b *promptBuilder) gemini(body []byte) bool {
	type gPart struct {
		Text         string          `json:"text"`
		Thought      bool            `json:"thought"`
		InlineData   json.RawMessage `json:"inlineData"`
		FileData     json.RawMessage `json:"fileData"`
		FunctionCall *struct {
			ID   string          `json:"id"`
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		} `json:"functionCall"`
		FunctionResponse *struct {
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Response json.RawMessage `json:"response"`
		} `json:"functionResponse"`
	}
	var req struct {
		System *struct {
			Parts []gPart `json:"parts"`
		} `json:"systemInstruction"`
		Contents []struct {
			Role  string  `json:"role"`
			Parts []gPart `json:"parts"`
		} `json:"contents"`
		Tools []struct {
			Decls []json.RawMessage `json:"functionDeclarations"`
		} `json:"tools"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	if req.System != nil {
		for _, p := range req.System.Parts {
			b.systemText(p.Text, "prompt")
		}
	}
	for _, t := range req.Tools {
		for _, d := range t.Decls {
			var n struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(d, &n)
			b.tool(n.Name, rawTokens(d))
		}
	}
	for _, c := range req.Contents {
		if c.Role != "model" && slices.ContainsFunc(c.Parts, func(p gPart) bool { return userSays(p.Text) && p.FunctionResponse == nil }) {
			b.newTurn()
		}
		for _, p := range c.Parts {
			switch {
			case p.FunctionCall != nil:
				// Gemini matches a response to its call by name, or id
				id := p.FunctionCall.ID
				if id == "" {
					id = "name:" + p.FunctionCall.Name
				}
				b.calls[id] = callOf(p.FunctionCall.Name, p.FunctionCall.Args)
				b.chat(tokensOf(p.FunctionCall.Name) + rawTokens(p.FunctionCall.Args))
			case p.FunctionResponse != nil:
				id := p.FunctionResponse.ID
				if id == "" {
					id = "name:" + p.FunctionResponse.Name
				}
				if _, ok := b.calls[id]; !ok {
					b.calls[id] = toolCall{name: p.FunctionResponse.Name}
				}
				b.result(id, rawTokens(p.FunctionResponse.Response))
			case len(p.InlineData) > 0 || len(p.FileData) > 0:
				b.add(PartFiles, "image", "image", imageTokens)
			case c.Role == "model":
				b.chat(tokensOf(p.Text))
			default:
				b.userText(p.Text)
			}
		}
	}
	return true
}

// inputList is a request's input list, for a reader that keeps none of its
// items. It is the semantics of magpie's zero-copy reader, decoded by
// encoding/json here.
func inputList(raw json.RawMessage) ([]json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) == 0:
		return nil, false
	case string(raw) == "null":
		return nil, true
	case raw[0] != '[':
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false
	}
	return items, true
}
