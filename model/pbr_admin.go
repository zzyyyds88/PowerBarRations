package model

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"pbr/common"

	"gorm.io/gorm"
)

// 管理凭据：无账号体系，只有一个登录口令（docs/token-spec-v1.md §2）。
//
//	管理密钥 = Base64( SHA256(登录口令) )
//	库里只存 hex( SHA256(管理密钥) )，口令与管理密钥明文都不落库。
//
// 安全取舍（token-spec §2.5）：SHA256 单次、无盐，抗离线爆破弱于 Argon2/bcrypt，
// 这是应用户指定的派生规则；缓解手段是长随机口令 + 数据库文件仅本机可读。

// PBRAdminCredential 单行表，Id 恒为 1。
type PBRAdminCredential struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	AdminKeySha256 string `json:"admin_key_sha256" gorm:"column:admin_key_sha256;type:varchar(64)"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint"`
}

const pbrAdminCredentialRowId = 1

// DeriveAdminKey 由登录口令派生管理密钥（标准 Base64，32 字节摘要）。
func DeriveAdminKey(password string) string {
	sum := sha256.Sum256([]byte(password))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// HashAdminKey 计算入库用的管理密钥摘要。
func HashAdminKey(adminKey string) string {
	return hex.EncodeToString(common.Sha256Raw([]byte(adminKey)))
}

// IsPBRInitialized 是否已设置登录口令。
func IsPBRInitialized() bool {
	cred, err := GetPBRAdminCredential()
	return err == nil && cred != nil && cred.AdminKeySha256 != ""
}

// GetPBRAdminCredential 读取管理凭据；未初始化返回 (nil, nil)。
func GetPBRAdminCredential() (*PBRAdminCredential, error) {
	var cred PBRAdminCredential
	err := DB.Where("id = ?", pbrAdminCredentialRowId).First(&cred).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cred, nil
}

// SetPBRAdminPassword 设置/变更登录口令，返回一次性派生管理密钥。
func SetPBRAdminPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("password is required")
	}
	adminKey := DeriveAdminKey(password)
	if err := StorePBRAdminKey(adminKey); err != nil {
		return "", err
	}
	return adminKey, nil
}

// StorePBRAdminKey 直接写入管理密钥（供 PBR_ADMIN_KEY 显式覆盖场景使用）。
func StorePBRAdminKey(adminKey string) error {
	cred := PBRAdminCredential{
		Id:             pbrAdminCredentialRowId,
		AdminKeySha256: HashAdminKey(adminKey),
		UpdatedAt:      common.GetTimestamp(),
	}
	return DB.Save(&cred).Error
}

// VerifyPBRAdminKey 校验管理密钥。用摘要比较，避免把明文带进比较过程。
func VerifyPBRAdminKey(adminKey string) bool {
	if strings.TrimSpace(adminKey) == "" {
		return false
	}
	cred, err := GetPBRAdminCredential()
	if err != nil || cred == nil || cred.AdminKeySha256 == "" {
		return false
	}
	return cred.AdminKeySha256 == HashAdminKey(adminKey)
}

// ResetPBRAdminCredential 清除库内凭据，使网关回到未初始化状态（CLI `pbr auth reset` 用）。
func ResetPBRAdminCredential() error {
	return DB.Where("id = ?", pbrAdminCredentialRowId).Delete(&PBRAdminCredential{}).Error
}
