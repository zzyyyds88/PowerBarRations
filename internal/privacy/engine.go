package privacy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zzyyyds88/PowerBarRations/internal/privacy/pii"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy/redact"
)

// Hit is one value masking found, as the log and the page show it: never
// the value itself.
type Hit struct {
	Kind        string `json:"kind"`
	By          string `json:"by"`
	Placeholder string `json:"placeholder"`
	Head        string `json:"head,omitempty"`
	Len         int    `json:"len"`
	N           int    `json:"n"`
	Action      string `json:"action,omitempty"`
}

// Status is the outcome of one request's masking, as mask_status.
type Status string

const (
	StatusOff              Status = "off"
	StatusOK               Status = "ok"
	StatusPartial          Status = "partial"
	StatusModelUnavailable Status = "model_unavailable"
	StatusTruncated        Status = "truncated"
	StatusUnsupported      Status = "unsupported"
	StatusRestoreFailed    Status = "restore_failed"
)

// Result is what masking did to one request.
type Result struct {
	Body    []byte
	Hits    []Hit
	Count   int // places masked; warn is not counted
	Status  Status
	Took    time.Duration
	Blocked bool
}

// ErrBlocked is a request a block action held back. The error names the
// kind and the hint, never the value.
var ErrBlocked = errors.New("privacy masking blocked this request")

// ErrModelUnavailable is a required local model that isn't ready.
var ErrModelUnavailable = errors.New("the required local masking model is unavailable")

// Engine masks with one Settings snapshot. It is created per request so
// the model callback's context can end with the request.
type Engine struct {
	Settings Settings
}

// MaskJSON masks body and records hits. A failure is fail-closed: the
// caller must not send the original bytes.
func (e Engine) MaskJSON(ctx context.Context, body []byte) (res Result, err error) {
	if !e.Settings.ActiveNow() {
		return Result{Body: body, Status: StatusOff}, nil
	}
	start := time.Now()
	defer func() {
		res.Took = time.Since(start)
		if r := recover(); r != nil {
			res = Result{Status: StatusModelUnavailable, Took: time.Since(start)}
			err = fmt.Errorf("%w: %v", ErrUnavailable, r)
		}
	}()

	var (
		seen             []Hit
		index            = map[string]int{}
		modelUnavailable bool
		partial          bool
		truncated        bool
	)
	o := Options(e.Settings, nil)
	o.Seen = func(h redact.Hit) {
		if i, ok := index[h.Placeholder]; ok {
			seen[i].N++
			return
		}
		index[h.Placeholder] = len(seen)
		seen = append(seen, Hit{
			Kind: h.Kind, By: h.By, Placeholder: h.Placeholder,
			Head: h.Head, Len: h.Len, N: 1, Action: h.Action,
		})
	}
	if e.Settings.Model.Enabled && (e.Settings.Secrets || e.Settings.Personal) {
		if !pii.Installed() {
			modelUnavailable = true
			if e.Settings.Model.Required {
				return Result{Status: StatusModelUnavailable}, ErrModelUnavailable
			}
		} else {
			pii.Ensure()
			modelCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			o.Model = func(texts []string) [][]redact.Found {
				got, ferr := pii.Find(modelCtx, texts)
				if ferr != nil {
					modelUnavailable = true
					return make([][]redact.Found, len(texts))
				}
				partial = partial || got.Partial
				truncated = truncated || got.Truncated
				return got.Spans
			}
		}
	}
	out, _ := redact.MaskJSON(body, o)
	for i := range seen {
		if seen[i].Action != redact.Warn {
			res.Count += seen[i].N
		}
		if seen[i].Action == redact.Block {
			res.Blocked = true
		}
	}
	res.Body = out
	res.Hits = seen
	switch {
	case res.Blocked:
		res.Status = StatusOK
	case modelUnavailable:
		res.Status = StatusModelUnavailable
		if e.Settings.Model.Required {
			return res, ErrModelUnavailable
		}
	case truncated:
		res.Status = StatusTruncated
	case partial:
		res.Status = StatusPartial
	default:
		res.Status = StatusOK
	}
	if res.Blocked {
		return res, ErrBlocked
	}
	return res, nil
}

// Preview masks s without remembering any value or placeholder, for the
// console's try-it box. It never registers a value in the vault.
func (e Engine) Preview(ctx context.Context, s string) (string, []Hit, error) {
	var seen []Hit
	index := map[string]int{}
	o := Options(e.Settings, nil)
	if !e.Settings.ActiveNow() {
		o.Secrets, o.Personal = true, true
	}
	o.Seen = func(h redact.Hit) {
		if i, ok := index[h.Placeholder]; ok {
			seen[i].N++
			return
		}
		index[h.Placeholder] = len(seen)
		seen = append(seen, Hit{Kind: h.Kind, By: h.By, Placeholder: h.Placeholder,
			Head: h.Head, Len: h.Len, N: 1, Action: h.Action})
	}
	if e.Settings.Model.Enabled && (e.Settings.Secrets || e.Settings.Personal) && pii.Installed() {
		modelCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		pii.Ensure()
		o.Model = func(texts []string) [][]redact.Found {
			got, err := pii.Find(modelCtx, texts)
			if err != nil {
				return make([][]redact.Found, len(texts))
			}
			return got.Spans
		}
	}
	return redact.Preview(s, o), seen, nil
}

// Restore puts the values masked before back in s. escaped is for text
// that is JSON of its own.
func Restore(s string, escaped bool) string { return redact.Restore(s, escaped) }
