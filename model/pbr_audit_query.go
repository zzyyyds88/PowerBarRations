package model

import "gorm.io/gorm"

// 审计查询（api-spec §5.5.2）：导出的过滤面比 /api/audit 多 action/resource/name
// 与时间区间。查询与计数必须共用同一个过滤收口，避免"导出行数"与"实际命中数"
// 口径分叉后让 10 万行上限判断失真。
//
// 本文件只读：不改写 pbr_audit.go 的写入与游标分页路径。

// PBRAuditFilter 是审计查询/导出的过滤条件。
//
// Since/Until 是**闭区间**的 Unix 秒（与 PBRRequestLogFilter 同口径，api-spec
// §5.5.1 的 from/to 端点包含在内）；零值表示该端不限。Action/Resource/Name 为
// 精确匹配，空串表示不过滤。
type PBRAuditFilter struct {
	Action   string
	Resource string
	Name     string
	Since    int64
	Until    int64
	// BeforeId 是 id 倒序游标：只取 id < BeforeId 的行（0 表示从头开始）。
	BeforeId int
	// Limit 是本页最大行数；<=0 表示不限（调用方负责给出上界）。
	Limit int
}

// applyPBRAuditFilter 把过滤条件收口到一处，列表与计数共用。
func applyPBRAuditFilter(query *gorm.DB, filter PBRAuditFilter) *gorm.DB {
	if filter.Action != "" {
		query = query.Where("action = ?", filter.Action)
	}
	if filter.Resource != "" {
		query = query.Where("resource = ?", filter.Resource)
	}
	if filter.Name != "" {
		query = query.Where("name = ?", filter.Name)
	}
	if filter.Since > 0 {
		query = query.Where("ts >= ?", filter.Since)
	}
	if filter.Until > 0 {
		query = query.Where("ts <= ?", filter.Until)
	}
	return query
}

// CountPBRAudits 统计命中行数（导出写响应体前的上限判断用）。
//
// BeforeId 不参与计数：它是翻页游标，不是过滤条件。
func CountPBRAudits(filter PBRAuditFilter) (int64, error) {
	var count int64
	query := DB.Model(&PBRAuditLog{})
	query = applyPBRAuditFilter(query, filter)
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ListPBRAuditsFiltered 按 id 倒序取一页（与 /api/audit 的稳定顺序一致）。
//
// 与 ListPBRAudits 的 +1 探测不同，这里 Limit 就是本页行数：调用方以
// "返回行数 < Limit" 判定末页，翻页时把最后一行 id 放进 BeforeId。
func ListPBRAuditsFiltered(filter PBRAuditFilter) ([]PBRAuditLog, error) {
	query := DB.Model(&PBRAuditLog{}).Order("id desc")
	query = applyPBRAuditFilter(query, filter)
	if filter.BeforeId > 0 {
		query = query.Where("id < ?", filter.BeforeId)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	var entries []PBRAuditLog
	if err := query.Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}
