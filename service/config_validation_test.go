package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	singservice "github.com/sagernet/sing/service"
)

func TestValidateConfigReferencesAcceptsValidRoute(t *testing.T) {
	config := []byte(`{
		"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":1080}],
		"outbounds":[
			{"type":"direct","tag":"proxy-a"},
			{"type":"selector","tag":"select","outbounds":["proxy-a"]}
		],
		"route":{
			"final":"select",
			"rule_set":[{"type":"remote","tag":"remote","format":"binary","url":"https://example.com/rules.srs","download_detour":"proxy-a"}],
			"rules":[{"inbound":["mixed-in"],"rule_set":["remote"],"action":"route","outbound":"proxy-a"}]
		}
	}`)
	if err := validateConfigReferences(config); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidateConfigReferencesAcceptsRuleSetHTTPClientDetour(t *testing.T) {
	config := []byte(`{
		"outbounds":[{"type":"direct","tag":"proxy-a"}],
		"route":{"rule_set":[{
			"type":"remote",
			"tag":"remote",
			"format":"binary",
			"url":"https://example.com/rules.srs",
			"http_client":{"detour":"proxy-a"}
		}]}
	}`)
	if err := validateConfigReferences(config); err != nil {
		t.Fatalf("valid HTTP client detour rejected: %v", err)
	}
}

func TestValidateConfigReferencesRejectsMissingRuleSetHTTPClientDetour(t *testing.T) {
	config := []byte(`{
		"route":{"rule_set":[{
			"type":"remote",
			"tag":"remote",
			"url":"https://example.com/rules.srs",
			"http_client":{"detour":"missing"}
		}]}
	}`)
	err := validateConfigReferences(config)
	if err == nil || !strings.Contains(err.Error(), "http_client.detour") {
		t.Fatalf("expected missing HTTP client detour error, got %v", err)
	}
}

func TestValidateConfigReferencesRejectsMissingOutbound(t *testing.T) {
	config := []byte(`{"outbounds":[{"type":"direct","tag":"proxy-a"}],"route":{"final":"missing"}}`)
	err := validateConfigReferences(config)
	if err == nil || !strings.Contains(err.Error(), "route.final") {
		t.Fatalf("expected missing final outbound error, got %v", err)
	}
}

func TestValidateConfigReferencesRejectsMissingNestedRuleSet(t *testing.T) {
	config := []byte(`{"route":{"rules":[{"type":"logical","rules":[{"rule_set":["missing"]}]}]}}`)
	err := validateConfigReferences(config)
	if err == nil || !strings.Contains(err.Error(), "不存在的规则集") {
		t.Fatalf("expected missing rule-set error, got %v", err)
	}
}

func TestValidateConfigReferencesRejectsOutboundCycle(t *testing.T) {
	config := []byte(`{
		"outbounds":[
			{"type":"selector","tag":"a","outbounds":["b"]},
			{"type":"selector","tag":"b","outbounds":["a"]}
		]
	}`)
	err := validateConfigReferences(config)
	if err == nil || !strings.Contains(err.Error(), "形成循环") {
		t.Fatalf("expected outbound cycle error, got %v", err)
	}
}

func TestValidateConfigReferencesRejectsEmptySelector(t *testing.T) {
	config := []byte(`{"outbounds":[{"type":"selector","tag":"select","outbounds":[]}]}`)
	err := validateConfigReferences(config)
	if err == nil || !strings.Contains(err.Error(), "至少需要一个成员出站") {
		t.Fatalf("expected empty selector error, got %v", err)
	}
}

func TestValidateConfigReferencesDNS(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{name: "no DNS", config: `{}`},
		{name: "implicit direct outbound", config: `{"route":{"final":"direct"},"dns":{"servers":[{"type":"local","tag":"dns","detour":"direct"}],"final":"dns"}}`},
		{name: "DNS and outbound namespaces are independent", config: `{"outbounds":[{"type":"direct","tag":"dns"}],"dns":{"servers":[{"type":"local","tag":"dns"}],"final":"dns"}}`},
		{name: "empty final uses core default", config: `{"dns":{"servers":[],"final":""}}`},
		{name: "missing final", config: `{"dns":{"final":"missing"}}`, want: "dns.final 引用了不存在的 DNS 服务器: missing"},
		{name: "outbound is not DNS", config: `{"dns":{"final":"direct"}}`, want: "dns.final 引用了不存在的 DNS 服务器: direct"},
		{name: "duplicate tags", config: `{"dns":{"servers":[{"type":"local","tag":"dns"},{"type":"local","tag":"dns"}]}}`, want: "dns.servers[1].tag DNS 服务器标签重复: dns"},
		{name: "duplicate explicit numeric tags", config: `{"dns":{"servers":[{"type":"local","tag":"1"},{"type":"local"},{"type":"local","tag":"1"}]}}`, want: "dns.servers[2].tag DNS 服务器标签重复: 1"},
		{name: "multiple untagged use index tags", config: `{"dns":{"servers":[{"type":"local"},{"type":"local","tag":""}],"final":"1","rules":[{"server":"0"}]}}`},
		{name: "explicit numeric replaces implicit", config: `{"dns":{"servers":[{"type":"local"},{"type":"local","tag":"0"}],"final":"0"}}`},
		{name: "implicit numeric replaces explicit", config: `{"dns":{"servers":[{"type":"local","tag":"1"},{"type":"local"}],"final":"1"}}`},
		{name: "missing implicit index", config: `{"dns":{"servers":[{"type":"local"}],"final":"1"}}`, want: "dns.final 引用了不存在的 DNS 服务器: 1"},
		{name: "default DNS route", config: `{"dns":{"rules":[{"domain":"example.com","server":"missing"}]}}`, want: "dns.rules[0].server 引用了不存在的 DNS 服务器: missing"},
		{name: "explicit DNS route", config: `{"dns":{"rules":[{"action":"route","server":"missing"}]}}`, want: "dns.rules[0].server 引用了不存在的 DNS 服务器: missing"},
		{name: "DNS evaluate", config: `{"dns":{"rules":[{"action":"evaluate","server":"missing","tag":"result"}]}}`, want: "dns.rules[0].server 引用了不存在的 DNS 服务器: missing"},
		{name: "logical DNS action", config: `{"dns":{"rules":[{"type":"logical","mode":"and","rules":[{"domain":"example.com"}],"action":"evaluate","server":"missing"}]}}`, want: "dns.rules[0].server 引用了不存在的 DNS 服务器: missing"},
		// The reference walker also covers nested actions in older inputs. The
		// current core independently rejects actions on logical rule children.
		{name: "deep nested DNS action", config: `{"dns":{"rules":[{"type":"logical","rules":[{"type":"logical","rules":[{"action":"evaluate","server":"missing"}]}]}]}}`, want: "dns.rules[0].rules[0].rules[0].server 引用了不存在的 DNS 服务器: missing"},
		{name: "valid deep nested DNS reference", config: `{"dns":{"servers":[{"type":"local","tag":"dns"}],"rules":[{"type":"logical","rules":[{"type":"logical","rules":[{"server":"dns"}]}]}]}}`},
		{name: "route resolve", config: `{"route":{"rules":[{"action":"resolve","server":"missing"}]}}`, want: "route.rules[0].server 引用了不存在的 DNS 服务器: missing"},
		{name: "nested route resolve", config: `{"route":{"rules":[{"type":"logical","rules":[{"action":"resolve","server":"missing"}]}]}}`, want: "route.rules[0].rules[0].server 引用了不存在的 DNS 服务器: missing"},
		{name: "valid route resolve", config: `{"dns":{"servers":[{"type":"local","tag":"dns"}]},"route":{"rules":[{"type":"logical","rules":[{"domain":"example.com"}],"action":"resolve","server":"dns"}]}}`},
		{name: "DNS rule default server is optional", config: `{"dns":{"rules":[{"action":"evaluate"},{"domain":"example.com"}]},"route":{"rules":[{"action":"resolve"}]}}`},
		{name: "predefined records are not references", config: `{"dns":{"rules":[{"action":"predefined","answer":["example.com. 60 IN A 192.0.2.1"],"ns":"example.com. IN NS ns.example.com.","extra":["ns.example.com. IN A 192.0.2.2"]}]}}`},
		{name: "host and IP servers are not references", config: `{"dns":{"servers":[{"type":"udp","server":"192.0.2.53"},{"type":"https","server":"resolver.example.com"}]},"outbounds":[{"type":"socks","tag":"proxy","server":"proxy.example.com"}],"endpoints":[{"type":"wireguard","tag":"wg","peers":[{"server":"192.0.2.1"}]}],"ntp":{"server":"time.example.com"},"services":[{"type":"derp","mesh_with":[{"server":"derp.example.com"}]}]}`},
		{name: "headers and host map keys are data", config: `{"dns":{"servers":[{"type":"hosts","predefined":{"domain_resolver":["192.0.2.1"],"server":["192.0.2.2"]}},{"type":"https","server":"dns.example.com","headers":{"domain_resolver":"not-a-tag","server":"not-a-tag"}}]},"http_clients":[{"tag":"web","headers":{"default_domain_resolver":["not-a-tag"]}}]}`},
		{name: "named handshake key is data", config: `{"dns":{"servers":[{"type":"local","tag":"dns"}]},"inbounds":[{"type":"shadowtls","tag":"in","handshake_for_server_name":{"domain_resolver":{"server":"host.example.com","domain_resolver":"dns"}}}]}`},
		// These option types exist upstream but are not registered by core/register.go.
		{name: "cloudflared control resolver", config: `{"inbounds":[{"type":"cloudflared","tag":"in","control_dialer":{"domain_resolver":{"server":"missing"}}}]}`, want: "inbounds[0].control_dialer.domain_resolver.server 引用了不存在的 DNS 服务器: missing"},
		{name: "cloudflared tunnel resolver", config: `{"inbounds":[{"type":"cloudflared","tag":"in","tunnel_dialer":{"domain_resolver":"missing"}}]}`, want: "inbounds[0].tunnel_dialer.domain_resolver 引用了不存在的 DNS 服务器: missing"},
		{name: "service dashboard resolver", config: `{"services":[{"type":"api","dashboard":{"http_client":{"domain_resolver":"missing"}}}]}`, want: "services[0].dashboard.http_client.domain_resolver 引用了不存在的 DNS 服务器: missing"},
		{name: "mdns resolver", config: `{"dns":{"servers":[{"type":"mdns","domain_resolver":{"server":"missing"}}]}}`, want: "dns.servers[0].domain_resolver.server 引用了不存在的 DNS 服务器: missing"},
		{name: "DNS rule set validation preserved", config: `{"dns":{"rules":[{"type":"logical","rules":[{"rule_set":["missing"]}]}]}}`, want: "dns.rules[0].rules[0].rule_set 引用了不存在的规则集: missing"},
		{name: "DNS detour validation preserved", config: `{"dns":{"servers":[{"type":"udp","server":"192.0.2.53","detour":"missing"}]}}`, want: "dns.servers[0].detour 引用了不存在的出站: missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateConfigReferences([]byte(test.config))
			if test.want == "" {
				if err != nil {
					t.Fatalf("valid references rejected: %v", err)
				}
			} else if err == nil || err.Error() != test.want {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestValidateRuntimeConfigDomainResolvers(t *testing.T) {
	// Exercise the save/preflight validation entry point, including the real
	// option decoder, without starting the core or accessing any database.
	previousCore := corePtr
	corePtr = &core.Core{}
	t.Cleanup(func() { corePtr = previousCore })
	tests := []struct {
		name     string
		fragment string
		path     string
	}{
		{"mixed inbound", `"inbounds":[{"type":"mixed","tag":"in","domain_resolver":RESOLVER}]`, "inbounds[0].domain_resolver"},
		{"socks inbound", `"inbounds":[{"type":"socks","tag":"in","domain_resolver":RESOLVER}]`, "inbounds[0].domain_resolver"},
		{"outbound", `"outbounds":[{"type":"socks","tag":"out","server":"proxy.example.com","domain_resolver":RESOLVER}]`, "outbounds[0].domain_resolver"},
		{"endpoint", `"endpoints":[{"type":"wireguard","tag":"wg","domain_resolver":RESOLVER}]`, "endpoints[0].domain_resolver"},
		{"HTTP client", `"http_clients":[{"tag":"web","domain_resolver":RESOLVER}]`, "http_clients[0].domain_resolver"},
		{"NTP", `"ntp":{"server":"time.example.com","domain_resolver":RESOLVER}`, "ntp.domain_resolver"},
		{"route default", `"route":{"default_domain_resolver":RESOLVER}`, "route.default_domain_resolver"},
		{"route direct action", `"route":{"rules":[{"action":"direct","domain_resolver":RESOLVER}]}`, "route.rules[0].domain_resolver"},
		{"shadowtls handshake", `"inbounds":[{"type":"shadowtls","tag":"in","handshake":{"server":"host.example.com","domain_resolver":RESOLVER}}]`, "inbounds[0].handshake.domain_resolver"},
		{"shadowtls named handshake", `"inbounds":[{"type":"shadowtls","tag":"in","handshake_for_server_name":{"host.example.com":{"server":"host.example.com","domain_resolver":RESOLVER}}}]`, `inbounds[0].handshake_for_server_name["host.example.com"].domain_resolver`},
		{"reality handshake", `"inbounds":[{"type":"vless","tag":"in","tls":{"reality":{"handshake":{"server":"host.example.com","domain_resolver":RESOLVER}}}}]`, "inbounds[0].tls.reality.handshake.domain_resolver"},
		{"realm STUN", `"inbounds":[{"type":"hysteria2","tag":"in","realm":{"stun_domain_resolver":RESOLVER}}]`, "inbounds[0].realm.stun_domain_resolver"},
		{"realm HTTP client", `"outbounds":[{"type":"hysteria2","tag":"out","realm":{"http_client":{"domain_resolver":RESOLVER}}}]`, "outbounds[0].realm.http_client.domain_resolver"},
		{"service mesh list", `"services":[{"type":"derp","mesh_with":[{"server":"derp.example.com","domain_resolver":RESOLVER}]}]`, "services[0].mesh_with[0].domain_resolver"},
		{"service mesh object", `"services":[{"type":"derp","mesh_with":{"server":"derp.example.com","domain_resolver":RESOLVER}}]`, "services[0].mesh_with.domain_resolver"},
		{"service verify URL", `"services":[{"type":"derp","verify_client_url":[{"url":"https://verify.example.com","domain_resolver":RESOLVER}]}]`, "services[0].verify_client_url[0].domain_resolver"},
		{"rule set HTTP client", `"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs","http_client":{"domain_resolver":RESOLVER}}]}`, "route.rule_set[0].http_client.domain_resolver"},
		{"certificate HTTP client", `"certificate_providers":[{"type":"acme","http_client":{"domain_resolver":RESOLVER}}]`, "certificate_providers[0].http_client.domain_resolver"},
		{"inline certificate HTTP client", `"inbounds":[{"type":"mixed","tag":"in","tls":{"certificate_provider":{"type":"acme","http_client":{"domain_resolver":RESOLVER}}}}]`, "inbounds[0].tls.certificate_provider.http_client.domain_resolver"},
	}
	for _, test := range tests {
		for _, object := range []bool{false, true} {
			for _, tag := range []string{"bootstrap", "missing"} {
				t.Run(fmt.Sprintf("%s/object=%t/%s", test.name, object, tag), func(t *testing.T) {
					resolver := fmt.Sprintf("%q", tag)
					path := test.path
					if object {
						resolver = fmt.Sprintf(`{"server":%q,"strategy":"ipv4_only"}`, tag)
						path += ".server"
					}
					config := []byte(`{"dns":{"servers":[{"type":"local","tag":"bootstrap"}]},` + strings.ReplaceAll(test.fragment, "RESOLVER", resolver) + `}`)
					err := validateRuntimeConfig(config)
					if tag == "bootstrap" {
						if err != nil {
							t.Fatalf("valid resolver rejected: %v", err)
						}
					} else if want := path + " 引用了不存在的 DNS 服务器: missing"; err == nil || err.Error() != want {
						t.Fatalf("want %q, got %v", want, err)
					}
				})
			}
		}
	}
}

func TestValidateRuntimeConfigDNSServerResolvers(t *testing.T) {
	previousCore := corePtr
	corePtr = &core.Core{}
	t.Cleanup(func() { corePtr = previousCore })
	for _, serverType := range []string{"local", "udp", "tcp", "tls", "https", "quic", "h3", "dhcp"} {
		for _, resolver := range []string{`"bootstrap"`, `{"server":"bootstrap","strategy":"ipv4_only"}`, `"missing"`, `{"server":"missing"}`} {
			t.Run(serverType+"/"+resolver, func(t *testing.T) {
				address := ""
				switch serverType {
				case "udp", "tcp", "tls", "https", "quic", "h3":
					address = `,"server":"resolver.example.com"`
				}
				config := []byte(fmt.Sprintf(`{"dns":{"servers":[{"type":"local","tag":"bootstrap"},{"type":%q,"tag":"dns","domain_resolver":%s%s}],"final":"dns"}}`, serverType, resolver, address))
				err := validateRuntimeConfig(config)
				if !strings.Contains(resolver, "missing") {
					if err != nil {
						t.Fatalf("valid DNS server resolver rejected: %v", err)
					}
					return
				}
				path := "dns.servers[1].domain_resolver"
				if strings.HasPrefix(resolver, "{") {
					path += ".server"
				}
				if want := path + " 引用了不存在的 DNS 服务器: missing"; err == nil || err.Error() != want {
					t.Fatalf("want %q, got %v", want, err)
				}
			})
		}
	}
}

func TestValidateConfigReferencesDNSCycles(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{"self cycle", `{"dns":{"servers":[{"type":"udp","tag":"a","server":"192.0.2.53","domain_resolver":"a"}]}}`, "dns.servers[0].domain_resolver DNS 服务器依赖形成循环: a -> a"},
		{"object cycle", `{"dns":{"servers":[{"type":"https","tag":"a","domain_resolver":{"server":"b"}},{"type":"tcp","tag":"b","domain_resolver":{"server":"a"}}]}}`, "dns.servers[1].domain_resolver.server DNS 服务器依赖形成循环: a -> b -> a"},
		{"three servers", `{"dns":{"servers":[{"type":"local","tag":"c","domain_resolver":"a"},{"type":"udp","tag":"b","domain_resolver":"c"},{"type":"tls","tag":"a","domain_resolver":"b"}]}}`, "dns.servers[0].domain_resolver DNS 服务器依赖形成循环: a -> b -> c -> a"},
		{"untagged cycle", `{"dns":{"servers":[{"type":"udp","domain_resolver":"1"},{"type":"tcp","domain_resolver":"0"}]}}`, "dns.servers[1].domain_resolver DNS 服务器依赖形成循环: 0 -> 1 -> 0"},
		{"detour does not remove core dependency", `{"dns":{"servers":[{"type":"udp","tag":"a","detour":"direct","domain_resolver":"a"}]}}`, "dns.servers[0].domain_resolver DNS 服务器依赖形成循环: a -> a"},
		{"acyclic forward references", `{"dns":{"servers":[{"type":"https","tag":"a","domain_resolver":{"server":"b"}},{"type":"udp","tag":"b","domain_resolver":"c"},{"type":"local","tag":"c"}],"final":"a"}}`, ""},
		{"do not infer default resolver dependency", `{"dns":{"servers":[{"type":"https","tag":"a","server":"resolver.example.com"}],"final":"a"},"route":{"default_domain_resolver":"a"}}`, ""},
		{"numeric explicit replaces cyclic implicit", `{"dns":{"servers":[{"type":"local","domain_resolver":"0"},{"type":"local","tag":"0"}],"final":"0"}}`, ""},
		{"numeric implicit replaces cyclic explicit", `{"dns":{"servers":[{"type":"local","tag":"1","domain_resolver":"1"},{"type":"local"}],"final":"1"}}`, ""},
		{"replacement retains last dependency", `{"dns":{"servers":[{"type":"local"},{"type":"local","tag":"0","domain_resolver":{"server":"0"}}]}}`, "dns.servers[1].domain_resolver.server DNS 服务器依赖形成循环: 0 -> 0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateConfigReferences([]byte(test.config))
			if test.want == "" {
				if err != nil {
					t.Fatalf("valid dependencies rejected: %v", err)
				}
			} else if err == nil || err.Error() != test.want {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestValidateRuntimeConfigDNSFallbackLifecycle(t *testing.T) {
	previousCore := corePtr
	corePtr = &core.Core{}
	t.Cleanup(func() { corePtr = previousCore })
	tests := []struct {
		name       string
		config     string
		wantPath   string
		newError   string
		startError string
		fallback   bool
	}{
		{name: "implicit direct with default DNS", config: `{}`, fallback: true},
		{name: "DNS route uses fallback", config: `{"dns":{"rules":[{"domain":"example.invalid","server":"local"}]}}`, fallback: true},
		{name: "logical DNS evaluate uses fallback", config: `{"dns":{"rules":[{"type":"logical","mode":"and","rules":[{"domain":"example.invalid"}],"action":"evaluate","server":"local","tag":"result"}]}}`, fallback: true},
		{name: "route resolve uses fallback", config: `{"route":{"rules":[{"domain":"example.invalid","action":"resolve","server":"local"}]}}`, fallback: true},
		{name: "explicit final prevents fallback", config: `{"dns":{"final":"local"}}`, wantPath: "dns.final", startError: "default DNS server not found: local"},
		{name: "direct resolver is eager", config: `{"outbounds":[{"type":"direct","tag":"direct","domain_resolver":"local"}]}`, wantPath: "outbounds[0].domain_resolver", newError: "domain resolver not found: local"},
		{name: "domain proxy resolver is eager", config: `{"outbounds":[{"type":"socks","tag":"proxy","server":"proxy.invalid","domain_resolver":{"server":"local"}}]}`, wantPath: "outbounds[0].domain_resolver.server", newError: "domain resolver not found: local"},
		{name: "default resolver uses implicit local fallback after initialization", config: `{"route":{"default_domain_resolver":"local"}}`, fallback: true},
		{name: "default resolver cannot use fallback when explicit servers exist", config: `{"dns":{"servers":[{"type":"local","tag":"bootstrap"}]},"route":{"default_domain_resolver":"local"}}`, wantPath: "route.default_domain_resolver", startError: "default domain resolver not found: local"},
		{name: "existing servers prevent fallback", config: `{"dns":{"servers":[{"type":"local","tag":"bootstrap"}]},"route":{"rules":[{"domain":"example.invalid","action":"resolve","server":"local"}]}}`, wantPath: "route.rules[0].server"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Same normalization and validator as getConfig -> save/preflight.
			// No database, listeners, certificates, NTP or outbound queries.
			config, err := NormalizeSingBoxConfig([]byte(test.config))
			if err != nil {
				t.Fatalf("normalize fixture: %v", err)
			}
			err = validateRuntimeConfig(config)
			if test.wantPath == "" {
				if err != nil {
					t.Fatalf("valid fallback references rejected: %v", err)
				}
			} else if want := test.wantPath + " 引用了不存在的 DNS 服务器: local"; err == nil || err.Error() != want {
				t.Fatalf("want %q, got %v", want, err)
			}

			ctx := singservice.ContextWithDefaultRegistry(context.Background())
			ctx = box.Context(ctx, core.InboundRegistry(), core.OutboundRegistry(), core.EndpointRegistry(), core.DNSTransportRegistry(), core.ServiceRegistry(), core.CertificateProviderRegistry())
			var options option.Options
			if err := options.UnmarshalJSONContext(ctx, config); err != nil {
				t.Fatalf("decode lifecycle fixture: %v", err)
			}
			options.Log = &option.LogOptions{Disabled: true}
			instance, err := box.New(box.Options{Options: options, Context: ctx})
			if test.newError != "" {
				if err == nil {
					instance.Close()
					t.Fatalf("expected constructor error %q", test.newError)
				}
				if !strings.Contains(err.Error(), test.newError) {
					t.Fatalf("want constructor error %q, got %v", test.newError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("construct core: %v", err)
			}
			defer func() {
				if err := instance.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
					t.Errorf("close core: %v", err)
				}
			}()
			manager := singservice.FromContext[adapter.DNSTransportManager](ctx)
			if _, exists := manager.Transport("local"); exists {
				t.Fatal("fallback unexpectedly available before Start")
			}
			err = instance.Start()
			if test.startError != "" {
				if err == nil || !strings.Contains(err.Error(), test.startError) {
					t.Fatalf("want startup error %q, got %v", test.startError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("start core: %v", err)
			}
			if _, exists := manager.Transport("local"); exists != test.fallback {
				t.Fatalf("local transport exists=%t, want %t", exists, test.fallback)
			}
		})
	}
}
