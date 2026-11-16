package usage

import "strings"

// knownClient is one agent PBR can name from a User-Agent. The table is
// ported from magpie's agent catalog (IDs, aliases and UA prefixes); display
// names are kept here so the UI does not duplicate them.
type knownClient struct {
	id    string
	name  string
	names []string // id and aliases, lower-cased
	ua    []string // what its User-Agent begins with, lower-cased
}

// knownClients is ordered as magpie's catalog: a name is matched before a UA
// prefix, and the first match wins.
var knownClients = []knownClient{
	{id: "claude", name: "Claude Code", names: []string{"claude", "cc", "claude-code"}, ua: []string{"claude-cli", "claude-code"}},
	{id: "codex", name: "Codex", names: []string{"codex"}, ua: []string{"acp-extension-codex", "codex"}},
	{id: "gemini", name: "Gemini CLI", names: []string{"gemini", "gemini-cli"}, ua: []string{"geminicli", "gemini-cli"}},
	{id: "qwen", name: "Qwen Code", names: []string{"qwen", "qwen-code", "qwen-cli"}, ua: []string{"qwencode"}},
	{id: "kimi", name: "Kimi Code", names: []string{"kimi", "kimi-code", "kimi-cli"}, ua: []string{"kimi-code-cli", "kimicli"}},
	{id: "copilot", name: "GitHub Copilot", names: []string{"copilot", "gh-copilot"}, ua: []string{"github-copilot", "copilot"}},
	{id: "cursor", name: "Cursor", names: []string{"cursor", "cursor-agent"}, ua: []string{"cursor"}},
	{id: "cline", name: "Cline", names: []string{"cline", "cline-cli"}, ua: []string{"cline"}},
	{id: "codebuddy", name: "CodeBuddy", names: []string{"codebuddy", "codebuddy-code", "cbc"}, ua: []string{"codebuddy"}},
	{id: "commandcode", name: "Command Code", names: []string{"commandcode", "command-code", "cmd"}, ua: []string{"command-code", "commandcode"}},
	{id: "crush", name: "Crush", names: []string{"crush"}, ua: []string{"crush"}},
	{id: "droid", name: "Droid", names: []string{"droid", "factory", "factory-droid"}, ua: []string{"factory-cli"}},
	{id: "goose", name: "Goose", names: []string{"goose"}, ua: []string{"goose"}},
	{id: "grok", name: "Grok", names: []string{"grok", "grok-build", "grok-cli"}, ua: []string{"grok-shell", "grok-pager", "xai-grok-build"}},
	{id: "hanako", name: "Hanako", names: []string{"hanako", "openhanako", "hana", "hanaagent"}, ua: []string{"hanaagent"}},
	{id: "hermes", name: "Hermes", names: []string{"hermes", "hermes-agent"}, ua: []string{"hermes-agent"}},
	{id: "minimax", name: "MiniMax Code", names: []string{"minimax-code", "mcode"}, ua: []string{"minimax-code", "minimaxcode"}},
	{id: "muse", name: "Muse", names: []string{"muse", "muse-code", "musecode"}, ua: []string{"muse-build", "muse-code"}},
	{id: "omp", name: "Oh My Pi", names: []string{"omp", "oh-my-pi"}, ua: []string{"oh-my-pi"}},
	{id: "paimon", name: "Paimon", names: []string{"paimon"}, ua: []string{"paimon/"}},
	{id: "reasonix", name: "Reasonix", names: []string{"reasonix", "reasonix-studio"}, ua: []string{"reasonix"}},
	{id: "stepcode", name: "StepCode", names: []string{"stepcode", "step"}, ua: []string{"stepcode"}},
	{id: "zcode", name: "ZCode", names: []string{"zcode", "z-code"}, ua: []string{"zcode"}},
	{id: "zed", name: "Zed", names: []string{"zed"}, ua: []string{"zed"}},
	{id: "alma", name: "Alma", names: []string{"alma"}, ua: []string{"alma"}},
	{id: "atomcode", name: "AtomCode", names: []string{"atomcode"}, ua: []string{"atomcode"}},
	{id: "cindy", name: "Cindy", names: []string{"cindy"}, ua: []string{"cindy"}},
	{id: "lody", name: "Lody", names: []string{"lody"}, ua: []string{"lody"}},
	{id: "shelley", name: "Shelley", names: []string{"shelley"}, ua: []string{"shelley"}},
	{id: "vibe", name: "Mistral Vibe", names: []string{"mistral-vibe"}, ua: []string{"mistral-vibe"}},
	{id: "morph", name: "Morph", names: []string{"morph"}, ua: []string{"mistermorph"}},
	{id: "nanobot", name: "Nanobot", names: []string{"nanobot"}, ua: []string{"nanobot"}},
	{id: "vscode", name: "VS Code", names: []string{"vscode", "vs-code", "copilot-chat", "vscode-chat"}, ua: []string{"githubcopilotchat", "vscode_agent_host"}},
	{id: "workbuddy", name: "WorkBuddy", names: []string{"workbuddy", "work-buddy"}, ua: []string{"workbuddy"}},
	{id: "workbuddy-ai", name: "WorkBuddy AI", names: []string{"workbuddy-ai", "workbuddy-intl", "workbuddyai"}, ua: []string{"workbuddy-ai"}},
	{id: "pi-desktop", name: "PI-Desktop", names: []string{"pi-desktop"}, ua: []string{"pi-desktop"}},
	{id: "flowdown", name: "FlowDown", names: []string{"flowdown"}, ua: []string{"flowdown"}},
	{id: "bob", name: "Bob", names: []string{"bob"}, ua: []string{"bob/"}},
	{id: "kimi-code-desktop", name: "Kimi Code Desktop", names: []string{"kimi-code-desktop"}, ua: []string{"kimi-code-desktop"}},
}

// libraries are the product names of HTTP libraries, runtimes and API SDKs
// as AgentOf names a client by its User-Agent. A request that says only one
// of these is from a script, or from a tool built on the library; the UI
// groups them as scripts.
var libraries = map[string]bool{
	"curl": true, "wget": true, "httpie": true, "postmanruntime": true, "insomnia": true,
	"bun": true, "node": true, "deno": true, "node-fetch": true, "undici": true,
	"axios": true, "got": true, "ky": true,
	"python": true, "python-httpx": true, "httpx": true, "python-requests": true,
	"requests": true, "python-urllib": true, "urllib3": true, "aiohttp": true,
	"go-http-client": true, "go-resty": true, "resty": true, "okhttp": true,
	"java": true, "java-http-client": true, "apache-httpclient": true,
	"reactor-netty": true, "ruby": true, "faraday": true, "rest-client": true,
	"guzzlehttp": true, "dart": true, "reqwest": true, "libcurl": true,
	"openai": true, "asyncopenai": true, "anthropic": true, "asyncanthropic": true,
	"ai-sdk": true, "google-genai-sdk": true,
}

// AgentOf names the agent behind a User-Agent. Known agents map to their
// catalog id; anything else keeps its product name. It is magpie's AgentOf:
// a known name is matched before a UA prefix, and the product name is the
// first token of the User-Agent.
func AgentOf(userAgent string) string {
	ua := strings.TrimSpace(userAgent)
	name, _, _ := strings.Cut(ua, "/")
	name, _, _ = strings.Cut(name, " ")
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return "other"
	}
	for _, known := range knownClients {
		for _, alias := range known.names {
			if lower == alias {
				return known.id
			}
		}
	}
	for _, known := range knownClients {
		for _, prefix := range known.ua {
			if strings.HasPrefix(lower, prefix) {
				return known.id
			}
		}
	}
	return clip(lower, 64)
}

// ClientOf is AgentOf under the API's name.
func ClientOf(userAgent string) string { return AgentOf(userAgent) }

// IsLibrary says whether a client, by the name AgentOf gives it, is an HTTP
// library, a runtime or an API SDK rather than an agent.
func IsLibrary(client string) bool {
	return libraries[strings.ToLower(strings.TrimSpace(client))]
}

// ClientGroup is how a client is grouped in usage views: libraries and
// unknown "other" requests are scripts; agents keep their own group.
func ClientGroup(client string) string {
	client = strings.TrimSpace(client)
	if client == "" {
		return ""
	}
	if client == "other" || IsLibrary(client) {
		return "script"
	}
	return client
}

// ClientName is the display name of a client id. Libraries keep their
// product name; unknown clients are shown as they are.
func ClientName(client string) string {
	client = strings.TrimSpace(client)
	if client == "" {
		return ""
	}
	for _, known := range knownClients {
		if known.id == client {
			return known.name
		}
	}
	if client == "other" {
		return "Other"
	}
	return client
}
