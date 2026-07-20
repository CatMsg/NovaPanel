package service

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

type portDriftRule struct {
	chain    string
	protocol string
	dport    string
	toPorts  string
	scope    string
	ownerTag string
}

type PortDriftIssue struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Family   string `json:"family,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Port     string `json:"port,omitempty"`
	ToPorts  string `json:"to_ports,omitempty"`
	Chain    string `json:"chain,omitempty"`
	OwnerTag string `json:"owner_tag,omitempty"`
	Count    int    `json:"count,omitempty"`
	Detail   string `json:"detail"`
}

type PortDriftReport struct {
	Status             string           `json:"status"`
	CheckedAt          string           `json:"checked_at"`
	DesiredRules       int              `json:"desired_rules"`
	ActualManagedRules int              `json:"actual_managed_rules"`
	IssueCount         int              `json:"issue_count"`
	MissingCount       int              `json:"missing_count"`
	DuplicateCount     int              `json:"duplicate_count"`
	OrphanCount        int              `json:"orphan_count"`
	UnexpectedCount    int              `json:"unexpected_count"`
	Issues             []PortDriftIssue `json:"issues"`
}

func detectPortDrift(natIPv4, natIPv6 []PortNatEntry, backend string, collectionErrors []string) PortDriftReport {
	if runtime.GOOS != "linux" {
		return PortDriftReport{
			Status:    "unsupported",
			CheckedAt: time.Now().Format(time.RFC3339),
			Issues:    []PortDriftIssue{{Type: "unsupported", Severity: "info", Detail: "当前系统不是 Linux，跳过防火墙规则漂移检测"}},
		}
	}
	desired, desiredErrors := loadDesiredPortDriftRules()
	allErrors := append(append([]string(nil), collectionErrors...), desiredErrors...)
	report := buildPortDriftReport(desired, natIPv4, natIPv6, backend)
	if len(allErrors) > 0 {
		report.Status = "unknown"
		for _, err := range allErrors {
			report.Issues = append(report.Issues, PortDriftIssue{
				Type:     "inspection-error",
				Severity: "warning",
				Detail:   err,
			})
		}
		report.IssueCount = len(report.Issues)
	}
	if backend == "unknown" && report.DesiredRules > 0 {
		report.Status = "unknown"
	}
	report.CheckedAt = time.Now().Format(time.RFC3339)
	return report
}

func buildPortDriftReport(desired []portDriftRule, natIPv4, natIPv6 []PortNatEntry, backend string) PortDriftReport {
	report := PortDriftReport{
		Status:       "healthy",
		DesiredRules: len(desired),
		Issues:       make([]PortDriftIssue, 0),
	}

	desiredKeys := make(map[string]portDriftRule, len(desired))
	for _, rule := range desired {
		key := portDriftDesiredRuleKey(rule, backend)
		if _, duplicate := desiredKeys[key]; duplicate {
			report.IssueCount++
			report.Issues = append(report.Issues, PortDriftIssue{
				Type:     "desired-duplicate",
				Severity: "error",
				Protocol: rule.protocol,
				Port:     rule.dport,
				ToPorts:  rule.toPorts,
				Chain:    rule.chain,
				OwnerTag: rule.ownerTag,
				Detail:   "数据库中的期望规则重复",
			})
			continue
		}
		desiredKeys[key] = rule
	}

	actualByRuleFamily := make(map[string]int)
	actualRules := make([]PortNatEntry, 0, len(natIPv4)+len(natIPv6))
	actualRules = append(actualRules, natIPv4...)
	actualRules = append(actualRules, natIPv6...)
	for _, entry := range actualRules {
		key, managed := portDriftActualRuleKey(entry, backend, desiredKeys)
		if !managed {
			continue
		}
		report.ActualManagedRules++
		actualByRuleFamily[entry.Family+"|"+key]++
	}

	processedDesired := make(map[string]struct{}, len(desiredKeys))
	for _, rule := range desired {
		key := portDriftDesiredRuleKey(rule, backend)
		if _, processed := processedDesired[key]; processed {
			continue
		}
		processedDesired[key] = struct{}{}

		familyCounts := make(map[string]int)
		for familyAndKey, count := range actualByRuleFamily {
			if strings.HasSuffix(familyAndKey, "|"+key) {
				family := strings.TrimSuffix(familyAndKey, "|"+key)
				familyCounts[family] = count
			}
		}
		if len(familyCounts) == 0 {
			report.MissingCount++
			report.IssueCount++
			report.Issues = append(report.Issues, PortDriftIssue{
				Type:     "missing",
				Severity: "error",
				Protocol: rule.protocol,
				Port:     rule.dport,
				ToPorts:  rule.toPorts,
				Chain:    rule.chain,
				OwnerTag: rule.ownerTag,
				Detail:   "期望的端口转发规则不存在",
			})
		}
		for family, count := range familyCounts {
			if count <= 1 {
				continue
			}
			report.DuplicateCount += count - 1
			report.IssueCount++
			report.Issues = append(report.Issues, PortDriftIssue{
				Type:     "duplicate",
				Severity: "error",
				Family:   family,
				Protocol: rule.protocol,
				Port:     rule.dport,
				ToPorts:  rule.toPorts,
				Chain:    rule.chain,
				OwnerTag: rule.ownerTag,
				Count:    count,
				Detail:   fmt.Sprintf("同一地址族存在 %d 条相同的受管规则", count),
			})
		}
	}

	for _, entry := range actualRules {
		if !strings.HasPrefix(entry.Chain, "NPHY2_") || !strings.EqualFold(entry.Target, "REDIRECT") {
			continue
		}
		key := portDriftActualComparisonKey(entry, backend)
		if _, expected := desiredKeys[key]; expected {
			continue
		}
		report.OrphanCount++
		report.IssueCount++
		report.Issues = append(report.Issues, PortDriftIssue{
			Type:     "orphan",
			Severity: "error",
			Family:   entry.Family,
			Protocol: entry.Protocol,
			Port:     entry.DPort,
			ToPorts:  entry.ToPorts,
			Chain:    entry.Chain,
			Detail:   "存在不再对应当前配置的受管规则",
		})
	}

	// A direct REDIRECT outside a NovaPanel chain is usually a stale rule left
	// by an older implementation. Only report it when it targets a managed port.
	managedPorts := make(map[string]struct{}, len(desired))
	for _, rule := range desired {
		managedPorts[rule.protocol+"|"+rule.dport] = struct{}{}
	}
	for _, entry := range actualRules {
		if strings.HasPrefix(entry.Chain, "NPHY2_") || !strings.EqualFold(entry.Target, "REDIRECT") {
			continue
		}
		if _, expected := desiredKeys[portDriftActualComparisonKey(entry, backend)]; expected {
			continue
		}
		if _, managed := managedPorts[entry.Protocol+"|"+entry.DPort]; !managed {
			continue
		}
		report.UnexpectedCount++
		report.IssueCount++
		detail := "受管端口存在未归属 NovaPanel 链的 REDIRECT 规则"
		if strings.EqualFold(backend, "UFW") {
			detail = "UFW 受管端口存在目标不匹配的 REDIRECT 规则"
		}
		report.Issues = append(report.Issues, PortDriftIssue{
			Type:     "unexpected",
			Severity: "warning",
			Family:   entry.Family,
			Protocol: entry.Protocol,
			Port:     entry.DPort,
			ToPorts:  entry.ToPorts,
			Chain:    entry.Chain,
			Detail:   detail,
		})
	}

	if report.IssueCount > 0 {
		report.Status = "drift"
	}
	sort.SliceStable(report.Issues, func(i, j int) bool {
		if report.Issues[i].Severity != report.Issues[j].Severity {
			return report.Issues[i].Severity == "error"
		}
		return report.Issues[i].Type < report.Issues[j].Type
	})
	return report
}

func portDriftDesiredRuleKey(rule portDriftRule, backend string) string {
	if strings.EqualFold(backend, "UFW") {
		return portDriftRuleKey("PREROUTING", rule.protocol, rule.dport, rule.toPorts)
	}
	return portDriftRuleKey(rule.chain, rule.protocol, rule.dport, rule.toPorts)
}

func portDriftActualComparisonKey(entry PortNatEntry, backend string) string {
	chain := entry.Chain
	if strings.EqualFold(backend, "UFW") && strings.EqualFold(chain, "PREROUTING") {
		chain = "PREROUTING"
	}
	return portDriftRuleKey(chain, entry.Protocol, entry.DPort, entry.ToPorts)
}

func portDriftActualRuleKey(entry PortNatEntry, backend string, desiredKeys map[string]portDriftRule) (string, bool) {
	if !strings.EqualFold(entry.Target, "REDIRECT") {
		return "", false
	}
	key := portDriftActualComparisonKey(entry, backend)
	if strings.HasPrefix(entry.Chain, "NPHY2_") {
		return key, true
	}
	if strings.EqualFold(backend, "UFW") && strings.EqualFold(entry.Chain, "PREROUTING") {
		_, expected := desiredKeys[key]
		return key, expected
	}
	return "", false
}

func portDriftRuleKey(chain, protocol, dport, toPorts string) string {
	return strings.Join([]string{chain, strings.ToLower(strings.TrimSpace(protocol)), strings.TrimSpace(dport), strings.TrimSpace(toPorts)}, "|")
}

func managedPortChain(tag string) string {
	sum := sha256.Sum256([]byte(tag))
	return fmt.Sprintf("NPHY2_%x", sum[:6])
}

func loadDesiredPortDriftRules() ([]portDriftRule, []string) {
	db := database.GetDB()
	if db == nil {
		return nil, []string{"database is not initialized"}
	}
	desired := make([]portDriftRule, 0)
	errors := make([]string, 0)
	appendSpec := func(spec managedForwardSpec, scope, ownerTag string) {
		if !spec.active {
			return
		}
		for _, port := range spec.ports {
			for _, protocol := range spec.protocols {
				desired = append(desired, portDriftRule{
					chain:    managedPortChain(spec.tag),
					protocol: protocol,
					dport:    strconv.Itoa(port),
					toPorts:  strconv.Itoa(spec.listenPort),
					scope:    scope,
					ownerTag: ownerTag,
				})
			}
		}
	}

	var inbounds []*model.Inbound
	if err := db.Model(&model.Inbound{}).Find(&inbounds).Error; err != nil {
		errors = append(errors, fmt.Sprintf("inbound desired rules query failed: %v", err))
	} else {
		for _, inbound := range inbounds {
			spec, err := collectInboundForwardSpec(inbound)
			if err != nil {
				errors = append(errors, fmt.Sprintf("inbound %s: %v", inbound.Tag, err))
				continue
			}
			appendSpec(spec, managedPortScopeInbound, inbound.Tag)
		}
	}

	var endpoints []*model.Endpoint
	if err := db.Model(&model.Endpoint{}).Find(&endpoints).Error; err != nil {
		errors = append(errors, fmt.Sprintf("endpoint desired rules query failed: %v", err))
	} else {
		for _, endpoint := range endpoints {
			spec, err := collectEndpointForwardSpec(endpoint)
			if err != nil {
				errors = append(errors, fmt.Sprintf("endpoint %s: %v", endpoint.Tag, err))
				continue
			}
			appendSpec(spec, managedPortScopeEndpoint, endpoint.Tag)
		}
	}

	settingService := &SettingService{}
	if port, err := settingService.GetPort(); err == nil {
		appendSpec(managedForwardSpec{tag: managedWebPortForwardTag, listenPort: port, ports: []int{port}, protocols: []string{"tcp"}, active: true}, "panel", managedWebPortForwardTag)
	} else {
		errors = append(errors, fmt.Sprintf("panel web port query failed: %v", err))
	}
	if port, err := settingService.GetSubPort(); err == nil {
		appendSpec(managedForwardSpec{tag: managedSubPortForwardTag, listenPort: port, ports: []int{port}, protocols: []string{"tcp"}, active: true}, "panel", managedSubPortForwardTag)
	} else {
		errors = append(errors, fmt.Sprintf("panel subscription port query failed: %v", err))
	}

	return desired, errors
}

func parseNftRedirectRule(raw string) (protocol, dport, toPorts string) {
	fields := strings.Fields(raw)
	for index, field := range fields {
		if (field == "tcp" || field == "udp") && protocol == "" {
			protocol = field
		}
		if field == "dport" && index+1 < len(fields) {
			dport = strings.Trim(fields[index+1], "{},")
		}
		if field == "redirect" && index+2 < len(fields) && fields[index+1] == "to" {
			toPorts = strings.TrimPrefix(fields[index+2], ":")
		}
	}
	return protocol, dport, toPorts
}
