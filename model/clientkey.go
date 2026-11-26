package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// ClientKey 客户端密钥（docs/token-spec-v1.md §3）。
//
// 设计取舍：**默认放行全部车道，只能显式拒绝**。这直接消灭了现网两代网关的踩坑源
// ——"忘了加白名单 → 故障那一刻 400"。分账由 KeyID 身份承载，不靠白名单。
//
// 明文按 token-spec §3.3 入库存储，KeyHash 仍作为鉴权索引。

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
	KeyPlain       string `json:"-" gorm:"column:key_plain;type:varchar(128)"`
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
	var key ClientKey
	err := DB.Select("id").Where("name = ?", name).First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := DB.Where("name = ?", name).Delete(&ClientKey{}).Error; err != nil {
		return err
	}
	DropClientKeyTouchState(key.Id)
	return nil
}

// UpsertClientKey 全量写：KeyHash 非空表示调用方提供了新密钥，同时更新明文。
func UpsertClientKey(key *ClientKey) error {
	return UpsertClientKeyTx(DB, key)
}

// UpsertClientKeyTx 在调用方事务内写密钥；tx 为 nil 时回落到全局 DB。
func UpsertClientKeyTx(tx *gorm.DB, key *ClientKey) error {
	if tx == nil {
		tx = DB
	}
	if strings.TrimSpace(key.Name) == "" {
		return errors.New("client key name is required")
	}
	now := common.GetTimestamp()
	{
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
		// 轮换/首次设置明文时才改哈希、前缀与明文，普通更新不动它们。
		if key.KeyHash != "" {
			updates["key_hash"] = key.KeyHash
			updates["key_prefix"] = key.KeyPrefix
			updates["key_plain"] = key.KeyPlain
		} else {
			key.KeyHash = existing.KeyHash
			key.KeyPrefix = existing.KeyPrefix
			key.KeyPlain = existing.KeyPlain
		}
		return tx.Model(&ClientKey{}).Where("id = ?", key.Id).Updates(updates).Error
	}
}

// TouchClientKey 记录最近使用时间（只读统计，不反哺准入）。
//
// 请求路径只做有界 map 合并与非阻塞通知；唯一后台 worker 按固定间隔批量落库，
// 避免高并发下“每请求一次 UPDATE”的写放大。map 同时受条目数与 TTL 约束。
const (
	clientKeyTouchInterval  = 30 * time.Second
	clientKeyTouchStateTTL  = 10 * time.Minute
	clientKeyTouchMaxKeys   = 4096
	clientKeyTouchQueueSize = 1
)

var (
	clientKeyTouchMu         sync.Mutex
	clientKeyLastTouch       = map[int]int64{}
	clientKeyTouchPending    = map[int]struct{}{}
	clientKeyTouchSignal     = make(chan struct{}, clientKeyTouchQueueSize)
	clientKeyTouchWorkerOnce sync.Once
	clientKeyTouchCloseOnce  sync.Once
	clientKeyTouchStop       = make(chan struct{})
	clientKeyTouchDone       = make(chan struct{})
	clientKeyTouchClosed     atomic.Bool
	clientKeyTouchLifecycle  sync.RWMutex
)

func TouchClientKey(id int) {
	if id <= 0 {
		return
	}
	clientKeyTouchLifecycle.RLock()
	defer clientKeyTouchLifecycle.RUnlock()
	if clientKeyTouchClosed.Load() {
		return
	}
	now := common.GetTimestamp()
	clientKeyTouchMu.Lock()
	pruneClientKeyTouchStateLocked(now)
	if last, ok := clientKeyLastTouch[id]; ok && now-last < int64(clientKeyTouchInterval.Seconds()) {
		clientKeyTouchMu.Unlock()
		return
	}
	if _, exists := clientKeyLastTouch[id]; !exists && len(clientKeyLastTouch) >= clientKeyTouchMaxKeys {
		// last_used_at 是纯展示字段；容量满时丢弃新 key 的 touch，绝不能把 map
		// 变成无界增长。
		clientKeyTouchMu.Unlock()
		return
	}
	clientKeyLastTouch[id] = now
	clientKeyTouchPending[id] = struct{}{}
	clientKeyTouchMu.Unlock()

	clientKeyTouchWorkerOnce.Do(func() {
		go clientKeyTouchWorker()
	})
	select {
	case clientKeyTouchSignal <- struct{}{}:
	default:
	}
}

func clientKeyTouchWorker() {
	defer close(clientKeyTouchDone)
	select {
	case <-clientKeyTouchSignal:
	case <-clientKeyTouchStop:
		return
	}
	ticker := time.NewTicker(clientKeyTouchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-clientKeyTouchSignal:
			// 请求已合并进 pending map；固定 ticker 负责写节奏。
		case <-ticker.C:
			flushClientKeyTouches(common.GetTimestamp())
		case <-clientKeyTouchStop:
			flushClientKeyTouches(common.GetTimestamp())
			return
		}
	}
}

// CloseClientKeyTouchWorker 停止 touch worker 并最终 flush。进程优雅停机时调用。
func CloseClientKeyTouchWorker() {
	clientKeyTouchCloseOnce.Do(func() {
		clientKeyTouchLifecycle.Lock()
		clientKeyTouchClosed.Store(true)
		clientKeyTouchWorkerOnce.Do(func() {
			go clientKeyTouchWorker()
		})
		close(clientKeyTouchStop)
		clientKeyTouchLifecycle.Unlock()
		<-clientKeyTouchDone
	})
}

func flushClientKeyTouches(now int64) {
	clientKeyTouchMu.Lock()
	if len(clientKeyTouchPending) == 0 {
		clientKeyTouchMu.Unlock()
		return
	}
	ids := make([]int, 0, len(clientKeyTouchPending))
	for id := range clientKeyTouchPending {
		ids = append(ids, id)
	}
	clear(clientKeyTouchPending)
	clientKeyTouchMu.Unlock()

	if err := DB.Model(&ClientKey{}).Where("id IN ?", ids).Update("last_used_at", now).Error; err != nil {
		common.SysError("pbr: touch client key failed: " + err.Error())
		clientKeyTouchMu.Lock()
		for _, id := range ids {
			if _, exists := clientKeyLastTouch[id]; !exists {
				continue
			}
			if len(clientKeyTouchPending) >= clientKeyTouchMaxKeys {
				break
			}
			clientKeyTouchPending[id] = struct{}{}
		}
		clientKeyTouchMu.Unlock()
	}
}

// DropClientKeyTouchState removes all pending/throttling state for a key. It is
// called on deletion so the bounded maps do not retain dead identities.
func DropClientKeyTouchState(id int) {
	if id <= 0 {
		return
	}
	clientKeyTouchMu.Lock()
	delete(clientKeyLastTouch, id)
	delete(clientKeyTouchPending, id)
	clientKeyTouchMu.Unlock()
}

func pruneClientKeyTouchStateLocked(now int64) {
	ttlSeconds := int64(clientKeyTouchStateTTL.Seconds())
	for id, last := range clientKeyLastTouch {
		if now-last >= ttlSeconds {
			delete(clientKeyLastTouch, id)
			delete(clientKeyTouchPending, id)
		}
	}
}

// ParseLanePolicy 解析车道权限 JSON；空/非法时回落到"允许全部"。
//
// 这是**读取**口径：历史数据里可能有脏值，读取必须宽容（否则存量密钥直接不可用）。
// **写入**校验请用 ParseLanePolicyStrict——非法 mode 静默变"允许全部"等于静默放宽权限。
func ParseLanePolicy(raw string) LanePolicy {
	policy, err := ParseLanePolicyStrict(raw)
	if err != nil {
		return LanePolicy{Mode: LanePolicyModeAll}
	}
	return policy
}

// ParseLanePolicyStrict 严格解析：mode 必须是 "" / all / allow，否则返回错误。
// 空字符串视为未配置（等价 all）；allow 模式下 allow_lanes 允许为空（= 什么都不允许）。
func ParseLanePolicyStrict(raw string) (LanePolicy, error) {
	policy := LanePolicy{Mode: LanePolicyModeAll}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return policy, nil
	}
	if err := common.UnmarshalJsonStr(raw, &policy); err != nil {
		return LanePolicy{Mode: LanePolicyModeAll}, fmt.Errorf("invalid lane_policy json: %w", err)
	}
	switch strings.TrimSpace(policy.Mode) {
	case "", LanePolicyModeAll:
		policy.Mode = LanePolicyModeAll
	case LanePolicyModeAllow:
		policy.Mode = LanePolicyModeAllow
	default:
		return LanePolicy{Mode: LanePolicyModeAll}, fmt.Errorf("lane_policy.mode must be all or allow, got %q", policy.Mode)
	}
	return policy, nil
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
