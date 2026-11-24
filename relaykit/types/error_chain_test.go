package types

import (
	"context"
	"errors"
	"testing"
)

// 错误链必须穿过展示层：分类依赖 errors.Is(DeadlineExceeded/Canceled)（ADR 0013）。
func TestHideErrMsgPreservesCauseChain(t *testing.T) {
	cause := context.DeadlineExceeded
	e := NewError(cause, ErrorCodeDoRequestFailed, ErrOptionWithHideErrMsg("upstream error: do request failed"))

	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("display-message option must not destroy the cause chain")
	}
	if got := e.MaskSensitiveError(); got != "upstream error: do request failed" {
		t.Fatalf("MaskSensitiveError should return display message, got %q", got)
	}
	if got := e.ToOpenAIError().Message; got != "upstream error: do request failed" {
		t.Fatalf("ToOpenAIError should use display message, got %q", got)
	}
}

func TestSetMessagePreservesCauseChain(t *testing.T) {
	e := NewError(context.Canceled, ErrorCodeDoRequestFailed)
	e.SetMessage("request canceled by client")

	if !errors.Is(e, context.Canceled) {
		t.Fatal("SetMessage must not replace the underlying error")
	}
	if got := e.ToClaudeError().Message; got != "request canceled by client" {
		t.Fatalf("ToClaudeError should use display message, got %q", got)
	}
}

func TestNewOpenAIErrorPreservesCauseChain(t *testing.T) {
	e := NewOpenAIError(context.DeadlineExceeded, ErrorCodeDoRequestFailed, 500)

	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("NewOpenAIError must keep the original cause for classification")
	}
	if e.GetErrorCode() != ErrorCodeDoRequestFailed {
		t.Fatalf("error code must remain %q, got %q", ErrorCodeDoRequestFailed, e.GetErrorCode())
	}
}

func TestDisplayMessageTakesPrecedenceWithoutCause(t *testing.T) {
	e := WithOpenAIError(OpenAIError{Message: "hidden upstream body", Type: "upstream_error"}, 429)
	e.SetMessage("rate limited")

	if got := e.ToOpenAIError().Message; got != "rate limited" {
		t.Fatalf("display message should win, got %q", got)
	}
	if got := e.MaskSensitiveError(); got != "rate limited" {
		t.Fatalf("MaskSensitiveError should return display message, got %q", got)
	}
}
