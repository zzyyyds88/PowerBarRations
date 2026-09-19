package helper

import (
	"github.com/zzyyyds88/PowerBarRations/setting/ratio_setting"
)

// 计费执行链（价格/倍率折算、预扣费额度计算、阶梯表达式求值）已按
// design-v1 §1.3 物理删除，转发路径不再读取 PriceData。这里仅保留
// "模型是否配置了价格条目"的目录检查——它服务于模型目录（/v1/models），
// 不产生任何额度语义。

// HasModelBillingConfig reports whether the model has a configured price or
// ratio entry in the (display-only) pricing settings.
func HasModelBillingConfig(modelName string) bool {
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	_, ok, _ := ratio_setting.GetModelRatio(modelName)
	return ok
}
