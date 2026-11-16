package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// 成员组（ADR 0009）：可复用的**有序成员子链**。车道链上的元素两态——散成员
// （lane_members 既有形态）与组引用（lane_members.GroupId != 0 的行）；组引用在
// 解析层展开为连续成员区段（routing-spec §1.4），运行态（冷却/熔断/亲和/探测）
// 完全不感知组。
//
// 两层开关：组实体的 Disabled（全局）+ 车道引用行的 Disabled（仅本车道）；
// 任一关闭 = 组内全体成员按既有「人工停用」口径跳过（不写冷却、不动熔断、
// 留痕 attempts[].status=disabled）。组不嵌套、无模式、无六键默认值。

// MemberGroup 成员组实体。
type MemberGroup struct {
	Id int `json:"id" gorm:"primaryKey"`
	// Name 全局唯一（与渠道名、车道名是各自独立的命名空间）。
	Name string `json:"name" gorm:"unique;not null;index"`
	// Disabled 反向命名（零值 = 启用），与 LaneMember.Disabled 同一约束：禁止
	// gorm default、禁止 *bool——理由见 routing-spec §1.2（正向字段会让存量行
	// 迁移后全为 false，等于整组被静默停用）。
	Disabled  bool              `json:"disabled"`
	CreatedAt int64             `json:"created_at" gorm:"bigint"`
	UpdatedAt int64             `json:"updated_at" gorm:"bigint"`
	Members   []MemberGroupItem `json:"members,omitempty" gorm:"foreignKey:GroupId;constraint:OnDelete:CASCADE"`
	// OrphanMemberCount 组内渠道已不存在的成员数；仅展示用，不落库。
	OrphanMemberCount int `json:"-" gorm:"-"`
}

// MemberGroupItem 组内成员：与车道散成员同构（渠道 + 所选模型 + 别名 + 六键覆盖
// + 人工停用），进入所有引用它的车道共享。
type MemberGroupItem struct {
	Id      int `json:"id" gorm:"primaryKey"`
	GroupId int `json:"group_id" gorm:"not null;index"`
	// ChannelId 渠道 id；所选模型的真名仍由 Channel.ModelMapping[Model] ?? Model
	// 推导（ADR 0008 单处规则），组不引入第二套解析。
	ChannelId   int    `json:"channel_id" gorm:"not null"`
	Model       string `json:"model" gorm:"type:varchar(191);not null"`
	PublicAlias string `json:"public_alias" gorm:"type:varchar(191)"`
	// Priority 组内顺序（数字大者优先），与 lane_members.priority 同约定。
	Priority int `json:"priority"`
	// Disabled 反向（零值 = 启用），约束同上。
	Disabled bool `json:"disabled"`
	// Overrides 成员级六键覆盖（JSON，同 LaneMember.Overrides 形态）。
	Overrides string `json:"overrides" gorm:"type:text"`
}

// ErrGroupHasNoMembers 组不允许空成员（空组没有存在意义，删组即可）。
var ErrGroupHasNoMembers = errors.New("member group must have at least one member")

// ErrMemberGroupNotFound 按名未找到成员组。
var ErrMemberGroupNotFound = errors.New("member group not found")

// ListMemberGroups 全部成员组（组内成员按 priority 降序），并统计引用它的车道名。
func ListMemberGroups() ([]MemberGroup, error) {
	var groups []MemberGroup
	if err := DB.Order("name asc").Find(&groups).Error; err != nil {
		return nil, err
	}
	for i := range groups {
		if err := DB.Where("group_id = ?", groups[i].Id).
			Order("priority desc, id asc").Find(&groups[i].Members).Error; err != nil {
			return nil, err
		}
	}
	return groups, nil
}

// GetMemberGroupByName 按名取组（含成员）；未找到返回 ErrMemberGroupNotFound。
func GetMemberGroupByName(name string) (*MemberGroup, error) {
	return getMemberGroupByNameWithDB(DB, name)
}

// GetMemberGroupByNameTx 与 GetMemberGroupByName 相同，但在给定事务内查询。
// SQLite 连接池默认 1，事务内调用全局 DB 会自锁；import 必须用 Tx 版本。
func GetMemberGroupByNameTx(tx *gorm.DB, name string) (*MemberGroup, error) {
	return getMemberGroupByNameWithDB(tx, name)
}

func getMemberGroupByNameWithDB(db *gorm.DB, name string) (*MemberGroup, error) {
	var group MemberGroup
	if err := db.Where("name = ?", name).First(&group).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMemberGroupNotFound
		}
		return nil, err
	}
	if err := db.Where("group_id = ?", group.Id).
		Order("priority desc, id asc").Find(&group.Members).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

// GroupsReferencedBy 返回引用某组的车道名（去重、升序），供列表/详情/删除确认。
func GroupsReferencedBy(groupId int) ([]string, error) {
	var refs []LaneMember
	if err := DB.Where("group_id = ?", groupId).Find(&refs).Error; err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, ref := range refs {
		var lane Lane
		if err := DB.First(&lane, ref.LaneId).Error; err != nil {
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

// UpsertMemberGroup 全量写：先写组与成员（成员整体替换，同 UpsertLane 模式），
// 调用方负责 members 非空校验（ErrGroupHasNoMembers）。组配置改动对引用它的
// 车道立即生效——解析层展开无缓存重存（routing-spec §1.4）。
func UpsertMemberGroup(group *MemberGroup) error {
	return UpsertMemberGroupTx(DB, group)
}

// UpsertMemberGroupTx 在调用方事务内写组；tx 为 nil 时回落到全局 DB。
func UpsertMemberGroupTx(tx *gorm.DB, group *MemberGroup) error {
	if tx == nil {
		tx = DB
	}
	if strings.TrimSpace(group.Name) == "" {
		return errors.New("member group name is required")
	}
	if len(group.Members) == 0 {
		return ErrGroupHasNoMembers
	}
	now := common.GetTimestamp()
	{
		var existing MemberGroup
		err := tx.Where("name = ?", group.Name).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			group.Id = 0
			group.CreatedAt = now
			group.UpdatedAt = now
			if err := tx.Omit("Members").Create(group).Error; err != nil {
				return err
			}
		} else {
			group.Id = existing.Id
			group.CreatedAt = existing.CreatedAt
			group.UpdatedAt = now
			if err := tx.Model(&MemberGroup{}).Where("id = ?", group.Id).Updates(map[string]any{
				"disabled":   group.Disabled,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
			if err := tx.Where("group_id = ?", group.Id).Delete(&MemberGroupItem{}).Error; err != nil {
				return err
			}
		}
		for i := range group.Members {
			group.Members[i].Id = 0
			group.Members[i].GroupId = group.Id
		}
		return tx.Create(&group.Members).Error
	}
}

// DeleteMemberGroupByName 删除组并级联清理：移除所有车道链上的该组引用；链因此
// 为空的车道整条删除（对齐 cleanup-members 口径，ADR 0009 决策 7）。返回
// （被清理但保留的车道, 被整条删除的车道）。被删车道的运行态由调用方清理。
func DeleteMemberGroupByName(name string) (cleaned, deleted []string, err error) {
	group, err := GetMemberGroupByName(name)
	if err != nil {
		return nil, nil, err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		// 先收集受影响车道，再删引用行。
		var refs []LaneMember
		if err := tx.Where("group_id = ?", group.Id).Find(&refs).Error; err != nil {
			return err
		}
		laneIds := map[int]bool{}
		for _, ref := range refs {
			laneIds[ref.LaneId] = true
		}
		if err := tx.Where("group_id = ?", group.Id).Delete(&LaneMember{}).Error; err != nil {
			return err
		}
		for laneId := range laneIds {
			var lane Lane
			if err := tx.First(&lane, laneId).Error; err != nil {
				continue
			}
			var remaining int64
			if err := tx.Model(&LaneMember{}).Where("lane_id = ?", laneId).Count(&remaining).Error; err != nil {
				return err
			}
			if remaining == 0 {
				if err := tx.Where("lane_id = ?", laneId).Delete(&LaneMember{}).Error; err != nil {
					return err
				}
				if err := tx.Delete(&Lane{}, laneId).Error; err != nil {
					return err
				}
				deleted = append(deleted, lane.Name)
			} else {
				cleaned = append(cleaned, lane.Name)
			}
		}
		if err := tx.Where("group_id = ?", group.Id).Delete(&MemberGroupItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&MemberGroup{}, group.Id).Error
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(cleaned)
	sort.Strings(deleted)
	return cleaned, deleted, nil
}

// LoadMemberGroupsByIDs 批量取组（含成员，组内按 priority 降序）。供车道解析展开
// 与写端点判重使用；不存在的 id 跳过（悬空引用按"组不存在"处理，见展开逻辑）。
func LoadMemberGroupsByIDs(ids []int) (map[int]*MemberGroup, error) {
	return loadMemberGroupsByIDsWithDB(DB, ids)
}

// LoadMemberGroupsByIDsTx 与 LoadMemberGroupsByIDs 相同，但在给定事务内查询。
func LoadMemberGroupsByIDsTx(tx *gorm.DB, ids []int) (map[int]*MemberGroup, error) {
	return loadMemberGroupsByIDsWithDB(tx, ids)
}

func loadMemberGroupsByIDsWithDB(db *gorm.DB, ids []int) (map[int]*MemberGroup, error) {
	unique := make([]int, 0, len(ids))
	seen := map[int]bool{}
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return map[int]*MemberGroup{}, nil
	}
	var groups []MemberGroup
	if err := db.Where("id IN ?", unique).Find(&groups).Error; err != nil {
		return nil, err
	}
	out := make(map[int]*MemberGroup, len(groups))
	for i := range groups {
		g := &groups[i]
		if err := db.Where("group_id = ?", g.Id).
			Order("priority desc, id asc").Find(&g.Members).Error; err != nil {
			return nil, err
		}
		out[g.Id] = g
	}
	return out, nil
}

// MemberGroupNames 返回全部组名（供车道写入校验"组名是否存在"与组名唯一性）。
func MemberGroupNames() ([]string, error) {
	var names []string
	if err := DB.Model(&MemberGroup{}).Pluck("name", &names).Error; err != nil {
		return nil, err
	}
	return names, nil
}

// ValidateMemberGroupItems 校验组内成员：渠道存在且无重名歧义、(渠道, 所选模型)
// 组内唯一。返回带渠道 id 的成员切片（保持传入顺序与 priority）。
func ValidateMemberGroupItems(items []MemberGroupItem) ([]MemberGroupItem, error) {
	if len(items) == 0 {
		return nil, ErrGroupHasNoMembers
	}
	seen := map[string]bool{}
	for i := range items {
		item := &items[i]
		item.Model = strings.TrimSpace(item.Model)
		if item.Model == "" {
			return nil, fmt.Errorf("group member[%d].model is required", i)
		}
		ch, err := ChannelOrNil(item.ChannelId)
		if err != nil {
			return nil, err
		}
		if ch == nil {
			return nil, fmt.Errorf("group member channel id %d not found", item.ChannelId)
		}
		key := fmt.Sprintf("%d\u0000%s", item.ChannelId, item.Model)
		if seen[key] {
			return nil, fmt.Errorf("duplicate group member: channel '%s' model '%s'", ch.Name, item.Model)
		}
		seen[key] = true
	}
	return items, nil
}
