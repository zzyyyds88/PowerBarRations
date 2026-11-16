package model

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAsyncObjectTest(t *testing.T) {
	t.Helper()
	require.NoError(t, AutoMigrateAsyncObjects(DB))
	require.NoError(t, DB.Exec("DELETE FROM pbr_async_objects").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM pbr_async_objects")
	})
}

func TestAsyncObjectCreateAndLookup(t *testing.T) {
	setupAsyncObjectTest(t)

	obj := &PBRAsyncObject{
		Kind:       PBRAsyncObjectKindFile,
		Lane:       "lane-a",
		UpstreamId: "file-abc",
		ChannelId:  7,
		Status:     "processed",
		Size:       128,
		CreatedAt:  1000,
		ExpiresAt:  2000,
	}
	require.NoError(t, CreateAsyncObject(obj))

	got, err := GetAsyncObject("lane-a", PBRAsyncObjectKindFile, "file-abc")
	require.NoError(t, err)
	require.Equal(t, 7, got.ChannelId)
	require.Equal(t, "processed", got.Status)
	require.Equal(t, int64(128), got.Size)
	require.Equal(t, int64(2000), got.ExpiresAt)

	// 跨车道不可见：同样的 upstream id 在另一条车道就是 not found。
	_, err = GetAsyncObject("lane-b", PBRAsyncObjectKindFile, "file-abc")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// 不同 kind 也是不同对象。
	_, err = GetAsyncObject("lane-a", PBRAsyncObjectKindBatch, "file-abc")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// 重复写入同一身份是幂等更新，不是唯一约束错误。
	require.NoError(t, CreateAsyncObject(&PBRAsyncObject{
		Kind:       PBRAsyncObjectKindFile,
		Lane:       "lane-a",
		UpstreamId: "file-abc",
		ChannelId:  9,
		Status:     "error",
		Size:       256,
		ExpiresAt:  3000,
	}))
	got, err = GetAsyncObject("lane-a", PBRAsyncObjectKindFile, "file-abc")
	require.NoError(t, err)
	require.Equal(t, 9, got.ChannelId)
	require.Equal(t, "error", got.Status)
	require.Equal(t, int64(256), got.Size)
	require.Equal(t, int64(3000), got.ExpiresAt)
}

func TestAsyncObjectUpdateMeta(t *testing.T) {
	setupAsyncObjectTest(t)

	require.NoError(t, CreateAsyncObject(&PBRAsyncObject{
		Kind:       PBRAsyncObjectKindBatch,
		Lane:       "lane-a",
		UpstreamId: "batch-1",
		ChannelId:  3,
		Status:     "validating",
	}))

	size := int64(0)
	expiresAt := int64(5000)
	require.NoError(t, UpdateAsyncObjectMeta("lane-a", PBRAsyncObjectKindBatch, "batch-1",
		PBRAsyncObjectUpdate{Status: "completed", Size: &size, ExpiresAt: &expiresAt}))

	got, err := GetAsyncObject("lane-a", PBRAsyncObjectKindBatch, "batch-1")
	require.NoError(t, err)
	require.Equal(t, "completed", got.Status)
	require.Equal(t, int64(0), got.Size)
	require.Equal(t, int64(5000), got.ExpiresAt)

	// 空更新是 no-op，而不是把字段清空。
	require.NoError(t, UpdateAsyncObjectMeta("lane-a", PBRAsyncObjectKindBatch, "batch-1",
		PBRAsyncObjectUpdate{}))
	got, err = GetAsyncObject("lane-a", PBRAsyncObjectKindBatch, "batch-1")
	require.NoError(t, err)
	require.Equal(t, "completed", got.Status)

	require.ErrorIs(t,
		UpdateAsyncObjectMeta("lane-a", PBRAsyncObjectKindBatch, "missing", PBRAsyncObjectUpdate{Status: "x"}),
		gorm.ErrRecordNotFound)
}

func TestCleanupExpiredAsyncObjects(t *testing.T) {
	setupAsyncObjectTest(t)

	now := time.Now()
	mk := func(id string, expiresAt int64) *PBRAsyncObject {
		return &PBRAsyncObject{
			Kind:       PBRAsyncObjectKindFile,
			Lane:       "lane-a",
			UpstreamId: id,
			ChannelId:  1,
			Status:     "processed",
			ExpiresAt:  expiresAt,
		}
	}
	require.NoError(t, CreateAsyncObject(mk("old", now.Add(-40*24*time.Hour).Unix())))
	require.NoError(t, CreateAsyncObject(mk("recent", now.Add(-10*24*time.Hour).Unix())))
	require.NoError(t, CreateAsyncObject(mk("no-expiry", 0)))
	require.NoError(t, CreateAsyncObject(mk("future", now.Add(24*time.Hour).Unix())))

	deleted, err := CleanupExpiredAsyncObjects(now)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	// 过期不足 30 天、未声明过期、尚未过期的行都保留。
	for _, id := range []string{"recent", "no-expiry", "future"} {
		_, err := GetAsyncObject("lane-a", PBRAsyncObjectKindFile, id)
		require.NoError(t, err, id)
	}
	_, err = GetAsyncObject("lane-a", PBRAsyncObjectKindFile, "old")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestGetAsyncObjectOwners(t *testing.T) {
	setupAsyncObjectTest(t)

	require.NoError(t, CreateAsyncObject(&PBRAsyncObject{
		Kind: PBRAsyncObjectKindFile, Lane: "lane-a", UpstreamId: "file-shared", ChannelId: 1,
	}))
	require.NoError(t, CreateAsyncObject(&PBRAsyncObject{
		Kind: PBRAsyncObjectKindFile, Lane: "lane-b", UpstreamId: "file-shared", ChannelId: 1,
	}))
	require.NoError(t, CreateAsyncObject(&PBRAsyncObject{
		Kind: PBRAsyncObjectKindFile, Lane: "lane-a", UpstreamId: "file-only-a", ChannelId: 2,
	}))
	require.NoError(t, CreateAsyncObject(&PBRAsyncObject{
		Kind: PBRAsyncObjectKindBatch, Lane: "lane-a", UpstreamId: "batch-only-a", ChannelId: 2,
	}))

	owners, err := GetAsyncObjectOwners(PBRAsyncObjectKindFile,
		[]string{"file-shared", "file-only-a", "missing", "file-shared", " "})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"lane-a", "lane-b"}, owners["file-shared"])
	require.Equal(t, []string{"lane-a"}, owners["file-only-a"])
	require.Empty(t, owners["missing"])

	// kind 是隔离维度：batch 查询看不到同名 file 记录。
	batchOwners, err := GetAsyncObjectOwners(PBRAsyncObjectKindBatch, []string{"file-only-a"})
	require.NoError(t, err)
	require.Empty(t, batchOwners["file-only-a"])

	_, err = GetAsyncObjectOwners("other", []string{"x"})
	require.Error(t, err)
}

func TestAsyncObjectValidationAndDelete(t *testing.T) {
	setupAsyncObjectTest(t)

	require.Error(t, CreateAsyncObject(&PBRAsyncObject{Kind: PBRAsyncObjectKindFile, Lane: "", UpstreamId: "x"}))
	require.Error(t, CreateAsyncObject(&PBRAsyncObject{Kind: "other", Lane: "lane-a", UpstreamId: "x"}))
	require.Error(t, CreateAsyncObject(&PBRAsyncObject{Kind: PBRAsyncObjectKindFile, Lane: "lane-a", UpstreamId: ""}))

	require.NoError(t, CreateAsyncObject(&PBRAsyncObject{
		Kind:       PBRAsyncObjectKindFile,
		Lane:       "lane-a",
		UpstreamId: "file-del",
		ChannelId:  1,
	}))
	require.NoError(t, DeleteAsyncObject("lane-a", PBRAsyncObjectKindFile, "file-del"))
	_, err := GetAsyncObject("lane-a", PBRAsyncObjectKindFile, "file-del")
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}
