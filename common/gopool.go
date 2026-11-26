package common

import (
	"context"
	"fmt"
	"sync/atomic"
)

const (
	// 长流式请求会同时占用 scanner/data/ping 三个 worker；上限按“可容纳几十条
	// 并发长流 + 短任务”设置，仍保持有界，避免 32 worker 被少量长流耗尽后
	// 让后续流式请求直接因队列饱和失败。
	relayWorkerCount = 256
	relayQueueSize   = 1024
)

type relayTask struct {
	ctx context.Context
	f   func()
}

type relayExecutor struct {
	tasks   chan relayTask
	dropped atomic.Uint64
}

func newRelayExecutor(workers int, queueSize int) *relayExecutor {
	if workers <= 0 {
		workers = 1
	}
	if queueSize < 0 {
		queueSize = 0
	}
	executor := &relayExecutor{tasks: make(chan relayTask, queueSize)}
	for range workers {
		go executor.run()
	}
	return executor
}

func (e *relayExecutor) run() {
	for task := range e.tasks {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					if task.ctx != nil {
						if stopChan, ok := task.ctx.Value("stop_chan").(chan bool); ok {
							SafeSendBool(stopChan, true)
						}
					}
					SysError(fmt.Sprintf("panic in gopool.RelayPool: %v", recovered))
				}
			}()
			// Always invoke f after a successful submit. Stream wrappers own
			// deferred wg.Done/channel-close work; skipping f because ctx was
			// cancelled after submission would strand that cleanup forever.
			task.f()
		}()
	}
}

// submit is deliberately non-blocking. A saturated request/stream executor must
// reject work instead of growing an unbounded goroutine or task queue.
func (e *relayExecutor) submit(ctx context.Context, f func()) bool {
	if e == nil || f == nil {
		return false
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return false
		default:
		}
	}
	select {
	case e.tasks <- relayTask{ctx: ctx, f: f}:
		return true
	default:
		e.dropped.Add(1)
		return false
	}
}

var relayGoPool = newRelayExecutor(relayWorkerCount, relayQueueSize)

// RelayCtxGo submits f to the bounded relay executor. It returns false when the
// context is already done or the bounded queue is full; callers must handle that
// rejection explicitly. The previous gopool-backed implementation had an
// unbounded linked-list task queue and a math.MaxInt32 worker ceiling.
func RelayCtxGo(ctx context.Context, f func()) bool {
	return relayGoPool.submit(ctx, f)
}

// RelayCtxGoDroppedTotal counts submissions rejected because the bounded relay
// queue was full. Context-cancelled submissions are not counted as saturation.
func RelayCtxGoDroppedTotal() uint64 {
	return relayGoPool.dropped.Load()
}
