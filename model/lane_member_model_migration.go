package model

import (
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

// lane_members 的模型语义迁移（ADR 0008）。
//
// 背景：`lane_members.upstream_model` 存的是**解析后的上游真名**（成员级覆盖）；
// 新口径下成员只存**所选模型** `model`，真名一律由 `Channel.ModelMapping[model] ?? model`
// 推导。列名随之从 `upstream_model` 改为 `model`（AutoMigrate 只加列不改名，
// 所以必须在这里显式重命名 + 回填）。
//
// 回填规则（逐成员，目标是**行为保持**——迁移前后解析结果一致）：
//  1. 当前值命中所属渠道某个映射的**右值** → 写回对应**左键**（该成员原本就是由这个
//     模型映射来的，这是最常见的情形：加入成员时被物化写入）；
//  2. 未命中 → 原值写回（该值本就是模型名，或用户手填的真名；推导结果不变）；
//  3. 空值 → 写回**车道路由键**（复现旧的"回落 `mapping[路由键]`"行为）。
//
// 三种情形下 `effectiveUpstreamModel(channel, 迁移后的值)` 都等于迁移前的真名；
// 唯一会变化的是"当前值恰好又是某映射左键"的碰撞情形，那在新口径下正是正确结果。
//
// `lanes.active_member` 同步重写：旧标签是 `channel/真名`，新标签是 `channel/模型`。
//
// 幂等：列已改名为 `model` 时直接返回。失败只记日志、不阻塞启动
// （与 migrateDropAbilitiesTable 的容错口径一致）——迁移失败时成员仍能读到旧值，
// 但真名推导会退化为"用旧真名当模型名"，需在日志里显式提示人工检查。
func migrateLaneMemberModelSemantics(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if !db.Migrator().HasTable(&LaneMember{}) {
		return nil
	}
	// **不能用 db.Migrator().HasColumn 判断**：GORM 会拿结构体的 `column:model` tag 去
	// 匹配，在"旧表只有 upstream_model"时也返回 true（实测假阳性），于是跳过重命名、
	// 迁移静默失败。这里直接读表结构，问数据库自己有什么列。
	columns, err := tableColumns(db, "lane_members")
	if err != nil {
		common.SysError("pbr: read lane_members columns failed: " + err.Error())
		return err
	}
	hasOld := columns["upstream_model"]
	hasNew := columns["model"]
	if !hasOld {
		// 已是新结构（或全新库）：AutoMigrate 会保证 `model` 存在。
		return nil
	}
	if !hasNew {
		if err := db.Migrator().RenameColumn(&LaneMember{}, "upstream_model", "model"); err != nil {
			common.SysError("pbr: rename lane_members.upstream_model -> model failed: " + err.Error())
			return err
		}
		common.SysLog("pbr: renamed lane_members.upstream_model -> model")
	} else {
		// 两列并存（此前迁移失败留下的中间态）：把旧列数据搬到新列再删旧列，
		// 避免"新列为空、旧列有值"导致成员模型名全丢。
		if err := db.Exec("UPDATE lane_members SET model = upstream_model WHERE (model IS NULL OR model = '') AND upstream_model IS NOT NULL AND upstream_model <> ''").Error; err != nil {
			common.SysError("pbr: copy lane_members.upstream_model -> model failed: " + err.Error())
			return err
		}
		// 用原始 SQL 删列：`Migrator().DropColumn(&LaneMember{}, "upstream_model")` 同样受
		// 结构体 tag 影响（结构体已无该字段，GORM 会认为"列不存在"而静默跳过，实测踩到）。
		if err := db.Exec("ALTER TABLE lane_members DROP COLUMN upstream_model").Error; err != nil {
			common.SysError("pbr: drop lane_members.upstream_model failed: " + err.Error())
			return err
		}
		common.SysLog("pbr: consolidated lane_members.upstream_model into model")
	}

	// 逐车道回填：需要该车道的路由键（规则 3）与每个成员的渠道映射（规则 1/2）。
	var lanes []Lane
	if err := db.Preload("Members").Find(&lanes).Error; err != nil {
		common.SysError("pbr: load lanes for member model backfill failed: " + err.Error())
		return err
	}
	mappingByChannel := map[int]map[string]string{}
	channelNameByID := map[int]string{}
	rewritten := 0
	for i := range lanes {
		lane := &lanes[i]
		for j := range lane.Members {
			member := &lane.Members[j]
			value := strings.TrimSpace(member.Model)
			if value == "" {
				value = lane.Name
			} else {
				channel, chErr := ChannelOrNil(member.ChannelId)
				if chErr != nil {
					// 查询渠道失败不能静默跳过回填：那会让迁移"看起来成功、实际没换语义"。
					common.SysError("pbr: load channel " + strconv.Itoa(member.ChannelId) +
						" during member-model migration failed: " + chErr.Error())
					return chErr
				}
				if channel != nil {
					if _, ok := mappingByChannel[channel.Id]; !ok {
						mappingByChannel[channel.Id] = channel.ModelMappingMap()
						channelNameByID[channel.Id] = channel.Name
					}
					if mappedModel := mappingKeyForValue(mappingByChannel[channel.Id], value); mappedModel != "" {
						value = mappedModel
					}
				}
			}
			if value == member.Model {
				continue
			}
			if err := db.Model(&LaneMember{}).Where("id = ?", member.Id).
				Update("model", value).Error; err != nil {
				common.SysError("pbr: backfill lane_members.model failed: " + err.Error())
				return err
			}
			rewritten++
		}
	}
	if rewritten > 0 {
		common.SysLog("pbr: backfilled lane_members.model for " + strconv.Itoa(rewritten) + " member(s)")
	}

	// active_member 标签：`channel/真名` → `channel/模型`。
	return migrateLaneActiveMemberLabels(db, mappingByChannel, channelNameByID)
}

// tableColumns 返回某张表的列名集合。
//
// 走 `ColumnTypes`（各 dialector 自己实现）而不是 `HasColumn(&Struct{}, "col")`：
// 后者拿结构体的 `column:` tag 去匹配，在"旧表只有 upstream_model"时也会对 `model`
// 返回 true（实测假阳性），会让迁移静默跳过。
func tableColumns(db *gorm.DB, table string) (map[string]bool, error) {
	types, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(types))
	for _, column := range types {
		out[column.Name()] = true
	}
	return out, nil
}

// mappingKeyForValue 反查映射：给定"上游真名"，返回对应的映射左键（模型名）。
// 多个左键映射到同一右值时取字典序最小的那个，保证迁移结果确定、可重复。
func mappingKeyForValue(mapping map[string]string, value string) string {
	best := ""
	for model, upstream := range mapping {
		if strings.TrimSpace(upstream) != value {
			continue
		}
		if best == "" || model < best {
			best = model
		}
	}
	return best
}

// migrateLaneActiveMemberLabels 把 manual 车道的 active_member 标签从
// `channel/真名` 重写为 `channel/模型`（ADR 0008 的成员身份变更）。
func migrateLaneActiveMemberLabels(db *gorm.DB, mappingByChannel map[int]map[string]string, channelNameByID map[int]string) error {
	var lanes []Lane
	if err := db.Where("active_member <> ''").Preload("Members").Find(&lanes).Error; err != nil {
		common.SysError("pbr: load lanes for active_member rewrite failed: " + err.Error())
		return err
	}
	for i := range lanes {
		lane := &lanes[i]
		active := strings.TrimSpace(lane.ActiveMember)
		// 别名形式的 active_member 不含 "/"，不参与重写。
		channelName, upstream, ok := strings.Cut(active, "/")
		if !ok {
			continue
		}
		// 在成员链里找"渠道名 + 真名"匹配的那一条，取其 Model 作为新标签。
		newLabel := ""
		for j := range lane.Members {
			member := &lane.Members[j]
			name, known := channelNameByID[member.ChannelId]
			if !known {
				if ch, err := ChannelOrNil(member.ChannelId); err == nil && ch != nil {
					name = ch.Name
				}
			}
			if name != channelName {
				continue
			}
			mapping, loaded := mappingByChannel[member.ChannelId]
			if !loaded {
				mapping = map[string]string{}
				if ch, err := ChannelOrNil(member.ChannelId); err == nil && ch != nil {
					mapping = ch.ModelMappingMap()
				}
			}
			if effectiveUpstreamModelByMapping(mapping, member.Model) == upstream {
				newLabel = channelName + "/" + member.Model
				break
			}
		}
		if newLabel == "" || newLabel == active {
			continue
		}
		if err := db.Model(&Lane{}).Where("id = ?", lane.Id).
			Update("active_member", newLabel).Error; err != nil {
			common.SysError("pbr: rewrite lanes.active_member failed: " + err.Error())
			return err
		}
		common.SysLog("pbr: rewrote active_member " + active + " -> " + newLabel)
	}
	return nil
}

// effectiveUpstreamModelByMapping 是 effectiveUpstreamModel 的纯映射版本（迁移期用，
// 避免为每个成员再查一次渠道）。
func effectiveUpstreamModelByMapping(mapping map[string]string, modelName string) string {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return ""
	}
	if mapped := strings.TrimSpace(mapping[modelName]); mapped != "" {
		return mapped
	}
	return modelName
}
