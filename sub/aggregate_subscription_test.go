package sub

import (
	"net/url"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSubscriptionSourceWithFormatPreservesExistingQuery(t *testing.T) {
	got, err := subscriptionSourceWithFormat("https://example.com/sub/alice?token=secret&format=json", "clash")
	if err != nil {
		t.Fatalf("add format: %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Query().Get("token") != "secret" || parsed.Query().Get("format") != "clash" {
		t.Fatalf("unexpected source query: %s", got)
	}
}

func TestClashProxiesFromSourcePreservesMasqueFields(t *testing.T) {
	proxies, err := clashProxiesFromSource(`proxies:
  - name: edge-masque
    type: masque
    server: edge.example.com
    port: 443
    network: quic
    private-key: client-private
    public-key: server-public
    ip: 172.16.0.2/32
    sni: edge.example.com
    mtu: 1380
    udp: true
`)
	if err != nil {
		t.Fatalf("parse clash source: %v", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("expected one proxy, got %d", len(proxies))
	}
	proxy := proxies[0]
	if proxy["type"] != "masque" || proxy["private-key"] != "client-private" || proxy["public-key"] != "server-public" {
		t.Fatalf("masque credentials were not preserved: %#v", proxy)
	}
	if proxy["ip"] != "172.16.0.2/32" || proxy["sni"] != "edge.example.com" {
		t.Fatalf("masque connection fields were not preserved: %#v", proxy)
	}
}

func TestConvertRawClashProxiesKeepsMasqueAndDeduplicatesNames(t *testing.T) {
	service := ClashService{}
	proxies := []map[string]interface{}{
		{
			"name": "edge-masque", "type": "masque", "server": "edge.example.com", "port": 443,
			"network": "quic", "private-key": "client-private", "public-key": "server-public",
			"ip": "172.16.0.2/32", "sni": "edge.example.com", "mtu": 1380, "udp": true,
		},
		{"name": "edge-masque", "type": "ss", "server": "duplicate.example.com", "port": 8443},
	}

	result, err := service.ConvertRawClashProxies(proxies, basicClashConfig)
	if err != nil {
		t.Fatalf("render raw proxies: %v", err)
	}
	var config struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(result), &config); err != nil {
		t.Fatalf("parse rendered config: %v", err)
	}
	if len(config.Proxies) != 1 {
		t.Fatalf("expected duplicate names to be removed, got %#v", config.Proxies)
	}
	proxy := config.Proxies[0]
	if proxy["type"] != "masque" || proxy["private-key"] != "client-private" || proxy["mtu"] != 1380 {
		t.Fatalf("raw masque fields were changed: %#v", proxy)
	}
}
