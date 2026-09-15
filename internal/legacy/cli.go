package legacy

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

// RunCLI 执行 pbr migrate：读旧两层库 → 产出计划与对账 → 离线写入目标 PBR 库。
//
// 用法：
//
//	pbr migrate --routing <octopus.db> --vendor <new-api.db> --target <pbr.db> \
//	            [--keys octopus|newapi|both] [--report report.json] [--dry-run]
//
// 只读旧库副本；真实渠道名/地址/key 只落目标库与报告文件，不打印到 stdout。
func RunCLI(args []string) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	routingPath := fs.String("routing", "", "octopus 路由层 SQLite 副本路径（只读）")
	vendorPath := fs.String("vendor", "", "new-api 厂商层 SQLite 副本路径（只读）")
	targetPath := fs.String("target", "", "目标 PBR SQLite 路径（离线写入）")
	reportPath := fs.String("report", "", "对账报告输出路径（JSON；缺省只打印摘要）")
	keySource := fs.String("keys", "both", "客户端密钥来源：octopus|newapi|both")
	dryRun := fs.Bool("dry-run", false, "只产出计划与报告，不写目标库")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *routingPath == "" || *vendorPath == "" {
		fmt.Fprintln(os.Stderr, "usage: pbr migrate --routing <octopus.db> --vendor <new-api.db> [--target <pbr.db>] [--report report.json] [--keys both] [--dry-run]")
		return 2
	}
	if !ValidKeySource(*keySource) {
		fmt.Fprintf(os.Stderr, "非法的 --keys 值 %q：只接受 octopus|newapi|both\n", *keySource)
		return 2
	}

	routing, err := ReadRouting(*routingPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取路由层失败: "+err.Error())
		return 1
	}
	vendor, err := ReadVendor(*vendorPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取厂商层失败: "+err.Error())
		return 1
	}
	plan, err := BuildPlan(routing, vendor, *keySource)
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成迁移计划失败: "+err.Error())
		return 1
	}

	if *reportPath != "" {
		payload := map[string]any{
			"report":   plan.Report,
			"channels": plannedChannelNames(plan),
			"lanes":    plannedLaneNames(plan),
			"keys":     plannedKeyNames(plan),
		}
		encoded, encodeErr := json.MarshalIndent(payload, "", "  ")
		if encodeErr != nil {
			fmt.Fprintln(os.Stderr, "编码报告失败: "+encodeErr.Error())
			return 1
		}
		if writeErr := os.WriteFile(*reportPath, encoded, 0o600); writeErr != nil {
			fmt.Fprintln(os.Stderr, "写报告失败: "+writeErr.Error())
			return 1
		}
	}

	if !*dryRun {
		db, openErr := OpenTarget(*targetPath)
		if openErr != nil {
			fmt.Fprintln(os.Stderr, "打开目标库失败: "+openErr.Error())
			return 1
		}
		if schemaErr := EnsureSchema(db); schemaErr != nil {
			fmt.Fprintln(os.Stderr, "目标库结构初始化失败: "+schemaErr.Error())
			return 1
		}
		if applyErr := Apply(db, plan); applyErr != nil {
			fmt.Fprintln(os.Stderr, "写入目标库失败: "+applyErr.Error())
			return 1
		}
	}

	printSummary(os.Stdout, plan, *dryRun, *targetPath)
	return 0
}

func plannedChannelNames(plan *Plan) []string {
	out := make([]string, 0, len(plan.Channels))
	for _, channel := range plan.Channels {
		out = append(out, channel.Name)
	}
	return out
}

func plannedLaneNames(plan *Plan) []string {
	out := make([]string, 0, len(plan.Lanes))
	for _, lane := range plan.Lanes {
		out = append(out, lane.Name)
	}
	return out
}

func plannedKeyNames(plan *Plan) []string {
	out := make([]string, 0, len(plan.Keys))
	for _, key := range plan.Keys {
		out = append(out, key.Name)
	}
	return out
}

func printSummary(out io.Writer, plan *Plan, dryRun bool, targetPath string) {
	report := plan.Report
	mode := "APPLIED"
	if dryRun {
		mode = "DRY-RUN"
	}
	fmt.Fprintf(out, "pbr migrate %s\n", mode)
	if targetPath != "" && !dryRun {
		fmt.Fprintf(out, "target: %s\n", targetPath)
	}
	fmt.Fprintf(out, "routing layer: channels=%d groups=%d members=%d\n", report.RoutingChannels, report.RoutingGroups, report.RoutingMembers)
	fmt.Fprintf(out, "vendor layer: channels=%d abilities=%d\n", report.VendorChannels, report.VendorAbilities)
	fmt.Fprintf(out, "planned: channels=%d lanes=%d members=%d keys=%d (key_source=%s)\n",
		report.PlannedChannels, report.PlannedLanes, report.PlannedMembers, report.PlannedKeys, report.KeySource)
	fmt.Fprintf(out, "reconcile: unresolved=%d ambiguous=%d widened=%d ignored_vendor_channels=%d duplicate_key_names=%d\n",
		len(report.Unresolved), len(report.Ambiguous), len(report.Widened), report.IgnoredVendor, len(report.DuplicateKeyNames))
	if len(report.DuplicateKeyNames) > 0 {
		fmt.Fprintf(out, "已对 %d 条同名不同明文的客户端密钥改名（详见报告 duplicate_key_names）\n", len(report.DuplicateKeyNames))
	}
	if len(report.Unresolved) > 0 || len(report.Ambiguous) > 0 {
		fmt.Fprintln(out, "存在待人工裁决项，详见报告文件（不打印私有名称）")
	}
}
