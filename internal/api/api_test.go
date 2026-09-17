package api

import (
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 源码审计修复的回归用例（管理面）。

// 游标必须能被自己读回来：写入端与读取端编码不一致会让翻页永远停在第一页。
// 非法游标必须报错（静默回退第一页会让调用方陷入翻页死循环，api-spec §2.4）。
func TestCursorRoundTrip(t *testing.T) {
	for _, raw := range []string{"3001", "1", "42", "999999999"} {
		encoded := encodeCursor(raw)
		require.NotEmpty(t, encoded)
		decoded, err := decodeCursor(encoded)
		require.NoError(t, err)
		assert.Equal(t, raw, decoded, "裸 id 与 base64 混用会让分页失效")
	}

	decoded, err := decodeCursor("")
	require.NoError(t, err)
	assert.Empty(t, decoded)

	if _, err := decodeCursor("!!!not-base64!!!"); err == nil {
		t.Fatal("非法游标必须报错，不得静默当成空串")
	}
}

// 渠道 key_prefix 必须是"展示前缀"，短密钥不得整串回显。
func TestChannelResponseNeverEchoesWholeKey(t *testing.T) {
	cases := []string{"sk-1234", "a", "ab", "shortkey", "sk-very-long-secret-key-value"}
	for _, key := range cases {
		channel := &model.Channel{Name: "c", Key: key}
		response := channelResponse(channel)
		prefix, _ := response["key_prefix"].(string)
		if key == "" {
			assert.Empty(t, prefix)
			continue
		}
		assert.NotEqual(t, key, prefix, "密钥明文不得作为前缀回显：%q", key)
		assert.True(t, strings.HasPrefix(key, prefix), "前缀应是原文的前缀")
		assert.Less(t, len(prefix), len(key)+1)
	}
}

// 时间过滤接受 RFC3339 也接受 Unix 秒；非法输入必须报错而不是静默忽略
// （静默忽略会把超出时间窗的数据返回给调用方）。
func TestParseTimeQuery(t *testing.T) {
	value, err := parseTimeQuery("2026-09-14T00:00:00Z")
	require.NoError(t, err)
	assert.EqualValues(t, 1789344000, value)

	value, err = parseTimeQuery("1789344000")
	require.NoError(t, err)
	assert.EqualValues(t, 1789344000, value)

	value, err = parseTimeQuery("")
	require.NoError(t, err)
	assert.Zero(t, value)

	if _, err := parseTimeQuery("昨天"); err == nil {
		t.Fatal("非法时间必须报错，不能被静默忽略")
	}
}

// 渠道响应里的 updated_at 必须是真实的最后修改时间，不能复用 created_at
// （复用会让依赖它做变更检测/缓存失效的调用方永远看不到更新）。
func TestChannelResponseReportsRealUpdatedAt(t *testing.T) {
	created := int64(1700000000)
	updated := created + 3600
	ch := &model.Channel{Name: "c", CreatedTime: created, UpdatedAt: updated}

	resp := channelResponse(ch)
	assert.NotEqual(t, resp["created_at"], resp["updated_at"],
		"updated_at 不得等于 created_at（改渠道后时间戳必须前进）")
	assert.Equal(t, rfc3339(updated), resp["updated_at"])
	assert.Equal(t, rfc3339(created), resp["created_at"])
}

// 审计时间必须是对外统一的 RFC3339（api-spec §2.5），不能是 Unix 秒。
// 此前 /audit 直接透传模型（Ts int64），而 /logs 已用 RFC3339，同一契约
// 两种时间表示会让按 §2.5 解析的调用方拿到错误年份。
func TestAuditTimestampsAreRFC3339(t *testing.T) {
	ts := int64(1789344000)
	assert.Equal(t, "2026-09-14T00:00:00Z", rfc3339(ts))

	// 反例守卫：Unix 秒的十进制形态绝不应出现在审计时间字段里
	assert.NotEqual(t, "1789344000", rfc3339(ts))
}

// 导出/导入必须幂等（design-v1 §12.4：export→import(dry_run) diff 为空）。
//
// 回归背景：关键词的写入侧 AutomaticDisableKeywordsFromString 会小写化，而内置
// 默认值是混合大小写。读侧原样输出时，"全新实例导出（混合大小写）→ 导入（小写）
// → 再导出（小写）"会让第一次往返就报 options 变更。故 currentSystemOptions
// 必须输出规范形（trim + 小写）。
func TestSystemOptionsKeywordsAreNormalized(t *testing.T) {
	original := operation_setting.AutomaticDisableKeywords
	t.Cleanup(func() { operation_setting.AutomaticDisableKeywords = original })

	operation_setting.AutomaticDisableKeywords = []string{
		"  Your Credit Balance Is Too Low  ",
		"PERMISSION DENIED",
		"",
	}

	options := currentSystemOptions()
	assert.Equal(t, []string{"your credit balance is too low", "permission denied"}, options.AutomaticDisableKeywords,
		"导出的关键词必须是规范形，否则 export→import 不能幂等")

	// 规范形必须与写入侧一致：再次导入同样的规范形不应产生变化。
	digestBefore := model.DigestOf(options)
	operation_setting.AutomaticDisableKeywordsFromString(strings.Join(options.AutomaticDisableKeywords, "\n"))
	assert.Equal(t, digestBefore, model.DigestOf(currentSystemOptions()),
		"写入再读出必须稳定（否则 dry_run 永远报 options 变更）")
}
