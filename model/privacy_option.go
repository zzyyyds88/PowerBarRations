package model

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"

	"github.com/zzyyyds88/PowerBarRations/common"
)

// OptionPrivacyKey is the 32-byte key placeholders are derived from. It is
// a secret: it never appears in an API response, an export, or a log.
const OptionPrivacyKey = "PBRPrivacyPlaceholderKey"

var privacyKeyMu sync.Mutex

// GetPrivacyKey returns the process's placeholder key, generating and
// persisting one on first use.
func GetPrivacyKey() ([]byte, error) {
	privacyKeyMu.Lock()
	defer privacyKeyMu.Unlock()

	common.OptionMapRWMutex.RLock()
	raw := strings.TrimSpace(common.OptionMap[OptionPrivacyKey])
	common.OptionMapRWMutex.RUnlock()
	if raw != "" {
		b, err := base64.StdEncoding.DecodeString(raw)
		if err == nil && len(b) >= 32 {
			return b[:32], nil
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(b)
	if err := UpdateOption(OptionPrivacyKey, encoded); err != nil {
		return nil, errors.New("store privacy placeholder key: " + err.Error())
	}
	return b, nil
}
