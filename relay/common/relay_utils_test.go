package common

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeURLForLogMasksSensitiveQueryValues(t *testing.T) {
	rawURL := "https://example.test/v1beta/models/gemini:streamGenerateContent?alt=sse&key=sk-secret&access_token=ya29-secret&api-version=2024-02-01"

	got := SanitizeURLForLog(rawURL)

	assert.NotContains(t, got, "sk-secret")
	assert.NotContains(t, got, "ya29-secret")
	parsedURL, err := url.Parse(got)
	require.NoError(t, err)
	query := parsedURL.Query()
	assert.Equal(t, "***masked***", query.Get("key"))
	assert.Equal(t, "***masked***", query.Get("access_token"))
	assert.Equal(t, "sse", query.Get("alt"))
	assert.Equal(t, "2024-02-01", query.Get("api-version"))
}

func TestSanitizeURLForLogMasksAWSAndSecretLikeQueryKeys(t *testing.T) {
	rawURL := "https://example.test/path?X-Amz-Credential=credential&X-Amz-Signature=signature&session_token=session&client_secret=secret&model=gpt-test"

	got := SanitizeURLForLog(rawURL)

	assert.NotContains(t, got, "X-Amz-Credential=credential")
	assert.NotContains(t, got, "X-Amz-Signature=signature")
	assert.NotContains(t, got, "session_token=session")
	assert.NotContains(t, got, "client_secret=secret")
	parsedURL, err := url.Parse(got)
	require.NoError(t, err)
	query := parsedURL.Query()
	assert.Equal(t, "***masked***", query.Get("X-Amz-Credential"))
	assert.Equal(t, "***masked***", query.Get("X-Amz-Signature"))
	assert.Equal(t, "***masked***", query.Get("session_token"))
	assert.Equal(t, "***masked***", query.Get("client_secret"))
	assert.Equal(t, "gpt-test", query.Get("model"))
}

func TestSanitizeURLForLogKeepsURLWithoutSensitiveQuery(t *testing.T) {
	rawURL := "https://example.test/v1/chat/completions?api-version=2024-02-01&alt=sse"

	got := SanitizeURLForLog(rawURL)

	assert.Equal(t, rawURL, got)
}

func TestNormalizeProtocolBaseURLStripsVersionAndEndpointTails(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://host", "https://host"},
		{"https://host/", "https://host"},
		{"https://host/v1", "https://host"},
		{"https://host/v1/", "https://host"},
		{"https://host/v1/chat/completions", "https://host"},
		{"https://host/v1/responses", "https://host"},
		{"https://host/v1/responses/compact", "https://host"},
		{"https://host/v1/messages", "https://host"},
		{"https://host/v1/completions", "https://host"},
		{"https://host/v1/embeddings", "https://host"},
		{"https://host/v1beta", "https://host"},
		{"https://host/v1beta/models/gemini:generateContent", "https://host/v1beta/models/gemini:generateContent"},
		{"https://proxy.example/api/v1", "https://proxy.example/api"},
		{"https://proxy.example/api/v1/chat/completions", "https://proxy.example/api"},
		{"https://v1", "https://v1"},
		{"", ""},
		{"not a url", "not a url"},
		{"https://host/v1?key=1", "https://host?key=1"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, NormalizeProtocolBaseURL(tc.in), "input %q", tc.in)
	}
}

func TestGetFullRequestURLEquivalentForAllBaseURLSpellings(t *testing.T) {
	// api-spec §4.1：host ≡ host/v1 ≡ host/v1/chat/completions 三种写法
	// 拼接结果必须一致，不出现 /v1/v1。
	want := "https://vendor.example/v1/chat/completions"
	for _, base := range []string{
		"https://vendor.example",
		"https://vendor.example/",
		"https://vendor.example/v1",
		"https://vendor.example/v1/chat/completions",
	} {
		assert.Equal(t, want, GetFullRequestURL(base, "/v1/chat/completions", 1), "base %q", base)
	}
	// 非协议型渠道不归一化（旧数据行为不变）。
	assert.Equal(t,
		"https://vendor.example/v1/v1/chat/completions",
		GetFullRequestURL("https://vendor.example/v1", "/v1/chat/completions", 8+1))
}
