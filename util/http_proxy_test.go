package util

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
)

func TestHTTPInboundVersionsListableDefaultsAndValidation(t *testing.T) {
	tests := []struct {
		name    string
		options string
		want    []int
		wantErr bool
	}{
		{name: "omitted", options: `{}`, want: []int{1, 2}},
		{name: "empty list", options: `{"version":[]}`, want: []int{1, 2}},
		{name: "scalar", options: `{"version":3}`, want: []int{3}},
		{name: "list", options: `{"version":[3,1,3]}`, want: []int{3, 1}},
		{name: "invalid number", options: `{"version":[1,4]}`, wantErr: true},
		{name: "invalid string", options: `{"version":"3"}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := HTTPInboundVersions(json.RawMessage(test.options))
			if test.wantErr {
				if err == nil {
					t.Fatal("expected invalid HTTP inbound version to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse HTTP inbound versions: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("versions = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestResolveHTTPOutboundVersionCompatibility(t *testing.T) {
	tests := []struct {
		name      string
		outbound  map[string]interface{}
		supported []int
		want      int
		wantErr   string
	}{
		{name: "ordinary Clash HTTP prefers H1", outbound: map[string]interface{}{}, supported: []int{1, 2}, want: 1},
		{name: "H2 when H1 unavailable", outbound: map[string]interface{}{"tls": map[string]interface{}{"enabled": true}}, supported: []int{2, 3}, want: 2},
		{name: "H2 without TLS is rejected when H1 is unavailable", outbound: map[string]interface{}{"version": float64(2)}, supported: []int{2}, wantErr: "falls back to HTTP/1, which the inbound does not serve"},
		{name: "H2 without TLS can fall back to served H1", outbound: map[string]interface{}{"version": float64(2)}, supported: []int{1, 2}, want: 2},
		{name: "H3 when only H3 is served", outbound: map[string]interface{}{"tls": map[string]interface{}{"enabled": true}}, supported: []int{3}, want: 3},
		{name: "core auto defaults to H2 with TLS", outbound: map[string]interface{}{"tls": map[string]interface{}{"enabled": true}}, want: 2},
		{name: "core auto defaults to H2 without TLS", outbound: map[string]interface{}{}, want: 2},
		{name: "path auto selects H1", outbound: map[string]interface{}{"path": "/proxy"}, supported: []int{1, 2}, want: 1},
		{name: "Host list auto selects H1", outbound: map[string]interface{}{"headers": map[string]interface{}{"hOsT": []interface{}{"proxy.example"}}}, supported: []int{1, 2}, want: 1},
		{name: "explicit H2 with Host list rejected", outbound: map[string]interface{}{"version": float64(2), "headers": map[string]interface{}{"Host": []interface{}{"proxy.example"}}}, supported: []int{1, 2}, wantErr: "versions 2 and 3 do not support path or Host"},
		{name: "empty Host list does not prevent H3", outbound: map[string]interface{}{"version": float64(3), "tls": map[string]interface{}{"enabled": true}, "headers": map[string]interface{}{"Host": []interface{}{}}}, supported: []int{1, 3}, want: 3},
		{name: "empty Host scalar does not conflict with path", outbound: map[string]interface{}{"path": "/proxy", "headers": map[string]interface{}{"Host": ""}}, supported: []int{1, 2}, want: 1},
		{name: "empty Host list does not conflict with path", outbound: map[string]interface{}{"path": "/proxy", "headers": map[string]interface{}{"Host": []interface{}{}}}, supported: []int{1, 2}, want: 1},
		{name: "first empty Host list value is not an override", outbound: map[string]interface{}{"path": "/proxy", "headers": map[string]interface{}{"Host": []interface{}{"", "proxy.example"}}}, supported: []int{1, 2}, want: 1},
		{name: "empty Host list does not force H1", outbound: map[string]interface{}{"tls": map[string]interface{}{"enabled": true}, "headers": map[string]interface{}{"Host": []interface{}{}}}, supported: []int{2}, want: 2},
		{name: "path and nonempty Host scalar are rejected for H1", outbound: map[string]interface{}{"version": float64(1), "path": "/proxy", "headers": map[string]interface{}{"Host": "proxy.example"}}, supported: []int{1}, wantErr: "path and Host header are not allowed at the same time"},
		{name: "path and nonempty Host list are rejected", outbound: map[string]interface{}{"path": "/proxy", "headers": map[string]interface{}{"Host": []interface{}{"proxy.example"}}}, supported: []int{1, 2}, wantErr: "path and Host header are not allowed at the same time"},
		{name: "H3 TLS mismatch", outbound: map[string]interface{}{"version": float64(3)}, supported: []int{3}, wantErr: "version 3 requires TLS to be enabled"},
		{name: "unsupported explicit version", outbound: map[string]interface{}{"version": float64(3), "tls": map[string]interface{}{"enabled": true}}, supported: []int{1, 2}, wantErr: "is not served"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveHTTPOutboundVersion(test.outbound, test.supported)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve HTTP outbound version: %v", err)
			}
			if got != test.want {
				t.Fatalf("version = %d, want %d", got, test.want)
			}
		})
	}
}

func TestSetHTTPOutboundVersionUsesProtocolSpecificTLSSettings(t *testing.T) {
	for _, test := range []struct {
		version      int
		wantALPN     string
		wantFallback bool
	}{
		{version: 1, wantALPN: "http/1.1"},
		{version: 2, wantALPN: "h2", wantFallback: true},
		{version: 3, wantALPN: "h3"},
	} {
		t.Run(fmt.Sprint(test.version), func(t *testing.T) {
			sharedTLS := map[string]interface{}{"enabled": true, "alpn": []interface{}{"h2", "h3"}, "server_name": "proxy.example"}
			outbound := map[string]interface{}{
				"type":                     "http",
				"version":                  3,
				"disable_version_fallback": true,
				"tls":                      sharedTLS,
			}
			if err := SetHTTPOutboundVersion(outbound, test.version); err != nil {
				t.Fatalf("set version: %v", err)
			}
			if outbound["version"] != test.version {
				t.Fatalf("version = %#v, want %d", outbound["version"], test.version)
			}
			if got := boolValue(outbound["disable_version_fallback"]); got != test.wantFallback {
				t.Fatalf("disable_version_fallback = %t, want %t", got, test.wantFallback)
			}
			tls := outbound["tls"].(map[string]interface{})
			if got := tls["alpn"].([]interface{}); !reflect.DeepEqual(got, []interface{}{test.wantALPN}) {
				t.Fatalf("TLS ALPN = %#v, want [%q]", got, test.wantALPN)
			}
			if got := sharedTLS["alpn"].([]interface{}); !reflect.DeepEqual(got, []interface{}{"h2", "h3"}) {
				t.Fatalf("shared TLS ALPN was mutated: %#v", got)
			}
		})
	}
}

func TestHTTPInboundURICompatibilityUsesSelectedVersion(t *testing.T) {
	inbound := &model.Inbound{
		Type:    "http",
		Options: json.RawMessage(`{"version":1}`),
		OutJson: json.RawMessage(`{"type":"http","version":1}`),
	}
	compatible, err := HTTPInboundURICompatible(json.RawMessage(`{"http":{"version":2}}`), inbound)
	if err != nil || !compatible {
		t.Fatalf("the selected HTTP/1 listener should produce a compatible URI: compatible=%t err=%v", compatible, err)
	}
}

func TestFillOutJsonUsesSelectedVersion(t *testing.T) {
	tests := []struct {
		name         string
		options      string
		outbound     string
		tls          *model.Tls
		wantVersion  int
		wantALPN     []interface{}
		wantFallback interface{}
	}{
		{
			name:        "singleton H1 replaces stale H3",
			options:     `{"version":1}`,
			outbound:    `{"type":"http","version":3,"disable_version_fallback":true}`,
			wantVersion: 1,
		},
		{
			name:         "singleton H2 replaces stale H1",
			options:      `{"version":2}`,
			outbound:     `{"type":"http","version":1}`,
			tls:          &model.Tls{Server: json.RawMessage(`{"enabled":true,"alpn":["http/1.1","h2"]}`), Client: json.RawMessage(`{"enabled":true}`)},
			wantVersion:  2,
			wantALPN:     []interface{}{"h2"},
			wantFallback: true,
		},
		{
			name:        "singleton H3 replaces stale H2",
			options:     `{"version":3}`,
			outbound:    `{"type":"http","version":2}`,
			tls:         &model.Tls{Server: json.RawMessage(`{"enabled":true,"alpn":["h2","h3"]}`), Client: json.RawMessage(`{"enabled":true}`)},
			wantVersion: 3,
			wantALPN:    []interface{}{"h3"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalTLSServer := ""
			if test.tls != nil {
				originalTLSServer = string(test.tls.Server)
			}
			inbound := &model.Inbound{
				Type:    "http",
				Tag:     "http-test",
				Options: json.RawMessage(test.options),
				OutJson: json.RawMessage(test.outbound),
				Tls:     test.tls,
			}
			if test.tls != nil {
				inbound.TlsId = 1
			}
			if err := FillOutJson(inbound, "proxy.example"); err != nil {
				t.Fatalf("fill outbound: %v", err)
			}
			var outbound map[string]interface{}
			if err := json.Unmarshal(inbound.OutJson, &outbound); err != nil {
				t.Fatalf("decode generated outbound: %v", err)
			}
			if outbound["version"] != float64(test.wantVersion) {
				t.Fatalf("version = %#v, want %d", outbound["version"], test.wantVersion)
			}
			if outbound["disable_version_fallback"] != test.wantFallback {
				t.Fatalf("disable_version_fallback = %#v, want %#v", outbound["disable_version_fallback"], test.wantFallback)
			}
			if test.wantALPN != nil {
				got := outbound["tls"].(map[string]interface{})["alpn"]
				if !reflect.DeepEqual(got, test.wantALPN) {
					t.Fatalf("ALPN = %#v, want %#v", got, test.wantALPN)
				}
			}
			if test.tls != nil && string(test.tls.Server) != originalTLSServer {
				t.Fatalf("source TLS config was mutated: %s", test.tls.Server)
			}
		})
	}
}

func TestValidateHTTPInboundRequiresEnabledTLSForHTTP3(t *testing.T) {
	inbound := &model.Inbound{
		Type:    "http",
		Options: json.RawMessage(`{"version":3}`),
		TlsId:   1,
		Tls:     &model.Tls{Server: json.RawMessage(`{"enabled":false}`)},
	}
	if _, err := ValidateHTTPInbound(inbound); err == nil {
		t.Fatal("expected HTTP/3 without enabled TLS to be rejected")
	}
	inbound.Tls.Server = json.RawMessage(`{"enabled":true}`)
	if versions, err := ValidateHTTPInbound(inbound); err != nil || !reflect.DeepEqual(versions, []int{3}) {
		t.Fatalf("enabled TLS validation returned versions=%#v err=%v", versions, err)
	}
}

func TestValidateHTTPInboundPreservesLegacyVersions(t *testing.T) {
	for _, test := range []struct {
		options string
		want    []int
	}{
		{options: `{}`, want: []int{1, 2}},
		{options: `{"version":null}`, want: []int{1, 2}},
		{options: `{"version":[]}`, want: []int{1, 2}},
		{options: `{"version":[1]}`, want: []int{1}},
		{options: `{"version":[1,2]}`, want: []int{1, 2}},
		{options: `{"version":[2,1]}`, want: []int{2, 1}},
	} {
		inbound := &model.Inbound{Type: "http", Options: json.RawMessage(test.options)}
		got, err := ValidateHTTPInbound(inbound)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Errorf("options %s returned %#v, %v; want %#v", test.options, got, err, test.want)
		}
	}
	inbound := &model.Inbound{Type: "http", Options: json.RawMessage(`{"version":[1,4]}`)}
	if _, err := ValidateHTTPInbound(inbound); err == nil {
		t.Fatal("expected invalid legacy HTTP versions to be rejected")
	}
	if !HasExplicitHTTPInboundVersion(json.RawMessage(`{"version":2}`)) {
		t.Fatal("scalar HTTP version should satisfy the panel's explicit selection contract")
	}
	for _, options := range []string{`{}`, `{"version":[]}`, `{"version":[1,2]}`} {
		if HasExplicitHTTPInboundVersion(json.RawMessage(options)) {
			t.Errorf("legacy options %s unexpectedly satisfy explicit selection", options)
		}
	}
	for _, version := range []int{1, 2, 3} {
		inbound := &model.Inbound{Type: "http", Options: json.RawMessage(fmt.Sprintf(`{"version":%d}`, version))}
		if version == 3 {
			inbound.TlsId = 1
			inbound.Tls = &model.Tls{Server: json.RawMessage(`{"enabled":true}`)}
		}
		got, err := ValidateHTTPInbound(inbound)
		if err != nil || !reflect.DeepEqual(got, []int{version}) {
			t.Errorf("scalar version %d returned %#v, %v", version, got, err)
		}
	}
}

func TestFillOutJsonRejectsH2CPanelInboundWithoutCompatibleClient(t *testing.T) {
	inbound := &model.Inbound{
		Type:    "http",
		Tag:     "h2c-only",
		Options: json.RawMessage(`{"listen_port":8080,"version":2}`),
		OutJson: json.RawMessage(`{"type":"http","version":2}`),
	}
	if _, err := ValidateHTTPInbound(inbound); err != nil {
		t.Fatalf("standalone h2c listener should remain valid: %v", err)
	}
	if compatible, err := HTTPInboundURICompatible(nil, inbound); err != nil || compatible {
		t.Fatalf("h2c server URI compatibility = %t, err=%v; want false without error", compatible, err)
	}
	if err := FillOutJson(inbound, "proxy.example"); err == nil || !strings.Contains(err.Error(), "falls back to HTTP/1, which the inbound does not serve") {
		t.Fatalf("expected panel-generated H2C client to be rejected, got %v", err)
	}
}

func TestFillOutJsonPreservesLegacyHTTPVersion(t *testing.T) {
	inbound := &model.Inbound{
		Type:    "http",
		Tag:     "legacy-http",
		TlsId:   1,
		Tls:     &model.Tls{Server: json.RawMessage(`{"enabled":true}`), Client: json.RawMessage(`{"enabled":true}`)},
		Options: json.RawMessage(`{"listen_port":8080}`),
		OutJson: json.RawMessage(`{"type":"http","tag":"legacy-http","version":2,"tls":{"enabled":true}}`),
	}
	if err := FillOutJson(inbound, "proxy.example"); err != nil {
		t.Fatalf("FillOutJson returned error: %v", err)
	}
	var outbound map[string]interface{}
	if err := json.Unmarshal(inbound.OutJson, &outbound); err != nil {
		t.Fatalf("decode generated HTTP outbound: %v", err)
	}
	if outbound["version"] != float64(2) {
		t.Fatalf("legacy HTTP version = %#v, want 2", outbound["version"])
	}
}

func TestHTTPLinkURIEncodingAndAddressSchemeIsolation(t *testing.T) {
	links := httpLink(map[string]interface{}{"username": "user@name", "password": "p:a ss"}, []map[string]interface{}{
		{"server": "2001:db8::1", "server_port": float64(8443), "remark": "secure", "tls": map[string]interface{}{"enabled": true}},
		{"server": "example.org", "server_port": float64(8080), "remark": "plain"},
	})
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2: %#v", len(links), links)
	}
	for index, wantScheme := range []string{"https", "http"} {
		parsed, err := url.Parse(links[index])
		if err != nil {
			t.Fatalf("parse link %q: %v", links[index], err)
		}
		if parsed.Scheme != wantScheme {
			t.Fatalf("link %d scheme = %q, want %q", index, parsed.Scheme, wantScheme)
		}
		username := parsed.User.Username()
		password, passwordOK := parsed.User.Password()
		if username != "user@name" || password != "p:a ss" || !passwordOK {
			t.Fatalf("link %d credentials did not round trip: %#v", index, parsed.User)
		}
	}
	if got := links[0]; got != "https://user%40name:p%3Aa%20ss@[2001:db8::1]:8443#secure" {
		t.Fatalf("IPv6 URL was not safely encoded: %q", got)
	}
}

func TestLinkGeneratorSkipsHTTPNonH1AndKeepsH1Variant(t *testing.T) {
	clientConfig := json.RawMessage(`{"http":{"username":"user","password":"pass"}}`)
	for _, test := range []struct {
		name      string
		options   string
		outbound  string
		tls       *model.Tls
		wantLinks int
	}{
		{name: "H2 only", options: `{"listen_port":8080,"version":2}`, outbound: `{"type":"http","version":2}`, wantLinks: 0},
		{name: "H3 only", options: `{"listen_port":8443,"version":3}`, outbound: `{"type":"http","version":3,"tls":{"enabled":true}}`, tls: &model.Tls{Server: json.RawMessage(`{"enabled":true}`), Client: json.RawMessage(`{"enabled":true}`)}, wantLinks: 0},
		{name: "H1 only", options: `{"listen_port":8444,"version":1}`, outbound: `{"type":"http","version":1}`, wantLinks: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			inbound := &model.Inbound{Type: "http", Tag: test.name, Options: json.RawMessage(test.options), Addrs: json.RawMessage(`[{"server":"proxy.example","server_port":8080}]`), OutJson: json.RawMessage(test.outbound), Tls: test.tls}
			if test.tls != nil {
				inbound.TlsId = 1
			}
			links := LinkGenerator(clientConfig, inbound, "proxy.example")
			if len(links) != test.wantLinks {
				t.Fatalf("links = %#v, want %d", links, test.wantLinks)
			}
		})
	}
}
