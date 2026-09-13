package core

import (
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"

	"github.com/sagernet/sing-box/option"
)

func TestExplainRouteIncludesMatchedDNSServer(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "routing-diagnostics.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	runtime := NewCore()
	config := []byte(`{
		"log":{"disabled":true},
		"dns":{
			"servers":[
				{"type":"local","tag":"dns-local"},
				{"type":"udp","tag":"dns-media","server":"138.2.89.178","server_port":53}
			],
			"rules":[{"domain_suffix":["netflix.com"],"action":"route","server":"dns-media"}],
			"final":"dns-local"
		},
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"final":"direct"}
	}`)
	if err := runtime.Start(config); err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	})

	result, err := ExplainRoute(RouteExplainInput{
		Domain:    "www.netflix.com",
		Port:      53,
		Network:   "udp",
		Protocol:  "dns",
		QueryType: "A",
	})
	if err != nil {
		t.Fatalf("explain route: %v", err)
	}
	if result.DNS == nil {
		t.Fatal("expected DNS diagnosis")
	}
	if !result.DNS.Matched || result.DNS.DefaultUsed {
		t.Fatalf("expected matched DNS rule: %#v", result.DNS)
	}
	if result.DNS.Server != "dns-media" || result.DNS.ServerType != "udp" {
		t.Fatalf("unexpected DNS server: %#v", result.DNS)
	}
	if result.DNS.ServerAddress != "138.2.89.178" || result.DNS.ServerPort != 53 {
		t.Fatalf("unexpected DNS endpoint: %#v", result.DNS)
	}

	defaultResult, err := ExplainRoute(RouteExplainInput{
		Domain:    "example.com",
		Port:      53,
		Network:   "udp",
		Protocol:  "dns",
		QueryType: "A",
	})
	if err != nil {
		t.Fatalf("explain default DNS route: %v", err)
	}
	if defaultResult.DNS == nil || !defaultResult.DNS.DefaultUsed {
		t.Fatalf("expected default DNS server: %#v", defaultResult.DNS)
	}
	if defaultResult.DNS.Server != "dns-local" || defaultResult.DNS.ServerType != "local" {
		t.Fatalf("unexpected default DNS server: %#v", defaultResult.DNS)
	}
}

func TestConfiguredDNSServerEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		options any
		address string
		port    uint16
	}{
		{
			name: "udp",
			options: &option.RemoteDNSServerOptions{DNSServerAddressOptions: option.DNSServerAddressOptions{
				Server: "138.2.89.178", ServerPort: 53,
			}},
			address: "138.2.89.178",
			port:    53,
		},
		{
			name: "tls",
			options: &option.RemoteTLSDNSServerOptions{RemoteDNSServerOptions: option.RemoteDNSServerOptions{
				DNSServerAddressOptions: option.DNSServerAddressOptions{Server: "dns.example.com", ServerPort: 853},
			}},
			address: "dns.example.com",
			port:    853,
		},
		{
			name: "https",
			options: &option.RemoteHTTPSDNSServerOptions{RemoteTLSDNSServerOptions: option.RemoteTLSDNSServerOptions{
				RemoteDNSServerOptions: option.RemoteDNSServerOptions{
					DNSServerAddressOptions: option.DNSServerAddressOptions{Server: "doh.example.com", ServerPort: 443},
				},
			}},
			address: "doh.example.com",
			port:    443,
		},
		{name: "local", options: &option.LocalDNSServerOptions{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address, port := configuredDNSServerEndpoint(option.DNSServerOptions{Options: test.options})
			if address != test.address || port != test.port {
				t.Fatalf("unexpected endpoint %q:%d", address, port)
			}
		})
	}
}

func TestApplyDNSServer(t *testing.T) {
	result := &DNSExplainResult{}
	applyDNSServer(result, dnsServerDescription{
		tag:      "dns-media",
		typeName: "udp",
		address:  "138.2.89.178",
		port:     53,
	})

	if result.Server != "dns-media" || result.ServerType != "udp" {
		t.Fatalf("unexpected DNS result identity: %#v", result)
	}
	if result.ServerAddress != "138.2.89.178" || result.ServerPort != 53 {
		t.Fatalf("unexpected DNS result endpoint: %#v", result)
	}
}
