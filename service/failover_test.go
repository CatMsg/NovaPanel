package service

import (
	"reflect"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
)

type recordingOutboundChecker struct {
	results map[string]core.CheckOutboundResult
	calls   []string
}

func (c *recordingOutboundChecker) CheckOutbound(tag string, _ string) core.CheckOutboundResult {
	c.calls = append(c.calls, tag)
	return c.results[tag]
}

func TestNormalizeFailoverPolicy(t *testing.T) {
	policy := normalizeFailoverPolicy(FailoverPolicy{
		Tag:     "  streaming  ",
		Members: []string{" primary ", "backup", "primary", "", "backup"},
	})

	if policy.Tag != "streaming" {
		t.Fatalf("unexpected tag: %q", policy.Tag)
	}
	if !reflect.DeepEqual(policy.Members, []string{"primary", "backup"}) {
		t.Fatalf("unexpected members: %#v", policy.Members)
	}
	if policy.TestURL != defaultFailoverTestURL {
		t.Fatalf("unexpected test URL: %q", policy.TestURL)
	}
	if policy.IntervalSeconds != 30 || policy.FailureThreshold != 2 || policy.RecoveryThreshold != 2 {
		t.Fatalf("unexpected defaults: %#v", policy)
	}
}

func TestValidateFailoverPolicy(t *testing.T) {
	valid := FailoverPolicy{
		Tag:               "streaming",
		Members:           []string{"primary", "backup"},
		TestURL:           defaultFailoverTestURL,
		IntervalSeconds:   30,
		FailureThreshold:  2,
		RecoveryThreshold: 2,
	}
	if err := validateFailoverPolicy(valid); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*FailoverPolicy)
	}{
		{name: "missing tag", mutate: func(policy *FailoverPolicy) { policy.Tag = "" }},
		{name: "one member", mutate: func(policy *FailoverPolicy) { policy.Members = []string{"primary"} }},
		{name: "invalid test URL", mutate: func(policy *FailoverPolicy) { policy.TestURL = "file:///tmp/check" }},
		{name: "short interval", mutate: func(policy *FailoverPolicy) { policy.IntervalSeconds = 9 }},
		{name: "large interval", mutate: func(policy *FailoverPolicy) { policy.IntervalSeconds = 3601 }},
		{name: "zero failure threshold", mutate: func(policy *FailoverPolicy) { policy.FailureThreshold = 0 }},
		{name: "large recovery threshold", mutate: func(policy *FailoverPolicy) { policy.RecoveryThreshold = 11 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := valid
			test.mutate(&policy)
			if err := validateFailoverPolicy(policy); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}

func TestFailoverProbesUseOrderedCheckOutboundResults(t *testing.T) {
	checker := &recordingOutboundChecker{results: map[string]core.CheckOutboundResult{
		"primary": {Error: "timeout"},
		"backup":  {OK: true, Delay: 42},
		"reserve": {OK: true, Delay: 90},
	}}
	service := &FailoverService{config: checker}
	policy := FailoverPolicy{
		Members: []string{"primary", "backup", "reserve"},
		TestURL: defaultFailoverTestURL,
	}

	probes, candidate, statusError := service.probeMembers(policy, policy.Members)
	if statusError != "" {
		t.Fatalf("unexpected status error: %s", statusError)
	}
	if candidate != "backup" {
		t.Fatalf("unexpected candidate: %q", candidate)
	}
	if !reflect.DeepEqual(checker.calls, []string{"primary", "backup"}) {
		t.Fatalf("failover did not retain ordered first-success behavior: %#v", checker.calls)
	}
	if len(probes) != 2 || probes[0].Error != "timeout" || !probes[1].OK || probes[1].Delay != 42 {
		t.Fatalf("CheckOutbound semantics were not preserved: %#v", probes)
	}
}
