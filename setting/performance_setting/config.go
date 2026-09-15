package performance_setting

import (
	"os"
	"strconv"
	"strings"

	"pbr/common"
	"pbr/setting/config"
)

// PerformanceSetting 性能设置配置
type PerformanceSetting struct {
	// DiskCacheEnabled 是否启用磁盘缓存（磁盘换内存）
	DiskCacheEnabled bool `json:"disk_cache_enabled"`
	// DiskCacheThresholdMB 触发磁盘缓存的请求体大小阈值（MB）
	DiskCacheThresholdMB int `json:"disk_cache_threshold_mb"`
	// DiskCacheMaxSizeMB 磁盘缓存最大总大小（MB）
	DiskCacheMaxSizeMB int `json:"disk_cache_max_size_mb"`
	// DiskCachePath 磁盘缓存目录
	DiskCachePath string `json:"disk_cache_path"`

	// MonitorEnabled 是否启用性能监控
	MonitorEnabled bool `json:"monitor_enabled"`
	// MonitorCPUThreshold CPU 使用率阈值（%）
	MonitorCPUThreshold int `json:"monitor_cpu_threshold"`
	// MonitorMemoryThreshold 内存使用率阈值（%）
	MonitorMemoryThreshold int `json:"monitor_memory_threshold"`
	// MonitorDiskThreshold 磁盘使用率阈值（%）
	MonitorDiskThreshold int `json:"monitor_disk_threshold"`
}

// 默认配置
var performanceSetting = PerformanceSetting{
	DiskCacheEnabled:     false,
	DiskCacheThresholdMB: 10,   // 超过 10MB 使用磁盘缓存
	DiskCacheMaxSizeMB:   1024, // 最大 1GB 磁盘缓存
	DiskCachePath:        "",   // 空表示使用系统临时目录

	// PBR（单用户自用网关）：默认关闭本机资源过载拒绝。基座默认开启（CPU>90% 直接 503），
	// 但 PBR 的 503 契约是"没有可用渠道"——把本机繁忙也映射成 503 会让下游把"网关忙"
	// 误判成"路由全挂"（真实渠道实测在忙机器上被 99% CPU 直接拒绝）。
	// 需要时用 PERFORMANCE_MONITOR_ENABLED=true 打开，阈值可用 PERFORMANCE_*_THRESHOLD 调整。
	MonitorEnabled:         envBool("PERFORMANCE_MONITOR_ENABLED", false),
	MonitorCPUThreshold:    envInt("PERFORMANCE_CPU_THRESHOLD", 90),
	MonitorMemoryThreshold: envInt("PERFORMANCE_MEMORY_THRESHOLD", 90),
	MonitorDiskThreshold:   envInt("PERFORMANCE_DISK_THRESHOLD", 95),
}

func envBool(name string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("performance_setting", &performanceSetting)
	// 同步初始配置到 common 包
	syncToCommon()
}

// syncToCommon 将配置同步到 common 包
func syncToCommon() {
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		Enabled:     performanceSetting.DiskCacheEnabled,
		ThresholdMB: performanceSetting.DiskCacheThresholdMB,
		MaxSizeMB:   performanceSetting.DiskCacheMaxSizeMB,
		Path:        performanceSetting.DiskCachePath,
	})

	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{
		Enabled:         performanceSetting.MonitorEnabled,
		CPUThreshold:    performanceSetting.MonitorCPUThreshold,
		MemoryThreshold: performanceSetting.MonitorMemoryThreshold,
		DiskThreshold:   performanceSetting.MonitorDiskThreshold,
	})
}

// GetPerformanceSetting 获取性能设置
func GetPerformanceSetting() *PerformanceSetting {
	return &performanceSetting
}

// UpdateAndSync 更新配置并同步到 common 包
// 当配置从数据库加载后，需要调用此函数同步
func UpdateAndSync() {
	syncToCommon()
}

// GetCacheStats 获取缓存统计信息（代理到 common 包）
func GetCacheStats() common.DiskCacheStats {
	return common.GetDiskCacheStats()
}

// ResetStats 重置统计信息
func ResetStats() {
	common.ResetDiskCacheStats()
}
