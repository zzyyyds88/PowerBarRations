package legacy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/model"
)

// MemberPlan 是拟落库的 PBR 车道成员。
type MemberPlan struct {
	Channel       string
	UpstreamModel string
	Priority      int
	Weight        int
}

// LanePlan 是拟落库的 PBR 显式车道。
type LanePlan struct {
	Name         string
	Mode         string
	ActiveMember string
	Members      []MemberPlan
}

// ChannelPlan 是拟落库的 PBR 渠道。
type ChannelPlan struct {
	Name     string
	Type     int
	BaseURL  string
	Key      string
	Models   []string
	Priority int
	Weight   int
	Enabled  bool
}

// KeyPlan 是拟落库的 PBR 客户端密钥（含只用于离线写入的明文）。
type KeyPlan struct {
	Name      string
	Plain     string
	Enabled   bool
	ExpiresAt *int64
	Notes     string
	Source    string
}

// Issue 是对账报告里的"待人工裁决/未归属/被放宽"项。
type Issue struct {
	Kind    string
	Lane    string
	Model   string
	Channel string
	Detail  string
}

// DuplicateKeyName 记录一次「同名不同明文」的客户端密钥改名事件。
//
// 库内 ClientKey.name 有唯一约束，两条同名密钥若都落库，后写会覆盖先写、
// 使先写那把永久 401（MIGRATION.md 的对账承诺要求条数与哈希一致）。因此规划期
// 必须改名并在此留痕，运维可据此核对。
type DuplicateKeyName struct {
	Name      string `json:"name"`       // 旧库里的原始名字
	KeyPrefix string `json:"key_prefix"` // 明文展示前缀（不泄漏完整密钥）
	Hash8     string `json:"sha256_8"`   // 明文 sha256 前 8 位，用于跨库比对
	RenamedTo string `json:"renamed_to"` // 落库时实际使用的唯一名字
}

// Report 是迁移对账报告（结构统计 + 待裁决清单）。
type Report struct {
	RoutingChannels   int                `json:"routing_channels"`
	RoutingGroups     int                `json:"routing_groups"`
	RoutingMembers    int                `json:"routing_members"`
	VendorChannels    int                `json:"vendor_channels"`
	VendorAbilities   int                `json:"vendor_abilities"`
	PlannedChannels   int                `json:"planned_channels"`
	PlannedLanes      int                `json:"planned_lanes"`
	PlannedMembers    int                `json:"planned_members"`
	PlannedKeys       int                `json:"planned_keys"`
	KeySource         string             `json:"key_source"`
	DuplicateKeyNames []DuplicateKeyName `json:"duplicate_key_names"`
	Unresolved        []Issue            `json:"unresolved"`
	Ambiguous         []Issue            `json:"ambiguous"`
	Widened           []Issue            `json:"widened_permissions"`
	IgnoredVendor     int                `json:"ignored_vendor_channels"`
}

// Plan 是一次迁移的完整产物。
type Plan struct {
	Channels []ChannelPlan
	Lanes    []LanePlan
	Keys     []KeyPlan
	Report   *Report
}

func parseModelMapping(raw string) map[string]string {
	out := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func mapLaneMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "failover", "":
		return "failover", true
	case "manual":
		return "manual", true
	case "weighted", "round_robin":
		// 这两种负载均衡模式已删除：迁移时降级为 failover 并上报。
		return "failover", false
	default:
		return "failover", false
	}
}

// BuildPlan 按 design-v1 §11.1 把两层旧数据折算为 PBR 配置。
//
// keySource 取 "octopus"、"newapi" 或 "both"，决定下游客户端密钥从哪一层导入。
func BuildPlan(routing *RoutingData, vendor *VendorData, keySource string) (*Plan, error) {
	if routing == nil || vendor == nil {
		return nil, fmt.Errorf("routing and vendor data are required")
	}
	if keySource == "" {
		keySource = "both"
	}
	if !ValidKeySource(keySource) {
		return nil, fmt.Errorf("invalid key source %q: want octopus|newapi|both", keySource)
	}
	plan := &Plan{Report: &Report{}}
	report := plan.Report
	report.KeySource = keySource
	report.RoutingChannels = len(routing.Channels)
	report.RoutingGroups = len(routing.Groups)
	report.RoutingMembers = len(routing.GroupItems)
	report.VendorChannels = len(vendor.Channels)
	report.VendorAbilities = len(vendor.Abilities)

	vendorChannelByID := map[int]VendorChannel{}
	for _, channel := range vendor.Channels {
		vendorChannelByID[channel.ID] = channel
	}
	abilityByModel := map[string][]VendorAbility{}
	for _, ability := range vendor.Abilities {
		if !ability.Enabled {
			continue
		}
		abilityByModel[ability.Model] = append(abilityByModel[ability.Model], ability)
	}
	channelModelByID := map[int]RoutingChannelModel{}
	for _, channelModel := range routing.ChannelModels {
		channelModelByID[channelModel.ID] = channelModel
	}

	// 渠道按 name 去重；不同 id 同名时加 id 后缀（不许静默合并）。
	channelNameByID := map[int]string{}
	usedChannelNames := map[string]int{}
	channelModels := map[int]map[string]bool{}
	channelPriority := map[int]int{}
	channelWeight := map[int]int{}
	referenced := map[int]bool{}

	pickChannelName := func(channel VendorChannel) string {
		if name, ok := channelNameByID[channel.ID]; ok {
			return name
		}
		name := strings.TrimSpace(channel.Name)
		if name == "" {
			name = fmt.Sprintf("channel-%d", channel.ID)
		}
		if owner, taken := usedChannelNames[name]; taken && owner != channel.ID {
			name = fmt.Sprintf("%s-%d", name, channel.ID)
		}
		usedChannelNames[name] = channel.ID
		channelNameByID[channel.ID] = name
		return name
	}

	// 车道按 name 排序，保证产物稳定（幂等对账依赖确定性）。
	groups := append([]RoutingGroup(nil), routing.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Name == groups[j].Name {
			return groups[i].ID < groups[j].ID
		}
		return groups[i].Name < groups[j].Name
	})

	for _, group := range groups {
		mode, knownMode := mapLaneMode(group.Mode)
		if !knownMode {
			report.Widened = append(report.Widened, Issue{Kind: "lane_mode", Lane: group.Name, Detail: "unknown mode mapped to failover: " + group.Mode})
		}
		lane := LanePlan{Name: group.Name, Mode: mode}

		memberByItemID := map[int]int{}
		items := make([]RoutingGroupItem, 0)
		for _, item := range routing.GroupItems {
			if item.GroupID == group.ID {
				items = append(items, item)
			}
		}
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Priority != items[j].Priority {
				return items[i].Priority > items[j].Priority
			}
			left, right := channelModelByID[items[i].ChannelModelID], channelModelByID[items[j].ChannelModelID]
			return left.Name < right.Name
		})

		for _, item := range items {
			channelModel, ok := channelModelByID[item.ChannelModelID]
			if !ok {
				report.Unresolved = append(report.Unresolved, Issue{Kind: "member", Lane: group.Name, Detail: fmt.Sprintf("channel_model %d missing", item.ChannelModelID)})
				continue
			}
			requestName := strings.TrimSpace(channelModel.Name)
			candidates := abilityByModel[requestName]
			if len(candidates) == 0 {
				report.Unresolved = append(report.Unresolved, Issue{Kind: "model", Lane: group.Name, Model: requestName, Detail: "no enabled vendor ability"})
				continue
			}
			bestPriority, bestWeight := candidates[0].Priority, candidates[0].Weight
			for _, candidate := range candidates[1:] {
				if candidate.Priority > bestPriority || (candidate.Priority == bestPriority && candidate.Weight > bestWeight) {
					bestPriority, bestWeight = candidate.Priority, candidate.Weight
				}
			}
			top := make([]VendorAbility, 0, 2)
			for _, candidate := range candidates {
				if candidate.Priority == bestPriority && candidate.Weight == bestWeight {
					top = append(top, candidate)
				}
			}
			sort.Slice(top, func(i, j int) bool { return top[i].ChannelID < top[j].ChannelID })
			chosen := top[0]
			if len(top) > 1 {
				report.Ambiguous = append(report.Ambiguous, Issue{
					Kind: "tie", Lane: group.Name, Model: requestName,
					Detail: fmt.Sprintf("同 (priority=%d, weight=%d) 命中 %d 个渠道，已取最小 channel_id=%d", bestPriority, bestWeight, len(top), chosen.ChannelID),
				})
			}
			vendorChannel, ok := vendorChannelByID[chosen.ChannelID]
			if !ok {
				report.Unresolved = append(report.Unresolved, Issue{Kind: "model", Lane: group.Name, Model: requestName, Detail: fmt.Sprintf("ability references missing channel %d", chosen.ChannelID)})
				continue
			}
			upstreamModel := requestName
			if mapped, ok := parseModelMapping(vendorChannel.ModelMapping)[requestName]; ok && strings.TrimSpace(mapped) != "" {
				upstreamModel = strings.TrimSpace(mapped)
			}
			name := pickChannelName(vendorChannel)
			referenced[vendorChannel.ID] = true
			if channelModels[vendorChannel.ID] == nil {
				channelModels[vendorChannel.ID] = map[string]bool{}
			}
			channelModels[vendorChannel.ID][upstreamModel] = true
			if _, seen := channelPriority[vendorChannel.ID]; !seen {
				channelPriority[vendorChannel.ID] = vendorChannel.Priority
				channelWeight[vendorChannel.ID] = vendorChannel.Weight
			}
			memberByItemID[item.ID] = len(lane.Members)
			lane.Members = append(lane.Members, MemberPlan{Channel: name, UpstreamModel: upstreamModel, Priority: item.Priority})
		}

		if mode == "manual" && group.ActiveItemID != 0 {
			if index, ok := memberByItemID[group.ActiveItemID]; ok {
				member := lane.Members[index]
				lane.ActiveMember = member.Channel + "/" + member.UpstreamModel
			} else {
				report.Unresolved = append(report.Unresolved, Issue{Kind: "manual_active", Lane: group.Name, Detail: "active_item_id did not resolve to a migrated member"})
			}
		}

		if len(lane.Members) == 0 {
			report.Unresolved = append(report.Unresolved, Issue{Kind: "lane", Lane: group.Name, Detail: "no members migrated; lane skipped"})
			continue
		}
		plan.Lanes = append(plan.Lanes, lane)
	}

	channelIDs := make([]int, 0, len(channelModels))
	for id := range channelModels {
		channelIDs = append(channelIDs, id)
	}
	sort.Ints(channelIDs)
	for _, id := range channelIDs {
		vendorChannel := vendorChannelByID[id]
		models := make([]string, 0, len(channelModels[id]))
		for model := range channelModels[id] {
			models = append(models, model)
		}
		sort.Strings(models)
		enabled := vendorChannel.Status == 1 && strings.TrimSpace(vendorChannel.Key) != ""
		plan.Channels = append(plan.Channels, ChannelPlan{
			Name:     channelNameByID[id],
			Type:     vendorChannel.Type,
			BaseURL:  vendorChannel.BaseURL,
			Key:      vendorChannel.Key,
			Models:   models,
			Priority: channelPriority[id],
			Weight:   channelWeight[id],
			Enabled:  enabled,
		})
	}
	report.IgnoredVendor = len(vendor.Channels) - len(referenced)

	// 客户端密钥：先按明文去重（同一把密钥出现在两层只导一次），再给最终名字去重。
	//
	// 关键：库内 ClientKey.name 有唯一约束，且 upsert 按 name 定位。若两条不同明文的
	// 密钥拿到同一个最终名字，后写会覆盖先写、丢凭据。所以名字索引必须建立在**最终
	// 名字**上（旧实现错误地拿明文哈希当索引去查名字，改名逻辑永不触发）。
	keyPlans := map[string]KeyPlan{}    // 明文 -> 计划
	usedKeyNames := map[string]string{} // 最终名字 -> 占用它的明文
	appendKey := func(name, plain, source string, enabled bool, expiresAt *int64, widened string) {
		plain = strings.TrimSpace(plain)
		if plain == "" {
			return
		}
		digest := plain
		if existing, ok := keyPlans[digest]; ok {
			if widened != "" {
				report.Widened = append(report.Widened, Issue{Kind: "client_key", Channel: existing.Name, Detail: widened})
			}
			return
		}
		base := strings.TrimSpace(name)
		if base == "" {
			base = source + "-key"
		}
		candidate := base
		if owner, clash := usedKeyNames[candidate]; clash && owner != digest {
			candidate = base + "-" + source
		}
		for suffix := 2; ; suffix++ {
			owner, clash := usedKeyNames[candidate]
			if !clash || owner == digest {
				break
			}
			candidate = fmt.Sprintf("%s-%s-%d", base, source, suffix)
		}
		if candidate != base {
			report.DuplicateKeyNames = append(report.DuplicateKeyNames, DuplicateKeyName{
				Name:      base,
				KeyPrefix: model.PrefixOfClientKey(plain),
				Hash8:     model.HashClientKey(plain)[:8],
				RenamedTo: candidate,
			})
		}
		plan := KeyPlan{Name: candidate, Plain: plain, Enabled: enabled, ExpiresAt: expiresAt, Source: source}
		if widened != "" {
			plan.Notes = widened
			report.Widened = append(report.Widened, Issue{Kind: "client_key", Channel: plan.Name, Detail: widened})
		}
		usedKeyNames[candidate] = digest
		keyPlans[digest] = plan
	}

	if keySource == "octopus" || keySource == "both" {
		for _, key := range routing.APIKeys {
			var expiresAt *int64
			if key.ExpireAt > 0 {
				value := key.ExpireAt
				expiresAt = &value
			}
			widened := ""
			if strings.TrimSpace(key.SupportedModels) != "" && strings.TrimSpace(key.SupportedModels) != "[]" {
				widened = "旧密钥带 supported_models 白名单，已放宽为全部车道"
			}
			appendKey(key.Name, key.Key, "octopus", key.Enabled, expiresAt, widened)
		}
	}
	if keySource == "newapi" || keySource == "both" {
		for _, token := range vendor.Tokens {
			if token.Deleted {
				continue
			}
			var expiresAt *int64
			if token.ExpiredTime > 0 {
				value := token.ExpiredTime
				expiresAt = &value
			}
			widened := ""
			if token.ModelLimitsEnabled || (strings.TrimSpace(token.ModelLimits) != "" && token.ModelLimits != "null" && token.ModelLimits != "[]") {
				widened = "旧令牌带 model_limits，已放宽为全部车道"
			}
			if strings.TrimSpace(token.AllowIPs) != "" {
				if widened != "" {
					widened += "；"
				}
				widened += "旧令牌带 IP 白名单，已放宽"
			}
			appendKey(token.Name, token.Key, "newapi", token.Status == 1, expiresAt, widened)
		}
	}
	digests := make([]string, 0, len(keyPlans))
	for digest := range keyPlans {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	for _, digest := range digests {
		plan.Keys = append(plan.Keys, keyPlans[digest])
	}

	report.PlannedChannels = len(plan.Channels)
	report.PlannedLanes = len(plan.Lanes)
	report.PlannedKeys = len(plan.Keys)
	for _, lane := range plan.Lanes {
		report.PlannedMembers += len(lane.Members)
	}
	sortIssues(report.Unresolved)
	sortIssues(report.Ambiguous)
	sortIssues(report.Widened)
	sort.SliceStable(report.DuplicateKeyNames, func(i, j int) bool {
		if report.DuplicateKeyNames[i].Name != report.DuplicateKeyNames[j].Name {
			return report.DuplicateKeyNames[i].Name < report.DuplicateKeyNames[j].Name
		}
		return report.DuplicateKeyNames[i].RenamedTo < report.DuplicateKeyNames[j].RenamedTo
	})
	return plan, nil
}

// ValidKeySource 判断 --keys 取值是否受支持。
func ValidKeySource(source string) bool {
	switch source {
	case "octopus", "newapi", "both":
		return true
	default:
		return false
	}
}

func sortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Lane != issues[j].Lane {
			return issues[i].Lane < issues[j].Lane
		}
		if issues[i].Model != issues[j].Model {
			return issues[i].Model < issues[j].Model
		}
		return issues[i].Detail < issues[j].Detail
	})
}
