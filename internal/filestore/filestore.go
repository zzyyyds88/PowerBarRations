// Package filestore 保存 Anthropic Files 上传的原始字节（ADR 0011）。
//
// 这是本项目唯一打破"不落盘"的地方，边界见 ADR 0011：
//   - 只存客户端显式上传的字节，不做任何隐式抓取；
//   - 单文件上限 model.PBRFileMaxBytes，总量上限 model.PBRFilesTotalQuotaBytes；
//   - 按 expires_after 过期，由系统任务 file_cleanup 清理；
//   - 字节不进数据库、不进日志正文、不进 /api/export。
//
// 本包只负责"字节在磁盘上怎么放、怎么取、怎么删"；归属、配额与生命周期
// 判定在 model.PBRFile 与协议层，避免这里反向依赖业务语义。
package filestore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"
)

// Dir 返回字节存放目录。PBR_FILES_DIR 优先；否则用 SQLite 数据库文件旁边的
// pbr-files/，让数据卷自然覆盖它（与 internal/privacy/pii.Dir 同一约定）。
func Dir() string {
	if v := strings.TrimSpace(os.Getenv("PBR_FILES_DIR")); v != "" {
		return v
	}
	path := common.SQLitePath
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return filepath.Join("pbr-files")
	}
	return filepath.Join(filepath.Dir(path), "pbr-files")
}

// Path 把对象 ID 映射到磁盘路径。
//
// 对象 ID 必须通过 model.IsPBRFileID 的严格校验才允许参与路径派生——这是
// 杜绝目录穿越的唯一入口，任何调用方都不得自行拼接路径。用 ID 的前两位做
// 一级分片，避免单目录堆到上万个文件。
func Path(objectID string) (string, error) {
	objectID = strings.TrimSpace(objectID)
	if !model.IsPBRFileID(objectID) {
		return "", fmt.Errorf("filestore: invalid object id %q", objectID)
	}
	hex := objectID[len(model.PBRFileIDPrefix):]
	return filepath.Join(Dir(), hex[:2], objectID), nil
}

// Save 把 src 的字节写入对象 ID 对应的文件，返回实际写入字节数与内容摘要。
//
// 实现要点：
//   - 先写同目录临时文件再 rename：中断不会留下"看起来完整"的半截文件；
//   - 边写边计数，超过 limit 立即中止并清理，绝不先读完再判（上传体不可重放，
//     但也不能把整个文件读进内存）；
//   - 目录 0710、文件 0600，与 pbr.db 的权限收紧口径一致。
func Save(objectID string, src io.Reader, limit int64) (int64, string, error) {
	if limit <= 0 {
		limit = model.PBRFileMaxBytes
	}
	finalPath, err := Path(objectID)
	if err != nil {
		return 0, "", err
	}
	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0o710); err != nil {
		return 0, "", fmt.Errorf("filestore: create dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return 0, "", fmt.Errorf("filestore: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// 无论成功失败都尝试清理临时文件；rename 成功后它已不存在，Remove 返回
	// 的错误可以忽略。
	defer func() { _ = os.Remove(tmpPath) }()

	hasher := sha256.New()
	// 多读 1 字节用于区分"恰好等于上限"与"超过上限"。
	written, copyErr := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(src, limit+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return 0, "", fmt.Errorf("filestore: write: %w", copyErr)
	}
	if closeErr != nil {
		return 0, "", fmt.Errorf("filestore: close: %w", closeErr)
	}
	if written > limit {
		return 0, "", ErrTooLarge
	}
	if written == 0 {
		return 0, "", ErrEmpty
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return 0, "", fmt.Errorf("filestore: chmod: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return 0, "", fmt.Errorf("filestore: commit: %w", err)
	}
	return written, hex.EncodeToString(hasher.Sum(nil)), nil
}

// 稳定的失败原因：调用方据此映射 HTTP 状态码，不解析文案。
var (
	ErrTooLarge = errors.New("filestore: payload too large")
	ErrEmpty    = errors.New("filestore: empty payload")
	ErrNotFound = errors.New("filestore: object not found")
)

// Open 打开一个对象供读取（content 下载与 file_id 展开共用）。
// 文件不存在返回 ErrNotFound，调用方转 404。
func Open(objectID string) (io.ReadSeekCloser, error) {
	path, err := Path(objectID)
	if err != nil {
		return nil, ErrNotFound
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("filestore: open: %w", err)
	}
	return f, nil
}

// ReadAll 读取整个对象（file_id 展开用）。上限由调用方用 model.PBRFileMaxBytes
// 保证——这里仍加一道防护，避免被篡改的元数据导致无界读入。
func ReadAll(objectID string) ([]byte, error) {
	f, err := Open(objectID)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, model.PBRFileMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("filestore: read: %w", err)
	}
	if int64(len(data)) > model.PBRFileMaxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

// Remove 删除一个对象的字节。文件已不存在视为成功（清理任务幂等）。
func Remove(objectID string) error {
	path, err := Path(objectID)
	if err != nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("filestore: remove: %w", err)
	}
	return nil
}
