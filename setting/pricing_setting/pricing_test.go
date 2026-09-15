package pricing_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 单价表只做成本折算展示（design-v1 §16.9#7）：这些用例锁住"人民币/百万 token"
// 的换算口径与"未配置即不折算"的语义，避免以后有人把它接回扣费。

func TestEstimateUsesPerMillionPricing(t *testing.T) {
	t.Cleanup(func() { _ = FromString("[]") })
	require.NoError(t, FromString(`[{"model":"m1","input":2.5,"output":10,"cache_read":1.25,"cache_write":3}]`))

	// 1M 输入 + 1M 输出 + 1M 缓存读 + 1M 缓存写 = 2.5 + 10 + 1.25 + 3
	assert.InDelta(t, 16.75, Estimate("m1", 1_000_000, 1_000_000, 1_000_000, 1_000_000), 1e-9)
	// 按比例：100 万 token 单价就是数字本身，所以 1000 token = 千分之一
	assert.InDelta(t, 0.0025, Estimate("m1", 1000, 0, 0, 0), 1e-9)
}

func TestEstimateUnknownModelIsZero(t *testing.T) {
	t.Cleanup(func() { _ = FromString("[]") })
	require.NoError(t, FromString(`[{"model":"m1","input":1}]`))

	assert.Equal(t, 0.0, Estimate("m2", 1_000_000, 0, 0, 0), "未配置单价的模型必须不折算，而不是回退到基座比例价")
	assert.False(t, HasModel("m2"))
	assert.True(t, HasModel("m1"))
}

func TestNormalizeRejectsBadInput(t *testing.T) {
	_, err := Normalize([]ModelPrice{{Model: "  "}})
	assert.Error(t, err, "模型名为空必须报错")

	_, err = Normalize([]ModelPrice{{Model: "m", Input: 1}, {Model: "m", Input: 2}})
	assert.Error(t, err, "模型名重复必须报错（否则整表替换语义不确定）")

	_, err = Normalize([]ModelPrice{{Model: "m", Output: -0.1}})
	assert.Error(t, err, "负单价必须报错")

	ok, err := Normalize([]ModelPrice{{Model: "b"}, {Model: "a", Input: 1}})
	require.NoError(t, err)
	require.Len(t, ok, 2)
	assert.Equal(t, "a", ok[0].Model, "规范化后按模型名排序，保证落库与导出稳定")
}

func TestStringRoundTripIsStable(t *testing.T) {
	t.Cleanup(func() { _ = FromString("[]") })
	require.NoError(t, FromString(`[{"model":"b","input":1},{"model":"a","output":2}]`))

	first := ToString()
	require.NoError(t, FromString(first))
	assert.Equal(t, first, ToString(), "序列化必须稳定（export → import 幂等依赖这一点）")
	assert.Equal(t, `[{"model":"a","output":2},{"model":"b","input":1}]`, first)
}

func TestParseToleratesEmptyAndUnknownFields(t *testing.T) {
	parsed, err := Parse("")
	require.NoError(t, err)
	assert.Empty(t, parsed)

	parsed, err = Parse(`[{"model":"m","input":1,"future_field":true}]`)
	require.NoError(t, err)
	assert.Contains(t, parsed, "m", "未知字段应被忽略，便于向后兼容")
}
