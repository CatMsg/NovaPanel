package sub

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseAggregateSourceClashProxies(t *testing.T) {
	data := `
proxies:
  - name: tk-masque
    type: masque
    server: tk.mile.news
    port: 443
    network: quic
    private-key: a
    public-key: b
    ip: 172.16.0.1/32
    mtu: 1280
    udp: true
  - name: tk-ss
    type: ss
    server: tk.mile.news
    port: 888
    cipher: aes-128-gcm
    password: test
`

	links, outbounds, proxies := parseAggregateSource(data)
	if len(links) != 0 {
		t.Fatalf("expected no plain links, got %v", links)
	}
	if len(outbounds) != 0 {
		t.Fatalf("expected no json outbounds, got %v", outbounds)
	}
	if len(proxies) != 2 {
		t.Fatalf("expected 2 clash proxies, got %d", len(proxies))
	}
}

func TestConvertToClashMetaWithExtraProxies(t *testing.T) {
	service := &ClashService{}
	outbounds := []map[string]interface{}{
		{
			"tag":         "local-ss",
			"type":        "shadowsocks",
			"server":      "127.0.0.1",
			"server_port": 8888,
			"method":      "aes-128-gcm",
			"password":    "pass",
		},
	}
	extra := []map[string]interface{}{
		{
			"name":        "remote-masque",
			"type":        "masque",
			"server":      "tk.mile.news",
			"port":        443,
			"network":     "quic",
			"private-key": "a",
			"public-key":  "b",
			"ip":          "172.16.0.1/32",
			"mtu":         1280,
			"udp":         true,
		},
	}

	result, err := service.ConvertToClashMetaWithExtraProxies(&outbounds, extra, basicClashConfig)
	if err != nil {
		t.Fatalf("convert clash meta: %v", err)
	}
	if !strings.Contains(result, "remote-masque") {
		t.Fatalf("expected remote masque proxy in result: %s", result)
	}

	var root map[string]interface{}
	if err := yaml.Unmarshal([]byte(result), &root); err != nil {
		t.Fatalf("yaml unmarshal result: %v", err)
	}
	proxies, ok := root["proxies"].([]interface{})
	if !ok || len(proxies) != 2 {
		t.Fatalf("expected 2 proxies in result, got %#v", root["proxies"])
	}
}
