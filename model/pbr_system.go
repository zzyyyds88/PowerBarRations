package model

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"

	"pbr/common"

	"gorm.io/gorm"
)

// 迁移期计费锚点（W7 随计费一起物理删除）。
//
// PBR 没有账号体系：访问凭据是 ClientKey，管理凭据是口令派生的管理密钥。
// 但迁移基座的转发管道仍会走一遍"计费记账"（§10.2 只做逻辑停用，不清空
// service/setting 包），而记账需要一个真实存在的用户行作为归属。
// 因此这里保证存在一个**内部系统用户**：不可登录、不出现在任何对外接口，
// 仅作为迁移期记账锚点。W7 物理清除计费后本函数与用户表一并删除。

const pbrSystemUsername = "pbr-system"

var (
	pbrSystemUserMu sync.Mutex
	pbrSystemUserID atomic.Int64
)

// EnsurePBRSystemUser 返回系统用户 id，不存在则创建。
//
// 并发安全：进程内互斥 + 双重检查 + 内存缓存。多请求同时首用时若各自去建，
// 会互相撞唯一索引，失败的那个就拿不到有效用户，下游记账会报 "invalid userId"
// 并给客户端 500（并发压测实测到过）。
func EnsurePBRSystemUser() (int, error) {
	if cached := pbrSystemUserID.Load(); cached > 0 {
		return int(cached), nil
	}
	pbrSystemUserMu.Lock()
	defer pbrSystemUserMu.Unlock()
	if cached := pbrSystemUserID.Load(); cached > 0 {
		return int(cached), nil
	}

	var user User
	err := DB.Where("role = ?", common.RoleRootUser).First(&user).Error
	if err == nil {
		pbrSystemUserID.Store(int64(user.Id))
		return user.Id, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	// 口令存的是一个不可逆的 HMAC 值（不是任何口令的 bcrypt 哈希），因此无法登录。
	password, err := common.GenerateKey()
	if err != nil {
		return 0, err
	}
	affCode, err := common.GenerateRandomCharsKey(8)
	if err != nil {
		return 0, err
	}
	accessToken, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return 0, err
	}
	user = User{
		Username:    pbrSystemUsername,
		Password:    common.GenerateHMAC(password),
		Role:        common.RoleRootUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       math.MaxInt32,
		AffCode:     affCode,
		AccessToken: &accessToken,
	}
	if err := DB.Create(&user).Error; err != nil {
		// 可能是并发下已被创建：回读一次再决定成败
		var existing User
		if readErr := DB.Where("role = ?", common.RoleRootUser).First(&existing).Error; readErr == nil {
			pbrSystemUserID.Store(int64(existing.Id))
			return existing.Id, nil
		}
		return 0, err
	}
	pbrSystemUserID.Store(int64(user.Id))
	return user.Id, nil
}
