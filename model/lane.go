package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"pbr/common"
	"pbr/setting/ratio_setting"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 本文件实现 PowerBarRations 的路由核心：**请求里的模型名即路由键**。
//
// 解析顺序（见 docs/routing-spec-v1.md §1.1）：
//  1. 同名启用车道（人工配置的成员链，支持改名/别名/池化）；
//  2. 否则没有可调用的路由 → 由调用方返回 503 No available channel for model <X>。
//
// 不存在"隐式车道"（ADR 0005）：渠道声明只是车道成员的候选来源，必须显式固化成
// 车道后才可调用。上游真名解析：成员级覆盖 > 渠道 model_mapping > 路由键。
//
// 约定：priority 一律 **数字大者优先**（与上游渠道优先级语义一致）。

const (
	RouteSourceExplicit = "explicit"
	// RouteSourceImplicit 已废弃（ADR 0005 删除隐式链），保留常量避免历史数据/调用方编译失败。
	RouteSourceImplicit = "implicit"
	// RouteSourceUnconfigured：渠道声明了该模型但没有对应启用车道 → 不可调用。
	RouteSourceUnconfigured = "unconfigured"

	LaneModeFailover   = "failover"
	LaneModeManual     = "manual"
	LaneModeWeighted   = "weighted"
	LaneModeRoundRobin = "round_robin"
)

// LaneRelayConfig 车道六键（design-v1 §7.3）。零值在 Normalize 时回落到默认值。
type LaneRelayConfig struct {
	MemberMaxAttempts                     int `json:"member_max_attempts"`
	MemberRetryIntervalSeconds            int `json:"member_retry_interval_seconds"`
	MemberNonStreamResponseTimeoutSeconds int `json:"member_non_stream_response_timeout_seconds"`
	MemberStreamFirstEventTimeoutSeconds  int `json:"member_stream_first_event_timeout_seconds"`
	MemberCooldownSeconds                 int `json:"member_cooldown_seconds"`
	MemberAffinitySeconds                 int `json:"member_affinity_seconds"`
}

// LaneRelayOverrides 成员级六键覆盖；nil 字段表示继承车道。
type LaneRelayOverrides struct {
	MemberMaxAttempts                     *int `json:"member_max_attempts,omitempty"`
	MemberRetryIntervalSeconds            *int `json:"member_retry_interval_seconds,omitempty"`
	MemberNonStreamResponseTimeoutSeconds *int `json:"member_non_stream_response_timeout_seconds,omitempty"`
	MemberStreamFirstEventTimeoutSeconds  *int `json:"member_stream_first_event_timeout_seconds,omitempty"`
	MemberCooldownSeconds                 *int `json:"member_cooldown_seconds,omitempty"`
	MemberAffinitySeconds                 *int `json:"member_affinity_seconds,omitempty"`
}

// OptionLaneDefaults 是 system/options 里"默认六键"的键名（JSON 形态的 LaneRelayConfig）。
const OptionLaneDefaults = "PBRLaneDefaults"

// BuiltinLaneRelayConfig 内置默认六键（2/3/120/30/60/0）。
//
// 与 DefaultLaneRelayConfig 的区别：前者是**编译期兜底**，后者是**当前生效值**
// （可能被 `PUT /api/system/options.lane_defaults` 改过）。需要"不可变基线"的地方
// （解析配置时补零）必须用这个函数，否则会把自己的输出再喂回自己。
func BuiltinLaneRelayConfig() LaneRelayConfig {
	return LaneRelayConfig{
		MemberMaxAttempts:                     2,
		MemberRetryIntervalSeconds:            3,
		MemberNonStreamResponseTimeoutSeconds: 120,
		MemberStreamFirstEventTimeoutSeconds:  30,
		MemberCooldownSeconds:                 60,
		// 亲和默认 0：不做粘滞，避免掩盖 priority 语义（design-v1 §7.3）。
		MemberAffinitySeconds: 0,
	}
}

// laneDefaultsProvider 由外部注入"默认六键"的当前值（接 system/options）。
// 放在 model 外部注入是为了避免 model 反向依赖 common.OptionMap 的装配顺序。
var laneDefaultsProvider func() LaneRelayConfig

// SetLaneDefaultsProvider 注入默认六键来源；未注入时用内置默认。
func SetLaneDefaultsProvider(provider func() LaneRelayConfig) {
	laneDefaultsProvider = provider
}

// DefaultLaneRelayConfig 返回当前生效的默认六键：默认取上游路由层
// DefaultGroupRelayConfig，但亲和改为 0（2/3/120/30/60/0；design-v1 §7.3 明确
// 不做粘滞，避免掩盖 priority 语义），可经 system/options 覆盖。
//
// 它是"新建/一键固化车道写入的初值"与"车道未显式配置时的回落值"。
func DefaultLaneRelayConfig() LaneRelayConfig {
	if laneDefaultsProvider == nil {
		return BuiltinLaneRelayConfig()
	}
	return laneDefaultsProvider()
}

// Normalize 用默认值补齐零值字段，保证下游拿到的是可直接使用的配置。
func (c LaneRelayConfig) Normalize() LaneRelayConfig {
	def := DefaultLaneRelayConfig()
	if c.MemberMaxAttempts <= 0 {
		c.MemberMaxAttempts = def.MemberMaxAttempts
	}
	if c.MemberRetryIntervalSeconds < 0 {
		c.MemberRetryIntervalSeconds = def.MemberRetryIntervalSeconds
	}
	if c.MemberNonStreamResponseTimeoutSeconds <= 0 {
		c.MemberNonStreamResponseTimeoutSeconds = def.MemberNonStreamResponseTimeoutSeconds
	}
	if c.MemberStreamFirstEventTimeoutSeconds <= 0 {
		c.MemberStreamFirstEventTimeoutSeconds = def.MemberStreamFirstEventTimeoutSeconds
	}
	if c.MemberCooldownSeconds <= 0 {
		c.MemberCooldownSeconds = def.MemberCooldownSeconds
	}
	if c.MemberAffinitySeconds < 0 {
		c.MemberAffinitySeconds = def.MemberAffinitySeconds
	}
	return c
}

// Apply 把成员级覆盖叠加到车道配置上。
func (c LaneRelayConfig) Apply(o LaneRelayOverrides) LaneRelayConfig {
	if o.MemberMaxAttempts != nil {
		c.MemberMaxAttempts = *o.MemberMaxAttempts
	}
	if o.MemberRetryIntervalSeconds != nil {
		c.MemberRetryIntervalSeconds = *o.MemberRetryIntervalSeconds
	}
	if o.MemberNonStreamResponseTimeoutSeconds != nil {
		c.MemberNonStreamResponseTimeoutSeconds = *o.MemberNonStreamResponseTimeoutSeconds
	}
	if o.MemberStreamFirstEventTimeoutSeconds != nil {
		c.MemberStreamFirstEventTimeoutSeconds = *o.MemberStreamFirstEventTimeoutSeconds
	}
	if o.MemberCooldownSeconds != nil {
		c.MemberCooldownSeconds = *o.MemberCooldownSeconds
	}
	if o.MemberAffinitySeconds != nil {
		c.MemberAffinitySeconds = *o.MemberAffinitySeconds
	}
	return c.Normalize()
}

// ParseLaneRelayConfig 解析车道 Config 字段（空串 → 默认值）。
func ParseLaneRelayConfig(raw string) LaneRelayConfig {
	cfg := DefaultLaneRelayConfig()
	if strings.TrimSpace(raw) == "" {
		return cfg
	}
	// 先反序列化到零值结构体，再交给 Normalize 填默认，避免"客户端显式写 0"被误当成未设置。
	var parsed LaneRelayConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return cfg
	}
	return parsed.Normalize()
}

// ParseLaneRelayOverrides 解析成员级覆盖（空串 → 无覆盖）。
func ParseLaneRelayOverrides(raw string) LaneRelayOverrides {
	var o LaneRelayOverrides
	if strings.TrimSpace(raw) == "" {
		return o
	}
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return LaneRelayOverrides{}
	}
	return o
}

// Lane 车道：唯一路由入口（ADR 0005）。
type Lane struct {
	Id   int    `json:"id" gorm:"primaryKey"`
	Name string `json:"name" gorm:"unique;not null;index"`
	Mode string `json:"mode" gorm:"type:varchar(32);default:'failover'"`
	// 不要给 Enabled 加 gorm default：GORM 会忽略零值，导致"显式停用"被默认值 true 覆盖。
	Enabled bool `json:"enabled"`
	// ActiveMember 仅 manual 模式使用：人工指定的成员（别名或 channel/upstream 标签）。
	ActiveMember string       `json:"active_member" gorm:"type:varchar(255)"`
	Config       string       `json:"config" gorm:"type:text"` // 六键 JSON
	CreatedAt    int64        `json:"created_at" gorm:"bigint"`
	UpdatedAt    int64        `json:"updated_at" gorm:"bigint"`
	Members      []LaneMember `json:"members,omitempty" gorm:"foreignKey:LaneId;constraint:OnDelete:CASCADE"`
}

// LaneMember 车道成员：渠道 + 上游真名。
type LaneMember struct {
	Id            int    `json:"id" gorm:"primaryKey"`
	LaneId        int    `json:"lane_id" gorm:"not null;index"`
	ChannelId     int    `json:"channel_id" gorm:"not null"`
	UpstreamModel string `json:"upstream_model" gorm:"not null"`
	PublicAlias   string `json:"public_alias" gorm:"type:varchar(128);index"`
	Priority      int    `json:"priority"` // 数字大者优先
	Weight        int    `json:"weight"`
	Overrides     string `json:"overrides" gorm:"type:text"` // 成员级六键覆盖 JSON
}

// RouteMember 一次解析出的成员（对外展示与选路共用）。
type RouteMember struct {
	ChannelId     int    `json:"channel_id"`
	Channel       string `json:"channel"`
	UpstreamModel string `json:"upstream_model"`
	// UpstreamOverride 是成员级显式改名（数据库原值，可能为空）。为空表示"用渠道映射"。
	UpstreamOverride string `json:"upstream_override,omitempty"`
	PublicAlias      string `json:"public_alias,omitempty"`
	Priority         int    `json:"priority"`
	Weight           int    `json:"weight"`
	MemberId         int    `json:"member_id,omitempty"`
	Overrides        string `json:"-"`
}

// ResolvedRoute 一个路由键解析出的完整成员链。
type ResolvedRoute struct {
	Model  string          `json:"model"`
	Source string          `json:"source"`
	Mode   string          `json:"mode"`
	Config LaneRelayConfig `json:"config"`
	// RouteKey 规范化后的路由键：显式车道（含别名点名）为车道名，隐式为请求模型名。
	// 令牌的车道权限以此判定（token-spec §3.2）。
	RouteKey string `json:"route_key"`
	// ActiveMember 仅 manual 模式使用：人工指定的成员，取值优先匹配成员的
	// PublicAlias，其次匹配 "channel/upstream_model" 标签；为空即"无可用"。
	ActiveMember string `json:"active_member,omitempty"`
	// PinnedMemberId 非 0 表示请求点名了某个成员别名，选路时应先尝试该成员。
	PinnedMemberId int           `json:"pinned_member_id,omitempty"`
	Members        []RouteMember `json:"members"`
}

// EffectiveConfig 返回某成员生效的六键（车道默认 + 成员级覆盖）。
func (r *ResolvedRoute) EffectiveConfig(member *RouteMember) LaneRelayConfig {
	cfg := r.Config.Normalize()
	if member != nil && strings.TrimSpace(member.Overrides) != "" {
		cfg = cfg.Apply(ParseLaneRelayOverrides(member.Overrides))
	}
	return cfg
}

// ModelSummary 用于 GET /models 的概览。
type ModelSummary struct {
	Model       string `json:"model"`
	Source      string `json:"source"`
	Routable    bool   `json:"routable"`
	MemberCount int    `json:"member_count"`
}

// ---------- 车道 CRUD ----------

func ListLanes() ([]Lane, error) {
	var lanes []Lane
	if err := DB.Order("name asc").Find(&lanes).Error; err != nil {
		return nil, err
	}
	for i := range lanes {
		if err := DB.Where("lane_id = ?", lanes[i].Id).Order("priority desc, id asc").Find(&lanes[i].Members).Error; err != nil {
			return nil, err
		}
	}
	return lanes, nil
}

func GetLaneByName(name string) (*Lane, error) {
	var lane Lane
	if err := DB.Where("name = ?", name).First(&lane).Error; err != nil {
		return nil, err
	}
	if err := DB.Where("lane_id = ?", lane.Id).Order("priority desc, id asc").Find(&lane.Members).Error; err != nil {
		return nil, err
	}
	return &lane, nil
}

func GetLaneById(id int) (*Lane, error) {
	var lane Lane
	if err := DB.Where("id = ?", id).First(&lane).Error; err != nil {
		return nil, err
	}
	if err := DB.Where("lane_id = ?", lane.Id).Order("priority desc, id asc").Find(&lane.Members).Error; err != nil {
		return nil, err
	}
	return &lane, nil
}

// GetLaneByAlias 按成员 public_alias 找显式车道，并返回被点名的成员。
func GetLaneByAlias(alias string) (*Lane, *LaneMember, error) {
	if strings.TrimSpace(alias) == "" {
		return nil, nil, gorm.ErrRecordNotFound
	}
	var member LaneMember
	if err := DB.Where("public_alias = ?", alias).First(&member).Error; err != nil {
		return nil, nil, err
	}
	lane, err := GetLaneById(member.LaneId)
	if err != nil {
		return nil, nil, err
	}
	return lane, &member, nil
}

func DeleteLaneByName(name string) error {
	lane, err := GetLaneByName(name)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("lane_id = ?", lane.Id).Delete(&LaneMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Lane{}, lane.Id).Error
	})
}

// UpsertLane 全量写：先写车道与成员，再由调用方重新读取作为回读。
func UpsertLane(lane *Lane) error {
	if strings.TrimSpace(lane.Name) == "" {
		return errors.New("lane name is required")
	}
	if lane.Mode == "" {
		lane.Mode = LaneModeFailover
	}
	if !ValidLaneMode(lane.Mode) {
		return fmt.Errorf("invalid lane mode: %s", lane.Mode)
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing Lane
		err := tx.Where("name = ?", lane.Name).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			lane.Id = 0
			lane.CreatedAt = now
			lane.UpdatedAt = now
			// 不带关联创建：成员由下方显式插入，否则 GORM 会连同 Members 关联再插一遍。
			if err := tx.Omit(clause.Associations).Create(lane).Error; err != nil {
				return err
			}
		} else {
			lane.Id = existing.Id
			lane.CreatedAt = existing.CreatedAt
			lane.UpdatedAt = now
			if err := tx.Model(&Lane{}).Where("id = ?", lane.Id).Updates(map[string]any{
				"mode":          lane.Mode,
				"enabled":       lane.Enabled,
				"active_member": lane.ActiveMember,
				"config":        lane.Config,
				"updated_at":    now,
			}).Error; err != nil {
				return err
			}
			if err := tx.Where("lane_id = ?", lane.Id).Delete(&LaneMember{}).Error; err != nil {
				return err
			}
		}
		for i := range lane.Members {
			lane.Members[i].Id = 0
			lane.Members[i].LaneId = lane.Id
		}
		if len(lane.Members) > 0 {
			if err := tx.Create(&lane.Members).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ValidLaneMode 校验模式取值。
func ValidLaneMode(mode string) bool {
	switch mode {
	case LaneModeFailover, LaneModeManual, LaneModeWeighted, LaneModeRoundRobin:
		return true
	}
	return false
}

// LaneNames 返回全部车道名，供 API 校验"车道名与成员别名冲突"。
func LaneNames() ([]string, error) {
	var names []string
	if err := DB.Model(&Lane{}).Pluck("name", &names).Error; err != nil {
		return nil, err
	}
	return names, nil
}

// ---------- 解析 ----------

func channelServesModel(channel *Channel, modelName string) bool {
	for _, m := range channel.GetModels() {
		if strings.TrimSpace(m) == modelName {
			return true
		}
	}
	return false
}

// ResolveRoute 按路由键解析成员链。返回的 Members 可能为空（表示没有任何渠道声明该模型）。
//
// 先按请求原文精确匹配；未命中时再按 new-api 既有的思考后缀归一化规则匹配一次
// （design-v1 §4.1「保留思考后缀归一化行为」），保证存量下游带后缀的请求不断流。
func ResolveRoute(modelName string) (*ResolvedRoute, error) {
	route, err := resolveExactRoute(modelName)
	if err != nil || len(route.Members) > 0 || route.Source == RouteSourceExplicit {
		return route, err
	}
	normalized := ratio_setting.RoutingMatchModelName(modelName)
	if normalized == "" || normalized == modelName {
		return route, nil
	}
	fallback, err := resolveExactRoute(normalized)
	if err != nil {
		return nil, err
	}
	if len(fallback.Members) == 0 && fallback.Source != RouteSourceExplicit {
		return route, nil
	}
	// 命中归一化名后，路由键仍是请求原文（响应回填、日志、令牌判定都用原文）。
	fallback.Model = modelName
	return fallback, nil
}

func resolveExactRoute(modelName string) (*ResolvedRoute, error) {
	route := &ResolvedRoute{
		Model:    modelName,
		RouteKey: modelName,
		Source:   RouteSourceUnconfigured,
		Mode:     LaneModeFailover,
		Config:   DefaultLaneRelayConfig(),
		Members:  []RouteMember{},
	}
	if strings.TrimSpace(modelName) == "" {
		return route, nil
	}

	// 唯一路由入口：同名启用车道；否则按成员别名点名（PinnedMemberId 记录被点名成员）。
	lane, err := GetLaneByName(modelName)
	var pinned *LaneMember
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		lane, pinned, err = GetLaneByAlias(modelName)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	// 没建车道 / 车道被停用 → 不可调用（ADR 0005）。渠道声明不参与直连。
	if lane == nil || !lane.Enabled {
		return route, nil
	}

	route.Source = RouteSourceExplicit
	route.Mode = lane.Mode
	route.RouteKey = lane.Name
	route.Config = ParseLaneRelayConfig(lane.Config)
	route.ActiveMember = lane.ActiveMember
	if pinned != nil {
		route.PinnedMemberId = pinned.Id
	}
	for _, m := range lane.Members {
		var ch *Channel
		name := ""
		if got, err := GetChannelById(m.ChannelId, false); err == nil && got != nil {
			ch = got
			name = got.Name
		}
		route.Members = append(route.Members, RouteMember{
			ChannelId:        m.ChannelId,
			Channel:          name,
			UpstreamModel:    effectiveUpstreamModel(ch, lane.Name, m.UpstreamModel),
			UpstreamOverride: m.UpstreamModel,
			PublicAlias:      m.PublicAlias,
			Priority:         m.Priority,
			Weight:           m.Weight,
			MemberId:         m.Id,
			Overrides:        m.Overrides,
		})
	}
	sort.SliceStable(route.Members, func(i, j int) bool {
		return route.Members[i].Priority > route.Members[j].Priority
	})
	return route, nil
}

// effectiveUpstreamModel 计算成员最终发给上游的模型名（ADR 0005）。
// 优先级：成员级显式改名（非空且 ≠ 路由键）> 渠道 model_mapping > 路由键。
func effectiveUpstreamModel(channel *Channel, routeKey, memberUpstream string) string {
	memberUpstream = strings.TrimSpace(memberUpstream)
	if memberUpstream != "" && memberUpstream != routeKey {
		return memberUpstream
	}
	if channel != nil {
		if mapped := strings.TrimSpace(channel.ModelMappingMap()[routeKey]); mapped != "" {
			return mapped
		}
	}
	if memberUpstream != "" {
		return memberUpstream
	}
	return routeKey
}

// suggestedMembers 返回"渠道声明"的候选成员（按渠道 priority 降序）。
// 仅用于模型管理页展示与 POST /api/lanes/seed 一键固化，不参与运行期路由（ADR 0005）。
func suggestedMembers(modelName string) ([]RouteMember, error) {
	channels, err := listEnabledChannels()
	if err != nil {
		return nil, err
	}
	cands := make([]*Channel, 0, len(channels))
	for _, ch := range channels {
		if channelServesModel(ch, modelName) {
			cands = append(cands, ch)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		pi, pj := cands[i].GetPriority(), cands[j].GetPriority()
		if pi != pj {
			return pi > pj
		}
		return cands[i].Id < cands[j].Id
	})
	members := make([]RouteMember, 0, len(cands))
	for _, c := range cands {
		members = append(members, RouteMember{
			ChannelId:     c.Id,
			Channel:       c.Name,
			UpstreamModel: effectiveUpstreamModel(c, modelName, ""),
			Priority:      int(c.GetPriority()),
			Weight:        c.GetWeight(),
		})
	}
	return members, nil
}

// ResolveRouteForDisplay 供管理面展示：没有车道时返回"建议成员链"（渠道声明，
// routable=false），而不是空链。运行期路由仍走 ResolveRoute（仅车道）。
func ResolveRouteForDisplay(modelName string) (*ResolvedRoute, error) {
	resolved, err := ResolveRoute(modelName)
	if err != nil {
		return nil, err
	}
	if resolved.Source == RouteSourceExplicit {
		return resolved, nil
	}
	members, err := suggestedMembers(resolved.RouteKey)
	if err != nil {
		return nil, err
	}
	resolved.Members = members
	return resolved, nil
}

// SeedLanes 为所有"渠道已声明但无车道"的模型生成 failover 车道（ADR 0005）。
// 幂等：已有同名车道的模型跳过。dryRun 只返回将创建的车道名，不落库。
func SeedLanes(dryRun bool) (created []string, skipped []string, err error) {
	channels, err := listEnabledChannels()
	if err != nil {
		return nil, nil, err
	}
	candidates := map[string]bool{}
	for _, ch := range channels {
		for _, m := range ch.GetModels() {
			if m = strings.TrimSpace(m); m != "" {
				candidates[m] = true
			}
		}
	}
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		existing, getErr := GetLaneByName(name)
		if getErr == nil && existing != nil {
			skipped = append(skipped, name)
			continue
		}
		if getErr != nil && !errors.Is(getErr, gorm.ErrRecordNotFound) {
			return created, skipped, getErr
		}
		members, memberErr := suggestedMembers(name)
		if memberErr != nil {
			return created, skipped, memberErr
		}
		if len(members) == 0 {
			skipped = append(skipped, name)
			continue
		}
		if dryRun {
			created = append(created, name)
			continue
		}
		lane := &Lane{Name: name, Enabled: true, Mode: LaneModeFailover}
		raw, _ := json.Marshal(DefaultLaneRelayConfig())
		lane.Config = string(raw)
		for _, m := range members {
			// 成员 upstream_model 留空 → 运行期用渠道 model_mapping 解析（ADR 0005）。
			lane.Members = append(lane.Members, LaneMember{
				ChannelId: m.ChannelId,
				Priority:  m.Priority,
			})
		}
		if err := UpsertLane(lane); err != nil {
			return created, skipped, err
		}
		created = append(created, name)
	}
	return created, skipped, nil
}

// ListModelSummaries 汇总全部路由键：已配车道的（routable=true）与渠道声明但
// 未配车道的（source=unconfigured，routable=false）。
func ListModelSummaries() ([]ModelSummary, error) {
	seen := map[string]*ModelSummary{}

	lanes, err := ListLanes()
	if err != nil {
		return nil, err
	}
	for _, lane := range lanes {
		if !lane.Enabled {
			continue
		}
		s, ok := seen[lane.Name]
		if !ok {
			s = &ModelSummary{Model: lane.Name, Source: RouteSourceExplicit, Routable: true}
			seen[lane.Name] = s
		}
		s.MemberCount = len(lane.Members)
	}

	channels, err := listEnabledChannels()
	if err != nil {
		return nil, err
	}
	for _, ch := range channels {
		for _, m := range ch.GetModels() {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			if s, ok := seen[m]; ok {
				// 已配车道的模型以车道为准，候选渠道不计入计数
				if s.Source != RouteSourceExplicit {
					s.MemberCount++
				}
				continue
			}
			seen[m] = &ModelSummary{Model: m, Source: RouteSourceUnconfigured, Routable: false, MemberCount: 1}
		}
	}

	out := make([]ModelSummary, 0, len(seen))
	for _, s := range seen {
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out, nil
}

func listEnabledChannels() ([]*Channel, error) {
	var channels []*Channel
	if err := DB.Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, err
	}
	return channels, nil
}
