package middleware

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"pbr/common"
)

const RouteTagKey = "route_tag"

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

func SetUpLogger(server *gin.Engine) {
	server.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		var requestID string
		if param.Keys != nil {
			requestID, _ = param.Keys[common.RequestIdKey].(string)
		}
		tag, _ := param.Keys[RouteTagKey].(string)
		if tag == "" {
			tag = "web"
		}
		// gin 的 LogFormatterParams.Path 形如 path?rawquery，查询串里的 ?key=、
		// ?token= 等凭据会因此进访问日志。这里只打印去掉查询串的路径做纵深防御。
		path := param.Path
		if idx := strings.IndexByte(path, '?'); idx >= 0 {
			path = path[:idx]
		}
		return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			tag,
			requestID,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			path,
		)
	}))
}
