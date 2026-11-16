package privacy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/privacy/redact"
	"github.com/zzyyyds88/PowerBarRations/model"
)

// Option keys. They live in the options table so a restart keeps them; the
// placeholder key is separate (model.OptionPrivacyKey) and is never
// exposed.
const (
	optSecrets       = "PBRPrivacySecrets"
	optPersonal      = "PBRPrivacyPersonal"
	optKinds         = "PBRPrivacyKinds"
	optWords         = "PBRPrivacyWords"
	optWordsAnyCase  = "PBRPrivacyWordsAnyCase"
	optWordsWhole    = "PBRPrivacyWordsWhole"
	optRules         = "PBRPrivacyRules"
	optActions       = "PBRPrivacyActions"
	optAllow         = "PBRPrivacyAllow"
	optShape         = "PBRPrivacyShape"
	optModelEnabled  = "PBRPrivacyModelEnabled"
	optModelRequired = "PBRPrivacyModelRequired"
	optModelMinScore = "PBRPrivacyModelMinScore"
)

// ModelSettings says how the local model participates.
type ModelSettings struct {
	Enabled  bool    `json:"enabled"`
	Required bool    `json:"required"`
	MinScore float64 `json:"min_score"`
}

// Settings is the instance-level masking configuration.
type Settings struct {
	Secrets      bool              `json:"secrets"`
	Personal     bool              `json:"personal"`
	Kinds        map[string]bool   `json:"kinds"`
	Words        []string          `json:"words"`
	WordsAnyCase bool              `json:"words_any_case"`
	WordsWhole   bool              `json:"words_whole"`
	Rules        []redact.Rule     `json:"rules"`
	Actions      map[string]string `json:"actions"`
	Allow        []string          `json:"allow"`
	Shape        bool              `json:"shape"`
	Model        ModelSettings     `json:"model"`
	Active       bool              `json:"active"`
}

// DefaultModelMinScore is the publisher's threshold.
const (
	DefaultModelMinScore = 0.80
	minModelScore        = 0.50
)

// Defaults returns the settings a new instance has: masking off, with the
// categories magpie turns on by default.
func Defaults() Settings {
	kinds := map[string]bool{}
	for _, c := range redact.Categories {
		kinds[c.ID] = c.On
	}
	return Settings{
		Kinds:   kinds,
		Words:   []string{},
		Rules:   []redact.Rule{},
		Actions: map[string]string{},
		Allow:   []string{},
		Model:   ModelSettings{MinScore: DefaultModelMinScore},
	}
}

// Active says any masking is asked for.
func (s Settings) ActiveNow() bool {
	return s.Secrets || s.Personal || len(s.Words) > 0
}

const (
	maxWords  = 200
	maxWord   = 256
	maxAllow  = 200
	maxAllowV = 256
)

// Normalize trims and validates s. Unknown personal-data kinds are
// dropped (the catalog is the source); unknown actions or invalid values
// are errors.
func Normalize(s Settings) (Settings, error) {
	if s.Model.MinScore == 0 {
		s.Model.MinScore = DefaultModelMinScore
	}
	kinds := map[string]bool{}
	for _, c := range redact.Categories {
		on, ok := s.Kinds[c.ID]
		if !ok {
			on = c.On
		}
		kinds[c.ID] = on
	}
	s.Kinds = kinds

	s.Words = cleanList(s.Words, maxWords, maxWord)
	if len(s.Words) > maxWords {
		return Settings{}, fmt.Errorf("at most %d words", maxWords)
	}
	for _, w := range s.Words {
		if len(w) > maxWord {
			return Settings{}, fmt.Errorf("a word is at most %d characters", maxWord)
		}
	}

	s.Allow = dedupeFold(s.Allow)
	if len(s.Allow) > maxAllow {
		return Settings{}, fmt.Errorf("at most %d allowed values", maxAllow)
	}
	for _, v := range s.Allow {
		if len(v) > maxAllowV {
			return Settings{}, fmt.Errorf("an allowed value is at most %d characters", maxAllowV)
		}
	}

	rules, err := redact.CheckRules(s.Rules)
	if err != nil {
		return Settings{}, err
	}
	s.Rules = rules

	actions := map[string]string{}
	for k, a := range s.Actions {
		k = strings.TrimSpace(k)
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" || a == "mask" {
			continue
		}
		if a != redact.Warn && a != redact.Block {
			return Settings{}, fmt.Errorf("the action for %s must be mask, %s or %s", k, redact.Warn, redact.Block)
		}
		if !redact.KindName(redact.RuleKind(k)) {
			return Settings{}, fmt.Errorf("%q isn't a kind masking can name", k)
		}
		actions[redact.RuleKind(k)] = a
	}
	s.Actions = actions

	if s.Model.MinScore < minModelScore || s.Model.MinScore > 0.99 {
		return Settings{}, fmt.Errorf("model.min_score must be between %.2f and 0.99", minModelScore)
	}
	if !s.Model.Enabled {
		s.Model.Required = false
	}
	s.Active = s.ActiveNow()
	if s.Words == nil {
		s.Words = []string{}
	}
	if s.Rules == nil {
		s.Rules = []redact.Rule{}
	}
	if s.Allow == nil {
		s.Allow = []string{}
	}
	return s, nil
}

func cleanList(in []string, maxN, maxLen int) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		low := strings.ToLower(v)
		if seen[low] {
			continue
		}
		seen[low] = true
		out = append(out, v)
		if len(out) > maxN {
			break
		}
	}
	return out
}

func dedupeFold(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}

func raw(key string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return strings.TrimSpace(common.OptionMap[key])
}

func rawBool(key string) bool {
	return strings.EqualFold(raw(key), "true")
}

// Load reads and normalizes the persisted settings. A malformed value is
// an error, never silently treated as "masking off".
func Load() (Settings, error) {
	s := Defaults()
	s.Secrets = rawBool(optSecrets)
	s.Personal = rawBool(optPersonal)
	s.WordsAnyCase = rawBool(optWordsAnyCase)
	s.WordsWhole = rawBool(optWordsWhole)
	s.Shape = rawBool(optShape)
	s.Model.Enabled = rawBool(optModelEnabled)
	s.Model.Required = rawBool(optModelRequired)
	if v := raw(optModelMinScore); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return Settings{}, fmt.Errorf("parsing %s: %w", optModelMinScore, err)
		}
		s.Model.MinScore = f
	}
	for key, dst := range map[string]any{
		optKinds:   &s.Kinds,
		optWords:   &s.Words,
		optRules:   &s.Rules,
		optActions: &s.Actions,
		optAllow:   &s.Allow,
	} {
		v := raw(key)
		if v == "" {
			continue
		}
		if err := json.Unmarshal([]byte(v), dst); err != nil {
			return Settings{}, fmt.Errorf("parsing %s: %w", key, err)
		}
	}
	return Normalize(s)
}

// Save normalizes, persists, and invalidates the cache. It does not start
// the model download; the caller does when enabled.
func Save(s Settings) (Settings, error) {
	n, err := Normalize(s)
	if err != nil {
		return Settings{}, err
	}
	kinds, _ := json.Marshal(n.Kinds)
	words, _ := json.Marshal(n.Words)
	rules, _ := json.Marshal(n.Rules)
	actions, _ := json.Marshal(n.Actions)
	allow, _ := json.Marshal(n.Allow)
	updates := map[string]string{
		optSecrets:       fmt.Sprintf("%t", n.Secrets),
		optPersonal:      fmt.Sprintf("%t", n.Personal),
		optKinds:         string(kinds),
		optWords:         string(words),
		optWordsAnyCase:  fmt.Sprintf("%t", n.WordsAnyCase),
		optWordsWhole:    fmt.Sprintf("%t", n.WordsWhole),
		optRules:         string(rules),
		optActions:       string(actions),
		optAllow:         string(allow),
		optShape:         fmt.Sprintf("%t", n.Shape),
		optModelEnabled:  fmt.Sprintf("%t", n.Model.Enabled),
		optModelRequired: fmt.Sprintf("%t", n.Model.Required),
		optModelMinScore: fmt.Sprintf("%.2f", n.Model.MinScore),
	}
	if err := model.UpdateOptionsBulk(updates); err != nil {
		return Settings{}, err
	}
	Invalidate()
	n.Active = n.ActiveNow()
	return n, nil
}

var (
	cacheMu   sync.Mutex
	cache     Settings
	cacheAt   time.Time
	cacheErr  error
	keyLoaded bool
)

// Current returns the settings requests use. It caches for a second; Save
// invalidates at once. When masking is active it also fixes the
// placeholder key from the options table.
func Current() (Settings, error) {
	cacheMu.Lock()
	if !cacheAt.IsZero() && time.Since(cacheAt) < time.Second {
		s, err := cache, cacheErr
		cacheMu.Unlock()
		return s, err
	}
	cacheMu.Unlock()

	s, err := Load()
	if err == nil && s.ActiveNow() {
		if !keyLoaded {
			key, kerr := model.GetPrivacyKey()
			if kerr != nil {
				err = kerr
			} else {
				redact.SetKey(key)
				keyLoaded = true
			}
		}
	}
	cacheMu.Lock()
	cache, cacheAt, cacheErr = s, time.Now(), err
	cacheMu.Unlock()
	return s, err
}

// Invalidate drops the cached settings.
func Invalidate() {
	cacheMu.Lock()
	cacheAt = time.Time{}
	cacheMu.Unlock()
}

// Options builds the redact options for these settings. ctx is not used
// here; the model callback carries its own deadline.
func Options(s Settings, modelFn redact.Model) redact.Options {
	return redact.Options{
		Secrets:      s.Secrets,
		Personal:     s.Personal,
		Words:        s.Words,
		WordsAnyCase: s.WordsAnyCase,
		WordsWhole:   s.WordsWhole,
		Rules:        s.Rules,
		Kinds:        s.Kinds,
		Model:        modelFn,
		MinScore:     s.Model.MinScore,
		Actions:      s.Actions,
		Allow:        s.Allow,
		Shape:        s.Shape,
	}
}

// ErrUnavailable is a masking engine failure: nothing is sent.
var ErrUnavailable = errors.New("privacy masking is enabled but failed; nothing was sent")
