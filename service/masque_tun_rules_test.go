package service

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestBuildMasqueTunIptablesRules(t *testing.T) {
	peerPrefix := netip.MustParsePrefix("172.16.0.7/32")
	rules := buildMasqueTunIptablesRules("npmq123456", peerPrefix)
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}

	if got := strings.Join(rules[0].addArgs, " "); got != "iptables -I FORWARD 1 -i npmq123456 -j ACCEPT" {
		t.Fatalf("unexpected first add rule: %s", got)
	}
	if got := strings.Join(rules[1].checkArgs, " "); got != "iptables -C FORWARD -o npmq123456 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT" {
		t.Fatalf("unexpected second check rule: %s", got)
	}
	if got := strings.Join(rules[2].deleteArgs, " "); got != "iptables -t nat -D POSTROUTING -s 172.16.0.7/32 ! -o npmq123456 -j MASQUERADE" {
		t.Fatalf("unexpected nat delete rule: %s", got)
	}
}

func TestMasqueTunKernelForwardSettings(t *testing.T) {
	got := masqueTunKernelForwardSettings("npmq123456")
	want := map[string]string{
		"/proc/sys/net/ipv4/ip_forward":                "1\n",
		"/proc/sys/net/ipv4/conf/all/rp_filter":        "0\n",
		"/proc/sys/net/ipv4/conf/default/rp_filter":    "0\n",
		"/proc/sys/net/ipv4/conf/npmq123456/rp_filter": "0\n",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected kernel settings: %#v", got)
	}
}

func TestApplyMasqueTunRulesAddsOnlyMissingRules(t *testing.T) {
	peerPrefix := netip.MustParsePrefix("172.16.0.7/32")
	rules := buildMasqueTunIptablesRules("npmq123456", peerPrefix)

	var calls []string
	run := func(args ...string) error {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		if strings.Contains(call, "-C FORWARD -i") {
			return nil
		}
		if strings.Contains(call, "-C ") {
			return errors.New("missing")
		}
		return nil
	}

	if err := applyMasqueTunRules(run, rules); err != nil {
		t.Fatalf("apply rules failed: %v", err)
	}

	if containsCall(calls, "iptables -I FORWARD 1 -i npmq123456 -j ACCEPT") {
		t.Fatalf("existing ingress rule should not be added again: %v", calls)
	}
	if !containsCall(calls, "iptables -I FORWARD 2 -o npmq123456 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT") {
		t.Fatalf("missing egress rule add command: %v", calls)
	}
	if !containsCall(calls, "iptables -t nat -A POSTROUTING -s 172.16.0.7/32 ! -o npmq123456 -j MASQUERADE") {
		t.Fatalf("missing nat masquerade add command: %v", calls)
	}
}

func TestCleanupMasqueTunRulesRepeatsDeletesUntilFailure(t *testing.T) {
	peerPrefix := netip.MustParsePrefix("172.16.0.7/32")
	rules := buildMasqueTunIptablesRules("npmq123456", peerPrefix)

	counts := map[string]int{}
	run := func(args ...string) error {
		call := strings.Join(args, " ")
		counts[call]++
		if counts[call] >= 2 {
			return errors.New("not found")
		}
		return nil
	}

	cleanupMasqueTunRules(run, rules)

	for _, rule := range rules {
		call := strings.Join(rule.deleteArgs, " ")
		if counts[call] != 2 {
			t.Fatalf("expected delete rule %q to be attempted twice, got %d", call, counts[call])
		}
	}
}

func containsCall(calls []string, target string) bool {
	for _, call := range calls {
		if call == target {
			return true
		}
	}
	return false
}
