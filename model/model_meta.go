package model

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"

	"gorm.io/gorm"
)

const (
	NameRuleExact = iota
	NameRulePrefix
	NameRuleContains
	NameRuleSuffix
)

type BoundChannel struct {
	Name string `json:"name"`
	Type int    `json:"type"`
}

type Model struct {
	Id                 int            `json:"id"`
	ModelName          string         `json:"model_name" gorm:"size:128;not null;uniqueIndex:uk_model_name_delete_at,priority:1"`
	Description        string         `json:"description,omitempty" gorm:"type:text"`
	Icon               string         `json:"icon,omitempty" gorm:"type:varchar(128)"`
	Tags               string         `json:"tags,omitempty" gorm:"type:varchar(255)"`
	Endpoints          string         `json:"endpoints,omitempty" gorm:"type:text"`
	SupportedEndpoints []string       `json:"supported_endpoints,omitempty" gorm:"-"`
	Status             int            `json:"status" gorm:"default:1"`
	SyncOfficial       int            `json:"sync_official" gorm:"default:1"`
	CreatedTime        int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime        int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt          gorm.DeletedAt `json:"-" gorm:"index;uniqueIndex:uk_model_name_delete_at,priority:2"`

	BoundChannels []BoundChannel `json:"bound_channels,omitempty" gorm:"-"`
	EnableGroups  []string       `json:"enable_groups,omitempty" gorm:"-"`
	QuotaTypes    []int          `json:"quota_types,omitempty" gorm:"-"`
	NameRule      int            `json:"name_rule" gorm:"default:0"`

	MatchedModels []string `json:"matched_models,omitempty" gorm:"-"`
	MatchedCount  int      `json:"matched_count,omitempty" gorm:"-"`

	HasMetadata            bool `json:"has_metadata" gorm:"-"`
	ConfiguredChannelCount int  `json:"configured_channel_count" gorm:"-"`
}

// MatchesName applies a metadata rule to a concrete channel model name.
func (mi *Model) MatchesName(name string) bool {
	switch mi.NameRule {
	case NameRulePrefix:
		return strings.HasPrefix(name, mi.ModelName)
	case NameRuleSuffix:
		return strings.HasSuffix(name, mi.ModelName)
	case NameRuleContains:
		return strings.Contains(name, mi.ModelName)
	default:
		return name == mi.ModelName
	}
}

// resolveModelMetadata preserves catalog precedence: exact, prefix, suffix,
// then contains. The first matching record within a rule type wins.
func resolveModelMetadata(records []Model, names []string) map[string]*Model {
	resolved := make(map[string]*Model)
	for i := range records {
		if records[i].NameRule == NameRuleExact {
			resolved[records[i].ModelName] = &records[i]
		}
	}
	for _, rule := range []int{NameRulePrefix, NameRuleSuffix, NameRuleContains} {
		for i := range records {
			metadata := &records[i]
			if metadata.NameRule != rule {
				continue
			}
			for _, name := range names {
				if _, exists := resolved[name]; !exists && metadata.MatchesName(name) {
					resolved[name] = metadata
				}
			}
		}
	}
	return resolved
}

// GetConfiguredModelChannels includes disabled channels and reads no credentials.
func GetConfiguredModelChannels() (map[string][]int, error) {
	var channels []Channel
	if err := DB.Select("id", "models").Find(&channels).Error; err != nil {
		return nil, err
	}
	configured := make(map[string][]int)
	for _, channel := range channels {
		for _, name := range normalizeLookupValues(channel.GetModels()) {
			configured[name] = append(configured[name], channel.Id)
		}
	}
	return configured, nil
}

// SearchModelsWithChannels augments metadata with concrete configured names.
// Synthetic rows never persist and never affect the public pricing catalog.
func SearchModelsWithChannels(keyword, status, syncOfficial string, offset, limit int) ([]*Model, int64, error) {
	records, _, err := SearchModels(keyword, status, syncOfficial, 0, -1)
	if err != nil {
		return nil, 0, err
	}
	_, filterStatus := parseModelStatusFilter(status)
	_, filterSync := parseModelSyncFilter(syncOfficial)
	if !filterStatus && !filterSync {
		configured, err := GetConfiguredModelChannels()
		if err != nil {
			return nil, 0, err
		}
		var exactNames []string
		if err := DB.Model(&Model{}).Where("name_rule = ?", NameRuleExact).Pluck("model_name", &exactNames).Error; err != nil {
			return nil, 0, err
		}
		for _, name := range exactNames {
			delete(configured, name)
		}
		names := make([]string, 0, len(configured))
		for name := range configured {
			if keyword == "" || strings.Contains(strings.ToLower(name), strings.ToLower(keyword)) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			records = append(records, &Model{ModelName: name, NameRule: NameRuleExact})
		}
	}
	total := len(records)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []*Model{}, int64(total), nil
	}
	end := total
	if limit >= 0 && limit < total-offset {
		end = offset + limit
	}
	return records[offset:end], int64(total), nil
}

func (mi *Model) Insert() error {
	return metadataTransaction(func(tx *gorm.DB) error {
		now := common.GetTimestamp()
		mi.CreatedTime, mi.UpdatedTime = now, now
		status, syncOfficial := mi.Status, mi.SyncOfficial
		if err := tx.Create(mi).Error; err != nil {
			return err
		}
		mi.Status, mi.SyncOfficial = status, syncOfficial
		return tx.Model(&Model{}).Where("id = ?", mi.Id).Updates(map[string]any{"status": status, "sync_official": syncOfficial}).Error
	})
}

func IsModelNameDuplicated(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&Model{}).Where("model_name = ? AND id <> ?", name, id).Count(&cnt).Error
	return cnt > 0, err
}

func (mi *Model) Update() error {
	return metadataTransaction(func(tx *gorm.DB) error {
		mi.UpdatedTime = common.GetTimestamp()
		return tx.Model(&Model{}).Where("id = ?", mi.Id).
			Select("model_name", "description", "icon", "tags", "endpoints", "status", "sync_official", "name_rule", "updated_time").Updates(mi).Error
	})
}

func (mi *Model) Delete() error {
	_, err := DeleteModelMetadata([]int{mi.Id}, false, false)
	return err
}

type ModelDeleteResult struct {
	DeletedCount    int `json:"deleted_count"`
	UpdatedChannels int `json:"updated_channels"`
}

// DeleteModelMetadata optionally removes exact model names from every channel.
// Channel removal requires exact-match metadata records. Pricing removal
// clears the selected names without expanding metadata matching rules.
// LaneReferenceError 表示"要移除的模型仍被车道引用"，携带 渠道名 → 车道名 清单。
// 调用方据此返回 conflict 包络，避免"模型已删、路由页仍显示可调用"的静默不一致。
type LaneReferenceError struct {
	Blocked map[string][]string
}

func (e *LaneReferenceError) Error() string {
	names := make([]string, 0, len(e.Blocked))
	for name := range e.Blocked {
		names = append(names, name)
	}
	sort.Strings(names)
	return "models still referenced by lanes on channels: " + strings.Join(names, ", ")
}

func DeleteModelMetadata(ids []int, removeFromChannels, removePricing bool) (ModelDeleteResult, error) {
	result := ModelDeleteResult{}
	if len(ids) == 0 || len(ids) > 1000 {
		return result, errors.New("select between 1 and 1000 models")
	}
	selected := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return result, errors.New("invalid model ID")
		}
		selected[id] = struct{}{}
	}
	modelIDs := make([]int, 0, len(selected))
	for id := range selected {
		modelIDs = append(modelIDs, id)
	}
	sort.Ints(modelIDs)
	names := make(map[string]struct{}, len(modelIDs))
	deleteRecords := func(tx *gorm.DB) error {
		var records []Model
		if err := lockForUpdate(tx).Where("id IN ?", modelIDs).Order("id").Find(&records).Error; err != nil {
			return err
		}
		if len(records) != len(modelIDs) {
			return errors.New("selected models changed; reload before deleting")
		}
		for _, record := range records {
			if removeFromChannels && record.NameRule != NameRuleExact {
				return errors.New("only exact-match models can be removed from channels")
			}
			names[record.ModelName] = struct{}{}
		}
		if removeFromChannels {
			var channels []Channel
			// Read only routing fields. Lock channels in a consistent order, then
			// update models and abilities in the same transaction as metadata.
			if err := lockForUpdate(tx).Select("id", "models", "status", "group", "tag").Order("id").Find(&channels).Error; err != nil {
				return err
			}
			// 车道引用守卫：被移除的路由键若命中同名车道且该车道有该渠道成员，默认拒绝，
			// 避免"模型已删、路由页仍显示可调用"的静默不一致（与渠道编辑/sync-models 同口径）。
			blocked := map[string][]string{}
			for _, channel := range channels {
				removedHere := make([]string, 0)
				for _, name := range channel.GetModels() {
					if trimmed := strings.TrimSpace(name); trimmed != "" {
						if _, remove := names[trimmed]; remove {
							removedHere = append(removedHere, trimmed)
						}
					}
				}
				if len(removedHere) == 0 {
					continue
				}
				refs, refErr := removedModelLaneRefsWith(tx, channel.Id, removedHere)
				if refErr != nil {
					return refErr
				}
				if len(refs) > 0 {
					blocked[channel.Name] = refs
				}
			}
			if len(blocked) > 0 {
				return &LaneReferenceError{Blocked: blocked}
			}
			for _, channel := range channels {
				models := channel.GetModels()
				remaining := make([]string, 0, len(models))
				for _, name := range models {
					if _, remove := names[strings.TrimSpace(name)]; !remove {
						remaining = append(remaining, name)
					}
				}
				if len(remaining) == len(models) {
					continue
				}
				channel.Models = strings.Join(remaining, ",")
				if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Update("models", channel.Models).Error; err != nil {
					return err
				}
				if err := channel.UpdateAbilities(tx); err != nil {
					return err
				}
				result.UpdatedChannels++
			}
		}
		if err := tx.Where("id IN ?", modelIDs).Delete(&Model{}).Error; err != nil {
			return err
		}
		result.DeletedCount = len(records)
		return nil
	}
	var err error
	if removePricing {
		// Use the pricing mutation path so both option persistence and runtime
		// publication stay serialized with ordinary pricing saves. All database
		// writes share one transaction; runtime prices publish only after commit.
		metadataMutationMu.Lock()
		defer metadataMutationMu.Unlock()
		err = mutateModelPricingOptions(func(tx *gorm.DB, values map[string]map[string]any) error {
			if err := lockMetadataMutation(tx); err != nil {
				return err
			}
			if err := deleteRecords(tx); err != nil {
				return err
			}
			for _, entries := range values {
				for name := range names {
					delete(entries, name)
				}
			}
			return nil
		})
	} else {
		err = metadataTransaction(deleteRecords)
	}
	if err != nil {
		return ModelDeleteResult{}, err
	}
	if result.UpdatedChannels > 0 {
		InitChannelCache()
	}
	RefreshPricing()
	return result, nil
}

func GetAllModels(offset int, limit int) ([]*Model, error) {
	models, _, err := SearchModels("", "", "", offset, limit)
	return models, err
}

// ModelConnection describes an enabled route independently of catalog visibility or price.
type ModelConnection struct {
	AbilityWithChannel
	ChannelName string `json:"channel_name"`
}

func GetModelConnections() ([]ModelConnection, error) {
	var connections []ModelConnection
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type, channels.name as channel_name").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.enabled = ? AND channels.status = ?", true, common.ChannelStatusEnabled).
		Order("abilities.model, abilities.channel_id").
		Scan(&connections).Error
	return connections, err
}

func normalizeLookupValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func GetPreferredModelOwnerChannelTypes(modelNames []string, groups []string) (map[string]int, error) {
	result := make(map[string]int)
	modelNames = normalizeLookupValues(modelNames)
	if len(modelNames) == 0 {
		return result, nil
	}

	type row struct {
		Model       string
		ChannelType int
	}
	var rows []row

	query := DB.Table("abilities").
		Select("abilities.model as model, channels.type as channel_type").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ? AND channels.status = ?", modelNames, true, common.ChannelStatusEnabled).
		Order("COALESCE(abilities.priority, 0) DESC").
		Order("abilities.weight DESC").
		Order("abilities.channel_id ASC")

	groups = normalizeLookupValues(groups)
	if len(groups) > 0 {
		query = query.Where("abilities."+commonGroupCol+" IN ?", groups)
	}

	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, r := range rows {
		if _, ok := result[r.Model]; ok {
			continue
		}
		result[r.Model] = r.ChannelType
	}
	return result, nil
}

func SearchModels(keyword string, status string, syncOfficial string, offset int, limit int) ([]*Model, int64, error) {
	var models []*Model
	db := DB.Model(&Model{})
	if keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("model_name LIKE ? OR description LIKE ? OR tags LIKE ?", like, like, like)
	}
	if statusValue, ok := parseModelStatusFilter(status); ok {
		db = db.Where("models.status = ?", statusValue)
	}
	if syncValue, ok := parseModelSyncFilter(syncOfficial); ok {
		db = db.Where("models.sync_official = ?", syncValue)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := db.Order("models.id DESC").Offset(offset).Limit(limit).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	return models, total, nil
}

// parseModelStatusFilter maps UI/API status values to the models.status column.
// Returns ok=false when no status filter should be applied.
func parseModelStatusFilter(status string) (value int, ok bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "all":
		return 0, false
	case "enabled", "1":
		return 1, true
	case "disabled", "0":
		return 0, true
	default:
		n, err := strconv.Atoi(status)
		if err != nil {
			return 0, false
		}
		return n, true
	}
}

// parseModelSyncFilter maps UI/API sync values to the models.sync_official column.
// Returns ok=false when no sync filter should be applied.
func parseModelSyncFilter(syncOfficial string) (value int, ok bool) {
	switch strings.ToLower(strings.TrimSpace(syncOfficial)) {
	case "", "all":
		return 0, false
	case "yes", "1":
		return 1, true
	case "no", "0":
		return 0, true
	default:
		n, err := strconv.Atoi(syncOfficial)
		if err != nil {
			return 0, false
		}
		return n, true
	}
}
