package common

import "testing"

// 自动恢复默认开启（design-v1 §7.5）。
//
// 基座默认 false 会让"自动禁用"变成单向操作：渠道被自动禁用后没有任何
// 路径能改回 enabled，而车道成员链只取 status=enabled 的渠道，成员就此
// 静默消失。本测试锁死默认值，防止回归。
func TestAutomaticEnableChannelEnabledDefaultsOn(t *testing.T) {
	if !AutomaticEnableChannelEnabled {
		t.Fatal("AutomaticEnableChannelEnabled 默认必须为 true（design-v1 §7.5）")
	}
}
