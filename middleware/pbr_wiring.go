package middleware

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/service"
	"github.com/zzyyyds88/PowerBarRations/setting/operation_setting"
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
		if raw, ok := common.OptionMap[route.OptionCircuitRollingMinSamples]; ok {
			if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && parsed > 0 {
				settings.RollingMinSamples = parsed
			}
		}
		if raw, ok := common.OptionMap[route.OptionCircuitRollingFailureRate]; ok {
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && parsed > 0 && parsed <= 1 {
				settings.RollingFailureRate = parsed
			}
		}
		return settings
	})

	model.SetLaneDefaultsProvider(configuredLaneDefaults)
}

// laneDefaultsCache 缓存解析结果：DefaultLaneRelayConfig 在每次请求的
// Normalize/EffectiveConfig 里都会被调用，不能每次都做 JSON 解析。
var laneDefaultsCache struct {
	mu  sync.Mutex
	raw string
	cfg model.LaneRelayConfig
}

// configuredLaneDefaults 读取 system/options 的默认六键（option 键 PBRLaneDefaults）。
//
// 逐字段判"是否存在"而不是"是否为零"：retry_interval 与 affinity 合法取 0，
// 用零值判断会把"没配这个字段"误当成"显式配 0"。非法/缺省一律回落内置默认。
func configuredLaneDefaults() model.LaneRelayConfig {
	common.OptionMapRWMutex.RLock()
	raw := strings.TrimSpace(common.OptionMap[model.OptionLaneDefaults])
	common.OptionMapRWMutex.RUnlock()

	laneDefaultsCache.mu.Lock()
	defer laneDefaultsCache.mu.Unlock()
	if raw == laneDefaultsCache.raw && raw != "" {
		return laneDefaultsCache.cfg
	}

	cfg := model.BuiltinLaneRelayConfig()
	if raw != "" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &fields); err == nil {
			readInt := func(key string, apply func(int)) {
				value, ok := fields[key]
				if !ok {
					return
				}
				var parsed int
				if err := json.Unmarshal(value, &parsed); err == nil && parsed >= 0 {
					apply(parsed)
				}
			}
			readInt("member_max_attempts", func(v int) {
				if v > 0 {
					cfg.MemberMaxAttempts = v
				}
			})
			readInt("member_retry_interval_seconds", func(v int) {
				cfg.MemberRetryIntervalSeconds = v
			})
			readInt("member_non_stream_response_timeout_seconds", func(v int) {
				if v > 0 {
					cfg.MemberNonStreamResponseTimeoutSeconds = v
				}
			})
			readInt("member_stream_first_event_timeout_seconds", func(v int) {
				if v > 0 {
					cfg.MemberStreamFirstEventTimeoutSeconds = v
				}
			})
			readInt("member_cooldown_seconds", func(v int) {
				if v > 0 {
					cfg.MemberCooldownSeconds = v
				}
			})
			readInt("member_affinity_seconds", func(v int) {
				cfg.MemberAffinitySeconds = v
			})
		}
	}
	laneDefaultsCache.raw = raw
	laneDefaultsCache.cfg = cfg
	return cfg
}
