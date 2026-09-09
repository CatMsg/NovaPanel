package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

type managedPortConflictError struct {
	ownerKind string
	ownerTag  string
	conflicts []string
}

func (e *managedPortConflictError) Error() string {
	if e == nil || len(e.conflicts) == 0 {
		return ""
	}

	scope := strings.TrimSpace(e.ownerKind)
	if strings.TrimSpace(e.ownerTag) != "" {
		scope = fmt.Sprintf("%s %s", scope, strings.TrimSpace(e.ownerTag))
	}
	if scope == "" {
		scope = "当前对象"
	}

	return fmt.Sprintf("保存失败：%s 的端口 %s 已被占用，请先修改端口", scope, strings.Join(e.conflicts, "、"))
}

func validateManagedPortConflicts(tx *gorm.DB, ownerKind string, ownerTag string, skipInboundID uint, skipEndpointID uint, candidatePorts []int) error {
	return validateManagedPortProtocolConflicts(tx, ownerKind, ownerTag, skipInboundID, skipEndpointID, candidatePorts, managedForwardProtocols)
}

func validateManagedPortProtocolConflicts(tx *gorm.DB, ownerKind string, ownerTag string, skipInboundID uint, skipEndpointID uint, candidatePorts []int, candidateProtocols []string) error {
	return validateManagedPortRangeProtocolConflicts(tx, ownerKind, ownerTag, skipInboundID, skipEndpointID, managedPortRangesFromPorts(candidatePorts), candidateProtocols)
}

func validateManagedPortRangeConflicts(tx *gorm.DB, ownerKind string, ownerTag string, skipInboundID uint, skipEndpointID uint, candidateRanges []managedPortRange) error {
	return validateManagedPortRangeProtocolConflicts(tx, ownerKind, ownerTag, skipInboundID, skipEndpointID, candidateRanges, managedForwardProtocols)
}

func validateManagedPortRangeProtocolConflicts(tx *gorm.DB, ownerKind string, ownerTag string, skipInboundID uint, skipEndpointID uint, candidateRanges []managedPortRange, candidateProtocols []string) error {
	candidateRanges = normalizeManagedPortRanges(candidateRanges)
	candidateProtocols = normalizeManagedProtocols(candidateProtocols)
	if len(candidateRanges) == 0 || len(candidateProtocols) == 0 {
		return nil
	}

	entries, err := findManagedPortRangeConflictEntries(tx, candidateRanges, skipInboundID, skipEndpointID)
	if err != nil {
		return err
	}

	conflictMap := make(map[string][]string)
	for _, entry := range entries {
		entryEnd := entry.EndPort
		if entryEnd < entry.Port {
			entryEnd = entry.Port
		}
		entryRange := managedPortRange{start: entry.Port, end: entryEnd}
		protocols := intersectManagedProtocols(candidateProtocols, managedPortEntryProtocols(entry.Protocols))
		if len(protocols) == 0 {
			continue
		}
		for _, candidate := range candidateRanges {
			overlap, ok := managedPortRangesOverlap(candidate, entryRange)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%s/%s", formatManagedPortRange(overlap, "-"), strings.Join(protocols, ","))
			conflictMap[key] = appendUniqueUsage(conflictMap[key], fmt.Sprintf("%s %s", managedPortEntryKind(entry.Scope), entry.OwnerTag))
		}
	}

	if len(conflictMap) == 0 {
		return nil
	}

	conflictRanges := make([]string, 0, len(conflictMap))
	for item := range conflictMap {
		conflictRanges = append(conflictRanges, item)
	}
	sort.Slice(conflictRanges, func(i, j int) bool {
		leftRange, _, _ := strings.Cut(conflictRanges[i], "/")
		rightRange, _, _ := strings.Cut(conflictRanges[j], "/")
		left, _ := parseManagedPortRange(leftRange)
		right, _ := parseManagedPortRange(rightRange)
		if left.start == right.start {
			return conflictRanges[i] < conflictRanges[j]
		}
		return left.start < right.start
	})

	conflicts := make([]string, 0, len(conflictRanges))
	for _, item := range conflictRanges {
		conflicts = append(conflicts, fmt.Sprintf("%s(%s)", item, strings.Join(conflictMap[item], "、")))
	}

	return &managedPortConflictError{
		ownerKind: ownerKind,
		ownerTag:  ownerTag,
		conflicts: conflicts,
	}
}

func validateManagedPanelPortConflicts(tx *gorm.DB, webPort int, subPort int) error {
	candidatePorts := normalizeManagedPorts([]int{webPort, subPort})
	if len(candidatePorts) == 0 {
		return nil
	}
	return validateManagedPortProtocolConflicts(tx, "面板", fmt.Sprintf("web=%d sub=%d", webPort, subPort), 0, 0, candidatePorts, managedPanelApplyProtocols)
}

func findManagedPortRangeConflictEntries(tx *gorm.DB, ranges []managedPortRange, skipInboundID uint, skipEndpointID uint) ([]model.ManagedPortEntry, error) {
	ranges = normalizeManagedPortRanges(ranges)
	if len(ranges) == 0 {
		return nil, nil
	}

	var entries []model.ManagedPortEntry
	query := tx.Model(&model.ManagedPortEntry{})
	overlap := tx.Where("1 = 0")
	for _, item := range ranges {
		overlap = overlap.Or("(CASE WHEN end_port >= port THEN end_port ELSE port END) >= ? AND port <= ?", item.start, item.end)
	}
	query = query.Where(overlap)
	query = query.Where(
		"(scope <> ? OR owner_id <> ?) AND (scope <> ? OR owner_id <> ?)",
		managedPortScopeInbound, skipInboundID,
		managedPortScopeEndpoint, skipEndpointID,
	)
	if err := query.Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

func managedPortEntryKind(scope string) string {
	switch scope {
	case managedPortScopeInbound:
		return "入站"
	case managedPortScopeEndpoint:
		return "节点"
	default:
		return "对象"
	}
}

func managedPortEntryProtocols(raw string) []string {
	protocols := normalizeManagedProtocols(strings.Split(raw, ","))
	if len(protocols) == 0 {
		return append([]string(nil), managedForwardProtocols...)
	}
	return protocols
}

func intersectManagedProtocols(left, right []string) []string {
	left = normalizeManagedProtocols(left)
	right = normalizeManagedProtocols(right)
	if len(left) == 0 || len(right) == 0 {
		return nil
	}

	rightSet := make(map[string]struct{}, len(right))
	for _, protocol := range right {
		rightSet[protocol] = struct{}{}
	}
	intersection := make([]string, 0, len(left))
	for _, protocol := range left {
		if _, ok := rightSet[protocol]; ok {
			intersection = append(intersection, protocol)
		}
	}
	return intersection
}

func collectEndpointManagedPorts(endpoint *model.Endpoint) ([]int, error) {
	if endpoint == nil {
		return nil, nil
	}

	_, ports, _, active, err := collectEndpointForwardPorts(endpoint)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, nil
	}
	return ports, nil
}

func normalizeManagedPorts(ports []int) []int {
	if len(ports) == 0 {
		return nil
	}

	seen := make(map[int]struct{}, len(ports))
	normalized := make([]int, 0, len(ports))
	for _, port := range ports {
		if port < 1 || port > 65535 {
			continue
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		normalized = append(normalized, port)
	}
	sort.Ints(normalized)
	return normalized
}

func appendUniqueUsage(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}
