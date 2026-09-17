package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"pbr/common"
	"pbr/internal/apierr"
	"pbr/internal/route"
	"pbr/model"
	"pbr/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// 全局选项（api-spec §5.1）。W2 只暴露与容错/熔断相关的键；其余系统选项在 W3 补齐。
//
// 读写在迁移基座的 options 表上（PUT 落库 + 进 common.OptionMap），所以重启后仍在。
// 关键词条目属部署数据，只经 API 读写，不进仓库（design-v1 §7.6）。

type systemOptions struct {
	CircuitFailureThreshold  float64  `json:"circuit_failure_threshold"`
	CircuitOpenSeconds       int      `json:"circuit_open_seconds"`
	CircuitMaxOpenSeconds    int      `json:"circuit_max_open_seconds"`
	LogRetentionDays         int      `json:"log_retention_days"`
	ProbeConcurrency         int      `json:"probe_concurrency"`
	AutomaticEnableChannel   bool     `json:"automatic_enable_channel_enabled"`
	AutomaticDisableChannel  bool     `json:"automatic_disable_channel_enabled"`
	AutomaticDisableKeywords []string `json:"automatic_disable_keywords"`
	// LaneDefaults 默认六键：新建/一键固化车道写入的初值，也是车道未显式配置时的回落值。
	// 指针用于区分"配置文件里没有这个字段"（保持原值）与"显式给了值"。
	LaneDefaults *model.LaneRelayConfig `json:"lane_defaults,omitempty"`
}

type systemOptionsPatch struct {
	CircuitFailureThreshold  *float64               `json:"circuit_failure_threshold"`
	CircuitOpenSeconds       *int                   `json:"circuit_open_seconds"`
	CircuitMaxOpenSeconds    *int                   `json:"circuit_max_open_seconds"`
	LogRetentionDays         *int                   `json:"log_retention_days"`
	ProbeConcurrency         *int                   `json:"probe_concurrency"`
	AutomaticEnableChannel   *bool                  `json:"automatic_enable_channel_enabled"`
	AutomaticDisableChannel  *bool                  `json:"automatic_disable_channel_enabled"`
	AutomaticDisableKeywords *[]string              `json:"automatic_disable_keywords"`
	LaneDefaults             *model.LaneRelayConfig `json:"lane_defaults"`
}

// validateLaneDefaults 校验默认六键：四个"必须为正"的时长/预算、两个允许为 0 的间隔。
func validateLaneDefaults(cfg *model.LaneRelayConfig) error {
	if cfg == nil {
		return nil
	}
	switch {
	case cfg.MemberMaxAttempts <= 0:
		return errors.New("lane_defaults.member_max_attempts must be > 0")
	case cfg.MemberRetryIntervalSeconds < 0:
		return errors.New("lane_defaults.member_retry_interval_seconds must be >= 0")
	case cfg.MemberNonStreamResponseTimeoutSeconds <= 0:
		return errors.New("lane_defaults.member_non_stream_response_timeout_seconds must be > 0")
	case cfg.MemberStreamFirstEventTimeoutSeconds <= 0:
		return errors.New("lane_defaults.member_stream_first_event_timeout_seconds must be > 0")
	case cfg.MemberCooldownSeconds <= 0:
		return errors.New("lane_defaults.member_cooldown_seconds must be > 0")
	case cfg.MemberAffinitySeconds < 0:
		return errors.New("lane_defaults.member_affinity_seconds must be >= 0")
	}
	return nil
}

// encodeLaneDefaults 序列化默认六键（写入 option 表）。
func encodeLaneDefaults(cfg *model.LaneRelayConfig) (string, error) {
	if cfg == nil {
		return "", errors.New("lane_defaults is required")
	}
	if err := validateLaneDefaults(cfg); err != nil {
		return "", err
	}
	raw, err := json.Marshal(cfg.Normalize())
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// currentSystemOptions 汇总当前生效的选项（导出与回读共用）。
//
// 关键词在此**归一化**（trim + 小写）：写入侧 AutomaticDisableKeywordsFromString
// 就是小写化的，而内置默认值是混合大小写。若读侧原样输出，则
//
//	全新实例导出（混合大小写）→ 导入（小写）→ 再导出（小写）
//
// 会让 `export → import(dry_run)` 在第一次往返时就报 options 变更，
// 违反 design-v1 §12.4"export→import(dry_run) diff 为空"的验收口径。
// 关键词本来也是与"小写化后的上游错误信息"做不区分大小写匹配，小写即规范形。
func currentSystemOptions() systemOptions {
	settings := route.CurrentCircuitSettings()
	keywords := make([]string, 0, len(operation_setting.AutomaticDisableKeywords))
	for _, keyword := range operation_setting.AutomaticDisableKeywords {
		if keyword = strings.ToLower(strings.TrimSpace(keyword)); keyword != "" {
			keywords = append(keywords, keyword)
		}
	}
	laneDefaults := model.DefaultLaneRelayConfig()
	return systemOptions{
		CircuitFailureThreshold:  settings.FailureThreshold,
		CircuitOpenSeconds:       settings.OpenSeconds,
		CircuitMaxOpenSeconds:    settings.MaxOpenSeconds,
		LogRetentionDays:         CurrentLogRetentionDays(),
		ProbeConcurrency:         CurrentProbeConcurrency(),
		AutomaticEnableChannel:   common.AutomaticEnableChannelEnabled,
		AutomaticDisableChannel:  common.AutomaticDisableChannelEnabled,
		AutomaticDisableKeywords: keywords,
		LaneDefaults:             &laneDefaults,
	}
}

// CurrentLogRetentionDays 明细日志保留天数（design-v1 §16.5，默认 30）。
func CurrentLogRetentionDays() int {
	common.OptionMapRWMutex.RLock()
	raw, ok := common.OptionMap[route.OptionLogRetentionDays]
	common.OptionMapRWMutex.RUnlock()
	if !ok {
		return route.DefaultLogRetentionDays
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed <= 0 {
		return route.DefaultLogRetentionDays
	}
	return parsed
}

// applySystemOptions 全量写入选项（导入用）。
func applySystemOptions(options systemOptions) error {
	updates := map[string]string{
		route.OptionCircuitFailureThreshold: strconv.FormatFloat(options.CircuitFailureThreshold, 'f', -1, 64),
		route.OptionCircuitOpenSeconds:      strconv.Itoa(options.CircuitOpenSeconds),
		route.OptionCircuitMaxOpenSeconds:   strconv.Itoa(options.CircuitMaxOpenSeconds),
		"AutomaticEnableChannelEnabled":     strconv.FormatBool(options.AutomaticEnableChannel),
		"AutomaticDisableChannelEnabled":    strconv.FormatBool(options.AutomaticDisableChannel),
	}
	if options.LogRetentionDays > 0 {
		updates[route.OptionLogRetentionDays] = strconv.Itoa(options.LogRetentionDays)
	}
	if options.ProbeConcurrency > 0 {
		updates[route.OptionProbeConcurrency] = strconv.Itoa(options.ProbeConcurrency)
	}
	if options.LaneDefaults != nil {
		encoded, err := encodeLaneDefaults(options.LaneDefaults)
		if err != nil {
			return err
		}
		updates[model.OptionLaneDefaults] = encoded
	}
	keywords := make([]string, 0, len(options.AutomaticDisableKeywords))
	for _, keyword := range options.AutomaticDisableKeywords {
		if keyword = strings.TrimSpace(keyword); keyword != "" {
			keywords = append(keywords, keyword)
		}
	}
	updates["AutomaticDisableKeywords"] = strings.Join(keywords, "\n")
	for key, value := range updates {
		if err := model.UpdateOption(key, value); err != nil {
			return err
		}
	}
	return nil
}

// GetSystemOptions GET /api/v1/system/options
func GetSystemOptions(c *gin.Context) {
	c.JSON(http.StatusOK, currentSystemOptions())
}

// PutSystemOptions PUT /api/v1/system/options
//
// 未在 body 中出现的键保持不变——避免"只想改熔断阈值"的调用把关键词表清空。
func PutSystemOptions(c *gin.Context) {
	var patch systemOptionsPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	updates := map[string]string{}
	if patch.CircuitFailureThreshold != nil {
		if *patch.CircuitFailureThreshold <= 0 {
			apierr.Validation(c, "circuit_failure_threshold must be > 0")
			return
		}
		updates[route.OptionCircuitFailureThreshold] = strconv.FormatFloat(*patch.CircuitFailureThreshold, 'f', -1, 64)
	}
	if patch.CircuitOpenSeconds != nil {
		if *patch.CircuitOpenSeconds < 0 {
			apierr.Validation(c, "circuit_open_seconds must be >= 0")
			return
		}
		updates[route.OptionCircuitOpenSeconds] = strconv.Itoa(*patch.CircuitOpenSeconds)
	}
	if patch.CircuitMaxOpenSeconds != nil {
		if *patch.CircuitMaxOpenSeconds <= 0 {
			apierr.Validation(c, "circuit_max_open_seconds must be > 0")
			return
		}
		updates[route.OptionCircuitMaxOpenSeconds] = strconv.Itoa(*patch.CircuitMaxOpenSeconds)
	}
	if patch.LogRetentionDays != nil {
		if *patch.LogRetentionDays <= 0 {
			apierr.Validation(c, "log_retention_days must be > 0")
			return
		}
		updates[route.OptionLogRetentionDays] = strconv.Itoa(*patch.LogRetentionDays)
	}
	if patch.ProbeConcurrency != nil {
		if *patch.ProbeConcurrency <= 0 {
			apierr.Validation(c, "probe_concurrency must be > 0")
			return
		}
		updates[route.OptionProbeConcurrency] = strconv.Itoa(*patch.ProbeConcurrency)
	}
	if patch.AutomaticEnableChannel != nil {
		updates["AutomaticEnableChannelEnabled"] = strconv.FormatBool(*patch.AutomaticEnableChannel)
	}
	if patch.AutomaticDisableChannel != nil {
		updates["AutomaticDisableChannelEnabled"] = strconv.FormatBool(*patch.AutomaticDisableChannel)
	}
	if patch.AutomaticDisableKeywords != nil {
		keywords := make([]string, 0, len(*patch.AutomaticDisableKeywords))
		for _, keyword := range *patch.AutomaticDisableKeywords {
			if keyword = strings.TrimSpace(keyword); keyword != "" {
				keywords = append(keywords, keyword)
			}
		}
		updates["AutomaticDisableKeywords"] = strings.Join(keywords, "\n")
	}
	if patch.LaneDefaults != nil {
		encoded, err := encodeLaneDefaults(patch.LaneDefaults)
		if err != nil {
			apierr.Validation(c, err.Error())
			return
		}
		updates[model.OptionLaneDefaults] = encoded
	}
	if len(updates) == 0 {
		apierr.Validation(c, "no updatable field provided")
		return
	}
	if dryRun(c) {
		// 留 dry_run=true 的审计痕迹（§2.6 审计字段含 dry_run）
		writeAudit(c, "update", "system_options", "system_options")
		c.JSON(http.StatusOK, gin.H{"dry_run": true, "valid": true, "diff": gin.H{"system_options": gin.H{"update": sortedKeys(updates)}}})
		return
	}
	before := currentSystemOptions()
	// 逐键写库并同步内存（沿用基座的 options 机制，重启后自动加载）。
	for key, value := range updates {
		if err := model.UpdateOption(key, value); err != nil {
			writeAPIError(c, err)
			return
		}
	}
	writeAudit(c, "update", "system_options", "system_options", before, currentSystemOptions())
	GetSystemOptions(c)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
