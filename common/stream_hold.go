package common

import (
	"bytes"
	"errors"

	"github.com/zzyyyds88/PowerBarRations/constant"

	"github.com/gin-gonic/gin"
)

// 流式开场帧有界暂存（routing-spec §3 第 6 步内容帧口径）。
//
// 提交点 = 第一个内容帧；角色/开场帧先在这里暂存：不写下游、不计提交、不打首字。
// 内容帧到达时按原顺序 flush 后随内容帧提交；成员失败/换人时丢弃。
// 上界 64 KiB，超出立即降级为"写出并提交"兜底（design §16.6 缓冲必须有界）。

const MaxHeldStreamBytes = 64 << 10

type StreamHoldState struct {
	buf  bytes.Buffer
	held bool
}

func heldStreamState(c *gin.Context) *StreamHoldState {
	if existing, ok := c.Get(string(constant.ContextKeyPBRStreamHeld)); ok {
		if st, ok := existing.(*StreamHoldState); ok {
			return st
		}
	}
	st := &StreamHoldState{}
	c.Set(string(constant.ContextKeyPBRStreamHeld), st)
	return st
}

// HoldStreamFrame 暂存一段已渲染的 SSE 字节。返回 false 表示不能暂存
// （超上界），调用方需降级为写出并提交。
func HoldStreamFrame(c *gin.Context, rendered string) bool {
	st := heldStreamState(c)
	if st.buf.Len()+len(rendered) > MaxHeldStreamBytes {
		return false
	}
	st.buf.WriteString(rendered)
	st.held = true
	return true
}

// FlushHeldStreamFrames 把暂存字节按原顺序写给客户端；无暂存时为 no-op。
// 只在提交（内容帧写出）时调用。
func FlushHeldStreamFrames(c *gin.Context) error {
	st := heldStreamState(c)
	if !st.held {
		return nil
	}
	if _, err := c.Writer.Write(st.buf.Bytes()); err != nil {
		return err
	}
	st.held = false
	st.buf.Reset()
	return nil
}

// DiscardHeldStreamFrames 丢弃暂存帧。每次成员尝试开始（InitChannelMeta）与
// 成员失败收尾时调用：上一条腿的暂存帧随其失败一并丢弃，绝不泄漏给客户端。
func DiscardHeldStreamFrames(c *gin.Context) {
	st := heldStreamState(c)
	st.buf.Reset()
	st.held = false
}

var ErrStreamHoldOverflow = errors.New("stream hold buffer overflow")
