package middleware

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/common"
)

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	// 不允许携带凭据：AllowAllOrigins 与 AllowCredentials 同时为 true 时，
	// 响应会同时下发 Access-Control-Allow-Origin: * 与
	// Access-Control-Allow-Credentials: true——浏览器会直接拒绝该组合；
	// 更严重的是它等于把带 Cookie 的跨域调用对任意站点放开，管理面会话
	// Cookie 会因此暴露给恶意页面。控制台同源访问不受影响（同源不发预检）。
	config.AllowCredentials = false
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	return cors.New(config)
}

func Version() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-PBR-Version", common.Version)
		c.Next()
	}
}
