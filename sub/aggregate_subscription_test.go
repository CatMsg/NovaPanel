package sub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
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
    uri: https://edge.example.com/.well-known/masque/ip/*/*/
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
	if proxy["ip"] != "172.16.0.2/32" || proxy["sni"] != "edge.example.com" || proxy["uri"] != "https://edge.example.com/.well-known/masque/ip/*/*/" {
		t.Fatalf("masque connection fields were not preserved: %#v", proxy)
	}
}

func TestConvertRawClashProxiesKeepsMasqueAndDeduplicatesNames(t *testing.T) {
	service := ClashService{}
	proxies := []map[string]interface{}{
		{
			"name": "edge-masque", "type": "masque", "server": "edge.example.com", "port": 443,
			"network": "quic", "private-key": "client-private", "public-key": "server-public",
			"uri": "https://edge.example.com/.well-known/masque/ip/*/*/",
			"ip":  "172.16.0.2/32", "sni": "edge.example.com", "mtu": 1380, "udp": true,
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
	if proxy["type"] != "masque" || proxy["private-key"] != "client-private" || proxy["mtu"] != 1380 || proxy["uri"] != "https://edge.example.com/.well-known/masque/ip/*/*/" {
		t.Fatalf("raw masque fields were changed: %#v", proxy)
	}
}

func TestConvertRawClashHTTPProxiesKeepsOnlyH1AndClampsALPN(t *testing.T) {
	service := ClashService{}
	input := []map[string]interface{}{
		{"name": "http-h2", "type": "http", "server": "h2.example", "port": 443, "version": 2, "tls": true, "alpn": []interface{}{"h2"}},
		{"name": "http-h3", "type": "http", "server": "h3.example", "port": 443, "version": 3, "tls": true, "alpn": []interface{}{"h3"}},
		{"name": "http-h1", "type": "http", "server": "h1.example", "port": 443, "version": 1, "tls": true, "alpn": []interface{}{"h2", "h3"}},
		{"name": "http-legacy", "type": "http", "server": "legacy.example", "port": 80},
	}
	result, err := service.ConvertRawClashProxies(input, basicClashConfig)
	if err != nil {
		t.Fatalf("render raw HTTP proxies: %v", err)
	}
	var config struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(result), &config); err != nil {
		t.Fatalf("parse rendered raw HTTP proxies: %v", err)
	}
	if len(config.Proxies) != 2 || config.Proxies[0]["name"] != "http-h1" || config.Proxies[1]["name"] != "http-legacy" {
		t.Fatalf("raw Clash HTTP variants = %#v, want explicit H1 and legacy only", config.Proxies)
	}
	if got := config.Proxies[0]["alpn"].([]interface{}); len(got) != 1 || got[0] != "http/1.1" {
		t.Fatalf("raw Clash H1 ALPN = %#v, want [http/1.1]", got)
	}
	if got := input[2]["alpn"].([]interface{}); len(got) != 2 || got[0] != "h2" || got[1] != "h3" {
		t.Fatalf("raw proxy input was mutated: %#v", input[2])
	}
}

func TestAggregateJSONPreservesStructuredHTTPAndFallsBackForPlainSource(t *testing.T) {
	var structuredRequests atomic.Int32
	var collisionRequests atomic.Int32
	var plainRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("request format = %q, want json", r.URL.Query().Get("format"))
		}
		switch r.URL.Path {
		case "/structured":
			structuredRequests.Add(1)
			w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=500; expire=900")
			_, _ = w.Write([]byte(`{"outbounds":[
				{"type":"http","tag":"edge","server":"h2.example","server_port":443,"username":"h2-user","password":"h2-pass","version":2,"disable_version_fallback":true,"tls":{"enabled":true,"server_name":"h2.example","alpn":["h2"]}},
				{"type":"http","tag":"h3","server":"h3.example","server_port":443,"username":"h3-user","password":"h3-pass","version":3,"tls":{"enabled":true,"server_name":"h3.example","alpn":["h3"]}},
				{"type":"selector","tag":"remote-selector","outbounds":["edge"]},
				{"type":"urltest","tag":"remote-auto","outbounds":["edge"]},
				{"type":"dns","tag":"remote-dns"},
				{"type":"direct","tag":"direct"},
				{"type":"trojan","tag":"depends-on-skipped-selector","server":"x.example","server_port":443,"password":"secret","detour":"remote-selector"},
				{"type":"trojan","tag":"depends-on-skipped-urltest","server":"x.example","server_port":443,"password":"secret","detour":"remote-auto"},
				{"type":"trojan","tag":"depends-on-skipped-dns","server":"x.example","server_port":443,"password":"secret","detour":"remote-dns"}
			]}`))
		case "/collision":
			collisionRequests.Add(1)
			w.Header().Set("Subscription-Userinfo", "upload=3; download=4; total=600; expire=800")
			_, _ = w.Write([]byte(`{"outbounds":[
				{"type":"http","tag":"edge","server":"other-h2.example","server_port":443,"username":"other-user","password":"other-pass","version":2,"disable_version_fallback":true,"tls":{"enabled":true,"alpn":["h2"]}},
				{"type":"socks","tag":"detour-child","server":"child.example","server_port":1080,"detour":"edge"}
			]}`))
		case "/plain":
			plainRequests.Add(1)
			w.Header().Set("Subscription-Userinfo", "upload=5; download=6; total=700; expire=700")
			// This source ignores format=json and returns the legacy plain-link body.
			_, _ = w.Write([]byte("vless://alice@plain.example:443#plain-node\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := database.InitDB(filepath.Join(t.TempDir(), "aggregate-json.db")); err != nil {
		t.Fatalf("init temporary DB: %v", err)
	}
	structuredSource := server.URL + "/structured?token=source-token"
	seedSubscriptionTestSettings(t,
		model.Setting{Key: "subMode", Value: "master"},
		model.Setting{Key: "subMasterSources", Value: strings.Join([]string{
			structuredSource,
			structuredSource + "&format=clash",
			server.URL + "/collision",
			server.URL + "/plain",
		}, "\n")},
		model.Setting{Key: "subJsonExt", Value: `{"dns":{"servers":[{"type":"local","tag":"local-dns"}],"rules":[]}}`},
		model.Setting{Key: "subUpdates", Value: "24"},
	)

	result, headers, err := (&AggregateService{}).GetAggregate("json", "panel.example")
	if err != nil {
		t.Fatalf("build aggregate JSON: %v", err)
	}
	if structuredRequests.Load() != 1 || collisionRequests.Load() != 1 || plainRequests.Load() != 1 {
		t.Fatalf("source requests = structured:%d collision:%d plain:%d; duplicate source must fetch once", structuredRequests.Load(), collisionRequests.Load(), plainRequests.Load())
	}
	var config struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(*result), &config); err != nil {
		t.Fatalf("decode aggregate JSON: %v", err)
	}
	byTag := make(map[string]map[string]interface{})
	for _, outbound := range config.Outbounds {
		byTag[asString(outbound["tag"])] = outbound
	}
	for _, tag := range []string{"edge", "h3", "3.edge", "detour-child", "plain-node"} {
		if byTag[tag] == nil {
			t.Errorf("missing expected outbound tag %q", tag)
		}
	}
	for _, tag := range []string{"remote-selector", "remote-auto", "remote-dns", "depends-on-skipped-selector", "depends-on-skipped-urltest", "depends-on-skipped-dns"} {
		if byTag[tag] != nil {
			t.Errorf("unexpected remote bookkeeping/dependent outbound %q: %#v", tag, byTag[tag])
		}
	}
	for _, test := range []struct {
		tag      string
		version  int
		username string
		password string
		alpn     string
	}{
		{tag: "edge", version: 2, username: "h2-user", password: "h2-pass", alpn: "h2"},
		{tag: "h3", version: 3, username: "h3-user", password: "h3-pass", alpn: "h3"},
	} {
		outbound := byTag[test.tag]
		if outbound["version"] != float64(test.version) || outbound["username"] != test.username || outbound["password"] != test.password {
			t.Errorf("HTTP outbound %q lost version or credentials: %#v", test.tag, outbound)
		}
		tls := outbound["tls"].(map[string]interface{})
		if tls["server_name"] == nil || !jsonValueEquals(tls["alpn"], []interface{}{test.alpn}) {
			t.Errorf("HTTP outbound %q lost TLS settings: %#v", test.tag, tls)
		}
	}
	if got := asString(byTag["detour-child"]["detour"]); got != "3.edge" {
		t.Errorf("collision detour = %q, want remapped tag 3.edge", got)
	}
	if len(headers) == 0 || headers[0] != "upload=9; download=12; total=1800; expire=700" {
		t.Errorf("aggregate usage header = %#v, want each unique source counted once", headers)
	}
}

func TestAggregateEmptyStructuredJSONDoesNotBecomePlainLinks(t *testing.T) {
	outbounds, structured, err := aggregateJSONOutbounds(`{"outbounds":[]}`)
	if err != nil {
		t.Fatalf("parse empty structured JSON: %v", err)
	}
	if !structured || len(outbounds) != 0 {
		t.Fatalf("empty outbounds parsed as structured=%t nodes=%#v; want structured empty result", structured, outbounds)
	}
	if links := aggregatePlainLinks(`{"outbounds":[]}`); len(links) != 1 {
		t.Fatalf("test input unexpectedly has no raw fallback line: %#v", links)
	}
}

func TestAggregateOutboundReferencesRemainValidAcrossCollisionsAndDedup(t *testing.T) {
	var outbounds []map[string]interface{}
	var tags []string
	seenNodes := make(map[string]string)
	seenTags := map[string]struct{}{"proxy": {}, "auto": {}, "direct": {}}
	localDNS := map[string]struct{}{"local-dns": {}}

	base := map[string]interface{}{"type": "socks", "tag": "edge", "server": "same.example", "server_port": 1080}
	if err := appendAggregateOutbounds([]map[string]interface{}{base}, 0, localDNS, &outbounds, &tags, seenNodes, seenTags); err != nil {
		t.Fatalf("append first source: %v", err)
	}
	collision := map[string]interface{}{"type": "socks", "tag": "edge", "server": "other.example", "server_port": 1080}
	child := map[string]interface{}{"type": "socks", "tag": "child", "server": "child.example", "server_port": 1080, "detour": "edge"}
	selectorDependent := map[string]interface{}{"type": "trojan", "tag": "selector-dependent", "server": "x.example", "server_port": 443, "password": "secret", "detour": "remote-selector"}
	dnsDependent := map[string]interface{}{"type": "trojan", "tag": "dns-dependent", "server": "x.example", "server_port": 443, "password": "secret", "detour": "remote-dns"}
	resolverDependent := map[string]interface{}{"type": "socks", "tag": "resolver-dependent", "server": "x.example", "server_port": 1080, "domain_resolver": "unknown-dns"}
	localResolver := map[string]interface{}{"type": "socks", "tag": "local-resolver", "server": "local.example", "server_port": 1080, "domain_resolver": "local-dns"}
	secondSource := []map[string]interface{}{
		collision,
		child,
		selectorDependent,
		dnsDependent,
		resolverDependent,
		localResolver,
		{"type": "selector", "tag": "remote-selector", "outbounds": []string{"edge"}},
		{"type": "dns", "tag": "remote-dns"},
	}
	if err := appendAggregateOutbounds(secondSource, 1, localDNS, &outbounds, &tags, seenNodes, seenTags); err != nil {
		t.Fatalf("append colliding source: %v", err)
	}
	if err := appendAggregateOutbounds([]map[string]interface{}{
		base,
		{"type": "socks", "tag": "dedup-child", "server": "dedup.example", "server_port": 1080, "detour": "edge"},
	}, 2, localDNS, &outbounds, &tags, seenNodes, seenTags); err != nil {
		t.Fatalf("append duplicate source: %v", err)
	}
	duplicateTags := []map[string]interface{}{
		{"type": "socks", "tag": "duplicate", "server": "one.example", "server_port": 1080},
		{"type": "socks", "tag": "duplicate", "server": "two.example", "server_port": 1080},
		{"type": "socks", "tag": "ambiguous-dependent", "server": "child.example", "server_port": 1080, "detour": "duplicate"},
	}
	if err := appendAggregateOutbounds(duplicateTags, 3, localDNS, &outbounds, &tags, seenNodes, seenTags); err != nil {
		t.Fatalf("append ambiguous-tag source: %v", err)
	}

	byTag := make(map[string]map[string]interface{})
	for _, outbound := range outbounds {
		byTag[asString(outbound["tag"])] = outbound
	}
	if byTag["edge"] == nil || byTag["2.edge"] == nil || byTag["child"] == nil || byTag["dedup-child"] == nil || byTag["local-resolver"] == nil {
		t.Fatalf("expected independent, colliding, deduplicated and local-DNS nodes: %#v", byTag)
	}
	if byTag["child"]["detour"] != "2.edge" || byTag["dedup-child"]["detour"] != "edge" {
		t.Fatalf("detours were not remapped: child=%#v dedup-child=%#v", byTag["child"], byTag["dedup-child"])
	}
	for _, tag := range []string{"selector-dependent", "dns-dependent", "resolver-dependent", "ambiguous-dependent"} {
		if byTag[tag] != nil {
			t.Fatalf("node with unavailable or ambiguous dependency %q was retained: %#v", tag, byTag[tag])
		}
	}
	for _, outbound := range outbounds {
		if detour := asString(outbound["detour"]); detour != "" && byTag[detour] == nil {
			t.Errorf("dangling detour %q on %#v", detour, outbound)
		}
	}
}

func TestAggregateOutboundDirectDetoursRequireSourceLocalTargets(t *testing.T) {
	edge := map[string]interface{}{"type": "socks", "tag": "edge", "server": "edge.example", "server_port": 1080}
	child := map[string]interface{}{"type": "socks", "tag": "child", "server": "child.example", "server_port": 1080, "detour": "direct"}
	grandchild := map[string]interface{}{"type": "socks", "tag": "grandchild", "server": "grandchild.example", "server_port": 1080, "detour": "child"}
	tests := []struct {
		name     string
		source   []map[string]interface{}
		want     []map[string]interface{}
		wantTags []string
	}{
		{
			name: "proxy named direct is renamed and referenced",
			source: []map[string]interface{}{
				child,
				{"type": "socks", "tag": "direct", "server": "proxy.example", "server_port": 1080},
				grandchild,
			},
			want: []map[string]interface{}{
				{"type": "socks", "tag": "child", "server": "child.example", "server_port": 1080, "detour": "1.direct"},
				{"type": "socks", "tag": "1.direct", "server": "proxy.example", "server_port": 1080},
				grandchild,
			},
			wantTags: []string{"child", "1.direct", "grandchild"},
		},
		{
			name: "custom direct and its dependent chain are omitted",
			source: []map[string]interface{}{
				edge,
				grandchild,
				child,
				{"type": "direct", "tag": "direct", "bind_interface": "source-only", "routing_mark": 42},
			},
			want:     []map[string]interface{}{edge},
			wantTags: []string{"edge"},
		},
		{
			name:     "missing direct cannot bind to generated direct",
			source:   []map[string]interface{}{edge, grandchild, child},
			want:     []map[string]interface{}{edge},
			wantTags: []string{"edge"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var outbounds []map[string]interface{}
			var tags []string
			seenNodes := make(map[string]string)
			seenTags := map[string]struct{}{"proxy": {}, "auto": {}, "direct": {}}
			if err := appendAggregateOutbounds(test.source, 0, nil, &outbounds, &tags, seenNodes, seenTags); err != nil {
				t.Fatalf("append source: %v", err)
			}
			if !jsonValueEquals(outbounds, test.want) || !jsonValueEquals(tags, test.wantTags) {
				t.Fatalf("outbounds=%#v tags=%#v, want outbounds=%#v tags=%#v", outbounds, tags, test.want, test.wantTags)
			}
		})
	}
}

func TestAggregateOutboundDetourChainsRemainIndependentAcrossSources(t *testing.T) {
	child := map[string]interface{}{"type": "socks", "tag": "child", "server": "child.example", "server_port": 1080, "detour": "edge"}
	grandchild := map[string]interface{}{"type": "socks", "tag": "grandchild", "server": "grandchild.example", "server_port": 1080, "detour": "child"}
	shared := map[string]interface{}{"type": "socks", "tag": "shared", "server": "shared.example", "server_port": 1080}
	firstEdge := map[string]interface{}{"type": "socks", "tag": "edge", "server": "first.example", "server_port": 1080}
	secondEdge := map[string]interface{}{"type": "socks", "tag": "edge", "server": "second.example", "server_port": 1080}
	sharedChild := map[string]interface{}{"type": "socks", "tag": "shared-child", "server": "shared-child.example", "server_port": 1080, "detour": "shared"}
	sources := [][]map[string]interface{}{
		{firstEdge, child, grandchild, shared},
		{secondEdge, child, grandchild, shared, sharedChild},
	}
	var outbounds []map[string]interface{}
	var tags []string
	seenNodes := make(map[string]string)
	seenTags := map[string]struct{}{"proxy": {}, "auto": {}, "direct": {}}
	for index, source := range sources {
		if err := appendAggregateOutbounds(source, index, nil, &outbounds, &tags, seenNodes, seenTags); err != nil {
			t.Fatalf("append source %d: %v", index+1, err)
		}
	}
	want := []map[string]interface{}{
		firstEdge, child, grandchild, shared,
		{"type": "socks", "tag": "2.edge", "server": "second.example", "server_port": 1080},
		{"type": "socks", "tag": "2.child", "server": "child.example", "server_port": 1080, "detour": "2.edge"},
		{"type": "socks", "tag": "2.grandchild", "server": "grandchild.example", "server_port": 1080, "detour": "2.child"},
		sharedChild,
	}
	wantTags := []string{"edge", "child", "grandchild", "shared", "2.edge", "2.child", "2.grandchild", "shared-child"}
	if !jsonValueEquals(outbounds, want) || !jsonValueEquals(tags, wantTags) {
		t.Fatalf("outbounds=%#v tags=%#v, want outbounds=%#v tags=%#v", outbounds, tags, want, wantTags)
	}
}

func jsonValueEquals(value interface{}, want interface{}) bool {
	left, leftErr := json.Marshal(value)
	right, rightErr := json.Marshal(want)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}

func TestAggregateSourceKeyDeduplicatesFormatVariants(t *testing.T) {
	first := "https://example.invalid/profile?token=private&format=clash"
	second := "https://example.invalid/profile?format=json&token=private"
	if got, want := aggregateSourceKey(first), aggregateSourceKey(second); got != want {
		t.Fatalf("source keys differ for format variants: %q != %q", got, want)
	}
	parsed, err := url.Parse(aggregateSourceKey(first))
	if err != nil {
		t.Fatalf("parse canonical key: %v", err)
	}
	if parsed.Query().Get("token") != "private" || parsed.Query().Get("format") != "" {
		t.Fatalf("canonical source key lost unrelated query values: %s", parsed.Redacted())
	}
}

func TestAggregateJSONKeepsMasqueClientEndpointAndDeduplicatesSource(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{
			"outbounds": [
				{"type":"http","tag":"http-proxy","server":"proxy.example","server_port":8080},
				{"type":"selector","tag":"upstream-selector","outbounds":["http-proxy","masque-client"]}
			],
			"endpoints": [
				{"type":"masque-client","tag":"masque-client","server":"masque.example","server_port":443,"detour":"http-proxy"},
				{"type":"masque-server","tag":"remote-masque-server","listen":"127.0.0.1","listen_port":4443}
			]
		}`))
	}))
	defer server.Close()

	if err := database.InitDB(filepath.Join(t.TempDir(), "aggregate-masque.db")); err != nil {
		t.Fatalf("init temporary DB: %v", err)
	}
	source := server.URL + "/profile?token=source-token"
	seedSubscriptionTestSettings(t,
		model.Setting{Key: "subMode", Value: "master"},
		model.Setting{Key: "subMasterSources", Value: strings.Join([]string{source, source}, "\n")},
		model.Setting{Key: "subJsonExt", Value: `{"dns":{"servers":[{"type":"local","tag":"local-dns"}],"rules":[]}}`},
		model.Setting{Key: "subUpdates", Value: "24"},
	)

	result, _, err := (&AggregateService{}).GetAggregate("json", "panel.example")
	if err != nil {
		t.Fatalf("build aggregate JSON: %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("upstream requests = %d, want one for duplicate source entries", requests.Load())
	}
	var config struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
		Endpoints []map[string]interface{} `json:"endpoints"`
	}
	if err := json.Unmarshal([]byte(*result), &config); err != nil {
		t.Fatalf("decode aggregate JSON: %v", err)
	}
	byTag := make(map[string]map[string]interface{}, len(config.Outbounds))
	for _, outbound := range config.Outbounds {
		tag := asString(outbound["tag"])
		byTag[tag] = outbound
		if outbound["type"] == "masque-client" || outbound["type"] == "masque-server" {
			t.Errorf("MASQUE endpoint leaked into outbounds: %#v", outbound)
		}
	}
	if byTag["http-proxy"] == nil || byTag["http-proxy"]["type"] != "http" {
		t.Fatalf("HTTP outbound was not retained: %#v", byTag)
	}
	selector := byTag["proxy"]
	if selector == nil || selector["type"] != "selector" {
		t.Fatalf("generated selector is missing: %#v", selector)
	}
	var hasMasqueClient bool
	for _, member := range selector["outbounds"].([]interface{}) {
		if member == "masque-client" {
			hasMasqueClient = true
		}
	}
	if !hasMasqueClient {
		t.Fatalf("generated selector lost MASQUE endpoint tag: %#v", selector["outbounds"])
	}
	if len(config.Endpoints) != 1 || config.Endpoints[0]["type"] != "masque-client" {
		t.Fatalf("aggregated endpoints = %#v, want only the MASQUE client endpoint", config.Endpoints)
	}
	endpoint := config.Endpoints[0]
	if got := asString(endpoint["tag"]); got != "masque-client" {
		t.Errorf("MASQUE client endpoint tag = %q, want masque-client", got)
	}
	if got := asString(endpoint["detour"]); got != "http-proxy" || byTag[got] == nil {
		t.Errorf("MASQUE client detour = %q, want existing HTTP outbound http-proxy", got)
	}
}
