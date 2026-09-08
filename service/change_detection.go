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

func equalTrimmedString(left, right string) bool {
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}
