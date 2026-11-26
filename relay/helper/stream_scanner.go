package helper

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/logger"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/service"
	"github.com/zzyyyds88/PowerBarRations/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10  // 64KB (64*1024)
	DefaultMaxScannerBufferSize = 128 << 20 // 128MB default SSE buffer size
	DefaultPingInterval         = 10 * time.Second
	// defaultStreamWriteTimeout bounds a single blocked write to a slow client so
	// cleanup's wg.Wait has a deterministic exit boundary.
	defaultStreamWriteTimeout = 30 * time.Second
)

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

// NewStreamScanner shares relay scanner configuration. Callers buffering bounded
// task state may additionally cap a line without increasing the configured limit.
func NewStreamScanner(reader io.Reader, maxBytes ...int) *bufio.Scanner {
	limit := getScannerBufferSize()
	if len(maxBytes) > 0 && maxBytes[0] > 0 {
		limit = min(limit, maxBytes[0])
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, min(InitialScannerBufferSize, limit)), limit)
	return scanner
}

func copyCodexSSEHeaders(c *gin.Context, resp *http.Response) {
	if c == nil || c.Writer == nil || resp == nil {
		return
	}
	// codex
	for _, name := range []string{"X-Reasoning-Included", "X-Codex-Turn-State"} {
		values := resp.Header.Values(name)
		if !service.ShouldCopyUpstreamHeader(c, name, values) {
			continue
		}
		for _, value := range values {
			if value != "" {
				c.Writer.Header().Add(name, value)
			}
		}
	}
}

// ExtendWriteDeadline pushes the connection write deadline forward before each
// stream write. Writers that do not support deadlines (for example httptest
// recorders) are ignored; all other failures are returned to the stream
// supervisor so it can terminate the stream exactly once.
func ExtendWriteDeadline(c *gin.Context) error {
	return extendWriteDeadline(c, defaultStreamWriteTimeout)
}

func extendWriteDeadline(c *gin.Context, timeout time.Duration) error {
	if c == nil || c.Writer == nil {
		return errors.New("stream response writer is nil")
	}
	if timeout <= 0 {
		return errors.New("stream write timeout must be positive")
	}
	err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(timeout))
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult)) {
	streamScannerHandler(c, resp, info, dataHandler, defaultStreamWriteTimeout)
}

func streamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult), writeTimeout time.Duration) {
	if resp == nil || dataHandler == nil {
		return
	}

	// 无条件新建 StreamStatus
	info.StreamStatus = relaycommon.NewStreamStatus()

	ctx, cancel := context.WithCancel(context.Background())

	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second
	var (
		stopChan    = make(chan bool, 1)
		abortChan   = make(chan struct{})
		scanner     = NewStreamScanner(resp.Body)
		ticker      *time.Ticker
		pingTicker  *time.Ticker
		writeMutex  sync.Mutex     // Mutex to protect concurrent writes
		wg          sync.WaitGroup // 用于等待所有 goroutines 退出
		cleanupOnce sync.Once
		stopOnce    sync.Once
		abortOnce   sync.Once
	)

	stop := func() {
		stopOnce.Do(func() {
			cancel()
			close(stopChan)
		})
	}

	// abort 用于异常出口：除停止 scanner 外，还通知 data worker 丢弃队列中
	// 尚未写出的帧。正常 [DONE]/EOF 只调用 stop，让 data worker 按序 drain。
	abort := func() {
		abortOnce.Do(func() {
			close(abortChan)
		})
		stop()
	}

	generalSettings := operation_setting.GetGeneralSetting()
	pingEnabled := generalSettings.PingIntervalEnabled && !info.DisablePing
	pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}

	// 非正 idle timeout 表示禁用该计时器；time.NewTicker 会 panic，不能直接把
	// 零/负值传给它。
	if streamingTimeout > 0 {
		ticker = time.NewTicker(streamingTimeout)
	}
	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
	}

	logger.LogDebug(c, "relay timeout seconds: %d", common.RelayTimeout)
	logger.LogDebug(c, "relay max idle conns: %d", common.RelayMaxIdleConns)
	logger.LogDebug(c, "relay max idle conns per host: %d", common.RelayMaxIdleConnsPerHost)
	logger.LogDebug(c, "streaming timeout seconds: %d", int64(streamingTimeout.Seconds()))
	logger.LogDebug(c, "ping interval seconds: %d", int64(pingInterval.Seconds()))

	cleanup := func() {
		cleanupOnce.Do(func() {
			// stop 必须先于 Body.Close/Wait：所有 goroutine 都能在同一轮 select
			// 中观察到退出信号，scanner 的阻塞读则由 Body.Close 解除。
			stop()
			if resp.Body != nil {
				_ = resp.Body.Close()
			}

			if ticker != nil {
				ticker.Stop()
			}
			if pingTicker != nil {
				pingTicker.Stop()
			}

			wg.Wait()
		})
	}
	// Ensure gin.Context is not returned to Gin's pool while any stream goroutine can still use it.
	defer cleanup()

	scanner.Split(bufio.ScanLines)
	copyCodexSSEHeaders(c, resp)
	SetEventStreamHeaders(c)

	ctx = context.WithValue(ctx, "stop_chan", stopChan)

	// Handle ping data sending with improved error handling.
	if pingEnabled && pingTicker != nil {
		wg.Add(1)
		submitted := common.RelayCtxGo(ctx, func() {
			defer func() {
				if r := recover(); r != nil {
					logger.LogError(c, fmt.Sprintf("ping goroutine panic: %v", r))
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("ping panic: %v", r))
					abort()
				}
				logger.LogDebug(c, "ping goroutine exited")
				wg.Done()
			}()

			for {
				select {
				case <-pingTicker.C:
					var err error
					func() {
						writeMutex.Lock()
						defer writeMutex.Unlock()
						err = extendWriteDeadline(c, writeTimeout)
						if err == nil {
							err = PingData(c)
						}
					}()
					if err != nil {
						logger.LogError(c, "ping data error: "+err.Error())
						if c.Request.Context().Err() != nil {
							info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
						} else {
							info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
						}
						abort()
						return
					}
					logger.LogDebug(c, "ping data sent")
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				case <-c.Request.Context().Done():
					return
				}
			}
		})
		if !submitted {
			wg.Done()
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, fmt.Errorf("relay executor queue is full"))
			abort()
		}
	}

	dataChan := make(chan string, 10)

	wg.Add(1)
	submitted := common.RelayCtxGo(ctx, func() {
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("data handler goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
				abort()
			}
			stop()
			wg.Done()
		}()
		sr := newStreamResult(info.StreamStatus)
		for data := range dataChan {
			select {
			case <-abortChan:
				return
			default:
			}
			sr.reset()
			func() {
				writeMutex.Lock()
				defer writeMutex.Unlock()
				if err := extendWriteDeadline(c, writeTimeout); err != nil {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonWriteFail, err)
					abort()
					return
				}
				dataHandler(data, sr)
			}()
			if err := streamWriteError(c); err != nil {
				if c.Request.Context().Err() != nil {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
				} else {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonWriteFail, err)
				}
				abort()
				return
			}
			if sr.IsStopped() {
				return
			}
		}
	})
	if !submitted {
		wg.Done()
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, fmt.Errorf("relay executor queue is full"))
		abort()
	}

	// Scanner goroutine with improved error handling.
	wg.Add(1)
	submitted = common.RelayCtxGo(ctx, func() {
		defer func() {
			close(dataChan)
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("scanner goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
				abort()
			}
			stop()
			logger.LogDebug(c, "scanner goroutine exited")
			wg.Done()
		}()

		for scanner.Scan() {
			select {
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			default:
			}

			if ticker != nil {
				ticker.Reset(streamingTimeout)
			}
			data := scanner.Text()
			logger.LogDebug(c, "stream scanner data: %s", data)

			if len(data) < 6 {
				continue
			}
			if data[:5] != "data:" && data[:6] != "[DONE]" {
				continue
			}
			data = data[5:]
			data = strings.TrimSpace(data)
			if data == "" {
				continue
			}
			if !strings.HasPrefix(data, "[DONE]") {
				info.SetFirstResponseTime()
				info.ReceivedResponseCount++
				if IsContentFrameRaw(info.RelayFormat, data) {
					info.ReceivedContentCount++
				}

				select {
				case dataChan <- data:
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				}
			} else {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				logger.LogDebug(c, "received [DONE], stopping scanner")
				return
			}
		}

		if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
			// abort 已由真正的触发者（ping/写失败/超时/取消）设置；此时
			// Body.Close 引起的次级 read error 不能覆盖原始终态。
			select {
			case <-abortChan:
				return
			default:
			}
			logger.LogError(c, "scanner error: "+err.Error())
			if c.Request.Context().Err() != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
			} else {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
			}
			abort()
			return
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	})
	if !submitted {
		wg.Done()
		// The scanner owns dataChan. If it could not be submitted, close the
		// channel here so the data worker has a deterministic exit path.
		close(dataChan)
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, fmt.Errorf("relay executor queue is full"))
		abort()
	}

	// 主循环等待完成、idle 超时或客户端取消。取消必须先于 stopChan 检查，
	// 避免 ping/scanner 错误与客户端断开同时就绪时被随机判成成员失败。
	var timeoutChan <-chan time.Time
	if ticker != nil {
		timeoutChan = ticker.C
	}
	if err := c.Request.Context().Err(); err != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		abort()
	} else {
		select {
		case <-timeoutChan:
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
			abort()
		case <-stopChan:
			if err := c.Request.Context().Err(); err != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			}
		case <-c.Request.Context().Done():
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
			abort()
		}
	}

	cleanup()
	if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
	} else {
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}
}
