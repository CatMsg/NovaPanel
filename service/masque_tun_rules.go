package service

import (
	"fmt"
	"net/netip"
)

type masqueTunRuleSpec struct {
	checkArgs  []string
	addArgs    []string
	deleteArgs []string
}

func buildMasqueTunIptablesRules(iface string, peerPrefix netip.Prefix) []masqueTunRuleSpec {
	return []masqueTunRuleSpec{
		{
			checkArgs:  []string{"iptables", "-C", "FORWARD", "-i", iface, "-j", "ACCEPT"},
			addArgs:    []string{"iptables", "-I", "FORWARD", "1", "-i", iface, "-j", "ACCEPT"},
			deleteArgs: []string{"iptables", "-D", "FORWARD", "-i", iface, "-j", "ACCEPT"},
		},
		{
			checkArgs:  []string{"iptables", "-C", "FORWARD", "-o", iface, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
			addArgs:    []string{"iptables", "-I", "FORWARD", "2", "-o", iface, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
			deleteArgs: []string{"iptables", "-D", "FORWARD", "-o", iface, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
		},
		{
			checkArgs:  []string{"iptables", "-t", "nat", "-C", "POSTROUTING", "-s", peerPrefix.String(), "!", "-o", iface, "-j", "MASQUERADE"},
			addArgs:    []string{"iptables", "-t", "nat", "-A", "POSTROUTING", "-s", peerPrefix.String(), "!", "-o", iface, "-j", "MASQUERADE"},
			deleteArgs: []string{"iptables", "-t", "nat", "-D", "POSTROUTING", "-s", peerPrefix.String(), "!", "-o", iface, "-j", "MASQUERADE"},
		},
	}
}

func masqueTunKernelForwardSettings(iface string) map[string]string {
	return map[string]string{
		"/proc/sys/net/ipv4/ip_forward":                            "1\n",
		"/proc/sys/net/ipv4/conf/all/rp_filter":                    "0\n",
		"/proc/sys/net/ipv4/conf/default/rp_filter":                "0\n",
		fmt.Sprintf("/proc/sys/net/ipv4/conf/%s/rp_filter", iface): "0\n",
	}
}

func applyMasqueTunRules(run func(...string) error, rules []masqueTunRuleSpec) error {
	for _, rule := range rules {
		if err := run(rule.checkArgs...); err == nil {
			continue
		}
		if err := run(rule.addArgs...); err != nil {
			return err
		}
	}
	return nil
}

func cleanupMasqueTunRules(run func(...string) error, rules []masqueTunRuleSpec) {
	for _, rule := range rules {
		for {
			if err := run(rule.deleteArgs...); err != nil {
				break
			}
		}
	}
}
