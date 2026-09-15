package middleware

import (
	"strconv"
	"strings"

	"pbr/common"
	"pbr/internal/route"
	"pbr/service"
	"pbr/setting/operation_setting"
)

// PBR 运行态的两处外部依赖在此注入：欠费关键词判定与熔断参数来源。
//
// 放在 middleware 而不是 internal/route，是为了让 internal/route 不反向依赖
// service / setting（也就不会与 middleware 形成导入环）。

func init() {
	// 关键词优先于状态码：欠费类上游以 400 到达，状态码路径捕不到（design-v1 §7.6）。
	// 条目属部署数据，读写一律走 PUT /api/v1/system/options（键 AutomaticDisableKeywords）。
	route.SetQuotaKeywordMatcher(func(message string) bool {
		message = strings.TrimSpace(message)
		if message == "" {
			return false
		}
		keywords := operation_setting.AutomaticDisableKeywords
		if len(keywords) == 0 {
			return false
		}
		lowered := make([]string, 0, len(keywords))
		for _, keyword := range keywords {
			if keyword = strings.ToLower(strings.TrimSpace(keyword)); keyword != "" {
				lowered = append(lowered, keyword)
			}
		}
		matched, _ := service.AcSearch(strings.ToLower(message), lowered, true)
		return matched
	})

	route.SetCircuitSettingsProvider(func() route.CircuitSettings {
		settings := route.DefaultCircuitSettings()
		common.OptionMapRWMutex.RLock()
		defer common.OptionMapRWMutex.RUnlock()
		if raw, ok := common.OptionMap[route.OptionCircuitFailureThreshold]; ok {
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && parsed > 0 {
				settings.FailureThreshold = parsed
			}
		}
		if raw, ok := common.OptionMap[route.OptionCircuitOpenSeconds]; ok {
			if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && parsed >= 0 {
				settings.OpenSeconds = parsed
			}
		}
		if raw, ok := common.OptionMap[route.OptionCircuitMaxOpenSeconds]; ok {
			if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && parsed > 0 {
				settings.MaxOpenSeconds = parsed
			}
		}
		return settings
	})
}
