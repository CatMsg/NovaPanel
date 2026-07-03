package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

var ErrNoChanges = errors.New("no changes")

func normalizedJSON(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err == nil {
		return compact.Bytes()
	}
	return trimmed
}

func equalJSONBytes(left, right []byte) bool {
	return bytes.Equal(normalizedJSON(left), normalizedJSON(right))
}

func equalStringMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func equalTrimmedString(left, right string) bool {
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}
