package console_setting

import "github.com/zzyyyds88/PowerBarRations/setting/config"

type ConsoleSetting struct {
	ApiInfo        string `json:"api_info"`         // 控制台 API 信息 (JSON 数组字符串)
	ApiInfoEnabled bool   `json:"api_info_enabled"` // 是否启用 API 信息面板
}

// 默认配置
var defaultConsoleSetting = ConsoleSetting{
	ApiInfo:        "",
	ApiInfoEnabled: true,
}

// 全局实例
var consoleSetting = defaultConsoleSetting

func init() {
	// 注册到全局配置管理器，键名为 console_setting
	config.GlobalConfig.Register("console_setting", &consoleSetting)
}

// GetConsoleSetting 获取 ConsoleSetting 配置实例
func GetConsoleSetting() *ConsoleSetting {
	return &consoleSetting
}
