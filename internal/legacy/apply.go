package legacy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"pbr/common"
	"pbr/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenTarget 打开目标 PBR SQLite 库（离线迁移写入）。
func OpenTarget(path string) (*gorm.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("target db path is required")
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	return db, nil
}

// EnsureSchema 保证目标库具备迁移所需的最小表结构。
func EnsureSchema(db *gorm.DB) error {
	return db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Lane{}, &model.LaneMember{}, &model.ClientKey{})
}

// Apply 把迁移计划幂等地写入目标 PBR 库：渠道/成员按 name upsert，车道整体替换成员，
// 客户端密钥按 name upsert（哈希由明文重新推导）。重复执行结果一致。
func Apply(db *gorm.DB, plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is required")
	}
	previousDB := model.DB
	model.DB = db
	defer func() { model.DB = previousDB }()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	now := common.GetTimestamp()

	channelIDByName := map[string]int{}
	for _, channel := range plan.Channels {
		status := common.ChannelStatusManuallyDisabled
		if channel.Enabled {
			status = common.ChannelStatusEnabled
		}
		channelKey := channel.Key
		if strings.TrimSpace(channelKey) == "" {
			// 设计红线：拿不到真实 key 时留占位符，绝不编造。
			channelKey = "__INJECT_BY_OPERATOR__"
		}
		var existing model.Channel
		err := db.Where("name = ?", channel.Name).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			baseURL := channel.BaseURL
			// 渠道 priority/weight 已删除：迁移时不再写入这两列（路由顺序由车道承载）。
			built := model.Channel{
				Type:        channel.Type,
				Key:         channelKey,
				Status:      status,
				Name:        channel.Name,
				CreatedTime: now,
				UpdatedAt:   now,
				BaseURL:     &baseURL,
				Models:      strings.Join(channel.Models, ","),
				Group:       "default",
			}
			if err := db.Create(&built).Error; err != nil {
				return fmt.Errorf("create channel %s: %w", channel.Name, err)
			}
			if err := built.AddAbilities(nil); err != nil {
				return fmt.Errorf("add abilities for %s: %w", channel.Name, err)
			}
			channelIDByName[channel.Name] = built.Id
			continue
		}
		if err != nil {
			return err
		}
		channelIDByName[channel.Name] = existing.Id
		updates := map[string]any{
			"type":       channel.Type,
			"key":        channelKey,
			"status":     status,
			"base_url":   channel.BaseURL,
			"models":     strings.Join(channel.Models, ","),
			"updated_at": now,
		}
		if err := db.Model(&model.Channel{}).Where("id = ?", existing.Id).Updates(updates).Error; err != nil {
			return fmt.Errorf("update channel %s: %w", channel.Name, err)
		}
		existing.Type = channel.Type
		existing.Status = status
		existing.BaseURL = &channel.BaseURL
		existing.Models = strings.Join(channel.Models, ",")
		existing.Group = "default"
		if err := existing.UpdateAbilities(nil); err != nil {
			return fmt.Errorf("update abilities for %s: %w", channel.Name, err)
		}
	}

	for _, lane := range plan.Lanes {
		built := model.Lane{Name: lane.Name, Mode: lane.Mode, Enabled: true, ActiveMember: lane.ActiveMember}
		for _, member := range lane.Members {
			channelID, ok := channelIDByName[member.Channel]
			if !ok {
				return fmt.Errorf("lane %s references unmigrated channel %s", lane.Name, member.Channel)
			}
			built.Members = append(built.Members, model.LaneMember{
				ChannelId:     channelID,
				UpstreamModel: member.UpstreamModel,
				Priority:      member.Priority,
			})
		}
		if err := model.UpsertLane(&built); err != nil {
			return fmt.Errorf("upsert lane %s: %w", lane.Name, err)
		}
	}

	// 防呆：ClientKey.name 唯一，plan 内若仍有重名，落库必然静默覆盖丢凭据。
	// 规划期已改名，这里再断言一次，宁可失败也不丢密钥。
	seenKeyNames := map[string]bool{}
	for _, key := range plan.Keys {
		if seenKeyNames[key.Name] {
			return fmt.Errorf("plan has duplicate client key name %q; refusing to apply (would silently overwrite)", key.Name)
		}
		seenKeyNames[key.Name] = true
	}

	policy, err := json.Marshal(model.LanePolicy{Mode: model.LanePolicyModeAll, AllowLanes: []string{}, DenyLanes: []string{}})
	if err != nil {
		return err
	}
	for _, key := range plan.Keys {
		built := model.ClientKey{
			Name:        key.Name,
			KeyHash:     model.HashClientKey(key.Plain),
			KeyPrefix:   model.PrefixOfClientKey(key.Plain),
			Enabled:     key.Enabled,
			LanePolicy:  string(policy),
			IPAllowlist: "[]",
			ExpiresAt:   key.ExpiresAt,
			Notes:       key.Notes,
		}
		if err := model.UpsertClientKey(&built); err != nil {
			return fmt.Errorf("upsert client key %s: %w", key.Name, err)
		}
	}

	model.InitChannelCache()
	return nil
}
