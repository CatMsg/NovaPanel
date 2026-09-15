package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	R "github.com/sagernet/sing-box/route/rule"
	M "github.com/sagernet/sing/common/metadata"
)

type RuleSetRuntimeHealth struct {
	Tag                 string `json:"tag"`
	Loaded              bool   `json:"loaded"`
	ContainsProcessRule bool   `json:"containsProcessRule"`
	ContainsWIFIRule    bool   `json:"containsWifiRule"`
	ContainsIPCIDRRule  bool   `json:"containsIpCidrRule"`
}

type RouteExplainInput struct {
	Domain      string `json:"domain"`
	Destination string `json:"destination"`
	Port        uint16 `json:"port"`
	Inbound     string `json:"inbound"`
	User        string `json:"user"`
	Network     string `json:"network"`
	Protocol    string `json:"protocol"`
	QueryType   string `json:"queryType"`
}

type RouteExplainResult struct {
	Matched     bool              `json:"matched"`
	RuleIndex   int               `json:"ruleIndex"`
	Rule        string            `json:"rule"`
	ActionType  string            `json:"actionType"`
	Action      string            `json:"action"`
	DefaultUsed bool              `json:"defaultUsed"`
	Note        string            `json:"note,omitempty"`
	DNS         *DNSExplainResult `json:"dns,omitempty"`
}

type DNSExplainResult struct {
	Matched       bool   `json:"matched"`
	RuleIndex     int    `json:"ruleIndex"`
	Rule          string `json:"rule,omitempty"`
	ActionType    string `json:"actionType"`
	Action        string `json:"action"`
	Server        string `json:"server,omitempty"`
	ServerType    string `json:"serverType,omitempty"`
	ServerAddress string `json:"serverAddress,omitempty"`
	ServerPort    uint16 `json:"serverPort,omitempty"`
	DefaultUsed   bool   `json:"defaultUsed"`
	QueryType     string `json:"queryType"`
	Note          string `json:"note,omitempty"`
}

type selectorController interface {
	Now() string
	All() []string
	SelectOutbound(tag string) bool
}

func RuleSetHealth(tags []string) []RuleSetRuntimeHealth {
	result := make([]RuleSetRuntimeHealth, 0, len(tags))
	for _, tag := range tags {
		item := RuleSetRuntimeHealth{Tag: tag}
		if router != nil {
			if ruleSet, loaded := router.RuleSet(tag); loaded {
				metadata := ruleSet.Metadata()
				item.Loaded = true
				item.ContainsProcessRule = metadata.ContainsProcessRule
				item.ContainsWIFIRule = metadata.ContainsWIFIRule
				item.ContainsIPCIDRRule = metadata.ContainsIPCIDRRule
			}
		}
		result = append(result, item)
	}
	return result
}

func ExplainRoute(input RouteExplainInput) (RouteExplainResult, error) {
	if router == nil {
		return RouteExplainResult{}, errors.New("core not running")
	}

	network := strings.ToLower(strings.TrimSpace(input.Network))
	if network == "" {
		network = "tcp"
	}
	if network != "tcp" && network != "udp" {
		return RouteExplainResult{}, errors.New("network must be tcp or udp")
	}

	domain := strings.TrimSpace(input.Domain)
	destination := strings.TrimSpace(input.Destination)
	if destination == "" {
		destination = domain
	}
	if destination == "" {
		return RouteExplainResult{}, errors.New("domain or destination is required")
	}

	metadata := adapter.InboundContext{
		Inbound:     strings.TrimSpace(input.Inbound),
		User:        strings.TrimSpace(input.User),
		Network:     network,
		Protocol:    strings.TrimSpace(input.Protocol),
		Domain:      domain,
		Destination: M.ParseSocksaddrHostPort(destination, input.Port),
	}
	if metadata.Destination.IsIPv4() {
		metadata.IPVersion = 4
	} else if metadata.Destination.IsIPv6() {
		metadata.IPVersion = 6
	}

	dnsResult, err := explainDNS(input)
	if err != nil {
		return RouteExplainResult{}, err
	}

	for index, rule := range router.Rules() {
		metadata.ResetRuleCache()
		if !rule.Match(&metadata) {
			continue
		}
		action := rule.Action()
		if action.Type() == C.RuleActionTypeSniff || action.Type() == C.RuleActionTypeResolve {
			continue
		}
		return RouteExplainResult{
			Matched:    true,
			RuleIndex:  index,
			Rule:       rule.String(),
			ActionType: action.Type(),
			Action:     action.String(),
			DNS:        dnsResult,
		}, nil
	}

	return RouteExplainResult{
		RuleIndex:   -1,
		DefaultUsed: true,
		Note:        "no final rule matched; route.final/default outbound will be used",
		DNS:         dnsResult,
	}, nil
}

func explainDNS(input RouteExplainInput) (*DNSExplainResult, error) {
	domain := strings.TrimSpace(input.Domain)
	if domain == "" {
		return nil, nil
	}
	if dns_transport_manager == nil {
		return nil, errors.New("DNS router not running")
	}

	queryTypeName := strings.ToUpper(strings.TrimSpace(input.QueryType))
	if queryTypeName == "" {
		queryTypeName = "A"
	}
	queryType, loaded := dns.StringToType[queryTypeName]
	if !loaded {
		return nil, errors.New("unsupported DNS query type")
	}

	metadata := adapter.InboundContext{
		Inbound:   strings.TrimSpace(input.Inbound),
		User:      strings.TrimSpace(input.User),
		Network:   strings.ToLower(strings.TrimSpace(input.Network)),
		Protocol:  strings.TrimSpace(input.Protocol),
		Domain:    domain,
		QueryType: queryType,
	}
	switch queryType {
	case dns.TypeA:
		metadata.IPVersion = 4
	case dns.TypeAAAA:
		metadata.IPVersion = 6
	}

	if dns_options == nil {
		return &DNSExplainResult{
			RuleIndex:   -1,
			DefaultUsed: true,
			QueryType:   queryTypeName,
			Note:        "DNS is not configured",
		}, nil
	}

	rules := make([]adapter.DNSRule, 0, len(dns_options.Rules))
	for index, ruleOptions := range dns_options.Rules {
		rule, err := R.NewDNSRule(globalCtx, factory.NewLogger("dns-diagnostics"), ruleOptions, true, false)
		if err != nil {
			closeDNSDiagnosticRules(rules)
			return nil, fmt.Errorf("parse DNS rule[%d]: %w", index, err)
		}
		if err := rule.Start(); err != nil {
			_ = rule.Close()
			closeDNSDiagnosticRules(rules)
			return nil, fmt.Errorf("initialize DNS rule[%d]: %w", index, err)
		}
		rules = append(rules, rule)
	}
	defer closeDNSDiagnosticRules(rules)

	for index, rule := range rules {
		metadata.ResetRuleCache()
		if !rule.Match(&metadata) {
			continue
		}
		action := rule.Action()
		if action == nil || action.Type() == C.RuleActionTypeRouteOptions {
			continue
		}

		result := &DNSExplainResult{
			Matched:    true,
			RuleIndex:  index,
			Rule:       rule.String(),
			ActionType: action.Type(),
			Action:     action.String(),
			QueryType:  queryTypeName,
		}
		if routeAction, ok := action.(*R.RuleActionDNSRoute); ok {
			server, available := describeDNSServer(routeAction.Server)
			if !available {
				continue
			}
			applyDNSServer(result, server)
		}
		return result, nil
	}

	defaultTransport := dns_transport_manager.Default()
	result := &DNSExplainResult{
		RuleIndex:   -1,
		DefaultUsed: true,
		QueryType:   queryTypeName,
		Note:        "no DNS rule matched; dns.final/default server will be used",
	}
	if defaultTransport != nil {
		server, _ := describeDNSServer(defaultTransport.Tag())
		applyDNSServer(result, server)
	}
	return result, nil
}

type dnsServerDescription struct {
	tag      string
	typeName string
	address  string
	port     uint16
}

func describeDNSServer(tag string) (dnsServerDescription, bool) {
	transport, loaded := dns_transport_manager.Transport(tag)
	if !loaded {
		return dnsServerDescription{}, false
	}
	description := dnsServerDescription{tag: transport.Tag(), typeName: transport.Type()}
	for _, server := range dns_options.Servers {
		if server.Tag != tag {
			continue
		}
		description.address, description.port = configuredDNSServerEndpoint(server)
		break
	}
	return description, true
}

func configuredDNSServerEndpoint(server option.DNSServerOptions) (string, uint16) {
	switch options := server.Options.(type) {
	case *option.RemoteDNSServerOptions:
		return options.Server, options.ServerPort
	case *option.RemoteTLSDNSServerOptions:
		return options.Server, options.ServerPort
	case *option.RemoteHTTPSDNSServerOptions:
		return options.Server, options.ServerPort
	default:
		return "", 0
	}
}

func applyDNSServer(result *DNSExplainResult, server dnsServerDescription) {
	result.Server = server.tag
	result.ServerType = server.typeName
	result.ServerAddress = server.address
	result.ServerPort = server.port
}

func closeDNSDiagnosticRules(rules []adapter.DNSRule) {
	for _, rule := range rules {
		_ = rule.Close()
	}
}

func SelectorState(tag string) (current string, members []string, err error) {
	if outbound_manager == nil {
		return "", nil, errors.New("core not running")
	}
	outbound, loaded := outbound_manager.Outbound(tag)
	if !loaded {
		return "", nil, errors.New("selector not found")
	}
	selector, ok := outbound.(selectorController)
	if !ok {
		return "", nil, errors.New("outbound is not a selector")
	}
	return selector.Now(), selector.All(), nil
}

func SelectOutbound(selectorTag, memberTag string) error {
	if outbound_manager == nil {
		return errors.New("core not running")
	}
	outbound, loaded := outbound_manager.Outbound(selectorTag)
	if !loaded {
		return errors.New("selector not found")
	}
	selector, ok := outbound.(selectorController)
	if !ok {
		return errors.New("outbound is not a selector")
	}
	if !selector.SelectOutbound(memberTag) {
		return errors.New("selector member not found")
	}
	return nil
}
