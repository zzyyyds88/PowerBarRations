package helper

import (
	"errors"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"

	"github.com/gin-gonic/gin"
)

// 流式暂存缓冲（routing-spec §3 第 6 步内容帧口径）。
//
// 提交点 = 向客户端写出第一个**内容帧**（§4.1.1 逐格式判定）。角色帧、空增量、
// message_start 等开场帧先在这里有界暂存：不写下游、不计提交、不打首字。
// 第一个内容帧到达时按原顺序 flush 暂存帧再写内容帧——客户端收到的字节序
// 与健康流完全一致。内容帧之前出现流内错误帧或 EOF ⇒ 未提交：暂存帧随
// 成员失败一起丢弃，故障转移正常进行，客户端不会收到垃圾 200。
//
// 上界 64 KiB：超出立即 flush + 提交兜底（design §16.6——缓冲必须有界），
// 防止某些非标准上游持续吐非内容帧把内存吃穿。

const streamWriteErrorKey = "pbr_stream_write_error"

func recordStreamWriteError(c *gin.Context, err error) {
	if c == nil || err == nil {
		return
	}
	c.Set(streamWriteErrorKey, err)
}

func streamWriteError(c *gin.Context) error {
	if c == nil {
		return nil
	}
	value, ok := c.Get(streamWriteErrorKey)
	if !ok {
		return nil
	}
	err, _ := value.(error)
	return err
}

// writeStreamEvent 写一帧 SSE（event 与 data 两段，按既有 helper 的渲染顺序）。
//
// isContent=false 且尚未提交：两段按序暂存，不写下游、不提交、不 flush；
// 暂存超上界时降级为直接写出并提交（兜底）。
// isContent=true 或已提交：先 flush 暂存帧（保序），再写本帧并提交（首字打点）。
func writeStreamEvent(c *gin.Context, isContent bool, event, data string) (err error) {
	defer func() {
		recordStreamWriteError(c, err)
	}()
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}
	if requestContextDone(c) {
		return errors.New("request context done")
	}

	committed := StreamCommitted(c)
	if !isContent && !committed {
		// 暂存必须保存**渲染后的字节**：c.Render 对 "data:" 开头的片段会补写
		// SSE 空行终止符（\n\n，见 common.writeData），event 段不补。暂存原始
		// 片段会在 flush 后缺终止符，把下一个事件粘进同一个 SSE 块。
		eventRendered, dataRendered := renderEventPieces(event, data)
		if common.HoldStreamFrame(c, eventRendered) && common.HoldStreamFrame(c, dataRendered) {
			return nil
		}
		// 暂存超上界：降级为内容帧处理（flush + 提交），防止无界缓冲。
	}
	if err := common.FlushHeldStreamFrames(c); err != nil {
		return err
	}
	if event != "" {
		if err := customEventOf(event).Render(c.Writer); err != nil {
			return err
		}
	}
	if err := customEventOf(data).Render(c.Writer); err != nil {
		return err
	}
	MarkStreamCommitted(c)
	// 与旧 helper 一致：每帧写完显式 flush（SSE 逐帧下发；响应字节数统计、
	// 测试的 NotifyingWriter 都依赖这次 Flush）。
	return FlushWriter(c)
}

// customEventOf 用既有渲染器生成与旧路径逐字节一致的 SSE 片段。
func customEventOf(data string) common.CustomEvent {
	return common.CustomEvent{Data: data}
}

// renderEventPieces 复刻 common.CustomEvent 的渲染语义：data 段写入后补
// "\n\n"（writeData 仅在片段以 "data" 开头时补终止符），event 段不补。
// 返回的两段与 c.Render 逐字节一致。
func renderEventPieces(event, data string) (string, string) {
	eventRendered := event
	dataRendered := data
	if strings.HasPrefix(data, "data") {
		dataRendered += "\n\n"
	}
	return eventRendered, dataRendered
}

// StringDataGated 带内容判定的通用写入口：isContent=false 且未提交时暂存，
// isContent=true 时 flush 暂存帧并提交。供有类型化判定的调用方使用
// （OpenAI chat 原始透传、Gemini 原生流）。
func StringDataGated(c *gin.Context, str string, isContent bool) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}
	if requestContextDone(c) {
		return errors.New("request context done")
	}
	return writeStreamEvent(c, isContent, "", "data: "+str)
}
