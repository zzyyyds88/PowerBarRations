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
//  1. 同名显式车道（人工配置的成员链，支持改名/别名/池化）；
//  2. 隐式车道：所有声明提供该模型的渠道，按渠道 priority 降序成链；
//  3. 都没有 → 由调用方返回 503 No available channel for model <X>。
//
// 约定：priority 一律 **数字大者优先**（与上游渠道优先级语义一致）。

const (
	RouteSourceExplicit = "explicit"
	RouteSourceImplicit = "implicit"

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

// DefaultLaneRelayConfig 默认取上游路由层 DefaultGroupRelayConfig，但亲和改为 0
// （2/3/120/30/60/0；design-v1 §7.3 明确不做粘滞，避免掩盖 priority 语义）。
func DefaultLaneRelayConfig() LaneRelayConfig {
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

// Lane 显式车道：可选的覆盖层。
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
	PublicAlias   string `json:"public_alias,omitempty"`
	Priority      int    `json:"priority"`
	Weight        int    `json:"weight"`
	MemberId      int    `json:"member_id,omitempty"`
	Overrides     string `json:"-"`
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
	for i := range fallback.Members {
		if fallback.Source == RouteSourceImplicit {
			fallback.Members[i].UpstreamModel = modelName
		}
	}
	return fallback, nil
}

func resolveExactRoute(modelName string) (*ResolvedRoute, error) {
	route := &ResolvedRoute{
		Model:    modelName,
		RouteKey: modelName,
		Source:   RouteSourceImplicit,
		Mode:     LaneModeFailover,
		Config:   DefaultLaneRelayConfig(),
		Members:  []RouteMember{},
	}
	if strings.TrimSpace(modelName) == "" {
		return route, nil
	}

	// 1) 同名显式车道；否则按成员别名点名（PinnedMemberId 记录被点名成员）
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
	// 显式车道被禁用时回落隐式链：显式层只是覆盖，关掉它就回到"渠道声明即可路由"的默认语义。
	if lane != nil && lane.Enabled {
		route.Source = RouteSourceExplicit
		route.Mode = lane.Mode
		route.RouteKey = lane.Name
		route.Config = ParseLaneRelayConfig(lane.Config)
		route.ActiveMember = lane.ActiveMember
		if pinned != nil {
			route.PinnedMemberId = pinned.Id
		}
		for _, m := range lane.Members {
			name := ""
			if ch, err := GetChannelById(m.ChannelId, false); err == nil && ch != nil {
				name = ch.Name
			}
			route.Members = append(route.Members, RouteMember{
				ChannelId:     m.ChannelId,
				Channel:       name,
				UpstreamModel: m.UpstreamModel,
				PublicAlias:   m.PublicAlias,
				Priority:      m.Priority,
				Weight:        m.Weight,
				MemberId:      m.Id,
				Overrides:     m.Overrides,
			})
		}
		sort.SliceStable(route.Members, func(i, j int) bool {
			return route.Members[i].Priority > route.Members[j].Priority
		})
		return route, nil
	}

	// 2) 隐式车道：所有声明提供该模型的渠道，按 priority 降序（相同则按 id 升序保证确定性）
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
	for _, c := range cands {
		route.Members = append(route.Members, RouteMember{
			ChannelId:     c.Id,
			Channel:       c.Name,
			UpstreamModel: modelName,
			Priority:      int(c.GetPriority()),
			Weight:        c.GetWeight(),
		})
	}
	return route, nil
}

// ListModelSummaries 汇总全部可路由模型（显式车道 + 渠道声明的模型）。
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
			s = &ModelSummary{Model: lane.Name, Source: RouteSourceExplicit}
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
				// 显式车道已覆盖该模型：成员链以显式为准，隐式渠道不计入计数
				if s.Source == RouteSourceImplicit {
					s.MemberCount++
				}
				continue
			}
			seen[m] = &ModelSummary{Model: m, Source: RouteSourceImplicit, MemberCount: 1}
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
