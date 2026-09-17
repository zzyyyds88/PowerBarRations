package middleware

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// SystemPerformanceCheck 检查系统性能中间件
func SystemPerformanceCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 仅检查 Relay 接口 (/v1, /v1beta 等)
		// 这里简单判断路径前缀，可以根据实际路由调整
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/v1/messages") {
			if err := checkSystemPerformance(); err != nil {
				c.JSON(err.StatusCode, gin.H{
					"error": err.ToClaudeError(),
				})
				c.Abort()
				return
			}
		} else {
			if err := checkSystemPerformance(); err != nil {
				c.JSON(err.StatusCode, gin.H{
					"error": err.ToOpenAIError(),
				})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// overloadStatusCode 本机资源过载的响应码。
//
// 不能用 503：模型面的 503 是"没有可用渠道"的专属契约（routing-spec §4.2 / ADR 0002），
// 下游据此做 fallback 分类。本机繁忙与路由全挂是两码事，用 503 会让下游误判。
// 取 529（非权威错误码，Cloudflare 用它表达"站点过载"）：语义明确、可重试，
// 与既有的 429（上游限流）和 503（无可用）都不冲突。
//
// 该守卫默认关闭（performance_setting）；即便经管理面重新打开，也不会污染 503 契约。
const overloadStatusCode = 529

// checkSystemPerformance 检查系统性能是否超过阈值
func checkSystemPerformance() *types.NewAPIError {
	config := common.GetPerformanceMonitorConfig()
	if !config.Enabled {
		return nil
	}

	status := common.GetSystemStatus()

	// 检查 CPU
	if config.CPUThreshold > 0 && int(status.CPUUsage) > config.CPUThreshold {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("system cpu overloaded (current: %.1f%%, threshold: %d%%)", status.CPUUsage, config.CPUThreshold),
			"system_cpu_overloaded", overloadStatusCode)
	}

	// 检查内存
	if config.MemoryThreshold > 0 && int(status.MemoryUsage) > config.MemoryThreshold {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("system memory overloaded (current: %.1f%%, threshold: %d%%)", status.MemoryUsage, config.MemoryThreshold),
			"system_memory_overloaded", overloadStatusCode)
	}

	// 检查磁盘
	if config.DiskThreshold > 0 && int(status.DiskUsage) > config.DiskThreshold {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("system disk overloaded (current: %.1f%%, threshold: %d%%)", status.DiskUsage, config.DiskThreshold),
			"system_disk_overloaded", overloadStatusCode)
	}

	return nil
}
