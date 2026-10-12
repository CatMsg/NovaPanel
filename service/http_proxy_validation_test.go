package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
)

func TestValidateHTTPInboundWriteUsesSingleVersionContract(t *testing.T) {
	tests := []struct {
		name        string
		action      string
		options     string
		tls         *model.Tls
		tlsID       uint
		wantVersion string
		wantError   string
	}{
		{name: "new omitted defaults to one", action: "new", options: `{"listen_port":8080}`, wantVersion: "1"},
		{name: "edit omitted requires explicit choice", action: "edit", options: `{"listen_port":8080}`, wantError: "requires exactly one version choice"},
		{name: "scalar stays scalar", action: "edit", options: `{"listen_port":8080,"version":1}`, wantVersion: "1"},
		{name: "singleton array canonicalizes", action: "edit", options: `{"listen_port":8080,"version":[3]}`, tls: enabledHTTPTestTLS(), tlsID: 1, wantVersion: "3"},
		{name: "multiple versions rejected", action: "edit", options: `{"listen_port":8080,"version":[1,2]}`, wantError: "requires exactly one version choice"},
		{name: "H2 requires active TLS association", action: "edit", options: `{"listen_port":8080,"version":2}`, wantError: "version 2 requires an enabled TLS"},
		{name: "disabled TLS record is not active TLS", action: "edit", options: `{"listen_port":8080,"version":2}`, tls: &model.Tls{Server: json.RawMessage(`{"enabled":false}`)}, tlsID: 1, wantError: "version 2 requires an enabled TLS"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inbound := &model.Inbound{
				Type:    "http",
				Options: json.RawMessage(test.options),
				TlsId:   test.tlsID,
				Tls:     test.tls,
			}
			err := validateHTTPInboundWrite(inbound, test.action)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate write: %v", err)
			}
			var options map[string]json.RawMessage
			if err := json.Unmarshal(inbound.Options, &options); err != nil {
				t.Fatalf("decode canonical options: %v", err)
			}
			if got := string(options["version"]); got != test.wantVersion {
				t.Fatalf("canonical version = %s, want %s", got, test.wantVersion)
			}
			if options["listen_port"] == nil {
				t.Fatal("unrelated inbound options were not preserved")
			}
		})
	}
}

func enabledHTTPTestTLS() *model.Tls {
	return &model.Tls{Server: json.RawMessage(`{"enabled":true}`)}
}
