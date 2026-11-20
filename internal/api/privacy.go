package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy/pii"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy/redact"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

const privacyPreviewMax = 64 << 10

// GetPrivacySettings GET /api/v1/privacy/settings
func GetPrivacySettings(c *gin.Context) {
	s, err := privacy.Load()
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, s)
}

// UpdatePrivacySettings PUT /api/v1/privacy/settings
func UpdatePrivacySettings(c *gin.Context) {
	before, err := privacy.Load()
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	var in privacy.Settings
	if err := json.NewDecoder(c.Request.Body).Decode(&in); err != nil {
		apierr.Validation(c, "invalid privacy settings: "+err.Error())
		return
	}
	after, err := privacy.Normalize(in)
	if err != nil {
		apierr.Unprocessable(c, apierr.CodeValidationFailed, err.Error())
		return
	}
	if dryRun(c) {
		c.JSON(http.StatusOK, gin.H{
			"dry_run":  true,
			"settings": after,
			"diff":     privacySettingsDiff(before, after),
		})
		return
	}
	saved, err := privacy.Save(after)
	if err != nil {
		apierr.Unprocessable(c, apierr.CodeValidationFailed, err.Error())
		return
	}
	writeAudit(c, "update", "privacy", "privacy settings", before, saved)
	if saved.Model.Enabled && !pii.Installed() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			_ = pii.Download(ctx)
		}()
	}
	c.JSON(http.StatusOK, saved)
}

func privacySettingsDiff(before, after privacy.Settings) map[string]any {
	out := map[string]any{}
	add := func(key string, a, b any) {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		if string(x) != string(y) {
			out[key] = gin.H{"from": a, "to": b}
		}
	}
	add("secrets", before.Secrets, after.Secrets)
	add("personal", before.Personal, after.Personal)
	add("kinds", before.Kinds, after.Kinds)
	add("words", before.Words, after.Words)
	add("words_any_case", before.WordsAnyCase, after.WordsAnyCase)
	add("words_whole", before.WordsWhole, after.WordsWhole)
	add("rules", before.Rules, after.Rules)
	add("actions", before.Actions, after.Actions)
	add("allow", before.Allow, after.Allow)
	add("shape", before.Shape, after.Shape)
	add("model", before.Model, after.Model)
	return out
}

// GetPrivacyRecommended GET /api/v1/privacy/recommended
//
// 返回发布方推荐基线（privacy-spec §4）。它是只读的「应该长什么样」，不是
// 「现在是什么」：三个总开关刻意保持 false，脱敏仍须用户在设置页手动开启。
// 控制台用它实现「应用推荐配置」，避免前端自己硬编码一份业务真源。
func GetPrivacyRecommended(c *gin.Context) {
	c.JSON(http.StatusOK, privacy.Recommended())
}

// GetPrivacyModel GET /api/v1/privacy/model
func GetPrivacyModel(c *gin.Context) {
	s, err := privacy.Load()
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	st := pii.Now()
	c.JSON(http.StatusOK, gin.H{
		"name": st.Name, "revision": st.Revision, "size": st.Size,
		"installed": st.Installed, "downloading": st.Downloading,
		"done": st.Done, "total": st.Total, "error": st.Error,
		"loaded": st.Loaded, "device": st.Device, "load_ms": st.LoadMS,
		"min_score": s.Model.MinScore, "required": s.Model.Required,
	})
}

// DownloadPrivacyModel POST /api/v1/privacy/model/download
func DownloadPrivacyModel(c *gin.Context) {
	if dryRun(c) {
		apierr.Validation(c, "model download is an external action and cannot be previewed")
		return
	}
	settings, err := privacy.Load()
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	if !settings.Model.Enabled {
		apierr.Unprocessable(c, apierr.CodeValidationFailed, "enable the local model in privacy settings before downloading it")
		return
	}
	started := false
	if !pii.Installed() {
		started = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			_ = pii.Download(ctx)
		}()
	}
	c.JSON(http.StatusOK, gin.H{"started": started, "model": pii.Now()})
}

// PreviewPrivacy POST /api/v1/privacy/preview
func PreviewPrivacy(c *gin.Context) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&in); err != nil {
		apierr.Validation(c, "invalid preview body: "+err.Error())
		return
	}
	truncated := false
	if len(in.Text) > privacyPreviewMax {
		in.Text = in.Text[:privacyPreviewMax]
		for len(in.Text) > 0 && !utf8.RuneStart(in.Text[len(in.Text)-1]) {
			in.Text = in.Text[:len(in.Text)-1]
		}
		truncated = true
	}
	s, err := privacy.Load()
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	engine := privacy.Engine{Settings: s}
	out, hits, err := engine.Preview(c.Request.Context(), in.Text)
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"text": out, "hits": hits, "active": s.ActiveNow(), "truncated": truncated,
	})
}

// ListPrivacyRecords GET /api/v1/privacy/records
func ListPrivacyRecords(c *gin.Context) {
	filter := model.PBRRequestLogFilter{}
	from, err := parseTimeQuery(c.Query("from"))
	if err != nil {
		apierr.Validation(c, "from must be RFC3339 or unix seconds")
		return
	}
	until, err := parseTimeQuery(c.Query("to"))
	if err != nil {
		apierr.Validation(c, "to must be RFC3339 or unix seconds")
		return
	}
	if from == 0 {
		from = time.Now().Add(-30 * 24 * time.Hour).Unix()
	}
	filter.Since, filter.Until = from, until
	filter.Client = strings.TrimSpace(c.Query("client"))
	filter.Model = strings.TrimSpace(c.Query("model"))
	filter.MaskKind = strings.TrimSpace(c.Query("kind"))
	filter.MaskBy = strings.TrimSpace(c.Query("by"))
	filter.MaskAction = strings.TrimSpace(c.Query("action"))
	masked := true
	filter.Masked = &masked
	if raw := strings.TrimSpace(c.Query("blocked")); raw != "" {
		blocked := raw == "1" || strings.EqualFold(raw, "true")
		if blocked {
			filter.MaskAction = redact.Block
		}
	}
	page := 1
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			apierr.Validation(c, "page must be a positive integer")
			return
		}
		page = v
	}
	pageSize := 50
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			apierr.Validation(c, "page_size must be a positive integer")
			return
		}
		pageSize = v
	}
	if pageSize > 100 {
		pageSize = 100
	}
	filter.Page, filter.PageSize = page, pageSize
	total, err := model.CountPBRRequestLogs(filter)
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	entries, err := model.ListPBRRequestLogs(filter)
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	items := make([]gin.H, 0, len(entries))
	for i := range entries {
		item := logResponse(&entries[i])
		item["mask_hits"] = parseMaskHits(entries[i].MaskHits)
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items, "total": total, "page": page, "page_size": pageSize,
	})
}

// GetPrivacySummary GET /api/v1/privacy/summary?days=
func GetPrivacySummary(c *gin.Context) {
	days := 7
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || v > 30 {
			apierr.Validation(c, "days must be an integer between 1 and 30")
			return
		}
		days = v
	}
	until := time.Now().Unix()
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	requests, masked, blocked, err := model.PBRPrivacyTotals(from, until)
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	rows, err := model.ListPBRPrivacySamples(from, until, 20000)
	if err != nil {
		apierr.Internal(c, err.Error())
		return
	}
	byKind := map[string]int64{}
	for i := range rows {
		var hits []privacy.Hit
		if strings.TrimSpace(rows[i].MaskHits) != "" {
			_ = json.Unmarshal([]byte(rows[i].MaskHits), &hits)
		}
		for _, h := range hits {
			if h.Action == redact.Warn {
				continue
			}
			byKind[h.Kind] += int64(h.N)
		}
	}
	kinds := make([]gin.H, 0, len(byKind))
	for k, n := range byKind {
		kinds = append(kinds, gin.H{"kind": k, "n": n})
	}
	c.JSON(http.StatusOK, gin.H{
		"days": days, "masked_values": masked, "requests": requests,
		"blocked": blocked, "by_kind": kinds, "model": pii.Now(),
		"by_kind_complete": int64(len(rows)) == requests,
	})
}
