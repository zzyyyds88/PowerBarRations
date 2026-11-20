package model

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPBRFileTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	original := DB
	DB = db
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { DB = original })
	require.NoError(t, AutoMigratePBRFiles(db))
}

func newTestFile(t *testing.T, tokenID int, size int64, expiresAt int64) *PBRFile {
	t.Helper()
	id, err := NewPBRFileID(nil)
	require.NoError(t, err)
	return &PBRFile{
		ObjectId:  id,
		TokenId:   tokenID,
		Filename:  "f.bin",
		MimeType:  "text/plain",
		SizeBytes: size,
		Sha256:    "deadbeef",
		CreatedAt: time.Now().Unix(),
		ExpiresAt: expiresAt,
	}
}

// 对象 ID 格式：前缀 + 24 位小写十六进制；非法形状一律不认。
func TestPBRFileIDFormat(t *testing.T) {
	id, err := NewPBRFileID(nil)
	require.NoError(t, err)
	assert.True(t, IsPBRFileID(id))
	assert.Len(t, id, len(PBRFileIDPrefix)+PBRFileIDHexLen)

	for _, bad := range []string{
		"",
		"pbrfile-",
		"pbrfile-abc",
		"pbrfile-0123456789abcdef0123456",  // 23 位
		"pbrfile-0123456789abcdef012345678", // 25 位
		"pbrfile-0123456789ABCDEF01234567",  // 大写
		"PBRFILE-0123456789abcdef01234567",
		"file-0123456789abcdef01234567",
	} {
		assert.False(t, IsPBRFileID(bad), "应当拒绝：%q", bad)
	}
}

// NewPBRFileID 必须避开已占用的 ID。
func TestNewPBRFileIDAvoidsCollision(t *testing.T) {
	taken := "pbrfile-0123456789abcdef01234567"
	attempts := 0
	id, err := NewPBRFileID(func(candidate string) bool {
		attempts++
		return candidate == taken
	})
	require.NoError(t, err)
	assert.NotEqual(t, taken, id)
	assert.GreaterOrEqual(t, attempts, 1)
}

// 归属判定：跨密钥与不存在同形（都返回 ErrRecordNotFound）。
func TestGetPBRFileScopesByToken(t *testing.T) {
	setupPBRFileTestDB(t)
	file := newTestFile(t, 7, 10, 0)
	require.NoError(t, file.Create())

	got, err := GetPBRFile(file.ObjectId, 7)
	require.NoError(t, err)
	assert.Equal(t, file.ObjectId, got.ObjectId)

	_, err = GetPBRFile(file.ObjectId, 8)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	_, err = GetPBRFile("pbrfile-0123456789abcdef01234567", 7)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// 非法 ID 也走同一条 not-found 路径，不做额外区分。
	_, err = GetPBRFile("../../etc/passwd", 7)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

// 尺寸校验：0 与超过上限都拒绝（元数据不得描述不可能的文件）。
func TestPBRFileCreateValidatesSize(t *testing.T) {
	setupPBRFileTestDB(t)

	zero := newTestFile(t, 7, 0, 0)
	assert.Error(t, zero.Create())

	tooBig := newTestFile(t, 7, PBRFileMaxBytes+1, 0)
	assert.Error(t, tooBig.Create())

	atLimit := newTestFile(t, 7, PBRFileMaxBytes, 0)
	assert.NoError(t, atLimit.Create())
}

// 重复对象 ID 不得覆盖既有行：覆盖会让已发出的 file_id 静默指向新内容。
func TestPBRFileCreateRejectsDuplicateID(t *testing.T) {
	setupPBRFileTestDB(t)
	first := newTestFile(t, 7, 10, 0)
	require.NoError(t, first.Create())

	second := &PBRFile{
		ObjectId:  first.ObjectId,
		TokenId:   8,
		MimeType:  "image/png",
		SizeBytes: 99,
		CreatedAt: time.Now().Unix(),
	}
	assert.Error(t, second.Create())

	stored, err := GetPBRFile(first.ObjectId, 7)
	require.NoError(t, err)
	assert.EqualValues(t, 10, stored.SizeBytes, "既有行不得被覆盖")
}

// 列表只返回本密钥名下未过期的对象。
func TestListPBRFilesFiltersExpiredAndOtherKeys(t *testing.T) {
	setupPBRFileTestDB(t)
	now := time.Now()

	fresh := newTestFile(t, 7, 10, now.Add(time.Hour).Unix())
	require.NoError(t, fresh.Create())
	expired := newTestFile(t, 7, 10, now.Add(-time.Hour).Unix())
	require.NoError(t, expired.Create())
	other := newTestFile(t, 8, 10, now.Add(time.Hour).Unix())
	require.NoError(t, other.Create())

	files, err := ListPBRFiles(7, now)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, fresh.ObjectId, files[0].ObjectId)
}

// 删除按 (object_id, token_id) 命中；跨密钥不算删除成功。
func TestDeletePBRFileScopesByToken(t *testing.T) {
	setupPBRFileTestDB(t)
	file := newTestFile(t, 7, 10, 0)
	require.NoError(t, file.Create())

	deleted, err := DeletePBRFile(file.ObjectId, 8)
	require.NoError(t, err)
	assert.False(t, deleted)

	deleted, err = DeletePBRFile(file.ObjectId, 7)
	require.NoError(t, err)
	assert.True(t, deleted)
}

// 配额统计是实例级的（跨密钥累加）。
func TestPBRFilesTotalBytesCountsAllKeys(t *testing.T) {
	setupPBRFileTestDB(t)
	require.NoError(t, newTestFile(t, 7, 100, 0).Create())
	require.NoError(t, newTestFile(t, 8, 250, 0).Create())

	total, err := PBRFilesTotalBytes()

	require.NoError(t, err)
	assert.EqualValues(t, 350, total)
}

// 过期回收：先取 ID，再删行；未过期的与 expires_at=0 的都不动。
func TestPBRFileExpiredIDsAndDelete(t *testing.T) {
	setupPBRFileTestDB(t)
	now := time.Now()

	expired := newTestFile(t, 7, 10, now.Add(-time.Minute).Unix())
	require.NoError(t, expired.Create())
	stillValid := newTestFile(t, 7, 10, now.Add(time.Hour).Unix())
	require.NoError(t, stillValid.Create())
	neverExpires := newTestFile(t, 7, 10, 0)
	require.NoError(t, neverExpires.Create())

	ids, err := PBRFileExpiredIDs(now)
	require.NoError(t, err)
	require.Equal(t, []string{expired.ObjectId}, ids)

	deleted, err := DeleteExpiredPBRFiles(now, ids)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	_, err = GetPBRFile(expired.ObjectId, 7)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = GetPBRFile(stillValid.ObjectId, 7)
	assert.NoError(t, err)
	_, err = GetPBRFile(neverExpires.ObjectId, 7)
	assert.NoError(t, err)
}
