package api

import (
	_ "embed"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed docs/api-guide.md
var apiGuide string

//go:embed docs/doc.html
var docHTML string

//go:embed docs/scalar.html
var scalarHTML string

// Doc GET /doc：面向 AI 的管理 API 手册。
//
// 默认返回 text/markdown（AI 直接读）；浏览器 Accept: text/html 时返回
// 服务端渲染说明页（交互式 OpenAPI UI 见 /doc/ui）。
func Doc(c *gin.Context) {
	c.Header("Cache-Control", "no-cache")
	if strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(docHTML))
		return
	}
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(apiGuide))
}

// LLMs GET /llms.txt：与 /doc 同源的纯文本手册（llms.txt 约定）。
func LLMs(c *gin.Context) {
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(apiGuide))
}

// DocUI GET /doc/ui：交互式 OpenAPI 文档（复用 Scalar，浏览器直接可用）。
func DocUI(c *gin.Context) {
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(scalarHTML))
}
