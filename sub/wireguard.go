package sub

import (
	"encoding/base64"
	"strings"
)

func normalizeWireguardPreSharedKey(raw interface{}) (string, bool) {
	value, ok := raw.(string)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}

	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return base64.StdEncoding.EncodeToString(decoded), true
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return base64.StdEncoding.EncodeToString(decoded), true
	}
	return "", false
}
