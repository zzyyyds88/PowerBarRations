package model

import (
	"sort"

	"gorm.io/gorm"
)

// OrphanCleanupResult 是一次悬空成员清理的结果。
//
// Lane 侧是历史散成员（lane_members.channel_id > 0）指向已删除渠道；Group 侧是
// member_group_items 指向已删除渠道（ADR 0009）。组内成员被清空后组本身不再
// 合法（组不允许空成员），因此会整组删除，并级联清理车道上的组引用；链因此为空
// 的车道也整条删除（与 DeleteMemberGroupByName 的口径一致）。
type OrphanCleanupResult struct {
	CleanedLanes  []string `json:"cleaned_lanes"`
	DeletedLanes  []string `json:"deleted_lanes"`
	CleanedGroups []string `json:"cleaned_groups"`
	DeletedGroups []string `json:"deleted_groups"`
}

// ListOrphanMemberReferences 只读列出悬空成员所在的车道与成员组（供 dry-run）。
//
// 必须排除 group_id != 0 的组引用行：它们 channel_id=0，是引用外壳而不是渠道
// 成员；把它们当悬空成员会把整条组引用链误删（历史 bug）。
func ListOrphanMemberReferences() (lanes []string, groups []string, err error) {
	var laneNames []string
	if err := DB.Table("lane_members AS lm").
		Joins("JOIN lanes AS l ON l.id = lm.lane_id").
		Joins("LEFT JOIN channels AS c ON c.id = lm.channel_id").
		Where("lm.channel_id > 0 AND c.id IS NULL").
		Distinct().Pluck("l.name", &laneNames).Error; err != nil {
		return nil, nil, err
	}
	var groupNames []string
	if hasMemberGroupItemsTable(DB) {
		if err := DB.Table("member_group_items AS i").
			Joins("JOIN member_groups AS g ON g.id = i.group_id").
			Joins("LEFT JOIN channels AS c ON c.id = i.channel_id").
			Where("c.id IS NULL").
			Distinct().Pluck("g.name", &groupNames).Error; err != nil {
			return nil, nil, err
		}
		// 组内孤儿会改变引用组车道的成员链；dry-run 也要把车道列出来。
		if err := DB.Table("lane_members AS lm").
			Joins("JOIN lanes AS l ON l.id = lm.lane_id").
			Joins("JOIN member_group_items AS i ON i.group_id = lm.group_id").
			Joins("LEFT JOIN channels AS c ON c.id = i.channel_id").
			Where("lm.group_id > 0 AND c.id IS NULL").
			Distinct().Pluck("l.name", &laneNames).Error; err != nil {
			return nil, nil, err
		}
	}
	return uniqueSortedStrings(laneNames), uniqueSortedStrings(groupNames), nil
}

// CleanupOrphanMembers 清理散成员与成员组里的悬空渠道成员。
// 整次清理在单事务内完成：失败不会留下“成员已删、组/车道未清”的半状态。
func CleanupOrphanMembers() (*OrphanCleanupResult, error) {
	result := &OrphanCleanupResult{
		CleanedLanes:  []string{},
		DeletedLanes:  []string{},
		CleanedGroups: []string{},
		DeletedGroups: []string{},
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		return cleanupOrphanMembersTx(tx, result)
	})
	if err != nil {
		return nil, err
	}
	result.CleanedLanes = uniqueSortedStrings(result.CleanedLanes)
	result.DeletedLanes = uniqueSortedStrings(result.DeletedLanes)
	result.CleanedGroups = uniqueSortedStrings(result.CleanedGroups)
	result.DeletedGroups = uniqueSortedStrings(result.DeletedGroups)
	deletedLaneSet := make(map[string]bool, len(result.DeletedLanes))
	for _, name := range result.DeletedLanes {
		deletedLaneSet[name] = true
	}
	filteredLanes := make([]string, 0, len(result.CleanedLanes))
	for _, name := range result.CleanedLanes {
		if !deletedLaneSet[name] {
			filteredLanes = append(filteredLanes, name)
		}
	}
	result.CleanedLanes = filteredLanes
	sort.Strings(result.CleanedLanes)
	sort.Strings(result.DeletedLanes)
	return result, nil
}

func cleanupOrphanMembersTx(tx *gorm.DB, result *OrphanCleanupResult) error {
	cleanedLanes, deletedLanes, err := cleanupOrphanLaneMembersTx(tx)
	if err != nil {
		return err
	}
	result.CleanedLanes = append(result.CleanedLanes, cleanedLanes...)
	result.DeletedLanes = append(result.DeletedLanes, deletedLanes...)

	if !hasMemberGroupItemsTable(tx) {
		return nil
	}
	var items []MemberGroupItem
	if err := tx.Where("channel_id NOT IN (SELECT id FROM channels)").Find(&items).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	itemIDs := make([]int, 0, len(items))
	groupIDs := make([]int, 0, len(items))
	seenGroup := map[int]bool{}
	for i := range items {
		itemIDs = append(itemIDs, items[i].Id)
		if !seenGroup[items[i].GroupId] {
			seenGroup[items[i].GroupId] = true
			groupIDs = append(groupIDs, items[i].GroupId)
		}
	}
	if err := tx.Where("id IN ?", itemIDs).Delete(&MemberGroupItem{}).Error; err != nil {
		return err
	}
	for _, groupID := range groupIDs {
		var group MemberGroup
		if err := tx.First(&group, groupID).Error; err != nil {
			continue
		}
		var remaining int64
		if err := tx.Model(&MemberGroupItem{}).Where("group_id = ?", groupID).Count(&remaining).Error; err != nil {
			return err
		}
		if remaining > 0 {
			result.CleanedGroups = append(result.CleanedGroups, group.Name)
			refLanes, refErr := groupsReferencedByTx(tx, groupID)
			if refErr != nil {
				return refErr
			}
			result.CleanedLanes = append(result.CleanedLanes, refLanes...)
			continue
		}
		// 组不允许空成员：整组删除，并级联清理车道引用与空车道。
		var refs []LaneMember
		if err := tx.Where("group_id = ?", groupID).Find(&refs).Error; err != nil {
			return err
		}
		laneIDs := make([]int, 0, len(refs))
		for _, ref := range refs {
			laneIDs = append(laneIDs, ref.LaneId)
		}
		if err := tx.Where("group_id = ?", groupID).Delete(&LaneMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", groupID).Delete(&MemberGroupItem{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&MemberGroup{}, groupID).Error; err != nil {
			return err
		}
		result.DeletedGroups = append(result.DeletedGroups, group.Name)
		for _, laneID := range laneIDs {
			var lane Lane
			if err := tx.First(&lane, laneID).Error; err != nil {
				continue
			}
			var laneRemaining int64
			if err := tx.Model(&LaneMember{}).Where("lane_id = ?", laneID).Count(&laneRemaining).Error; err != nil {
				return err
			}
			if laneRemaining == 0 {
				if err := tx.Where("lane_id = ?", laneID).Delete(&LaneMember{}).Error; err != nil {
					return err
				}
				if err := tx.Delete(&Lane{}, laneID).Error; err != nil {
					return err
				}
				result.DeletedLanes = append(result.DeletedLanes, lane.Name)
			} else {
				result.CleanedLanes = append(result.CleanedLanes, lane.Name)
			}
		}
	}
	return nil
}

func groupsReferencedByTx(tx *gorm.DB, groupID int) ([]string, error) {
	var refs []LaneMember
	if err := tx.Where("group_id = ?", groupID).Find(&refs).Error; err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, ref := range refs {
		var lane Lane
		if err := tx.First(&lane, ref.LaneId).Error; err != nil {
			continue
		}
		names[lane.Name] = true
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}
