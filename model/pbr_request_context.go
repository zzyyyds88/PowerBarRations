package model

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/internal/usage"

	"gorm.io/gorm"
)

// PBRRequestContext is the prompt-composition detail for one request. It is a
// companion of pbr_request_logs (one row per chat-like request) and is pruned
// with the log it belongs to; the log row keeps only the summary columns.
type PBRRequestContext struct {
	LogId           int    `json:"log_id" gorm:"primaryKey;autoIncrement:false"`
	Version         int    `json:"version"`
	Status          string `json:"status" gorm:"type:varchar(16)"`
	Tokens          int    `json:"tokens"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Window          int    `json:"window"`
	Counted         bool   `json:"counted"`
	Held            bool   `json:"held"`
	Turns           int    `json:"turns"`
	Truncated       bool   `json:"truncated"`
	Parts           string `json:"parts" gorm:"type:text"`
	BodyBytes       int    `json:"body_bytes"`
	CreatedAt       int64  `json:"created_at" gorm:"index"`
}

func (PBRRequestContext) TableName() string { return "pbr_request_contexts" }

// SavePBRRequestContext writes the detail row after the log has an id.
// createdAt is the log's ts, so pruning by either clock removes the same rows.
func SavePBRRequestContext(logID int, context *usage.Context, bodyBytes int, createdAt int64) error {
	return savePBRRequestContextWithDB(DB, logID, context, bodyBytes, createdAt)
}

func savePBRRequestContextWithDB(db *gorm.DB, logID int, context *usage.Context, bodyBytes int, createdAt int64) error {
	if logID <= 0 || context == nil {
		return nil
	}
	parts, err := json.Marshal(context.Parts)
	if err != nil {
		return err
	}
	return db.Create(&PBRRequestContext{
		LogId:           logID,
		Version:         context.Version,
		Status:          context.Status,
		Tokens:          context.Tokens,
		EstimatedTokens: context.EstimatedTokens,
		Window:          context.Window,
		Counted:         context.Counted,
		Held:            context.Held,
		Turns:           context.Turns,
		Truncated:       context.Truncated,
		Parts:           string(parts),
		BodyBytes:       bodyBytes,
		CreatedAt:       createdAt,
	}).Error
}

// GetPBRRequestContext returns the detail row, nil when the request had none.
func GetPBRRequestContext(logID int) (*PBRRequestContext, error) {
	var row PBRRequestContext
	err := DB.Where("log_id = ?", logID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UsageContext reassembles the detail row for the API. Parts that cannot be
// decoded stay empty rather than turning the whole answer into an error.
func (row *PBRRequestContext) UsageContext() *usage.Context {
	if row == nil {
		return nil
	}
	context := &usage.Context{
		Version:         row.Version,
		Status:          row.Status,
		Tokens:          row.Tokens,
		EstimatedTokens: row.EstimatedTokens,
		Window:          row.Window,
		Counted:         row.Counted,
		Held:            row.Held,
		Turns:           row.Turns,
		Truncated:       row.Truncated,
		Parts:           []usage.Part{},
	}
	if strings.TrimSpace(row.Parts) != "" {
		_ = json.Unmarshal([]byte(row.Parts), &context.Parts)
	}
	return context
}
