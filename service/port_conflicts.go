package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

type managedPortUsage struct {
	kind  string
	tag   string
	ports []int
}

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

	usages, err := collectManagedPortUsages(tx, skipInboundID, skipEndpointID)
	if err != nil {
		return err
	}

	conflictMap := make(map[int][]string)
	for _, usage := range usages {
		for _, port := range usage.ports {
			if _, ok := candidateSet[port]; !ok {
				continue
			}
			conflictMap[port] = appendUniqueUsage(conflictMap[port], fmt.Sprintf("%s %s", usage.kind, usage.tag))
		}
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

func collectManagedPortUsages(tx *gorm.DB, skipInboundID uint, skipEndpointID uint) ([]managedPortUsage, error) {
	usages := make([]managedPortUsage, 0)

	var inbounds []*model.Inbound
	if err := tx.Model(model.Inbound{}).Find(&inbounds).Error; err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		if inbound == nil || inbound.Id == skipInboundID {
			continue
		}
		_, ports, err := collectInboundForwardPorts(inbound)
		if err != nil {
			return nil, err
		}
		if len(ports) == 0 {
			continue
		}
		usages = append(usages, managedPortUsage{
			kind:  "入站",
			tag:   inbound.Tag,
			ports: normalizeManagedPorts(ports),
		})
	}

	var endpoints []*model.Endpoint
	if err := tx.Model(model.Endpoint{}).Find(&endpoints).Error; err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		if endpoint == nil || endpoint.Id == skipEndpointID {
			continue
		}
		ports, err := collectEndpointManagedPorts(endpoint)
		if err != nil {
			return nil, err
		}
		if len(ports) == 0 {
			continue
		}
		usages = append(usages, managedPortUsage{
			kind:  "节点",
			tag:   endpoint.Tag,
			ports: normalizeManagedPorts(ports),
		})
	}

	return usages, nil
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
