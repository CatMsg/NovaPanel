package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestTlsServerConfigMigratesLegacyACMELosslesslyWithoutMutatingStorage(t *testing.T) {
	stored := json.RawMessage(`{
		"enabled":true,
		"server_name":"edge.example",
		"certificate_path":"/etc/novas/cert.pem",
		"key_path":"/etc/novas/key.pem",
		"acme":{
			"domain":["edge.example","alt.example"],
			"data_directory":"/var/lib/novas/acme",
			"default_server_name":"edge.example",
			"email":"ops@example.com",
			"provider":"https://acme.example/directory",
			"disable_http_challenge":true,
			"alternative_tls_port":8443,
			"external_account":{"key_id":"account-id","mac_key":"account-key"},
			"dns01_challenge":{"provider":"cloudflare","api_token":"dns-token"},
			"profile":"shortlived"
		}
	}`)
	tls := Tls{Server: stored}

	got, err := tls.ServerConfig()
	if err != nil {
		t.Fatalf("ServerConfig returned error: %v", err)
	}
	if !reflect.DeepEqual([]byte(tls.Server), []byte(stored)) {
		t.Fatal("ServerConfig mutated the stored TLS JSON")
	}

	var server map[string]json.RawMessage
	if err := json.Unmarshal(got, &server); err != nil {
		t.Fatalf("decode server config: %v", err)
	}
	if _, ok := server["acme"]; ok {
		t.Fatalf("deprecated acme field remains in generated config: %s", got)
	}
	var provider map[string]json.RawMessage
	if err := json.Unmarshal(server["certificate_provider"], &provider); err != nil {
		t.Fatalf("decode certificate provider: %v", err)
	}
	var providerType string
	if err := json.Unmarshal(provider["type"], &providerType); err != nil || providerType != "acme" {
		t.Fatalf("provider type = %q, want acme (err=%v)", providerType, err)
	}
	delete(provider, "type")

	var originalServer map[string]json.RawMessage
	if err := json.Unmarshal(stored, &originalServer); err != nil {
		t.Fatalf("decode stored config: %v", err)
	}
	var originalACME map[string]json.RawMessage
	if err := json.Unmarshal(originalServer["acme"], &originalACME); err != nil {
		t.Fatalf("decode stored acme config: %v", err)
	}
	if !reflect.DeepEqual(provider, originalACME) {
		t.Fatalf("ACME options changed during migration\n got: %#v\nwant: %#v", provider, originalACME)
	}
}

func TestTlsServerConfigOmitsInactiveLegacyACMEOnlyAtRuntime(t *testing.T) {
	stored := json.RawMessage(`{"enabled":true,"certificate_path":"cert.pem","key_path":"key.pem","acme":{"domain":[],"email":"draft@example.com"}}`)
	tls := Tls{Server: stored}

	got, err := tls.ServerConfig()
	if err != nil {
		t.Fatalf("ServerConfig returned error: %v", err)
	}
	if !reflect.DeepEqual([]byte(tls.Server), []byte(stored)) {
		t.Fatal("inactive ACME draft was removed from stored data")
	}
	if strings.Contains(string(got), `"acme"`) || !strings.Contains(string(got), `"certificate_path":"cert.pem"`) {
		t.Fatalf("runtime config did not omit only inactive ACME: %s", got)
	}
}

func TestTlsServerConfigRejectsConflictingOrIncompleteCertificateProviders(t *testing.T) {
	tests := []struct {
		name   string
		server string
		want   string
	}{
		{
			name:   "legacy and modern provider",
			server: `{"acme":{"domain":["edge.example"]},"certificate_provider":{"type":"acme","domain":["edge.example"]}}`,
			want:   "both acme and certificate_provider",
		},
		{
			name:   "empty inline ACME provider",
			server: `{"certificate_provider":{"type":"acme","domain":[]}}`,
			want:   "requires at least one domain",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (&Tls{Server: json.RawMessage(test.server)}).ServerConfig()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ServerConfig error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestInboundAndServiceMarshalModernizeLegacyTLS(t *testing.T) {
	legacyTLS := &Tls{Server: json.RawMessage(`{"enabled":true,"acme":{"domain":"edge.example","email":"ops@example.com"}}`)}
	for name, marshal := range map[string]func() ([]byte, error){
		"inbound": func() ([]byte, error) {
			return (Inbound{Type: "vless", Tag: "vless-in", Tls: legacyTLS}).MarshalJSON()
		},
		"service": func() ([]byte, error) {
			return (Service{Type: "naive", Tag: "naive-service", Tls: legacyTLS}).MarshalJSON()
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := marshal()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var result map[string]json.RawMessage
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatalf("decode result: %v", err)
			}
			var tlsOptions map[string]json.RawMessage
			if err := json.Unmarshal(result["tls"], &tlsOptions); err != nil {
				t.Fatalf("decode TLS options: %v", err)
			}
			if _, ok := tlsOptions["acme"]; ok {
				t.Fatalf("deprecated TLS field emitted: %s", raw)
			}
			if !strings.Contains(string(tlsOptions["certificate_provider"]), `"type":"acme"`) {
				t.Fatalf("modern certificate provider missing: %s", raw)
			}
		})
	}
}
