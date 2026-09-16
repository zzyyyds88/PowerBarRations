package model

// Webhook 投递日志（design-v1 §16.10，api-spec §5.8）。
//
// 每次投递（含重试的最终结果与死信）写一行，供 GET /api/webhooks/deliveries
// 排障。随明细日志一起由 POST /logs/prune 按 PBRLogRetentionDays 清理。

// WebhookDelivery 单次投递的最终结果。
type WebhookDelivery struct {
	Id int `json:"id" gorm:"primaryKey"`
	// Ts 投递结论时刻（Unix 秒）；游标分页用自增 id，与 /logs 同一模式。
	Ts int64 `json:"ts" gorm:"bigint;index:idx_webhook_delivery_ts"`

	Target    string `json:"target" gorm:"type:varchar(191);index"`
	EventType string `json:"event_type" gorm:"type:varchar(32)"`
	Lane      string `json:"lane" gorm:"type:varchar(191)"`
	Member    string `json:"member" gorm:"type:varchar(191)"`
	// Status success | failed（failed 即重试耗尽的死信）。
	Status string `json:"status" gorm:"type:varchar(16);index"`
	// HTTPStatus 上游响应码；超时/连接失败等无响应时为 0。
	HTTPStatus int    `json:"http_status"`
	Error      string `json:"error" gorm:"type:text"`
	// Attempt 得出结论的尝试序号（1 = 首次即成/即败，最大 4 = 首次 + 3 次重试）。
	Attempt int `json:"attempt"`
}

// TableName 固定表名（design-v1 §16.10：webhook_deliveries）。
func (WebhookDelivery) TableName() string { return "webhook_deliveries" }

// InsertWebhookDelivery 落一条投递记录；DB 不可用时静默返回（投递日志是
// 排障辅助，不能反过来影响请求路径）。
func InsertWebhookDelivery(entry *WebhookDelivery) error {
	if DB == nil {
		return nil
	}
	return DB.Create(entry).Error
}

// ListWebhookDeliveries 按 id 倒序（即时间倒序）取 limit+1 条用于游标分页。
func ListWebhookDeliveries(limit int, beforeId int) ([]WebhookDelivery, error) {
	query := DB.Model(&WebhookDelivery{}).Order("id desc").Limit(limit + 1)
	if beforeId > 0 {
		query = query.Where("id < ?", beforeId)
	}
	var entries []WebhookDelivery
	if err := query.Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// CountWebhookDeliveriesBefore 统计将被清理的条数（dry_run 用）。
func CountWebhookDeliveriesBefore(before int64) (int64, error) {
	var count int64
	if err := DB.Model(&WebhookDelivery{}).Where("ts < ?", before).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// PruneWebhookDeliveriesBefore 删除 before 之前的投递记录，返回删除条数。
func PruneWebhookDeliveriesBefore(before int64) (int64, error) {
	result := DB.Where("ts < ?", before).Delete(&WebhookDelivery{})
	return result.RowsAffected, result.Error
}
