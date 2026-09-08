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
	return validateManagedPortRangeConflicts(tx, ownerKind, ownerTag, skipInboundID, skipEndpointID, managedPortRangesFromPorts(candidatePorts))
}

func validateManagedPortRangeConflicts(tx *gorm.DB, ownerKind string, ownerTag string, skipInboundID uint, skipEndpointID uint, candidateRanges []managedPortRange) error {
	candidateRanges = normalizeManagedPortRanges(candidateRanges)
	if len(candidateRanges) == 0 {
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
		for _, candidate := range candidateRanges {
			overlap, ok := managedPortRangesOverlap(candidate, entryRange)
			if !ok {
				continue
			}
			key := formatManagedPortRange(overlap, "-")
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
		left, _ := parseManagedPortRange(conflictRanges[i])
		right, _ := parseManagedPortRange(conflictRanges[j])
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
	return validateManagedPortConflicts(tx, "面板", fmt.Sprintf("web=%d sub=%d", webPort, subPort), 0, 0, candidatePorts)
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
