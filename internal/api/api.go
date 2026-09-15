// Package api 实现 PowerBarRations 管理 API（`/api/v1/*`）。
//
// 契约依据 docs/api-spec-v1.md；本波（W1）只实现路由与配置所需的最小子集：
// setup/login、channels、lanes、models、routes。观测、令牌、导入导出、OpenAPI
// 等按 W3 补齐，handler 与错误码沿用同一套约定，避免后续改契约。
package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pbr/internal/apierr"

	"github.com/gin-gonic/gin"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

// rfc3339 把 Unix 秒格式化为 API 约定的 RFC3339 UTC。
func rfc3339(sec int64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

// pageParams 解析分页参数。
func pageParams(c *gin.Context) (limit int, cursor string, err error) {
	limit = defaultLimit
	if raw := c.Query("limit"); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed <= 0 {
			return 0, "", &apiError{code: apierr.CodeValidationFailed, message: "limit must be a positive integer"}
		}
		limit = min(parsed, maxLimit)
	}
	return limit, decodeCursor(c.Query("cursor")), nil
}

type apiError struct {
	code    string
	message string
}

func (e *apiError) Error() string { return e.message }

func writeAPIError(c *gin.Context, err error) {
	if ae, ok := err.(*apiError); ok {
		apierr.Write(c, http.StatusBadRequest, ae.code, ae.message, "")
		return
	}
	apierr.Write(c, http.StatusInternalServerError, "internal_error", err.Error(), "")
}

func encodeCursor(name string) string {
	if name == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

func decodeCursor(cursor string) string {
	if cursor == "" {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return ""
	}
	return string(decoded)
}

// dryRun 是否只做校验与 diff、不落库。
func dryRun(c *gin.Context) bool {
	v := strings.ToLower(strings.TrimSpace(c.Query("dry_run")))
	return v == "1" || v == "true" || v == "yes"
}

// dryRunResult 是 dry_run 的响应体形态（api-spec §2.3）。
//
// 同时写一条 dry_run=true 的审计：api-spec §2.6 的审计字段清单含 `dry_run`，
// 若干跑不留痕，该字段恒为 false、成为死字段，且"谁在何时探测过破坏性变更"
// 事后无从追查（§2.6 要求所有变更写 audit_logs）。
func dryRunResult(c *gin.Context, resource, action, name string) {
	writeAudit(c, action, resource, name)
	diff := gin.H{"add": []string{}, "update": []string{}, "remove": []string{}}
	switch action {
	case "add":
		diff["add"] = []string{name}
	case "update":
		diff["update"] = []string{name}
	case "remove":
		diff["remove"] = []string{name}
	}
	c.JSON(http.StatusOK, gin.H{
		"dry_run": true,
		"valid":   true,
		"diff":    gin.H{resource: diff},
	})
}

// jsonUnmarshal 容错解析（失败不致命，调用方自行回落默认值）。
func jsonUnmarshal(raw string, out any) error {
	return json.Unmarshal([]byte(raw), out)
}

// jsonObject 解析 JSON 对象字段；空字符串返回 nil。
func jsonObject(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
