package model

// ListPBRPrivacySamples returns masked requests in a window, newest first,
// for /api/privacy/summary. It is deliberately capped: the summary is a
// presentation aggregate over a single user's recent logs, and reading a
// whole month's rows into memory must not become an unbounded query.
func ListPBRPrivacySamples(from, until int64, limit int) ([]PBRRequestLog, error) {
	if limit <= 0 {
		limit = 20000
	}
	query := DB.Model(&PBRRequestLog{}).
		Where("masked_count > 0").
		Order("id desc").
		Limit(limit)
	if from > 0 {
		query = query.Where("ts >= ?", from)
	}
	if until > 0 {
		query = query.Where("ts <= ?", until)
	}
	var rows []PBRRequestLog
	err := query.Find(&rows).Error
	return rows, err
}

// PBRPrivacyTotals is the exact window aggregate for the masking page.
func PBRPrivacyTotals(from, until int64) (requests, masked, blocked int64, err error) {
	query := DB.Model(&PBRRequestLog{}).Where("masked_count > 0")
	if from > 0 {
		query = query.Where("ts >= ?", from)
	}
	if until > 0 {
		query = query.Where("ts <= ?", until)
	}
	var row struct {
		Requests int64
		Masked   int64
		Blocked  int64
	}
	err = query.Select(
		"COUNT(*) AS requests, COALESCE(SUM(masked_count),0) AS masked, " +
			"COALESCE(SUM(CASE WHEN mask_blocked THEN 1 ELSE 0 END),0) AS blocked").
		Scan(&row).Error
	return row.Requests, row.Masked, row.Blocked, err
}
