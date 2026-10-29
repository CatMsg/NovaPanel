package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ServerConfig returns a sing-box-ready TLS server object without mutating the
// stored JSON. It upgrades legacy inline ACME options in-memory so old database
// records remain usable after sing-box removes tls.acme.
func (t *Tls) ServerConfig() (json.RawMessage, error) {
	if t == nil {
		return nil, nil
	}
	if len(t.Server) == 0 {
		return t.Server, nil
	}
	var server map[string]json.RawMessage
	if err := json.Unmarshal(t.Server, &server); err != nil {
		return nil, fmt.Errorf("decode TLS server options: %w", err)
	}
	if server == nil {
		return t.Server, nil
	}

	legacyACME, hasLegacyACME := server["acme"]
	if hasLegacyACME && !isJSONNull(legacyACME) {
		var options map[string]json.RawMessage
		if err := json.Unmarshal(legacyACME, &options); err != nil || options == nil {
			if err == nil {
				err = errors.New("expected an object")
			}
			return nil, fmt.Errorf("decode deprecated TLS acme options: %w", err)
		}

		hasDomains, err := hasACMEDomains(options["domain"])
		if err != nil {
			return nil, fmt.Errorf("decode TLS acme domain: %w", err)
		}
		if !hasDomains {
			// sing-box ignores legacy acme when its domain list is empty. Keep the
			// draft in the database/UI, but omit this inactive option at runtime.
			delete(server, "acme")
		} else {
			if provider, exists := server["certificate_provider"]; exists && !isJSONNull(provider) {
				return nil, errors.New("TLS server cannot contain both acme and certificate_provider")
			}
			options["type"] = json.RawMessage(`"acme"`)
			provider, err := json.Marshal(options)
			if err != nil {
				return nil, fmt.Errorf("encode TLS certificate provider: %w", err)
			}
			server["certificate_provider"] = provider
			delete(server, "acme")
		}
	} else if hasLegacyACME {
		delete(server, "acme")
	}

	provider, exists := server["certificate_provider"]
	if exists && !isJSONNull(provider) {
		var inline struct {
			Type   string          `json:"type"`
			Domain json.RawMessage `json:"domain"`
		}
		if err := json.Unmarshal(provider, &inline); err == nil && inline.Type == "acme" {
			hasDomains, err := hasACMEDomains(inline.Domain)
			if err != nil {
				return nil, fmt.Errorf("decode TLS certificate_provider.domain: %w", err)
			}
			if !hasDomains {
				return nil, errors.New("TLS ACME certificate_provider requires at least one domain")
			}
		}
	}

	encoded, err := json.Marshal(server)
	if err != nil {
		return nil, fmt.Errorf("encode TLS server options: %w", err)
	}
	return encoded, nil
}

func hasACMEDomains(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return false, nil
	}
	var domains []string
	if err := json.Unmarshal(raw, &domains); err == nil {
		for _, domain := range domains {
			if strings.TrimSpace(domain) != "" {
				return true, nil
			}
		}
		return false, nil
	}
	var domain string
	if err := json.Unmarshal(raw, &domain); err == nil {
		return strings.TrimSpace(domain) != "", nil
	}
	return false, errors.New("expected a domain string or list of strings")
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
