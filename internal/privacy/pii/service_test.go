package pii

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestNotInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PBR_PII_MODEL_DIR", dir)
	if Installed() {
		t.Fatal("empty dir reported installed")
	}
	if _, err := Find(context.Background(), []string{"hello"}); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("err = %v", err)
	}
	s := Now()
	if s.Installed || s.Size <= 0 || s.Revision != Revision {
		t.Fatalf("status = %+v", s)
	}
}

// TestDownloadAndFind is the live model check. It downloads ~38 MB; run it
// with PBR_PII_LIVE=1 and a reachable base URL:
//
//	PBR_PII_LIVE=1 PBR_PII_MODEL_BASE_URL=https://hf-mirror.com/QuantumNous/astrlink-guard/resolve/<rev>/ \
//	  go test ./internal/privacy/pii -run TestDownloadAndFind -v
func TestDownloadAndFind(t *testing.T) {
	if os.Getenv("PBR_PII_LIVE") != "1" {
		t.Skip("set PBR_PII_LIVE=1 to download and run the model")
	}
	dir := t.TempDir()
	t.Setenv("PBR_PII_MODEL_DIR", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := Download(ctx); err != nil {
		t.Fatal(err)
	}
	if !Installed() {
		t.Fatal("download finished but files are not installed")
	}
	if st := Now(); st.Done != st.Total {
		t.Fatalf("progress = %d/%d", st.Done, st.Total)
	}
	res, err := Find(ctx, []string{"联系人：张三，电话 13800138000，邮箱 zhangsan@acme.io。"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Spans) != 1 || len(res.Spans[0]) == 0 {
		t.Fatalf("spans = %+v", res.Spans)
	}
	labels := map[string]bool{}
	for _, s := range res.Spans[0] {
		labels[s.Label] = true
	}
	if !labels["private_phone"] && !labels["private_email"] && !labels["private_person"] {
		t.Fatalf("labels = %v", labels)
	}
}
