package channel

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/zzyyyds88/PowerBarRations/logger"
)

// 服务商硬性要求（provider quirks）。
//
// 背景：4 种上游协议描述了"请求长什么样"，但描述不了"这家服务商还额外要什么"。
// 最典型的是 OpenCode Zen/Go：即使 OpenAI 兼容协议完全正确，缺少
// `x-opencode-session` 也会被上游 400 拒绝（"cannot be routed efficiently"）。
// 此前只能由用户在渠道的 param_override 里手写 pass_headers + set_header，
// 属于**用户替网关适配上游**，既容易漏、又不可发现。
//
// 这里把已知的、与账号无关的硬性要求内置到网关：在组装上游请求时按目标 host
// （必要时叠加 path 前缀）匹配，自动补齐。用户显式配置的 header_override 仍然
// 后应用、优先级更高（见 DoApiRequest 的顺序）。
//
// 收录标准（避免"猜"）：
//   - 必须是上游文档或实测确认的硬性要求，缺了就请求失败；
//   - 必须与具体账号/密钥无关（账号级差异应该留在渠道配置里）；
//   - 值必须是安全常量或"客户端带什么就转发什么"，不能注入凭据。
type providerQuirkHeader struct {
	// Name 上游要求的请求头名（大小写不敏感）。
	Name string
	// Fallback 客户端没带该头时写入的兜底值；空串表示不写。
	Fallback string
	// PassThrough 客户端带了该头时按原值转发（优先于 Fallback）。
	PassThrough bool
}

type providerQuirk struct {
	ID    string
	Hosts []string
	// PathPrefix 同一 host 下按路径区分的服务（如 opencode.ai 的 /zen/go 与 /zen）。
	// 空串表示只按 host 匹配。
	PathPrefix string
	Headers    []providerQuirkHeader
}

// providerQuirks 是内置的服务商适配清单。
//
// 新增条目前先确认三件事：上游是否真的强制、是否与账号无关、兜底值是否安全。
var providerQuirks = []providerQuirk{
	{
		// OpenCode Go / Zen：官方 CLI 用 x-opencode-session 标识会话；网关不带
		// 会被 400 拒绝。客户端（opencode CLI）通常会带，原样转发即可；
		// 不带时用固定值兜底——实测 pbr 侧用固定值也能正常路由。
		ID:         "opencode-go",
		Hosts:      []string{"opencode.ai"},
		PathPrefix: "/zen/go",
		Headers: []providerQuirkHeader{
			{Name: "x-opencode-session", Fallback: "pbr-gateway", PassThrough: true},
		},
	},
	{
		ID:         "opencode-zen",
		Hosts:      []string{"opencode.ai"},
		PathPrefix: "/zen",
		Headers: []providerQuirkHeader{
			{Name: "x-opencode-session", Fallback: "pbr-gateway", PassThrough: true},
		},
	},
}

// matchProviderQuirk 返回该 URL 命中的服务商要求；未命中返回 nil。
func matchProviderQuirk(target *url.URL) *providerQuirk {
	if target == nil {
		return nil
	}
	host := strings.ToLower(target.Hostname())
	path := strings.ToLower(target.Path)
	for i := range providerQuirks {
		quirk := &providerQuirks[i]
		if !hostMatches(quirk.Hosts, host) {
			continue
		}
		if quirk.PathPrefix != "" && !strings.HasPrefix(path, quirk.PathPrefix) {
			continue
		}
		return quirk
	}
	return nil
}

func hostMatches(hosts []string, host string) bool {
	for _, item := range hosts {
		if strings.EqualFold(strings.TrimSpace(item), host) {
			return true
		}
	}
	return false
}

// applyProviderQuirks 给上游请求补齐服务商硬性要求的请求头。
//
// 只补**缺失**的头：适配器/用户已经设置的值不覆盖；客户端带了且允许透传的
// 用客户端值，否则用兜底常量。必须在 Header Override 之前调用，使渠道显式
// 配置始终拥有最终优先级。
func applyProviderQuirks(req *http.Request, c *gin.Context) {
	if req == nil {
		return
	}
	applyProviderQuirksToHeader(req.URL, req.Header, c)
}

// applyProviderQuirksToHeader 是 applyProviderQuirks 的纯 header 版本，
// 供 WebSocket（握手头先收集再 Dial）等不走 *http.Request 的路径复用。
func applyProviderQuirksToHeader(
	target *url.URL,
	headers http.Header,
	c *gin.Context,
) {
	if target == nil || headers == nil {
		return
	}
	quirk := matchProviderQuirk(target)
	if quirk == nil {
		return
	}

	applied := make([]string, 0, len(quirk.Headers))
	for _, header := range quirk.Headers {
		if strings.TrimSpace(headers.Get(header.Name)) != "" {
			continue
		}
		value := ""
		if header.PassThrough && c != nil && c.Request != nil {
			value = strings.TrimSpace(c.Request.Header.Get(header.Name))
		}
		if value == "" {
			value = header.Fallback
		}
		if value == "" {
			continue
		}
		headers.Set(header.Name, value)
		applied = append(applied, header.Name)
	}
	if len(applied) > 0 {
		logger.LogDebug(c, "provider quirk %s applied headers: %s",
			quirk.ID, strings.Join(applied, ", "))
	}
}
