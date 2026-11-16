package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 2000 * 1.8446744073686647e19 overflows int64; the constant below reproduces
// that oversized product for the saturation checks.
const overflowingProduct = 2000 * 1.8446744073686647e19

// TestQuotaFromFloat guards that oversized products (e.g. a value multiplied
// by a huge user-supplied count) saturate at the int32 bounds instead of
// wrapping into a negative result. QuotaFromFloat truncates toward zero.
func TestQuotaFromFloat(t *testing.T) {
	assert.Equal(t, 42, QuotaFromFloat(42.4))
	assert.Equal(t, 42, QuotaFromFloat(42.9))
	assert.Equal(t, -42, QuotaFromFloat(-42.9))
	assert.Equal(t, MaxQuota, QuotaFromFloat(float64(math.MaxInt32)+42))
	assert.Equal(t, MaxQuota, QuotaFromFloat(overflowingProduct))
	assert.Equal(t, MinQuota, QuotaFromFloat(-overflowingProduct))
	assert.Equal(t, MaxQuota, QuotaFromFloat(math.Inf(1)))
	assert.Equal(t, MinQuota, QuotaFromFloat(math.Inf(-1)))
	assert.Equal(t, 0, QuotaFromFloat(math.NaN()))
}

// TestQuotaRound checks half-away-from-zero rounding with the same
// saturation policy.
func TestQuotaRound(t *testing.T) {
	assert.Equal(t, 42, QuotaRound(41.5))
	assert.Equal(t, 43, QuotaRound(42.5))
	assert.Equal(t, -43, QuotaRound(-42.5))
	assert.Equal(t, MaxQuota, QuotaRound(float64(math.MaxInt32)+0.5))
	assert.Equal(t, MaxQuota, QuotaRound(overflowingProduct))
	assert.Equal(t, 0, QuotaRound(math.NaN()))
}
