// Ported from magpie (https://github.com/yetone/magpie) internal/redact.
// Copyright (c) 2026 yetone. MIT License; see NOTICE.
package redact

// Forget lets go of every value masked so far, so that what a test checks
// is what its texts make masked, not what the tests before it masked and
// left known.
func Forget() {
	mu.Lock()
	values = map[string]string{}
	used = map[string]uint64{}
	shapes = map[string]string{}
	scores = map[string]float64{}
	reindexKnown()
	mu.Unlock()
	resetSent()
}
