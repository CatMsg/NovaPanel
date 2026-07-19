package service

import "testing"

func TestBuildPortDriftReportHealthy(t *testing.T) {
	desired := []portDriftRule{{
		chain:    managedPortChain("inbound-a"),
		protocol: "tcp",
		dport:    "443",
		toPorts:  "443",
		ownerTag: "inbound-a",
	}}
	actual := []PortNatEntry{{
		Family:   "ipv4",
		Chain:    desired[0].chain,
		Protocol: "tcp",
		DPort:    "443",
		Target:   "REDIRECT",
		ToPorts:  "443",
	}}
	report := buildPortDriftReport(desired, actual, nil)
	if report.Status != "healthy" || report.IssueCount != 0 {
		t.Fatalf("unexpected healthy report: %+v", report)
	}
}

func TestBuildPortDriftReportDetectsMissingDuplicateAndOrphan(t *testing.T) {
	desired := []portDriftRule{
		{chain: managedPortChain("inbound-a"), protocol: "tcp", dport: "443", toPorts: "443", ownerTag: "inbound-a"},
		{chain: managedPortChain("inbound-b"), protocol: "udp", dport: "8443", toPorts: "8443", ownerTag: "inbound-b"},
	}
	actual := []PortNatEntry{
		{Family: "ipv4", Chain: desired[0].chain, Protocol: "tcp", DPort: "443", Target: "REDIRECT", ToPorts: "443"},
		{Family: "ipv4", Chain: desired[0].chain, Protocol: "tcp", DPort: "443", Target: "REDIRECT", ToPorts: "443"},
		{Family: "ipv4", Chain: managedPortChain("old-inbound"), Protocol: "udp", DPort: "9000", Target: "REDIRECT", ToPorts: "9000"},
	}
	report := buildPortDriftReport(desired, actual, nil)
	if report.Status != "drift" {
		t.Fatalf("expected drift status, got %+v", report)
	}
	if report.MissingCount != 1 || report.DuplicateCount != 1 || report.OrphanCount != 1 {
		t.Fatalf("unexpected drift counts: %+v", report)
	}
}

func TestParseNftRedirectRule(t *testing.T) {
	protocol, dport, toPorts := parseNftRedirectRule("udp dport 555 counter packets 0 bytes 0 redirect to :555")
	if protocol != "udp" || dport != "555" || toPorts != "555" {
		t.Fatalf("unexpected nft rule: %q %q %q", protocol, dport, toPorts)
	}
}
