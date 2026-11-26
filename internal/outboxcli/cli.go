// Package outboxcli 实现 `pbr outbox` 运维子命令：查看/重试/丢弃聚合 outbox
// 死信。生产机只运行容器，因此入口必须在镜像内的 pbr 二进制里。
//
// 设计前提：pbr_request_logs 汇总行永久保留，明细是聚合的唯一真源。
// 死信只是"增量缓存"的失败项，不会阻断 POST /api/stats/rebuild；
// discard 后执行一次 rebuild 即可从明细恢复聚合。
package outboxcli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// RunCLI 处理 `pbr outbox <status|retry|discard>`。
func RunCLI(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "status":
		return runStatus(args[1:])
	case "retry":
		return runRetry(args[1:])
	case "discard":
		return runDiscard(args[1:])
	default:
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  pbr outbox status  [--db <pbr.db>] [--json]
  pbr outbox retry   [--db <pbr.db>] (--dead | --id <id>)
  pbr outbox discard [--db <pbr.db>] (--dead | --id <id>) --yes`)
}

func dbPath(flagValue string) string {
	path := strings.TrimSpace(flagValue)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("SQLITE_PATH"))
	}
	if path == "" {
		path = common.SQLitePath
	}
	path = strings.TrimPrefix(path, "file:")
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	return path
}

func openDB(path string) (*gorm.DB, func(), error) {
	if path == "" || strings.HasPrefix(path, ":") {
		return nil, nil, fmt.Errorf("无法确定数据库路径：请用 --db 指定")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, nil, fmt.Errorf("数据库不存在: %s", path)
	}
	db, err := gorm.Open(sqlite.Open(common.SQLiteDSN(path)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	return db, closeFn, nil
}

func runStatus(args []string) int {
	fs := flag.NewFlagSet("outbox status", flag.ContinueOnError)
	dbFlag := fs.String("db", "", "目标 PBR SQLite 路径（缺省取 $SQLITE_PATH）")
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	db, closeFn, err := openDB(dbPath(*dbFlag))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer closeFn()
	status, err := model.LoadPBRStatsOutboxStatus(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取 outbox 状态失败: "+err.Error())
		return 1
	}
	if *asJSON {
		raw, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(raw))
		return 0
	}
	fmt.Printf("pending=%d dead=%d\n", status.Pending, status.Dead)
	fmt.Printf("oldest_pending=%s\n", formatUnix(status.OldestPendingAt))
	fmt.Printf("oldest_dead=%s\n", formatUnix(status.OldestDeadAt))
	if status.LastError != "" {
		fmt.Printf("last_error=%s\n", status.LastError)
	}
	return 0
}

func runRetry(args []string) int {
	fs := flag.NewFlagSet("outbox retry", flag.ContinueOnError)
	dbFlag := fs.String("db", "", "目标 PBR SQLite 路径")
	allDead := fs.Bool("dead", false, "重试全部死信")
	id := fs.Int64("id", 0, "只重试指定死信 id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if (*allDead) == (*id > 0) {
		fmt.Fprintln(os.Stderr, "必须且只能指定 --dead 或 --id 之一")
		return 2
	}
	db, closeFn, err := openDB(dbPath(*dbFlag))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer closeFn()
	count, err := model.RetryPBRStatsOutboxDead(db, *id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "重置死信失败: "+err.Error())
		return 1
	}
	fmt.Printf("retried %d dead outbox row(s); running worker will pick them up within a few seconds\n", count)
	return 0
}

func runDiscard(args []string) int {
	fs := flag.NewFlagSet("outbox discard", flag.ContinueOnError)
	dbFlag := fs.String("db", "", "目标 PBR SQLite 路径")
	allDead := fs.Bool("dead", false, "丢弃全部死信")
	id := fs.Int64("id", 0, "只丢弃指定死信 id")
	confirm := fs.Bool("yes", false, "确认删除死信（必填）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if (*allDead) == (*id > 0) {
		fmt.Fprintln(os.Stderr, "必须且只能指定 --dead 或 --id 之一")
		return 2
	}
	if !*confirm {
		fmt.Fprintln(os.Stderr, "discard 会删除死信行；确认请加 --yes")
		return 2
	}
	db, closeFn, err := openDB(dbPath(*dbFlag))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer closeFn()
	count, err := model.DiscardPBRStatsOutboxDead(db, *id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "丢弃死信失败: "+err.Error())
		return 1
	}
	fmt.Printf("discarded %d dead outbox row(s)\n", count)
	fmt.Println("hourly aggregates may miss those requests until POST /api/stats/rebuild completes")
	return 0
}

func formatUnix(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}
