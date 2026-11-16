package model

import (
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
)

// 渠道模型目录读取（design-v1 §3.3）：abilities 表已随"基座旧 priority/weight
// 选路 + 显式渠道 pin"一并物理删除，车道（Lane/LaneMember）是唯一选路入口。
// 这里直接从 channels 表展开 (分组, 模型, 渠道) 行，只服务定价目录、模型清单与
// 管理面元数据展示，不参与任何路由决策。

// channelCapability 是一条由启用渠道直接展开的 (group, model, channel) 行。
type channelCapability struct {
	ChannelId   int
	ChannelName string
	ChannelType int
	Group       string
	Model       string
}

// getEnabledChannelCapabilities 展开所有"启用渠道 × 其分组 × 其声明模型"。
// 行序按渠道 id 升序、组内保持渠道声明顺序，结果稳定可解释。
func getEnabledChannelCapabilities() ([]channelCapability, error) {
	var channels []Channel
	err := DB.Omit("key").Where("status = ?", common.ChannelStatusEnabled).Order("id").Find(&channels).Error
	if err != nil {
		return nil, err
	}
	rows := make([]channelCapability, 0, len(channels))
	for _, channel := range channels {
		for _, group := range channel.GetGroups() {
			if group == "" {
				continue
			}
			for _, modelName := range channel.GetModels() {
				rows = append(rows, channelCapability{
					ChannelId:   channel.Id,
					ChannelName: channel.Name,
					ChannelType: channel.Type,
					Group:       group,
					Model:       modelName,
				})
			}
		}
	}
	return rows, nil
}

// GetGroupEnabledModels 返回该分组下启用渠道声明的去重模型名（保持首次出现顺序）。
func GetGroupEnabledModels(group string) []string {
	group = strings.TrimSpace(group)
	if group == "" {
		return []string{}
	}
	var channels []Channel
	if err := DB.Omit("key").Where("status = ?", common.ChannelStatusEnabled).Order("id").Find(&channels).Error; err != nil {
		return []string{}
	}
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, channel := range channels {
		matched := false
		for _, chGroup := range channel.GetGroups() {
			if chGroup == group {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, modelName := range channel.GetModels() {
			if _, ok := seen[modelName]; ok {
				continue
			}
			seen[modelName] = struct{}{}
			models = append(models, modelName)
		}
	}
	return models
}

// GetEnabledModels 返回所有启用渠道声明的去重模型名。
func GetEnabledModels() []string {
	rows, err := getEnabledChannelCapabilities()
	if err != nil {
		return []string{}
	}
	seen := make(map[string]struct{}, len(rows))
	models := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.Model]; ok {
			continue
		}
		seen[row.Model] = struct{}{}
		models = append(models, row.Model)
	}
	return models
}
