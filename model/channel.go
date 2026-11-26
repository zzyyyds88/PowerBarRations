package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/events"
	"github.com/zzyyyds88/PowerBarRations/logger"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type" gorm:"default:0"`
	Key                string  `json:"key" gorm:"not null"`
	OpenAIOrganization *string `json:"openai_organization"`
	TestModel          *string `json:"test_model"`
	Status             int     `json:"status" gorm:"default:1"`
	Name               string  `json:"name" gorm:"index"`
	CreatedTime        int64   `json:"created_time" gorm:"bigint"`
	// UpdatedAt 最后修改时间（api-spec §4.1 要求 created_at / updated_at 同时返回）。
	// 基座没有该列，此前响应里的 updated_at 直接复用了创建时间，导致任何依赖它
	// 做变更检测或缓存失效的调用方永远看不到渠道更新。
	UpdatedAt          int64   `json:"updated_at" gorm:"bigint"`
	TestTime           int64   `json:"test_time" gorm:"bigint"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other              string  `json:"other"`
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time" gorm:"bigint"`
	Models             string  `json:"models"`
	Group              string  `json:"group" gorm:"type:varchar(64);default:'default'"`
	UsedQuota          int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping       *string `json:"model_mapping" gorm:"type:text"`
	//MaxInputTokens     *int    `json:"max_input_tokens" gorm:"default:0"`
	StatusCodeMapping *string `json:"status_code_mapping" gorm:"type:varchar(1024);default:''"`
	AutoBan           *int    `json:"auto_ban" gorm:"default:1"`
	OtherInfo         string  `json:"other_info"`
	Tag               *string `json:"tag" gorm:"index"`
	Setting           *string `json:"setting" gorm:"type:text"` // 渠道额外设置
	ParamOverride     *string `json:"param_override" gorm:"type:text"`
	HeaderOverride    *string `json:"header_override" gorm:"type:text"`
	Remark            *string `json:"remark" gorm:"type:varchar(255)" validate:"max=255"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info" gorm:"type:json"`

	OtherSettings string `json:"settings" gorm:"column:settings"` // 其他设置，存储azure版本等不需要检索的信息，详见dto.ChannelOtherSettings

	// cache info
	Keys []string `json:"-" gorm:"-"`

	// settingsSnapshot 是请求级渠道快照的纯解析结果。只在 SnapshotChannel 系列调用中
	// 写入；普通数据库对象保持未初始化，getter 仍可无副作用地即时解析。
	settingsSnapshot      dto.ChannelSettings
	otherSettingsSnapshot dto.ChannelOtherSettings
	settingsSnapshotReady bool
	settingsSnapshotErr   *ChannelConfigError
}

type ChannelInfo struct {
	IsMultiKey             bool                  `json:"is_multi_key"`                        // 是否多Key模式
	MultiKeySize           int                   `json:"multi_key_size"`                      // 多Key模式下的Key数量
	MultiKeyStatusList     map[int]int           `json:"multi_key_status_list"`               // key状态列表，key index -> status
	MultiKeyDisabledReason map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key禁用原因列表，key index -> reason
	MultiKeyDisabledTime   map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key禁用时间列表，key index -> time
	MultiKeyPollingIndex   int                   `json:"multi_key_polling_index"`             // 多Key模式下轮询的key索引
	MultiKeyMode           constant.MultiKeyMode `json:"multi_key_mode"`
}

type ChannelSortOptions struct {
	SortBy    string
	SortOrder string
	IDSort    bool
}

// 渠道 priority/weight 列已删除：排序字段不再包含 priority，默认按 id 降序。
var channelSortColumns = map[string]string{
	"id":            "id",
	"name":          "name",
	"balance":       "balance",
	"response_time": "response_time",
	"test_time":     "test_time",
}

func NewChannelSortOptions(sortBy string, sortOrder string, idSort bool) ChannelSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := channelSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = ""
		normalizedSortOrder = ""
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}

	return ChannelSortOptions{
		SortBy:    normalizedSortBy,
		SortOrder: normalizedSortOrder,
		IDSort:    idSort,
	}
}

func (options ChannelSortOptions) Apply(query *gorm.DB) *gorm.DB {
	if columnName, ok := channelSortColumns[options.SortBy]; ok {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: columnName},
			Desc:   options.SortOrder != "asc",
		})
	}
	if options.IDSort {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: "id"},
			Desc:   true,
		})
	}
	return query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: "id"},
		Desc:   true,
	})
}

func resolveChannelSortOptions(idSort bool, sortOptions []ChannelSortOptions) ChannelSortOptions {
	if len(sortOptions) == 0 {
		return NewChannelSortOptions("", "", idSort)
	}
	options := sortOptions[0]
	options.IDSort = options.IDSort || idSort
	return options
}

func NormalizeChannelGroupFilter(group string) string {
	group = strings.TrimSpace(group)
	if group == "" || strings.EqualFold(group, "all") || strings.EqualFold(group, "null") {
		return ""
	}
	return group
}

func channelGroupFilterCondition() string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return `CONCAT(',', ` + commonGroupCol + `, ',') LIKE ? ESCAPE '!'`
	}
	return `(',' || ` + commonGroupCol + ` || ',') LIKE ? ESCAPE '!'`
}

func channelGroupFilterPattern(group string) string {
	group = strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(group)
	return "%," + group + ",%"
}

func ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	group = NormalizeChannelGroupFilter(group)
	if group == "" {
		return query
	}
	return query.Where(channelGroupFilterCondition(), channelGroupFilterPattern(group))
}

// Value implements driver.Valuer interface
// 必须返回 string 而非 []byte:PG simple protocol 下 []byte 参数按 bytea
// 编码,写 json 列会触发 SQLSTATE 22P02。
func (c ChannelInfo) Value() (driver.Value, error) {
	b, err := common.Marshal(&c)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner interface
func (c *ChannelInfo) Scan(value any) error {
	return common.Unmarshal(jsonScanBytes(value), c)
}

func (channel *Channel) GetKeys() []string {
	if channel.Key == "" {
		return []string{}
	}
	if len(channel.Keys) > 0 {
		return channel.Keys
	}
	trimmed := strings.TrimSpace(channel.Key)
	// If the key starts with '[', try to parse it as a JSON array (e.g., for Vertex AI scenarios)
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
			res := make([]string, len(arr))
			for i, v := range arr {
				res[i] = string(v)
			}
			return res
		}
	}
	// Otherwise, fall back to splitting by newline
	keys := strings.Split(strings.Trim(channel.Key, "\n"), "\n")
	return keys
}

func (channel *Channel) GetNextEnabledKey() (string, int, *types.NewAPIError) {
	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	// channel 可能是加锁前的请求快照。内存缓存开启时重新读取权威状态，
	// 避免旧 ChannelInfo 全量写回复活刚被禁用的 key；关闭缓存时调用方刚
	// 从库/快照取得 channel，保持原有零额外查询语义。
	current := channel
	if common.MemoryCacheEnabled {
		loaded, err := CacheGetChannel(channel.Id)
		if err != nil {
			return "", 0, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if loaded == nil {
			return "", 0, types.NewError(errors.New("channel not found"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		current = loaded
	}
	if !current.ChannelInfo.IsMultiKey {
		return current.Key, 0, nil
	}

	keys := current.GetKeys()
	if len(keys) == 0 {
		return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}

	statusList := current.ChannelInfo.MultiKeyStatusList
	// helper to get key status, default to enabled when missing
	getStatus := func(idx int) int {
		if statusList == nil {
			return common.ChannelStatusEnabled
		}
		if status, ok := statusList[idx]; ok {
			return status
		}
		return common.ChannelStatusEnabled
	}

	// Collect indexes of enabled keys
	enabledIdx := make([]int, 0, len(keys))
	for i := range keys {
		if getStatus(i) == common.ChannelStatusEnabled {
			enabledIdx = append(enabledIdx, i)
		}
	}
	if len(enabledIdx) == 0 {
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}

	switch current.ChannelInfo.MultiKeyMode {
	case constant.MultiKeyModeRandom:
		selectedIdx := enabledIdx[rand.Intn(len(enabledIdx))]
		return keys[selectedIdx], selectedIdx, nil
	case constant.MultiKeyModePolling:
		defer func() {
			if common.DebugEnabled {
				logger.LogDebug(nil, "channel %d polling index: %d", current.Id, current.ChannelInfo.MultiKeyPollingIndex)
			}
			if !common.MemoryCacheEnabled {
				_ = current.SaveChannelInfo()
			} else {
				CacheUpdateChannelInfo(current.Id, current.ChannelInfo)
			}
		}()
		start := current.ChannelInfo.MultiKeyPollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
		for i := range keys {
			idx := (start + i) % len(keys)
			if getStatus(idx) == common.ChannelStatusEnabled {
				current.ChannelInfo.MultiKeyPollingIndex = (idx + 1) % len(keys)
				return keys[idx], idx, nil
			}
		}
		return keys[enabledIdx[0]], enabledIdx[0], nil
	default:
		return keys[enabledIdx[0]], enabledIdx[0], nil
	}
}

func (channel *Channel) SaveChannelInfo() error {
	return DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error
}

// GetModels 返回渠道声明的路由键集合：**去空白、丢空项、按首次出现去重**。
//
// 模型名列表在语义上是一个集合；基座曾把它当裸字符串切分，导致空模型名（`models:[""]`）
// 会写进 abilities，重复声明会把 /models 的 member_count 虚高。这里统一收敛为集合语义。
func (channel *Channel) GetModels() []string {
	if channel.Models == "" {
		return []string{}
	}
	parts := strings.Split(channel.Models, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func (channel *Channel) GetGroups() []string {
	if channel.Group == "" {
		return []string{}
	}
	groups := strings.Split(strings.Trim(channel.Group, ","), ",")
	for i, group := range groups {
		groups[i] = strings.TrimSpace(group)
	}
	return groups
}

func (channel *Channel) GetOtherInfo() map[string]any {
	otherInfo := make(map[string]any)
	if channel.OtherInfo != "" {
		err := common.Unmarshal([]byte(channel.OtherInfo), &otherInfo)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		}
	}
	return otherInfo
}

func (channel *Channel) SetOtherInfo(otherInfo map[string]any) {
	otherInfoBytes, err := json.Marshal(otherInfo)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		return
	}
	channel.OtherInfo = string(otherInfoBytes)
}

func (channel *Channel) GetTag() string {
	if channel.Tag == nil {
		return ""
	}
	return *channel.Tag
}

func (channel *Channel) SetTag(tag string) {
	channel.Tag = &tag
}

func (channel *Channel) GetAutoBan() bool {
	if channel.AutoBan == nil {
		return false
	}
	return *channel.AutoBan == 1
}

func (channel *Channel) Save() error {
	return DB.Save(channel).Error
}

// saveStatusState persists only the fields owned by the channel status flow.
// Keeping this allowlist here prevents a stale channel snapshot from
// overwriting credentials, accounting counters, or channel configuration.
func (channel *Channel) saveStatusState() error {
	if channel.Id == 0 {
		return errors.New("channel ID is 0")
	}
	updates := map[string]any{
		"status":     channel.Status,
		"other_info": channel.OtherInfo,
	}
	if channel.ChannelInfo.IsMultiKey {
		updates["channel_info"] = channel.ChannelInfo
	}
	return DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(updates).Error
}

func GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	var err error
	order := resolveChannelSortOptions(idSort, sortOptions)
	if selectAll {
		err = order.Apply(DB).Find(&channels).Error
	} else {
		err = order.Apply(DB).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	return channels, err
}

func GetChannelsByTag(tag string, idSort bool, selectAll bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	order := resolveChannelSortOptions(idSort, sortOptions)
	query := order.Apply(DB.Where("tag = ?", tag))
	if !selectAll {
		query = query.Omit("key")
	}
	err := query.Find(&channels).Error
	return channels, err
}

func SearchChannels(keyword string, group string, model string, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := resolveChannelSortOptions(idSort, sortOptions)

	// 构造基础查询
	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	// 执行查询
	err := order.Apply(baseQuery).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	return channels, nil
}

// GetChannelById loads a channel directly from the database, bypassing the
// in-memory channel cache.
//
// WARNING: do NOT call this on request hot paths (middleware, distribution,
// relay submit/retry, polling). Every call is a synchronous DB query and will
// not see cache-only state. Use CacheGetChannel instead: it serves from the
// in-memory cache and falls back to this function automatically when
// MemoryCacheEnabled is false. Direct use is appropriate only where fresh DB
// state is required, e.g. admin CRUD, channel testing, or cache (re)building.
func GetChannelById(id int, selectAll bool) (*Channel, error) {
	return getChannelByIDWithDB(DB, id, selectAll)
}

// getChannelByIDWithDB 是 GetChannelById 的 db 参数版本；事务内必须传 tx。
func getChannelByIDWithDB(db *gorm.DB, id int, selectAll bool) (*Channel, error) {
	channel := &Channel{Id: id}
	var err error = nil
	if selectAll {
		err = db.First(channel, "id = ?", id).Error
	} else {
		err = db.Omit("key").First(channel, "id = ?", id).Error
	}
	if err != nil {
		return nil, err
	}
	return channel, nil
}

// GetChannelByName 按渠道名取渠道（不含 key 之外的选择由 selectAll 控制）。
// 契约端点统一以渠道名寻址，这是"名字 → 渠道"的唯一入口。
//
// 渠道名必须唯一（api-spec §4.1）。历史数据若存在同名渠道，返回
// ChannelNameAmbiguousError，调用方必须转成 409，**不得静默取第一条**——
// 静默取第一条会让车道成员保存绑定到另一条同名渠道。
func GetChannelByName(name string) (*Channel, error) {
	return getChannelByNameWithDB(DB, name)
}

// GetChannelByNameTx 与 GetChannelByName 相同，但在给定事务内查询。
//
// PBR 的 SQLite 连接池默认 1（model/main.go 的 defaultOpenConns），事务内调用全局
// DB 会在等待第二条连接时自锁；import 这类长事务必须使用 Tx 版本。
func GetChannelByNameTx(tx *gorm.DB, name string) (*Channel, error) {
	return getChannelByNameWithDB(tx, name)
}

func getChannelByNameWithDB(db *gorm.DB, name string) (*Channel, error) {
	var channels []*Channel
	if err := db.Where("name = ?", name).Order("id asc").Find(&channels).Error; err != nil {
		return nil, err
	}
	switch len(channels) {
	case 0:
		return nil, gorm.ErrRecordNotFound
	case 1:
		return channels[0], nil
	default:
		return nil, &ChannelNameAmbiguousError{Name: name, Count: len(channels)}
	}
}

// ChannelNameAmbiguousError 表示按名寻址命中了多条渠道。
type ChannelNameAmbiguousError struct {
	Name  string
	Count int
}

func (e *ChannelNameAmbiguousError) Error() string {
	return fmt.Sprintf("channel name %q is ambiguous: %d channels share it; rename or delete the duplicates first", e.Name, e.Count)
}

// ChannelNameConflictError 表示写入会破坏渠道名唯一性。
// Reason: empty（名称为空）/ duplicate（同一批内重名）/ taken（已被其他渠道占用）。
type ChannelNameConflictError struct {
	Name   string
	Reason string
}

func (e *ChannelNameConflictError) Error() string {
	switch e.Reason {
	case "empty":
		return "channel name is required"
	case "duplicate":
		return fmt.Sprintf("channel name %q is duplicated in this request", e.Name)
	default:
		return fmt.Sprintf("channel name %q already exists", e.Name)
	}
}

// ChannelNameTaken 判断 name 是否已被其他渠道占用；excludeID > 0 时排除自身。
func ChannelNameTaken(name string, excludeID int) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, nil
	}
	query := DB.Model(&Channel{}).Where("name = ?", name)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// EnsureChannelNamesAvailable 校验一批待写入渠道名：非空、批内不重复、未占用。
// 所有创建路径（单条/批量/复制/导入）都必须先过这里，保证唯一性不只依赖 DB 索引
// （历史重名库上索引可能尚未建立）。
func EnsureChannelNamesAvailable(channels []Channel) error {
	seen := make(map[string]bool, len(channels))
	for i := range channels {
		name := strings.TrimSpace(channels[i].Name)
		if name == "" {
			return &ChannelNameConflictError{Reason: "empty"}
		}
		if seen[name] {
			return &ChannelNameConflictError{Name: name, Reason: "duplicate"}
		}
		seen[name] = true
		taken, err := ChannelNameTaken(name, 0)
		if err != nil {
			return err
		}
		if taken {
			return &ChannelNameConflictError{Name: name, Reason: "taken"}
		}
	}
	return nil
}

// ChannelNameDuplicate 历史重名分组（启动审计用）。
type ChannelNameDuplicate struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// DuplicateChannelNames 返回全部重名渠道名及条数（无重名时返回空）。
func DuplicateChannelNames() ([]ChannelNameDuplicate, error) {
	var dups []ChannelNameDuplicate
	err := DB.Model(&Channel{}).
		Select("name, count(*) as count").
		Group("name").
		Having("count(*) > 1").
		Order("name asc").
		Scan(&dups).Error
	return dups, err
}

// ChannelOrNil 取渠道：不存在时返回 (nil, nil)，真实 DB 错误原样上抛。
//
// 这是"渠道是否仍然存在"的**唯一判定口径**：列表/导出把 nil 记为悬空成员，导入剪枝把
// nil 记为待跳过，可用成员计数把非 nil 且启用者计入。此前四处各写一遍 err == nil && ch != nil，
// 口径容易漏改（也容易把真实 DB 故障误判成"渠道不存在"）。
func ChannelOrNil(id int) (*Channel, error) {
	return channelOrNilWithDB(DB, id)
}

// ChannelOrNilTx 与 ChannelOrNil 相同，但在给定事务内查询（事务内禁止用全局 DB）。
func ChannelOrNilTx(tx *gorm.DB, id int) (*Channel, error) {
	return channelOrNilWithDB(tx, id)
}

func channelOrNilWithDB(db *gorm.DB, id int) (*Channel, error) {
	channel, err := getChannelByIDWithDB(db, id, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return channel, nil
}

// ChannelExistsByID 判定渠道是否存在；真实 DB 错误按"存在"处理（宁可保留成员，
// 也不把临时故障当成"渠道已删"而误清理）。
func ChannelExistsByID(id int) bool {
	channel, err := ChannelOrNil(id)
	return err != nil || channel != nil
}

func BatchInsertChannels(channels []Channel) error {
	if len(channels) == 0 {
		return nil
	}
	if err := EnsureChannelNamesAvailable(channels); err != nil {
		return err
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, chunk := range lo.Chunk(channels, 50) {
		if err := tx.Create(&chunk).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

func BatchDeleteChannels(ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var deletedChannels []Channel
	if err := DB.Where("id in ?", ids).Find(&deletedChannels).Error; err != nil {
		return 0, err
	}
	// 使用事务 分批删除 channel 表
	tx := DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	// 事务内重查引用：防止“检查后被并发插入成员”导致批量删除留下悬空引用。
	blocked := map[string][]string{}
	blockedGroups := map[string][]string{}
	for _, id := range ids {
		refs, refErr := ChannelReferencesTx(tx, id)
		if refErr != nil {
			tx.Rollback()
			return 0, refErr
		}
		if refs.Empty() {
			continue
		}
		name := strconv.Itoa(id)
		var ch Channel
		if findErr := tx.Select("name").First(&ch, id).Error; findErr == nil {
			name = ch.Name
		}
		if len(refs.Lanes) > 0 {
			blocked[name] = refs.Lanes
		}
		if len(refs.Groups) > 0 {
			blockedGroups[name] = refs.Groups
		}
	}
	if len(blocked) > 0 || len(blockedGroups) > 0 {
		tx.Rollback()
		return 0, &BatchReferencesError{Blocked: blocked, Groups: blockedGroups}
	}
	var deletedCount int64
	for _, chunk := range lo.Chunk(ids, 200) {
		result := tx.Where("id in (?)", chunk).Delete(&Channel{})
		if result.Error != nil {
			tx.Rollback()
			return 0, result.Error
		}
		deletedCount += result.RowsAffected
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	for i := range deletedChannels {
		NotifyChannelDeleted(&deletedChannels[i])
	}
	return deletedCount, nil
}

func (channel *Channel) GetBaseURL() string {
	if channel.BaseURL == nil {
		return ""
	}
	url := *channel.BaseURL
	if url == "" {
		url = constant.GetChannelBaseURL(channel.Type)
	}
	return normalizeChannelBaseURL(channel.Type, url)
}

// channelVersionSegments 是 base_url 允许以之结尾的版本段（ui-spec §6.4 /
// api-spec §4.1："API 地址只填到 /v1 即可"）。按长度降序匹配，避免 /v1 误吞
// /v1beta 以外的更长后缀。
var channelVersionSegments = []string{"/v1alpha", "/v1beta", "/v1"}

// channelEndpointSuffixes 是用户可能直接粘进 base_url 的完整端点路径。按长度
// 降序匹配（/responses/compact 先于 /responses）。
var channelEndpointSuffixes = []string{
	"/chat/completions",
	"/responses/compact",
	"/responses",
	"/messages",
	"/completions",
	"/embeddings",
}

// normalizeChannelBaseURL 把用户填的 API 地址归一化成"版本根 + 自定义前缀"，让下面
// 三种写法等价（ui-spec §6.4 / api-spec §4.1）：
//
//	https://host
//	https://host/v1
//	https://host/v1/chat/completions   ← 完整端点（很多第三方中转只给这个地址）
//
// 各协议适配器随后会自行补全 /v1/chat/completions、/v1/messages、
// /v1beta/models/{model}:generateContent，因此既不会出现 /v1/v1，也不会
// /v1/chat/completions/v1/chat/completions。历史 bug：base_url 填完整端点时，
// 转发与"探测上游模型"都会拼成 .../v1/chat/completions/v1/... 而 404。
//
// 只对自行拼接路径的协议型渠道生效：OpenAI(1)、Anthropic(14)、Gemini(24)。
// Custom(8) 有自己的 {model} 变量与完整端点补全规则，base_url 必须原样保留；
// 其余厂商类型（Azure 等）路径结构不同，也不改动。
func normalizeChannelBaseURL(channelType int, raw string) string {
	switch channelType {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeAnthropic, constant.ChannelTypeGemini:
	default:
		return raw
	}
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	lower := strings.ToLower(trimmed)
	stripped := false
	for _, endpoint := range channelEndpointSuffixes {
		if strings.HasSuffix(lower, endpoint) {
			trimmed = strings.TrimRight(trimmed[:len(trimmed)-len(endpoint)], "/")
			lower = strings.ToLower(trimmed)
			stripped = true
			break
		}
	}
	for _, segment := range channelVersionSegments {
		if strings.HasSuffix(lower, segment) {
			return trimmed[:len(trimmed)-len(segment)]
		}
	}
	if stripped {
		return trimmed
	}
	return raw
}

// DeriveOpenAICompatibleModelsURL 从"可能是完整端点"的 API 地址推导 OpenAI 兼容的
// 模型清单地址，用于 Custom(8) 未配置 /v1/models 路由时的兜底探测（ui-spec §6.4）：
// 已带版本段 → {base}/models；否则 → {base}/v1/models。
func DeriveOpenAICompatibleModelsURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	lower := strings.ToLower(base)
	for _, endpoint := range channelEndpointSuffixes {
		if strings.HasSuffix(lower, endpoint) {
			base = strings.TrimRight(base[:len(base)-len(endpoint)], "/")
			lower = strings.ToLower(base)
			break
		}
	}
	switch {
	case strings.HasSuffix(lower, "/v1beta"), strings.HasSuffix(lower, "/v1alpha"),
		strings.HasSuffix(lower, "/v1"):
		return base + "/models"
	default:
		return base + "/v1/models"
	}
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}

// ModelMappingMap 解析渠道 model_mapping（模型名 → 上游真名，ADR 0008）。
// 空串 / "{}" / 非法 JSON 一律返回空表（调用方回落到路由键）。
func (channel *Channel) ModelMappingMap() map[string]string {
	out := map[string]string{}
	raw := strings.TrimSpace(channel.GetModelMapping())
	if raw == "" || raw == "{}" {
		return out
	}
	parsed := map[string]string{}
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return map[string]string{}
	}
	for key, value := range parsed {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func (channel *Channel) GetStatusCodeMapping() string {
	if channel.StatusCodeMapping == nil {
		return ""
	}
	return *channel.StatusCodeMapping
}

// StatusCodeMappingMap 解析渠道 status_code_mapping（上游码 → 对外码，ADR 0013）。
// 值可能是字符串或数字（历史写入），统一归一化成十进制字符串；空表/非法 JSON
// 一律返回空表（不映射）。
func (channel *Channel) StatusCodeMappingMap() map[string]string {
	out := map[string]string{}
	raw := strings.TrimSpace(channel.GetStatusCodeMapping())
	if raw == "" || raw == "{}" {
		return out
	}
	parsed := map[string]any{}
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return map[string]string{}
	}
	for key, value := range parsed {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		switch v := value.(type) {
		case string:
			if v = strings.TrimSpace(v); v != "" {
				out[key] = v
			}
		case float64:
			out[key] = strconv.Itoa(int(v))
		case int:
			out[key] = strconv.Itoa(v)
		default:
			if v := strings.TrimSpace(fmt.Sprint(value)); v != "" {
				out[key] = v
			}
		}
	}
	return out
}

func (channel *Channel) Insert() error {
	if strings.TrimSpace(channel.Name) == "" {
		return &ChannelNameConflictError{Reason: "empty"}
	}
	taken, err := ChannelNameTaken(channel.Name, 0)
	if err != nil {
		return err
	}
	if taken {
		return &ChannelNameConflictError{Name: strings.TrimSpace(channel.Name), Reason: "taken"}
	}
	return DB.Create(channel).Error
}

func (channel *Channel) Update() error {
	return channel.update(nil)
}

// UpdateWithLaneCleanup 在同一个事务里完成渠道写入、改名标签重写与“被移除模型”
// 的散车道成员清理；任一步失败整体回滚。返回被清理但保留的车道与整条删除的车道。
// 成员组引用仍由调用方在写入前硬拦，这里不会碰组。
func (channel *Channel) UpdateWithLaneCleanup(removedModels, approvedLanes []string, force bool) (cleaned, deleted []string, err error) {
	err = channel.update(func(tx *gorm.DB) error {
		refs, refErr := RemovedModelReferencesTx(tx, channel.Id, removedModels)
		if refErr != nil {
			return refErr
		}
		approved := make(map[string]bool, len(approvedLanes))
		for _, lane := range approvedLanes {
			approved[lane] = true
		}
		newLanes := make([]string, 0)
		for _, lane := range refs.Lanes {
			if !approved[lane] {
				newLanes = append(newLanes, lane)
			}
		}
		if len(refs.Groups) > 0 || (len(refs.Lanes) > 0 && !force) || len(newLanes) > 0 {
			return &LaneReferencesChangedError{
				Lanes: refs.Lanes, Groups: refs.Groups, NewLanes: newLanes,
			}
		}
		c, d, cleanupErr := CleanupLanesForRemovedModelsTx(tx, channel.Id, removedModels)
		cleaned, deleted = c, d
		return cleanupErr
	})
	return
}

func (channel *Channel) update(after func(tx *gorm.DB) error) error {
	// 渠道名是寻址键：改名不得撞上别的渠道（update 时排除自身）。
	if name := strings.TrimSpace(channel.Name); name != "" {
		taken, nameErr := ChannelNameTaken(name, channel.Id)
		if nameErr != nil {
			return nameErr
		}
		if taken {
			return &ChannelNameConflictError{Name: name, Reason: "taken"}
		}
	}
	// If this is a multi-key channel, recalculate MultiKeySize based on the current key list to avoid inconsistency after editing keys
	if channel.ChannelInfo.IsMultiKey {
		var keyStr string
		if channel.Key != "" {
			keyStr = channel.Key
		} else {
			// If key is not provided, read the existing key from the database
			if existing, err := GetChannelById(channel.Id, true); err == nil {
				keyStr = existing.Key
			}
		}
		// Parse the key list (supports newline separation or JSON array)
		keys := []string{}
		if keyStr != "" {
			trimmed := strings.TrimSpace(keyStr)
			if strings.HasPrefix(trimmed, "[") {
				var arr []json.RawMessage
				if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
					keys = make([]string, len(arr))
					for i, v := range arr {
						keys[i] = string(v)
					}
				}
			}
			if len(keys) == 0 { // fallback to newline split
				keys = strings.Split(strings.Trim(keyStr, "\n"), "\n")
			}
		}
		channel.ChannelInfo.MultiKeySize = len(keys)
		// Clean up status data that exceeds the new key count to prevent index out of range
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			for idx := range channel.ChannelInfo.MultiKeyStatusList {
				if idx >= channel.ChannelInfo.MultiKeySize {
					delete(channel.ChannelInfo.MultiKeyStatusList, idx)
				}
			}
		}
	}
	oldName := ""
	if existing, err := GetChannelById(channel.Id, false); err == nil && existing != nil {
		oldName = existing.Name
	}
	var err error
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(channel).Updates(channel).Error; err != nil {
			return err
		}
		// GORM 的 struct updates 会跳过零值：显式写 models，保证清空模型清单
		// 也能落库（否则清理了车道、渠道却仍声明旧模型）。
		if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).
			Update("models", channel.Models).Error; err != nil {
			return err
		}
		// manual 车道的 active_member 以 `channel/model` 为标签；渠道改名后必须
		// 同步重写，否则车道会“配置还在、点名成员已不存在”而永久 503。
		if oldName != "" && strings.TrimSpace(channel.Name) != oldName {
			if _, err := RewriteManualActiveMemberChannelName(tx, oldName, channel.Name); err != nil {
				return err
			}
		}
		if after != nil {
			return after(tx)
		}
		return nil
	})
	if err != nil {
		return err
	}
	DB.Model(channel).First(channel, "id = ?", channel.Id)
	return err
}

func (channel *Channel) UpdateResponseTime(responseTime int64) {
	err := DB.Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     common.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update response time: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) UpdateBalance(balance float64) {
	err := DB.Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: common.GetTimestamp(),
		Balance:            balance,
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update balance: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) Delete() error {
	result := DB.Delete(channel)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		NotifyChannelDeleted(channel)
	}
	return nil
}

// DeleteIfUnreferenced 在删除事务内重查车道/成员组引用。若仍被引用，返回冲突
// 清单且不删除（调用方据此返回 409）；否则删除并通知运行态。
func (channel *Channel) DeleteIfUnreferenced() (*ChannelReference, error) {
	var conflict *ChannelReference
	err := DB.Transaction(func(tx *gorm.DB) error {
		refs, refErr := ChannelReferencesTx(tx, channel.Id)
		if refErr != nil {
			return refErr
		}
		if !refs.Empty() {
			conflict = refs
			return nil
		}
		return tx.Delete(&Channel{}, channel.Id).Error
	})
	if err != nil {
		return nil, err
	}
	if conflict != nil {
		return conflict, nil
	}
	NotifyChannelDeleted(channel)
	return nil, nil
}

var channelStatusLock sync.Mutex

// 固定条带锁替代按 channelId 动态创建/删除的锁表。动态删除会让仍在
// 执行的请求继续持有旧锁、而新请求拿到新锁，形成同渠道双锁竞态；固定
// 条带则有界、无清理窗口，并保持 polling lock -> channelSyncLock 的锁序。
const channelPollingLockStripes = 256

var channelPollingLocks [channelPollingLockStripes]sync.Mutex

// GetChannelPollingLock returns the fixed striped mutex for the given channel ID.
func GetChannelPollingLock(channelId int) *sync.Mutex {
	if channelId < 0 {
		channelId = -channelId
	}
	return &channelPollingLocks[channelId%channelPollingLockStripes]
}

// CleanupChannelPollingLocks is retained for callers but intentionally does
// nothing: striped locks are bounded and must never be detached from waiters.
func CleanupChannelPollingLocks() {}

func handlerMultiKeyUpdate(channel *Channel, usingKey string, status int, reason string) {
	if channel == nil {
		return
	}
	keys := channel.GetKeys()
	if len(keys) == 0 {
		channel.Status = status
		return
	}
	keyIndex := -1
	for i, key := range keys {
		if key == usingKey {
			keyIndex = i
			break
		}
	}
	if keyIndex < 0 {
		if usingKey != "" {
			common.SysLog(fmt.Sprintf("failed to update multi-key status: channel_id=%d, using key not found", channel.Id))
			return
		}
		channel.Status = status
		info := channel.GetOtherInfo()
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
		return
	}
	handlerMultiKeyUpdateAtIndex(channel, keyIndex, status, reason)
}

func handlerMultiKeyUpdateAtIndex(channel *Channel, keyIndex int, status int, reason string) {
	keys := channel.GetKeys()
	if keyIndex < 0 || keyIndex >= len(keys) {
		return
	}
	if channel.ChannelInfo.MultiKeyStatusList == nil {
		channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
	}
	if status == common.ChannelStatusEnabled {
		delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
	} else {
		channel.ChannelInfo.MultiKeyStatusList[keyIndex] = status
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		channel.ChannelInfo.MultiKeyDisabledReason[keyIndex] = reason
		channel.ChannelInfo.MultiKeyDisabledTime[keyIndex] = common.GetTimestamp()
	}
	if !hasEnabledMultiKey(keys, channel.ChannelInfo.MultiKeyStatusList) {
		channel.Status = common.ChannelStatusAutoDisabled
		info := channel.GetOtherInfo()
		info["status_reason"] = "All keys are disabled"
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
	} else if status == common.ChannelStatusEnabled {
		channel.Status = common.ChannelStatusEnabled
	}
}

func hasEnabledMultiKey(keys []string, statusList map[int]int) bool {
	for i := range keys {
		if statusList == nil {
			return true
		}
		status, ok := statusList[i]
		if !ok || status == common.ChannelStatusEnabled {
			return true
		}
	}
	return false
}

func UpdateChannelStatus(channelId int, usingKey string, status int, reason string) bool {
	return updateChannelStatus(channelId, usingKey, -1, status, reason)
}

// UpdateChannelStatusAtKeyIndex 更新多 key 渠道中指定索引的状态，避免后台
// 闭包携带明文 key。非多 key 渠道退化为整渠道状态更新。
func UpdateChannelStatusAtKeyIndex(channelId int, keyIndex int, status int, reason string) bool {
	return updateChannelStatus(channelId, "", keyIndex, status, reason)
}

func updateChannelStatus(channelId int, usingKey string, keyIndex int, status int, reason string) bool {
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
	}

	pollingLock := GetChannelPollingLock(channelId)
	pollingLock.Lock()
	defer pollingLock.Unlock()

	if common.MemoryCacheEnabled {
		channelCache, _ := CacheGetChannel(channelId)
		if channelCache == nil {
			return false
		}
		if channelCache.ChannelInfo.IsMultiKey {
			beforeStatus := channelCache.Status
			if keyIndex >= 0 {
				handlerMultiKeyUpdateAtIndex(channelCache, keyIndex, status, reason)
			} else {
				handlerMultiKeyUpdate(channelCache, usingKey, status, reason)
			}
			CacheUpdateChannelInfo(channelId, channelCache.ChannelInfo)
			if beforeStatus != channelCache.Status {
				CacheUpdateChannelStatus(channelId, channelCache.Status)
			}
		} else {
			if channelCache.Status == status {
				return false
			}
			CacheUpdateChannelStatus(channelId, status)
		}
	}

	channel, err := GetChannelById(channelId, true)
	if err != nil {
		return false
	}
	if channel.Status == status {
		return false
	}
	beforeStatus := channel.Status
	if channel.ChannelInfo.IsMultiKey {
		if keyIndex >= 0 {
			handlerMultiKeyUpdateAtIndex(channel, keyIndex, status, reason)
		} else {
			handlerMultiKeyUpdate(channel, usingKey, status, reason)
		}
	} else {
		info := channel.GetOtherInfo()
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
		channel.Status = status
	}
	if err = channel.saveStatusState(); err != nil {
		common.SysLog(fmt.Sprintf("failed to update channel status: channel_id=%d, status=%d, error=%v", channel.Id, status, err))
		return false
	}
	publishChannelStatusEvent(channel, beforeStatus, channel.Status, reason)
	notifyChannelRuntimeMutation("channel_status", channel)
	return true
}

func publishChannelStatusEvent(channel *Channel, beforeStatus, afterStatus int, reason string) {
	if channel == nil || beforeStatus == afterStatus {
		return
	}
	eventType := ""
	switch {
	case beforeStatus == common.ChannelStatusEnabled && afterStatus != common.ChannelStatusEnabled:
		eventType = events.EventChannelDisabled
	case beforeStatus != common.ChannelStatusEnabled && afterStatus == common.ChannelStatusEnabled:
		eventType = events.EventChannelEnabled
	}
	if eventType == "" {
		return
	}
	events.Publish(events.Event{
		Ts:     common.GetTimestamp() * 1000,
		Type:   eventType,
		Member: strconv.Itoa(channel.Id),
		Detail: fmt.Sprintf("channel_id=%d name=%q from_status=%d to_status=%d reason=%s", channel.Id, channel.Name, beforeStatus, afterStatus, strings.TrimSpace(reason)),
	})
}

var (
	channelRuntimeMutationHookMu sync.RWMutex
	channelRuntimeMutationHook   func(operation string, channels []*Channel)
)

// SetChannelRuntimeMutationHook 由装配层注入配置写后的运行态维护入口。model 层不能
// 反向 import internal/route，因此通过一次性 hook 保持依赖方向。
func SetChannelRuntimeMutationHook(hook func(operation string, channels []*Channel)) {
	channelRuntimeMutationHookMu.Lock()
	channelRuntimeMutationHook = hook
	channelRuntimeMutationHookMu.Unlock()
}

func notifyChannelRuntimeMutation(operation string, channels ...*Channel) {
	channelRuntimeMutationHookMu.RLock()
	hook := channelRuntimeMutationHook
	channelRuntimeMutationHookMu.RUnlock()
	if hook != nil {
		hook(operation, append([]*Channel(nil), channels...))
	}
}

// NotifyChannelStatusChanged is for complete channel writes that also change
// status (for example the management upsert path). UpdateChannelStatus uses
// the same helper after its atomic status write.
func NotifyChannelStatusChanged(channel *Channel, beforeStatus, afterStatus int, reason string) {
	publishChannelStatusEvent(channel, beforeStatus, afterStatus, reason)
	if channel != nil {
		notifyChannelRuntimeMutation("channel_status", channel)
	}
}

// NotifyChannelDeleted publishes after the row has been deleted. Secrets are
// intentionally excluded from the event detail.
func NotifyChannelDeleted(channel *Channel) {
	if channel == nil {
		return
	}
	events.Publish(events.Event{
		Ts:     common.GetTimestamp() * 1000,
		Type:   events.EventChannelDeleted,
		Member: strconv.Itoa(channel.Id),
		Detail: fmt.Sprintf("channel_id=%d name=%q status=%d", channel.Id, channel.Name, channel.Status),
	})
	notifyChannelRuntimeMutation("channel_deleted", channel)
}

func EnableChannelByTag(tag string) error {
	var channels []Channel
	if err := DB.Where("tag = ?", tag).Find(&channels).Error; err != nil {
		return err
	}
	if err := DB.Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusEnabled).Error; err != nil {
		return err
	}
	for i := range channels {
		publishChannelStatusEvent(&channels[i], channels[i].Status, common.ChannelStatusEnabled, "tag operation")
	}
	changed := make([]*Channel, 0, len(channels))
	for i := range channels {
		changed = append(changed, &channels[i])
	}
	notifyChannelRuntimeMutation("channel_status", changed...)
	return nil
}

func DisableChannelByTag(tag string) error {
	var channels []Channel
	if err := DB.Where("tag = ?", tag).Find(&channels).Error; err != nil {
		return err
	}
	if err := DB.Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusManuallyDisabled).Error; err != nil {
		return err
	}
	for i := range channels {
		publishChannelStatusEvent(&channels[i], channels[i].Status, common.ChannelStatusManuallyDisabled, "tag operation")
	}
	changed := make([]*Channel, 0, len(channels))
	for i := range channels {
		changed = append(changed, &channels[i])
	}
	notifyChannelRuntimeMutation("channel_status", changed...)
	return nil
}

// EditChannelByTag 按标签批量编辑渠道。渠道 priority/weight 与 abilities 表均已
// 物理删除（路由顺序只在车道上），这里只改写渠道自身字段。
func EditChannelByTag(tag string, newTag *string, modelMapping *string, models *string, group *string, paramOverride *string, headerOverride *string) error {
	updateData := Channel{}
	// 如果 newTag 不为空且不等于 tag，则更新 tag
	if newTag != nil && *newTag != tag {
		updateData.Tag = newTag
	}
	if modelMapping != nil {
		updateData.ModelMapping = modelMapping
	}
	if models != nil && *models != "" {
		// 按标签批量改写模型清单同样要过车道引用守卫：被移除的路由键若命中同名车道
		// 且该车道有该渠道成员，返回 LaneReferenceError，由调用方转成 conflict 包络
		// （与渠道编辑/sync-models/模型删除同口径）。
		newModels := make([]string, 0)
		for _, m := range strings.Split(*models, ",") {
			if m = strings.TrimSpace(m); m != "" {
				newModels = append(newModels, m)
			}
		}
		channels, err := GetChannelsByTag(tag, false, false)
		if err != nil {
			return err
		}
		blocked := map[string][]string{}
		blockedGroups := map[string][]string{}
		for _, ch := range channels {
			removed := removedModelNames(ch.GetModels(), newModels)
			if len(removed) == 0 {
				continue
			}
			refs, refErr := RemovedModelReferences(ch.Id, removed)
			if refErr != nil {
				return refErr
			}
			if len(refs.Lanes) > 0 {
				blocked[ch.Name] = refs.Lanes
			}
			if len(refs.Groups) > 0 {
				blockedGroups[ch.Name] = refs.Groups
			}
		}
		if len(blocked) > 0 || len(blockedGroups) > 0 {
			return &LaneReferenceError{Blocked: blocked, Groups: blockedGroups}
		}
		updateData.Models = *models
	}
	if group != nil && *group != "" {
		updateData.Group = *group
	}
	if paramOverride != nil {
		updateData.ParamOverride = paramOverride
	}
	if headerOverride != nil {
		updateData.HeaderOverride = headerOverride
	}

	if err := DB.Model(&Channel{}).Where("tag = ?", tag).Updates(updateData).Error; err != nil {
		return err
	}
	notifyChannelRuntimeMutation("channel_config")
	return nil
}

func DeleteDisabledChannel() (int64, error) {
	var deletedChannels []Channel
	if err := DB.Where("status = ? or status = ?", common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled).Find(&deletedChannels).Error; err != nil {
		return 0, err
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	ids := make([]int, 0, len(deletedChannels))
	blocked := map[string][]string{}
	blockedGroups := map[string][]string{}
	for i := range deletedChannels {
		ids = append(ids, deletedChannels[i].Id)
		refs, refErr := ChannelReferencesTx(tx, deletedChannels[i].Id)
		if refErr != nil {
			tx.Rollback()
			return 0, refErr
		}
		if refs.Empty() {
			continue
		}
		if len(refs.Lanes) > 0 {
			blocked[deletedChannels[i].Name] = refs.Lanes
		}
		if len(refs.Groups) > 0 {
			blockedGroups[deletedChannels[i].Name] = refs.Groups
		}
	}
	if len(blocked) > 0 || len(blockedGroups) > 0 {
		tx.Rollback()
		return 0, &BatchReferencesError{Blocked: blocked, Groups: blockedGroups}
	}
	result := tx.Where("id IN ?", ids).Delete(&Channel{})
	if result.Error != nil {
		tx.Rollback()
		return 0, result.Error
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	for i := range deletedChannels {
		NotifyChannelDeleted(&deletedChannels[i])
	}
	return result.RowsAffected, nil
}

func GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	var tags []*string
	err := query.
		Select("DISTINCT tag").
		Where("tag is not null AND tag != ''").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "tag"}}).
		Offset(offset).
		Limit(limit).
		Find(&tags).Error
	return tags, err
}

func SearchTags(keyword string, group string, model string, idSort bool) ([]*string, error) {
	var tags []*string
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	// 渠道 priority 列已删除（W1）：搜索结果统一按 id 降序，与渠道列表默认排序一致。
	order := "id desc"

	// 构造基础查询
	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	subQuery := baseQuery.
		Select("tag").
		Where("tag != ''").
		Order(order)

	err := DB.Table("(?) as sub", subQuery).
		Select("DISTINCT tag").
		Find(&tags).Error

	if err != nil {
		return nil, err
	}

	return tags, nil
}

func (channel *Channel) ValidateSettings() error {
	channelParams := &dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), channelParams)
		if err != nil {
			return err
		}
	}
	if _, err := common.ParseProxyURLStrict(channelParams.Proxy); err != nil {
		return fmt.Errorf("invalid channel proxy: %w", err)
	}
	if err := channelParams.ValidateHTTPTransport(); err != nil {
		return err
	}
	channelOtherSettings := &dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, channelOtherSettings)
		if err != nil {
			return err
		}
	}
	if err := channelOtherSettings.ValidateToolLossPolicy(); err != nil {
		return err
	}
	if err := channelOtherSettings.ValidateProtocol(); err != nil {
		return err
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if channelOtherSettings.AdvancedCustom == nil {
			return fmt.Errorf("advanced_custom is required")
		}
	}
	if channelOtherSettings.AdvancedCustom != nil {
		if err := channelOtherSettings.AdvancedCustom.Validate(); err != nil {
			return err
		}
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom && channelOtherSettings.UpstreamModelUpdateCheckEnabled {
		if _, ok := channelOtherSettings.AdvancedCustom.ModelListRoute(); !ok {
			return fmt.Errorf("advanced custom channels require a %s route when upstream model update checks are enabled", dto.AdvancedCustomModelListPath)
		}
	}
	return nil
}

// ChannelConfigError 是渠道配置的只读、脱敏错误投影。Path 使用 JSON Pointer。
// 该结构不保留原始配置值，避免密钥、URL、请求头等敏感片段进入管理面或健康快照。
type ChannelConfigError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path"`
}

const (
	channelConfigErrorInvalidJSON  = "invalid_json"
	channelConfigErrorInvalidValue = "invalid_value"
)

// ParseChannelSettings 纯解析渠道 setting JSON。空串按空配置处理；不会修改渠道、
// 缓存或数据库。管理面写入仍由 ValidateSettings 做完整语义校验。
func ParseChannelSettings(raw string) (dto.ChannelSettings, error) {
	var setting dto.ChannelSettings
	if strings.TrimSpace(raw) == "" {
		return setting, nil
	}
	if err := common.Unmarshal([]byte(raw), &setting); err != nil {
		return dto.ChannelSettings{}, err
	}
	return setting, nil
}

// ParseChannelOtherSettings 纯解析渠道 settings JSON（azure 版本等附属配置）。
func ParseChannelOtherSettings(raw string) (dto.ChannelOtherSettings, error) {
	var setting dto.ChannelOtherSettings
	if strings.TrimSpace(raw) == "" {
		return setting, nil
	}
	if err := common.UnmarshalJsonStr(raw, &setting); err != nil {
		return dto.ChannelOtherSettings{}, err
	}
	return setting, nil
}

// SnapshotChannelSettings 在请求快照创建时解析一次渠道配置，并把结果与脱敏错误
// 投影保存在该渠道副本上。后续 getter 直接消费快照，不再重复解析。
func (channel *Channel) SnapshotChannelSettings() {
	if channel == nil || channel.settingsSnapshotReady {
		return
	}
	setting := dto.ChannelSettings{}
	other := dto.ChannelOtherSettings{}
	var configErr *ChannelConfigError
	if channel.Setting != nil {
		parsed, err := ParseChannelSettings(*channel.Setting)
		if err != nil {
			configErr = channelConfigErrorFrom(err, "/setting")
		} else {
			setting = parsed
		}
	}
	if configErr == nil && channel.OtherSettings != "" {
		parsed, err := ParseChannelOtherSettings(channel.OtherSettings)
		if err != nil {
			configErr = channelConfigErrorFrom(err, "/settings")
		} else {
			other = parsed
		}
	}
	channel.settingsSnapshot = setting
	channel.otherSettingsSnapshot = other
	channel.settingsSnapshotErr = configErr
	channel.settingsSnapshotReady = true
}

// SettingsSnapshot 返回请求级纯解析结果。未调用 SnapshotChannelSettings 的普通渠道
// 对象会即时纯解析，不会写库或清空原始配置。
func (channel *Channel) SettingsSnapshot() (dto.ChannelSettings, dto.ChannelOtherSettings, *ChannelConfigError) {
	if channel == nil {
		return dto.ChannelSettings{}, dto.ChannelOtherSettings{}, nil
	}
	if channel.settingsSnapshotReady {
		return channel.settingsSnapshot, channel.otherSettingsSnapshot, channel.settingsSnapshotErr
	}
	setting := dto.ChannelSettings{}
	other := dto.ChannelOtherSettings{}
	var configErr *ChannelConfigError
	if channel.Setting != nil {
		parsed, err := ParseChannelSettings(*channel.Setting)
		if err != nil {
			configErr = channelConfigErrorFrom(err, "/setting")
		} else {
			setting = parsed
		}
	}
	if configErr == nil && channel.OtherSettings != "" {
		parsed, err := ParseChannelOtherSettings(channel.OtherSettings)
		if err != nil {
			configErr = channelConfigErrorFrom(err, "/settings")
		} else {
			other = parsed
		}
	}
	return setting, other, configErr
}

// ConfigError 返回渠道配置的脱敏错误投影；无错误时返回 nil。
func (channel *Channel) ConfigError() *ChannelConfigError {
	if channel == nil {
		return nil
	}
	if channel.settingsSnapshotReady {
		return channel.settingsSnapshotErr
	}
	_, _, configErr := channel.SettingsSnapshot()
	return configErr
}

func channelConfigErrorFrom(err error, path string) *ChannelConfigError {
	if err == nil {
		return nil
	}
	code := channelConfigErrorInvalidValue
	message := "channel configuration contains an invalid value"
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) {
		code = channelConfigErrorInvalidJSON
		message = "channel configuration is not valid JSON"
	} else if errors.As(err, &typeErr) {
		if field := strings.TrimSpace(typeErr.Field); field != "" {
			path += "/" + strings.ReplaceAll(strings.ReplaceAll(field, ".", "/"), "[", "/")
			path = strings.ReplaceAll(path, "]", "")
		}
	}
	return &ChannelConfigError{Code: code, Message: message, Path: path}
}

func (channel *Channel) GetSetting() dto.ChannelSettings {
	setting, _, _ := channel.SettingsSnapshot()
	return setting
}

func (channel *Channel) SetSetting(setting dto.ChannelSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.Setting = common.GetPointer[string](string(settingBytes))
}

func (channel *Channel) GetOtherSettings() dto.ChannelOtherSettings {
	_, setting, _ := channel.SettingsSnapshot()
	return setting
}

func (channel *Channel) SetOtherSettings(setting dto.ChannelOtherSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.OtherSettings = string(settingBytes)
}

func (channel *Channel) GetParamOverride() map[string]any {
	paramOverride := make(map[string]any)
	if channel.ParamOverride != nil && *channel.ParamOverride != "" {
		err := common.Unmarshal([]byte(*channel.ParamOverride), &paramOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal param override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return paramOverride
}

func (channel *Channel) GetHeaderOverride() map[string]any {
	headerOverride := make(map[string]any)
	if channel.HeaderOverride != nil && *channel.HeaderOverride != "" {
		err := common.Unmarshal([]byte(*channel.HeaderOverride), &headerOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal header override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return headerOverride
}

// GetChannelsDeclaringModel 返回"声明了该模型"的渠道（直查 channels 表，含停用渠道）。
//
// 用于模型元数据删除前的车道引用检查：必须覆盖所有声明该模型的渠道，不能只看启用渠道，
// 否则停用渠道上的成员会被漏判。
func GetChannelsDeclaringModel(modelName string) ([]*Channel, error) {
	if strings.TrimSpace(modelName) == "" {
		return nil, nil
	}
	var channels []*Channel
	if err := DB.Omit("key").Find(&channels).Error; err != nil {
		return nil, err
	}
	declaring := make([]*Channel, 0)
	for _, channel := range channels {
		for _, name := range channel.GetModels() {
			if name == modelName {
				declaring = append(declaring, channel)
				break
			}
		}
	}
	return declaring, nil
}

func BatchSetChannelTag(ids []int, tag *string) error {
	// 开启事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	// 更新标签
	err := tx.Model(&Channel{}).Where("id in (?)", ids).Update("tag", tag).Error
	if err != nil {
		tx.Rollback()
		return err
	}

	// 提交事务（abilities 表已删除，标签变更只需改写 channels.tag）
	return tx.Commit().Error
}

// CountAllChannels returns total channels in DB
func CountAllChannels() (int64, error) {
	var total int64
	err := DB.Model(&Channel{}).Count(&total).Error
	return total, err
}

// CountAllTags returns number of non-empty distinct tags
func CountAllTags() (int64, error) {
	return CountChannelTags(DB.Model(&Channel{}))
}

func CountChannelTags(query *gorm.DB) (int64, error) {
	var total int64
	err := query.Where("tag is not null AND tag != ''").Distinct("tag").Count(&total).Error
	return total, err
}

// Get channels of specified type with pagination
func GetChannelsByType(startIdx int, num int, idSort bool, channelType int) ([]*Channel, error) {
	var channels []*Channel
	// 渠道 priority 列已删除（W1）：统一按 id 降序（保留 idSort 形参以兼容调用方）。
	_ = idSort
	err := DB.Where("type = ?", channelType).Order("id desc").Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	return channels, err
}
