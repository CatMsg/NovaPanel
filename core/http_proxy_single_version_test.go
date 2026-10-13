package core

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
)

func TestHTTPProxyGeneratedSingletonVersionLoopback(t *testing.T) {
	initHTTPProxyTestDatabase(t)
	echo := startHTTPProxyEchoEndpoints(t)
	tlsFiles := makeHTTPProxyTestTLS(t)

	for _, version := range []int{1, 2, 3} {
		version := version
		t.Run(fmt.Sprintf("http%d", version), func(t *testing.T) {
			if version == 3 && !http3AvailableInBuild() {
				t.Skip("HTTP/3 integration requires a with_quic test build")
			}

			alpn := map[int]string{1: "http/1.1", 2: "h2", 3: "h3"}[version]
			serverAddress := reserveLoopbackPort(t)
			serverPort := portFromAddress(t, serverAddress)
			serverInbound := httpProxyServerInboundOptions(serverPort, []int{version}, tlsFiles, httpProxyTestPassword)
			serverInbound["tls"].(map[string]any)["alpn"] = []string{alpn}
			serverConfig := marshalHTTPProxyTestConfig(map[string]any{
				"inbounds":  []any{serverInbound},
				"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
				"route":     map[string]any{"final": "direct"},
			})
			runtime := startHTTPProxyTestCore(t, serverConfig)

			staleVersion := 2
			if version == 2 {
				staleVersion = 1
			}
			outJSON, err := json.Marshal(map[string]any{
				"type":        "http",
				"tag":         "stale",
				"server":      "stale.example",
				"server_port": 1,
				"username":    httpProxyTestUser,
				"password":    httpProxyTestPassword,
				"version":     staleVersion,
			})
			if err != nil {
				t.Fatalf("marshal stale synthetic outbound fixture: %v", err)
			}
			inboundOptions, err := json.Marshal(map[string]any{
				"listen":      "127.0.0.1",
				"listen_port": serverPort,
				"version":     version,
			})
			if err != nil {
				t.Fatalf("marshal singleton HTTP inbound fixture: %v", err)
			}
			serverTLS, err := json.Marshal(map[string]any{
				"enabled":     true,
				"server_name": "localhost",
				"alpn":        []string{alpn},
			})
			if err != nil {
				t.Fatalf("marshal synthetic HTTP server TLS fixture: %v", err)
			}
			clientTLS, err := json.Marshal(map[string]any{
				"enabled":          true,
				"server_name":      "localhost",
				"certificate_path": tlsFiles.rootPath,
			})
			if err != nil {
				t.Fatalf("marshal trusted HTTP client TLS fixture: %v", err)
			}
			inbound := &model.Inbound{
				Type:    "http",
				Tag:     "test-http",
				TlsId:   1,
				Tls:     &model.Tls{Server: serverTLS, Client: clientTLS},
				Options: inboundOptions,
				OutJson: outJSON,
			}
			if err := util.FillOutJson(inbound, "127.0.0.1"); err != nil {
				t.Fatalf("generate HTTP/%d outbound from singleton inbound: %v", version, err)
			}

			var generated struct {
				Type                   string `json:"type"`
				Tag                    string `json:"tag"`
				Server                 string `json:"server"`
				ServerPort             int    `json:"server_port"`
				Username               string `json:"username"`
				Password               string `json:"password"`
				Version                int    `json:"version"`
				DisableVersionFallback bool   `json:"disable_version_fallback"`
				TLS                    struct {
					Enabled         bool     `json:"enabled"`
					ServerName      string   `json:"server_name"`
					CertificatePath string   `json:"certificate_path"`
					ALPN            []string `json:"alpn"`
				} `json:"tls"`
			}
			if err := json.Unmarshal(inbound.OutJson, &generated); err != nil {
				t.Fatalf("decode generated HTTP/%d outbound %s: %v", version, inbound.OutJson, err)
			}
			if generated.Type != "http" {
				t.Errorf("generated outbound type = %q, want %q (outbound: %s)", generated.Type, "http", inbound.OutJson)
			}
			if generated.Version != version {
				t.Errorf("generated outbound version = %d, want singleton inbound version %d (stale out_json version %d; outbound: %s)", generated.Version, version, staleVersion, inbound.OutJson)
			}
			if generated.Tag != inbound.Tag || generated.Server != "127.0.0.1" || generated.ServerPort != serverPort {
				t.Errorf("generated outbound endpoint = tag:%q server:%q port:%d, want tag:%q server:%q port:%d", generated.Tag, generated.Server, generated.ServerPort, inbound.Tag, "127.0.0.1", serverPort)
			}
			if generated.Username != httpProxyTestUser || generated.Password != httpProxyTestPassword {
				t.Errorf("generated credentials = %q/%q, want synthetic credentials %q/%q", generated.Username, generated.Password, httpProxyTestUser, httpProxyTestPassword)
			}
			if !generated.TLS.Enabled || generated.TLS.ServerName != "localhost" || generated.TLS.CertificatePath != tlsFiles.rootPath {
				t.Errorf("generated TLS = enabled:%t server_name:%q certificate_path:%q, want enabled:true server_name:%q certificate_path:%q", generated.TLS.Enabled, generated.TLS.ServerName, generated.TLS.CertificatePath, "localhost", tlsFiles.rootPath)
			}
			if len(generated.TLS.ALPN) != 1 || generated.TLS.ALPN[0] != alpn {
				t.Errorf("generated TLS ALPN = %#v, want exactly [%q] for HTTP/%d", generated.TLS.ALPN, alpn, version)
			}
			if version == 2 && !generated.DisableVersionFallback {
				t.Errorf("generated HTTP/2 disable_version_fallback = %t, want true (outbound: %s)", generated.DisableVersionFallback, inbound.OutJson)
			}

			var generatedOutbound map[string]any
			if err := json.Unmarshal(inbound.OutJson, &generatedOutbound); err != nil {
				t.Fatalf("decode generated outbound for sing-box client config: %v", err)
			}
			clientConfig := marshalHTTPProxyTestConfig(map[string]any{
				"outbounds": []any{generatedOutbound},
				"route":     map[string]any{"final": generated.Tag},
			})
			client := startHTTPProxyTestBox(t, clientConfig)
			proxy, ok := client.Outbound().Outbound(generated.Tag)
			if !ok {
				t.Fatal("generated HTTP proxy outbound was not registered")
			}
			assertHTTPProxyRoundTrip(t, proxy, runtime.GetInstance(), echo, version, "generated singleton")
		})
	}
}
