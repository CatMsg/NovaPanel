package service

import (
	"encoding/json"
	"errors"

	"github.com/CatMsg/NovaPanel/core"
)

type RuleSetHealth struct {
	Tag                 string `json:"tag"`
	Type                string `json:"type"`
	Format              string `json:"format"`
	URL                 string `json:"url,omitempty"`
	Loaded              bool   `json:"loaded"`
	ContainsProcessRule bool   `json:"containsProcessRule"`
	ContainsWIFIRule    bool   `json:"containsWifiRule"`
	ContainsIPCIDRRule  bool   `json:"containsIpCidrRule"`
}

func (s *ConfigService) GetRuleSetHealth() ([]RuleSetHealth, error) {
	startCoreMu.Lock()
	defer startCoreMu.Unlock()
	rawConfig, err := s.GetConfig("")
	if err != nil {
		return nil, err
	}
	var config struct {
		Route struct {
			RuleSets []struct {
				Tag    string `json:"tag"`
				Type   string `json:"type"`
				Format string `json:"format"`
				URL    string `json:"url"`
			} `json:"rule_set"`
		} `json:"route"`
	}
	if err := json.Unmarshal(*rawConfig, &config); err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(config.Route.RuleSets))
	for _, item := range config.Route.RuleSets {
		tags = append(tags, item.Tag)
	}
	runtimeHealth := make([]core.RuleSetRuntimeHealth, 0)
	if corePtr != nil && corePtr.IsRunning() {
		runtimeHealth = core.RuleSetHealth(tags)
	}
	healthByTag := make(map[string]core.RuleSetRuntimeHealth, len(runtimeHealth))
	for _, item := range runtimeHealth {
		healthByTag[item.Tag] = item
	}
	result := make([]RuleSetHealth, 0, len(config.Route.RuleSets))
	for _, item := range config.Route.RuleSets {
		runtime := healthByTag[item.Tag]
		result = append(result, RuleSetHealth{
			Tag:                 item.Tag,
			Type:                item.Type,
			Format:              item.Format,
			URL:                 item.URL,
			Loaded:              runtime.Loaded,
			ContainsProcessRule: runtime.ContainsProcessRule,
			ContainsWIFIRule:    runtime.ContainsWIFIRule,
			ContainsIPCIDRRule:  runtime.ContainsIPCIDRRule,
		})
	}
	return result, nil
}

func (s *ConfigService) ExplainRoute(input core.RouteExplainInput) (core.RouteExplainResult, error) {
	startCoreMu.Lock()
	defer startCoreMu.Unlock()
	if corePtr == nil || !corePtr.IsRunning() {
		return core.RouteExplainResult{}, errors.New("core not running")
	}
	return core.ExplainRoute(input)
}

func selectorState(tag string) (string, []string, error) {
	startCoreMu.Lock()
	defer startCoreMu.Unlock()
	if corePtr == nil || !corePtr.IsRunning() {
		return "", nil, errors.New("core not running")
	}
	return core.SelectorState(tag)
}

func selectOutbound(selectorTag, memberTag string) error {
	startCoreMu.Lock()
	defer startCoreMu.Unlock()
	if corePtr == nil || !corePtr.IsRunning() {
		return errors.New("core not running")
	}
	return core.SelectOutbound(selectorTag, memberTag)
}
