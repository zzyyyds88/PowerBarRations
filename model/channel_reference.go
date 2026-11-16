package model

import (
	"sort"
	"strings"

	"gorm.io/gorm"
)

// ChannelReference 描述一个渠道被哪些车道 / 成员组引用（ADR 0009 之后两者必须
// 一起看：车道可以只引用成员组，`lane_members.channel_id` 并不会出现该渠道）。
//
// Lanes 同时包含“散成员所在车道”与“引用含该渠道的成员组的车道”，供删除确认与
// 错误明细展示；Groups 是直接包含该渠道的成员组。任何一项非空都不得静默删除渠道
// 或从渠道模型清单里移除仍被使用的模型。
type ChannelReference struct {
	Lanes  []string `json:"lanes"`
	Groups []string `json:"groups"`
}

// Empty 表示没有任何车道或成员组引用。
func (r *ChannelReference) Empty() bool {
	return r == nil || (len(r.Lanes) == 0 && len(r.Groups) == 0)
}

// BatchReferencesError 表示批量删除在事务内重查时发现渠道仍被引用。
type BatchReferencesError struct {
	Blocked map[string][]string
	Groups  map[string][]string
}

func (e *BatchReferencesError) Error() string {
	return "one or more channels are still referenced by lanes or member groups"
}

// ChannelReferences 返回渠道的全部引用（车道 + 成员组）。
func ChannelReferences(channelId int) (*ChannelReference, error) {
	return channelReferencesWith(DB, channelId)
}

// ChannelReferencesTx 是事务内版本：删除/收窄渠道前在同一事务里重查引用，
// 避免“检查后被并发插入成员”留下悬空引用。
func ChannelReferencesTx(tx *gorm.DB, channelId int) (*ChannelReference, error) {
	if tx == nil {
		tx = DB
	}
	return channelReferencesWith(tx, channelId)
}

func channelReferencesWith(db *gorm.DB, channelId int) (*ChannelReference, error) {
	refs := &ChannelReference{Lanes: []string{}, Groups: []string{}}
	if channelId <= 0 {
		return refs, nil
	}

	laneIDs := make([]int, 0, 4)
	if err := db.Model(&LaneMember{}).
		Where("channel_id = ?", channelId).
		Distinct().Pluck("lane_id", &laneIDs).Error; err != nil {
		return nil, err
	}

	var groupIDs []int
	if hasMemberGroupItemsTable(db) {
		if err := db.Model(&MemberGroupItem{}).
			Where("channel_id = ?", channelId).
			Distinct().Pluck("group_id", &groupIDs).Error; err != nil {
			return nil, err
		}
	}
	if len(groupIDs) > 0 {
		var groups []MemberGroup
		if err := db.Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
			return nil, err
		}
		for i := range groups {
			refs.Groups = append(refs.Groups, groups[i].Name)
		}
		var groupLaneIDs []int
		if err := db.Model(&LaneMember{}).
			Where("group_id IN ?", groupIDs).
			Distinct().Pluck("lane_id", &groupLaneIDs).Error; err != nil {
			return nil, err
		}
		laneIDs = append(laneIDs, groupLaneIDs...)
	}

	if len(laneIDs) > 0 {
		var lanes []Lane
		if err := db.Where("id IN ?", laneIDs).Find(&lanes).Error; err != nil {
			return nil, err
		}
		for i := range lanes {
			refs.Lanes = append(refs.Lanes, lanes[i].Name)
		}
	}
	refs.Lanes = uniqueSortedStrings(refs.Lanes)
	refs.Groups = uniqueSortedStrings(refs.Groups)
	return refs, nil
}

// RemovedModelReferences 返回“本次从渠道移除的模型”仍被哪些车道 / 成员组使用。
//
// 判据是成员的**所选模型**（`lane_members.model` / `member_group_items.model`），
// 不是车道名：ADR 0006 允许任意已声明模型进入任意车道，车道名与成员模型可以不同。
// 历史散成员的 model 为空时按车道名兜底，保证旧数据仍能被守卫识别。
func RemovedModelReferences(channelId int, removed []string) (*ChannelReference, error) {
	return removedModelReferencesWith(DB, channelId, removed)
}

// RemovedModelReferencesTx 是事务内版本，供写事务在提交前重查引用。
func RemovedModelReferencesTx(tx *gorm.DB, channelId int, removed []string) (*ChannelReference, error) {
	if tx == nil {
		tx = DB
	}
	return removedModelReferencesWith(tx, channelId, removed)
}

func removedModelReferencesWith(db *gorm.DB, channelId int, removed []string) (*ChannelReference, error) {
	refs := &ChannelReference{Lanes: []string{}, Groups: []string{}}
	removedSet := normalizedStringSet(removed)
	if channelId <= 0 || len(removedSet) == 0 {
		return refs, nil
	}
	removedList := sortedKeys(removedSet)

	// 散成员：model 命中被移除集合；历史空 model 用车道名兜底。
	var directLanes []string
	if err := db.Table("lane_members AS lm").
		Joins("JOIN lanes AS l ON l.id = lm.lane_id").
		Where("lm.channel_id = ?", channelId).
		Where("(lm.model IN ? OR (COALESCE(lm.model, '') = '' AND l.name IN ?))", removedList, removedList).
		Distinct().Pluck("l.name", &directLanes).Error; err != nil {
		return nil, err
	}
	refs.Lanes = append(refs.Lanes, directLanes...)

	// 成员组：组内成员的 model 命中被移除集合。
	var groupIDs []int
	if hasMemberGroupItemsTable(db) {
		if err := db.Model(&MemberGroupItem{}).
			Where("channel_id = ? AND model IN ?", channelId, removedList).
			Distinct().Pluck("group_id", &groupIDs).Error; err != nil {
			return nil, err
		}
	}
	if len(groupIDs) > 0 {
		var groups []MemberGroup
		if err := db.Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
			return nil, err
		}
		for i := range groups {
			refs.Groups = append(refs.Groups, groups[i].Name)
		}
		var groupLaneIDs []int
		if err := db.Model(&LaneMember{}).
			Where("group_id IN ?", groupIDs).
			Distinct().Pluck("lane_id", &groupLaneIDs).Error; err != nil {
			return nil, err
		}
		if len(groupLaneIDs) > 0 {
			var lanes []Lane
			if err := db.Where("id IN ?", groupLaneIDs).Find(&lanes).Error; err != nil {
				return nil, err
			}
			for i := range lanes {
				refs.Lanes = append(refs.Lanes, lanes[i].Name)
			}
		}
	}
	refs.Lanes = uniqueSortedStrings(refs.Lanes)
	refs.Groups = uniqueSortedStrings(refs.Groups)
	return refs, nil
}

// removedDirectLaneMembersWith 返回本次应移除的散成员行（只按所选模型判定；
// 历史空 model 用车道名兜底）。组内成员不在这里处理：组是不可拆的复用单元，
// 模型移除必须先编辑组成员的配置，不能由渠道写操作静默改组。
func removedDirectLaneMembersWith(db *gorm.DB, channelId int, removed []string) ([]LaneMember, error) {
	removedSet := normalizedStringSet(removed)
	if channelId <= 0 || len(removedSet) == 0 {
		return nil, nil
	}
	var members []LaneMember
	if err := db.Where("channel_id = ?", channelId).Find(&members).Error; err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}
	laneIDs := make([]int, 0, len(members))
	for i := range members {
		laneIDs = append(laneIDs, members[i].LaneId)
	}
	var lanes []Lane
	if err := db.Where("id IN ?", laneIDs).Find(&lanes).Error; err != nil {
		return nil, err
	}
	laneNames := make(map[int]string, len(lanes))
	for i := range lanes {
		laneNames[lanes[i].Id] = lanes[i].Name
	}
	out := make([]LaneMember, 0, len(members))
	for i := range members {
		modelName := strings.TrimSpace(members[i].Model)
		if modelName == "" {
			modelName = laneNames[members[i].LaneId]
		}
		if removedSet[modelName] {
			out = append(out, members[i])
		}
	}
	return out, nil
}

func normalizedStringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = true
		}
	}
	return out
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// hasMemberGroupItemsTable 兼容只建了部分表的测试库/旧库：组表不存在时按
// “没有组引用”处理，不让引用守卫把整个渠道写操作打成 500。生产库由 AutoMigrate
// 保证表存在。
func hasMemberGroupItemsTable(db *gorm.DB) bool {
	return db != nil && db.Migrator().HasTable(&MemberGroupItem{})
}
