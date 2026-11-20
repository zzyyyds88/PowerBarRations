package filestore

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"
)

func useTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PBR_FILES_DIR", dir)
	return dir
}

func validID(t *testing.T) string {
	t.Helper()
	id, err := model.NewPBRFileID(nil)
	require.NoError(t, err)
	require.True(t, model.IsPBRFileID(id))
	return id
}

// Save/ReadAll 往返：字节原样保存，摘要可校验。
func TestSaveAndReadRoundTrip(t *testing.T) {
	dir := useTempDir(t)
	id := validID(t)
	payload := []byte("hello file bytes")

	written, digest, err := Save(id, bytes.NewReader(payload), model.PBRFileMaxBytes)
	require.NoError(t, err)
	assert.EqualValues(t, len(payload), written)
	assert.Len(t, digest, 64, "返回 sha256 十六进制串")

	data, err := ReadAll(id)
	require.NoError(t, err)
	assert.Equal(t, payload, data)

	// 目录/文件权限必须收紧（与 pbr.db 口径一致）。
	path, err := Path(id)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Equal(t, dir, filepath.Dir(filepath.Dir(path)), "落在一级分片目录下")
}

// 超过上限必须拒绝，且不留下半成品文件。
func TestSaveRejectsOversizedPayload(t *testing.T) {
	useTempDir(t)
	id := validID(t)

	_, _, err := Save(id, bytes.NewReader(bytes.Repeat([]byte("a"), 101)), 100)

	assert.ErrorIs(t, err, ErrTooLarge)
	_, openErr := Open(id)
	assert.ErrorIs(t, openErr, ErrNotFound, "超限上传不得留下文件")
}

// 恰好等于上限应被接受（边界不多读不少读）。
func TestSaveAcceptsExactlyAtLimit(t *testing.T) {
	useTempDir(t)
	id := validID(t)
	payload := bytes.Repeat([]byte("a"), 100)

	written, _, err := Save(id, bytes.NewReader(payload), 100)

	require.NoError(t, err)
	assert.EqualValues(t, 100, written)
}

// 空负载拒绝。
func TestSaveRejectsEmptyPayload(t *testing.T) {
	useTempDir(t)
	id := validID(t)

	_, _, err := Save(id, bytes.NewReader(nil), model.PBRFileMaxBytes)

	assert.ErrorIs(t, err, ErrEmpty)
}

// 非法对象 ID 不得参与路径派生——这是目录穿越的唯一防线。
func TestPathRejectsInvalidIDs(t *testing.T) {
	useTempDir(t)
	for _, id := range []string{
		"../../etc/passwd",
		"pbrfile-../../etc/passwd",
		"pbrfile-0123456789abcdef0123456",   // 少一位
		"pbrfile-0123456789ABCDEF01234567",  // 大写
		"pbrfile-gggggggggggggggggggggggg",  // 非十六进制
		"file-0123456789abcdef01234567",     // 前缀不对
		"",
	} {
		_, err := Path(id)
		assert.Error(t, err, "id=%q 必须被拒", id)
	}
}

// Remove 对不存在的对象视为成功（清理任务需要幂等）。
func TestRemoveIsIdempotent(t *testing.T) {
	useTempDir(t)
	id := validID(t)
	require.NoError(t, Remove(id))
	require.NoError(t, Remove(id))

	_, _, err := Save(id, bytes.NewReader([]byte("x")), model.PBRFileMaxBytes)
	require.NoError(t, err)
	require.NoError(t, Remove(id))
	_, openErr := Open(id)
	assert.ErrorIs(t, openErr, ErrNotFound)
}

// Dir 默认落在 SQLite 数据库旁边，让数据卷自然覆盖（与 privacy 模型目录同约定）。
func TestDirDefaultsBesideDatabase(t *testing.T) {
	t.Setenv("PBR_FILES_DIR", "")
	original := common.SQLitePath
	common.SQLitePath = "/data/pbr.db?_pragma=busy_timeout(30000)"
	t.Cleanup(func() { common.SQLitePath = original })

	assert.Equal(t, filepath.Join("/data", "pbr-files"), Dir())
}

// 内存库（测试环境）没有数据库目录可依附：退化为工作目录下的 pbr-files。
func TestDirFallsBackForMemoryDatabase(t *testing.T) {
	t.Setenv("PBR_FILES_DIR", "")
	original := common.SQLitePath
	common.SQLitePath = ":memory:"
	t.Cleanup(func() { common.SQLitePath = original })

	assert.Equal(t, "pbr-files", Dir())
}

// Open 返回可重复读取的句柄（content 下载与展开共用）。
func TestOpenReturnsSeekableReader(t *testing.T) {
	useTempDir(t)
	id := validID(t)
	payload := []byte("seekable")
	_, _, err := Save(id, bytes.NewReader(payload), model.PBRFileMaxBytes)
	require.NoError(t, err)

	reader, err := Open(id)
	require.NoError(t, err)
	defer reader.Close()

	first, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, payload, first)

	_, err = reader.Seek(0, io.SeekStart)
	require.NoError(t, err)
	second, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, payload, second)
}

// 大文件不整体读进内存（Save 边写边限流）：用一个会记录读取量的 reader 验证
// 读取在超过上限后立即中止，而不是读完整个输入。
func TestSaveStopsReadingAtLimit(t *testing.T) {
	useTempDir(t)
	id := validID(t)

	counting := &countingReader{remaining: 10_000}
	_, _, err := Save(id, counting, 512)

	assert.ErrorIs(t, err, ErrTooLarge)
	assert.LessOrEqual(t, counting.read, int64(513), "超过上限后不得继续读入")
}

type countingReader struct {
	remaining int64
	read      int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	for i := int64(0); i < n; i++ {
		p[i] = 'x'
	}
	r.remaining -= n
	r.read += n
	return int(n), nil
}

// 稳定错误：调用方按 errors.Is 分支，不解析文案。
func TestErrorsAreStable(t *testing.T) {
	assert.True(t, errors.Is(ErrTooLarge, ErrTooLarge))
	assert.False(t, errors.Is(ErrTooLarge, ErrEmpty))
	assert.False(t, errors.Is(ErrNotFound, ErrEmpty))
	assert.NotEmpty(t, ErrNotFound.Error())
}
