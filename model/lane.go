package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/setting/ratio_setting"

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
	// RouteSourceUnconfigured：渠道声明了该模型但没有对应启用车道 → 不可调用。
	RouteSourceUnconfigured = "unconfigured"

	// 只有两种模式：failover（默认，按成员顺序故障切换）与 manual（只走点名成员）。
	// weighted / round_robin 已删除：单用户自用网关不需要随机/轮询负载均衡，
	// "这次为什么走了另一个上游"不可解释（design-v1 §7.2）。
	LaneModeFailover = "failover"
	LaneModeManual   = "manual"
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

// MaxLaneMemberAttempts 成员单请求尝试次数硬上限。
//
// 429 在 Retry-After 封顶内会原地重试同一成员；若不设上界，一个请求可能
// 被配置放大成数十分钟的串行重试（而请求已经超时）。10 次对单节点网关足够。
const MaxLaneMemberAttempts = 10

// MaxLaneDurationSeconds 六键中时长/间隔的硬上限（7 天）。
// int 秒直接乘 time.Second 在接近上限时会溢出成负 Duration，必须提前拒绝。
const MaxLaneDurationSeconds = 7 * 24 * 3600

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
		// TODO(默认值调整): 生产 lane_defaults 已把流式首事件超时改为 120s
		// （2026-11-27，30s 对推理型上游过紧，实修见 deploy/production.env 备注）。
		// 下次升级把这里的 30 一并改成 120，并同步 routing-spec §85 的默认值表述
		// 与受影响断言；当前编译期兜底先保持 30，避免与已发布文档漂移。
		MemberStreamFirstEventTimeoutSeconds: 30,
		MemberCooldownSeconds:                60,
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

// LaneConfigError 是车道六键或成员覆盖配置的脱敏结构化错误。请求路径必须
// fail-closed，不能静默 Normalize 成默认值后继续转发。
type LaneConfigError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path"`
}

// ParseLaneRelayConfigE 解析车道 Config 字段（空串 → 默认值），并显式返回
// JSON/范围错误。请求选路使用此版本；旧 ParseLaneRelayConfig 仅供展示兼容。
func ParseLaneRelayConfigE(raw string) (LaneRelayConfig, *LaneConfigError) {
	cfg := DefaultLaneRelayConfig()
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	var parsed LaneRelayConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return cfg, &LaneConfigError{Code: "invalid_lane_config", Message: "车道六键配置不是合法 JSON", Path: "config"}
	}
	if err := validateLaneRelayConfigValues(raw); err != nil {
		return cfg, err
	}
	return parsed.Normalize(), nil
}

// ParseLaneRelayOverridesE 解析成员级覆盖并显式返回错误。
func ParseLaneRelayOverridesE(raw string) (LaneRelayOverrides, *LaneConfigError) {
	var o LaneRelayOverrides
	if strings.TrimSpace(raw) == "" {
		return o, nil
	}
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return LaneRelayOverrides{}, &LaneConfigError{Code: "invalid_member_overrides", Message: "成员六键覆盖不是合法 JSON", Path: "overrides"}
	}
	if err := validateLaneRelayConfigValues(raw); err != nil {
		return LaneRelayOverrides{}, err
	}
	return o, nil
}

func validateLaneRelayConfigValues(raw string) *LaneConfigError {
	var values map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return &LaneConfigError{Code: "invalid_lane_config", Message: "车道六键配置不是合法 JSON", Path: "config"}
	}
	for key, value := range values {
		var number int
		if err := json.Unmarshal(value, &number); err != nil {
			continue
		}
		if number < 0 {
			return &LaneConfigError{Code: "invalid_lane_config", Message: "车道六键不能为负数", Path: key}
		}
		if key == "member_max_attempts" && number > MaxLaneMemberAttempts {
			return &LaneConfigError{Code: "invalid_lane_config", Message: "member_max_attempts 不能超过 10", Path: key}
		}
		switch key {
		case "member_retry_interval_seconds", "member_non_stream_response_timeout_seconds",
			"member_stream_first_event_timeout_seconds", "member_cooldown_seconds", "member_affinity_seconds":
			if number > MaxLaneDurationSeconds {
				return &LaneConfigError{Code: "invalid_lane_config", Message: "时长/间隔不能超过 604800 秒", Path: key}
			}
		}
	}
	return nil
}

// ParseLaneRelayConfig 解析车道 Config 字段（空串 → 默认值）。
func ParseLaneRelayConfig(raw string) LaneRelayConfig {
	cfg, _ := ParseLaneRelayConfigE(raw)
	return cfg
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
	// OrphanMemberCount 是成员里渠道已不存在（悬空）的数量；仅列表展示用，不落库。
	OrphanMemberCount int `json:"-" gorm:"-"`
}

// LaneMember 车道成员：渠道 + 上游真名。
type LaneMember struct {
	Id        int `json:"id" gorm:"primaryKey"`
	LaneId    int `json:"lane_id" gorm:"not null;index"`
	ChannelId int `json:"channel_id" gorm:"not null"`
	// GroupId 非 0 = 本行是**成员组引用**（ADR 0009）：ChannelId/Model 为 0/空串，
	// 公共别名/覆盖不适用；解析时展开为所引组的成员区段（expandLaneChain）。
	// 用 0 表示散成员而非可空外键，避免存量 DDL 变更（not null 约束保持不动）。
	GroupId int `json:"group_id" gorm:"index"`
	// Model 是该成员**所选、由该渠道声明的模型名**（列名 `model`）。
	//
	// 成员**不存上游真名**：真名一律由 Model 推导（`Channel.ModelMapping[Model] ?? Model`，
	// ADR 0008）。此前存解析后的真名并允许成员级覆盖，后果是"加入时把渠道映射物化写入"
	// 让后续映射改动失效、且运行期与加入期的查表键不一致（池化车道必错）。
	// 列名由 AutoMigrate 从旧的 upstream_model 迁移（见 migrateLaneMemberModelSemantics）。
	Model       string `json:"model" gorm:"column:model;not null"`
	PublicAlias string `json:"public_alias" gorm:"type:varchar(128);index"`
	Priority    int    `json:"priority"`                   // 车道内顺序：数字大者优先
	Overrides   string `json:"overrides" gorm:"type:text"` // 成员级六键覆盖 JSON
	// Disabled 是该成员被人工停用（可逆）：仍留在成员链里，但不参与选路。
	//
	// 字段刻意取反（不是 Enabled）且**禁止加 gorm default**：GORM 更新时会忽略
	// 零值，加了 default 之后"显式关闭"会被默认值吃掉；而直接用 Enabled bool 会让
	// 存量行迁移后全为 false，等于所有成员被静默停用、全站 503。取反后零值 = 启用，
	// 存量行天然正确，列由 AutoMigrate 自动添加，不需要迁移脚本。
	// 同一条约束也写在 Lane.Enabled 上。口径真源：routing-spec §1.2。
	Disabled bool `json:"disabled"`
}

// RouteMember 一次解析出的成员（对外展示与选路共用）。
// LaneReferencesChangedError 表示写事务内重查发现渠道模型引用在确认之后新增。
// 调用方必须要求用户重新确认，不能把未展示给用户的新引用静默清理。
type LaneReferencesChangedError struct {
	Lanes    []string
	Groups   []string
	NewLanes []string
}

func (e *LaneReferencesChangedError) Error() string {
	return "channel model references changed while saving; reload and confirm the new references"
}

// LaneActiveMemberError 表示 manual 车道的点名成员配置不可保存或已失效。
// Reason: "required"（启用中的 manual 车道没有点名成员）或 "not_found"
// （点名成员已不在成员链里）。
type LaneActiveMemberError struct {
	Lane         string
	ActiveMember string
	Reason       string
}

func (e *LaneActiveMemberError) Error() string {
	if e.Reason == "required" {
		return "manual lane '" + e.Lane + "' requires an active member"
	}
	return "active member '" + e.ActiveMember + "' is not in lane '" + e.Lane + "'"
}

// ActiveMemberIndex 解析 manual 车道的 active_member：先按成员别名，再按
// `channel/model` 标签。运行期选路与管理面校验必须共用这一处，避免两边对
// "点名成员是否存在"得出不同结论。
func ActiveMemberIndex(active string, members []RouteMember) int {
	active = strings.TrimSpace(active)
	if active == "" {
		return -1
	}
	for i := range members {
		if members[i].PublicAlias != "" && members[i].PublicAlias == active {
			return i
		}
	}
	for i := range members {
		label := members[i].Model
		if members[i].Channel != "" {
			label = members[i].Channel + "/" + members[i].Model
		}
		if label == active {
			return i
		}
	}
	return -1
}

type RouteMember struct {
	ChannelId int    `json:"channel_id"`
	Channel   string `json:"channel"`
	// ChannelEnabled 该成员渠道当前是否启用；停用渠道成员在控制台应可见地标灰
	// （否则同一渠道在模型页有停用徽章、路由页却是绿色"可调用"）。
	ChannelEnabled bool `json:"channel_enabled"`
	// Model 是成员**所选**的模型名（成员身份，`active_member` 标签与 attempts 用它）。
	Model string `json:"model"`
	// UpstreamModel 是**派生**的上游真名 = `Channel.ModelMapping[Model] ?? Model`（ADR 0008）。
	// 只读：由解析时算出，供展示、转发与日志，**不接受写回**。
	UpstreamModel string `json:"upstream_model"`
	PublicAlias   string `json:"public_alias,omitempty"`
	Priority      int    `json:"priority"`
	MemberId      int    `json:"member_id,omitempty"`
	// Group 非 empty = 该成员经**成员组引用**展开而来（ADR 0009）：值是组名。
	// 只进快照签名与排障标注，不参与成员身份（身份仍是 (渠道, 所选模型)）。
	Group     string `json:"-"`
	Overrides string `json:"-"`
	// Disabled 该成员被人工停用（配置态），选路时应跳过。
	//
	// 与 LaneMember.Disabled 同样是**反向**命名，理由一致且更硬：RouteMember 由多处
	// 字面量构造（生产 4 处 + 测试夹具），正向 Enabled 的零值是 false，会让任何忘记
	// 置真的成员静默变成"已停用"，表现为该模型必 503。反向命名下零值 = 参与选路，
	// 与新增该字段之前的行为完全一致。
	// 本结构体不被直接 JSON 序列化（handler 全部手工拼 gin.H），所以对外仍是正向
	// `enabled`，取反只做在输出处一次（api-spec §4.2 命名规则、routing-spec §1.2）。
	Disabled bool `json:"-"`

	// 请求级不可变快照字段。它们不进入管理面 JSON，但参与选路、转发与准入。
	ChannelSnapshot *Channel              `json:"-"`
	RateLimit       *dto.ChannelRateLimit `json:"-"`
	ChannelVersion  int64                 `json:"-"`
	// ConfigError 是渠道配置错误的脱敏结构化投影；nil 表示无错误。请求路径 fail-closed。
	ConfigError *ChannelConfigError `json:"-"`
	// OverrideError 是成员六键覆盖解析错误；nil 表示无错误。请求路径 fail-closed。
	OverrideError *LaneConfigError `json:"-"`
}

// RouteSourceDisabled 车道存在但被停用：模型不可调用，但仍应在管理面可见，
// 否则"只有一条停用车道的模型"会从控制台彻底消失（既不在 /models 也不在路由页）。
const RouteSourceDisabled = "disabled"

// ResolvedRoute 一个路由键解析出的完整成员链。
type ResolvedRoute struct {
	Model  string          `json:"model"`
	Source string          `json:"source"`
	Mode   string          `json:"mode"`
	Config LaneRelayConfig `json:"config"`
	// RouteKey 规范化后的路由键：显式车道（含别名点名）为车道名；没有车道可解析
	// （unconfigured）时为请求模型名本身。
	// 令牌的车道权限以此判定（token-spec §3.2）。
	RouteKey string `json:"route_key"`
	// ActiveMember 仅 manual 模式使用：人工指定的成员，取值优先匹配成员的
	// PublicAlias，其次匹配 "channel/upstream_model" 标签；为空即"无可用"。
	ActiveMember string `json:"active_member,omitempty"`
	// PinnedMemberId 非 0 表示请求点名了某个成员别名，选路时应先尝试该成员。
	PinnedMemberId int           `json:"pinned_member_id,omitempty"`
	Members        []RouteMember `json:"members"`
	// LaneVersion 车道配置版本（Lane.UpdatedAt，秒级）。选路运行态（route.PruneStaleState）
	// 用它判断"本次解析是否比上次处理过的更新"，避免旧配置的并发请求用旧成员集合
	// 误删新配置刚写入的成员运行态。0 表示来源无法提供版本（测试夹具/历史数据）。
	LaneVersion int64 `json:"-"`
	// ConfigError 是车道级六键解析错误；请求入口必须 fail-closed。
	ConfigError *LaneConfigError `json:"-"`
}

// EffectiveConfigE 返回某成员生效的六键（车道默认 + 成员级覆盖），并显式
// 返回成员覆盖解析错误。请求 State 使用此版本 fail-closed。
func (r *ResolvedRoute) EffectiveConfigE(member *RouteMember) (LaneRelayConfig, *LaneConfigError) {
	cfg := r.Config.Normalize()
	if member != nil && strings.TrimSpace(member.Overrides) != "" {
		overrides, err := ParseLaneRelayOverridesE(member.Overrides)
		if err != nil {
			return cfg, err
		}
		cfg = cfg.Apply(overrides)
	}
	return cfg, nil
}

// EffectiveConfig 返回某成员生效的六键（车道默认 + 成员级覆盖）。
func (r *ResolvedRoute) EffectiveConfig(member *RouteMember) LaneRelayConfig {
	cfg, _ := r.EffectiveConfigE(member)
	return cfg
}

// ModelSummary 用于 GET /models 的概览。
type ModelSummary struct {
	Model    string `json:"model"`
	Source   string `json:"source"`
	Routable bool   `json:"routable"`
	// MemberCount 是车道成员总数（含渠道已删/停用的悬空成员）；unconfigured 时
	// 表示声明该模型的候选渠道数。
	MemberCount int `json:"member_count"`
	// AvailableMemberCount 是当前真正可路由的成员数（渠道存在且启用，且未被人工停用）；
	// 与 MemberCount 的差值即"不可用成员"，供界面标注"含不可用"（P3-1）。
	AvailableMemberCount int `json:"available_member_count"`
	// DisabledMemberCount 是成员链里被人工停用（LaneMember.Disabled）的数量。
	// 它的唯一用途是把 degraded=true 拆成两种排障结论："成员全被我关了"
	// （== MemberCount）与"上游全不可用"（== 0）。degraded 的定义不变。
	DisabledMemberCount int `json:"disabled_member_count"`
	// ChannelDisabledMemberCount 是"渠道存在但被停用"导致不参与选路的成员数
	// （成员本身未被人工停用）。与 Disabled/Orphan 互斥。
	ChannelDisabledMemberCount int `json:"channel_disabled_member_count"`
	// OrphanMemberCount 是"渠道已不存在"的悬空成员数（成员本身未被人工停用）。
	OrphanMemberCount int `json:"orphan_member_count"`
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
	return getLaneByNameWithDB(DB, name)
}

// GetLaneByNameTx 与 GetLaneByName 相同，但在给定事务内查询。
// SQLite 连接池默认 1，事务内调用全局 DB 会自锁；import 必须用 Tx 版本。
func GetLaneByNameTx(tx *gorm.DB, name string) (*Lane, error) {
	return getLaneByNameWithDB(tx, name)
}

func getLaneByNameWithDB(db *gorm.DB, name string) (*Lane, error) {
	var lane Lane
	if err := db.Where("name = ?", name).First(&lane).Error; err != nil {
		return nil, err
	}
	if err := db.Where("lane_id = ?", lane.Id).Order("priority desc, id asc").Find(&lane.Members).Error; err != nil {
		return nil, err
	}
	return &lane, nil
}

func GetLaneById(id int) (*Lane, error) {
	return GetLaneByIdTx(DB, id)
}

// GetLaneByIdTx 与 GetLaneById 相同，但在给定事务内查询。
func GetLaneByIdTx(tx *gorm.DB, id int) (*Lane, error) {
	var lane Lane
	if err := tx.Where("id = ?", id).First(&lane).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("lane_id = ?", lane.Id).Order("priority desc, id asc").Find(&lane.Members).Error; err != nil {
		return nil, err
	}
	return &lane, nil
}

// AliasAmbiguousError 表示历史数据里有多个成员共用同一 public_alias。
type AliasAmbiguousError struct {
	Alias string
	Count int
}

func (e *AliasAmbiguousError) Error() string {
	return "member alias '" + e.Alias + "' is ambiguous: " + fmt.Sprintf("%d members share it", e.Count)
}

// GetLaneByAlias 按成员 public_alias 找显式车道，并返回被点名的成员。
// 历史重复别名不再静默取第一条，而是返回 409 语义的错误。
func GetLaneByAlias(alias string) (*Lane, *LaneMember, error) {
	return GetLaneByAliasTx(DB, alias)
}

// GetLaneByAliasTx 与 GetLaneByAlias 相同，但在给定事务内查询。
func GetLaneByAliasTx(tx *gorm.DB, alias string) (*Lane, *LaneMember, error) {
	if strings.TrimSpace(alias) == "" {
		return nil, nil, gorm.ErrRecordNotFound
	}
	var members []LaneMember
	if err := tx.Where("public_alias = ?", alias).Order("id").Find(&members).Error; err != nil {
		return nil, nil, err
	}
	if len(members) == 0 {
		return nil, nil, gorm.ErrRecordNotFound
	}
	if len(members) > 1 {
		return nil, nil, &AliasAmbiguousError{Alias: alias, Count: len(members)}
	}
	member := members[0]
	lane, err := GetLaneByIdTx(tx, member.LaneId)
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

// removedModelNames 返回 oldModels 中有、newModels 中没有的模型名（trim、去重、保持顺序）。
func removedModelNames(oldModels, newModels []string) []string {
	newSet := map[string]bool{}
	for _, m := range newModels {
		if m = strings.TrimSpace(m); m != "" {
			newSet[m] = true
		}
	}
	out := make([]string, 0)
	seen := map[string]bool{}
	for _, m := range oldModels {
		m = strings.TrimSpace(m)
		if m == "" || newSet[m] || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// RemovedModelLaneRefs 兼容旧调用：只返回车道名。新代码应使用
// RemovedModelReferences 以同时拿到成员组引用（组可以不被任何车道引用，但仍
// 持有本渠道成员，不能只靠车道名判断）。
func RemovedModelLaneRefs(channelId int, removed []string) ([]string, error) {
	refs, err := RemovedModelReferences(channelId, removed)
	if err != nil || refs == nil {
		return nil, err
	}
	return refs.Lanes, nil
}

// removedModelLaneRefsWith 兼容事务内调用：只返回车道名。新代码应使用
// removedModelReferencesWith。
func removedModelLaneRefsWith(db *gorm.DB, channelId int, removed []string) ([]string, error) {
	refs, err := removedModelReferencesWith(db, channelId, removed)
	if err != nil || refs == nil {
		return nil, err
	}
	return refs.Lanes, nil
}

// LanesReferencingChannel 兼容旧调用：只返回车道名（含通过成员组间接引用的
// 车道）。新代码应使用 ChannelReferences 以同时检查成员组。
func LanesReferencingChannel(channelId int) ([]string, error) {
	refs, err := ChannelReferences(channelId)
	if err != nil || refs == nil {
		return nil, err
	}
	return refs.Lanes, nil
}

// CleanupLanesForRemovedModels 处理"渠道不再声明某些模型"后的车道一致性：只移除
// **所选模型确实被移除**的本渠道散成员；成员被清空的车道整条删除。组内成员不在这里
// 自动清理——组是跨车道复用单元，必须由用户显式编辑，避免渠道写操作静默改组。
// 返回（被清理但保留的车道, 被删除的车道, error）。被删除车道的运行态由调用方通过
// route.Default.Remove 清理（model 层不能反向 import internal/route）。
func CleanupLanesForRemovedModels(channelId int, removed []string) (cleaned, deleted []string, err error) {
	return CleanupLanesForRemovedModelsTx(DB, channelId, removed)
}

// CleanupLanesForRemovedModelsTx 是 CleanupLanesForRemovedModels 的事务内版本：
// 渠道写、成员删除与空车道删除可以放进同一个事务，避免“渠道已不声明模型、
// 车道还留着悬空成员/空壳”的半清理状态。调用方负责在提交后清理 route 运行态。
func CleanupLanesForRemovedModelsTx(db *gorm.DB, channelId int, removed []string) (cleaned, deleted []string, err error) {
	if db == nil {
		db = DB
	}
	members, err := removedDirectLaneMembersWith(db, channelId, removed)
	if err != nil || len(members) == 0 {
		return nil, nil, err
	}
	memberIDs := make([]int, 0, len(members))
	laneIDs := make([]int, 0, len(members))
	seenLane := map[int]bool{}
	for i := range members {
		memberIDs = append(memberIDs, members[i].Id)
		if !seenLane[members[i].LaneId] {
			seenLane[members[i].LaneId] = true
			laneIDs = append(laneIDs, members[i].LaneId)
		}
	}
	if err := db.Where("id IN ?", memberIDs).Delete(&LaneMember{}).Error; err != nil {
		return nil, nil, err
	}
	var lanes []Lane
	if err := db.Where("id IN ?", laneIDs).Find(&lanes).Error; err != nil {
		return nil, nil, err
	}
	for i := range lanes {
		var remaining int64
		if err := db.Model(&LaneMember{}).Where("lane_id = ?", lanes[i].Id).Count(&remaining).Error; err != nil {
			return cleaned, deleted, err
		}
		if remaining == 0 {
			// 事务内直接删成员与车道，不能调用 DeleteLaneByName（它自己开事务）。
			if err := db.Where("lane_id = ?", lanes[i].Id).Delete(&LaneMember{}).Error; err != nil {
				return cleaned, deleted, err
			}
			if err := db.Delete(&Lane{}, lanes[i].Id).Error; err != nil {
				return cleaned, deleted, err
			}
			deleted = append(deleted, lanes[i].Name)
		} else {
			cleaned = append(cleaned, lanes[i].Name)
		}
	}
	sort.Strings(cleaned)
	sort.Strings(deleted)
	return cleaned, deleted, nil
}

// CleanupOrphanLaneMembers 清理"渠道已不存在"的悬空散成员（历史数据修复入口）。
// 悬空成员指向已删除的渠道，请求打到它只会失败；成员清空的车道整条删除。
// 必须排除 channel_id=0 的组引用行：它们是 ADR 0009 的引用外壳，不是渠道成员；
// 把 0 当悬空渠道会误删整条组引用链。
// 返回被清理成员所在的车道名与被删除的车道名（均去重、升序）。
func CleanupOrphanLaneMembers() (cleaned []string, deleted []string, err error) {
	return cleanupOrphanLaneMembersTx(DB)
}

func cleanupOrphanLaneMembersTx(tx *gorm.DB) (cleaned []string, deleted []string, err error) {
	if tx == nil {
		tx = DB
	}
	var memberChannelIDs []int
	if err := tx.Model(&LaneMember{}).Where("channel_id > 0").Distinct().Pluck("channel_id", &memberChannelIDs).Error; err != nil {
		return nil, nil, err
	}
	var existingIDs []int
	if len(memberChannelIDs) > 0 {
		if err := tx.Model(&Channel{}).Where("id IN ?", memberChannelIDs).Pluck("id", &existingIDs).Error; err != nil {
			return nil, nil, err
		}
	}
	existing := map[int]bool{}
	for _, id := range existingIDs {
		existing[id] = true
	}
	orphanIDs := make([]int, 0)
	for _, id := range memberChannelIDs {
		if !existing[id] {
			orphanIDs = append(orphanIDs, id)
		}
	}
	if len(orphanIDs) == 0 {
		return nil, nil, nil
	}
	var members []LaneMember
	if err := tx.Where("channel_id IN ? AND channel_id > 0", orphanIDs).Find(&members).Error; err != nil {
		return nil, nil, err
	}
	laneIDs := make([]int, 0, len(members))
	for _, m := range members {
		laneIDs = append(laneIDs, m.LaneId)
	}
	if err := tx.Where("channel_id IN ? AND channel_id > 0", orphanIDs).Delete(&LaneMember{}).Error; err != nil {
		return nil, nil, err
	}
	seenLane := map[int]bool{}
	for _, id := range laneIDs {
		if seenLane[id] {
			continue
		}
		seenLane[id] = true
		var lane Lane
		if getErr := tx.First(&lane, id).Error; getErr != nil {
			continue
		}
		var remaining int64
		if countErr := tx.Model(&LaneMember{}).Where("lane_id = ?", id).Count(&remaining).Error; countErr != nil {
			return cleaned, deleted, countErr
		}
		if remaining == 0 {
			if delErr := tx.Where("lane_id = ?", id).Delete(&LaneMember{}).Error; delErr != nil {
				return cleaned, deleted, delErr
			}
			if delErr := tx.Delete(&Lane{}, id).Error; delErr != nil {
				return cleaned, deleted, delErr
			}
			deleted = append(deleted, lane.Name)
		} else {
			cleaned = append(cleaned, lane.Name)
		}
	}
	sort.Strings(cleaned)
	sort.Strings(deleted)
	return cleaned, deleted, nil
}

// UpsertLane 全量写：先写车道与成员，再由调用方重新读取作为回读。
func UpsertLane(lane *Lane) error {
	return UpsertLaneTx(DB, lane)
}

// UpsertLaneTx 在调用方事务内写车道；tx 为 nil 时回落到全局 DB。
func UpsertLaneTx(tx *gorm.DB, lane *Lane) error {
	if tx == nil {
		tx = DB
	}
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
	{
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
	}
}

// ValidLaneMode 校验模式取值。
func ValidLaneMode(mode string) bool {
	switch mode {
	case LaneModeFailover, LaneModeManual:
		return true
	}
	return false
}

// LaneNames 返回全部车道名，供 API 校验"车道名与成员别名冲突"。
func LaneNames() ([]string, error) {
	return laneNamesWithDB(DB)
}

// LaneNamesTx 与 LaneNames 相同，但在给定事务内查询。
func LaneNamesTx(tx *gorm.DB) ([]string, error) {
	return laneNamesWithDB(tx)
}

func laneNamesWithDB(db *gorm.DB) ([]string, error) {
	var names []string
	if err := db.Model(&Lane{}).Pluck("name", &names).Error; err != nil {
		return nil, err
	}
	return names, nil
}

// ---------- 解析 ----------

// channelServesModel 判断渠道是否"提供"某路由键：声明在 models 清单里，**或**在
// model_mapping 里把它作为源键映射到上游真名。
//
// 只看 models 会让"只配了 model_mapping 的渠道"在模型页（含映射键）出现、却在路由页
// 候选里消失，两处口径不一致（ui-spec §6.3 要求统一）。
func channelServesModel(channel *Channel, modelName string) bool {
	for _, m := range channel.GetModels() {
		if strings.TrimSpace(m) == modelName {
			return true
		}
	}
	if mapped := strings.TrimSpace(channel.ModelMappingMap()[modelName]); mapped != "" {
		return true
	}
	return false
}

// ResolveRoute 按路由键解析成员链。返回的 Members 可能为空（表示没有任何渠道声明该模型）。
//
// 先按请求原文精确匹配；未命中时再按 new-api 既有的思考后缀归一化规则匹配一次
// （design-v1 §4.1「保留思考后缀归一化行为」），保证存量下游带后缀的请求不断流。
func ResolveRoute(modelName string) (*ResolvedRoute, error) {
	var route *ResolvedRoute
	err := DB.Transaction(func(tx *gorm.DB) error {
		resolved, err := resolveExactRouteWithDB(tx, modelName)
		if err != nil {
			return err
		}
		route = resolved
		if len(route.Members) > 0 || route.Source == RouteSourceExplicit {
			return nil
		}
		normalized := ratio_setting.RoutingMatchModelName(modelName)
		if normalized == "" || normalized == modelName {
			return nil
		}
		fallback, err := resolveExactRouteWithDB(tx, normalized)
		if err != nil {
			return err
		}
		if len(fallback.Members) == 0 && fallback.Source != RouteSourceExplicit {
			return nil
		}
		// 命中归一化名后，路由键仍是请求原文（响应回填、日志、令牌判定都用原文）。
		fallback.Model = modelName
		route = fallback
		return nil
	})
	return route, err
}

// expandLaneChain 把车道链（含组引用行，ADR 0009）展开为连续成员序列。
//
// 输入 lane.Members 必须已按 priority 降序排列（GetLaneByName / ListLanes 保证）。
// 组引用行展开为组内成员区段：区段内按组内 priority 降序；成员的 Priority 统一取
// 组引用行的车道位置值——区段成员共享同一 priority + 稳定排序，使调用方尾部的
// 全局排序不会把区段打散（routing-spec §1.4「组引用在链上占一个位置」）。
//
// MemberId：散成员 = lane_members 主键；组展开成员 = -(member_group_items 主键)。
// 负数空间与 lane_members 主键互不冲突，运行态键 laneID:memberID 无需改动。
//
// 组展开成员的 Disabled = 组引用行.Disabled（车道级引用开关）|| 组.Disabled（组
// 全局开关）|| 组内成员.Disabled——三处闭合后按既有「人工停用」口径跳过。
// 悬空组引用（组已不存在；正常路径不会出现——删组会级联清理引用）跳过该行。
func expandLaneChain(lane *Lane) ([]RouteMember, error) {
	return expandLaneChainWithDB(DB, lane)
}

// expandLaneChainWithDB 是 expandLaneChain 的 db 参数版本；事务内必须传 tx。
func expandLaneChainWithDB(db *gorm.DB, lane *Lane) ([]RouteMember, error) {
	members, _, err := expandLaneChainWithVersion(db, lane)
	return members, err
}

// expandLaneChainWithVersion 与 expandLaneChainWithDB 相同，额外返回所引成员组的
// 最大 UpdatedAt（无组引用时为 0）。ResolveRoute 用它把组编辑纳入 LaneVersion，
// 使 PruneStaleState 的"过期配置不剪枝"守卫对组成员变更同样生效（routing-spec §1.3）。
func expandLaneChainWithVersion(db *gorm.DB, lane *Lane) ([]RouteMember, int64, error) {
	if db == nil {
		db = DB
	}
	groupIds := make([]int, 0, 4)
	for _, m := range lane.Members {
		if m.GroupId != 0 {
			groupIds = append(groupIds, m.GroupId)
		}
	}
	groups := map[int]*MemberGroup{}
	var groupVersion int64
	if len(groupIds) > 0 {
		unique := make([]int, 0, len(groupIds))
		seen := map[int]bool{}
		for _, id := range groupIds {
			if id != 0 && !seen[id] {
				seen[id] = true
				unique = append(unique, id)
			}
		}
		var loaded []MemberGroup
		if err := db.Where("id IN ?", unique).Find(&loaded).Error; err != nil {
			return nil, 0, err
		}
		var items []MemberGroupItem
		if err := db.Where("group_id IN ?", unique).Order("priority desc, id asc").Find(&items).Error; err != nil {
			return nil, 0, err
		}
		itemsByGroup := make(map[int][]MemberGroupItem, len(loaded))
		for i := range items {
			itemsByGroup[items[i].GroupId] = append(itemsByGroup[items[i].GroupId], items[i])
		}
		for i := range loaded {
			g := &loaded[i]
			g.Members = itemsByGroup[g.Id]
			groups[g.Id] = g
			if g.UpdatedAt > groupVersion {
				groupVersion = g.UpdatedAt
			}
		}
	}

	// 所有直接成员与组成员引用的渠道一次 IN 查询加载，消除逐成员 N+1。
	channelIDs := make([]int, 0, len(lane.Members))
	seenChannel := map[int]bool{}
	for i := range lane.Members {
		m := &lane.Members[i]
		if m.GroupId == 0 && m.ChannelId != 0 && !seenChannel[m.ChannelId] {
			seenChannel[m.ChannelId] = true
			channelIDs = append(channelIDs, m.ChannelId)
		}
	}
	for _, g := range groups {
		for i := range g.Members {
			id := g.Members[i].ChannelId
			if id != 0 && !seenChannel[id] {
				seenChannel[id] = true
				channelIDs = append(channelIDs, id)
			}
		}
	}
	channels := map[int]*Channel{}
	if len(channelIDs) > 0 {
		var loaded []Channel
		if err := db.Where("id IN ?", channelIDs).Find(&loaded).Error; err != nil {
			return nil, 0, err
		}
		for i := range loaded {
			ch := &loaded[i]
			ch.SnapshotChannelSettings()
			channels[ch.Id] = ch
		}
	}

	members := make([]RouteMember, 0, len(lane.Members))
	for i := range lane.Members {
		m := &lane.Members[i]
		if m.GroupId == 0 {
			members = append(members, routeMemberFromChannelSnapshot(channels[m.ChannelId], m.ChannelId, m.Model,
				m.PublicAlias, m.Priority, m.Id, "", m.Overrides, m.Disabled))
			continue
		}
		group, ok := groups[m.GroupId]
		if !ok {
			continue
		}
		for j := range group.Members {
			item := &group.Members[j]
			members = append(members, routeMemberFromChannelSnapshot(channels[item.ChannelId], item.ChannelId, item.Model,
				item.PublicAlias, m.Priority, -item.Id, group.Name, item.Overrides,
				m.Disabled || group.Disabled || item.Disabled))
		}
	}
	return members, groupVersion, nil
}

// routeMemberFromChannelSnapshot 从请求级渠道快照构造成员，确保映射、启用态、准入
// 配置与配置错误全部来自同一版本。
func routeMemberFromChannelSnapshot(ch *Channel, channelID int, modelName, publicAlias string,
	priority, memberID int, group, overrides string, disabled bool) RouteMember {
	member := RouteMember{
		ChannelId:       channelID,
		ChannelEnabled:  ch != nil && ch.Status == common.ChannelStatusEnabled,
		Model:           modelName,
		UpstreamModel:   effectiveUpstreamModel(ch, modelName),
		PublicAlias:     publicAlias,
		Priority:        priority,
		MemberId:        memberID,
		Group:           group,
		Overrides:       overrides,
		Disabled:        disabled,
		ChannelSnapshot: ch,
	}
	if _, configErr := ParseLaneRelayOverridesE(overrides); configErr != nil {
		member.OverrideError = configErr
	}
	if ch == nil {
		return member
	}
	member.Channel = ch.Name
	member.ChannelVersion = ch.UpdatedAt
	if configErr := ch.ConfigError(); configErr != nil {
		member.ConfigError = configErr
	}
	settings, _, _ := ch.SettingsSnapshot()
	for i := range settings.PBRRateLimits {
		item := settings.PBRRateLimits[i]
		if strings.TrimSpace(item.Model) == strings.TrimSpace(modelName) {
			item.Normalize()
			member.RateLimit = &item
			break
		}
	}
	return member
}

// ValidateLaneActiveMember 校验 manual 车道的点名成员：非空 active_member
// 必须能解析到当前展开后的成员链（别名或 channel/model）。空 pin 是合法的
// “待点名/不可用”状态（清理动作会清空失效 pin），由界面显著提示。
func ValidateLaneActiveMember(lane *Lane) error {
	return validateLaneActiveMemberWithDB(DB, lane)
}

// ValidateLaneActiveMemberTx 与 ValidateLaneActiveMember 相同，但在给定事务内查询。
func ValidateLaneActiveMemberTx(tx *gorm.DB, lane *Lane) error {
	return validateLaneActiveMemberWithDB(tx, lane)
}

func validateLaneActiveMemberWithDB(db *gorm.DB, lane *Lane) error {
	if lane == nil || lane.Mode != LaneModeManual || !lane.Enabled {
		return nil
	}
	active := strings.TrimSpace(lane.ActiveMember)
	if active == "" {
		// 空 pin 是合法的“待点名”状态（渠道/组成员被清理后会自动清空）。
		// 它会让 manual 车道不可用，但不会悄悄指向错误成员；界面会显著提示。
		return nil
	}
	members, err := expandLaneChainWithDB(db, lane)
	if err != nil {
		return err
	}
	if ActiveMemberIndex(active, members) < 0 {
		return &LaneActiveMemberError{
			Lane:         lane.Name,
			ActiveMember: active,
			Reason:       "not_found",
		}
	}
	return nil
}

// ReconcileManualActiveMembersByNames 清理点名成员已不存在的 manual 车道：
// 把 active_member 置空（不自动改点其他人，保持 manual "不静默换人"的语义），
// 返回实际被清理的车道名。调用方应在车道/组成员变更提交后调用，并把结果回给
// 前端，让用户知道需要重新点名。
func ReconcileManualActiveMembersByNames(names []string) ([]string, error) {
	seen := map[string]bool{}
	cleared := make([]string, 0)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		lane, err := GetLaneByName(name)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return cleared, err
		}
		if lane.Mode != LaneModeManual || strings.TrimSpace(lane.ActiveMember) == "" {
			continue
		}
		members, err := expandLaneChain(lane)
		if err != nil {
			return cleared, err
		}
		if ActiveMemberIndex(lane.ActiveMember, members) >= 0 {
			continue
		}
		if err := DB.Model(&Lane{}).Where("id = ?", lane.Id).Updates(map[string]any{
			"active_member": "",
			"updated_at":    common.GetTimestamp(),
		}).Error; err != nil {
			return cleared, err
		}
		cleared = append(cleared, lane.Name)
	}
	sort.Strings(cleared)
	return cleared, nil
}

// ReconcileAllManualActiveMembers 修复历史遗留的悬空点名成员；启动时与清理
// 动作后都可安全重复调用。只清空悬空值，不重新分配成员。
func ReconcileAllManualActiveMembers() ([]string, error) {
	var names []string
	if err := DB.Model(&Lane{}).
		Where("mode = ? AND active_member <> ''", LaneModeManual).
		Pluck("name", &names).Error; err != nil {
		return nil, err
	}
	return ReconcileManualActiveMembersByNames(names)
}

// RewriteManualActiveMemberChannelName 渠道改名时重写 manual 点名的 channel/model
// 前缀；点名按成员身份（所选模型）而不是上游真名，所以只需替换渠道名。
// 同一事务内调用，返回被改写的车道名。
func RewriteManualActiveMemberChannelName(tx *gorm.DB, oldName, newName string) ([]string, error) {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if tx == nil {
		tx = DB
	}
	if oldName == "" || newName == "" || oldName == newName {
		return nil, nil
	}
	var lanes []Lane
	if err := tx.Where("mode = ? AND active_member <> ''", LaneModeManual).Find(&lanes).Error; err != nil {
		return nil, err
	}
	changed := make([]string, 0)
	prefix := oldName + "/"
	for i := range lanes {
		active := strings.TrimSpace(lanes[i].ActiveMember)
		if !strings.HasPrefix(active, prefix) {
			continue
		}
		next := newName + "/" + strings.TrimPrefix(active, prefix)
		if err := tx.Model(&Lane{}).Where("id = ?", lanes[i].Id).Updates(map[string]any{
			"active_member": next,
			"updated_at":    common.GetTimestamp(),
		}).Error; err != nil {
			return changed, err
		}
		changed = append(changed, lanes[i].Name)
	}
	sort.Strings(changed)
	return changed, nil
}

// routeMemberFromLaneMember 散成员行 → RouteMember（渠道查询错误必须上抛：
// 把数据库故障静默降级成"渠道不存在"的空名成员，会让路由在故障期间看似可用、
// 实际无从排障）。
func routeMemberFromLaneMember(m *LaneMember) (RouteMember, error) {
	return routeMemberFromLaneMemberWithDB(DB, m)
}

// routeMemberFromLaneMemberWithDB 是 routeMemberFromLaneMember 的 db 参数版本。
func routeMemberFromLaneMemberWithDB(db *gorm.DB, m *LaneMember) (RouteMember, error) {
	ch, chErr := channelOrNilWithDB(db, m.ChannelId)
	if chErr != nil {
		return RouteMember{}, chErr
	}
	name := ""
	if ch != nil {
		name = ch.Name
	}
	return RouteMember{
		ChannelId:      m.ChannelId,
		Channel:        name,
		ChannelEnabled: ch != nil && ch.Status == common.ChannelStatusEnabled,
		Model:          m.Model,
		UpstreamModel:  effectiveUpstreamModel(ch, m.Model),
		PublicAlias:    m.PublicAlias,
		Priority:       m.Priority,
		MemberId:       m.Id,
		Overrides:      m.Overrides,
		Disabled:       m.Disabled,
	}, nil
}

func resolveExactRoute(modelName string) (*ResolvedRoute, error) {
	return resolveExactRouteWithDB(DB, modelName)
}

func resolveExactRouteWithDB(db *gorm.DB, modelName string) (*ResolvedRoute, error) {
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
	// 别名点名只覆盖散成员（lane_members.public_alias）：组内成员的别名不参与跨车道
	// 点名——一个组可被多条车道引用，别名点名会有车道歧义（ADR 0009）。
	lane, err := GetLaneByNameTx(db, modelName)
	var pinned *LaneMember
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		lane, pinned, err = GetLaneByAliasTx(db, modelName)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	// 没建车道 → 不可调用（ADR 0005）。渠道声明不参与直连。
	if lane == nil {
		return route, nil
	}
	if !lane.Enabled {
		return route, nil
	}

	route.Source = RouteSourceExplicit
	route.Mode = lane.Mode
	route.RouteKey = lane.Name
	route.Config, route.ConfigError = ParseLaneRelayConfigE(lane.Config)
	route.ActiveMember = lane.ActiveMember
	route.LaneVersion = lane.UpdatedAt
	if pinned != nil {
		route.PinnedMemberId = pinned.Id
	}
	members, groupVersion, err := expandLaneChainWithVersion(db, lane)
	if err != nil {
		return nil, err
	}
	if groupVersion > route.LaneVersion {
		// 只改成员组（不改车道）时也让 LaneVersion 前进，PruneStaleState 的
		// "过期配置不剪枝"守卫才能拒绝旧快照（routing-spec §1.3 / ADR 0013）。
		route.LaneVersion = groupVersion
	}
	route.Members = members
	sort.SliceStable(route.Members, func(i, j int) bool {
		return route.Members[i].Priority > route.Members[j].Priority
	})
	return route, nil
}

// effectiveUpstreamModel 计算成员最终发给上游的模型名（ADR 0008）。
//
// 规则只有一条：`Channel.ModelMapping[model] ?? model`。**查表键恒为成员所选模型**，
// 与路由键无关——此前"加入时以成员模型为键、运行期以路由键为键"的两套口径已废除
// （池化车道下必然查错）。成员级覆盖已整体移除，所以这里不再有第一层分支。
func effectiveUpstreamModel(channel *Channel, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if channel != nil {
		if mapped := strings.TrimSpace(channel.ModelMappingMap()[model]); mapped != "" {
			return mapped
		}
	}
	return model
}

// EffectiveUpstreamForMember 供管理面按 (渠道 id, 所选模型) 求派生上游真名（ADR 0008）。
//
// 成员表不存真名，读端点要给出 `upstream_model` 就得现算；渠道不存在时按"无映射"处理
// （回落模型名本身），与运行期一致——渠道被删的悬空成员仍能显示出它原本要打的真名。
func EffectiveUpstreamForMember(channelID int, modelName string) string {
	channel, err := ChannelOrNil(channelID)
	if err != nil {
		channel = nil
	}
	return effectiveUpstreamModel(channel, modelName)
}

// suggestedMembers 返回"渠道声明"的候选成员（按渠道 id 升序；渠道 priority 已删除）。
// 仅用于管理面展示与「可添加成员」候选，不参与运行期路由（ADR 0005）。
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
	// 渠道 priority 已删除：候选链按渠道 id 升序（仅用于 seed 与"可添加成员"展示，
	// 运行期路由只认车道成员顺序）。
	sort.SliceStable(cands, func(i, j int) bool {
		return cands[i].Id < cands[j].Id
	})
	members := make([]RouteMember, 0, len(cands))
	total := len(cands)
	for i, c := range cands {
		members = append(members, RouteMember{
			ChannelId:      c.Id,
			Channel:        c.Name,
			ChannelEnabled: true, // listEnabledChannels 只含启用渠道
			// 候选/推荐成员没有落库成员身份，恒为参与选路（api-spec §5.7：unconfigured
			// 推荐项 enabled 恒 true、member_id 为 0）。Disabled 是反向字段，零值即真，
			// 故这里不需要显式赋值；反而若改成正向字段必须逐处置真。
			// 推荐项的"所选模型"就是它服务的路由键本身。
			Model:         modelName,
			UpstreamModel: effectiveUpstreamModel(c, modelName),
			// priority 数字大者优先：渠道 id 升序 → priority 递减，
			// 使落库后 resolveExactRoute（按 priority 降序）得到的实际顺序
			// 仍是渠道 id 升序（routing-spec §1.1 / design-v1 §7.7 的 seed 语义）。
			Priority: total - i,
		})
	}
	return members, nil
}

// SuggestedMembers 导出 suggestedMembers：供管理面生成"可添加的候选渠道"
// （已配车道的模型也能看到新声明该模型的渠道，避免只能删车道重建）。
func SuggestedMembers(modelName string) ([]RouteMember, error) {
	return suggestedMembers(modelName)
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
	// 停用车道：运行期不可调用（ResolveRoute 返回空链/unconfigured），但管理面要能
	// 看到并编辑它的真实成员链，否则"只有一条停用车道的模型"会从控制台消失（P2-8）。
	if lane, laneErr := GetLaneByName(resolved.RouteKey); laneErr == nil && lane != nil && !lane.Enabled {
		resolved.Source = RouteSourceDisabled
		resolved.Mode = lane.Mode
		resolved.Config = ParseLaneRelayConfig(lane.Config)
		resolved.ActiveMember = lane.ActiveMember
		resolved.LaneVersion = lane.UpdatedAt
		members, memberErr := displayMembersForLane(lane)
		if memberErr != nil {
			return nil, memberErr
		}
		resolved.Members = members
		return resolved, nil
	}
	members, err := suggestedMembers(resolved.RouteKey)
	if err != nil {
		return nil, err
	}
	resolved.Members = members
	return resolved, nil
}

// displayMembersForLane 把车道成员解析成展示用成员链（含渠道是否启用；组引用
// 行展开为成员区段，与运行期同一条展开路径）。仅供管理面 ResolveRouteForDisplay
// 使用，不参与运行期选路。
func displayMembersForLane(lane *Lane) ([]RouteMember, error) {
	members, err := expandLaneChain(lane)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(members, func(i, j int) bool {
		return members[i].Priority > members[j].Priority
	})
	return members, nil
}

// ListModelSummaries 汇总全部路由键：已配车道的（routable=true）与渠道声明但
// 未配车道的（source=unconfigured，routable=false）。
func ListModelSummaries() ([]ModelSummary, error) {
	seen := map[string]*ModelSummary{}

	// 只有"渠道存在且启用"的成员才真正可路由：用它算 available_member_count。
	enabledChannelIDs := map[int]bool{}
	allChannelIDs := map[int]bool{}
	var allChannels []*Channel
	if err := DB.Find(&allChannels).Error; err != nil {
		return nil, err
	}
	enabledChannels := make([]*Channel, 0, len(allChannels))
	for _, ch := range allChannels {
		allChannelIDs[ch.Id] = true
		if ch.Status == common.ChannelStatusEnabled {
			enabledChannelIDs[ch.Id] = true
			enabledChannels = append(enabledChannels, ch)
		}
	}

	lanes, err := ListLanes()
	if err != nil {
		return nil, err
	}
	for i := range lanes {
		lane := &lanes[i]
		// 成员数按**展开后**口径（组引用展开为成员区段，ADR 0009）：member_count
		// 反映真实参与选路的成员数，而不是"链上元素数"。
		expanded, expandErr := expandLaneChain(lane)
		if expandErr != nil {
			return nil, expandErr
		}
		available := countLaneAvailableMembers(lane, expanded, enabledChannelIDs)
		disabledCount := countDisabledMembers(expanded)
		channelDisabledCount, orphanCount := countUnselectableByChannel(expanded, allChannelIDs, enabledChannelIDs)
		if !lane.Enabled {
			// 停用车道不可调用，但仍要在管理面可见（source=disabled）。
			if _, ok := seen[lane.Name]; !ok {
				seen[lane.Name] = &ModelSummary{
					Model:                      lane.Name,
					Source:                     RouteSourceDisabled,
					Routable:                   false,
					MemberCount:                len(expanded),
					AvailableMemberCount:       available,
					DisabledMemberCount:        disabledCount,
					ChannelDisabledMemberCount: channelDisabledCount,
					OrphanMemberCount:          orphanCount,
				}
			}
			continue
		}
		s, ok := seen[lane.Name]
		if !ok {
			s = &ModelSummary{Model: lane.Name, Source: RouteSourceExplicit, Routable: true}
			seen[lane.Name] = s
		}
		s.MemberCount = len(expanded)
		s.AvailableMemberCount = available
		s.DisabledMemberCount = disabledCount
		s.ChannelDisabledMemberCount = channelDisabledCount
		s.OrphanMemberCount = orphanCount
	}

	for _, ch := range enabledChannels {
		for _, m := range ch.GetModels() {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			if s, ok := seen[m]; ok {
				// 已配车道（含停用）的模型以车道为准，候选渠道不计入计数。
				if s.Source == RouteSourceUnconfigured {
					s.MemberCount++
					s.AvailableMemberCount++
				}
				continue
			}
			seen[m] = &ModelSummary{Model: m, Source: RouteSourceUnconfigured, Routable: false, MemberCount: 1, AvailableMemberCount: 1}
		}
	}

	out := make([]ModelSummary, 0, len(seen))
	for _, s := range seen {
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out, nil
}

// countLaneAvailableMembers 按车道模式统计配置态可用成员：failover 是全部满足
// "渠道启用且成员未停用"的成员；manual 只认 active_member 指向的那一个成员——
// 点名缺失/悬空时即使链上还有其他成员，也不能报告 available>0（否则管理面
// 会显示可调用，实际请求必然 503）。
func countLaneAvailableMembers(lane *Lane, members []RouteMember, enabledChannelIDs map[int]bool) int {
	if lane.Mode != LaneModeManual {
		return countAvailableMembers(members, enabledChannelIDs)
	}
	idx := ActiveMemberIndex(lane.ActiveMember, members)
	if idx < 0 {
		return 0
	}
	member := members[idx]
	if member.Disabled || !enabledChannelIDs[member.ChannelId] {
		return 0
	}
	return 1
}

// countAvailableMembers 统计展开后成员里"渠道存在且启用、且未被人工停用"的数量
// （P3-1）。人工停用必须一起排除，否则"全部成员被我关掉"的车道仍会报
// available==全量，界面上的"可调用"就成了假健康（routing-spec §7）。
// 组展开成员的 Disabled 已含组全局开关与车道引用开关（expandLaneChain 三处闭合）。
func countAvailableMembers(members []RouteMember, enabledChannelIDs map[int]bool) int {
	n := 0
	for _, m := range members {
		if !m.Disabled && enabledChannelIDs[m.ChannelId] {
			n++
		}
	}
	return n
}

// countDisabledMembers 统计展开后成员里被人工停用的数量（含"所在组被关"的成员，
// ADR 0009：组关的与成员关的同为 ModelSummary.DisabledMemberCount——它回答
// "多少成员不参与选路是人工配置导致"）。
func countDisabledMembers(members []RouteMember) int {
	n := 0
	for _, m := range members {
		if m.Disabled {
			n++
		}
	}
	return n
}

// countUnselectableByChannel 把"成员未人工停用、但渠道侧不可用"的成员分成两类：
// channel_disabled（渠道存在但被停用）与 orphan（渠道不存在）。两类与
// available/disabled 合起来是 member_count 的互斥分解。
func countUnselectableByChannel(members []RouteMember, allChannelIDs, enabledChannelIDs map[int]bool) (channelDisabled int, orphan int) {
	for _, m := range members {
		if m.Disabled {
			continue
		}
		if !allChannelIDs[m.ChannelId] {
			orphan++
			continue
		}
		if !enabledChannelIDs[m.ChannelId] {
			channelDisabled++
		}
	}
	return channelDisabled, orphan
}

func listEnabledChannels() ([]*Channel, error) {
	var channels []*Channel
	if err := DB.Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, err
	}
	return channels, nil
}
