package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// Cache 设置静态资源缓存策略。
//
// 关键：**SPA 的 HTML 入口一律 no-cache**。控制台是前端路由，深链（如 /routes、
// /channels）在没有对应静态文件时会回落到 index.html；若按静态资源给一周长缓存，
// 浏览器会把当次的 index.html 缓存下来，之后即使服务端换了新 bundle，页面仍加载
// 旧 JS——表现为"代码已更新但界面没变"（真实踩过：一键固化入口已删仍可见）。
//
// 带内容哈希的 /static/** 才适合长缓存（文件名变化即失效）。
func Cache() func(c *gin.Context) {
	return func(c *gin.Context) {
		uri := c.Request.RequestURI
		if strings.HasPrefix(uri, "/static/") {
			c.Header("Cache-Control", "public, max-age=604800, immutable") // one week
		} else {
			c.Header("Cache-Control", "no-cache")
		}
		c.Header("Cache-Version", "b688f2fb5be447c25e5aa3bd063087a83db32a288bf6a4f35f2d8db310e40b14")
		c.Next()
	}
}
