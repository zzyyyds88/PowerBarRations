package model

import "strings"

// ContextWindowForModel resolves the context window of a request's model from
// the catalog metadata. Candidates are tried in order (member-selected model,
// upstream real name, request model); the first non-empty result wins. The
// catalog's existing rule precedence (exact > prefix > suffix > contains) is
// reused, so a prefix rule such as "claude-" can set a window for a family.
//
// 0 means unknown; the caller must not turn it into a 0% fill ratio.
func ContextWindowForModel(names ...string) int {
	if DB == nil {
		return 0
	}
	candidates := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return 0
	}
	var records []Model
	if err := DB.Select("model_name", "name_rule", "context_window").
		Where("context_window > 0").Find(&records).Error; err != nil {
		return 0
	}
	resolved := resolveModelMetadata(records, candidates)
	for _, name := range candidates {
		if metadata := resolved[name]; metadata != nil && metadata.ContextWindow > 0 {
			return metadata.ContextWindow
		}
	}
	return 0
}
