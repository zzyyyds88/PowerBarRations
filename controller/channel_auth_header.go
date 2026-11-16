package controller

import (
	"fmt"
	"net/http"
)

// GetAuthHeader 返回 Bearer 认证头。
//
// 原先与渠道余额查询同处 controller/channel-billing.go；W7 删除余额查询能力后，
// 仅剩此处被"拉取上游模型列表"（controller/channel.go 的 buildFetchModelsHeaders）使用。
func GetAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	return h
}

// GetClaudeAuthHeader 返回 Anthropic 认证头。
func GetClaudeAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Add("x-api-key", token)
	h.Add("anthropic-version", "2023-06-01")
	return h
}
