// Ported from magpie (https://github.com/yetone/magpie) internal/redact.
// Copyright (c) 2026 yetone. MIT License; see NOTICE.
package redact

import (
	"os"
	"path/filepath"
	"testing"
)

// The tests run in a home of their own, never the user's real files.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pbr-redact-test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", dir)
	_ = os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
