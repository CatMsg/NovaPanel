package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
)

func withWarpTestServer(t *testing.T, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	oldBaseURL := warpAPIBaseURL
	oldClient := warpHTTPClient
	warpAPIBaseURL = server.URL
	warpHTTPClient = server.Client()
	t.Cleanup(func() {
		warpAPIBaseURL = oldBaseURL
		warpHTTPClient = oldClient
	})
}

func TestRegisterWarpRejectsHTTPFailure(t *testing.T) {
	withWarpTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "registration denied", http.StatusForbidden)
	}))

	endpoint := &model.Endpoint{Options: json.RawMessage(`{}`)}
	err := (&WarpService{}).RegisterWarp(endpoint)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("expected HTTP failure, got %v", err)
	}
}

func TestRegisterWarpRejectsIncompleteResponse(t *testing.T) {
	withWarpTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"device-only"}`))
	}))

	endpoint := &model.Endpoint{Options: json.RawMessage(`{}`)}
	if err := (&WarpService{}).RegisterWarp(endpoint); err == nil {
		t.Fatal("expected incomplete registration response to fail")
	}
}

func TestRegisterWarpBuildsEndpointConfiguration(t *testing.T) {
	withWarpTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/reg":
			_, _ = w.Write([]byte(`{
				"id":"device-id",
				"token":"access-token",
				"account":{"license":"license-key"}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/reg/device-id":
			if r.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("unexpected authorization header: %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{
				"config":{
					"client_id":"AQID",
					"interface":{"addresses":{"v4":"172.16.0.2","v6":"2606:4700:110:8765::2"}},
					"peers":[{
						"endpoint":{"host":"162.159.192.1:2408"},
						"public_key":"peer-public-key"
					}]
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))

	endpoint := &model.Endpoint{Options: json.RawMessage(`{"tag":"ignored"}`)}
	if err := (&WarpService{}).RegisterWarp(endpoint); err != nil {
		t.Fatalf("register WARP: %v", err)
	}

	var ext map[string]string
	if err := json.Unmarshal(endpoint.Ext, &ext); err != nil {
		t.Fatalf("decode extension: %v", err)
	}
	if ext["device_id"] != "device-id" || ext["access_token"] != "access-token" || ext["license_key"] != "license-key" {
		t.Fatalf("unexpected extension data: %#v", ext)
	}

	var options map[string]interface{}
	if err := json.Unmarshal(endpoint.Options, &options); err != nil {
		t.Fatalf("decode options: %v", err)
	}
	addresses, ok := options["address"].([]interface{})
	if !ok || len(addresses) != 2 || addresses[0] != "172.16.0.2/32" {
		t.Fatalf("unexpected addresses: %#v", options["address"])
	}
	peers, ok := options["peers"].([]interface{})
	if !ok || len(peers) != 1 {
		t.Fatalf("unexpected peers: %#v", options["peers"])
	}
}

func TestSetWarpLicenseRejectsHTTPFailure(t *testing.T) {
	withWarpTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "license denied", http.StatusBadRequest)
	}))

	ext, err := json.Marshal(map[string]string{
		"device_id":    "device-id",
		"access_token": "access-token",
		"license_key":  "new-license",
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &model.Endpoint{Ext: ext}
	err = (&WarpService{}).SetWarpLicense("old-license", endpoint)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("expected HTTP failure, got %v", err)
	}
}
