package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestValidateSingBoxConfigDoesNotRewriteFields(t *testing.T) {
	raw := []byte(`{
		"http_clients":[{"tag":"novapanel-default","detour":"direct"}],
		"dns":{"independent_cache":true,"rules":[{"ip_cidr":["10.0.0.0/8"],"strategy":"ipv4_only"}]},
		"inbounds":[{"type":"tun","tag":"tun-in","stack":"system","tls":{"acme":{"domain":["example.com"]}}}],
		"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs","download_detour":"direct"}]},
		"experimental":{"cache_file":{"store_rdrc":true,"rdrc_timeout":"7d"}}
	}`)

	validated, err := ValidateSingBoxConfig(raw)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	var want, got map[string]interface{}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(validated, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("validation rewrote configuration fields\nwant: %#v\ngot: %#v", want, got)
	}
}

func TestValidateSingBoxConfigRequiresObject(t *testing.T) {
	if _, err := ValidateSingBoxConfig([]byte(`null`)); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("expected a root object validation error, got %v", err)
	}
}

func TestValidateSingBoxConfigValidatesDNSRuleMapReferences(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:   "valid scalar and list values in nested rules",
			config: `{"dns":{"servers":[{"tag":"local","type":"local"},{"tag":"resolved","type":"resolved"}],"rules":[{"type":"logical","mode":"and","rules":[{"dns_server_address":{"local":["192.0.2.0/24","2001:db8::1"],"resolved":"198.51.100.2"},"dns_search_domain":{"local":["Example.COM.","example.net"],"resolved":"example.org"}}]}]},"route":{"rules":[]}}`,
		},
		{
			name:   "implicit local resolver",
			config: `{"route":{"rules":[{"type":"logical","mode":"and","rules":[{"dns_search_domain":{"local":"example.org"}}]}]}}`,
		},
		{name: "empty map", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_server_address":{}}]}}`, wantErr: "non-empty map"},
		{name: "empty list", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_server_address":{"local":[]}}]}}`, wantErr: "non-empty string or string list"},
		{name: "blank key", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_search_domain":{" ":"example.org"}}]}}`, wantErr: "blank DNS server tag"},
		{name: "blank value", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_search_domain":{"local":[" "]}}]}}`, wantErr: "must not be blank"},
		{name: "invalid CIDR", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_server_address":{"local":"192.0.2.1/99"}}]}}`, wantErr: "IP address or CIDR"},
		{name: "missing DNS server", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"type":"logical","mode":"and","rules":[{"dns_server_address":{"missing":"192.0.2.1"}}]}]}}`, wantErr: "missing DNS server tag"},
		{name: "unsupported DNS transport", config: `{"dns":{"servers":[{"tag":"remote","type":"udp"}],"rules":[{"dns_server_address":{"remote":"192.0.2.1"}}]}}`, wantErr: "does not support dns_server_address"},
		{name: "implicit local unavailable with explicit final", config: `{"dns":{"final":"local","rules":[{"dns_search_domain":{"local":"example.org"}}]}}`, wantErr: "missing DNS server tag"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateSingBoxConfig([]byte(test.config))
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("valid config rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateSingBoxConfigRejectsHTTPVersionAndTLSMismatches(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{name: "HTTP3 requires enabled TLS", config: `{"inbounds":[{"type":"http","version":3,"tls":{"enabled":false}}]}`, wantErr: "version 3 requires an enabled TLS"},
		{name: "invalid HTTP inbound version", config: `{"inbounds":[{"type":"http","version":[1,4]}]}`, wantErr: "version must contain only 1, 2, or 3"},
		{name: "HTTP3 outbound requires TLS", config: `{"outbounds":[{"type":"http","version":3}]}`, wantErr: "version 3 requires TLS to be enabled"},
		{name: "HTTP3 with Host is invalid", config: `{"outbounds":[{"type":"http","version":3,"tls":{"enabled":true},"headers":{"Host":["proxy.example"]}}]}`, wantErr: "versions 2 and 3 do not support path or Host"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateSingBoxConfig([]byte(test.config))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateSingBoxConfigAllowsStandaloneH2CInbound(t *testing.T) {
	if _, err := ValidateSingBoxConfig([]byte(`{"inbounds":[{"type":"http","tag":"standalone-h2c","version":[2]}]}`)); err != nil {
		t.Fatalf("standalone raw H2C inbound should remain valid: %v", err)
	}
}

func TestValidateSingBoxConfigRejectsDeprecatedHysteriaTuningFields(t *testing.T) {
	tests := []struct {
		section string
		field   string
		newName string
	}{
		{section: "inbounds", field: "recv_window_conn", newName: "connection_receive_window"},
		{section: "inbounds", field: "recv_window_client", newName: "stream_receive_window"},
		{section: "inbounds", field: "max_conn_client", newName: "max_concurrent_streams"},
		{section: "inbounds", field: "disable_mtu_discovery", newName: "disable_path_mtu_discovery"},
		{section: "outbounds", field: "recv_window", newName: "stream_receive_window"},
		{section: "outbounds", field: "recv_window_conn", newName: "connection_receive_window"},
	}
	for _, test := range tests {
		t.Run(test.section+"/"+test.field, func(t *testing.T) {
			config := `{"` + test.section + `":[{"type":"hysteria","tag":"hy","` + test.field + `":1}]}`
			_, err := ValidateSingBoxConfig([]byte(config))
			if err == nil || !strings.Contains(err.Error(), test.newName) {
				t.Fatalf("error = %v, want replacement %q", err, test.newName)
			}
		})
	}
}

func TestValidateSingBoxConfigAllowsCurrentHysteriaTuningFields(t *testing.T) {
	config := `{"inbounds":[{"type":"hysteria","tag":"hy-in","connection_receive_window":1234,"stream_receive_window":5678,"max_concurrent_streams":32,"disable_path_mtu_discovery":true}],"outbounds":[{"type":"hysteria","tag":"hy-out","connection_receive_window":1234,"stream_receive_window":5678,"disable_path_mtu_discovery":true}]}`
	if _, err := ValidateSingBoxConfig([]byte(config)); err != nil {
		t.Fatalf("current Hysteria fields rejected: %v", err)
	}
}
