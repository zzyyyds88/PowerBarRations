package common

import "math"

// 额度计费链已按 design-v1 §1.3 物理删除，原"quota 钳制 + 审计"体系
// （QuotaClamp、Strict/Checked/Wallet 系列）随之移除。这里仅保留估算与非
// 计费路径仍在使用的两个通用饱和转换工具（token 估算、时长折算）。
const (
	MaxQuota = math.MaxInt32
	MinQuota = math.MinInt32
)

func saturateQuota(value float64) int {
	switch {
	case math.IsNaN(value):
		return 0
	case value > float64(MaxQuota):
		return MaxQuota
	case value < float64(MinQuota):
		return MinQuota
	default:
		return int(value)
	}
}

// QuotaFromFloat converts a computed value to int, truncating toward zero,
// with saturation at the int32 bounds.
func QuotaFromFloat(value float64) int {
	return saturateQuota(value)
}

// QuotaRound converts a float64 value to int using half-away-from-zero
// rounding, with saturation at the int32 bounds.
func QuotaRound(value float64) int {
	return saturateQuota(math.Round(value))
}
