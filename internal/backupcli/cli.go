// Package backupcli 实现 `pbr backup` 子命令：不依赖宿主机 python/sqlite3，
// 直接使用二进制内置的 SQLite 驱动做在线一致性快照并 quick_check。
//
// 生产机只运行 Docker、不保留源码，因此备份入口必须是镜像内的 pbr 二进制，
// 而不是仓库脚本。备份只读源库；输出文件 0600，已存在则拒绝覆盖。
package backupcli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// RunCLI 处理 `pbr backup --db <pbr.db> --out <backup.db>`。
func RunCLI(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dbPath := fs.String("db", "", "源 SQLite 路径（缺省取 $SQLITE_PATH）")
	outPath := fs.String("out", "", "输出文件（缺省 ./backups/pbr-<UTC>.db）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	src := strings.TrimSpace(*dbPath)
	if src == "" {
		src = strings.TrimSpace(os.Getenv("SQLITE_PATH"))
	}
	if src == "" {
		src = common.SQLitePath
	}
	src = sqliteFileOnly(src)
	if src == "" {
		fmt.Fprintln(os.Stderr, "无法确定源数据库路径：请用 --db 指定")
		return 2
	}
	if _, err := os.Stat(src); err != nil {
		fmt.Fprintln(os.Stderr, "源数据库不存在: "+src)
		return 1
	}

	dst := strings.TrimSpace(*outPath)
	if dst == "" {
		dst = filepath.Join("backups", "pbr-"+time.Now().UTC().Format("20060102T150405Z")+".db")
	}
	if _, err := os.Stat(dst); err == nil {
		fmt.Fprintln(os.Stderr, "拒绝覆盖已存在的文件: "+dst)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "创建输出目录失败: "+err.Error())
		return 1
	}

	// VACUUM INTO 生成一致快照；对运行中的 WAL 库安全（不阻塞写）。
	// 源连接只加 busy_timeout，不强制切 journal_mode——备份命令不应改源库配置。
	source, err := gorm.Open(sqlite.Open(src+"?_pragma=busy_timeout(5000)"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开源数据库失败: "+err.Error())
		return 1
	}
	if sqlDB, dbErr := source.DB(); dbErr == nil {
		defer sqlDB.Close()
	}
	if err := source.Exec("VACUUM INTO ?", dst).Error; err != nil {
		fmt.Fprintln(os.Stderr, "备份失败: "+err.Error())
		_ = os.Remove(dst)
		return 1
	}

	// 独立打开副本做 quick_check，失败即删除，避免留下一份不可用备份。
	copyDB, err := gorm.Open(sqlite.Open(dst), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "校验备份失败: "+err.Error())
		_ = os.Remove(dst)
		return 1
	}
	var check string
	checkErr := copyDB.Raw("PRAGMA quick_check").Scan(&check).Error
	if sqlDB, dbErr := copyDB.DB(); dbErr == nil {
		_ = sqlDB.Close()
	}
	if checkErr != nil || !strings.EqualFold(strings.TrimSpace(check), "ok") {
		detail := "quick_check returned " + strings.TrimSpace(check)
		if checkErr != nil {
			detail = checkErr.Error()
		}
		fmt.Fprintln(os.Stderr, "备份 quick_check 失败: "+detail)
		_ = os.Remove(dst)
		return 1
	}
	if err := os.Chmod(dst, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "设置备份权限失败: "+err.Error())
		return 1
	}
	fmt.Println("backup ok: " + dst)
	return 0
}

// sqliteFileOnly 去掉 DSN 查询参数与 file: 前缀。
func sqliteFileOnly(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	dsn = strings.TrimPrefix(dsn, "file:")
	if idx := strings.Index(dsn, "?"); idx >= 0 {
		dsn = dsn[:idx]
	}
	if strings.HasPrefix(dsn, ":") {
		return ""
	}
	return dsn
}
