// Package authutil 实现 `pbr auth` 子命令（token-spec §2.4 口令恢复途径之一）。
//
// 现有两条恢复途径都必须有机器访问权限，符合"自用"定位：
//
//  1. PBR_ADMIN_KEY 环境变量显式指定管理密钥（覆盖口令派生）；
//  2. `pbr auth reset` 清除库内 admin_key_sha256，使网关回到未初始化状态，
//     下次访问重新设置口令。
//
// 本包只做第 2 条。reset 是破坏性动作：必须显式 --yes，避免误触把在用网关
// 打回未初始化。它只清除库内凭据摘要，不做备份；需要在 reset 前留底时，
// 请自行复制数据库文件（凭据摘要只入库，不落独立备份文件）。
package authutil

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// RunCLI 处理 "pbr auth" 子命令：
//
//	pbr auth reset --db <pbr.db> --yes
func RunCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pbr auth reset --db <pbr.db> --yes")
		return 2
	}
	switch args[0] {
	case "reset":
		return runReset(args[1:])
	default:
		fmt.Fprintln(os.Stderr, "usage: pbr auth reset --db <pbr.db> --yes")
		return 2
	}
}

func runReset(args []string) int {
	fs := flag.NewFlagSet("auth reset", flag.ContinueOnError)
	dbPath := fs.String("db", "", "目标 PBR SQLite 路径（缺省取 $SQLITE_PATH）")
	confirm := fs.Bool("yes", false, "确认清除管理凭据（必填）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path := strings.TrimSpace(*dbPath)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("SQLITE_PATH"))
	}
	if path == "" {
		path = common.SQLitePath
	}
	path = sqlitePathOnly(path)
	if path == "" {
		fmt.Fprintln(os.Stderr, "无法确定数据库路径：请用 --db 指定")
		return 2
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintln(os.Stderr, "数据库不存在: "+path)
		return 1
	}
	if !*confirm {
		fmt.Fprintln(os.Stderr, "拒绝执行：reset 会把网关打回未初始化，确认请加 --yes")
		return 2
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开数据库失败: "+err.Error())
		return 1
	}
	previousDB := model.DB
	model.DB = db
	defer func() { model.DB = previousDB }()

	cred, err := model.GetPBRAdminCredential()
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取管理凭据失败: "+err.Error())
		return 1
	}
	if cred == nil || cred.AdminKeySha256 == "" {
		fmt.Println("网关当前未初始化，无需 reset。")
		return 0
	}
	if err := model.ResetPBRAdminCredential(); err != nil {
		fmt.Fprintln(os.Stderr, "清除管理凭据失败: "+err.Error())
		return 1
	}
	fmt.Println("已清除库内管理凭据，网关回到未初始化状态（下次访问重新设置口令）。")
	return 0
}

// sqlitePathOnly 去掉 DSN 的查询参数，得到文件路径。
func sqlitePathOnly(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	dsn = strings.TrimPrefix(dsn, "file:")
	if idx := strings.Index(dsn, "?"); idx >= 0 {
		dsn = dsn[:idx]
	}
	return dsn
}
