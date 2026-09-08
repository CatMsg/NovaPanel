package service

import (
	"crypto/sha256"
	"fmt"
	"regexp"
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
	ID         string `json:"id"`
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Scope      string `json:"scope,omitempty"`
	Family     string `json:"family,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Port       string `json:"port,omitempty"`
	ToPorts    string `json:"to_ports,omitempty"`
	Chain      string `json:"chain,omitempty"`
	OwnerTag   string `json:"owner_tag,omitempty"`
	Count      int    `json:"count,omitempty"`
	Detail     string `json:"detail"`
	Repairable bool   `json:"repairable"`
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
		return finalizePortDriftReport(PortDriftReport{
			Status:    "unsupported",
			CheckedAt: time.Now().Format(time.RFC3339),
			Issues:    []PortDriftIssue{{Type: "unsupported", Severity: "info", Detail: "当前系统不是 Linux，跳过防火墙规则漂移检测"}},
		})
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
	return finalizePortDriftReport(report)
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
				Scope:    rule.scope,
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
				Scope:    rule.scope,
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
				Scope:    rule.scope,
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
	managedPorts := make(map[string]portDriftRule, len(desired))
	for _, rule := range desired {
		managedPorts[rule.protocol+"|"+rule.dport] = rule
	}
	for _, entry := range actualRules {
		if strings.HasPrefix(entry.Chain, "NPHY2_") || !strings.EqualFold(entry.Target, "REDIRECT") {
			continue
		}
		if _, expected := desiredKeys[portDriftActualComparisonKey(entry, backend)]; expected {
			continue
		}
		rule, managed := managedPorts[entry.Protocol+"|"+entry.DPort]
		if !managed {
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
			Scope:    rule.scope,
			Family:   entry.Family,
			Protocol: entry.Protocol,
			Port:     entry.DPort,
			ToPorts:  entry.ToPorts,
			Chain:    entry.Chain,
			OwnerTag: rule.ownerTag,
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
	return finalizePortDriftReport(report)
}

func finalizePortDriftReport(report PortDriftReport) PortDriftReport {
	for index := range report.Issues {
		issue := &report.Issues[index]
		issue.Repairable = issue.Type == "missing" || issue.Type == "duplicate" || issue.Type == "unexpected" || issue.Type == "orphan"
		sum := sha256.Sum256([]byte(strings.Join([]string{
			issue.Type, issue.Scope, issue.Family, issue.Protocol, issue.Port,
			issue.ToPorts, issue.Chain, issue.OwnerTag,
		}, "|")))
		issue.ID = fmt.Sprintf("%x", sum[:8])
	}
	report.IssueCount = len(report.Issues)
	return report
}

var managedPortChainPattern = regexp.MustCompile(`^NPHY2_[a-f0-9]{12}$`)

// RepairPortDriftIssue rechecks the requested issue before changing firewall
// state. Desired-rule issues reapply only their owner; orphan cleanup can only
// remove a chain created by NovaPanel.
func RepairPortDriftIssue(issueID string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("当前系统不支持端口规则修复")
	}
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return fmt.Errorf("缺少端口问题 ID")
	}

	clearServerStatusCache("ports", "sys")
	natIPv4, natIPv6, collectionErrors := collectNatEntries()
	report := detectPortDrift(natIPv4, natIPv6, detectFirewallBackend(), collectionErrors)
	var issue *PortDriftIssue
	for index := range report.Issues {
		if report.Issues[index].ID == issueID {
			issue = &report.Issues[index]
			break
		}
	}
	if issue == nil {
		return fmt.Errorf("端口问题已不存在，请刷新诊断结果")
	}
	if !issue.Repairable {
		return fmt.Errorf("该问题不能自动修复: %s", issue.Detail)
	}

	if issue.Type == "orphan" {
		if !managedPortChainPattern.MatchString(issue.Chain) {
			return fmt.Errorf("拒绝删除非 NovaPanel 链: %s", issue.Chain)
		}
		if err := removeManagedOrphanChain(issue.Chain, issue.Family); err != nil {
			return err
		}
	} else {
		desired, loadErrors := loadDesiredPortDriftRules()
		if len(loadErrors) > 0 {
			return fmt.Errorf("读取期望端口规则失败: %s", strings.Join(loadErrors, "; "))
		}
		spec, err := managedForwardSpecForDriftIssue(desired, *issue)
		if err != nil {
			return err
		}
		if err := applyManagedForwardSpec(spec); err != nil {
			return err
		}
	}

	clearServerStatusCache("ports", "sys")
	natIPv4, natIPv6, collectionErrors = collectNatEntries()
	refreshed := detectPortDrift(natIPv4, natIPv6, detectFirewallBackend(), collectionErrors)
	for _, current := range refreshed.Issues {
		if current.ID == issueID {
			return fmt.Errorf("单项修复后问题仍然存在，请使用全部重建: %s", current.Detail)
		}
	}
	return nil
}

func managedForwardSpecForDriftIssue(desired []portDriftRule, issue PortDriftIssue) (managedForwardSpec, error) {
	ownerTag := strings.TrimSpace(issue.OwnerTag)
	if ownerTag == "" {
		return managedForwardSpec{}, fmt.Errorf("端口问题缺少规则归属")
	}
	spec := managedForwardSpec{tag: ownerTag, active: true}
	for _, rule := range desired {
		if rule.ownerTag != ownerTag || (issue.Scope != "" && rule.scope != issue.Scope) {
			continue
		}
		portRange, err := parseManagedPortRange(rule.dport)
		if err != nil {
			return managedForwardSpec{}, fmt.Errorf("无效期望端口 %q: %w", rule.dport, err)
		}
		toPort, err := strconv.Atoi(rule.toPorts)
		if err != nil {
			return managedForwardSpec{}, fmt.Errorf("无效目标端口 %q: %w", rule.toPorts, err)
		}
		if spec.listenPort != 0 && spec.listenPort != toPort {
			return managedForwardSpec{}, fmt.Errorf("规则归属 %s 存在多个目标端口", ownerTag)
		}
		spec.listenPort = toPort
		spec.portRanges = append(spec.portRanges, portRange)
		spec.protocols = append(spec.protocols, rule.protocol)
	}
	spec.removeProtocols = append([]string(nil), spec.protocols...)
	spec = spec.normalized()
	if !spec.active {
		return managedForwardSpec{}, fmt.Errorf("未找到端口问题对应的期望规则")
	}
	return spec, nil
}

func removeManagedOrphanChain(chain, family string) error {
	if !managedPortChainPattern.MatchString(chain) {
		return fmt.Errorf("无效 NovaPanel 链: %s", chain)
	}
	if family != "ipv4" && family != "ipv6" && family != "ip" && family != "ip6" {
		return fmt.Errorf("无效地址族: %s", family)
	}
	portForwardingMu.Lock()
	defer portForwardingMu.Unlock()
	_, err := runCommandOutput(externalCommandTimeout, "bash", hy2ForwardScriptPath(), "remove-chain", chain, family)
	if err != nil {
		return formatExternalCommandError("remove orphan port chain failed", err)
	}
	return nil
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
	if item, err := parseManagedPortRange(dport); err == nil {
		dport = formatManagedPortRange(item, ":")
	}
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
		spec = spec.normalized()
		if !spec.active {
			return
		}
		for _, item := range spec.portRanges {
			for _, protocol := range spec.protocols {
				desired = append(desired, portDriftRule{
					chain:    managedPortChain(spec.tag),
					protocol: protocol,
					dport:    formatManagedPortRange(item, ":"),
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
		appendSpec(managedForwardSpec{tag: managedWebPortForwardTag, listenPort: port, portRanges: []managedPortRange{{start: port, end: port}}, protocols: []string{"tcp"}, active: true}.normalized(), "panel", managedWebPortForwardTag)
	} else {
		errors = append(errors, fmt.Sprintf("panel web port query failed: %v", err))
	}
	if port, err := settingService.GetSubPort(); err == nil {
		appendSpec(managedForwardSpec{tag: managedSubPortForwardTag, listenPort: port, portRanges: []managedPortRange{{start: port, end: port}}, protocols: []string{"tcp"}, active: true}.normalized(), "panel", managedSubPortForwardTag)
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
