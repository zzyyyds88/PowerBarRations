package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"

	"github.com/gin-gonic/gin"
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
	// BaseURL 与 Proxy 用非指针：导出总是给出确定值（空串即"无"），
	// 导入侧据此覆盖；要"保持原值"必须显式省略整个字段（见 §5.6 的缺席/空值语义）。
	BaseURL       string   `json:"base_url"`
	Models        []string `json:"models"`
	ParamOverride any      `json:"param_override,omitempty"`
	Enabled       bool     `json:"enabled"`
	Proxy         string   `json:"proxy,omitempty"`
	// Prices 渠道级上游单价（成本折算）；空表导出为 []，与"缺席=保持原值"区分。
	Prices []dto.ChannelModelPrice `json:"prices"`
	// ModelMapping 路由键 → 上游真名；空表导出为 {}（ADR 0005）。
	ModelMapping map[string]string `json:"model_mapping"`
	KeySet       bool              `json:"key_set"`
}

// LaneMemberConfig 车道成员的可导出形态。
type LaneMemberConfig struct {
	Channel       string `json:"channel"`
	UpstreamModel string `json:"upstream_model"`
	PublicAlias   string `json:"public_alias,omitempty"`
	Priority      int    `json:"priority"`
	Overrides     any    `json:"overrides,omitempty"`
}

// LaneConfig 显式车道的可导出形态。
type LaneConfig struct {
	Name         string                `json:"name"`
	Enabled      bool                  `json:"enabled"`
	Mode         string                `json:"mode"`
	ActiveMember string                `json:"active_member,omitempty"`
	Config       model.LaneRelayConfig `json:"config"`
	Members      []LaneMemberConfig    `json:"members"`
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
	Version    string            `json:"version"`
	ExportedAt string            `json:"exported_at,omitempty"`
	Channels   []ChannelConfig   `json:"channels"`
	Lanes      []LaneConfig      `json:"lanes"`
	ClientKeys []ClientKeyConfig `json:"client_keys"`
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

	lanes, err := model.ListLanes()
	if err != nil {
		return nil, err
	}
	for i := range lanes {
		lane := &lanes[i]
		entry := LaneConfig{
			Name:         lane.Name,
			Enabled:      lane.Enabled,
			Mode:         lane.Mode,
			ActiveMember: lane.ActiveMember,
			Config:       model.ParseLaneRelayConfig(lane.Config),
		}
		for _, member := range lane.Members {
			channelName := ""
			if channel, err := model.GetChannelById(member.ChannelId, false); err == nil && channel != nil {
				channelName = channel.Name
			}
			entry.Members = append(entry.Members, LaneMemberConfig{
				Channel:       channelName,
				UpstreamModel: member.UpstreamModel,
				PublicAlias:   member.PublicAlias,
				Priority:      member.Priority,
				Overrides:     jsonObject(member.Overrides),
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
			"channels": newDiffList(),
			"lanes":    newDiffList(),
			"keys":     newDiffList(),
			"options":  gin.H{"changed": []string{}},
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
	for _, lane := range bundle.Lanes {
		if strings.TrimSpace(lane.Name) == "" {
			apierr.Validation(c, "lanes[].name is required")
			return
		}
		if !model.ValidLaneMode(orDefault(lane.Mode, model.LaneModeFailover)) {
			apierr.Validation(c, "invalid lane mode '"+lane.Mode+"'")
			return
		}
	}

	// 导入前剔除"成员指向不存在渠道"的悬空成员（跳过并警告，而非整包 422）。
	pruneMissingMemberChannels(&bundle, &result)

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
		BaseURL: &config.BaseURL,
		Enabled: &config.Enabled,
	}
	payload.Models = config.Models
	if config.ParamOverride != nil {
		encoded, _ := json.Marshal(config.ParamOverride)
		payload.ParamOverride = encoded
	}
	proxy := config.Proxy
	payload.Proxy = &proxy
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

// lanePayloadFromConfig 同理：车道的导出形态 → 写入 payload。
func lanePayloadFromConfig(lane LaneConfig) *lanePayload {
	payload := &lanePayload{
		Enabled:      &lane.Enabled,
		Mode:         orDefault(lane.Mode, model.LaneModeFailover),
		ActiveMember: lane.ActiveMember,
	}
	encodedConfig, _ := json.Marshal(lane.Config)
	payload.Config = encodedConfig
	for _, member := range lane.Members {
		entry := laneMemberPayload{
			Channel:       member.Channel,
			UpstreamModel: member.UpstreamModel,
			PublicAlias:   member.PublicAlias,
			Priority:      member.Priority,
		}
		if member.Overrides != nil {
			encoded, _ := json.Marshal(member.Overrides)
			entry.Overrides = encoded
		}
		payload.Members = append(payload.Members, entry)
	}
	return payload
}

// pruneMissingMemberChannels 剔除导入 bundle 里"成员指向不存在渠道"的成员，并记录警告。
//
// 导入是原子操作：此前一个悬空成员会让整包 422。现在改为跳过该成员（而不是整包失败），
// 并在 warnings / diff.skipped 里列出，让用户知情；成员被清空的启用车道改为停用
// （而不是整包拒绝），避免写入"启用但无成员"的非法车道。
func pruneMissingMemberChannels(bundle *ConfigBundle, result *ImportResult) {
	for i := range bundle.Lanes {
		lane := &bundle.Lanes[i]
		kept := make([]LaneMemberConfig, 0, len(lane.Members))
		for _, m := range lane.Members {
			name := strings.TrimSpace(m.Channel)
			if name == "" {
				continue
			}
			if _, err := findChannelByName(name); err != nil {
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
			lane.Enabled = false
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
	for _, channel := range bundle.Channels {
		found, findErr := findChannelByName(channel.Name)
		isCreate := findErr != nil
		payload := channelPayloadFromConfig(channel)
		if _, buildErr := buildChannel(channel.Name, found, payload, isCreate); buildErr != nil {
			return &apiError{status: http.StatusBadRequest, code: buildErr.code, message: buildErr.message}
		}
	}
	for _, lane := range bundle.Lanes {
		if _, buildErr := buildLane(lane.Name, lanePayloadFromConfig(lane)); buildErr != nil {
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
		case err != nil:
			result.add("channels", channel.Name)
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
// 两阶段：**先把所有对象构造并校验完**（不落库），任何一条不合法就整体拒绝；
// 校验通过后才逐条写入。否则中途失败会留下"改了一半"的半成品状态。
// applyImport 落库（与 planImport 使用同一套比较口径）。
//
// 两阶段：**先把所有对象构造并校验完**（不落库），任何一条不合法就整体拒绝；
// 校验通过后才逐条写入。否则中途失败会留下"改了一半"的半成品状态。
func applyImport(c *gin.Context, bundle *ConfigBundle, result *ImportResult) error {
	type channelPlan struct {
		name     string
		built    *model.Channel
		config   ChannelConfig
		isCreate bool
	}
	type lanePlan struct {
		name     string
		built    *model.Lane
		config   LaneConfig
		isCreate bool
	}

	channelPlans := make([]channelPlan, 0, len(bundle.Channels))
	for _, channel := range bundle.Channels {
		found, findErr := findChannelByName(channel.Name)
		isCreate := findErr != nil
		built, buildErr := buildChannel(channel.Name, found, channelPayloadFromConfig(channel), isCreate)
		if buildErr != nil {
			// 只返回错误；响应体由 PostImport → writeAPIError 写一次（审查 F6）。
			return &apiError{status: http.StatusBadRequest, code: buildErr.code, message: buildErr.message}
		}
		channelPlans = append(channelPlans, channelPlan{name: channel.Name, built: built, config: channel, isCreate: isCreate})
	}

	lanePlans := make([]lanePlan, 0, len(bundle.Lanes))
	for _, lane := range bundle.Lanes {
		built, buildErr := buildLane(lane.Name, lanePayloadFromConfig(lane))
		if buildErr != nil {
			return &apiError{status: buildErr.status, code: buildErr.code, message: buildErr.message, hint: buildErr.hint}
		}
		_, existingErr := model.GetLaneByName(lane.Name)
		lanePlans = append(lanePlans, lanePlan{name: lane.Name, built: built, config: lane, isCreate: existingErr != nil})
	}

	// 校验全部通过，开始落库

	for _, plan := range channelPlans {
		built := plan.built
		if plan.isCreate {
			built.CreatedTime = time.Now().Unix()
			built.UpdatedAt = built.CreatedTime
			if err := model.DB.Create(built).Error; err != nil {
				return err
			}
			_ = built.AddAbilities(nil)
			result.add("channels", plan.name)
			continue
		}
		built.UpdatedAt = time.Now().Unix()
		if err := model.DB.Model(&model.Channel{}).Where("id = ?", built.Id).
			Select("*").Omit("id", "created_time").Updates(built).Error; err != nil {
			return err
		}
		_ = built.UpdateAbilities(nil)
		if saved, findErr := findChannelByName(plan.name); findErr == nil &&
			channelDigestOfChannel(saved) == model.DigestOf(plan.config) {
			result.unchanged("channels", plan.name)
		} else {
			result.update("channels", plan.name)
		}
	}
	model.InitChannelCache()

	for _, plan := range lanePlans {
		if err := model.UpsertLane(plan.built); err != nil {
			return err
		}
		if plan.isCreate {
			result.add("lanes", plan.name)
			continue
		}
		if saved, findErr := model.GetLaneByName(plan.name); findErr == nil &&
			laneDigestOfLane(saved) == laneDigestOf(&plan.config) {
			result.unchanged("lanes", plan.name)
		} else {
			result.update("lanes", plan.name)
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
	for _, entry := range bundle.ClientKeys {
		if _, ok := keyByName[entry.Name]; !ok {
			// 导出不含明文/哈希，无法在目标实例上凭本文件新建凭据
			result.skip("client_keys", entry.Name)
			result.Warnings = append(result.Warnings,
				"client key '"+entry.Name+"' 不存在且配置里没有明文，已跳过（需用 POST /api/v1/keys 重建或导入原哈希）")
			continue
		}
		updated := &model.ClientKey{Name: entry.Name}
		if buildErr := applyClientKeyPayload(updated, clientKeyPayloadToConfig(entry)); buildErr != nil {
			apierr.Write(c, http.StatusBadRequest, buildErr.code, buildErr.message, "")
			return &apiError{code: buildErr.code, message: buildErr.message}
		}
		updated.Enabled = entry.Enabled
		if err := model.UpsertClientKey(updated); err != nil {
			return err
		}
		if saved, findErr := model.GetClientKeyByName(entry.Name); findErr == nil &&
			clientKeyDigestOf(saved) == model.DigestOf(entry) {
			result.unchanged("client_keys", entry.Name)
		} else {
			result.update("client_keys", entry.Name)
		}
	}

	// 未在 bundle 中给出 system_options 时保持原样：不能拿零值把关键词表清空
	if bundle.SystemOptions != nil {
		// 先取"改动前"快照：与 planImport 同一口径（before vs bundle）。
		// 若拿改动后的状态比，两者恒等，changed 永远是空的。
		beforeOptions := currentSystemOptions()
		if err := applySystemOptions(*bundle.SystemOptions); err != nil {
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
	return ChannelConfig{
		Name:          channel.Name,
		Type:          ChannelTypeSlug(channel.Type),
		BaseURL:       channel.GetBaseURL(),
		Models:        models,
		ParamOverride: jsonObject(derefString(channel.ParamOverride)),
		Enabled:       channel.Status == common.ChannelStatusEnabled,
		Proxy:         channel.GetSetting().Proxy,
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
	if copyLane.Config == (model.LaneRelayConfig{}) {
		copyLane.Config = model.DefaultLaneRelayConfig()
	} else {
		copyLane.Config = copyLane.Config.Normalize()
	}
	if copyLane.Members == nil {
		copyLane.Members = []LaneMemberConfig{}
	}
	return model.DigestOf(copyLane)
}

func laneDigestOfLane(lane *model.Lane) string {
	config := LaneConfig{
		Name:         lane.Name,
		Enabled:      lane.Enabled,
		Mode:         lane.Mode,
		ActiveMember: lane.ActiveMember,
		Config:       model.ParseLaneRelayConfig(lane.Config),
		Members:      []LaneMemberConfig{},
	}
	for _, member := range lane.Members {
		channelName := ""
		if channel, err := model.GetChannelById(member.ChannelId, false); err == nil && channel != nil {
			channelName = channel.Name
		}
		config.Members = append(config.Members, LaneMemberConfig{
			Channel:       channelName,
			UpstreamModel: member.UpstreamModel,
			PublicAlias:   member.PublicAlias,
			Priority:      member.Priority,
			Overrides:     jsonObject(member.Overrides),
		})
	}
	return laneDigestOf(&config)
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
		"/channels/{name}": gin.H{
			"get":    secured("get", "渠道详情", pathParam("name"))["get"],
			"put":    secured("put", "渠道全量 upsert（key 只写不读）", append(pathParam("name"), dryRunParam))["put"],
			"delete": secured("delete", "删除渠道", pathParam("name"))["delete"],
		},
		"/channels/{name}/test": gin.H{
			"post": secured("post", "单渠道探活", pathParam("name"))["post"],
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
		"/keys": gin.H{
			"get":  secured("get", "客户端密钥列表", nil)["get"],
			"post": secured("post", "创建客户端密钥（响应含一次性明文）", nil)["post"],
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
			"get": secured("get", "请求日志（W5）", nil)["get"],
		},
		"/logs/{id}": gin.H{
			"get": secured("get", "单条日志（含 attempts 链，W5）", pathParam("id"))["get"],
		},
		"/logs/prune": gin.H{
			"post": secured("post", "按需清理明细日志（before 省略则按 log_retention_days）",
				[]gin.H{dryRunParam, queryParam("before", "RFC3339 或 Unix 秒")})["post"],
		},
		"/stats": gin.H{
			"get": secured("get", "聚合统计（W5）", nil)["get"],
		},
		"/route-events": gin.H{
			"get": secured("get", "SSE 车道运行态（W4）", nil)["get"],
		},
		"/audit": gin.H{
			"get": secured("get", "变更审计", nil)["get"],
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
	}
}
