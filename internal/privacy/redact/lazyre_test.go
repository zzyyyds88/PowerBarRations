// Ported from magpie (https://github.com/yetone/magpie) internal/redact.
// Copyright (c) 2026 yetone. MIT License; see NOTICE.
package redact

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/privacy/lazyre"
)

// TestAllCompile compiles every pattern this package compiles on first
// use, so a bad one fails here and not on a user's request.
func TestAllCompile(t *testing.T) {
	if n := lazyre.MustAll(); n == 0 {
		t.Fatal("no lazy patterns registered")
	}
}
