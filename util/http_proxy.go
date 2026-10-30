package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
)

var defaultHTTPInboundVersions = []int{1, 2}

// HTTPInboundVersions returns the versions sing-box serves for an HTTP inbound.
// An omitted or empty list follows sing-box's 1.15 default of HTTP/1 and HTTP/2.
func HTTPInboundVersions(options json.RawMessage) ([]int, error) {
	var fields map[string]interface{}
	if len(bytes.TrimSpace(options)) != 0 && !bytes.Equal(bytes.TrimSpace(options), []byte("null")) {
		if err := json.Unmarshal(options, &fields); err != nil {
			return nil, fmt.Errorf("HTTP inbound options: %w", err)
		}
	}
	if fields == nil {
		return append([]int(nil), defaultHTTPInboundVersions...), nil
	}
	return parseHTTPInboundVersions(fields)
}

func parseHTTPInboundVersions(fields map[string]interface{}) ([]int, error) {
	raw, exists := fields["version"]
	if !exists || raw == nil {
		return append([]int(nil), defaultHTTPInboundVersions...), nil
	}
	values := make([]interface{}, 0, 1)
	switch value := raw.(type) {
	case []interface{}:
		values = append(values, value...)
	case float64, int, int64, json.Number:
		values = append(values, value)
	default:
		return nil, fmt.Errorf("HTTP inbound version must be an integer or a list of integers")
	}
	if len(values) == 0 {
		return append([]int(nil), defaultHTTPInboundVersions...), nil
	}
	versions := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, rawVersion := range values {
		version, ok := integerValue(rawVersion)
		if !ok || version < 1 || version > 3 {
			return nil, fmt.Errorf("HTTP inbound version must contain only 1, 2, or 3")
		}
		if _, exists := seen[version]; exists {
			continue
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}
	return versions, nil
}

// ValidateHTTPInbound preserves sing-box's legacy HTTP/1+HTTP/2 defaults for
// existing records while validating HTTP/3 against the actual TLS configuration.
func ValidateHTTPInbound(inbound *model.Inbound) ([]int, error) {
	if inbound == nil || inbound.Type != "http" {
		return nil, nil
	}
	versions, err := HTTPInboundVersions(inbound.Options)
	if err != nil {
		return nil, err
	}
	if containsHTTPVersion(versions, 3) && !HTTPInboundTLSEnabled(inbound) {
		return nil, fmt.Errorf("HTTP inbound version 3 requires an enabled TLS server configuration")
	}
	return versions, nil
}

// SelectedHTTPInboundVersion reads the panel's single-version inbound contract.
// HTTPInboundVersions remains available for validating raw Sing-Box configs.
func SelectedHTTPInboundVersion(options json.RawMessage) (int, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(options, &fields); err != nil || fields == nil {
		return 0, fmt.Errorf("HTTP inbound requires exactly one version choice (1, 2, or 3)")
	}
	rawVersion, exists := fields["version"]
	var version int
	if !exists || json.Unmarshal(rawVersion, &version) != nil || version < 1 || version > 3 {
		return 0, fmt.Errorf("HTTP inbound requires exactly one version choice (1, 2, or 3)")
	}
	return version, nil
}

// HasExplicitHTTPInboundVersion distinguishes the panel's single-version
// contract from legacy Sing-Box lists and the default HTTP/1+HTTP/2 behavior.
func HasExplicitHTTPInboundVersion(options json.RawMessage) bool {
	_, err := SelectedHTTPInboundVersion(options)
	return err == nil
}

// ValidateHTTPClientVersionOverride validates the shape of a per-user HTTP
// version hint. The hint is not authoritative for the listener's protocol.
func ValidateHTTPClientVersionOverride(httpConfig map[string]interface{}) error {
	if httpConfig == nil {
		return nil
	}
	raw, exists := httpConfig["version"]
	if !exists || raw == nil {
		return nil
	}
	version, ok := integerValue(raw)
	if !ok || version < 0 || version > 3 {
		return fmt.Errorf("HTTP client version override must be 0, 1, 2, or 3")
	}
	return nil
}

// CloneJSONMap makes an independent copy of a JSON-compatible object,
// including nested TLS maps and slices.
func CloneJSONMap(source map[string]interface{}) (map[string]interface{}, error) {
	if source == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var clone map[string]interface{}
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil, err
	}
	return clone, nil
}

// SetHTTPOutboundVersion derives protocol-specific settings from the listener
// version without mutating shared TLS configuration.
func SetHTTPOutboundVersion(outbound map[string]interface{}, version int) error {
	if outbound == nil || version < 1 || version > 3 {
		return fmt.Errorf("HTTP outbound version must be 1, 2, or 3")
	}
	outbound["version"] = version
	if version == 2 {
		outbound["disable_version_fallback"] = true
	} else {
		delete(outbound, "disable_version_fallback")
	}

	tls, ok := outbound["tls"].(map[string]interface{})
	if !ok {
		return nil
	}
	tls, err := CloneJSONMap(tls)
	if err != nil {
		return fmt.Errorf("copy HTTP outbound TLS: %w", err)
	}
	if enabled, _ := tls["enabled"].(bool); enabled {
		alpn := map[int]string{1: "http/1.1", 2: "h2", 3: "h3"}[version]
		tls["alpn"] = []interface{}{alpn}
	}
	outbound["tls"] = tls
	return nil
}

// ResolveHTTPOutboundVersion applies sing-box HTTP outbound validation and, when
// supportedVersions is non-nil, selects a version served by the corresponding
// HTTP inbound. For ordinary Clash type:http subscriptions, HTTP/1 is the
// only representable version and is preferred when no version is specified.
func ResolveHTTPOutboundVersion(outbound map[string]interface{}, supportedVersions []int) (int, error) {
	if outbound == nil {
		return 0, fmt.Errorf("HTTP outbound options must be an object")
	}
	version := 0
	if raw, exists := outbound["version"]; exists && raw != nil {
		var ok bool
		version, ok = integerValue(raw)
		if !ok || version < 0 || version > 3 {
			return 0, fmt.Errorf("HTTP outbound version must be 0, 1, 2, or 3")
		}
	}
	path := hasHTTPPath(outbound["path"])
	host := hasHTTPHostHeader(outbound["headers"])
	if path && host {
		return 0, fmt.Errorf("HTTP outbound path and Host header are not allowed at the same time")
	}
	if (path || host) && (version == 2 || version == 3) {
		return 0, fmt.Errorf("HTTP outbound versions 2 and 3 do not support path or Host header")
	}
	if version == 0 {
		switch {
		case path || host:
			version = 1
		case supportedVersions != nil:
			version = preferredHTTPVersion(supportedVersions)
		default:
			version = 2
		}
	}
	if version == 0 {
		return 0, fmt.Errorf("HTTP outbound has no supported HTTP version")
	}
	if supportedVersions != nil && !containsHTTPVersion(supportedVersions, version) {
		return 0, fmt.Errorf("HTTP outbound version %d is not served by the HTTP inbound (supported: %s)", version, formatHTTPVersions(supportedVersions))
	}
	if version == 3 && !tlsOutboundEnabled(outbound["tls"]) {
		return 0, fmt.Errorf("HTTP outbound version 3 requires TLS to be enabled")
	}
	if version == 2 && supportedVersions != nil && !containsHTTPVersion(supportedVersions, 1) && !tlsOutboundEnabled(outbound["tls"]) {
		return 0, fmt.Errorf("HTTP outbound version 2 without TLS falls back to HTTP/1, which the inbound does not serve")
	}
	if version == 2 && boolValue(outbound["disable_version_fallback"]) && !tlsOutboundEnabled(outbound["tls"]) {
		return 0, fmt.Errorf("HTTP outbound version 2 requires TLS when version fallback is disabled")
	}
	return version, nil
}

// HTTPInboundTLSEnabled reports the TLS configuration actually attached to an
// inbound. A populated association without a TLS record ID is not active TLS.
func HTTPInboundTLSEnabled(inbound *model.Inbound) bool {
	return inbound != nil && inbound.TlsId > 0 && tlsServerEnabled(inbound.Tls)
}

// ValidateHTTPOutboundTLSForInbound rejects generated nodes whose TLS setting
// cannot reach the HTTP inbound's actual listener configuration.
func ValidateHTTPOutboundTLSForInbound(outbound map[string]interface{}, inbound *model.Inbound) error {
	if inbound == nil || inbound.Type != "http" {
		return nil
	}
	wantTLS := HTTPInboundTLSEnabled(inbound)
	gotTLS := tlsOutboundEnabled(outbound["tls"])
	if gotTLS != wantTLS {
		return fmt.Errorf("HTTP outbound TLS enabled=%t does not match HTTP inbound TLS enabled=%t", gotTLS, wantTLS)
	}
	return nil
}

// HTTPInboundURICompatible reports whether a standard HTTP proxy URI can encode
// the configured client version. Such URIs represent HTTP/1 only.
func HTTPInboundURICompatible(clientConfig json.RawMessage, inbound *model.Inbound) (bool, error) {
	versions, err := ValidateHTTPInbound(inbound)
	if err != nil {
		return false, err
	}
	if inbound == nil || inbound.Type != "http" {
		return false, nil
	}
	if !containsHTTPVersion(versions, 1) {
		return false, nil
	}
	var outbound map[string]interface{}
	if len(inbound.OutJson) > 0 && !bytes.Equal(bytes.TrimSpace(inbound.OutJson), []byte("null")) {
		if err := json.Unmarshal(inbound.OutJson, &outbound); err != nil {
			return false, fmt.Errorf("HTTP inbound out_json: %w", err)
		}
	}
	if outbound == nil {
		outbound = make(map[string]interface{})
	}
	var configs map[string]interface{}
	if len(clientConfig) > 0 && !bytes.Equal(bytes.TrimSpace(clientConfig), []byte("null")) {
		if err := json.Unmarshal(clientConfig, &configs); err != nil {
			return false, fmt.Errorf("client HTTP configuration: %w", err)
		}
	}
	if httpConfig, ok := configs["http"].(map[string]interface{}); ok {
		if err := ValidateHTTPClientVersionOverride(httpConfig); err != nil {
			return false, err
		}
		for key, value := range httpConfig {
			if key != "version" {
				outbound[key] = value
			}
		}
	}
	if err := SetHTTPOutboundVersion(outbound, 1); err != nil {
		return false, err
	}
	version, err := ResolveHTTPOutboundVersion(outbound, versions)
	if err != nil {
		return false, err
	}
	return version == 1 && !HTTPOutboundHasPathOrHost(outbound), nil
}

// HTTPOutboundHasPathOrHost reports options a standard HTTP proxy URI or Clash
// HTTP proxy entry cannot faithfully represent.
func HTTPOutboundHasPathOrHost(outbound map[string]interface{}) bool {
	return outbound != nil && (hasHTTPPath(outbound["path"]) || hasHTTPHostHeader(outbound["headers"]))
}

func preferredHTTPVersion(versions []int) int {
	for _, candidate := range []int{1, 2, 3} {
		if containsHTTPVersion(versions, candidate) {
			return candidate
		}
	}
	return 0
}

func containsHTTPVersion(versions []int, wanted int) bool {
	for _, version := range versions {
		if version == wanted {
			return true
		}
	}
	return false
}

func formatHTTPVersions(versions []int) string {
	parts := make([]string, 0, len(versions))
	for _, version := range versions {
		parts = append(parts, fmt.Sprint(version))
	}
	return strings.Join(parts, ", ")
}

func integerValue(value interface{}) (int, bool) {
	switch number := value.(type) {
	case float64:
		if number != float64(int(number)) {
			return 0, false
		}
		return int(number), true
	case int:
		return number, true
	case int64:
		return int(number), int64(int(number)) == number
	case json.Number:
		parsed, err := number.Int64()
		return int(parsed), err == nil && int64(int(parsed)) == parsed
	default:
		return 0, false
	}
}

func hasHTTPHostHeader(value interface{}) bool {
	headers := reflect.ValueOf(value)
	if !headers.IsValid() || headers.Kind() != reflect.Map {
		return false
	}
	for _, name := range headers.MapKeys() {
		if name.Kind() == reflect.String && strings.EqualFold(name.String(), "Host") {
			return hasHTTPHeaderValue(headers.MapIndex(name))
		}
	}
	return false
}

func hasHTTPHeaderValue(value reflect.Value) bool {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.String:
		return value.String() != ""
	case reflect.Slice, reflect.Array:
		if value.Len() == 0 {
			return false
		}
		first := value.Index(0)
		for first.IsValid() && (first.Kind() == reflect.Interface || first.Kind() == reflect.Pointer) {
			if first.IsNil() {
				return false
			}
			first = first.Elem()
		}
		return first.Kind() == reflect.String && first.String() != ""
	default:
		return false
	}
}

func hasHTTPPath(value interface{}) bool {
	path, ok := value.(string)
	return ok && path != ""
}

func tlsOutboundEnabled(value interface{}) bool {
	tls, ok := value.(map[string]interface{})
	if !ok {
		return false
	}
	enabled, _ := tls["enabled"].(bool)
	return enabled
}

func tlsServerEnabled(tls *model.Tls) bool {
	if tls == nil || len(tls.Server) == 0 {
		return false
	}
	var options map[string]interface{}
	if err := json.Unmarshal(tls.Server, &options); err != nil {
		return false
	}
	enabled, _ := options["enabled"].(bool)
	return enabled
}

func boolValue(value interface{}) bool {
	result, _ := value.(bool)
	return result
}
