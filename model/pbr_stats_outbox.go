package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/metrics"

	"gorm.io/gorm"
)

// 小时聚合的持久化 outbox（design-v1 §8/§16.5）。
//
// 明细与上下文必须在同一事务里落库，聚合不能拖慢或拖垮明细写入，因此
// 明细事务只插入一行 outbox；常驻 worker 再把 outbox 逐行转成小时聚合。
// 每条 outbox 在一个事务内完成「upsert 全部维度 + 删除 outbox 行」，崩溃后
// 要么整条未生效（重试），要么整条已生效（不再重复计数）。
const (
	pbrStatsOutboxBatchSize     = 100
	pbrStatsOutboxMaxAttempts   = 10
	pbrStatsOutboxMaxBackoff    = 5 * time.Minute
	pbrStatsOutboxPollInterval  = 2 * time.Second
	pbrStatsOutboxRetryBaseWait = time.Second
)

// PBRStatsOutbox 待聚合的请求快照。Payload 只含聚合需要的字段，不存
// attempts/error_summary 等明细内容，避免 outbox 表随请求体积膨胀。
type PBRStatsOutbox struct {
	Id int64 `json:"id" gorm:"primaryKey"`
	// LogID 对应 pbr_request_logs.id：rebuild 只清水位以内的 outbox，
	// 避免把重建期间新请求的待办一起删掉。历史行默认 0（归入水位以内）。
	LogID         int64  `json:"log_id" gorm:"not null;default:0;index"`
	Payload       string `json:"payload" gorm:"type:text;not null"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;not null;index"`
	Attempts      int    `json:"attempts" gorm:"not null;default:0;index"`
	NextAttemptAt int64  `json:"next_attempt_at" gorm:"bigint;not null;default:0;index:idx_pbr_stats_outbox_ready,priority:1"`
	LastError     string `json:"last_error" gorm:"type:text"`
}

func (PBRStatsOutbox) TableName() string { return "pbr_stats_outbox" }

// pbrStatsOutboxPayload 是 upsertPBRHourlyStatTx 所需的全部输入。
type pbrStatsOutboxPayload struct {
	Ts               int64   `json:"ts"`
	RouteSource      string  `json:"route_source"`
	LaneName         string  `json:"lane_name"`
	MemberChannel    string  `json:"member_channel_name"`
	TokenName        string  `json:"token_name"`
	Client           string  `json:"client"`
	UpstreamModel    string  `json:"upstream_model"`
	UsageSemantic    string  `json:"usage_semantic"`
	Success          bool    `json:"success"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
	TTFTMs           int64   `json:"ttft_ms"`
}

func newPBRStatsOutboxPayload(entry *PBRRequestLog) pbrStatsOutboxPayload {
	return pbrStatsOutboxPayload{
		Ts:               entry.Ts,
		RouteSource:      entry.RouteSource,
		LaneName:         entry.LaneName,
		MemberChannel:    entry.MemberChannelName,
		TokenName:        entry.TokenName,
		Client:           entry.Client,
		UpstreamModel:    entry.UpstreamModel,
		UsageSemantic:    entry.UsageSemantic,
		Success:          entry.Success,
		PromptTokens:     entry.PromptTokens,
		CompletionTokens: entry.CompletionTokens,
		CacheReadTokens:  entry.CacheReadTokens,
		CacheWriteTokens: entry.CacheWriteTokens,
		ReasoningTokens:  entry.ReasoningTokens,
		EstimatedCost:    entry.EstimatedCost,
		TTFTMs:           entry.TTFTMs,
	}
}

func (p *pbrStatsOutboxPayload) toEntry() *PBRRequestLog {
	return &PBRRequestLog{
		Ts:                p.Ts,
		RouteSource:       p.RouteSource,
		LaneName:          p.LaneName,
		MemberChannelName: p.MemberChannel,
		TokenName:         p.TokenName,
		Client:            p.Client,
		UpstreamModel:     p.UpstreamModel,
		UsageSemantic:     p.UsageSemantic,
		Success:           p.Success,
		PromptTokens:      p.PromptTokens,
		CompletionTokens:  p.CompletionTokens,
		CacheReadTokens:   p.CacheReadTokens,
		CacheWriteTokens:  p.CacheWriteTokens,
		ReasoningTokens:   p.ReasoningTokens,
		EstimatedCost:     p.EstimatedCost,
		TTFTMs:            p.TTFTMs,
	}
}

// enqueuePBRStatsOutboxTx 在明细事务内写入 outbox。测试流量（渠道/模型测试）
// 不进聚合，直接跳过（与 upsertPBRHourlyStat 的口径一致）。
func enqueuePBRStatsOutboxTx(tx *gorm.DB, entry *PBRRequestLog) error {
	if entry.RouteSource == PBRRouteSourceTest {
		return nil
	}
	payload, err := json.Marshal(newPBRStatsOutboxPayload(entry))
	if err != nil {
		return err
	}
	return tx.Create(&PBRStatsOutbox{
		LogID:     int64(entry.Id),
		Payload:   string(payload),
		CreatedAt: time.Now().Unix(),
	}).Error
}

var (
	pbrStatsOutboxMu     sync.Mutex
	pbrStatsOutboxCancel context.CancelFunc
	pbrStatsOutboxWG     sync.WaitGroup
	pbrStatsOutboxOnce   sync.Once
)

// StartPBRStatsOutboxWorker 启动聚合 worker。幂等；由 main 在 DB 就绪后调用。
func StartPBRStatsOutboxWorker() {
	pbrStatsOutboxOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		pbrStatsOutboxCancel = cancel
		pbrStatsOutboxWG.Add(1)
		go func() {
			defer pbrStatsOutboxWG.Done()
			runPBRStatsOutboxWorker(ctx)
		}()
	})
}

// ClosePBRStatsOutboxWorker 停止 worker；等待当前批次最多 5 秒，未完成的
// outbox 行留在表中，下次启动继续。
func ClosePBRStatsOutboxWorker() {
	if pbrStatsOutboxCancel == nil {
		return
	}
	pbrStatsOutboxCancel()
	done := make(chan struct{})
	go func() {
		pbrStatsOutboxWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		common.SysError("pbr: stats outbox worker did not stop within 5s; pending rows will resume on next start")
	}
}

func runPBRStatsOutboxWorker(ctx context.Context) {
	// 启动即 drain：上次进程退出/崩溃留下的 outbox 不必等第一个 tick。
	_, _ = drainPBRStatsOutbox(ctx)
	ticker := time.NewTicker(pbrStatsOutboxPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = drainPBRStatsOutbox(ctx)
		}
	}
}

// FlushPBRStatsOutbox 同步 drain 全部待处理行（测试/诊断用）。
// stats rebuild 不再依赖它：rebuild 以明细为真源，成功后只清空水位以内的
// outbox；水位之后的新请求仍由 worker 增量补算。
func FlushPBRStatsOutbox(ctx context.Context) error {
	pbrStatsOutboxMu.Lock()
	defer pbrStatsOutboxMu.Unlock()
	return flushPBRStatsOutboxLocked(ctx)
}

func drainPBRStatsOutbox(ctx context.Context) (processed int, err error) {
	pbrStatsOutboxMu.Lock()
	defer pbrStatsOutboxMu.Unlock()
	for {
		n, batchErr := processPBRStatsOutboxBatchLocked(ctx)
		processed += n
		if batchErr != nil {
			// 失败记账本身写不进去（典型：磁盘满）时立即停手，避免同一批
			// attempts 未递增的行被无 sleep 地反复重选、把日志刷爆。
			return processed, batchErr
		}
		if n < pbrStatsOutboxBatchSize || ctx.Err() != nil {
			break
		}
	}
	updatePBRStatsOutboxMetrics()
	return processed, err
}

func flushPBRStatsOutboxLocked(ctx context.Context) error {
	for {
		n, err := processPBRStatsOutboxBatchLocked(ctx)
		if err != nil {
			return err
		}
		if n < pbrStatsOutboxBatchSize {
			break
		}
	}
	updatePBRStatsOutboxMetrics()
	var pending, dead int64
	if err := DB.Model(&PBRStatsOutbox{}).Where("attempts < ?", pbrStatsOutboxMaxAttempts).Count(&pending).Error; err != nil {
		return err
	}
	if err := DB.Model(&PBRStatsOutbox{}).Where("attempts >= ?", pbrStatsOutboxMaxAttempts).Count(&dead).Error; err != nil {
		return err
	}
	if pending > 0 || dead > 0 {
		return fmt.Errorf("stats outbox not drained: pending=%d dead=%d", pending, dead)
	}
	return nil
}

func processPBRStatsOutboxBatchLocked(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	var rows []PBRStatsOutbox
	if err := DB.WithContext(ctx).
		Where("attempts < ? AND next_attempt_at <= ?", pbrStatsOutboxMaxAttempts, now).
		Order("id asc").Limit(pbrStatsOutboxBatchSize).Find(&rows).Error; err != nil {
		return 0, err
	}
	for i := range rows {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := applyPBRStatsOutboxRow(ctx, &rows[i]); err != nil {
			if recordErr := recordPBRStatsOutboxFailure(&rows[i], err); recordErr != nil {
				return i + 1, recordErr
			}
			continue
		}
	}
	updatePBRStatsOutboxMetrics()
	return len(rows), nil
}

func applyPBRStatsOutboxRow(ctx context.Context, row *PBRStatsOutbox) error {
	var payload pbrStatsOutboxPayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	entry := payload.toEntry()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := upsertPBRHourlyStatTx(tx, entry); err != nil {
			return err
		}
		return tx.Where("id = ?", row.Id).Delete(&PBRStatsOutbox{}).Error
	})
}

func recordPBRStatsOutboxFailure(row *PBRStatsOutbox, cause error) error {
	attempts := row.Attempts + 1
	next := int64(0)
	if attempts < pbrStatsOutboxMaxAttempts {
		backoff := pbrStatsOutboxRetryBaseWait << (attempts - 1)
		if backoff > pbrStatsOutboxMaxBackoff {
			backoff = pbrStatsOutboxMaxBackoff
		}
		next = time.Now().Add(backoff).Unix()
	}
	lastErr := common.TruncateUTF8(common.MaskSensitiveInfo(cause.Error()), 1024)
	if err := DB.Model(&PBRStatsOutbox{}).Where("id = ?", row.Id).
		Updates(map[string]any{
			"attempts":        attempts,
			"next_attempt_at": next,
			"last_error":      lastErr,
		}).Error; err != nil {
		RecordDBWriteError("stats_outbox", err)
		common.SysError("pbr: update stats outbox failure failed: " + err.Error())
		return err
	}
	metrics.ObserveLogWriteFailure("stats_outbox")
	if attempts >= pbrStatsOutboxMaxAttempts {
		common.SysError(fmt.Sprintf("pbr: stats outbox row=%d dead after %d attempts: %s", row.Id, attempts, lastErr))
	}
	return nil
}

func updatePBRStatsOutboxMetrics() {
	if DB == nil {
		return
	}
	var pending, dead int64
	if err := DB.Model(&PBRStatsOutbox{}).Where("attempts < ?", pbrStatsOutboxMaxAttempts).Count(&pending).Error; err != nil {
		return
	}
	if err := DB.Model(&PBRStatsOutbox{}).Where("attempts >= ?", pbrStatsOutboxMaxAttempts).Count(&dead).Error; err != nil {
		return
	}
	metrics.SetStatsOutboxBacklog(pending, dead)
}

// PBRStatsOutboxBacklog 供 /api/health 等只读端点展示聚合落后程度。
func PBRStatsOutboxBacklog() (pending int64, dead int64, err error) {
	if DB == nil {
		return 0, 0, errors.New("database not initialized")
	}
	if err := DB.Model(&PBRStatsOutbox{}).Where("attempts < ?", pbrStatsOutboxMaxAttempts).Count(&pending).Error; err != nil {
		return 0, 0, err
	}
	if err := DB.Model(&PBRStatsOutbox{}).Where("attempts >= ?", pbrStatsOutboxMaxAttempts).Count(&dead).Error; err != nil {
		return 0, 0, err
	}
	return pending, dead, nil
}

// PBRStatsOutboxStatus 是 CLI 运维视图：积压量、最早时间与最近一次错误。
type PBRStatsOutboxStatus struct {
	Pending         int64  `json:"pending"`
	Dead            int64  `json:"dead"`
	OldestPendingAt int64  `json:"oldest_pending_at"`
	OldestDeadAt    int64  `json:"oldest_dead_at"`
	LastError       string `json:"last_error"`
}

// LoadPBRStatsOutboxStatus 读取 outbox 运维状态（只读）。
func LoadPBRStatsOutboxStatus(db *gorm.DB) (PBRStatsOutboxStatus, error) {
	var status PBRStatsOutboxStatus
	if db == nil {
		return status, errors.New("database not initialized")
	}
	var pending, dead int64
	if err := db.Model(&PBRStatsOutbox{}).Where("attempts < ?", pbrStatsOutboxMaxAttempts).Count(&pending).Error; err != nil {
		return status, err
	}
	if err := db.Model(&PBRStatsOutbox{}).Where("attempts >= ?", pbrStatsOutboxMaxAttempts).Count(&dead).Error; err != nil {
		return status, err
	}
	status.Pending, status.Dead = pending, dead
	var oldest struct {
		Pending int64
		Dead    int64
	}
	if err := db.Model(&PBRStatsOutbox{}).
		Select("COALESCE(MIN(CASE WHEN attempts < ? THEN created_at END), 0) AS pending, "+
			"COALESCE(MIN(CASE WHEN attempts >= ? THEN created_at END), 0) AS dead",
			pbrStatsOutboxMaxAttempts, pbrStatsOutboxMaxAttempts).
		Scan(&oldest).Error; err != nil {
		return status, err
	}
	status.OldestPendingAt, status.OldestDeadAt = oldest.Pending, oldest.Dead
	var last PBRStatsOutbox
	if err := db.Where("last_error <> ''").
		Order("id desc").First(&last).Error; err == nil {
		status.LastError = last.LastError
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return status, err
	}
	return status, nil
}

// RetryPBRStatsOutboxDead 把死信重置为可重试；id=0 表示全部死信。
func RetryPBRStatsOutboxDead(db *gorm.DB, id int64) (int64, error) {
	if db == nil {
		return 0, errors.New("database not initialized")
	}
	query := db.Model(&PBRStatsOutbox{}).Where("attempts >= ?", pbrStatsOutboxMaxAttempts)
	if id > 0 {
		query = query.Where("id = ?", id)
	}
	result := query.Updates(map[string]any{
		"attempts": 0, "next_attempt_at": 0, "last_error": "",
	})
	return result.RowsAffected, result.Error
}

// DiscardPBRStatsOutboxDead 删除死信；id=0 表示全部死信。
// 明细行是聚合的唯一真源，删死信后可由 POST /api/stats/rebuild 完整恢复。
func DiscardPBRStatsOutboxDead(db *gorm.DB, id int64) (int64, error) {
	if db == nil {
		return 0, errors.New("database not initialized")
	}
	query := db.Where("attempts >= ?", pbrStatsOutboxMaxAttempts)
	if id > 0 {
		query = query.Where("id = ?", id)
	}
	result := query.Delete(&PBRStatsOutbox{})
	return result.RowsAffected, result.Error
}
