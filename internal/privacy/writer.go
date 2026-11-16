package privacy

import (
	"net/http"
	"sync"

	"github.com/zzyyyds88/PowerBarRations/internal/privacy/redact"

	"github.com/gin-gonic/gin"
)

// ResponseWriter restores placeholders in what a relay writes. It embeds
// the gin writer so Flush, CloseNotify, Hijack and Status keep working;
// only the bytes are transformed. Finish must run after every write,
// including an error body written by a deferred handler.
type ResponseWriter struct {
	gin.ResponseWriter
	rw   *redact.Writer
	once sync.Once
}

// NewResponseWriter wraps w. done finishes the stream; it is safe to call
// more than once.
func NewResponseWriter(w gin.ResponseWriter) (*ResponseWriter, func()) {
	rw := redact.NewWriter(w)
	out := &ResponseWriter{ResponseWriter: w, rw: rw}
	return out, func() { out.Finish() }
}

func (w *ResponseWriter) Write(p []byte) (int, error) { return w.rw.Write(p) }

func (w *ResponseWriter) WriteString(s string) (int, error) {
	return w.rw.Write([]byte(s))
}

func (w *ResponseWriter) WriteHeader(code int) { w.rw.WriteHeader(code) }

func (w *ResponseWriter) Flush() { w.rw.Flush() }

// Unwrap is for http.ResponseController.
func (w *ResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Finish writes what the stream still holds. It never reports its own
// failure: the response has already begun or not, and masking must not
// turn a relay error into a second one.
func (w *ResponseWriter) Finish() {
	w.once.Do(func() { w.rw.Finish() })
}

// Put is how many places of each placeholder the answer restored, for
// diagnostics.
func (w *ResponseWriter) Put() map[string]int { return w.rw.Put() }
