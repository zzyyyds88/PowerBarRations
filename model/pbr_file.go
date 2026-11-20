package model

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

// 网关侧 Anthropic Files 存储的元数据（ADR 0011、design-v1 §4.2）。
//
// 与 pbr_async_objects 完全独立：那张表记录"对象固定在哪个上游渠道"（OpenAI
// Files 语义），这张表记录"字节存在本机、归属哪个客户端密钥"（Anthropic Files
// 语义）。两者不共用表、不共用清理任务、不共用归属判定。
//
// 本表**只存元数据**；字节落在 <SQLITE_PATH 目录>/pbr-files 下（见
// internal/filestore）。这是本项目唯一打破"不落盘"的显式例外，边界见 ADR 0011。

const (
	// PBRFileIDPrefix 网关侧对象 ID 前缀，与上游 OpenAI 对象 ID（file-...）
	// 天然隔离，按前缀即可无歧义分流。
	PBRFileIDPrefix = "pbrfile-"

	// PBRFileIDHexLen 前缀之后随机十六进制串的长度（24 位 = 96 bit）。
	PBRFileIDHexLen = 24

	// PBRFileMaxBytes 单文件上限 64 MiB。比 OpenAI Files 的 512 MiB 小得多：
	// 这些字节要在引用时展开成 base64 内联进 messages 请求体，512 MiB 会膨胀成
	// 约 680 MiB 的单次上行。
	PBRFileMaxBytes int64 = 64 << 20

	// PBRFilesTotalQuotaBytes 全部网关侧文件的总量上限 1 GiB。
	PBRFilesTotalQuotaBytes int64 = 1 << 30

	// PBRFileDefaultTTL 客户端未声明 expires_after 时的缺省存活时间。
	PBRFileDefaultTTL = 24 * time.Hour

	// PBRFileMaxTTL 客户端可声明的最大存活时间（7 天）。
	PBRFileMaxTTL = 7 * 24 * time.Hour
)

// pbrFileIDPattern 严格匹配网关侧对象 ID；路径派生依赖它杜绝目录穿越。
var pbrFileIDPattern = regexp.MustCompile(`^` + PBRFileIDPrefix + `[0-9a-f]{24}$`)

// PBRFile 一个网关侧文件的元数据行。
type PBRFile struct {
	// ObjectId 主键，形如 pbrfile-<24 位小写十六进制>。
	ObjectId string `json:"id" gorm:"column:object_id;type:varchar(40);primaryKey"`
	// TokenId 归属的客户端密钥。读/删/展开都必须匹配，跨密钥一律视为不存在。
	TokenId int `json:"-" gorm:"not null;index"`
	// Filename 客户端上传时提供的文件名（仅用于回显，不参与路径派生）。
	Filename string `json:"filename" gorm:"type:varchar(255)"`
	MimeType string `json:"mime_type" gorm:"type:varchar(128);index"`
	SizeBytes int64 `json:"size_bytes"`
	// Sha256 内容摘要，用于回读校验与排障；不用于去重。
	Sha256 string `json:"sha256" gorm:"type:varchar(64)"`
	// CreatedAt / ExpiresAt 均为 Unix 秒。ExpiresAt=0 表示不过期（当前不会产生）。
	CreatedAt int64 `json:"created_at" gorm:"bigint;index"`
	ExpiresAt int64 `json:"expires_at" gorm:"bigint;index"`
}

// TableName 显式定名，避免依赖 GORM 的复数化策略。
func (PBRFile) TableName() string { return "pbr_files" }

// AutoMigratePBRFiles 建表/补列。由 model.InitDB 在启动时调用。
func AutoMigratePBRFiles(db *gorm.DB) error {
	if db == nil {
		return errors.New("pbr files: nil database")
	}
	return db.AutoMigrate(&PBRFile{})
}

// IsPBRFileID 判断一个对象 ID 是否属于网关侧存储（而不是上游 OpenAI 对象）。
func IsPBRFileID(id string) bool {
	return pbrFileIDPattern.MatchString(strings.TrimSpace(id))
}

// NewPBRFileID 生成网关侧对象 ID。覆盖 existing 中已占用的值（极小概率）。
func NewPBRFileID(existing func(string) bool) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		id := PBRFileIDPrefix + randomHex(PBRFileIDHexLen)
		if existing == nil || !existing(id) {
			return id, nil
		}
	}
	return "", errors.New("pbr files: failed to allocate a unique object id")
}

// CreatePBRFile 写入一条文件元数据行。ObjectId 冲突时返回错误（不覆盖既有对象：
// 覆盖会让已发出的 file_id 静默指向新内容）。
func (f *PBRFile) Create() error {
	if f == nil {
		return errors.New("pbr files: nil file")
	}
	if !IsPBRFileID(f.ObjectId) {
		return fmt.Errorf("pbr files: invalid object id %q", f.ObjectId)
	}
	if f.SizeBytes <= 0 || f.SizeBytes > PBRFileMaxBytes {
		return fmt.Errorf("pbr files: size %d out of range", f.SizeBytes)
	}
	if f.CreatedAt == 0 {
		f.CreatedAt = time.Now().Unix()
	}
	return DB.Create(f).Error
}

// GetPBRFile 按 (object_id, token_id) 查询。跨密钥与不存在都返回
// gorm.ErrRecordNotFound，调用方统一转 404 以免泄露对象存在性。
func GetPBRFile(objectID string, tokenID int) (*PBRFile, error) {
	objectID = strings.TrimSpace(objectID)
	if !IsPBRFileID(objectID) {
		return nil, gorm.ErrRecordNotFound
	}
	var f PBRFile
	if err := DB.Where("object_id = ? AND token_id = ?", objectID, tokenID).First(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// ListPBRFiles 列出某客户端密钥名下未过期的文件，按创建时间倒序。
// 过期但尚未被清理任务删除的行不返回（对外语义是"不存在"）。
func ListPBRFiles(tokenID int, now time.Time) ([]PBRFile, error) {
	if now.IsZero() {
		now = time.Now()
	}
	var files []PBRFile
	err := DB.Where("token_id = ? AND (expires_at = 0 OR expires_at > ?)", tokenID, now.Unix()).
		Order("created_at DESC").
		Find(&files).Error
	return files, err
}

// DeletePBRFile 删除一条元数据行，返回是否命中。
func DeletePBRFile(objectID string, tokenID int) (bool, error) {
	objectID = strings.TrimSpace(objectID)
	if !IsPBRFileID(objectID) {
		return false, nil
	}
	res := DB.Where("object_id = ? AND token_id = ?", objectID, tokenID).Delete(&PBRFile{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// PBRFilesTotalBytes 已占用总字节数（配额判定用）。统计全部密钥——配额是实例级的。
func PBRFilesTotalBytes() (int64, error) {
	var total int64
	err := DB.Model(&PBRFile{}).Select("COALESCE(SUM(size_bytes), 0)").Scan(&total).Error
	return total, err
}

// PBRFileExpiredIDs 返回已过期对象的 ID 列表（供清理任务删字节）。
// 只返回 ID，不连带查其他列：清理路径只关心"删哪个"。
func PBRFileExpiredIDs(now time.Time) ([]string, error) {
	if now.IsZero() {
		now = time.Now()
	}
	var ids []string
	err := DB.Model(&PBRFile{}).
		Where("expires_at > 0 AND expires_at <= ?", now.Unix()).
		Pluck("object_id", &ids).Error
	return ids, err
}

// DeleteExpiredPBRFiles 删除已过期的元数据行，返回删除行数。
// 字节由调用方在拿到 PBRFileExpiredIDs 后自行删除——先删字节再删行会让
// "字节删了但行还在"的中间态对外暴露为可读但内容缺失，反过来更安全。
func DeleteExpiredPBRFiles(now time.Time, objectIDs []string) (int64, error) {
	if len(objectIDs) == 0 {
		return 0, nil
	}
	cutoff := now.Unix()
	var deleted int64
	const chunkSize = 500
	for start := 0; start < len(objectIDs); start += chunkSize {
		end := start + chunkSize
		if end > len(objectIDs) {
			end = len(objectIDs)
		}
		res := DB.Where("object_id IN ? AND expires_at > 0 AND expires_at <= ?", objectIDs[start:end], cutoff).
			Delete(&PBRFile{})
		if res.Error != nil {
			return deleted, res.Error
		}
		deleted += res.RowsAffected
	}
	return deleted, nil
}

// randomHex 生成 n 位小写十六进制随机串。
func randomHex(n int) string {
	const alphabet = "0123456789abcdef"
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// 随机源不可用时退回时间派生值：ID 只需唯一，不需密码学强度。
		seed := time.Now().UnixNano()
		for i := range buf {
			buf[i] = byte(seed >> (uint(i%8) * 8))
		}
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf)
}
