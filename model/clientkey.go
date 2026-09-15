package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"pbr/common"

	"gorm.io/gorm"
)

// ClientKey 客户端密钥（docs/token-spec-v1.md §3）。
//
// 设计取舍：**默认放行全部车道，只能显式拒绝**。这直接消灭了现网两代网关的踩坑源
// ——"忘了加白名单 → 故障那一刻 400"。分账由 KeyID 身份承载，不靠白名单。
//
// 明文只在创建/轮换响应里出现一次；库里只存 sha256。

const (
	LanePolicyModeAll   = "all"
	LanePolicyModeAllow = "allow"

	// ClientKeyPrefix 新密钥格式 pbr-<32 位 base62>。
	clientKeyPrefix = "pbr-"
	// ClientKeyPrefixLength 展示用前缀长度（含 pbr-）。
	ClientKeyPrefixLength = 12
)

type ClientKey struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	Name           string `json:"name" gorm:"unique;not null;index"`
	KeyHash        string `json:"key_hash" gorm:"type:varchar(64);index"`
	KeyPrefix      string `json:"key_prefix" gorm:"type:varchar(16)"`
	Enabled        bool   `json:"enabled"`
	LanePolicy     string `json:"lane_policy" gorm:"type:text"`
	IPAllowlist    string `json:"ip_allowlist" gorm:"type:text"`
	RateLimitRPM   int    `json:"rate_limit_rpm"`
	MaxConcurrency int    `json:"max_concurrency"`
	ExpiresAt      *int64 `json:"expires_at" gorm:"bigint"`
	Notes          string `json:"notes" gorm:"type:varchar(255)"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint"`
	LastUsedAt     int64  `json:"last_used_at" gorm:"bigint"`
}

// LanePolicy 车道权限（token-spec §3.2）：
//
//	候选 = (Mode=="all") ? 全部车道 : AllowLanes
//	生效 = 候选 - DenyLanes
type LanePolicy struct {
	Mode       string   `json:"mode"`
	AllowLanes []string `json:"allow_lanes"`
	DenyLanes  []string `json:"deny_lanes"`
}

// GenerateClientKey 生成新密钥明文（熵约 190 bit）与展示前缀。
func GenerateClientKey() (plain string, prefix string, err error) {
	random, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", "", err
	}
	plain = clientKeyPrefix + random
	return plain, plain[:ClientKeyPrefixLength], nil
}

// HashClientKey 明文 → 入库哈希。
func HashClientKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// PrefixOfClientKey 明文前缀（导入存量密钥时按原值截取，不做格式规整）。
func PrefixOfClientKey(plain string) string {
	if len(plain) <= ClientKeyPrefixLength {
		return plain
	}
	return plain[:ClientKeyPrefixLength]
}

func GetClientKeyByName(name string) (*ClientKey, error) {
	var key ClientKey
	if err := DB.Where("name = ?", name).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

// GetClientKeyByPlain 按明文哈希查表（O(1)，token-spec §3.4 第 2 步）。
func GetClientKeyByPlain(plain string) (*ClientKey, error) {
	if strings.TrimSpace(plain) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var key ClientKey
	if err := DB.Where("key_hash = ?", HashClientKey(plain)).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

func ListClientKeys() ([]ClientKey, error) {
	var keys []ClientKey
	if err := DB.Order("name asc").Find(&keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

func DeleteClientKeyByName(name string) error {
	return DB.Where("name = ?", name).Delete(&ClientKey{}).Error
}

// UpsertClientKey 全量写：明文只写不读，KeyHash 由调用方在提供新明文时设置。
func UpsertClientKey(key *ClientKey) error {
	if strings.TrimSpace(key.Name) == "" {
		return errors.New("client key name is required")
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing ClientKey
		err := tx.Where("name = ?", key.Name).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			key.Id = 0
			key.CreatedAt = now
			key.UpdatedAt = now
			return tx.Create(key).Error
		}
		if err != nil {
			return err
		}
		key.Id = existing.Id
		key.CreatedAt = existing.CreatedAt
		key.UpdatedAt = now
		updates := map[string]any{
			"enabled":         key.Enabled,
			"lane_policy":     key.LanePolicy,
			"ip_allowlist":    key.IPAllowlist,
			"rate_limit_rpm":  key.RateLimitRPM,
			"max_concurrency": key.MaxConcurrency,
			"expires_at":      key.ExpiresAt,
			"notes":           key.Notes,
			"updated_at":      now,
		}
		// 轮换/首次设置明文时才改哈希与前缀，普通更新不动它们。
		if key.KeyHash != "" {
			updates["key_hash"] = key.KeyHash
			updates["key_prefix"] = key.KeyPrefix
		} else {
			key.KeyHash = existing.KeyHash
			key.KeyPrefix = existing.KeyPrefix
		}
		return tx.Model(&ClientKey{}).Where("id = ?", key.Id).Updates(updates).Error
	})
}

// TouchClientKey 记录最近使用时间（只读统计，不反哺准入）。
//
// 节流：这是纯展示字段，不值得每请求写一次库——在高并发下"每请求一次 UPDATE"
// 会成为写放大主力（实测把 SQLite 连接池与 OS 线程顶到上限）。同一把密钥
// 在 clientKeyTouchInterval 内只写一次。
const clientKeyTouchInterval = 30 * time.Second

var (
	clientKeyTouchMu   sync.Mutex
	clientKeyLastTouch = map[int]int64{}
)

func TouchClientKey(id int) {
	if id <= 0 {
		return
	}
	now := common.GetTimestamp()
	clientKeyTouchMu.Lock()
	if last, ok := clientKeyLastTouch[id]; ok && now-last < int64(clientKeyTouchInterval.Seconds()) {
		clientKeyTouchMu.Unlock()
		return
	}
	clientKeyLastTouch[id] = now
	clientKeyTouchMu.Unlock()

	if err := DB.Model(&ClientKey{}).Where("id = ?", id).Update("last_used_at", now).Error; err != nil {
		common.SysError("pbr: touch client key failed: " + err.Error())
	}
}

// ParseLanePolicy 解析车道权限 JSON；空/非法时回落到"允许全部"。
func ParseLanePolicy(raw string) LanePolicy {
	policy := LanePolicy{Mode: LanePolicyModeAll}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return policy
	}
	if err := common.UnmarshalJsonStr(raw, &policy); err != nil {
		return LanePolicy{Mode: LanePolicyModeAll}
	}
	if policy.Mode != LanePolicyModeAllow {
		policy.Mode = LanePolicyModeAll
	}
	return policy
}

// AllowsLane 判定该密钥是否可访问某个路由键（token-spec §3.2）。
func (p LanePolicy) AllowsLane(lane string) bool {
	for _, denied := range p.DenyLanes {
		if strings.TrimSpace(denied) == lane {
			return false
		}
	}
	if p.Mode != LanePolicyModeAllow {
		return true
	}
	for _, allowed := range p.AllowLanes {
		if strings.TrimSpace(allowed) == lane {
			return true
		}
	}
	return false
}
