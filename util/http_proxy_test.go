package util

import (
	"encoding/json"
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
		{name: "legacy prefers H1", outbound: map[string]interface{}{}, supported: []int{1, 2}, want: 1},
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

func TestFillOutJsonRejectsH2CPanelInboundWithoutCompatibleClient(t *testing.T) {
	inbound := &model.Inbound{
		Type:    "http",
		Tag:     "h2c-only",
		Options: json.RawMessage(`{"listen_port":8080,"version":[2]}`),
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
		{name: "H2 only", options: `{"listen_port":8080,"version":[2]}`, outbound: `{"type":"http","version":2}`, wantLinks: 0},
		{name: "H3 only", options: `{"listen_port":8443,"version":3}`, outbound: `{"type":"http","version":3,"tls":{"enabled":true}}`, tls: &model.Tls{Server: json.RawMessage(`{"enabled":true}`), Client: json.RawMessage(`{"enabled":true}`)}, wantLinks: 0},
		{name: "H1 and H3", options: `{"listen_port":8444,"version":[1,3]}`, outbound: `{"type":"http","version":1}`, tls: &model.Tls{Server: json.RawMessage(`{"enabled":true}`), Client: json.RawMessage(`{"enabled":true}`)}, wantLinks: 1},
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
