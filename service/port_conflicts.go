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
	ports := normalizeManagedPorts(candidatePorts)
	if len(ports) == 0 {
		return nil
	}

	candidateSet := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		candidateSet[port] = struct{}{}
	}

	entries, err := findManagedPortConflictEntries(tx, ports, skipInboundID, skipEndpointID)
	if err != nil {
		return err
	}

	conflictMap := make(map[int][]string)
	for _, entry := range entries {
		if _, ok := candidateSet[entry.Port]; !ok {
			continue
		}
		conflictMap[entry.Port] = appendUniqueUsage(conflictMap[entry.Port], fmt.Sprintf("%s %s", managedPortEntryKind(entry.Scope), entry.OwnerTag))
	}

	if len(conflictMap) == 0 {
		return nil
	}

	conflictPorts := make([]int, 0, len(conflictMap))
	for port := range conflictMap {
		conflictPorts = append(conflictPorts, port)
	}
	sort.Ints(conflictPorts)

	conflicts := make([]string, 0, len(conflictPorts))
	for _, port := range conflictPorts {
		conflicts = append(conflicts, fmt.Sprintf("%d(%s)", port, strings.Join(conflictMap[port], "、")))
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

func findManagedPortConflictEntries(tx *gorm.DB, candidatePorts []int, skipInboundID uint, skipEndpointID uint) ([]model.ManagedPortEntry, error) {
	ports := normalizeManagedPorts(candidatePorts)
	if len(ports) == 0 {
		return nil, nil
	}

	var entries []model.ManagedPortEntry
	query := tx.Model(&model.ManagedPortEntry{}).Where("port IN ?", ports)
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
