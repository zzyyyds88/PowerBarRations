package model

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 异步对象（Files / Batch）的归属索引（ADR 0010、design-v1 §4.2、routing-spec §4.5）。
//
// 异步对象的请求体没有 model，无法按"路由键 = 模型名"选路；创建时按车道选出一个
// 成员后，对象就固定在该渠道上。本表只存**元数据**，不存文件内容：
//
//	(lane, kind, upstream_id) → channel_id
//
// 跨车道不可见：查询键里带 lane，换个车道寻址就是 not found。上游对象过期后
// 元数据再保留 AsyncObjectRetention（30 天）供审计，由系统任务调用
// CleanupExpiredAsyncObjects 清理，不引入 cron。

const (
	PBRAsyncObjectKindFile  = "file"
	PBRAsyncObjectKindBatch = "batch"

	// PBRAsyncObjectRetention 上游对象过期后元数据的保留时长（ADR 0010 第 6 条）。
	PBRAsyncObjectRetention = 30 * 24 * time.Hour
)

// PBRAsyncObject 一条异步对象归属记录。
type PBRAsyncObject struct {
	Id int64 `json:"id" gorm:"primaryKey"`
	// Kind 只有 file / batch 两种；与 lane、upstream_id 共同构成唯一身份。
	Kind string `json:"kind" gorm:"type:varchar(16);not null;uniqueIndex:idx_pbr_async_object_identity,priority:1"`
	Lane string `json:"lane" gorm:"type:varchar(191);not null;uniqueIndex:idx_pbr_async_object_identity,priority:2"`
	// UpstreamId 上游返回的对象 ID（file-... / batch_...）。网关不重写它，
	// 下游拿到的仍是上游 ID。
	UpstreamId string `json:"upstream_id" gorm:"column:upstream_id;type:varchar(191);not null;uniqueIndex:idx_pbr_async_object_identity,priority:3"`
	ChannelId  int    `json:"channel_id" gorm:"not null;index"`
	// Status 上游状态原样保存（uploaded / processed / in_progress / completed /
	// cancelling / cancelled / expired / deleted ...），不在这里重新解释语义。
	Status string `json:"status" gorm:"type:varchar(32);index"`
	// Size 文件字节数；batch 不适用时为 0。
	Size int64 `json:"size"`
	// CreatedAt 上游对象创建时间（Unix 秒）；上游未返回时取本行写入时间。
	CreatedAt int64 `json:"created_at" gorm:"bigint;index"`
	// ExpiresAt 上游对象过期时间（Unix 秒）；0 表示上游未声明过期。
	ExpiresAt int64 `json:"expires_at" gorm:"bigint;index"`
}

// TableName 显式定名，避免依赖 GORM 的复数化/缩写策略。
func (PBRAsyncObject) TableName() string { return "pbr_async_objects" }

// AutoMigrateAsyncObjects 建表/补列。由 model.InitDB 在启动时调用。
func AutoMigrateAsyncObjects(db *gorm.DB) error {
	if db == nil {
		return errors.New("async objects: nil database")
	}
	return db.AutoMigrate(&PBRAsyncObject{})
}

func validateAsyncObjectIdentity(lane, kind, upstreamID string) error {
	if strings.TrimSpace(lane) == "" {
		return errors.New("async objects: lane is required")
	}
	switch kind {
	case PBRAsyncObjectKindFile, PBRAsyncObjectKindBatch:
	default:
		return errors.New("async objects: kind must be file or batch")
	}
	if strings.TrimSpace(upstreamID) == "" {
		return errors.New("async objects: upstream_id is required")
	}
	return nil
}

// CreateAsyncObject 写入归属行。同一 (lane, kind, upstream_id) 重复写入时更新
// 渠道与状态而不是报错：创建请求重发、或上游对同一次创建返回同一 ID 时，
// 幂等更新比留下一条不可重试的唯一约束错误更符合"创建即固定"的语义。
func CreateAsyncObject(obj *PBRAsyncObject) error {
	if obj == nil {
		return errors.New("async objects: nil object")
	}
	if err := validateAsyncObjectIdentity(obj.Lane, obj.Kind, obj.UpstreamId); err != nil {
		return err
	}
	if obj.CreatedAt == 0 {
		obj.CreatedAt = time.Now().Unix()
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "kind"},
			{Name: "lane"},
			{Name: "upstream_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"channel_id", "status", "size", "expires_at"}),
	}).Create(obj).Error
}

// GetAsyncObject 按 (lane, kind, upstream_id) 查询归属行。
// 不存在时返回 gorm.ErrRecordNotFound（调用方转 404 async_object_not_found）。
func GetAsyncObject(lane, kind, upstreamID string) (*PBRAsyncObject, error) {
	if err := validateAsyncObjectIdentity(lane, kind, upstreamID); err != nil {
		return nil, err
	}
	var obj PBRAsyncObject
	if err := DB.Where("lane = ? AND kind = ? AND upstream_id = ?", lane, kind, upstreamID).
		First(&obj).Error; err != nil {
		return nil, err
	}
	return &obj, nil
}

// PBRAsyncObjectUpdate 局部更新；nil 指针表示"该字段本次不可用，保持原值"。
// 用指针而不是零值判断，避免把合法的 size=0 / expires_at=0 误当成"不更新"。
type PBRAsyncObjectUpdate struct {
	Status    string
	Size      *int64
	ExpiresAt *int64
}

// UpdateAsyncObjectMeta 更新一条归属行的状态/大小/过期时间。
// 行不存在时返回 gorm.ErrRecordNotFound（调用方决定是否忽略竞态）。
func UpdateAsyncObjectMeta(lane, kind, upstreamID string, update PBRAsyncObjectUpdate) error {
	if err := validateAsyncObjectIdentity(lane, kind, upstreamID); err != nil {
		return err
	}
	updates := make(map[string]any, 3)
	if status := strings.TrimSpace(update.Status); status != "" {
		updates["status"] = truncateAsyncField(status, 32)
	}
	if update.Size != nil {
		updates["size"] = *update.Size
	}
	if update.ExpiresAt != nil {
		updates["expires_at"] = *update.ExpiresAt
	}
	if len(updates) == 0 {
		return nil
	}
	res := DB.Model(&PBRAsyncObject{}).
		Where("lane = ? AND kind = ? AND upstream_id = ?", lane, kind, upstreamID).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetAsyncObjectOwners 返回给定 kind 下每个 upstream_id 在 pbr_async_objects
// 中出现过的车道集合。列表按车道过滤依赖它：没有记录的对象无法证明属于任何
// 车道，调用方必须按"不在集合中"剔除（宁可不列，也不跨车道泄漏）。
func GetAsyncObjectOwners(kind string, upstreamIDs []string) (map[string][]string, error) {
	switch kind {
	case PBRAsyncObjectKindFile, PBRAsyncObjectKindBatch:
	default:
		return nil, errors.New("async objects: kind must be file or batch")
	}
	owners := make(map[string][]string)
	ids := make([]string, 0, len(upstreamIDs))
	seen := make(map[string]bool, len(upstreamIDs))
	for _, id := range upstreamIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return owners, nil
	}
	// SQLite 的绑定参数有上限；按 500 一批查询，列表页通常远小于这个值。
	const chunkSize = 500
	for start := 0; start < len(ids); start += chunkSize {
		end := start + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		var rows []PBRAsyncObject
		if err := DB.Select("upstream_id", "lane").
			Where("kind = ? AND upstream_id IN ?", kind, ids[start:end]).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			owners[row.UpstreamId] = append(owners[row.UpstreamId], row.Lane)
		}
	}
	return owners, nil
}

// DeleteAsyncObject 删除一条归属行（供显式清理/测试使用；过期清理走
// CleanupExpiredAsyncObjects）。
func DeleteAsyncObject(lane, kind, upstreamID string) error {
	if err := validateAsyncObjectIdentity(lane, kind, upstreamID); err != nil {
		return err
	}
	return DB.Where("lane = ? AND kind = ? AND upstream_id = ?", lane, kind, upstreamID).
		Delete(&PBRAsyncObject{}).Error
}

// CleanupExpiredAsyncObjects 删除"上游过期已超过保留期"的元数据行，返回删除行数。
// 由既有系统任务调用；本函数不建 cron、不自行调度。
//
// 只删除 expires_at > 0 的行：上游没有声明过期时间的对象（expires_at=0）没有
// 可靠的生命周期依据，保留比误删安全。
func CleanupExpiredAsyncObjects(now time.Time) (int64, error) {
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-PBRAsyncObjectRetention).Unix()
	res := DB.Where("expires_at > 0 AND expires_at <= ?", cutoff).Delete(&PBRAsyncObject{})
	return res.RowsAffected, res.Error
}

func truncateAsyncField(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
