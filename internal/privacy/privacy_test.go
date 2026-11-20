package privacy

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/privacy/redact"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	redact.SetKey([]byte("0123456789abcdef0123456789abcdef"))
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "pbr-privacy-test")
	if err == nil {
		_ = os.Setenv("PBR_PII_MODEL_DIR", dir)
		defer os.RemoveAll(dir)
	}
	m.Run()
}

func settingsFor(secrets, personal bool) Settings {
	s := Defaults()
	s.Secrets, s.Personal = secrets, personal
	return s
}

func TestMaskSecretsAndRestore(t *testing.T) {
	engine := Engine{Settings: settingsFor(true, false)}
	body := []byte(`{"messages":[{"role":"user","content":"key sk-proj-abcdEFGH1234ijklMNOP5678qrst"}]}`)
	res, err := engine.MaskJSON(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if res.Count == 0 || len(res.Hits) != 1 || res.Hits[0].Kind != "API_KEY" {
		t.Fatalf("hits = %+v", res.Hits)
	}
	if strings.Contains(string(res.Body), "sk-proj-abcdEFGH1234ijklMNOP5678qrst") {
		t.Fatalf("body still has the key: %s", res.Body)
	}
	got := Restore(string(res.Body), false)
	if !strings.Contains(got, "sk-proj-abcdEFGH1234ijklMNOP5678qrst") {
		t.Fatalf("restore = %s", got)
	}
}

func TestPersonalOffLeavesEmail(t *testing.T) {
	engine := Engine{Settings: settingsFor(true, false)}
	body := []byte(`{"messages":[{"role":"user","content":"mail ops@example.com"}]}`)
	res, err := engine.MaskJSON(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res.Body), "{{EMAIL_") {
		t.Fatalf("personal off but email masked: %s", res.Body)
	}
}

func TestPersonalMaskAndAllow(t *testing.T) {
	s := settingsFor(false, true)
	s.Allow = []string{"ops@acme.io"}
	engine := Engine{Settings: s}
	body := []byte(`{"messages":[{"role":"user","content":"ops@acme.io and dev@corp-mail.net"}]}`)
	res, err := engine.MaskJSON(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Body), "ops@acme.io") {
		t.Fatalf("allowed value masked: %s", res.Body)
	}
	if strings.Contains(string(res.Body), "dev@corp-mail.net") {
		t.Fatalf("email not masked: %s", res.Body)
	}
}

func TestWordsWholeAndAnyCase(t *testing.T) {
	s := settingsFor(false, false)
	s.Words = []string{"Nightjar"}
	s.WordsAnyCase = true
	s.WordsWhole = true
	engine := Engine{Settings: s}
	body := []byte(`{"messages":[{"role":"user","content":"NIGHTJAR and XNIGHTJARY"}]}`)
	res, err := engine.MaskJSON(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Body), "XNIGHTJARY") {
		t.Fatalf("whole-word matched inside a word: %s", res.Body)
	}
	if strings.Contains(string(res.Body), "NIGHTJAR ") {
		t.Fatalf("word not masked: %s", res.Body)
	}
}

func TestBlock(t *testing.T) {
	s := settingsFor(true, false)
	s.Actions = map[string]string{"API_KEY": redact.Block}
	engine := Engine{Settings: s}
	body := []byte(`{"messages":[{"role":"user","content":"sk-proj-abcdEFGH1234ijklMNOP5678qrst"}]}`)
	res, err := engine.MaskJSON(context.Background(), body)
	if !errors.Is(err, ErrBlocked) || !res.Blocked {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if res.Hits[0].Head != "sk-" || res.Hits[0].Len == 0 {
		t.Fatalf("hit hint = %+v", res.Hits[0])
	}
}

func TestWarnSendsAsWritten(t *testing.T) {
	s := settingsFor(true, false)
	s.Actions = map[string]string{"API_KEY": redact.Warn}
	engine := Engine{Settings: s}
	body := []byte(`{"messages":[{"role":"user","content":"sk-proj-abcdEFGH1234ijklMNOP5678qrst"}]}`)
	res, err := engine.MaskJSON(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Body), "sk-proj-abcdEFGH1234ijklMNOP5678qrst") {
		t.Fatalf("warn value was masked: %s", res.Body)
	}
	if res.Count != 0 || len(res.Hits) != 1 || res.Hits[0].Action != redact.Warn {
		t.Fatalf("hits=%+v count=%d", res.Hits, res.Count)
	}
}

func TestModelRequiredUnavailable(t *testing.T) {
	s := settingsFor(true, false)
	s.Model = ModelSettings{Enabled: true, Required: true, MinScore: 0.8}
	engine := Engine{Settings: s}
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	_, err := engine.MaskJSON(context.Background(), body)
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestPreviewDoesNotKeep(t *testing.T) {
	engine := Engine{Settings: settingsFor(true, false)}
	out, hits, err := engine.Preview(context.Background(), "sk-proj-QQQQwwwwEEEE1111RRRR2222")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "{{API_KEY_") || len(hits) != 1 {
		t.Fatalf("out=%s hits=%+v", out, hits)
	}
	// Preview must not make the value restorable: there is no vault entry.
	if got := Restore(out, false); got != out {
		t.Fatalf("preview registered a value: %s", got)
	}
}

func TestResponseWriterRestoresSplitSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	rw, done := NewResponseWriter(c.Writer)

	engine := Engine{Settings: settingsFor(true, false)}
	res, err := engine.MaskJSON(context.Background(), []byte(`{"x":"sk-proj-abcdEFGH1234ijklMNOP5678qrst"}`))
	if err != nil {
		t.Fatal(err)
	}
	placeholder := res.Hits[0].Placeholder
	head, tail := placeholder[:5], placeholder[5:]
	_, _ = rw.Write([]byte("data: {\"t\":\"" + head))
	_, _ = rw.Write([]byte(tail + "\"}\n\n"))
	done()
	if got := rec.Body.String(); !strings.Contains(got, "sk-proj-abcdEFGH1234ijklMNOP5678qrst") {
		t.Fatalf("restored stream = %q", got)
	}
}

// 推荐基线是「打开开关后应该长什么样」，不是「现在就打开」：
// 三个总开关必须保持 false，否则新实例会默认开始改写转发字节。
func TestRecommendedKeepsMaskingOff(t *testing.T) {
	s := Recommended()
	if s.Secrets || s.Personal || s.Model.Enabled {
		t.Fatalf("recommended must not enable masking: secrets=%v personal=%v model=%v",
			s.Secrets, s.Personal, s.Model.Enabled)
	}
	if s.Model.Required {
		t.Fatal("recommended must not require the model")
	}
	if s.Model.MinScore != DefaultModelMinScore {
		t.Fatalf("min_score = %v, want %v", s.Model.MinScore, DefaultModelMinScore)
	}
	if s.ActiveNow() {
		t.Fatal("recommended must be inactive by default")
	}
	n, err := Normalize(s)
	if err != nil {
		t.Fatalf("recommended must normalize: %v", err)
	}
	if n.Active {
		t.Fatal("normalized recommended must stay inactive")
	}
	// 推荐基线必须与 Defaults() 完全一致，避免新实例和「应用推荐配置」漂移。
	d := Defaults()
	if len(d.Kinds) != len(n.Kinds) {
		t.Fatalf("kind count = %d, want %d", len(n.Kinds), len(d.Kinds))
	}
	for id, on := range d.Kinds {
		if n.Kinds[id] != on {
			t.Fatalf("kind %s = %v, want %v", id, n.Kinds[id], on)
		}
	}
}

// 推荐基线勾选的是高置信度类别：ip/home/userhost/bucket 这些高噪声项不勾。
func TestRecommendedKindsFollowCategoryDefaults(t *testing.T) {
	s := Recommended()
	on := map[string]bool{}
	for _, c := range redact.Categories {
		on[c.ID] = c.On
	}
	for id, want := range on {
		if s.Kinds[id] != want {
			t.Fatalf("kind %s = %v, want %v", id, s.Kinds[id], want)
		}
	}
}
