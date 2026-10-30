package sub

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
)

func TestInboundAddressTagPreservesSingleAddressName(t *testing.T) {
	if got := inboundAddressTag("la-hy2", "", 0, 1); got != "la-hy2" {
		t.Fatalf("single address tag = %q", got)
	}
	if got := inboundAddressTag("la-hy2", "-alt", 1, 2); got != "2.la-hy2-alt" {
		t.Fatalf("multiple address tag = %q", got)
	}
}

func TestSingleAddressSubscriptionRetainsInboundName(t *testing.T) {
	inbound := &model.Inbound{Type: "socks", Tag: "la-socks", Options: json.RawMessage(`{"listen_port":1080}`), Addrs: json.RawMessage(`[{"server":"la.example","server_port":1080}]`), OutJson: json.RawMessage(`{"type":"socks","tag":"la-socks"}`)}
	outbounds, tags, err := (&JsonService{}).getOutbounds(json.RawMessage(`{}`), []*model.Inbound{inbound})
	if err != nil {
		t.Fatal(err)
	}
	if len(*tags) != 1 || (*tags)[0] != "la-socks" || (*outbounds)[0]["tag"] != "la-socks" || (*outbounds)[0]["server"] != "la.example" {
		t.Fatalf("unexpected single-address node: %#v %#v", *outbounds, *tags)
	}
}

func TestSingleAddressHTTPClashKeepsInboundVersionMapping(t *testing.T) {
	inbound := &model.Inbound{Type: "http", Tag: "la-http", Options: json.RawMessage(`{"listen_port":1080,"version":1}`), Addrs: json.RawMessage(`[{"server":"la.example","server_port":1080}]`), OutJson: json.RawMessage(`{"type":"http","tag":"la-http","version":1}`)}
	outbounds, tags, err := (&JsonService{}).getOutbounds(json.RawMessage(`{}`), []*model.Inbound{inbound})
	if err != nil {
		t.Fatal(err)
	}
	filtered, filteredTags, err := filterClashHTTPOutbounds([]*model.Inbound{inbound}, *outbounds, *tags)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || len(filteredTags) != 1 || filteredTags[0] != "la-http" {
		t.Fatalf("HTTP node was not retained: %#v %#v", filtered, filteredTags)
	}
}

func TestJSONSubscriptionDerivesAndPreservesHTTPVersionFields(t *testing.T) {
	service := &JsonService{}
	tests := []struct {
		name     string
		options  string
		outbound string
		client   string
		tls      *model.Tls
		want     int
		wantErr  string
	}{
		{
			name:     "selected H1 version is preserved",
			options:  `{"listen_port":8080,"version":1}`,
			outbound: `{"type":"http","tag":"http-default","path":"/proxy","headers":{"X-Test":["kept"]},"transport":{"type":"ws","path":"/kept"}}`,
			want:     1,
		},
		{
			name:     "H2 only derives H2",
			options:  `{"listen_port":8081,"version":2}`,
			outbound: `{"type":"http","tag":"http-h2","tls":{"enabled":true}}`,
			tls:      &model.Tls{Server: json.RawMessage(`{"enabled":true}`), Client: json.RawMessage(`{"enabled":true}`)},
			want:     2,
		},
		{
			name:    "H2C-only listener stays available to external clients without a generated node",
			options: `{"listen_port":8083,"version":2}`,
		},
		{
			name:     "H3 only derives H3 with matching TLS",
			options:  `{"listen_port":8443,"version":3}`,
			outbound: `{"type":"http","tag":"http-h3","tls":{"enabled":true}}`,
			tls:      &model.Tls{Server: json.RawMessage(`{"enabled":true}`), Client: json.RawMessage(`{"enabled":true}`)},
			want:     3,
		},
		{
			name:     "selected H1 overrides valid client version",
			options:  `{"listen_port":8082,"version":1}`,
			outbound: `{"type":"http","tag":"http-explicit"}`,
			client:   `{"http":{"version":3}}`,
			want:     1,
		},
		{
			name:     "TLS override mismatch rejected",
			options:  `{"listen_port":8084,"version":1}`,
			outbound: `{"type":"http","tag":"http-tls","version":1,"tls":{"enabled":true}}`,
			client:   `{"http":{"tls":{"enabled":false}}}`,
			wantErr:  "does not match HTTP inbound TLS",
			tls:      &model.Tls{Server: json.RawMessage(`{"enabled":true}`), Client: json.RawMessage(`{"enabled":true}`)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inbound := &model.Inbound{
				Id:      1,
				Type:    "http",
				Tag:     "http-test",
				Options: json.RawMessage(test.options),
				Addrs:   json.RawMessage(`[]`),
				OutJson: json.RawMessage(test.outbound),
				Tls:     test.tls,
			}
			if test.tls != nil {
				inbound.TlsId = 1
			}
			clientConfig := test.client
			if clientConfig == "" {
				clientConfig = `{}`
			}
			outbounds, tags, err := service.getOutbounds(json.RawMessage(clientConfig), []*model.Inbound{inbound})
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("build JSON subscription outbounds: %v", err)
			}
			if test.name == "H2C-only listener stays available to external clients without a generated node" {
				if len(*outbounds) != 0 || len(*tags) != 0 {
					t.Fatalf("h2c-only server emitted native HTTP outbound: %#v %#v", *outbounds, *tags)
				}
				return
			}
			if len(*outbounds) != 1 || len(*tags) != 1 {
				t.Fatalf("outbounds=%#v tags=%#v, want one", *outbounds, *tags)
			}
			outbound := (*outbounds)[0]
			if version, ok := outbound["version"].(int); !ok || version != test.want {
				t.Fatalf("version = %#v, want %d", version, test.want)
			}
		})
	}
}

func TestJSONSubscriptionAcceptsLegacyInboundAndRejectsInvalidClientVersionShapes(t *testing.T) {
	inbound := &model.Inbound{
		Type:    "http",
		Tag:     "http-singleton",
		Options: json.RawMessage(`{"version":[1]}`),
		OutJson: json.RawMessage(`{"type":"http","tag":"http-singleton","version":1}`),
		Addrs:   json.RawMessage(`[]`),
	}
	if outbounds, _, err := (&JsonService{}).getOutbounds(json.RawMessage(`{}`), []*model.Inbound{inbound}); err != nil || len(*outbounds) != 1 {
		t.Fatalf("legacy array inbound should remain subscribable, outbounds=%v err=%v", outbounds, err)
	}
	inbound.Options = json.RawMessage(`{"version":1}`)
	_, _, err := (&JsonService{}).getOutbounds(json.RawMessage(`{"http":{"version":[2]}}`), []*model.Inbound{inbound})
	if err == nil || !strings.Contains(err.Error(), "HTTP client version override must be 0, 1, 2, or 3") {
		t.Fatalf("invalid singleton client version shape error = %v", err)
	}
}

func TestJSONSubscriptionPreservesLegacyHTTPDefaultsAndVersion(t *testing.T) {
	tls := enabledSubHTTPTestTLS()
	tests := []struct {
		name        string
		options     string
		outbound    string
		wantVersion int
		wantAbsent  bool
	}{
		{
			name:       "omitted inbound version keeps Sing-Box default",
			options:    `{"listen_port":8080}`,
			outbound:   `{"type":"http","tag":"legacy-default","tls":{"enabled":true}}`,
			wantAbsent: true,
		},
		{
			name:        "legacy version list preserves configured HTTP/2",
			options:     `{"listen_port":8081,"version":[1,2]}`,
			outbound:    `{"type":"http","tag":"legacy-h2","version":2,"tls":{"enabled":true}}`,
			wantVersion: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inbound := &model.Inbound{
				Id:      1,
				Type:    "http",
				Tag:     "legacy-http",
				Options: json.RawMessage(test.options),
				Addrs:   json.RawMessage(`[]`),
				OutJson: json.RawMessage(test.outbound),
				TlsId:   1,
				Tls:     tls,
			}
			outbounds, _, err := (&JsonService{}).getOutbounds(json.RawMessage(`{}`), []*model.Inbound{inbound})
			if err != nil {
				t.Fatalf("build legacy HTTP subscription: %v", err)
			}
			if len(*outbounds) != 1 {
				t.Fatalf("got %d outbounds, want 1", len(*outbounds))
			}
			outbound := (*outbounds)[0]
			version, exists := outbound["version"]
			if test.wantAbsent {
				if exists {
					t.Fatalf("legacy default version was rewritten to %#v", version)
				}
			} else if version != float64(test.wantVersion) {
				t.Fatalf("version = %#v, want %v", version, test.wantVersion)
			}
		})
	}
}

func TestJSONSubscriptionSingletonListenerOverridesStaleClientVersion(t *testing.T) {
	tests := []struct {
		name          string
		options       string
		outbound      string
		clientVersion int
		version       int
		wantALPN      string
		wantFallback  bool
		tls           *model.Tls
	}{
		{
			name:          "H1 listener",
			options:       `{"version":1}`,
			outbound:      `{"type":"http","tag":"http-h1","version":3}`,
			clientVersion: 2,
			version:       1,
		},
		{
			name:          "H2 listener",
			options:       `{"version":2}`,
			outbound:      `{"type":"http","tag":"http-h2","version":1,"tls":{"enabled":true,"alpn":["http/1.1","h2"]}}`,
			clientVersion: 1,
			version:       2,
			wantALPN:      "h2",
			wantFallback:  true,
			tls:           enabledSubHTTPTestTLS(),
		},
		{
			name:          "H3 listener",
			options:       `{"version":3}`,
			outbound:      `{"type":"http","tag":"http-h3","version":2,"tls":{"enabled":true,"alpn":["h2","h3"]}}`,
			clientVersion: 2,
			version:       3,
			wantALPN:      "h3",
			tls:           enabledSubHTTPTestTLS(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inbound := &model.Inbound{
				Id:      10,
				Type:    "http",
				Tag:     "http-test",
				Options: json.RawMessage(test.options),
				Addrs:   json.RawMessage(`[]`),
				OutJson: json.RawMessage(test.outbound),
				Tls:     test.tls,
			}
			if test.tls != nil {
				inbound.TlsId = 1
			}
			clientConfig := json.RawMessage(`{"http":{"version":` + strconv.Itoa(test.clientVersion) + `,"username":"proxy-user","password":"proxy-secret"}}`)
			outbounds, _, err := (&JsonService{}).getOutbounds(clientConfig, []*model.Inbound{inbound})
			if err != nil {
				t.Fatalf("build JSON subscription: %v", err)
			}
			if len(*outbounds) != 1 {
				t.Fatalf("got %d outbounds, want 1", len(*outbounds))
			}
			outbound := (*outbounds)[0]
			if got := outbound["version"]; got != test.version {
				t.Fatalf("version = %#v, want %d", got, test.version)
			}
			if outbound["username"] != "proxy-user" || outbound["password"] != "proxy-secret" {
				t.Fatalf("HTTP credentials were not retained: %#v", outbound)
			}
			gotFallback, _ := outbound["disable_version_fallback"].(bool)
			if gotFallback != test.wantFallback {
				t.Fatalf("disable_version_fallback = %t, want %t", gotFallback, test.wantFallback)
			}
			if test.wantALPN != "" {
				tls := outbound["tls"].(map[string]interface{})
				if got := tls["alpn"].([]interface{}); !reflect.DeepEqual(got, []interface{}{test.wantALPN}) {
					t.Fatalf("ALPN = %#v, want [%q]", got, test.wantALPN)
				}
			}
		})
	}
}

func TestJSONSubscriptionHTTPAddressTLSOverridesAreIndependent(t *testing.T) {
	inbound := &model.Inbound{
		Id:      11,
		Type:    "http",
		Tag:     "http-multi-address",
		TlsId:   1,
		Options: json.RawMessage(`{"version":2}`),
		OutJson: json.RawMessage(`{"type":"http","tag":"http-multi-address","tls":{"enabled":true,"server_name":"base.example","alpn":["http/1.1","h2"]}}`),
		Tls:     enabledSubHTTPTestTLS(),
		Addrs: json.RawMessage(`[
			{"server":"one.example","server_port":443,"tls":{"enabled":true,"server_name":"one.example","alpn":["h3"]}},
			{"server":"two.example","server_port":443,"tls":{"enabled":true,"server_name":"two.example","alpn":["h3"]}}
		]`),
	}
	outbounds, _, err := (&JsonService{}).getOutbounds(json.RawMessage(`{}`), []*model.Inbound{inbound})
	if err != nil {
		t.Fatalf("build address-specific HTTP outbounds: %v", err)
	}
	if len(*outbounds) != 2 {
		t.Fatalf("got %d address outbounds, want 2", len(*outbounds))
	}
	firstTLS := (*outbounds)[0]["tls"].(map[string]interface{})
	secondTLS := (*outbounds)[1]["tls"].(map[string]interface{})
	if firstTLS["server_name"] != "one.example" || secondTLS["server_name"] != "two.example" {
		t.Fatalf("address TLS overrides were mixed: %#v %#v", firstTLS, secondTLS)
	}
	for _, outbound := range *outbounds {
		tls := outbound["tls"].(map[string]interface{})
		if got := tls["alpn"].([]interface{}); !reflect.DeepEqual(got, []interface{}{"h2"}) {
			t.Fatalf("HTTP/2 ALPN = %#v, want [h2]", got)
		}
	}
	firstTLS["server_name"] = "mutated.example"
	firstTLS["alpn"].([]interface{})[0] = "mutated"
	if secondTLS["server_name"] != "two.example" || !reflect.DeepEqual(secondTLS["alpn"], []interface{}{"h2"}) {
		t.Fatalf("mutating first address changed second address TLS: %#v", secondTLS)
	}
}

func enabledSubHTTPTestTLS() *model.Tls {
	return &model.Tls{
		Server: json.RawMessage(`{"enabled":true}`),
		Client: json.RawMessage(`{"enabled":true}`),
	}
}

func TestClashHTTPExportOmitsHTTP2AndHTTP3VariantsWithoutDowngrading(t *testing.T) {
	tlsEnabled := &model.Tls{Server: json.RawMessage(`{"enabled":true}`), Client: json.RawMessage(`{"enabled":true}`)}
	inbounds := []*model.Inbound{
		{Type: "http", Tag: "h1-only", Options: json.RawMessage(`{"version":1}`), Addrs: json.RawMessage(`[]`), TlsId: 1, Tls: tlsEnabled},
		{Type: "http", Tag: "h2-only", Options: json.RawMessage(`{"version":2}`), Addrs: json.RawMessage(`[]`)},
		{Type: "http", Tag: "h3-only", Options: json.RawMessage(`{"version":3}`), Addrs: json.RawMessage(`[]`), TlsId: 1, Tls: tlsEnabled},
	}
	outbounds := []map[string]interface{}{
		{"type": "http", "tag": "h1-only", "version": 1, "tls": map[string]interface{}{"enabled": true, "alpn": []interface{}{"h2", "h3"}}},
		{"type": "http", "tag": "h2-only", "version": 2},
		{"type": "http", "tag": "h3-only", "version": 3, "tls": map[string]interface{}{"enabled": true}},
		{"type": "mixed", "tag": "mixed-proxy"},
	}
	tags := []string{"h1-only", "h2-only", "h3-only", "mixed-proxy"}
	filtered, filteredTags, err := filterClashHTTPOutbounds(inbounds, outbounds, tags)
	if err != nil {
		t.Fatalf("filter Clash outbounds: %v", err)
	}
	if len(filtered) != 2 || len(filteredTags) != 2 {
		t.Fatalf("filtered outbounds=%#v tags=%#v, want only the explicit H1 variant plus mixed", filtered, filteredTags)
	}
	if filteredTags[0] != "h1-only" || filtered[0]["version"] != 1 {
		t.Fatalf("HTTP/1 server was not retained: %#v", filtered[0])
	}
	if got := filtered[0]["tls"].(map[string]interface{})["alpn"].([]interface{}); !reflect.DeepEqual(got, []interface{}{"http/1.1"}) {
		t.Fatalf("Clash H1 ALPN = %#v, want [http/1.1]", got)
	}
	if filteredTags[1] != "mixed-proxy" || filtered[1]["type"] != "mixed" {
		t.Fatalf("non-HTTP mixed outbound was not preserved: %#v", filtered[1])
	}
}

func TestClashHTTPExportOmitsUnknownNonH1Versions(t *testing.T) {
	outbounds := []map[string]interface{}{
		{"type": "http", "tag": "external-h3", "version": 3, "tls": map[string]interface{}{"enabled": true}},
		{"type": "http", "tag": "external-h2", "version": float64(2), "tls": map[string]interface{}{"enabled": true}},
		{"type": "http", "tag": "external-h1", "version": 1},
		{"type": "http", "tag": "legacy-http"},
		{"type": "mixed", "tag": "mixed-proxy"},
	}
	tags := []string{"external-h3", "external-h2", "external-h1", "legacy-http", "mixed-proxy"}
	filtered, filteredTags, err := filterClashHTTPOutbounds(nil, outbounds, tags)
	if err != nil {
		t.Fatalf("filter unknown-source Clash outbounds: %v", err)
	}
	if len(filtered) != 3 || len(filteredTags) != 3 {
		t.Fatalf("outbounds=%#v tags=%#v, want explicit H1, legacy HTTP, and mixed", filtered, filteredTags)
	}
	if filteredTags[0] != "external-h1" || filteredTags[1] != "legacy-http" || filteredTags[2] != "mixed-proxy" {
		t.Fatalf("unexpected retained unknown-source outbounds: %#v", filteredTags)
	}
}

func TestClashHTTPExportUsesHTTP1ForLegacyDefaultInbound(t *testing.T) {
	inbound := &model.Inbound{
		Type:    "http",
		Tag:     "legacy-http",
		Options: json.RawMessage(`{"listen_port":8080}`),
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"http","tag":"legacy-http","version":2,"tls":{"enabled":true}}`),
		TlsId:   1,
		Tls:     enabledSubHTTPTestTLS(),
	}
	outbounds := []map[string]interface{}{{
		"type": "http", "tag": "legacy-http", "version": 2,
		"tls": map[string]interface{}{"enabled": true, "alpn": []interface{}{"h2"}},
	}}
	filtered, tags, err := filterClashHTTPOutbounds([]*model.Inbound{inbound}, outbounds, []string{"legacy-http"})
	if err != nil {
		t.Fatalf("filter legacy HTTP for Clash: %v", err)
	}
	if len(filtered) != 1 || len(tags) != 1 || tags[0] != "legacy-http" {
		t.Fatalf("legacy HTTP node was dropped: outbounds=%#v tags=%#v", filtered, tags)
	}
	if filtered[0]["version"] != 1 {
		t.Fatalf("Clash HTTP version = %#v, want HTTP/1", filtered[0]["version"])
	}
	if got := filtered[0]["tls"].(map[string]interface{})["alpn"].([]interface{}); !reflect.DeepEqual(got, []interface{}{"http/1.1"}) {
		t.Fatalf("Clash legacy HTTP ALPN = %#v, want [http/1.1]", got)
	}
}
