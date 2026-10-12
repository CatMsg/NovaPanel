package core

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	R "github.com/sagernet/sing-box/route/rule"
	"github.com/sagernet/sing/service"

	mDNS "github.com/miekg/dns"
)

func TestDNSConfigurationRuleContextIntegration(t *testing.T) {
	v4 := &controlledDNSConfigurationTransport{
		tag:       "fake-v4",
		addresses: []netip.Addr{netip.MustParseAddr("192.0.2.53")},
		search:    []string{"EXAMPLE.COM."},
	}
	v6 := &controlledDNSConfigurationTransport{
		tag:       "fake-v6",
		addresses: []netip.Addr{netip.MustParseAddr("2001:db8:1234::53")},
		search:    []string{"example.com"},
	}
	manager := &controlledDNSConfigurationManager{
		transports: map[string]adapter.DNSTransport{
			v4.tag: v4,
			v6.tag: v6,
		},
	}
	ctx := service.ContextWith[adapter.DNSTransportManager](context.Background(), manager)

	var options option.Rule
	if err := options.UnmarshalJSONContext(ctx, []byte(`{
		"type": "logical",
		"mode": "and",
		"rules": [
			{
				"type": "logical",
				"mode": "or",
				"rules": [
					{"dns_server_address": {"fake-v4": ["192.0.2.0/24"]}},
					{"dns_server_address": {"fake-v6": ["2001:db8:1234::/48"]}}
				]
			},
			{"dns_search_domain": {
				"fake-v4": ["example.com"],
				"fake-v6": ["EXAMPLE.COM."]
			}}
		]
	}`)); err != nil {
		t.Fatalf("decode nested DNS configuration rule: %v", err)
	}
	rule, err := R.NewRule(ctx, log.NewNOPFactory().NewLogger("dns-context-integration"), options, false)
	if err != nil {
		t.Fatalf("construct nested DNS configuration rule: %v", err)
	}
	if err := rule.Start(); err != nil {
		t.Fatalf("start nested DNS configuration rule: %v", err)
	}
	defer func() {
		if err := rule.Close(); err != nil {
			t.Errorf("close nested DNS configuration rule: %v", err)
		}
	}()

	metadata := &adapter.InboundContext{}
	if !rule.Match(metadata) {
		t.Fatal("nested rule did not match canonical IPv4/IPv6 DNS configuration")
	}

	t.Run("ipv4_cidr_branch", func(t *testing.T) {
		v6.addresses = []netip.Addr{netip.MustParseAddr("2001:db8:ffff::53")}
		if !rule.Match(metadata) {
			t.Fatal("nested OR rule did not match the IPv4 CIDR branch")
		}
	})
	t.Run("ipv6_cidr_branch", func(t *testing.T) {
		v4.addresses = []netip.Addr{netip.MustParseAddr("198.51.100.53")}
		v6.addresses = []netip.Addr{netip.MustParseAddr("2001:db8:1234::53")}
		if !rule.Match(metadata) {
			t.Fatal("nested OR rule did not match the IPv6 CIDR branch")
		}
	})
	t.Run("address_cidr_miss", func(t *testing.T) {
		v4.addresses = []netip.Addr{netip.MustParseAddr("198.51.100.53")}
		v6.addresses = []netip.Addr{netip.MustParseAddr("2001:db8:ffff::53")}
		if rule.Match(metadata) {
			t.Fatal("nested OR rule matched when neither DNS address was in its CIDR")
		}
	})
	t.Run("canonical_exact_search_domain", func(t *testing.T) {
		v4.addresses = []netip.Addr{netip.MustParseAddr("192.0.2.53")}
		v6.addresses = []netip.Addr{netip.MustParseAddr("2001:db8:1234::53")}
		v4.search = []string{"child.example.com."}
		v6.search = []string{"example.com."}
		if rule.Match(metadata) {
			t.Fatal("search-domain rule matched a nested rather than exact domain")
		}
		v4.search = []string{"ExAmPlE.CoM"}
		if !rule.Match(metadata) {
			t.Fatal("search-domain rule did not match case/trailing-dot canonical form")
		}
	})
}

type controlledDNSConfigurationTransport struct {
	tag       string
	addresses []netip.Addr
	search    []string
}

func (*controlledDNSConfigurationTransport) Start(adapter.StartStage, *adapter.Scope) error {
	return nil
}

func (*controlledDNSConfigurationTransport) Type() string           { return "controlled-test" }
func (t *controlledDNSConfigurationTransport) Tag() string          { return t.tag }
func (*controlledDNSConfigurationTransport) Dependencies() []string { return nil }
func (*controlledDNSConfigurationTransport) Reset()                 {}
func (t *controlledDNSConfigurationTransport) ServerAddresses() []netip.Addr {
	return t.addresses
}
func (t *controlledDNSConfigurationTransport) SearchDomains() []string {
	return t.search
}
func (*controlledDNSConfigurationTransport) Exchange(context.Context, *mDNS.Msg) (*mDNS.Msg, error) {
	return nil, errors.New("controlled DNS configuration transport must not perform queries")
}
func (*controlledDNSConfigurationTransport) ExchangeAsync(_ context.Context, _ *mDNS.Msg, callback func(*mDNS.Msg, error)) {
	callback(nil, errors.New("controlled DNS configuration transport must not perform queries"))
}

type controlledDNSConfigurationManager struct {
	transports map[string]adapter.DNSTransport
}

func (*controlledDNSConfigurationManager) Start(adapter.StartStage, *adapter.Scope) error { return nil }
func (m *controlledDNSConfigurationManager) Transports() []adapter.DNSTransport {
	transports := make([]adapter.DNSTransport, 0, len(m.transports))
	for _, transport := range m.transports {
		transports = append(transports, transport)
	}
	return transports
}
func (m *controlledDNSConfigurationManager) Transport(tag string) (adapter.DNSTransport, bool) {
	transport, loaded := m.transports[tag]
	return transport, loaded
}
func (*controlledDNSConfigurationManager) Default() adapter.DNSTransport   { return nil }
func (*controlledDNSConfigurationManager) FakeIP() adapter.FakeIPTransport { return nil }
func (*controlledDNSConfigurationManager) Create(context.Context, log.ContextLogger, string, string, any) error {
	return errors.New("controlled DNS configuration manager does not create transports")
}
