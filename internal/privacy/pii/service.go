// Package pii runs AstrLink Guard, the local token classifier masking uses
// for what the rules can't find: a name or an address in free text. It is
// the pure-Go CPU path only (magpie's CoreML and Privacy Filter are not
// ported). The model files are downloaded and checked against pinned
// SHA-256 values; nothing is bundled in the binary.
package pii

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy/pii/guard"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy/redact"
)

// Name and revision are the model magpie pins (v0.1.3).
const (
	Name     = "AstrLink Guard"
	Revision = "7216b6a3b897ef903d4b1ba367ae6fb95b0fa5e4"
)

type modelFile struct {
	path string
	size int64
	sha  string
}

var modelFiles = []modelFile{
	{"int8/model_int8.onnx", 330781, "0a070d3113615e33ba3b84d6bd486f663a377c04c4b056e7381732179f51832a"},
	{"int8/model_int8.onnx_data", 37124608, "0ba34216ceead01e0ae2dfd8e46d52b67e7aee80b04f722d9b758613a91c0067"},
	{"int8/tokenizer.json", 1502881, "0cf3b235ef125658015a6c8026a5fe6f28088bf825101a990ec18a10200feef4"},
}

const baseURL = "https://hf-mirror.com/QuantumNous/astrlink-guard/resolve/" + Revision + "/"

// modelBase is where the files are downloaded from. The default is
// hf-mirror.com (the mirror magpie uses where Hugging Face is blocked);
// PBR_PII_MODEL_BASE_URL can point anywhere that serves the same paths,
// for example https://huggingface.co/QuantumNous/astrlink-guard/resolve/<rev>/.
func modelBase() string {
	if v := strings.TrimSpace(os.Getenv("PBR_PII_MODEL_BASE_URL")); v != "" {
		if strings.HasSuffix(v, "/") {
			return v
		}
		return v + "/"
	}
	return baseURL
}

// TotalSize is what enabling the model downloads.
func TotalSize() int64 {
	var n int64
	for _, f := range modelFiles {
		n += f.size
	}
	return n
}

// Dir is where the model's files are kept. PBR_PII_MODEL_DIR wins; else
// the folder beside the SQLite database, so the data volume carries it.
func Dir() string {
	if v := strings.TrimSpace(os.Getenv("PBR_PII_MODEL_DIR")); v != "" {
		return v
	}
	path := common.SQLitePath
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return filepath.Join("privacy", "astrlink-guard", Revision[:12])
	}
	return filepath.Join(filepath.Dir(path), "privacy", "astrlink-guard", Revision[:12])
}

// Files returns the file names Load expects in dir, for diagnostics.
func Files() []string {
	out := make([]string, 0, len(modelFiles))
	for _, f := range modelFiles {
		out = append(out, filepath.Base(f.path))
	}
	return out
}

// Installed reports whether every file is on disk at its pinned size.
func Installed() bool {
	for _, f := range modelFiles {
		fi, err := os.Stat(filepath.Join(Dir(), filepath.Base(f.path)))
		if err != nil || fi.Size() != f.size {
			return false
		}
	}
	return true
}

// ErrNotInstalled is what Find returns while the model isn't downloaded.
var ErrNotInstalled = errors.New("the local masking model isn't downloaded")

var state struct {
	mu          sync.Mutex
	model       *guard.Model
	loading     bool
	downloading bool
	done        int64
	err         string
	loadMS      int64
}

// Status is what the console shows of the model.
type Status struct {
	Name        string `json:"name"`
	Revision    string `json:"revision"`
	Size        int64  `json:"size"`
	Installed   bool   `json:"installed"`
	Downloading bool   `json:"downloading"`
	Done        int64  `json:"done"`
	Total       int64  `json:"total"`
	Error       string `json:"error,omitempty"`
	Loaded      bool   `json:"loaded"`
	Device      string `json:"device,omitempty"`
	LoadMS      int64  `json:"load_ms,omitempty"`
}

// Now reads the model's status.
func Now() Status {
	state.mu.Lock()
	defer state.mu.Unlock()
	s := Status{
		Name: Name, Revision: Revision, Size: TotalSize(),
		Installed: Installed(), Downloading: state.downloading,
		Done: state.done, Total: TotalSize(), Error: state.err,
		Loaded: state.model != nil, LoadMS: state.loadMS,
	}
	if state.model != nil {
		s.Device = "CPU"
	}
	return s
}

// Ensure starts loading the model if it is installed, and returns at once.
func Ensure() {
	if !Installed() {
		return
	}
	state.mu.Lock()
	if state.model != nil || state.loading {
		state.mu.Unlock()
		return
	}
	state.loading = true
	state.mu.Unlock()
	go func() {
		start := time.Now()
		m, err := guard.Load(Dir())
		state.mu.Lock()
		state.loading = false
		if err != nil {
			state.err = err.Error()
		} else {
			state.model = m
			state.err = ""
			state.loadMS = time.Since(start).Milliseconds()
		}
		state.mu.Unlock()
	}()
}

// Download fetches the model's missing files, verifies each size and
// SHA-256, and loads it. At most one download runs at a time.
func Download(ctx context.Context) error {
	state.mu.Lock()
	if state.downloading {
		state.mu.Unlock()
		return nil
	}
	if Installed() {
		state.mu.Unlock()
		Ensure()
		return nil
	}
	state.downloading = true
	state.done = 0
	state.err = ""
	state.mu.Unlock()

	err := download(ctx)
	state.mu.Lock()
	state.downloading = false
	if err != nil {
		state.err = err.Error()
	} else {
		state.done = TotalSize()
	}
	state.mu.Unlock()
	if err != nil {
		return err
	}
	Ensure()
	return nil
}

func download(ctx context.Context) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	var done int64
	for _, f := range modelFiles {
		name := filepath.Base(f.path)
		path := filepath.Join(dir, name)
		if fi, err := os.Stat(path); err == nil && fi.Size() == f.size {
			done += f.size
			atomic.StoreInt64(&state.done, done)
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelBase()+f.path, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("downloading %s: %w", name, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("downloading %s: HTTP %d", name, resp.StatusCode)
		}
		tmp := path + ".part"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			resp.Body.Close()
			return err
		}
		h := sha256.New()
		var fileDone int64
		n, copyErr := io.Copy(io.MultiWriter(out, h), io.TeeReader(resp.Body, progressWriter{base: done, n: &fileDone}))
		resp.Body.Close()
		closeErr := out.Close()
		if copyErr != nil {
			os.Remove(tmp)
			return fmt.Errorf("downloading %s: %w", name, copyErr)
		}
		if closeErr != nil {
			os.Remove(tmp)
			return closeErr
		}
		if n != f.size {
			os.Remove(tmp)
			return fmt.Errorf("downloading %s: %d bytes, want %d", name, n, f.size)
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != f.sha {
			os.Remove(tmp)
			return fmt.Errorf("downloading %s: sha256 %s, want %s", name, got, f.sha)
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			return err
		}
		done += f.size
		atomic.StoreInt64(&state.done, done)
	}
	return nil
}

type progressWriter struct {
	base int64
	n    *int64
}

func (p progressWriter) Write(b []byte) (int, error) {
	n := atomic.AddInt64(p.n, int64(len(b)))
	atomic.StoreInt64(&state.done, p.base+n)
	return len(b), nil
}

// Result is what the model found, and whether it read all of it.
type Result struct {
	Spans     [][]redact.Found
	Partial   bool
	Truncated bool
}

const (
	perTextLimit = 64 << 10
	textBudget   = 256 << 10
	chunkSize    = 4 << 10
	concurrent   = 2
)

var turn = make(chan struct{}, concurrent)

// Find reads texts with the model, in chunks, until ctx ends. A model not
// installed is ErrNotInstalled; a read that runs out of ctx returns what
// it read with Partial set. Spans are redact.Found, with magpie's label
// mapping.
func Find(ctx context.Context, texts []string) (Result, error) {
	if !Installed() {
		return Result{}, ErrNotInstalled
	}
	m, err := loadedModel(ctx)
	if err != nil {
		return Result{}, err
	}

	select {
	case turn <- struct{}{}:
		defer func() { <-turn }()
	case <-ctx.Done():
		return Result{Partial: true}, ctx.Err()
	}

	out := make([][]redact.Found, len(texts))
	budget := textBudget
	truncated, partial := false, false
	for i, text := range texts {
		if budget <= 0 {
			truncated = true
			break
		}
		limit := min(perTextLimit, budget)
		if len(text) > limit {
			text = text[:validCut(text, limit)]
			truncated = true
		}
		budget -= len(text)
		if spans, ok, err := findOne(ctx, m, text); err != nil {
			partial = true
			if len(spans) > 0 {
				out[i] = spans
			}
			break
		} else if ok {
			out[i] = spans
		}
	}
	return Result{Spans: out, Partial: partial, Truncated: truncated}, nil
}

// loadedModel waits for the model's load, starting one if none is under
// way. A load that failed is an error, not "not downloaded": the caller
// must report the failure rather than silently fall back.
func loadedModel(ctx context.Context) (*guard.Model, error) {
	state.mu.Lock()
	if state.model != nil {
		m := state.model
		state.mu.Unlock()
		return m, nil
	}
	if !state.loading {
		state.mu.Unlock()
		Ensure()
		state.mu.Lock()
	}
	for {
		if state.model != nil {
			m := state.model
			state.mu.Unlock()
			return m, nil
		}
		if !state.loading {
			err := state.err
			state.mu.Unlock()
			if err != "" {
				return nil, fmt.Errorf("local masking model failed to load: %s", err)
			}
			return nil, ErrNotInstalled
		}
		state.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
		state.mu.Lock()
	}
}

func validCut(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

// cache keeps a text's spans, by its digest, so a conversation's history
// is read once. Bounded; least recently used goes first.
var cache struct {
	mu    sync.Mutex
	items map[[32]byte]*list.Element
	order *list.List
}

type cacheItem struct {
	key   [32]byte
	spans []redact.Found
}

const cacheMax = 512

func cacheGet(key [32]byte) ([]redact.Found, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.items == nil {
		return nil, false
	}
	if e, ok := cache.items[key]; ok {
		cache.order.MoveToFront(e)
		return e.Value.(cacheItem).spans, true
	}
	return nil, false
}

func cachePut(key [32]byte, spans []redact.Found) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.items == nil {
		cache.items = map[[32]byte]*list.Element{}
		cache.order = list.New()
	}
	if _, ok := cache.items[key]; ok {
		return
	}
	e := cache.order.PushFront(cacheItem{key, spans})
	cache.items[key] = e
	for cache.order.Len() > cacheMax {
		last := cache.order.Back()
		cache.order.Remove(last)
		delete(cache.items, last.Value.(cacheItem).key)
	}
}

func findOne(ctx context.Context, m *guard.Model, text string) ([]redact.Found, bool, error) {
	var out []redact.Found
	seen := map[[3]any]bool{}
	add := func(start int, spans []guard.Span) {
		for _, s := range spans {
			label := guardLabel(s.Label)
			key := [3]any{start + s.Start, start + s.End, label}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, redact.Found{
				Start: start + s.Start,
				End:   start + s.End,
				Label: label,
				Score: s.Score,
			})
		}
	}
	for start := 0; start < len(text); {
		select {
		case <-ctx.Done():
			return out, false, ctx.Err()
		default:
		}
		end := min(start+chunkSize, len(text))
		end = validCut(text, end)
		if end <= start {
			break
		}
		chunk := text[start:end]
		sum := sha256.Sum256([]byte(chunk))
		if spans, ok := cacheGet(sum); ok {
			add(start, fromFound(spans))
		} else {
			spans := m.Find(chunk)
			cachePut(sum, toFound(spans))
			add(start, spans)
		}
		if end == len(text) {
			break
		}
		// A 512-byte overlap keeps a name or an address split by the cut
		// readable; duplicate spans from it are removed above.
		next := end - 512
		if next <= start {
			next = end
		}
		start = next
	}
	return out, true, nil
}

// toFound and fromFound keep one chunk's spans in redact.Found, so a
// chunk read before is reused even when the request that read it ran out
// of its model budget.
func toFound(spans []guard.Span) []redact.Found {
	out := make([]redact.Found, 0, len(spans))
	for _, s := range spans {
		out = append(out, redact.Found{Start: s.Start, End: s.End, Label: guardLabel(s.Label), Score: s.Score})
	}
	return out
}

func fromFound(spans []redact.Found) []guard.Span {
	out := make([]guard.Span, 0, len(spans))
	for _, s := range spans {
		out = append(out, guard.Span{Start: s.Start, End: s.End, Label: s.Label, Score: s.Score})
	}
	return out
}

var guardLabels = map[string]string{
	"private_person":  "private_person",
	"private_address": "private_address",
	"phone":           "private_phone",
	"email":           "private_email",
	"account":         "account_number",
	"payment_card":    "account_number",
	"common_secret":   "secret",
	"url":             "private_url",
	"ip_address":      "ip_address",
	"private_date":    "private_date",
}

func guardLabel(l string) string {
	if n, ok := guardLabels[l]; ok {
		return n
	}
	return l
}
