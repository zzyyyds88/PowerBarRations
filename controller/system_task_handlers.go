package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/filestore"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/service"
	"github.com/zzyyyds88/PowerBarRations/setting/operation_setting"
)

// RegisterScheduledSystemTasks wires the periodic channel test and upstream
// model update jobs into the system task framework so a DB
// lease dedups execution across multiple master instances and each run is
// recorded as one task row. Call this before service.StartSystemTaskRunner.
func RegisterScheduledSystemTasks() {
	service.RegisterSystemTaskHandler(channelTestHandler{})
	service.RegisterSystemTaskHandler(modelUpdateHandler{})
	service.RegisterSystemTaskHandler(fileCleanupHandler{})
	service.RegisterSystemTaskHandler(asyncObjectCleanupHandler{})
}

// fileCleanupHandler 定期清理网关侧 Anthropic Files 的过期对象（ADR 0011）。
// 只删行 + 尽力删字节；失败仅告警，不阻塞后续任务。
type fileCleanupHandler struct{}

func (fileCleanupHandler) Type() string { return model.SystemTaskTypeFileCleanup }

func (fileCleanupHandler) Enabled() bool { return true }

// 一小时一轮：缺省 TTL 是 24h、上限 7d，小时级延迟对配额回收足够，
// 又不至于让过期对象长期占盘。
func (fileCleanupHandler) Interval() time.Duration { return time.Hour }

func (fileCleanupHandler) NewPayload() any { return nil }

func (fileCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	now := time.Now()
	ids, err := model.PBRFileExpiredIDs(now)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	deleted, err := model.DeleteExpiredPBRFiles(now, ids)
	for _, id := range ids {
		// 行已删，字节删失败只是残留；下一轮会再试（Remove 对不存在的路径视为成功，
		// 因此这里只对真实错误告警）。
		if removeErr := filestore.Remove(id); removeErr != nil {
			common.SysLog(fmt.Sprintf("file_cleanup: remove bytes for %s failed: %v", id, removeErr))
		}
	}
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed,
			map[string]any{"expired": len(ids), "deleted": deleted}, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded,
		map[string]any{"expired": len(ids), "deleted": deleted}, nil)
}

// asyncObjectCleanupHandler 定期清理「上游已过期超过保留期」的 OpenAI 异步对象
// 元数据行（ADR 0010 承诺"清理走既有系统任务"，此前 model.CleanupExpiredAsyncObjects
// 在非测试代码中零调用点，pbr_async_objects 只增不减）。
type asyncObjectCleanupHandler struct{}

func (asyncObjectCleanupHandler) Type() string { return model.SystemTaskTypeAsyncTaskPoll }

func (asyncObjectCleanupHandler) Enabled() bool { return true }

// 一天一轮：保留期是 30 天，日级粒度足够；也避免每小时扫一张只增不删的表。
func (asyncObjectCleanupHandler) Interval() time.Duration { return 24 * time.Hour }

func (asyncObjectCleanupHandler) NewPayload() any { return nil }

func (asyncObjectCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	deleted, err := model.CleanupExpiredAsyncObjects(time.Now())
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed,
			map[string]any{"deleted": deleted}, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded,
		map[string]any{"deleted": deleted}, nil)
}

// channelTestHandler runs the scheduled "test all channels" job. Enablement and
// cadence still come from the monitor settings; only the execution path moved
// into the system task runner.
type channelTestHandler struct{}

func (channelTestHandler) Type() string { return model.SystemTaskTypeChannelTest }

func (channelTestHandler) Enabled() bool {
	return operation_setting.GetMonitorSetting().AutoTestChannelEnabled
}

func (channelTestHandler) Interval() time.Duration {
	minutes := operation_setting.GetMonitorSetting().AutoTestChannelMinutes
	if minutes <= 0 {
		minutes = 10
	}
	return time.Duration(minutes * float64(time.Minute))
}

func (channelTestHandler) NewPayload() any { return nil }

// channelTestTaskPayload controls one channel_test run. A nil/empty payload is a
// scheduled run, which uses the configured monitor ChannelTestMode and does not
// notify. A manual "test all channels" trigger sets Mode=scheduled_all and
// Notify=true to reproduce the legacy manual behavior (test every channel and
// notify root on completion).
type channelTestTaskPayload struct {
	Mode   string `json:"mode,omitempty"`
	Notify bool   `json:"notify,omitempty"`
}

func (channelTestHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := channelTestTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	summary, err := runChannelTestTask(ctx, payload.Mode, payload.Notify, service.NewSystemTaskProgressReporter(task, runnerID))
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// modelUpdateHandler runs the scheduled upstream model update detection job.
type modelUpdateHandler struct{}

func (modelUpdateHandler) Type() string { return model.SystemTaskTypeModelUpdate }

func (modelUpdateHandler) Enabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", true)
}

func (modelUpdateHandler) Interval() time.Duration {
	intervalMinutes := common.GetEnvOrDefault(
		"CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES",
		channelUpstreamModelUpdateTaskDefaultIntervalMinutes,
	)
	if intervalMinutes < 1 {
		intervalMinutes = channelUpstreamModelUpdateTaskDefaultIntervalMinutes
	}
	return time.Duration(intervalMinutes) * time.Minute
}

func (modelUpdateHandler) NewPayload() any { return nil }

// modelUpdateTaskPayload controls one model_update run. A scheduled run
// (Manual=false) respects the per-channel minimum check interval and may
// auto-apply detected models when a channel has auto-sync enabled. A manual
// "detect all" trigger sets Manual=true to reproduce the legacy detect-all
// semantics: force a re-check regardless of the interval and never auto-apply,
// so the admin reviews and applies changes explicitly.
type modelUpdateTaskPayload struct {
	Manual bool `json:"manual,omitempty"`
}

func (modelUpdateHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := modelUpdateTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	summary := runChannelUpstreamModelUpdateTaskOnce(ctx, payload.Manual, !payload.Manual, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

func finishSystemTaskHandler(task *model.SystemTask, runnerID string, status model.SystemTaskStatus, result any, runErr error) {
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage); err != nil {
		common.SysLog(fmt.Sprintf("system task %s failed to persist result: %v", task.TaskID, err))
	}
}
