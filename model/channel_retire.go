package model

import (
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/zzyyyds88/PowerBarRations/common"
)

// 渠道退役编排（design-v1 §7.9）：停用 → 解除引用 → 删除 → 历史标记。
//
// 它是唯一允许变更车道/成员组的渠道动作，因此必须满足三件事，缺一不可：
// ①调用方先拿 RetirePlan 做完整预览；②控制台二次确认（输入渠道全名）；
// ③结果逐项返回并进审计。除此之外的渠道写操作仍不得静默变更车道链（ADR 0009）。
//
// 历史标记把该渠道的请求日志与统计组键改写为「原名（已退役 YYYY-MM-DD HH:MM）」：
// 历史账保留、检索不断，且同名新渠道不会与退役历史混在同一聚合键下。
// pbr_request_logs 按 member_channel_id 圈定（改名/重建同名渠道互不影响）；
// pbr_stats_hourly 的组键是名称字符串，channel 维度精确匹配、channel_model
// 维度在 Go 侧按分隔符拆头比对（渠道名可含 % 等字符，不能用 LIKE 碰运气）。

// RetiredChannelName 生成退役后的展示名。
func RetiredChannelName(name string, at time.Time) string {
	return name + "（已退役 " + at.Format("2006-01-02 15:04") + "）"
}

// RetirePlan 退役预览：逐项列出将要发生的事（dry-run 与实际执行共用同一份计算）。
type RetirePlan struct {
	Name                string   `json:"name"`
	Disable             bool     `json:"disable"`
	LanesCleaned        []string `json:"lanes_cleaned"`
	LanesDeleted        []string `json:"lanes_deleted"`
	GroupsAffected      []string `json:"groups_affected"`
	GroupsEmptied       []string `json:"groups_emptied"`
	LogsRename          int64    `json:"logs_rename"`
	StatsRowsRename     int64    `json:"stats_rows_rename"`
	PendingAsyncObjects int64    `json:"pending_async_objects"`
}

// RetireResult 退役执行结果：与 RetirePlan 同形，外加改写后的展示名。
type RetireResult struct {
	Retired             bool     `json:"retired"`
	Name                string   `json:"name"`
	RetiredAs           string   `json:"retired_as"`
	Disabled            bool     `json:"disable"`
	LanesCleaned        []string `json:"lanes_cleaned"`
	LanesDeleted        []string `json:"lanes_deleted"`
	GroupsAffected      []string `json:"groups_affected"`
	GroupsEmptied       []string `json:"groups_emptied"`
	ManualLanesCleared  []string `json:"manual_lanes_cleared"`
	LogsRenamed         int64    `json:"logs_renamed"`
	StatsRowsRenamed    int64    `json:"stats_rows_rename"`
	PendingAsyncObjects int64    `json:"pending_async_objects"`
}

// GetRetirePlan 只读收集退役将影响的清单（dry-run 预览），不落任何库。
func GetRetirePlan(name string) (*RetirePlan, error) {
	channel, err := GetChannelByName(name)
	if err != nil {
		return nil, err
	}
	var plan *RetirePlan
	err = DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		plan, txErr = retirePlanTx(tx, channel)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// RetireChannel 执行退役事务，返回逐项结果。now 注入便于测试与展示名稳定。
func RetireChannel(name string, now time.Time) (*RetireResult, error) {
	channel, err := GetChannelByName(name)
	if err != nil {
		return nil, err
	}
	retiredAs := RetiredChannelName(name, now)
	result := &RetireResult{Name: name, RetiredAs: retiredAs}
	err = DB.Transaction(func(tx *gorm.DB) error {
		plan, planErr := retirePlanTx(tx, channel)
		if planErr != nil {
			return planErr
		}
		result.Disabled = plan.Disable
		result.PendingAsyncObjects = plan.PendingAsyncObjects
		// ① 停用：先落安全态——即使后续步骤失败，也不会留下"继续承载流量"的半退役。
		if plan.Disable {
			if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).
				Update("status", common.ChannelStatusManuallyDisabled).Error; err != nil {
				return err
			}
		}
		// ② 解除引用：散成员整段移除，成员组只摘条目（组是复用模板，保留）。
		if err := tx.Where("channel_id = ?", channel.Id).Delete(&LaneMember{}).Error; err != nil {
			return err
		}
		if len(plan.GroupsAffected) > 0 {
			if err := tx.Where("channel_id = ?", channel.Id).Delete(&MemberGroupItem{}).Error; err != nil {
				return err
			}
		}
		// ③ 删除退役后零有效成员的车道（预览已按同一判定圈定），避免留下
		// "解析为零成员"的空壳链；仍有效成员的车道记为 cleaned。
		deleted, cleaned, delErr := retireDeleteLanesTx(tx, plan.LanesDeleted, plan.LanesCleaned)
		if delErr != nil {
			return delErr
		}
		result.LanesDeleted = deleted
		result.LanesCleaned = cleaned
		result.GroupsAffected = plan.GroupsAffected
		result.GroupsEmptied = plan.GroupsEmptied
		// ④ 删除渠道行。不需要引用守卫：引用已在本事务内摘除，事务隔离保证
		// 不会有并发写入引用一个尚未提交删除的渠道。
		if err := tx.Delete(&Channel{}, channel.Id).Error; err != nil {
			return err
		}
		// ⑤ 历史标记：按 member_channel_id 精确圈定，天然规避同名渠道混淆。
		logs := tx.Model(&PBRRequestLog{}).
			Where("member_channel_id = ?", channel.Id).
			Update("member_channel_name", retiredAs)
		if logs.Error != nil {
			return logs.Error
		}
		result.LogsRenamed = logs.RowsAffected
		statsRenamed, statsErr := retireRenameStatsTx(tx, name, retiredAs)
		if statsErr != nil {
			return statsErr
		}
		result.StatsRowsRenamed = statsRenamed
		return nil
	})
	if err != nil {
		return nil, err
	}
	NotifyChannelDeleted(channel)
	result.Retired = true
	return result, nil
}

// retirePlanTx 在事务内计算退役将影响的清单。放在事务里是为了与执行路径
// 共享同一份判定（干跑与实跑不会有第二种语义）。
func retirePlanTx(tx *gorm.DB, channel *Channel) (*RetirePlan, error) {
	plan := &RetirePlan{
		Name:           channel.Name,
		Disable:        channel.Status != common.ChannelStatusManuallyDisabled,
		LanesCleaned:   []string{},
		LanesDeleted:   []string{},
		GroupsAffected: []string{},
		GroupsEmptied:  []string{},
	}

	// 散成员所在车道。
	directLaneIDs := make([]int, 0, 4)
	if err := tx.Model(&LaneMember{}).
		Where("channel_id = ? AND group_id = 0", channel.Id).
		Distinct().Pluck("lane_id", &directLaneIDs).Error; err != nil {
		return nil, err
	}

	// 成员组条目与变空组（组内除本渠道外再无条目）。
	affectedGroupIDs := make([]int, 0, 4)
	if hasMemberGroupItemsTable(tx) {
		if err := tx.Model(&MemberGroupItem{}).
			Where("channel_id = ?", channel.Id).
			Distinct().Pluck("group_id", &affectedGroupIDs).Error; err != nil {
			return nil, err
		}
	}
	emptiedGroupIDs := make(map[int]bool, len(affectedGroupIDs))
	if len(affectedGroupIDs) > 0 {
		var groups []MemberGroup
		if err := tx.Where("id IN ?", affectedGroupIDs).Order("name").Find(&groups).Error; err != nil {
			return nil, err
		}
		for i := range groups {
			plan.GroupsAffected = append(plan.GroupsAffected, groups[i].Name)
			var items int64
			if err := tx.Model(&MemberGroupItem{}).
				Where("group_id = ? AND channel_id <> ?", groups[i].Id, channel.Id).
				Count(&items).Error; err != nil {
				return nil, err
			}
			if items == 0 {
				plan.GroupsEmptied = append(plan.GroupsEmptied, groups[i].Name)
				emptiedGroupIDs[groups[i].Id] = true
			}
		}
	}
	sort.Strings(plan.GroupsAffected)
	sort.Strings(plan.GroupsEmptied)

	// 受影响车道 = 散成员车道 ∪ 引用受影响组的车道。对每条做"退役后还剩
	// 多少有效成员"的模拟：其余散成员 + 指向未变空组的引用行。
	laneIDSet := make(map[int]bool, len(directLaneIDs))
	for _, id := range directLaneIDs {
		laneIDSet[id] = true
	}
	if len(affectedGroupIDs) > 0 {
		var groupLaneIDs []int
		if err := tx.Model(&LaneMember{}).
			Where("group_id IN ?", affectedGroupIDs).
			Distinct().Pluck("lane_id", &groupLaneIDs).Error; err != nil {
			return nil, err
		}
		for _, id := range groupLaneIDs {
			laneIDSet[id] = true
		}
	}
	laneIDs := make([]int, 0, len(laneIDSet))
	for id := range laneIDSet {
		laneIDs = append(laneIDs, id)
	}
	if len(laneIDs) == 0 {
		return plan, nil
	}
	var lanes []Lane
	if err := tx.Where("id IN ?", laneIDs).Order("name").Find(&lanes).Error; err != nil {
		return nil, err
	}
	for i := range lanes {
		lane := &lanes[i]
		var members []LaneMember
		if err := tx.Where("lane_id = ?", lane.Id).Find(&members).Error; err != nil {
			return nil, err
		}
		effective := 0
		for j := range members {
			m := &members[j]
			switch {
			case m.GroupId == 0 && m.ChannelId != channel.Id:
				effective++
			case m.GroupId != 0 && !emptiedGroupIDs[m.GroupId]:
				// 组还有别的渠道条目（或不在本次受影响集合里），退役后仍贡献成员。
				effective++
			}
		}
		if effective == 0 {
			plan.LanesDeleted = append(plan.LanesDeleted, lane.Name)
		} else {
			plan.LanesCleaned = append(plan.LanesCleaned, lane.Name)
		}
	}
	sort.Strings(plan.LanesCleaned)
	sort.Strings(plan.LanesDeleted)

	// 历史改写量（预览与执行同一判定）。
	var logs int64
	if err := tx.Model(&PBRRequestLog{}).
		Where("member_channel_id = ?", channel.Id).
		Count(&logs).Error; err != nil {
		return nil, err
	}
	plan.LogsRename = logs
	statsRows, err := countStatsRowsForChannelTx(tx, channel.Name)
	if err != nil {
		return nil, err
	}
	plan.StatsRowsRename = statsRows

	// 未过期异步对象（ADR 0010 固定到渠道；只提示，不阻断）。
	pending, err := retiredPendingAsyncObjects(tx, channel.Id)
	if err != nil {
		return nil, err
	}
	plan.PendingAsyncObjects = pending
	return plan, nil
}

// retireDeleteLanesTx 删除退役后零有效成员的车道（预览已按同一判定圈定）。
// 删车道前先清掉该车道残余的引用壳（组引用行），与既有空车道删除语义一致。
func retireDeleteLanesTx(tx *gorm.DB, deleted, cleaned []string) (deletedOut, cleanedOut []string, err error) {
	if len(deleted) == 0 {
		out := []string{}
		if len(cleaned) == 0 {
			return out, []string{}, nil
		}
		return out, cleaned, nil
	}
	var lanes []Lane
	if err := tx.Where("name IN ?", deleted).Find(&lanes).Error; err != nil {
		return nil, nil, err
	}
	for i := range lanes {
		if err := tx.Where("lane_id = ?", lanes[i].Id).Delete(&LaneMember{}).Error; err != nil {
			return nil, nil, err
		}
		if err := tx.Delete(&Lane{}, lanes[i].Id).Error; err != nil {
			return nil, nil, err
		}
	}
	deletedOut = append([]string{}, deleted...)
	sort.Strings(deletedOut)
	cleanedOut = cleaned
	if len(cleanedOut) == 0 {
		cleanedOut = []string{}
	}
	return deletedOut, cleanedOut, nil
}

// retireRenameStatsTx 把统计聚合里以渠道名作组键的行改写为退役名。
// channel 维度精确匹配；channel_model 维度取回后按分隔符拆头比对，
// 避免对含 % / _ 的渠道名做 LIKE 误匹配。
func retireRenameStatsTx(tx *gorm.DB, oldName, retiredAs string) (int64, error) {
	res := tx.Model(&PBRStatsHourly{}).
		Where("group_kind = ? AND group_key = ?", "channel", oldName).
		Update("group_key", retiredAs)
	if res.Error != nil {
		return 0, res.Error
	}
	renamed := res.RowsAffected
	var keys []string
	if err := tx.Model(&PBRStatsHourly{}).
		Where("group_kind = ?", "channel_model").
		Distinct().Pluck("group_key", &keys).Error; err != nil {
		return 0, err
	}
	prefix := oldName + PBRChannelModelSeparator
	for _, key := range keys {
		rest, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}
		res := tx.Model(&PBRStatsHourly{}).
			Where("group_kind = ? AND group_key = ?", "channel_model", key).
			Update("group_key", retiredAs+PBRChannelModelSeparator+rest)
		if res.Error != nil {
			return 0, res.Error
		}
		renamed += res.RowsAffected
	}
	return renamed, nil
}

// countStatsRowsForChannelTx 与 retireRenameStatsTx 同口径的计数（预览用）。
func countStatsRowsForChannelTx(tx *gorm.DB, name string) (int64, error) {
	var total int64
	if err := tx.Model(&PBRStatsHourly{}).
		Where("group_kind = ? AND group_key = ?", "channel", name).
		Count(&total).Error; err != nil {
		return 0, err
	}
	var keys []string
	if err := tx.Model(&PBRStatsHourly{}).
		Where("group_kind = ?", "channel_model").
		Distinct().Pluck("group_key", &keys).Error; err != nil {
		return 0, err
	}
	prefix := name + PBRChannelModelSeparator
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			total++
		}
	}
	return total, nil
}

// retiredPendingAsyncObjects 未过期的异步对象数（ADR 0010 固定到渠道；
// 退役后 retrieve 将 503，由过期清理兜底——只提示，不阻断）。
func retiredPendingAsyncObjects(tx *gorm.DB, channelID int) (int64, error) {
	if !tx.Migrator().HasTable(&PBRAsyncObject{}) {
		return 0, nil
	}
	var pending int64
	if err := tx.Model(&PBRAsyncObject{}).
		Where("channel_id = ? AND (expires_at = 0 OR expires_at > ?)", channelID, time.Now().Unix()).
		Count(&pending).Error; err != nil {
		return 0, err
	}
	return pending, nil
}
