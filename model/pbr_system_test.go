/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"gorm.io/gorm"
)

// EnsurePBRSystemUser 是渠道测试与自动巡检在"无多用户体系"下的记账身份来源：
// users 表为空时必须自动建一个 root 系统用户并缓存其 id，否则 relay 管道会报
// "failed to resolve channel test user: record not found"（历史上真机复现过）。

func usePBRSystemTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousType := common.MainDatabaseType()
	cached := pbrSystemUserID.Swap(0)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// 纯 Go sqlite 的 :memory: 库每连接独立，不钉 MaxOpenConns(1) 时建表与查询
	// 可能落在不同连接上，偶发 no such table（webhook 测试实测 flaky）。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	require.NoError(t, db.AutoMigrate(&User{}))
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		pbrSystemUserID.Store(cached)
		DB = previousDB
		common.SetMainDatabaseType(previousType)
	})
	return db
}

func TestEnsurePBRSystemUserCreatesSystemUserWhenUsersEmpty(t *testing.T) {
	db := usePBRSystemTestDB(t)

	userID, err := EnsurePBRSystemUser()
	require.NoError(t, err)
	require.Greater(t, userID, 0, "空 users 表也必须解析出正向记账身份")

	var count int64
	require.NoError(t, db.Model(&User{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	var created User
	require.NoError(t, db.First(&created).Error)
	assert.Equal(t, pbrSystemUsername, created.Username)
	assert.Equal(t, common.RoleRootUser, created.Role)
	assert.Equal(t, "default", created.Group)
}

func TestEnsurePBRSystemUserIsIdempotentAcrossCacheResets(t *testing.T) {
	db := usePBRSystemTestDB(t)

	firstID, err := EnsurePBRSystemUser()
	require.NoError(t, err)
	// 模拟进程内缓存被清（如另一路径冷启动）：应回读到同一用户，而非再建。
	pbrSystemUserID.Store(0)
	secondID, err := EnsurePBRSystemUser()
	require.NoError(t, err)

	assert.Equal(t, firstID, secondID)
	var count int64
	require.NoError(t, db.Model(&User{}).Count(&count).Error)
	assert.Equal(t, int64(1), count, "重复调用不得产生第二个系统用户")
}

func TestEnsurePBRSystemUserReusesExistingRootUser(t *testing.T) {
	db := usePBRSystemTestDB(t)

	existing := User{
		Username: "preexisting-root",
		Password: "x",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&existing).Error)

	userID, err := EnsurePBRSystemUser()
	require.NoError(t, err)
	assert.Equal(t, existing.Id, userID)

	var count int64
	require.NoError(t, db.Model(&User{}).Count(&count).Error)
	assert.Equal(t, int64(1), count, "已存在 root 用户时不得再建系统用户")
}
