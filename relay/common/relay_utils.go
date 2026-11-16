package common

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/constant"

	"github.com/gin-gonic/gin"
)

type HasPrompt interface {
	GetPrompt() string
}

type HasImage interface {
	HasImage() bool
}

// protocolChannelTypes 是 api-spec §4.1「base_url 归一化」适用的协议型渠道：
// OpenAI 兼容 / Anthropic / Gemini。其余厂商类型与 Custom(8) 原样直通，
// 旧数据行为不变。
var protocolChannelTypes = map[int]bool{
	constant.ChannelTypeOpenAI:    true,
	constant.ChannelTypeAnthropic: true,
	constant.ChannelTypeGemini:    true,
}

// upstreamEndpointSuffixes 是可剥掉的结尾完整端点（多段在前，按尾部段组匹配）。
var upstreamEndpointSuffixes = [][]string{
	{"chat", "completions"},
	{"responses", "compact"},
	{"responses"},
	{"messages"},
	{"completions"},
	{"embeddings"},
}

// upstreamVersionSegments 是可剥掉的结尾版本段。
var upstreamVersionSegments = []string{"v1", "v1beta", "v1alpha"}

// NormalizeProtocolBaseURL 把协议型渠道的 base_url 归一化成"版本根"：
// 剥掉结尾的完整端点与版本段（含尾斜杠），使
// https://host ≡ https://host/v1 ≡ https://host/v1/chat/completions 三种
// 写法拼接出同一个上游 URL（api-spec §4.1）。只剥一次端点再剥一次版本段，
// 自定义前缀路径（如 /api/v1 → /api）原样保留；非法 URL 或无 host 原样返回。
func NormalizeProtocolBaseURL(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return baseURL
	}
	trimmedPath := strings.TrimRight(parsed.Path, "/")
	segments := strings.Split(trimmedPath, "/")
	// segments 首元素恒为 ""（前导斜杠）或整段无斜杠时只剩 1 项。
	rest := segments
	if len(rest) > 1 {
		tail := strings.ToLower(rest[len(rest)-1])
		if tail == "compact" && len(rest) > 2 && strings.ToLower(rest[len(rest)-2]) == "responses" {
			rest = rest[:len(rest)-2]
		} else {
			for _, suffix := range upstreamEndpointSuffixes {
				n := len(suffix)
				if len(rest) <= n {
					continue
				}
				matched := true
				for i, seg := range suffix {
					if strings.ToLower(rest[len(rest)-n+i]) != seg {
						matched = false
						break
					}
				}
				if matched {
					rest = rest[:len(rest)-n]
					break
				}
			}
		}
	}
	if len(rest) > 1 {
		last := strings.ToLower(rest[len(rest)-1])
		for _, version := range upstreamVersionSegments {
			if last == version {
				rest = rest[:len(rest)-1]
				break
			}
		}
	}
	parsed.Path = strings.Join(rest, "/")
	return strings.TrimRight(parsed.String(), "/")
}

// NormalizeBaseURLForChannel 按渠道类型决定是否归一化 base_url，
// 供各适配器在拼接上游路径前统一调用。
func NormalizeBaseURLForChannel(baseURL string, channelType int) string {
	if !protocolChannelTypes[channelType] {
		return baseURL
	}
	return NormalizeProtocolBaseURL(baseURL)
}

func GetFullRequestURL(baseURL string, requestURL string, channelType int) string {
	baseURL = NormalizeBaseURLForChannel(baseURL, channelType)
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)

	if strings.HasPrefix(baseURL, "https://gateway.ai.cloudflare.com") {
		switch channelType {
		case constant.ChannelTypeOpenAI:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/v1"))
		case constant.ChannelTypeAzure:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/openai/deployments"))
		}
	}
	return fullRequestURL
}

func SanitizeURLForLog(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	query := parsedURL.Query()
	if len(query) == 0 {
		return rawURL
	}

	changed := false
	for key := range query {
		if isSensitiveURLQueryKey(key) {
			query.Set(key, "***masked***")
			changed = true
		}
	}
	if !changed {
		return rawURL
	}

	parsedURL.RawQuery = query.Encode()
	return parsedURL.String()
}

func isSensitiveURLQueryKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "key",
		"api_key",
		"api-key",
		"apikey",
		"x-api-key",
		"access_token",
		"refresh_token",
		"id_token",
		"token",
		"authorization",
		"auth",
		"client_secret",
		"secret",
		"password",
		"passwd",
		"signature",
		"sig",
		"awsaccesskeyid",
		"x-amz-credential",
		"x-amz-security-token",
		"x-amz-signature":
		return true
	}
	return strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "signature")
}

func GetAPIVersion(c *gin.Context) string {
	query := c.Request.URL.Query()
	apiVersion := query.Get("api-version")
	if apiVersion == "" {
		apiVersion = c.GetString("api_version")
	}
	return apiVersion
}
