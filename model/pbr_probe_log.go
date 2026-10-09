package model

import (
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
)

// PBRProbeLog 渠道探活记录：每次 `POST /api/channels/{name}/test` 落一条，
// 供渠道健康详情的"探活历史"（api-spec §6.12）。
//
// 为什么单独一张表而不是复用 request_logs：探活不是模型面请求，混进明细日志会
// 污染用量列表与小时聚合（探活会被计成该渠道的一次"请求"）；历史又必须成对地
// 可查（"这个渠道最近到底通不通"）。因此单表、只存元数据、**不进聚合**。
//
// 有界：每渠道只保留最近 ProbeLogKeepPerChannel 条（写入时滚动裁剪），
// 另有 `POST /api/logs/prune` 按时间清理（design-v1 §16.6：不许无界增长）。
type PBRProbeLog struct {
	Id            int    `json:"id" gorm:"primaryKey"`
	Ts            int64  `json:"ts" gorm:"bigint;index:idx_pbr_probe_channel_ts,priority:2"`
	ChannelId     int    `json:"channel_id" gorm:"index:idx_pbr_probe_channel_ts,priority:1"`
	ChannelName   string `json:"channel" gorm:"type:varchar(191);index"`
	UpstreamModel string `json:"upstream_model" gorm:"type:varchar(191)"`
	Status        string `json:"status" gorm:"type:varchar(16)"` // success | failed
	DurationMs    int64  `json:"duration_ms"`
	HTTPStatus    int    `json:"http_status"`
	ErrorKind     string `json:"error_kind,omitempty" gorm:"type:varchar(32)"`
	Msg           string `json:"msg,omitempty" gorm:"type:varchar(512)"`
}

func (PBRProbeLog) TableName() string { return "pbr_probe_logs" }

// ProbeLogKeepPerChannel 每渠道保留的探活记录条数上限（api-spec §6.12）。
const ProbeLogKeepPerChannel = 200

// ProbeLogMsgLimit 探活记录里上游报文的最大保留长度（超长截断，只存元数据）。
const ProbeLogMsgLimit = 500

// WriteProbeLog 写一条探活记录，并把该渠道的历史滚动裁剪到上限。
// 失败返回错误由调用方决定是否容忍（探活结论本身不因留痕失败而改变）。
func WriteProbeLog(entry *PBRProbeLog) error {
	if entry == nil {
		return nil
	}
	if entry.Ts == 0 {
		entry.Ts = time.Now().Unix()
	}
	if err := DB.Create(entry).Error; err != nil {
		return err
	}
	// 滚动裁剪：找到该渠道第 N+1 新的记录 id 作为分界，删掉更旧的。
	var boundary []int
	if err := DB.Model(&PBRProbeLog{}).
		Where("channel_id = ?", entry.ChannelId).
		Order("id desc").Offset(ProbeLogKeepPerChannel).Limit(1).
		Pluck("id", &boundary).Error; err != nil {
		return err
	}
	if len(boundary) == 0 {
		return nil
	}
	return DB.Where("channel_id = ? AND id <= ?", entry.ChannelId, boundary[0]).
		Delete(&PBRProbeLog{}).Error
}

// RecentProbeLogs 取某渠道最近 limit 条探活记录（新 → 旧）。
func RecentProbeLogs(channelId, limit int) ([]PBRProbeLog, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows []PBRProbeLog
	err := DB.Where("channel_id = ?", channelId).
		Order("id desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// LatestProbeLogByChannel 取每个渠道的最近一条探活记录（渠道健康汇总用）。
// 渠道数很小（个位数~几十），按渠道各取一条即可，不做复杂窗口函数。
func LatestProbeLogByChannel(channelIds []int) (map[int]PBRProbeLog, error) {
	out := map[int]PBRProbeLog{}
	for _, id := range channelIds {
		var row PBRProbeLog
		err := DB.Where("channel_id = ?", id).Order("id desc").Limit(1).Find(&row).Error
		if err != nil {
			return nil, err
		}
		if row.Id != 0 {
			out[id] = row
		}
	}
	return out, nil
}

// PruneProbeLogsBefore 清理 before（Unix 秒）之前的探活记录，返回删除行数。
func PruneProbeLogsBefore(before int64) (int64, error) {
	if before <= 0 {
		return 0, nil
	}
	res := DB.Where("ts < ?", before).Delete(&PBRProbeLog{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// CountProbeLogsBefore 统计 before（Unix 秒）之前的探活记录条数（dry-run 用）。
func CountProbeLogsBefore(before int64) (int64, error) {
	if before <= 0 {
		return 0, nil
	}
	var count int64
	err := DB.Model(&PBRProbeLog{}).Where("ts < ?", before).Count(&count).Error
	return count, err
}

// LogProbeWriteFailure 探活留痕失败只记一行日志，不改变探活结论（历史是辅助观测）。
func LogProbeWriteFailure(err error) {
	if err == nil {
		return
	}
	common.SysError("write probe log failed: " + err.Error())
}
