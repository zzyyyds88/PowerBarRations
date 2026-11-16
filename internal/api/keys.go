package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/middleware"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 客户端密钥（api-spec §4.3 / §5.4，token-spec §3）。
//
// 新建与轮换的密钥明文入库存储，列表/详情可直接回读；迁移前存量密钥的 key 为 null。

type lanePolicyPayload struct {
	Mode       string   `json:"mode"`
	AllowLanes []string `json:"allow_lanes"`
	DenyLanes  []string `json:"deny_lanes"`
}

type clientKeyPayload struct {
	Name           string             `json:"name"`
	Enabled        *bool              `json:"enabled"`
	LanePolicy     *lanePolicyPayload `json:"lane_policy"`
	IPAllowlist    *[]string          `json:"ip_allowlist"`
	RateLimitRPM   *int               `json:"rate_limit_rpm"`
	MaxConcurrency *int               `json:"max_concurrency"`
	ExpiresAt      *string            `json:"expires_at"`
	Notes          *string            `json:"notes"`
}

// clientKeyResponse 组装密钥响应。cost 是响应期由小时聚合表派生的只读统计
// （api-spec §5.4 / token-spec §3.7），不是 ClientKey 的存储字段。
func clientKeyResponse(key *model.ClientKey, cost float64) gin.H {
	policy := model.ParseLanePolicy(key.LanePolicy)
	allow := policy.AllowLanes
	if allow == nil {
		allow = []string{}
	}
	deny := policy.DenyLanes
	if deny == nil {
		deny = []string{}
	}
	var ipAllowlist []string
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
	var lastUsedAt any
	if key.LastUsedAt > 0 {
		lastUsedAt = time.Unix(key.LastUsedAt, 0).UTC().Format(time.RFC3339)
	}
	// 迁移前存量密钥无明文可回显（token-spec §3.3）：返回 null，前端提示轮换。
	var plain any
	if key.KeyPlain != "" {
		plain = key.KeyPlain
	}
	return gin.H{
		"id":              key.Id,
		"name":            key.Name,
		"enabled":         key.Enabled,
		"lane_policy":     gin.H{"mode": policy.Mode, "allow_lanes": allow, "deny_lanes": deny},
		"ip_allowlist":    ipAllowlist,
		"rate_limit_rpm":  key.RateLimitRPM,
		"max_concurrency": key.MaxConcurrency,
		"expires_at":      expiresAt,
		"notes":           key.Notes,
		"key":             plain,
		"key_prefix":      key.KeyPrefix,
		"created_at":      rfc3339(key.CreatedAt),
		"updated_at":      rfc3339(key.UpdatedAt),
		"last_used_at":    lastUsedAt,
		"cost":            cost,
	}
}

// clientKeyCosts 返回 密钥名 → 累计上游折算花费（元）的映射。
//
// 数据源是小时聚合表 pbr_stats_hourly（与看板/日志同源，api-spec §5.4、
// token-spec §3.7）：group_kind='key'，group_key 存的是密钥 Name
// （见 model/pbr_request_log.go 的 dimensions 映射 "key": entry.TokenName）。
//
// 列表端必须**一次查询 + map 回填**，禁止按密钥逐个查询（N+1）。name 非空时
// 只查该密钥，供详情与写后回读复用同一条代码路径。
//
// cost 是纯统计展示，不参与任何鉴权/限额/拒绝逻辑，因此查询失败只记日志并回落
// 到空表（所有密钥 cost 为 0），不阻塞密钥管理（token-spec §3.7）。
func clientKeyCosts(name string) map[string]float64 {
	costs := map[string]float64{}
	if model.DB == nil {
		return costs
	}
	query := model.DB.Model(&model.PBRStatsHourly{}).
		Select("group_key AS group_key, SUM(cost_sum) AS cost_sum").
		Where("group_kind = ?", "key")
	if name != "" {
		query = query.Where("group_key = ?", name)
	}
	var rows []struct {
		GroupKey string  `gorm:"column:group_key"`
		CostSum  float64 `gorm:"column:cost_sum"`
	}
	if err := query.Group("group_key").Scan(&rows).Error; err != nil {
		common.SysError("pbr: load client key cost failed: " + err.Error())
		return costs
	}
	for _, row := range rows {
		costs[row.GroupKey] = row.CostSum
	}
	return costs
}

// clientKeyCost 取单个密钥的 cost；无聚合数据时为 0。
func clientKeyCost(name string) float64 {
	return clientKeyCosts(name)[name]
}

// ListKeys GET /api/v1/keys
func ListKeys(c *gin.Context) {
	limit, cursor, err := pageParams(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	query := model.DB.Model(&model.ClientKey{}).Order("name asc").Limit(limit + 1)
	if cursor != "" {
		query = query.Where("name > ?", cursor)
	}
	var keys []model.ClientKey
	if err := query.Find(&keys).Error; err != nil {
		writeAPIError(c, err)
		return
	}
	var nextCursor any
	if len(keys) > limit {
		nextCursor = encodeCursor(keys[limit-1].Name)
		keys = keys[:limit]
	}
	costs := clientKeyCosts("")
	items := make([]gin.H, 0, len(keys))
	for i := range keys {
		items = append(items, clientKeyResponse(&keys[i], costs[keys[i].Name]))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor})
}

// GetKey GET /api/v1/keys/{name}
func GetKey(c *gin.Context) {
	key, err := model.GetClientKeyByName(c.Param("name"))
	if err != nil {
		apierr.NotFound(c, apierr.CodeKeyNotFound, "key '"+c.Param("name")+"' not found", "GET /api/v1/keys")
		return
	}
	c.JSON(http.StatusOK, clientKeyResponse(key, clientKeyCost(key.Name)))
}

// CreateKey POST /api/v1/keys：生成并持久化明文，响应含 key。
func CreateKey(c *gin.Context) {
	var payload clientKeyPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		apierr.Validation(c, "name is required")
		return
	}
	if _, err := model.GetClientKeyByName(name); err == nil {
		apierr.Conflict(c, apierr.CodeConflict, "key '"+name+"' already exists", "POST /api/v1/keys/"+name+"/rotate")
		return
	} else if err != gorm.ErrRecordNotFound {
		writeAPIError(c, err)
		return
	}
	plain, prefix, err := model.GenerateClientKey()
	if err != nil {
		writeAPIError(c, err)
		return
	}
	key := &model.ClientKey{
		Name:      name,
		KeyHash:   model.HashClientKey(plain),
		KeyPrefix: prefix,
		KeyPlain:  plain,
		Enabled:   true,
	}
	if buildErr := applyClientKeyPayload(key, &payload); buildErr != nil {
		// 状态由 apiError 自带（lane_policy 语义校验 422，其余字段校验 400，api-spec §5.4）。
		writeAPIError(c, buildErr)
		return
	}
	if dryRun(c) {
		dryRunResult(c, "keys", "add", name)
		return
	}
	if err := model.UpsertClientKey(key); err != nil {
		writeAPIError(c, err)
		return
	}
	saved, err := model.GetClientKeyByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	writeAudit(c, "create", "client_key", name)
	response := clientKeyResponse(saved, clientKeyCost(saved.Name))
	response["key"] = plain
	c.JSON(http.StatusOK, response)
}

// PutKey PUT /api/v1/keys/{name}：更新权限/限流/备注，不改密钥与哈希。
func PutKey(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	existing, err := model.GetClientKeyByName(name)
	if err != nil {
		apierr.NotFound(c, apierr.CodeKeyNotFound, "key '"+name+"' not found", "POST /api/v1/keys")
		return
	}
	var payload clientKeyPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	key := &model.ClientKey{Name: name}
	if buildErr := applyClientKeyPayload(key, &payload); buildErr != nil {
		// 状态由 apiError 自带（lane_policy 语义校验 422，其余字段校验 400，api-spec §5.4）。
		writeAPIError(c, buildErr)
		return
	}
	// 未提供的字段保持原值。
	if payload.Enabled == nil {
		key.Enabled = existing.Enabled
	}
	if payload.LanePolicy == nil {
		key.LanePolicy = existing.LanePolicy
	}
	if payload.IPAllowlist == nil {
		key.IPAllowlist = existing.IPAllowlist
	}
	if payload.RateLimitRPM == nil {
		key.RateLimitRPM = existing.RateLimitRPM
	}
	if payload.MaxConcurrency == nil {
		key.MaxConcurrency = existing.MaxConcurrency
	}
	if payload.ExpiresAt == nil {
		key.ExpiresAt = existing.ExpiresAt
	}
	if payload.Notes == nil {
		key.Notes = existing.Notes
	}
	if dryRun(c) {
		dryRunResult(c, "keys", "update", name)
		return
	}
	if err := model.UpsertClientKey(key); err != nil {
		writeAPIError(c, err)
		return
	}
	saved, err := model.GetClientKeyByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	writeAudit(c, "update", "client_key", name)
	c.JSON(http.StatusOK, clientKeyResponse(saved, clientKeyCost(saved.Name)))
}

// DeleteKey DELETE /api/v1/keys/{name}
func DeleteKey(c *gin.Context) {
	name := c.Param("name")
	existing, err := model.GetClientKeyByName(name)
	if err != nil {
		apierr.NotFound(c, apierr.CodeKeyNotFound, "key '"+name+"' not found", "GET /api/v1/keys")
		return
	}
	if dryRun(c) {
		dryRunResult(c, "keys", "remove", name)
		return
	}
	if err := model.DeleteClientKeyByName(name); err != nil {
		writeAPIError(c, err)
		return
	}
	// 同步清掉进程内限流状态，避免条目随删除的密钥长期累积
	middleware.DropPBRKeyLimiter(existing.Id)
	writeAudit(c, "delete", "client_key", name)
	c.JSON(http.StatusOK, gin.H{"deleted": true, "name": name})
}

// RotateKey POST /api/v1/keys/{name}/rotate：立即作废旧密钥，持久化并返回新明文。
func RotateKey(c *gin.Context) {
	name := c.Param("name")
	existing, err := model.GetClientKeyByName(name)
	if err != nil {
		apierr.NotFound(c, apierr.CodeKeyNotFound, "key '"+name+"' not found", "GET /api/v1/keys")
		return
	}
	plain, prefix, err := model.GenerateClientKey()
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if dryRun(c) {
		dryRunResult(c, "keys", "update", name)
		return
	}
	existing.KeyHash = model.HashClientKey(plain)
	existing.KeyPrefix = prefix
	existing.KeyPlain = plain
	if err := model.UpsertClientKey(existing); err != nil {
		writeAPIError(c, err)
		return
	}
	saved, err := model.GetClientKeyByName(name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	writeAudit(c, "rotate", "client_key", name)
	response := clientKeyResponse(saved, clientKeyCost(saved.Name))
	response["key"] = plain
	c.JSON(http.StatusOK, response)
}

// unknownRouteKeys 返回既没有同名车道、也没有任何启用渠道声明的路由键（去重、保序）。
// 校验失败不阻断 DB 故障：查询出错时按"未知"处理并上抛由调用方决定；这里只在
// 明确查得候选集合后判定，避免把临时故障误判成非法输入。
func unknownRouteKeys(keys []string) []string {
	unique := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		unique = append(unique, k)
	}
	if len(unique) == 0 {
		return nil
	}
	known := map[string]bool{}
	if lanes, err := model.ListLanes(); err == nil {
		for _, lane := range lanes {
			known[lane.Name] = true
		}
	}
	if summaries, err := model.ListModelSummaries(); err == nil {
		for _, s := range summaries {
			known[s.Model] = true
		}
	}
	unknown := make([]string, 0)
	for _, k := range unique {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	return unknown
}

func applyClientKeyPayload(key *model.ClientKey, payload *clientKeyPayload) *apiError {
	if payload.Enabled != nil {
		key.Enabled = *payload.Enabled
	}
	if payload.LanePolicy != nil {
		mode := strings.TrimSpace(payload.LanePolicy.Mode)
		if mode == "" {
			mode = model.LanePolicyModeAll
		}
		if mode != model.LanePolicyModeAll && mode != model.LanePolicyModeAllow {
			// 显式拒绝非法 mode：此前只有读取路径的宽容回落（静默变"允许全部"），
			// 写入路径必须报错，避免用户以为设置了限制、实际被放宽。
			// 语义类校验失败按契约返回 422（api-spec §5.4）。
			return &apiError{status: http.StatusUnprocessableEntity,
				code: apierr.CodeValidationFailed, message: "lane_policy.mode must be all or allow"}
		}
		// 权限判定的对象是"路由键"（token-spec §3.2）：allow_lanes 里写不存在的键会被
		// 静默接受，用户以为"允许了模型 X"，实际是死键（请求得到 503 而非 403，分不清
		// 权限还是没配车道）。这里显式拒绝并列出未知键（api-spec §5.4：422）。
		if unknown := unknownRouteKeys(payload.LanePolicy.AllowLanes); len(unknown) > 0 {
			return &apiError{
				status:  http.StatusUnprocessableEntity,
				code:    apierr.CodeValidationFailed,
				message: "unknown route key(s) in lane_policy.allow_lanes: " + strings.Join(unknown, ", ") + " (no lane and no channel declares them)",
			}
		}
		// deny_lanes 与 allow_lanes 对称校验：否则用户以为"拒绝了模型 X"，实际是拼错的
		// 死键，等于没有拒绝——同样属于静默放宽权限（api-spec §5.4：422）。
		if unknown := unknownRouteKeys(payload.LanePolicy.DenyLanes); len(unknown) > 0 {
			return &apiError{
				status:  http.StatusUnprocessableEntity,
				code:    apierr.CodeValidationFailed,
				message: "unknown route key(s) in lane_policy.deny_lanes: " + strings.Join(unknown, ", ") + " (no lane and no channel declares them)",
			}
		}
		encoded, err := json.Marshal(model.LanePolicy{
			Mode:       mode,
			AllowLanes: payload.LanePolicy.AllowLanes,
			DenyLanes:  payload.LanePolicy.DenyLanes,
		})
		if err != nil {
			return &apiError{code: apierr.CodeValidationFailed, message: "invalid lane_policy"}
		}
		key.LanePolicy = string(encoded)
	}
	if payload.IPAllowlist != nil {
		encoded, err := json.Marshal(*payload.IPAllowlist)
		if err != nil {
			return &apiError{code: apierr.CodeValidationFailed, message: "invalid ip_allowlist"}
		}
		key.IPAllowlist = string(encoded)
	}
	if payload.RateLimitRPM != nil {
		if *payload.RateLimitRPM < 0 {
			return &apiError{code: apierr.CodeValidationFailed, message: "rate_limit_rpm must be >= 0"}
		}
		key.RateLimitRPM = *payload.RateLimitRPM
	}
	if payload.MaxConcurrency != nil {
		if *payload.MaxConcurrency < 0 {
			return &apiError{code: apierr.CodeValidationFailed, message: "max_concurrency must be >= 0"}
		}
		key.MaxConcurrency = *payload.MaxConcurrency
	}
	if payload.ExpiresAt != nil {
		raw := strings.TrimSpace(*payload.ExpiresAt)
		if raw == "" || raw == "null" {
			key.ExpiresAt = nil
		} else {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return &apiError{code: apierr.CodeValidationFailed, message: "expires_at must be RFC3339 or null"}
			}
			unix := parsed.Unix()
			key.ExpiresAt = &unix
		}
	}
	if payload.Notes != nil {
		key.Notes = strings.TrimSpace(*payload.Notes)
	}
	return nil
}
