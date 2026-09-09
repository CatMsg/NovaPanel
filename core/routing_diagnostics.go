package core

import (
	"errors"
	"strings"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
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
}

type RouteExplainResult struct {
	Matched     bool   `json:"matched"`
	RuleIndex   int    `json:"ruleIndex"`
	Rule        string `json:"rule"`
	ActionType  string `json:"actionType"`
	Action      string `json:"action"`
	DefaultUsed bool   `json:"defaultUsed"`
	Note        string `json:"note,omitempty"`
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
		}, nil
	}

	return RouteExplainResult{
		RuleIndex:   -1,
		DefaultUsed: true,
		Note:        "no final rule matched; route.final/default outbound will be used",
	}, nil
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
