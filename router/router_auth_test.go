package router

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/require"
)

// createTestClientKey 造一把模型面可用的 PBR 客户端密钥。
//
// W7 起模型面只认客户端密钥：`PBRTokenAuth` 里"未命中就回落基座令牌"的迁移期兜底
// 已按 design-v1 §10.2.1 删除，因此这些路由测试必须用真客户端密钥而不是基座 Token。
func createTestClientKey(t *testing.T, name string, plain string) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.ClientKey{}))
	require.NoError(t, model.DB.Create(&model.ClientKey{
		Name:      name,
		KeyHash:   model.HashClientKey(plain),
		KeyPrefix: model.PrefixOfClientKey(plain),
		Enabled:   true,
	}).Error)
}
