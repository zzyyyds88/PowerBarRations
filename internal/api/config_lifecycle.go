package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 配置生命周期与自描述（api-spec §5.1 / §5.6）：
// /capabilities、/openapi.json、/export、/import?dry_run=。
//
// 导出**不含密钥明文与哈希**：渠道只有 key_set/key_prefix，客户端密钥只有 key_prefix。
// 因此 re-import 到同一实例是幂等的（diff 为空）；把配置搬到另一台机器后需要重新注入密钥，
// 这属于"私有台账"范畴，不进仓库。

// ChannelConfig 是渠道的可导出形态（无密钥）。
//
// 字段必须覆盖"路由与成本折算所依赖的全部配置"：漏掉 model_mapping 会让恢复后的
// 实例把路由键直发上游（通常 404），漏掉 prices 会让成本折算归零。新增渠道级字段时
// 必须同时更新 BuildConfigBundle 与 channelDigestOfChannel，否则导出会静默丢字段、
// dry-run diff 也会假装"无变更"。
type ChannelConfig struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// 指针三态：导出恒给出；导入文件省略 = 保持原值，显式空串/false = 覆盖。
	BaseURL       *string  `json:"base_url,omitempty"`
	Models        []string `json:"models"`
	ParamOverride any      `json:"param_override,omitempty"`
	Enabled       *bool    `json:"enabled,omitempty"`
	Proxy         *string  `json:"proxy,omitempty"`
	// Prices 渠道级上游单价（成本折算）；空表导出为 []，与"缺席=保持原值"区分。
	Prices []dto.ChannelModelPrice `json:"prices"`
	// ModelMapping 模型名 → 上游真名；空表导出为 {}（ADR 0005/0008）。
	ModelMapping map[string]string `json:"model_mapping"`
	KeySet       bool              `json:"key_set"`
}

// LaneMemberConfig 车道成员的可导出形态（**两态**，api-spec §4.2）：散成员含
// channel/model；组引用元素只含 group（组名）。
//
// 只导出 `model`（成员所选模型，ADR 0008）：上游真名可由 `model_mapping` 重算，
// **导出冗余的真名反而有害**——把旧文件导回一个已改过映射的实例时，会让旧真名
// 复活、覆盖新映射。成员级覆盖已移除，因此这里没有 `upstream_model` 字段。
type LaneMemberConfig struct {
	Channel string `json:"channel"`
	Model   string `json:"model"`
	// Group 非 empty = 组引用元素（ADR 0009）：Channel/Model/别名/覆盖为空。
	Group       string `json:"group,omitempty"`
	PublicAlias string `json:"public_alias,omitempty"`
	Priority    int    `json:"priority"`
	Overrides   any    `json:"overrides,omitempty"`
	// Enabled 可空三态：导出恒给出（非 nil），导入文件省略时按 api-spec §4.2 的
	// 三态规则处理（新建成员=启用、命中既有成员=保留原值）。若这里用裸 bool，
	// "省略"会被读成 false，一次"导出→清空→导入"就会把运维人工停用的成员
	// 静默放回选路。
	Enabled *bool `json:"enabled,omitempty"`
}

// LaneConfig 显式车道的可导出形态。
type LaneConfig struct {
	Name         string                 `json:"name"`
	Enabled      *bool                  `json:"enabled,omitempty"`
	Mode         *string                `json:"mode,omitempty"`
	ActiveMember string                 `json:"active_member,omitempty"`
	Config       *model.LaneRelayConfig `json:"config,omitempty"`
	Members      []LaneMemberConfig     `json:"members"`
}

// GroupMemberConfig 组内成员的可导出形态（与散成员同构，无 Group 字段）。
type GroupMemberConfig struct {
	Channel     string `json:"channel"`
	Model       string `json:"model"`
	PublicAlias string `json:"public_alias,omitempty"`
	Priority    int    `json:"priority"`
	Overrides   any    `json:"overrides,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

// MemberGroupConfig 成员组的可导出形态（ADR 0009，api-spec §5.6）。
type MemberGroupConfig struct {
	Name    string              `json:"name"`
	Enabled bool                `json:"enabled"`
	Members []GroupMemberConfig `json:"members"`
}

// ClientKeyConfig 客户端密钥的可导出形态（无明文、无哈希）。
type ClientKeyConfig struct {
	Name           string   `json:"name"`
	Enabled        bool     `json:"enabled"`
	LanePolicy     any      `json:"lane_policy"`
	IPAllowlist    []string `json:"ip_allowlist"`
	RateLimitRPM   int      `json:"rate_limit_rpm"`
	MaxConcurrency int      `json:"max_concurrency"`
	ExpiresAt      any      `json:"expires_at"`
	Notes          string   `json:"notes"`
	KeyPrefix      string   `json:"key_prefix"`
}

// ConfigBundle 导出/导入的完整配置。
type ConfigBundle struct {
	Version    string          `json:"version"`
	ExportedAt string          `json:"exported_at,omitempty"`
	Channels   []ChannelConfig `json:"channels"`
	// MemberGroups 成员组（ADR 0009）：导入时**先于车道**处理——车道链上的组引用
	// 按组名解析（api-spec §5.6）。
	MemberGroups []MemberGroupConfig `json:"member_groups"`
	Lanes        []LaneConfig        `json:"lanes"`
	ClientKeys   []ClientKeyConfig   `json:"client_keys"`
	// 指针：区分"调用方没带这个字段"（导入时保持原样）与"显式给了全零值"。
	SystemOptions *systemOptions `json:"system_options,omitempty"`
}

// BuildConfigBundle 汇总当前配置（读操作，不落库）。
func BuildConfigBundle() (*ConfigBundle, error) {
	bundle := &ConfigBundle{Version: "v1", ExportedAt: time.Now().UTC().Format(time.RFC3339)}

	var channels []*model.Channel
	if err := model.DB.Order("name asc").Find(&channels).Error; err != nil {
		return nil, err
	}
	for _, channel := range channels {
		models := make([]string, 0, 4)
		for _, m := range channel.GetModels() {
			if m = strings.TrimSpace(m); m != "" {
				models = append(models, m)
			}
		}
		bundle.Channels = append(bundle.Channels, exportedChannelConfig(channel, models))
	}

	groups, err := model.ListMemberGroups()
	if err != nil {
		return nil, err
	}
	for i := range groups {
		group := &groups[i]
		entry := MemberGroupConfig{Name: group.Name, Enabled: !group.Disabled}
		for _, item := range group.Members {
			channelName := ""
			if channel, chErr := model.ChannelOrNil(item.ChannelId); chErr == nil && channel != nil {
				channelName = channel.Name
			}
			enabled := !item.Disabled
			entry.Members = append(entry.Members, GroupMemberConfig{
				Channel:     channelName,
				Model:       item.Model,
				PublicAlias: item.PublicAlias,
				Priority:    item.Priority,
				Overrides:   jsonObject(item.Overrides),
				Enabled:     &enabled,
			})
		}
		bundle.MemberGroups = append(bundle.MemberGroups, entry)
	}

	lanes, err := model.ListLanes()
	if err != nil {
		return nil, err
	}
	for i := range lanes {
		lane := &lanes[i]
		enabled := lane.Enabled
		mode := lane.Mode
		cfg := model.ParseLaneRelayConfig(lane.Config)
		entry := LaneConfig{
			Name:         lane.Name,
			Enabled:      &enabled,
			Mode:         &mode,
			ActiveMember: lane.ActiveMember,
			Config:       &cfg,
		}
		for _, member := range lane.Members {
			// 组引用元素（ADR 0009）：按组名导出（组实体在 member_groups 里）。
			if member.GroupId != 0 {
				enabled := !member.Disabled
				entry.Members = append(entry.Members, LaneMemberConfig{
					Group:    groupNameById(member.GroupId),
					Priority: member.Priority,
					Enabled:  &enabled,
				})
				continue
			}
			channelName := ""
			if channel, err := model.ChannelOrNil(member.ChannelId); err == nil && channel != nil {
				channelName = channel.Name
			}
			enabled := !member.Disabled
			entry.Members = append(entry.Members, LaneMemberConfig{
				Channel:     channelName,
				Model:       member.Model,
				PublicAlias: member.PublicAlias,
				Priority:    member.Priority,
				Overrides:   jsonObject(member.Overrides),
				Enabled:     &enabled,
			})
		}
		bundle.Lanes = append(bundle.Lanes, entry)
	}

	keys, err := model.ListClientKeys()
	if err != nil {
		return nil, err
	}
	for i := range keys {
		key := &keys[i]
		policy := model.ParseLanePolicy(key.LanePolicy)
		// 空名单统一导出为 []（不是 null），使导出→导入的摘要可比。
		ipAllowlist := []string{}
		if strings.TrimSpace(key.IPAllowlist) != "" {
			_ = json.Unmarshal([]byte(key.IPAllowlist), &ipAllowlist)
		}
		if ipAllowlist == nil {
			ipAllowlist = []string{}
		}
		var expiresAt any
		if key.ExpiresAt != nil && *key.ExpiresAt > 0 {
			expiresAt = time.Unix(*key.ExpiresAt, 0).UTC().Format(time.RFC3339)
		}
		bundle.ClientKeys = append(bundle.ClientKeys, ClientKeyConfig{
			Name:           key.Name,
			Enabled:        key.Enabled,
			LanePolicy:     policy,
			IPAllowlist:    ipAllowlist,
			RateLimitRPM:   key.RateLimitRPM,
			MaxConcurrency: key.MaxConcurrency,
			ExpiresAt:      expiresAt,
			Notes:          key.Notes,
			KeyPrefix:      key.KeyPrefix,
		})
	}
	current := currentSystemOptions()
	bundle.SystemOptions = &current
	return bundle, nil
}

// groupNameById 组 id → 组名（导出车道组引用元素用）；查不到返回空串（悬空引用，
// 正常路径不会出现——删组会级联清理引用）。
func groupNameById(id int) string {
	return groupNameByIdWithDB(model.DB, id)
}

// groupNameByIdWithDB 是 groupNameById 的 db 参数版本；事务内必须传 tx。
func groupNameByIdWithDB(db *gorm.DB, id int) string {
	groups, err := model.LoadMemberGroupsByIDsTx(db, []int{id})
	if err != nil {
		return ""
	}
	if g, ok := groups[id]; ok {
		return g.Name
	}
	return ""
}

// GetExport GET /api/v1/export
func GetExport(c *gin.Context) {
	bundle, err := BuildConfigBundle()
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, bundle)
}

// ImportResult 导入结果：每个资源的归类（新增/更新/不变/跳过）。
//
// diff 的键名遵循 api-spec §6.10：`channels` / `lanes` / `keys` / `options`。
// 注意导出体（ConfigBundle）里用的是 `client_keys` / `system_options`（那是
// 配置文件的字段名）；两处命名不同是有意的——diff 对齐端点资源名，配置文件
// 对齐导出格式。此前 diff 误用了配置文件的名字，AI 按文档取 `diff.keys` 会拿到
// undefined，从而把"有变更"误判为"无变更"（回滚前 dry-run 最危险）。
type ImportResult struct {
	DryRun   bool           `json:"dry_run"`
	Valid    bool           `json:"valid"`
	Diff     map[string]any `json:"diff"`
	Warnings []string       `json:"warnings,omitempty"`
}

// DiffList 单个资源的 diff 列表。
type DiffList struct {
	Add       []string `json:"add"`
	Update    []string `json:"update"`
	Unchanged []string `json:"unchanged"`
	Remove    []string `json:"remove"`
	Skipped   []string `json:"skipped"`
}

// PostImport POST /api/v1/import?dry_run=：幂等 upsert，不删除未出现在配置中的资源。
func PostImport(c *gin.Context) {
	var bundle ConfigBundle
	if err := c.ShouldBindJSON(&bundle); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	result := ImportResult{
		DryRun: dryRun(c),
		Valid:  true,
		Diff: map[string]any{
			"channels":      newDiffList(),
			"member_groups": newDiffList(),
			"lanes":         newDiffList(),
			"keys":          newDiffList(),
			"options":       gin.H{"changed": []string{}},
		},
	}

	// 先做整体校验，避免"导入一半失败"。
	for _, channel := range bundle.Channels {
		if strings.TrimSpace(channel.Name) == "" || strings.TrimSpace(channel.Type) == "" {
			apierr.Validation(c, "channels[].name and channels[].type are required")
			return
		}
		if _, ok := channelTypeBySlug[slugify(channel.Type)]; !ok {
			apierr.Validation(c, "unknown channel type '"+channel.Type+"'")
			return
		}
	}
	for _, group := range bundle.MemberGroups {
		if strings.TrimSpace(group.Name) == "" {
			apierr.Validation(c, "member_groups[].name is required")
			return
		}
	}
	for _, lane := range bundle.Lanes {
		if strings.TrimSpace(lane.Name) == "" {
			apierr.Validation(c, "lanes[].name is required")
			return
		}
		mode := model.LaneModeFailover
		if lane.Mode != nil {
			mode = *lane.Mode
		}
		if !model.ValidLaneMode(mode) {
			apierr.Validation(c, "invalid lane mode '"+mode+"'")
			return
		}
	}

	// 导入前剔除"成员指向不存在渠道"的悬空成员（跳过并警告，而非整包 422）。
	pruneMissingMemberChannels(&bundle, &result)
	// 组侧同理（ADR 0009 / api-spec §5.6）：组内成员渠道悬空剔除（组全空整组剔除）、
	// 车道组引用指向"bundle 与库都没有"的组时剔除该引用。
	pruneMissingGroupsAndRefs(&bundle, &result)

	if result.DryRun {
		// dry-run：先跑与真实导入同一套构造/校验（不落库），再算 diff。
		// 否则调用方会拿到 valid:true，却在真实导入时因"车道成员引用不存在的渠道"等 422 失败。
		if err := validateBundle(c, &bundle); err != nil {
			writeAPIError(c, err)
			return
		}
		if err := planImport(&bundle, &result); err != nil {
			writeAPIError(c, err)
			return
		}
		writeAudit(c, "import", "config", "bundle")
		c.JSON(http.StatusOK, result)
		return
	}

	// 真实导入前也跑与 dry-run 相同的完整构造/校验，避免“校验通过、落库一半才报错”。
	if err := validateBundle(c, &bundle); err != nil {
		writeAPIError(c, err)
		return
	}
	if err := applyImport(c, &bundle, &result); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (r *ImportResult) add(resource, name string) {
	r.appendDiff(resource, func(d *DiffList) { d.Add = append(d.Add, name) })
}

func (r *ImportResult) update(resource, name string) {
	r.appendDiff(resource, func(d *DiffList) { d.Update = append(d.Update, name) })
}

func (r *ImportResult) unchanged(resource, name string) {
	r.appendDiff(resource, func(d *DiffList) { d.Unchanged = append(d.Unchanged, name) })
}

func (r *ImportResult) skip(resource, name string) {
	r.appendDiff(resource, func(d *DiffList) { d.Skipped = append(d.Skipped, name) })
}

// diffResourceKeys 把内部资源名映射为 api-spec §6.10 的 diff 键名。
// 导出体用 `client_keys` / `system_options`，diff 用 `keys` / `options`。
func diffResourceKeys(resource string) string {
	switch resource {
	case "client_keys":
		return "keys"
	case "system_options":
		return "options"
	default:
		return resource
	}
}

func (r *ImportResult) appendDiff(resource string, mutate func(*DiffList)) {
	if r.Diff == nil {
		r.Diff = map[string]any{}
	}
	key := diffResourceKeys(resource)
	list, _ := r.Diff[key].(DiffList)
	mutate(&list)
	r.Diff[key] = list
}

// markOptionsChanged 记录 system_options 的变更（api-spec §6.10 用
// `options: {changed: [...]}` 而非 add/update/remove 三分类）。
func (r *ImportResult) markOptionsChanged(changed bool) {
	if r.Diff == nil {
		r.Diff = map[string]any{}
	}
	changedList := []string{}
	if changed {
		changedList = append(changedList, "system_options")
	}
	r.Diff["options"] = gin.H{"changed": changedList}
}

func newDiffList() DiffList {
	return DiffList{Add: []string{}, Update: []string{}, Unchanged: []string{}, Remove: []string{}, Skipped: []string{}}
}

// channelPayloadFromConfig 把导出形态还原成渠道写入 payload。
// validateBundle（dry-run）与 applyImport（真实落库）共用同一实现，避免两处漂移
// 导致"dry-run 说没问题、真实导入报错"。
func channelPayloadFromConfig(config ChannelConfig) *channelPayload {
	payload := &channelPayload{
		Type:    &config.Type,
		BaseURL: config.BaseURL,
		Enabled: config.Enabled,
	}
	payload.Models = config.Models
	if config.ParamOverride != nil {
		encoded, _ := json.Marshal(config.ParamOverride)
		payload.ParamOverride = encoded
	}
	payload.Proxy = config.Proxy
	// nil = 配置文件里没这个字段 → 保持原值；非 nil（含空表）= 覆盖/清空。
	if config.Prices != nil {
		prices := config.Prices
		payload.Prices = &prices
	}
	if config.ModelMapping != nil {
		mapping := config.ModelMapping
		payload.ModelMapping = &mapping
	}
	return payload
}

// lanePayloadFromConfig 同理：车道的导出形态 → 写入 payload（含组引用元素）。
func lanePayloadFromConfig(lane LaneConfig) *lanePayload {
	payload := &lanePayload{
		Enabled:      lane.Enabled,
		ActiveMember: lane.ActiveMember,
	}
	if lane.Mode != nil {
		payload.Mode = *lane.Mode
	}
	if lane.Config != nil {
		encodedConfig, _ := json.Marshal(lane.Config)
		payload.Config = encodedConfig
	}
	// nil = 字段缺席 = 保持现有成员链；空数组 = 显式清空（buildLane 同口径）。
	if lane.Members == nil {
		payload.Members = nil
	} else {
		payload.Members = make([]laneMemberPayload, 0, len(lane.Members))
	}
	for _, member := range lane.Members {
		entry := laneMemberPayload{
			Channel:     member.Channel,
			Model:       member.Model,
			PublicAlias: member.PublicAlias,
			Priority:    member.Priority,
			// 指针直接透传，保住三态：导出恒有值 → 导入按文件里的开关还原；
			// 老文件省略该字段 → nil → buildLaneMember 按"新建启用/既有保留原值"处理。
			Enabled: member.Enabled,
		}
		// 组引用元素（ADR 0009）：只带 Group 名（buildLaneGroupRef 解析）。
		if member.Group != "" {
			entry = laneMemberPayload{
				Group:    member.Group,
				Priority: member.Priority,
				Enabled:  member.Enabled,
			}
		}
		if member.Overrides != nil && member.Group == "" {
			encoded, _ := json.Marshal(member.Overrides)
			entry.Overrides = encoded
		}
		payload.Members = append(payload.Members, entry)
	}
	return payload
}

// groupPayloadFromConfig 组的导出形态 → 写入 payload。
func groupPayloadFromConfig(config MemberGroupConfig) *memberGroupPayload {
	enabled := config.Enabled
	payload := &memberGroupPayload{Name: config.Name, Enabled: &enabled}
	members := make([]memberGroupItemPayload, 0, len(config.Members))
	for _, item := range config.Members {
		entry := memberGroupItemPayload{
			Channel:     item.Channel,
			Model:       item.Model,
			PublicAlias: item.PublicAlias,
			Priority:    item.Priority,
			Enabled:     item.Enabled,
		}
		if item.Overrides != nil {
			encoded, _ := json.Marshal(item.Overrides)
			entry.Overrides = encoded
		}
		members = append(members, entry)
	}
	payload.Members = &members
	return payload
}

// bundleDeclaredChannels 返回本 bundle 里声明的渠道名集合。
//
// 导入是两阶段（先构造校验全部对象，再落库），构造车道时本 bundle 的新渠道**还没落库**。
// 若只查数据库，导出「新渠道 + 引用它的车道」导入空实例时成员会被误判悬空。因此
// 剪枝必须把"本 bundle 自己声明的渠道"也算作存在（api-spec §5.6 承诺导出文件可跨实例还原）。
func bundleDeclaredChannels(bundle *ConfigBundle) map[string]bool {
	declared := make(map[string]bool, len(bundle.Channels))
	for _, channel := range bundle.Channels {
		if name := strings.TrimSpace(channel.Name); name != "" {
			declared[name] = true
		}
	}
	return declared
}

// pruneMissingMemberChannels 剔除导入 bundle 里"成员指向不存在渠道"的成员，并记录警告。
//
// 判定"存在"= 本 bundle 声明了该渠道 **或** 库里已有该渠道；两者都没有才算悬空。
// 此前一个悬空成员会让整包 422；现在改为跳过该成员（而不是整包失败），并在
// warnings / diff.skipped 里列出，让用户知情；成员被清空的启用车道改为停用
// （而不是整包拒绝），避免写入"启用但无成员"的非法车道。
func pruneMissingMemberChannels(bundle *ConfigBundle, result *ImportResult) {
	declared := bundleDeclaredChannels(bundle)
	for i := range bundle.Lanes {
		lane := &bundle.Lanes[i]
		kept := make([]LaneMemberConfig, 0, len(lane.Members))
		for _, m := range lane.Members {
			name := strings.TrimSpace(m.Channel)
			if name == "" {
				continue
			}
			if declared[name] {
				kept = append(kept, m)
				continue
			}
			if _, err := findChannelByName(name); err != nil {
				// 历史重名不是"渠道不存在"：保留成员，交给 validateBundle 报 409，
				// 不能把成员静默剪成悬空。
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					kept = append(kept, m)
					continue
				}
				result.Warnings = append(result.Warnings,
					"lane "+lane.Name+": skipped member '"+name+"' (channel not found)")
				result.skip("lanes", lane.Name+"#"+name)
				continue
			}
			kept = append(kept, m)
		}
		if len(kept) == len(lane.Members) {
			continue
		}
		lane.Members = kept
		if len(kept) == 0 {
			disabled := false
			lane.Enabled = &disabled
			result.Warnings = append(result.Warnings,
				"lane "+lane.Name+" has no valid members left; imported as disabled")
		}
	}
}

// pruneMissingGroupsAndRefs 组侧悬空剔除（api-spec §5.6）：①组内成员指向不存在
// 渠道（bundle 声明或库存在都不算悬空）→ 剔除该成员并警告，组全空 → 整组剔除；
// ②车道链上的组引用指向"bundle 与库都没有"的组 → 剔除该引用并警告。
func pruneMissingGroupsAndRefs(bundle *ConfigBundle, result *ImportResult) {
	declaredChannels := bundleDeclaredChannels(bundle)
	declaredGroups := make(map[string]bool, len(bundle.MemberGroups))
	for _, g := range bundle.MemberGroups {
		if name := strings.TrimSpace(g.Name); name != "" {
			declaredGroups[name] = true
		}
	}
	keptGroups := make([]MemberGroupConfig, 0, len(bundle.MemberGroups))
	for i := range bundle.MemberGroups {
		group := &bundle.MemberGroups[i]
		kept := make([]GroupMemberConfig, 0, len(group.Members))
		for _, m := range group.Members {
			name := strings.TrimSpace(m.Channel)
			if name == "" {
				continue
			}
			if declaredChannels[name] {
				kept = append(kept, m)
				continue
			}
			if _, err := findChannelByName(name); err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					kept = append(kept, m)
					continue
				}
				result.Warnings = append(result.Warnings,
					"member group "+group.Name+": skipped member '"+name+"' (channel not found)")
				result.skip("member_groups", group.Name+"#"+name)
				continue
			}
			kept = append(kept, m)
		}
		if len(kept) == 0 {
			result.Warnings = append(result.Warnings,
				"member group "+group.Name+" has no valid members left; group not imported")
			result.skip("member_groups", group.Name)
			// 已剔除的组不能再作为车道组引用的解析目标。
			delete(declaredGroups, strings.TrimSpace(group.Name))
			continue
		}
		group.Members = kept
		keptGroups = append(keptGroups, *group)
	}
	bundle.MemberGroups = keptGroups

	for i := range bundle.Lanes {
		lane := &bundle.Lanes[i]
		keptElems := make([]LaneMemberConfig, 0, len(lane.Members))
		for _, m := range lane.Members {
			if m.Group == "" {
				keptElems = append(keptElems, m)
				continue
			}
			if declaredGroups[m.Group] {
				keptElems = append(keptElems, m)
				continue
			}
			if _, err := model.GetMemberGroupByName(m.Group); err == nil {
				keptElems = append(keptElems, m)
				continue
			}
			result.Warnings = append(result.Warnings,
				"lane "+lane.Name+": skipped group reference '"+m.Group+"' (member group not found)")
			result.skip("lanes", lane.Name+"#"+m.Group)
		}
		if len(keptElems) == len(lane.Members) {
			continue
		}
		lane.Members = keptElems
		if len(keptElems) == 0 {
			disabled := false
			lane.Enabled = &disabled
			result.Warnings = append(result.Warnings,
				"lane "+lane.Name+" has no valid members left; imported as disabled")
		}
	}
}

// validateBundle 用与真实导入完全相同的构造/校验路径检查 bundle（不落库）。
// dry-run 必须能发现"apply 会失败"的问题，否则 valid:true 是误导。
//
// 只返回错误、不写响应：响应由 PostImport 统一写一次，避免"helper 写一次 +
// 调用方再写一次"造成响应体拼接两个 JSON（审查 F6）。
func validateBundle(c *gin.Context, bundle *ConfigBundle) error {
	if err := validateImportChannelModelRefs(bundle); err != nil {
		return err
	}
	seenChannels := make(map[string]bool, len(bundle.Channels))
	for _, channel := range bundle.Channels {
		name := strings.TrimSpace(channel.Name)
		if seenChannels[name] {
			return &apiError{status: http.StatusConflict, code: apierr.CodeConflict,
				message: "channel name '" + name + "' is duplicated in this bundle"}
		}
		seenChannels[name] = true
		found, findErr := findChannelByName(name)
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			var ambiguous *model.ChannelNameAmbiguousError
			if errors.As(findErr, &ambiguous) {
				return &apiError{status: http.StatusConflict, code: apierr.CodeChannelNameAmbiguous, message: ambiguous.Error()}
			}
			return findErr
		}
		isCreate := errors.Is(findErr, gorm.ErrRecordNotFound)
		payload := channelPayloadFromConfig(channel)
		if _, buildErr := buildChannel(name, found, payload, isCreate); buildErr != nil {
			return &apiError{status: http.StatusBadRequest, code: buildErr.code, message: buildErr.message}
		}
	}
	// dry-run 必须与真实导入同口径：bundle 内声明的渠道同样算存在。
	resolver := func(name string) (*model.Channel, bool) {
		for i := range bundle.Channels {
			if strings.TrimSpace(bundle.Channels[i].Name) == name {
				found, findErr := findChannelByName(name)
				if findErr != nil {
					if !errors.Is(findErr, gorm.ErrRecordNotFound) {
						return nil, false
					}
					// 尚未落库的新渠道：给一个带名字的占位对象即可（只用于取 Id/名字做校验）。
					return &model.Channel{Name: name}, true
				}
				return found, true
			}
		}
		return nil, false
	}
	// 组先于车道校验（ADR 0009 / api-spec §5.6）：车道链上的组引用要能解析到
	// "bundle 声明"的组——dry-run 不落库，用内存构造体顶替。
	builtGroups := map[string]*model.MemberGroup{}
	for _, group := range bundle.MemberGroups {
		existing, existingErr := model.GetMemberGroupByName(group.Name)
		if existingErr != nil && !errors.Is(existingErr, model.ErrMemberGroupNotFound) {
			return existingErr
		}
		if errors.Is(existingErr, model.ErrMemberGroupNotFound) {
			existing = nil
		}
		built, buildErr := buildMemberGroup(groupPayloadFromConfig(group), existing, resolver, model.DB)
		if buildErr != nil {
			return buildErr
		}
		builtGroups[built.Name] = built
	}
	groupResolver := func(name string) (*model.MemberGroup, bool) {
		if g, ok := builtGroups[name]; ok {
			return g, true
		}
		found, err := model.GetMemberGroupByName(name)
		if err != nil {
			return nil, false
		}
		return found, true
	}
	for _, lane := range bundle.Lanes {
		if _, buildErr := buildLane(lane.Name, lanePayloadFromConfig(lane), &laneBuildLookup{channel: resolver, group: groupResolver}); buildErr != nil {
			return &apiError{status: buildErr.status, code: buildErr.code, message: buildErr.message, hint: buildErr.hint}
		}
	}
	return nil
}

// planImport 计算 diff（不改库）。
func planImport(bundle *ConfigBundle, result *ImportResult) error {
	for _, channel := range bundle.Channels {
		existing, err := findChannelByName(channel.Name)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			result.add("channels", channel.Name)
		case err != nil:
			return err
		case channelDigestOfChannel(existing) == model.DigestOf(channel):
			result.unchanged("channels", channel.Name)
		default:
			result.update("channels", channel.Name)
		}
	}
	lanes, err := model.ListLanes()
	if err != nil {
		return err
	}
	laneByName := map[string]*model.Lane{}
	for i := range lanes {
		laneByName[lanes[i].Name] = &lanes[i]
	}
	for _, group := range bundle.MemberGroups {
		existing, existingErr := model.GetMemberGroupByName(group.Name)
		switch {
		case errors.Is(existingErr, model.ErrMemberGroupNotFound):
			result.add("member_groups", group.Name)
		case existingErr != nil:
			return existingErr
		case groupDigestOfGroup(existing) == groupDigestOf(&group):
			result.unchanged("member_groups", group.Name)
		default:
			result.update("member_groups", group.Name)
		}
	}
	for _, lane := range bundle.Lanes {
		if _, ok := laneByName[lane.Name]; !ok {
			result.add("lanes", lane.Name)
			continue
		}
		if laneDigestOf(&lane) == laneDigestOfLane(laneByName[lane.Name]) {
			result.unchanged("lanes", lane.Name)
		} else {
			result.update("lanes", lane.Name)
		}
	}
	keys, err := model.ListClientKeys()
	if err != nil {
		return err
	}
	keyByName := map[string]*model.ClientKey{}
	for i := range keys {
		keyByName[keys[i].Name] = &keys[i]
	}
	for _, key := range bundle.ClientKeys {
		existing, ok := keyByName[key.Name]
		if !ok {
			// 导出不含明文/哈希，无法在目标实例上凭本文件新建凭据。
			result.skip("client_keys", key.Name)
			result.Warnings = append(result.Warnings,
				"client key '"+key.Name+"' 不存在且配置里没有明文，已跳过（需用 POST /api/v1/keys 重建或导入原哈希）")
			continue
		}
		if clientKeyDigestOf(existing) == model.DigestOf(key) {
			result.unchanged("client_keys", key.Name)
		} else {
			result.update("client_keys", key.Name)
		}
	}
	if bundle.SystemOptions == nil {
		return nil
	}
	if model.DigestOf(currentSystemOptions()) == model.DigestOf(*bundle.SystemOptions) {
		result.markOptionsChanged(false)
	} else {
		result.markOptionsChanged(true)
	}
	return nil
}

// applyImport 落库（与 planImport 使用同一套比较口径）。
//
// 分层顺序：**渠道 → 成员组 → 车道**，每层"构造+校验全部 → 逐条落库"后再进下一层
// ——车道链上的组引用与组内成员的 ChannelId 都要真实 id，组/渠道必须先落库
// （ADR 0009 / api-spec §5.6"组先于车道导入"）。中途失败可能留下"渠道/组已入库、
// 车道未入"的中间态：全部写入是幂等 upsert，重新导入同一 bundle 即收敛。
// validateImportChannelModelRefs 防止导入把渠道 models 收窄成“仍有 bundle 外
// 车道/成员组引用”的状态。bundle 内声明的车道/组视为会一起更新，不在此拦截。
func validateImportChannelModelRefs(bundle *ConfigBundle) error {
	laneSet := make(map[string]bool, len(bundle.Lanes))
	for _, lane := range bundle.Lanes {
		laneSet[strings.TrimSpace(lane.Name)] = true
	}
	groupSet := make(map[string]bool, len(bundle.MemberGroups))
	for _, group := range bundle.MemberGroups {
		groupSet[strings.TrimSpace(group.Name)] = true
	}
	for _, cfg := range bundle.Channels {
		if cfg.Models == nil {
			// 字段缺席 = 保持原值，不会移除任何模型。
			continue
		}
		existing, err := findChannelByName(strings.TrimSpace(cfg.Name))
		if err != nil || existing == nil {
			continue
		}
		_, removed := diffModels(existing.GetModels(), cfg.Models)
		if len(removed) == 0 {
			continue
		}
		refs, refErr := model.RemovedModelReferences(existing.Id, removed)
		if refErr != nil {
			return refErr
		}
		outsideLanes := make([]string, 0)
		for _, lane := range refs.Lanes {
			if !laneSet[lane] {
				outsideLanes = append(outsideLanes, lane)
			}
		}
		outsideGroups := make([]string, 0)
		for _, group := range refs.Groups {
			if !groupSet[group] {
				outsideGroups = append(outsideGroups, group)
			}
		}
		if len(outsideLanes) > 0 || len(outsideGroups) > 0 {
			return &apiError{status: http.StatusConflict, code: apierr.CodeConflict,
				message: "import would remove models still referenced outside the bundle: lanes=" +
					strings.Join(outsideLanes, ", ") + " groups=" + strings.Join(outsideGroups, ", "),
				hint: "include those lanes/member groups in the bundle or remove the references first"}
		}
	}
	return nil
}

func applyImport(c *gin.Context, bundle *ConfigBundle, result *ImportResult) error {
	if err := validateImportChannelModelRefs(bundle); err != nil {
		return err
	}
	type channelPlan struct {
		name     string
		built    *model.Channel
		config   ChannelConfig
		existing *model.Channel
		isCreate bool
	}
	type groupPlan struct {
		name     string
		built    *model.MemberGroup
		config   MemberGroupConfig
		existing *model.MemberGroup
		isCreate bool
	}
	type lanePlan struct {
		name     string
		built    *model.Lane
		config   LaneConfig
		existing *model.Lane
		isCreate bool
	}

	// —— 第一层：渠道（先构造；落库在下方事务内）——
	channelPlans := make([]channelPlan, 0, len(bundle.Channels))
	seenChannels := make(map[string]bool, len(bundle.Channels))
	for _, channel := range bundle.Channels {
		name := strings.TrimSpace(channel.Name)
		if seenChannels[name] {
			return &apiError{status: http.StatusConflict, code: apierr.CodeConflict,
				message: "channel name '" + name + "' is duplicated in this bundle"}
		}
		seenChannels[name] = true
		found, findErr := findChannelByName(name)
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			var ambiguous *model.ChannelNameAmbiguousError
			if errors.As(findErr, &ambiguous) {
				return &apiError{status: http.StatusConflict, code: apierr.CodeChannelNameAmbiguous, message: ambiguous.Error()}
			}
			return findErr
		}
		isCreate := errors.Is(findErr, gorm.ErrRecordNotFound)
		built, buildErr := buildChannel(name, found, channelPayloadFromConfig(channel), isCreate)
		if buildErr != nil {
			return &apiError{status: http.StatusBadRequest, code: buildErr.code, message: buildErr.message}
		}
		channelPlans = append(channelPlans, channelPlan{
			name: name, built: built, config: channel, existing: found, isCreate: isCreate,
		})
	}

	// 密钥与选项快照在事务外读取；密钥写入进事务，选项在提交后用 bulk 事务写。
	keys, err := model.ListClientKeys()
	if err != nil {
		return err
	}
	keyByName := map[string]*model.ClientKey{}
	for i := range keys {
		keyByName[keys[i].Name] = &keys[i]
	}
	var optionUpdates map[string]string
	var beforeOptions systemOptions
	if bundle.SystemOptions != nil {
		beforeOptions = currentSystemOptions()
		optionUpdates, err = systemOptionsUpdates(*bundle.SystemOptions)
		if err != nil {
			return err
		}
	}

	err = model.DB.Transaction(func(tx *gorm.DB) error {
		// —— 渠道落库 ——
		builtChannels := make(map[string]*model.Channel, len(channelPlans))
		for _, plan := range channelPlans {
			built := plan.built
			if plan.isCreate {
				built.CreatedTime = time.Now().Unix()
				built.UpdatedAt = built.CreatedTime
				if err := tx.Create(built).Error; err != nil {
					return err
				}
				result.add("channels", plan.name)
			} else {
				built.UpdatedAt = time.Now().Unix()
				if err := tx.Model(&model.Channel{}).Where("id = ?", built.Id).
					Select("*").Omit("id", "created_time").Updates(built).Error; err != nil {
					return err
				}
				if plan.existing != nil && channelDigestOfChannel(plan.existing) == model.DigestOf(plan.config) {
					result.unchanged("channels", plan.name)
				} else {
					result.update("channels", plan.name)
				}
			}
			builtChannels[plan.name] = built
		}
		channelResolver := func(name string) (*model.Channel, bool) {
			if ch, ok := builtChannels[name]; ok {
				return ch, true
			}
			ch, err := model.GetChannelByNameTx(tx, name)
			if err != nil {
				return nil, false
			}
			return ch, true
		}

		// —— 成员组（渠道已在事务内构造；组成员用 resolver 回填真实 id）——
		groupPlans := make([]groupPlan, 0, len(bundle.MemberGroups))
		seenGroups := make(map[string]bool, len(bundle.MemberGroups))
		for _, group := range bundle.MemberGroups {
			name := strings.TrimSpace(group.Name)
			if name == "" {
				return &apiError{status: http.StatusBadRequest, code: apierr.CodeValidationFailed,
					message: "member_groups[].name is required"}
			}
			if seenGroups[name] {
				return &apiError{status: http.StatusConflict, code: apierr.CodeConflict,
					message: "member group name '" + name + "' is duplicated in this bundle"}
			}
			seenGroups[name] = true
			existing, existingErr := model.GetMemberGroupByNameTx(tx, name)
			isCreate := errors.Is(existingErr, model.ErrMemberGroupNotFound)
			if existingErr != nil && !isCreate {
				return existingErr
			}
			if isCreate {
				existing = nil
			}
			built, buildErr := buildMemberGroup(groupPayloadFromConfig(group), existing, channelResolver, tx)
			if buildErr != nil {
				return buildErr
			}
			groupPlans = append(groupPlans, groupPlan{
				name: name, built: built, config: group, existing: existing, isCreate: isCreate,
			})
		}
		builtGroups := make(map[string]*model.MemberGroup, len(groupPlans))
		for _, plan := range groupPlans {
			if err := model.UpsertMemberGroupTx(tx, plan.built); err != nil {
				return err
			}
			builtGroups[plan.name] = plan.built
			if plan.isCreate {
				result.add("member_groups", plan.name)
			} else if plan.existing != nil && groupDigestOfGroupWithDB(tx, plan.existing) == groupDigestOf(&plan.config) {
				result.unchanged("member_groups", plan.name)
			} else {
				result.update("member_groups", plan.name)
			}
		}
		groupResolver := func(name string) (*model.MemberGroup, bool) {
			if group, ok := builtGroups[name]; ok {
				return group, true
			}
			group, err := model.GetMemberGroupByNameTx(tx, name)
			if err != nil {
				return nil, false
			}
			return group, true
		}

		// —— 车道（渠道/组均已构造，成员 id 由 resolver 回填）——
		for _, lane := range bundle.Lanes {
			existing, existingErr := model.GetLaneByNameTx(tx, lane.Name)
			isCreate := errors.Is(existingErr, gorm.ErrRecordNotFound)
			if existingErr != nil && !isCreate {
				return existingErr
			}
			var existingPtr *model.Lane
			if !isCreate {
				existingPtr = existing
			}
			built, buildErr := buildLane(lane.Name, lanePayloadFromConfig(lane), &laneBuildLookup{
				channel: channelResolver,
				group:   groupResolver,
				db:      tx,
			})
			if buildErr != nil {
				return &apiError{status: buildErr.status, code: buildErr.code, message: buildErr.message, hint: buildErr.hint}
			}
			if err := model.ValidateLaneActiveMemberTx(tx, built); err != nil {
				return &apiError{status: http.StatusUnprocessableEntity, code: apierr.CodeInvalidActiveMember,
					message: err.Error(), hint: "fix the lane's active_member in the bundle"}
			}
			if err := model.UpsertLaneTx(tx, built); err != nil {
				return err
			}
			if isCreate {
				result.add("lanes", lane.Name)
			} else if existingPtr != nil && laneDigestOfLaneWithDB(tx, existingPtr) == laneDigestOf(&lane) {
				result.unchanged("lanes", lane.Name)
			} else {
				result.update("lanes", lane.Name)
			}
		}

		// —— 客户端密钥（导出不含明文/哈希；只更新既有密钥的策略字段）——
		for _, entry := range bundle.ClientKeys {
			existing := keyByName[entry.Name]
			if existing == nil {
				result.skip("client_keys", entry.Name)
				result.Warnings = append(result.Warnings,
					"client key '"+entry.Name+"' 不存在且配置里没有明文，已跳过（需用 POST /api/v1/keys 重建或导入原哈希）")
				continue
			}
			updated := &model.ClientKey{Name: entry.Name}
			if buildErr := applyClientKeyPayload(updated, clientKeyPayloadToConfig(entry)); buildErr != nil {
				return &apiError{status: http.StatusBadRequest, code: buildErr.code, message: buildErr.message}
			}
			updated.Enabled = entry.Enabled
			if err := model.UpsertClientKeyTx(tx, updated); err != nil {
				return err
			}
			if clientKeyDigestOf(existing) == model.DigestOf(entry) {
				result.unchanged("client_keys", entry.Name)
			} else {
				result.update("client_keys", entry.Name)
			}
		}
		if optionUpdates != nil {
			if err := model.UpdateOptionsBulkTx(tx, optionUpdates); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	model.InitChannelCache()
	// 导入的组成员变更也可能让 bundle 之外的车道失去点名成员；统一清空悬空 pin。
	if cleared, err := model.ReconcileAllManualActiveMembers(); err != nil {
		common.SysError("failed to reconcile manual lanes after import: " + err.Error())
	} else if len(cleared) > 0 {
		common.SysLog("import cleared dangling manual lane active members: " + strings.Join(cleared, ", "))
	}

	// 选项 DB 写入已与其它配置同事务提交；这里只刷新运行时 OptionMap。
	if optionUpdates != nil {
		if err := model.PublishOptions(optionUpdates); err != nil {
			return err
		}
		result.markOptionsChanged(model.DigestOf(beforeOptions) != model.DigestOf(currentSystemOptions()))
	}
	writeAudit(c, "import", "config", "bundle")
	return nil
}

func clientKeyPayloadToConfig(entry ClientKeyConfig) *clientKeyPayload {
	payload := &clientKeyPayload{Enabled: &entry.Enabled}
	policy := lanePolicyPayload{Mode: model.LanePolicyModeAll}
	if raw, ok := entry.LanePolicy.(map[string]any); ok {
		if mode, ok := raw["mode"].(string); ok && mode != "" {
			policy.Mode = mode
		}
		policy.AllowLanes = toStringSlice(raw["allow_lanes"])
		policy.DenyLanes = toStringSlice(raw["deny_lanes"])
	}
	payload.LanePolicy = &policy
	allowlist := entry.IPAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	payload.IPAllowlist = &allowlist
	rpm := entry.RateLimitRPM
	payload.RateLimitRPM = &rpm
	concurrency := entry.MaxConcurrency
	payload.MaxConcurrency = &concurrency
	notes := entry.Notes
	payload.Notes = &notes
	if entry.ExpiresAt != nil {
		if raw, ok := entry.ExpiresAt.(string); ok {
			payload.ExpiresAt = &raw
		}
	}
	return payload
}

func toStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// exportedChannelConfig 把库内渠道归一化成导出形态。**导出与摘要共用同一实现**，
// 避免两处字段漂移导致"导出丢了字段、diff 却报无变更"。
//
// Prices / ModelMapping 一律给非 nil 值（空 → [] / {}）：这样"导出 → 导入"能把
// 清空动作也带过去，且再次导出的摘要稳定；JSON 里字段缺席（旧导出文件）反序列化后
// 是 nil，导入侧据此保持原值。
func exportedChannelConfig(channel *model.Channel, models []string) ChannelConfig {
	if models == nil {
		models = []string{}
	}
	prices := channel.GetSetting().PBRPrices
	if prices == nil {
		prices = []dto.ChannelModelPrice{}
	}
	mapping := channel.ModelMappingMap()
	if mapping == nil {
		mapping = map[string]string{}
	}
	baseURL := channel.GetBaseURL()
	enabled := channel.Status == common.ChannelStatusEnabled
	proxy := channel.GetSetting().Proxy
	return ChannelConfig{
		Name:          channel.Name,
		Type:          ChannelTypeSlug(channel.Type),
		BaseURL:       &baseURL,
		Models:        models,
		ParamOverride: jsonObject(derefString(channel.ParamOverride)),
		Enabled:       &enabled,
		Proxy:         &proxy,
		Prices:        prices,
		ModelMapping:  mapping,
		KeySet:        strings.TrimSpace(channel.Key) != "",
	}
}

// 归一化摘要：导出形态与库内形态必须落到同一个可比对象上。
func channelDigestOfChannel(channel *model.Channel) string {
	models := make([]string, 0, 4)
	for _, m := range channel.GetModels() {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	return model.DigestOf(exportedChannelConfig(channel, models))
}

func laneDigestOf(lane *LaneConfig) string {
	copyLane := *lane
	if copyLane.Config == nil {
		cfg := model.DefaultLaneRelayConfig()
		copyLane.Config = &cfg
	} else {
		cfg := copyLane.Config.Normalize()
		copyLane.Config = &cfg
	}
	if copyLane.Members == nil {
		copyLane.Members = []LaneMemberConfig{}
	}
	return model.DigestOf(copyLane)
}

func laneDigestOfLane(lane *model.Lane) string {
	return laneDigestOfLaneWithDB(model.DB, lane)
}

// laneDigestOfLaneWithDB 是 laneDigestOfLane 的 db 参数版本；事务内必须传 tx。
func laneDigestOfLaneWithDB(db *gorm.DB, lane *model.Lane) string {
	enabled := lane.Enabled
	mode := lane.Mode
	cfg := model.ParseLaneRelayConfig(lane.Config)
	config := LaneConfig{
		Name:         lane.Name,
		Enabled:      &enabled,
		Mode:         &mode,
		ActiveMember: lane.ActiveMember,
		Config:       &cfg,
		Members:      []LaneMemberConfig{},
	}
	for _, member := range lane.Members {
		// 组引用元素（ADR 0009）：按组名进摘要——组引用增删/换组/引用开关变化
		// 都必须让 digest 变化，否则导入 diff 会把"链上组引用改动"伪装成无变更。
		if member.GroupId != 0 {
			memberEnabled := !member.Disabled
			config.Members = append(config.Members, LaneMemberConfig{
				Group:    groupNameByIdWithDB(db, member.GroupId),
				Priority: member.Priority,
				Enabled:  &memberEnabled,
			})
			continue
		}
		channelName := ""
		if channel, err := model.ChannelOrNilTx(db, member.ChannelId); err == nil && channel != nil {
			channelName = channel.Name
		}
		// Enabled 必须进摘要：api-spec §5.2 承诺"谁把这条成员关掉了"可从审计
		// 判定，靠的就是 after_digest 随开关变化而变化。漏进来会让一次停用
		// 在审计里与不动作同形。
		memberEnabled := !member.Disabled
		config.Members = append(config.Members, LaneMemberConfig{
			Channel:     channelName,
			Model:       member.Model,
			PublicAlias: member.PublicAlias,
			Priority:    member.Priority,
			Overrides:   jsonObject(member.Overrides),
			Enabled:     &memberEnabled,
		})
	}
	return laneDigestOf(&config)
}

// groupDigestOf / groupDigestOfGroup 组的归一化摘要（导入 diff 口径，同车道模式）。
func groupDigestOf(config *MemberGroupConfig) string {
	copyGroup := *config
	if copyGroup.Members == nil {
		copyGroup.Members = []GroupMemberConfig{}
	}
	return model.DigestOf(copyGroup)
}

func groupDigestOfGroup(group *model.MemberGroup) string {
	return groupDigestOfGroupWithDB(model.DB, group)
}

// groupDigestOfGroupWithDB 是 groupDigestOfGroup 的 db 参数版本；事务内必须传 tx。
func groupDigestOfGroupWithDB(db *gorm.DB, group *model.MemberGroup) string {
	config := MemberGroupConfig{Name: group.Name, Enabled: !group.Disabled, Members: []GroupMemberConfig{}}
	for _, item := range group.Members {
		channelName := ""
		if channel, err := model.ChannelOrNilTx(db, item.ChannelId); err == nil && channel != nil {
			channelName = channel.Name
		}
		enabled := !item.Disabled
		config.Members = append(config.Members, GroupMemberConfig{
			Channel:     channelName,
			Model:       item.Model,
			PublicAlias: item.PublicAlias,
			Priority:    item.Priority,
			Overrides:   jsonObject(item.Overrides),
			Enabled:     &enabled,
		})
	}
	return groupDigestOf(&config)
}

func clientKeyDigestOf(key *model.ClientKey) string {
	ipAllowlist := []string{}
	if strings.TrimSpace(key.IPAllowlist) != "" {
		_ = json.Unmarshal([]byte(key.IPAllowlist), &ipAllowlist)
	}
	if ipAllowlist == nil {
		ipAllowlist = []string{}
	}
	var expiresAt any
	if key.ExpiresAt != nil && *key.ExpiresAt > 0 {
		expiresAt = time.Unix(*key.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	return model.DigestOf(ClientKeyConfig{
		Name:           key.Name,
		Enabled:        key.Enabled,
		LanePolicy:     model.ParseLanePolicy(key.LanePolicy),
		IPAllowlist:    ipAllowlist,
		RateLimitRPM:   key.RateLimitRPM,
		MaxConcurrency: key.MaxConcurrency,
		ExpiresAt:      expiresAt,
		Notes:          key.Notes,
		KeyPrefix:      key.KeyPrefix,
	})
}

// GetCapabilities GET /api/v1/capabilities
func GetCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"api_version":     "v1",
		"lane_modes":      []string{model.LaneModeFailover, model.LaneModeManual},
		"adapters":        AdapterList(),
		"inbound_formats": []string{"openai", "openai_responses", "anthropic", "embeddings"},
		"circuit": gin.H{
			"states":   []string{route.CircuitClosed, route.CircuitOpen, route.CircuitHalfOpen},
			"settings": route.CurrentCircuitSettings(),
		},
		// 模型面能力枚举（design-v1 §4、§4.2；api-spec §5.1）。只增键、不删既有键。
		//
		// 这里的 true/false 必须与 router 的实际注册一致，由
		// router.TestCapabilitiesMatchRegisteredModelRoutes 守卫：声明 true 的功能
		// 必须有路由，声明 false 的不得已注册。
		"features": gin.H{
			"responses":         true,
			"count_tokens":      true,
			"images_variations": true,
			"moderations":       true,
			"files":             true,
			"batches":           true,
			"metrics":           true,
		},
		// 异步对象（Files/Batches，ADR 0010）：lane_header 是契约里固定的寻址头。
		// anthropic_files（ADR 0011）表示网关侧 Anthropic Files 存储已启用：
		// anthropic-beta: files-api-* 的请求按该语义分流，不需要 lane 头。
		"async_objects": gin.H{
			"lane_header":     "X-PBR-Lane",
			"files":           true,
			"batches":         true,
			"anthropic_files": true,
		},
		// 指标导出（api-spec §5.1）：根路径、Prometheus 文本格式、复用管理密钥。
		"metrics": gin.H{
			"path":   "/metrics",
			"format": "prometheus",
			"auth":   "admin_key",
		},
	})
}

// GetOpenAPI GET /api/openapi.json（免鉴权）
//
// 由端点表生成，保证与实现同步（api-spec §2 要求自描述、可被工具解析）。
func GetOpenAPI(c *gin.Context) {
	doc := gin.H{
		"openapi": "3.0.3",
		"info": gin.H{
			"title":       "PowerBarRations 管理 API",
			"version":     common.Version,
			"description": "单层化 LLM 聚合网关的管理面契约。管理密钥 = Base64(SHA256(登录口令))。",
		},
		"servers": []gin.H{{"url": "/api"}},
		"components": gin.H{
			"securitySchemes": gin.H{
				"adminKey": gin.H{"type": "http", "scheme": "bearer"},
			},
			"schemas": gin.H{
				"Error": gin.H{
					"type": "object",
					"properties": gin.H{"error": gin.H{
						"type": "object",
						"properties": gin.H{
							"code":    gin.H{"type": "string"},
							"message": gin.H{"type": "string"},
							"hint":    gin.H{"type": "string"},
							"details": gin.H{"type": "object", "description": "可选结构化明细（如车道引用守卫的 blocked 映射）"},
						},
						"required": []string{"code", "message"},
					}},
				},
			},
		},
		"security": []gin.H{{"adminKey": []string{}}},
		"paths":    openAPIPaths(),
	}
	c.JSON(http.StatusOK, doc)
}

// OpenAPIPathsForTest 暴露端点表给路由覆盖守卫测试（design-v1 §16.3）。
func OpenAPIPathsForTest() gin.H { return openAPIPaths() }

// openAPIPaths 端点表：新增端点时同步这里。
func openAPIPaths() gin.H {
	pathParam := func(name string) []gin.H {
		return []gin.H{{"name": name, "in": "path", "required": true, "schema": gin.H{"type": "string"}}}
	}
	dryRunParam := gin.H{"name": "dry_run", "in": "query", "schema": gin.H{"type": "boolean"}}
	forceParam := gin.H{"name": "force", "in": "query", "schema": gin.H{"type": "boolean"}}
	queryParam := func(name, description string) gin.H {
		return gin.H{"name": name, "in": "query", "description": description, "schema": gin.H{"type": "string"}}
	}
	okResponse := gin.H{"200": gin.H{"description": "OK"}}
	secured := func(method, summary string, params []gin.H) gin.H {
		operation := gin.H{"summary": summary, "responses": okResponse}
		if len(params) > 0 {
			operation["parameters"] = params
		}
		return gin.H{method: operation}
	}
	return gin.H{
		"/health":        secured("get", "存活与依赖状态", nil),
		"/version":       secured("get", "版本与构建信息", nil),
		"/setup/status":  secured("get", "是否已初始化", nil),
		"/setup":         secured("post", "首次设置登录口令", nil),
		"/auth/login":    secured("post", "口令换管理密钥并签发会话 Cookie", nil),
		"/auth/logout":   secured("post", "清除会话 Cookie", nil),
		"/auth/session":  secured("get", "查询会话状态（authenticated / stale）", nil),
		"/auth/password": secured("post", "修改口令（管理密钥随之变化）", nil),
		"/capabilities":  secured("get", "适配器、模式与能力枚举", nil),
		"/openapi.json":  secured("get", "OpenAPI 3 文档", nil),
		"/system/options": gin.H{
			"get": secured("get", "全局选项", nil)["get"],
			"put": secured("put", "更新全局选项", nil)["put"],
		},
		"/webhooks": gin.H{
			"get": secured("get", "Webhook 目标配置（secret 掩码回显）", nil)["get"],
			"put": secured("put", "Webhook 目标配置全量 upsert（secret 留空 = 保留原值）",
				[]gin.H{dryRunParam})["put"],
		},
		"/webhooks/test": gin.H{
			"post": secured("post", "向指定目标同步发一条测试事件（同步等待含重试的最终结果）", nil)["post"],
		},
		"/webhooks/deliveries": gin.H{
			"get": secured("get", "投递记录（cursor 分页，按时间倒序）",
				[]gin.H{queryParam("cursor", "上一页返回的 next_cursor"),
					queryParam("limit", "默认 50，上限 200")})["get"],
		},
		"/channels": gin.H{
			"get": secured("get", "渠道列表", nil)["get"],
		},
		"/channels/export": gin.H{
			"get": secured("get", "导出渠道（CSV，全量；不含密钥明文，只含 key_set/key_prefix）", nil)["get"],
		},
		"/channels/{name}": gin.H{
			"get":    secured("get", "渠道详情", pathParam("name"))["get"],
			"put":    secured("put", "渠道全量 upsert（key 只写不读）", append(pathParam("name"), dryRunParam))["put"],
			"delete": secured("delete", "删除渠道", pathParam("name"))["delete"],
		},
		"/model-status": gin.H{
			"get": secured("get", "按上游模型聚合的运行表现（只读）：成功率 / 平均延迟 / 输出速度(tps) / token 数",
				[]gin.H{queryParam("from", "起始时间（RFC3339 或 Unix 秒；省略=最近 24 小时）"),
					queryParam("to", "结束时间（同上）")})["get"],
		},
		"/channel-speed": gin.H{
			"get": secured("get", "按渠道聚合的实时速度（只读）：成功率 / 延迟分位 / 输出速度(tps) / token 数；覆盖全部配置渠道",
				[]gin.H{queryParam("from", "起始时间（RFC3339 或 Unix 秒；省略=最近 15 分钟）"),
					queryParam("to", "结束时间（同上）")})["get"],
		},
		"/channels/status": gin.H{
			"get": secured("get", "全部渠道的状态汇总（只读；window=24h|7d）",
				[]gin.H{queryParam("window", "统计窗口：24h（默认）或 7d")})["get"],
		},
		"/channels/{name}/status": gin.H{
			"get": secured("get", "单渠道状态详情（只读）：运行态 / 窗口统计 / 最近失败 / 探活历史 / 上游变动提示",
				append(pathParam("name"), queryParam("window", "统计窗口：24h（默认）或 7d")))["get"],
		},
		"/channels/{name}/test": gin.H{
			"post": secured("post", "单渠道探活（每次探活写一条探活记录，供渠道状态页历史）", pathParam("name"))["post"],
		},
		"/channels/{name}/sync-models": gin.H{
			"post": secured("post", "从上游拉取模型清单", append(pathParam("name"), dryRunParam))["post"],
		},
		"/lanes": gin.H{
			"get": secured("get", "车道列表", nil)["get"],
		},
		"/lanes/cleanup-members": gin.H{
			"post": secured("post", "清理渠道已不存在的悬空车道成员（成员清空的车道整条删除）", []gin.H{dryRunParam})["post"],
		},
		"/lanes/{name}": gin.H{
			"get":    secured("get", "车道详情", pathParam("name"))["get"],
			"put":    secured("put", "车道全量 upsert", append(pathParam("name"), dryRunParam))["put"],
			"delete": secured("delete", "删除车道", pathParam("name"))["delete"],
		},
		"/lanes/{name}/members": gin.H{
			"put": secured("put", "仅替换成员列表", append(pathParam("name"), dryRunParam))["put"],
		},
		"/lanes/{name}/probe": gin.H{
			"post": secured("post", "逐成员探活", pathParam("name"))["post"],
		},
		"/lanes/{name}/health": gin.H{
			"get": secured("get", "冷却/熔断/亲和快照", pathParam("name"))["get"],
		},
		"/lanes/{name}/circuits/reset": gin.H{
			"post": secured("post", "清除该车道熔断与冷却", pathParam("name"))["post"],
		},
		"/member-groups": gin.H{
			"get":  secured("get", "成员组列表（含 referenced_by）", nil)["get"],
			"post": secured("post", "创建成员组", []gin.H{dryRunParam})["post"],
		},
		"/member-groups/{name}": gin.H{
			"get": secured("get", "成员组详情（含组内成员与引用车道）", pathParam("name"))["get"],
			"put": secured("put", "成员组全量 upsert（members 省略=保留；改动对引用车道立即生效）",
				append(pathParam("name"), dryRunParam))["put"],
			"delete": secured("delete", "删除组并级联清理全部车道上的引用（链空车道整条删除）",
				pathParam("name"))["delete"],
		},
		"/keys": gin.H{
			"get":  secured("get", "客户端密钥列表", nil)["get"],
			"post": secured("post", "创建客户端密钥（响应含一次性明文）", nil)["post"],
		},
		"/keys/export": gin.H{
			"get": secured("get", "导出客户端密钥（CSV，全量；不含明文，只含 key_prefix 与策略）", nil)["get"],
		},
		"/keys/{name}": gin.H{
			"get":    secured("get", "密钥详情（无明文）", pathParam("name"))["get"],
			"put":    secured("put", "更新权限/限流/备注", append(pathParam("name"), dryRunParam))["put"],
			"delete": secured("delete", "删除密钥", pathParam("name"))["delete"],
		},
		"/keys/{name}/rotate": gin.H{
			"post": secured("post", "轮换（响应含新明文一次）", pathParam("name"))["post"],
		},
		"/logs": gin.H{
			"get": secured("get", "请求日志（W5）",
				[]gin.H{queryParam("lane", "车道过滤"),
					queryParam("channel", "渠道过滤"),
					queryParam("key", "客户端密钥名过滤"),
					queryParam("model", "请求模型过滤"),
					queryParam("client", "客户端过滤"),
					queryParam("session", "session_key 或完整 session_id"),
					queryParam("success", "true|false"),
					queryParam("masked", "true|false（有/无脱敏命中）"),
					queryParam("context", "true|false（有/无上下文元数据）"),
					queryParam("since", "起始时间"),
					queryParam("until", "结束时间"),
					queryParam("page", "页码（偏移分页）"),
					queryParam("page_size", "每页条数（上限 100）"),
					queryParam("cursor", "游标"),
					queryParam("limit", "游标分页条数")})["get"],
		},
		"/logs/{id}": gin.H{
			"get": secured("get", "单条日志（含 attempts 链与上下文分区，W5）", pathParam("id"))["get"],
		},
		"/logs/sessions": gin.H{
			"get": secured("get", "会话聚合（按 session_key；时间默认最近 30 天）",
				[]gin.H{queryParam("from", "起始时间（RFC3339 或 Unix 秒）"),
					queryParam("to", "结束时间（RFC3339 或 Unix 秒）"),
					queryParam("client", "客户端过滤"),
					queryParam("model", "请求模型过滤"),
					queryParam("lane", "车道过滤"),
					queryParam("q", "会话 id 子串（≤128）"),
					queryParam("sort", "last_ts|first_ts|requests|tokens|cost"),
					queryParam("order", "asc|desc"),
					queryParam("page", "页码（默认 1）"),
					queryParam("page_size", "每页条数（默认 50，上限 100）")})["get"],
		},
		"/logs/sessions/{key}": gin.H{
			"get": secured("get", "会话详情（摘要 + 请求时间线 + 上下文曲线）",
				append(pathParam("key"),
					queryParam("page", "页码（默认 1）"),
					queryParam("page_size", "每页条数（默认 50，上限 100）"),
					queryParam("order", "asc|desc（默认 desc）")))["get"],
		},
		"/logs/export": gin.H{
			"get": secured("get", "导出请求日志（CSV/NDJSON，≤31 天、≤100000 行；流式返回）",
				[]gin.H{queryParam("format", "csv|ndjson（默认 csv）"),
					queryParam("from", "起始时间（RFC3339，必填）"),
					queryParam("to", "结束时间（RFC3339，必填）"),
					queryParam("lane", "车道过滤（同 /api/logs）"),
					queryParam("channel", "渠道过滤（同 /api/logs）"),
					queryParam("key", "客户端密钥名过滤（同 /api/logs）"),
					queryParam("model", "模型过滤（同 /api/logs）"),
					queryParam("success", "true|false"),
					queryParam("masked", "true|false（有/无脱敏命中）"),
					queryParam("error_kind", "错误分类过滤")})["get"],
		},
		"/logs/prune": gin.H{
			"post": secured("post", "按需清理明细日志（before 省略则按 log_retention_days）",
				[]gin.H{dryRunParam, queryParam("before", "RFC3339 或 Unix 秒")})["post"],
		},
		"/privacy/settings": gin.H{
			"get": secured("get", "隐私脱敏设置（不含脱敏键）", nil)["get"],
			"put": secured("put", "全量替换隐私脱敏设置（dry-run 返回归一化设置与 diff）",
				[]gin.H{dryRunParam})["put"],
		},
		"/privacy/recommended": gin.H{
			"get": secured("get", "发布方推荐基线（总开关恒为关闭，只读）", nil)["get"],
		},
		"/privacy/model": gin.H{
			"get": secured("get", "本地脱敏模型状态（AstrLink Guard）", nil)["get"],
		},
		"/privacy/model/download": gin.H{
			"post": secured("post", "下载本地脱敏模型（外部动作，dry-run 拒绝）", nil)["post"],
		},
		"/privacy/preview": gin.H{
			"post": secured("post", "预览一段文本的脱敏结果（不落库、不登记仓库）", nil)["post"],
		},
		"/privacy/records": gin.H{
			"get": secured("get", "脱敏请求记录（分页；只含命中元数据）",
				[]gin.H{queryParam("from", "起始时间（RFC3339 或 Unix 秒）"),
					queryParam("to", "结束时间（RFC3339 或 Unix 秒）"),
					queryParam("kind", "命中类型过滤"),
					queryParam("by", "命中来源过滤（rule|yours|word|model|known）"),
					queryParam("action", "动作过滤（warn|block）"),
					queryParam("blocked", "true|false"),
					queryParam("client", "客户端过滤"),
					queryParam("model", "请求模型过滤"),
					queryParam("page", "页码（默认 1）"),
					queryParam("page_size", "每页条数（默认 50，上限 100）")})["get"],
		},
		"/privacy/summary": gin.H{
			"get": secured("get", "脱敏汇总（最近 1–30 天）", []gin.H{queryParam("days", "1–30，默认 7")})["get"],
		},
		"/stats": gin.H{
			"get": secured("get", "聚合统计（W5）", nil)["get"],
		},
		"/stats/rebuild": gin.H{
			"post": secured("post", "按当前口径重建小时聚合表并按当前单价重折算明细成本（幂等）",
				[]gin.H{dryRunParam})["post"],
		},
		"/route-events": gin.H{
			"get": secured("get", "SSE 车道运行态（W4）", nil)["get"],
		},
		"/audit": gin.H{
			"get": secured("get", "变更审计", nil)["get"],
		},
		"/audit/export": gin.H{
			"get": secured("get", "导出审计（CSV/NDJSON，≤31 天、≤100000 行；流式返回）",
				[]gin.H{queryParam("format", "csv|ndjson（默认 csv）"),
					queryParam("from", "起始时间（RFC3339，必填）"),
					queryParam("to", "结束时间（RFC3339，必填）"),
					queryParam("action", "动作过滤"),
					queryParam("resource", "资源过滤"),
					queryParam("name", "对象名过滤")})["get"],
		},
		"/export": gin.H{
			"get": secured("get", "导出完整配置（不含密钥明文与哈希）", nil)["get"],
		},
		"/import": gin.H{
			"post": secured("post", "导入配置", []gin.H{dryRunParam})["post"],
		},
		"/models": gin.H{
			"get": secured("get", "全部可路由模型名", nil)["get"],
		},
		"/routes/{model}": gin.H{
			"get": secured("get", "解析某模型的成员链", pathParam("model"))["get"],
		},
		"/lane-summaries": gin.H{
			"get": secured("get", "全部车道的成员顺序摘要（不分页）", nil)["get"],
		},
		"/model-metadata": gin.H{
			"get": secured("get", "模型目录元数据（不分页；含仅由渠道声明的模型）", nil)["get"],
		},
		"/model-metadata/{model}": gin.H{
			"put":    secured("put", "写入模型目录元数据（全量 upsert）", append(pathParam("model"), dryRunParam))["put"],
			"delete": secured("delete", "删除模型目录记录（可 remove_from_channels / force）", pathParam("model"))["delete"],
		},

		// 渠道批量运维（api-spec §5.3.1）。响应为裸资源：动作类返回语义化最小对象
		// （如 {"changed":n}），列表返回 {items,next_cursor}。
		"/channels/batch/status": gin.H{
			"post": secured("post", "批量启用/停用渠道：body {channels:[name]} 或 {ids:[int]} 二选一 + status；成功 {changed:n}", nil)["post"],
		},
		"/channels/batch/tag": gin.H{
			"post": secured("post", "批量设置渠道标签：body {channels:[name]} 或 {ids:[int]} + tag；成功 {changed:n}", nil)["post"],
		},
		"/channels/batch/copy": gin.H{
			"post": secured("post", "复制渠道：body {channel:name,suffix?,reset_balance?}；成功为写后回读的渠道对象", nil)["post"],
		},
		"/channels/batch/fetch-models": gin.H{
			"post": secured("post", "拉取上游模型清单（不落库）：body {channel:name} / {channel_id} / {base_url,key,type}；成功 {models:[...]}", nil)["post"],
		},
		"/channels/by-tag": gin.H{
			"put": secured("put", "按标签批量改渠道配置；成功 {tag,updated:true}", nil)["put"],
		},
		"/channels/by-tag/status": gin.H{
			"post": secured("post", "按标签批量启用/停用渠道：body {tag,status}；成功 {tag,enabled:bool}", nil)["post"],
		},
		"/channels/by-tag/models": gin.H{
			"get": secured("get", "按标签取模型清单", []gin.H{queryParam("tag", "标签")})["get"],
		},
		"/channels/disabled": gin.H{
			"delete": secured("delete", "删除全部已停用渠道；成功 {deleted:n}，被车道引用时 409 + details.blocked", []gin.H{dryRunParam})["delete"],
		},
		"/channels/upstream-updates/detect-all": gin.H{
			"post": secured("post", "探测全部渠道的上游模型变更", nil)["post"],
		},
		"/channels/upstream-updates/apply-all": gin.H{
			"post": secured("post", "应用全部渠道的上游模型变更", nil)["post"],
		},
		"/channels/{name}/key": gin.H{
			"get": secured("get", "读取渠道上游密钥（运维排障）；成功 {key: 明文}", pathParam("name"))["get"],
		},
		"/channels/{name}/multi-keys": gin.H{
			"post": secured("post", "多密钥管理：body {action,...}；get_key_status 成功为分页对象，其余 {applied:true,message}", pathParam("name"))["post"],
		},
		"/channels/{name}/upstream-updates/detect": gin.H{
			"post": secured("post", "探测该渠道的上游模型变更", pathParam("name"))["post"],
		},
		"/channels/{name}/upstream-updates/apply": gin.H{
			"post": secured("post", "应用该渠道的上游模型变更（车道引用守卫 + force）", append(pathParam("name"), forceParam))["post"],
		},
		"/channels/{name}/codex/refresh": gin.H{
			"post": secured("post", "刷新 Codex 渠道凭据", pathParam("name"))["post"],
		},
		"/channels/{name}/codex/usage": gin.H{
			"get": secured("get", "Codex 用量；成功 {upstream_status,body}，上游非 2xx → 502", pathParam("name"))["get"],
		},
		"/channels/{name}/codex/reset-credits": gin.H{
			"get": secured("get", "Codex 限额重置额度", pathParam("name"))["get"],
		},
		"/channels/{name}/codex/reset": gin.H{
			"post": secured("post", "重置 Codex 用量", pathParam("name"))["post"],
		},
		"/channels/{name}/ollama/pull": gin.H{
			"post": secured("post", "Ollama 拉取模型（非流式）：body {model_name}；成功 {channel,model,pulled:true}", pathParam("name"))["post"],
		},
		"/channels/{name}/ollama/pull/stream": gin.H{
			"post": secured("post", "Ollama 拉取模型（SSE 进度，text/event-stream，不套信封）", pathParam("name"))["post"],
		},
		"/channels/{name}/ollama/models": gin.H{
			"delete": secured("delete", "Ollama 删除模型", pathParam("name"))["delete"],
		},
		"/channels/{name}/ollama/version": gin.H{
			"get": secured("get", "Ollama 版本；成功 {channel,version}", pathParam("name"))["get"],
		},

		// 系统选项、任务与性能（api-spec §5.3.2）
		"/system/options/all": gin.H{
			"get": secured("get", "完整系统选项（站点/内容/运维）；成功 {items:[{key,value}]}", nil)["get"],
			"put": secured("put", "更新单个系统选项：body {key,value}；成功 {key,updated:true}", nil)["put"],
		},
		"/system-tasks": gin.H{
			"get": secured("get", "系统任务列表", []gin.H{queryParam("cursor", "上一页返回的 next_cursor"), queryParam("limit", "默认 50，上限 200")})["get"],
		},
		"/system-tasks/current": gin.H{
			"get": secured("get", "当前运行中的任务（必须带 ?type=）；成功 {task:<对象或 null>}", nil)["get"],
		},
		"/system-tasks/{id}": gin.H{
			"get": secured("get", "单任务详情", pathParam("id"))["get"],
		},
		"/system-tasks/log-cleanup": gin.H{
			"post": secured("post", "创建清理日志文件任务", nil)["post"],
		},
		"/system/performance": gin.H{
			"get": secured("get", "性能统计", nil)["get"],
		},
		"/system/performance/reset": gin.H{
			"post": secured("post", "重置性能统计；成功 {reset:true}", nil)["post"],
		},
		"/system/performance/gc": gin.H{
			"post": secured("post", "强制 GC；成功 {collected:true}", nil)["post"],
		},
		"/system/performance/disk-cache": gin.H{
			"delete": secured("delete", "清除磁盘缓存；成功 {cleared:true}", nil)["delete"],
		},
		"/system/log-files": gin.H{
			"get":    secured("get", "日志文件列表", nil)["get"],
			"delete": secured("delete", "清理日志文件", nil)["delete"],
		},

		// 预填组（api-spec §5.3.3）
		"/prefill-groups": gin.H{
			"get":  secured("get", "预填组列表", nil)["get"],
			"post": secured("post", "创建预填组", nil)["post"],
		},
		"/prefill-groups/{id}": gin.H{
			"put":    secured("put", "更新预填组", pathParam("id"))["put"],
			"delete": secured("delete", "删除预填组", pathParam("id"))["delete"],
		},

		// 模型目录运维（api-spec §5.3.4）
		"/model-catalog/sync-upstream/preview": gin.H{
			"get": secured("get", "预览上游同步的模型变更（不落库）", nil)["get"],
		},
		"/model-catalog/sync-upstream": gin.H{
			"post": secured("post", "应用上游同步", nil)["post"],
		},
		"/model-catalog/missing": gin.H{
			"get": secured("get", "渠道声明但无目录记录的模型；成功 {models:[...]}", nil)["get"],
		},
		"/model-catalog/batch-delete": gin.H{
			"post": secured("post", "批量删除目录记录：body {models:[name]} 或 {model_ids:[int]} 二选一；成功 {deleted_count,updated_channels}", nil)["post"],
		},
	}
}
