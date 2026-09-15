package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"pbr/common"
)

// PBRAuditLog 管理面变更审计（api-spec §2.6）：**只记元数据**，
// 不记密钥明文、不记请求正文。
type PBRAuditLog struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	Ts           int64  `json:"ts" gorm:"bigint;index"`
	Actor        string `json:"actor" gorm:"type:varchar(64)"`
	Action       string `json:"action" gorm:"type:varchar(32)"`
	Resource     string `json:"resource" gorm:"type:varchar(32)"`
	Name         string `json:"name" gorm:"type:varchar(128);index"`
	BeforeDigest string `json:"before_digest" gorm:"type:varchar(64)"`
	AfterDigest  string `json:"after_digest" gorm:"type:varchar(64)"`
	DryRun       bool   `json:"dry_run"`
}

// DigestOf 计算对象摘要（用于审计的 before/after 与导入 diff 比对，不含明文）。
//
// 先 JSON 往返一次归一化：结构体按字段序、map 按键序输出，两侧必须先落到
// 同一个规范形态，否则"内容相同但类型不同"会被判成变更。
func DigestOf(payload any) string {
	if payload == nil {
		return ""
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	var generic any
	if err := json.Unmarshal(encoded, &generic); err == nil {
		if normalized, err := json.Marshal(generic); err == nil {
			encoded = normalized
		}
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// WritePBRAudit 落一条审计记录。
func WritePBRAudit(actor, action, resource, name, beforeDigest, afterDigest string, dryRun bool) error {
	entry := PBRAuditLog{
		Ts:           common.GetTimestamp(),
		Actor:        actor,
		Action:       action,
		Resource:     resource,
		Name:         name,
		BeforeDigest: beforeDigest,
		AfterDigest:  afterDigest,
		DryRun:       dryRun,
	}
	return DB.Create(&entry).Error
}

// ListPBRAudits 按时间倒序分页。
func ListPBRAudits(limit int, beforeId int) ([]PBRAuditLog, error) {
	query := DB.Model(&PBRAuditLog{}).Order("id desc").Limit(limit + 1)
	if beforeId > 0 {
		query = query.Where("id < ?", beforeId)
	}
	var entries []PBRAuditLog
	if err := query.Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}
