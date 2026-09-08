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
	report := buildPortDriftReport(desired, actual, nil, "iptables")
	if report.Status != "healthy" || report.IssueCount != 0 {
		t.Fatalf("unexpected healthy report: %+v", report)
	}
}

func TestBuildPortDriftReportNormalizesPortRangeSyntax(t *testing.T) {
	desired := []portDriftRule{{
		chain: managedPortChain("hy2-range"), protocol: "udp", dport: "20000:49999", toPorts: "20000", ownerTag: "hy2-range",
	}}
	actual := []PortNatEntry{{
		Family: "ipv4", Chain: desired[0].chain, Protocol: "udp", DPort: "20000-49999", Target: "REDIRECT", ToPorts: "20000",
	}}
	report := buildPortDriftReport(desired, actual, nil, "nftables")
	if report.Status != "healthy" || report.DesiredRules != 1 || report.ActualManagedRules != 1 {
		t.Fatalf("unexpected range drift report: %+v", report)
	}
}

func TestBuildPortDriftReportDetectsMissingDuplicateAndOrphan(t *testing.T) {
	desired := []portDriftRule{
		{chain: managedPortChain("inbound-a"), protocol: "tcp", dport: "443", toPorts: "443", scope: managedPortScopeInbound, ownerTag: "inbound-a"},
		{chain: managedPortChain("inbound-b"), protocol: "udp", dport: "8443", toPorts: "8443", scope: managedPortScopeInbound, ownerTag: "inbound-b"},
	}
	actual := []PortNatEntry{
		{Family: "ipv4", Chain: desired[0].chain, Protocol: "tcp", DPort: "443", Target: "REDIRECT", ToPorts: "443"},
		{Family: "ipv4", Chain: desired[0].chain, Protocol: "tcp", DPort: "443", Target: "REDIRECT", ToPorts: "443"},
		{Family: "ipv4", Chain: managedPortChain("old-inbound"), Protocol: "udp", DPort: "9000", Target: "REDIRECT", ToPorts: "9000"},
	}
	report := buildPortDriftReport(desired, actual, nil, "iptables")
	if report.Status != "drift" {
		t.Fatalf("expected drift status, got %+v", report)
	}
	if report.MissingCount != 1 || report.DuplicateCount != 1 || report.OrphanCount != 1 {
		t.Fatalf("unexpected drift counts: %+v", report)
	}
	for _, issue := range report.Issues {
		if issue.ID == "" || !issue.Repairable {
			t.Fatalf("expected repairable issue with stable id: %+v", issue)
		}
	}
}

func TestBuildPortDriftReportAcceptsUFWPreroutingRules(t *testing.T) {
	desired := []portDriftRule{{
		chain:    managedPortChain("inbound-a"),
		protocol: "udp",
		dport:    "8443",
		toPorts:  "443",
		ownerTag: "inbound-a",
	}}
	actualIPv4 := []PortNatEntry{{
		Family:   "ipv4",
		Chain:    "PREROUTING",
		Protocol: "udp",
		DPort:    "8443",
		Target:   "REDIRECT",
		ToPorts:  "443",
	}}
	actualIPv6 := []PortNatEntry{{
		Family:   "ipv6",
		Chain:    "PREROUTING",
		Protocol: "udp",
		DPort:    "8443",
		Target:   "REDIRECT",
		ToPorts:  "443",
	}}

	report := buildPortDriftReport(desired, actualIPv4, actualIPv6, "UFW")
	if report.Status != "healthy" || report.IssueCount != 0 || report.ActualManagedRules != 2 {
		t.Fatalf("unexpected UFW report: %+v", report)
	}
}

func TestBuildPortDriftReportRejectsWrongUFWRedirectTarget(t *testing.T) {
	desired := []portDriftRule{{
		chain:    managedPortChain("inbound-a"),
		protocol: "udp",
		dport:    "8443",
		toPorts:  "443",
		ownerTag: "inbound-a",
	}}
	actual := []PortNatEntry{{
		Family:   "ipv4",
		Chain:    "PREROUTING",
		Protocol: "udp",
		DPort:    "8443",
		Target:   "REDIRECT",
		ToPorts:  "444",
	}}

	report := buildPortDriftReport(desired, actual, nil, "UFW")
	if report.Status != "drift" || report.MissingCount != 1 || report.UnexpectedCount != 1 {
		t.Fatalf("expected UFW target drift, got %+v", report)
	}
}

func TestParseNftRedirectRule(t *testing.T) {
	protocol, dport, toPorts := parseNftRedirectRule("udp dport 555 counter packets 0 bytes 0 redirect to :555")
	if protocol != "udp" || dport != "555" || toPorts != "555" {
		t.Fatalf("unexpected nft rule: %q %q %q", protocol, dport, toPorts)
	}
}

func TestManagedForwardSpecForDriftIssueGroupsOwnerRules(t *testing.T) {
	desired := []portDriftRule{
		{protocol: "tcp", dport: "443", toPorts: "8443", scope: managedPortScopeInbound, ownerTag: "inbound-a"},
		{protocol: "udp", dport: "444", toPorts: "8443", scope: managedPortScopeInbound, ownerTag: "inbound-a"},
		{protocol: "tcp", dport: "9000", toPorts: "9000", scope: managedPortScopeEndpoint, ownerTag: "endpoint-a"},
	}
	spec, err := managedForwardSpecForDriftIssue(desired, PortDriftIssue{Scope: managedPortScopeInbound, OwnerTag: "inbound-a"})
	if err != nil {
		t.Fatalf("build owner spec: %v", err)
	}
	if spec.tag != "inbound-a" || spec.listenPort != 8443 || len(spec.portRanges) != 1 || spec.portRanges[0] != (managedPortRange{start: 443, end: 444}) || len(spec.protocols) != 2 {
		t.Fatalf("unexpected owner spec: %+v", spec)
	}
}
