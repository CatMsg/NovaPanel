package service

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ValidateSingBoxConfig validates panel-specific invariants without rewriting
// configuration fields. sing-box remains the authority for core validation.
func ValidateSingBoxConfig(raw []byte) ([]byte, error) {
	var config map[string]interface{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, errors.New("sing-box config must be a JSON object")
	}
	if err := validateDeprecatedHysteriaOptions(config); err != nil {
		return nil, err
	}
	if err := validateHTTPProxyConfig(config); err != nil {
		return nil, err
	}
	if err := validateDNSRuleMapValues(config); err != nil {
		return nil, err
	}
	formatted, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("format sing-box config: %w", err)
	}
	return formatted, nil
}

func validateDeprecatedHysteriaOptions(config map[string]interface{}) error {
	for _, section := range []string{"inbounds", "outbounds"} {
		for index, item := range objectList(config[section]) {
			raw, err := json.Marshal(item)
			if err != nil {
				return fmt.Errorf("%s[%d]: %w", section, index, err)
			}
			if err := validateDeprecatedHysteriaProtocolOptions(stringValue(item["type"]), raw); err != nil {
				return fmt.Errorf("%s[%d]: %w", section, index, err)
			}
		}
	}
	return nil
}

func validateDeprecatedHysteriaProtocolOptions(protocol string, raw json.RawMessage) error {
	if protocol != "hysteria" || len(raw) == 0 {
		return nil
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(raw, &options); err != nil {
		return fmt.Errorf("hysteria options: %w", err)
	}
	deprecated := map[string]string{
		"recv_window_conn":      "connection_receive_window",
		"recv_window":           "stream_receive_window",
		"recv_window_client":    "stream_receive_window",
		"max_conn_client":       "max_concurrent_streams",
		"disable_mtu_discovery": "disable_path_mtu_discovery",
	}
	for field, replacement := range deprecated {
		if _, exists := options[field]; exists {
			return fmt.Errorf("hysteria field %s is no longer supported; use %s", field, replacement)
		}
	}
	return nil
}

func objectValue(value interface{}) map[string]interface{} {
	result, _ := value.(map[string]interface{})
	return result
}

func boolValue(value interface{}) bool {
	result, _ := value.(bool)
	return result
}
