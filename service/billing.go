package service

import (
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
)

// 计费执行链在 W7 被物理删除（design-v1 §10.2.1）：预扣费/结算/退款/钱包/订阅/
// 额度钳制/资金来源/阶梯结算的实现（原 BillingSession、FundingSource、
// TieredSettle 等）全部移除，这里只保留**接口形状与调用点**，使转发管道与 40 家
// 适配器不必改动（§10.2 的"惰性遗留"约定）。
//
// 语义：不预扣、不结算、不退费——`relayInfo.Billing` 恒为 nil，日志里的
// 用量与折算由 PBR 单价表（§16.9#7）在 `model.RecordConsumeLog` 里完成。

// BillingSourceWallet 只作为日志字段（`billing_source`）的遗留取值保留：PBR 无钱包/
// 订阅语义，恒为空。删掉字符串会让保留的日志链路出现空值，故仅保留 wallet 取值；
// subscription 取值已无任何生产者与消费者，随订阅链路一并删除。
const BillingSourceWallet = "wallet"

// PreConsumeBilling 迁移期空实现：不再创建计费会话，也不做任何额度预占。
//
// 保留参数与返回类型是为了不动调用方（controller/relay.go、relay/relay_task.go）。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	return nil
}

// SettleBilling 迁移期空实现：不再结算，只让调用方拿到"成功"。
//
// 注意它**不写基座 logs 表**：PBR 的元数据日志是 pbr_request_logs，由转发收尾统一落库。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	return nil
}
